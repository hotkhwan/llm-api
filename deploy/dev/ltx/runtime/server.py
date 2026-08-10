#!/usr/bin/env python3
"""Private one-at-a-time LTX-2.3 distilled image-to-audio-video wrapper."""

import base64
import json
import os
import secrets
import subprocess
import tempfile
import threading
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from PIL import Image, ImageOps

AUTH = os.environ["LTX_API_KEY"]
MODEL_DIR = Path(os.environ.get("LTX_MODEL_DIR", "/models"))
SOURCE_ROOT = os.environ.get("LTX_ROOT", "/runtime/LTX-2")
TIMEOUT = int(os.environ.get("LTX_GENERATION_TIMEOUT_SECONDS", "1200"))
LOCK = threading.Lock()


def decode_reference(value: str) -> tuple[str, bytes]:
    prefix, encoded = value.split(",", 1)
    allowed = {"data:image/jpeg;base64": ".jpg", "data:image/png;base64": ".png", "data:image/webp;base64": ".webp"}
    if prefix not in allowed:
        raise ValueError("referenceImage must be JPEG, PNG or WebP data URL")
    raw = base64.b64decode(encoded, validate=True)
    if not raw or len(raw) > 16 * 1024 * 1024:
        raise ValueError("referenceImage is empty or too large")
    return allowed[prefix], raw


def prepare_reference(source: Path, target: Path):
    with Image.open(source) as image:
        image = ImageOps.exif_transpose(image).convert("RGB")
        contained = ImageOps.contain(image, (448, 768), Image.Resampling.LANCZOS)
        canvas = Image.new("RGB", (448, 768), (238, 238, 238))
        canvas.paste(contained, ((448 - contained.width) // 2, (768 - contained.height) // 2))
        canvas.save(target, format="PNG", optimize=True)


class Handler(BaseHTTPRequestHandler):
    server_version = "kwanni-ltx/0.1"

    def log_message(self, fmt, *args):
        print(json.dumps({"component": "kwanni-ltx", "message": fmt % args}))

    def send_json(self, status: int, payload: dict):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/healthz":
            self.send_json(200, {"status": "ok", "busy": LOCK.locked(), "model": "LTX-2.3-22B-distilled-1.1"})
            return
        self.send_json(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/generate":
            self.send_json(404, {"error": "not found"})
            return
        if not secrets.compare_digest(self.headers.get("Authorization", ""), "Bearer " + AUTH):
            self.send_json(401, {"error": "unauthorized"})
            return
        if not LOCK.acquire(blocking=False):
            self.send_json(409, {"error": "local preview worker is busy"})
            return
        try:
            size = int(self.headers.get("Content-Length", "0"))
            if size <= 0 or size > 24 * 1024 * 1024:
                raise ValueError("request body is empty or too large")
            payload = json.loads(self.rfile.read(size))
            prompt = str(payload.get("prompt", "")).strip()
            if not prompt or len(prompt) > 24000:
                raise ValueError("prompt is empty or too long")
            extension, image = decode_reference(str(payload.get("referenceImage", "")))
            task_id = str(uuid.uuid4())
            with tempfile.TemporaryDirectory(prefix="kwanni-ltx-") as directory:
                root = Path(directory)
                source_path = root / ("source" + extension)
                image_path = root / "reference-portrait.png"
                output_path = root / "preview.mp4"
                source_path.write_bytes(image)
                prepare_reference(source_path, image_path)
                command = [
                    "/runtime/venv/bin/python", "-m", "ltx_pipelines.distilled",
                    "--distilled-checkpoint-path", str(MODEL_DIR / "ltx-2.3-22b-distilled-1.1.safetensors"),
                    "--spatial-upsampler-path", str(MODEL_DIR / "ltx-2.3-spatial-upscaler-x2-1.1.safetensors"),
                    "--gemma-root", str(MODEL_DIR / "gemma-3-12b"),
                    "--seed", "42", "--height", "768", "--width", "448",
                    "--num-frames", "121", "--frame-rate", "24",
                    "--image", str(image_path), "0", "1.0", "0",
                    "--quantization", "fp8-cast", "--offload", "cpu",
                    "--output-path", str(output_path), "--prompt", prompt,
                ]
                subprocess.run(command, check=True, timeout=TIMEOUT, cwd=SOURCE_ROOT)
                result = output_path.read_bytes()
                if not result:
                    raise RuntimeError("LTX produced an empty preview")
            self.send_response(200)
            self.send_header("Content-Type", "video/mp4")
            self.send_header("Content-Length", str(len(result)))
            self.send_header("X-Kwanni-Task-ID", task_id)
            self.end_headers()
            self.wfile.write(result)
        except (ValueError, json.JSONDecodeError) as error:
            self.send_json(400, {"error": str(error)})
        except subprocess.TimeoutExpired:
            self.send_json(504, {"error": "LTX generation exceeded its bounded timeout"})
        except Exception as error:
            self.send_json(500, {"error": type(error).__name__})
        finally:
            LOCK.release()


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8092), Handler).serve_forever()
