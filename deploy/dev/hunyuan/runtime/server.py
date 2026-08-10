#!/usr/bin/env python3
"""Private one-at-a-time HunyuanVideo-1.5 distilled I2V wrapper."""

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

AUTH = os.environ["HUNYUAN_API_KEY"]
MODEL_DIR = os.environ.get("HUNYUAN_MODEL_DIR", "/models")
SOURCE_ROOT = os.environ.get("HUNYUAN_ROOT", "/runtime/HunyuanVideo-1.5")
TIMEOUT = int(os.environ.get("HUNYUAN_GENERATION_TIMEOUT_SECONDS", "900"))
LOCK = threading.Lock()


def decode_reference(value: str) -> tuple[str, bytes]:
    prefix, encoded = value.split(",", 1)
    allowed = {
        "data:image/jpeg;base64": ".jpg",
        "data:image/png;base64": ".png",
        "data:image/webp;base64": ".webp",
    }
    if prefix not in allowed:
        raise ValueError("referenceImage must be JPEG, PNG or WebP data URL")
    raw = base64.b64decode(encoded, validate=True)
    if not raw or len(raw) > 16 * 1024 * 1024:
        raise ValueError("referenceImage is empty or too large")
    return allowed[prefix], raw


def prepare_portrait_reference(source: Path, target: Path):
    """Contain the immutable reference; never crop, stretch, or synthesize it."""
    with Image.open(source) as image:
        image = ImageOps.exif_transpose(image).convert("RGB")
        contained = ImageOps.contain(image, (480, 848), Image.Resampling.LANCZOS)
        canvas = Image.new("RGB", (480, 848), (238, 238, 238))
        offset = ((480 - contained.width) // 2, (848 - contained.height) // 2)
        canvas.paste(contained, offset)
        canvas.save(target, format="PNG", optimize=True)


class Handler(BaseHTTPRequestHandler):
    server_version = "kwanni-hunyuan/0.1"

    def log_message(self, fmt, *args):
        print(json.dumps({"component": "kwanni-hunyuan", "message": fmt % args}))

    def send_json(self, status: int, payload: dict):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/healthz":
            self.send_json(200, {"status": "ok", "busy": LOCK.locked(), "model": "HunyuanVideo-1.5-480p-I2V-step-distilled"})
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
            with tempfile.TemporaryDirectory(prefix="kwanni-hunyuan-") as directory:
                root = Path(directory)
                source_path = root / ("source" + extension)
                image_path = root / "reference-portrait.png"
                output_path = root / "preview.mp4"
                source_path.write_bytes(image)
                prepare_portrait_reference(source_path, image_path)
                command = [
                    "torchrun", "--nproc_per_node=1", str(Path(SOURCE_ROOT) / "generate.py"),
                    "--prompt", prompt, "--image_path", str(image_path),
                    "--resolution", "480p", "--aspect_ratio", "9:16",
                    "--model_path", MODEL_DIR, "--output_path", str(output_path),
                    "--video_length", "121", "--num_inference_steps", "12",
                    "--enable_step_distill", "true", "--cfg_distilled", "false",
                    "--rewrite", "false", "--sr", "false", "--sparse_attn", "false",
                    "--offloading", "true", "--group_offloading", "true",
                    "--overlap_group_offloading", "true", "--seed", "123",
                ]
                subprocess.run(command, check=True, timeout=TIMEOUT, cwd=SOURCE_ROOT)
                result = output_path.read_bytes()
                if not result:
                    raise RuntimeError("Hunyuan produced an empty preview")
            self.send_response(200)
            self.send_header("Content-Type", "video/mp4")
            self.send_header("Content-Length", str(len(result)))
            self.send_header("X-Kwanni-Task-ID", task_id)
            self.end_headers()
            self.wfile.write(result)
        except (ValueError, json.JSONDecodeError) as error:
            self.send_json(400, {"error": str(error)})
        except subprocess.TimeoutExpired:
            self.send_json(504, {"error": "Hunyuan generation exceeded its bounded timeout"})
        except Exception as error:
            self.send_json(500, {"error": type(error).__name__})
        finally:
            LOCK.release()


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8091), Handler).serve_forever()
