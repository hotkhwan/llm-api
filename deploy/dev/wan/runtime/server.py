#!/usr/bin/env python3
"""Private synchronous wrapper for one Wan2.2 TI2V preview at a time."""

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

AUTH = os.environ["WAN_API_KEY"]
MODEL_DIR = os.environ.get("WAN_MODEL_DIR", "/models")
WAN_ROOT = os.environ.get("WAN_ROOT", "/opt/wan/Wan2.2")
TIMEOUT = int(os.environ.get("WAN_GENERATION_TIMEOUT_SECONDS", "2700"))
LOCK = threading.Lock()


def decode_reference(value: str) -> tuple[str, bytes]:
    prefix, encoded = value.split(",", 1)
    if prefix not in ("data:image/jpeg;base64", "data:image/png;base64", "data:image/webp;base64"):
        raise ValueError("referenceImage must be JPEG, PNG or WebP data URL")
    raw = base64.b64decode(encoded, validate=True)
    if not raw or len(raw) > 16 * 1024 * 1024:
        raise ValueError("referenceImage is empty or too large")
    extension = {"data:image/jpeg;base64": ".jpg", "data:image/png;base64": ".png", "data:image/webp;base64": ".webp"}[prefix]
    return extension, raw


def prepare_portrait_reference(source: Path, target: Path):
    """Contain the exact image on a portrait canvas; never crop or stretch it."""
    with Image.open(source) as image:
        image = ImageOps.exif_transpose(image).convert("RGB")
        contained = ImageOps.contain(image, (704, 1280), Image.Resampling.LANCZOS)
        canvas = Image.new("RGB", (704, 1280), (238, 238, 238))
        offset = ((704 - contained.width) // 2, (1280 - contained.height) // 2)
        canvas.paste(contained, offset)
        canvas.save(target, format="PNG", optimize=True)


class Handler(BaseHTTPRequestHandler):
    server_version = "kwanni-wan/0.1"

    def log_message(self, fmt, *args):
        print(json.dumps({"component": "kwanni-wan", "message": fmt % args}))

    def send_json(self, status: int, payload: dict):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/healthz":
            self.send_json(200, {"status": "ok", "busy": LOCK.locked(), "model": "Wan2.2-TI2V-5B"})
            return
        self.send_json(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/generate":
            self.send_json(404, {"error": "not found"})
            return
        supplied = self.headers.get("Authorization", "")
        if not secrets.compare_digest(supplied, "Bearer " + AUTH):
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
            with tempfile.TemporaryDirectory(prefix="kwanni-wan-") as directory:
                root = Path(directory)
                source_path = root / ("source" + extension)
                image_path = root / "reference-portrait.png"
                output_path = root / "preview.mp4"
                source_path.write_bytes(image)
                prepare_portrait_reference(source_path, image_path)
                command = [
                    "python3", "/runtime/launcher.py",
                    "--task", "ti2v-5B", "--size", "704*1280",
                    "--ckpt_dir", MODEL_DIR, "--offload_model", "True",
                    "--convert_model_dtype", "--t5_cpu", "--frame_num", "121",
                    "--image", str(image_path), "--prompt", prompt,
                    "--save_file", str(output_path),
                ]
                subprocess.run(
                    command, check=True, timeout=TIMEOUT, cwd=WAN_ROOT,
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                )
                result = output_path.read_bytes()
                if not result:
                    raise RuntimeError("Wan produced an empty preview")
            self.send_response(200)
            self.send_header("Content-Type", "video/mp4")
            self.send_header("Content-Length", str(len(result)))
            self.send_header("X-Kwanni-Task-ID", task_id)
            self.end_headers()
            self.wfile.write(result)
        except (ValueError, json.JSONDecodeError) as error:
            self.send_json(400, {"error": str(error)})
        except subprocess.TimeoutExpired:
            self.send_json(504, {"error": "Wan generation exceeded its bounded timeout"})
        except Exception as error:
            self.send_json(500, {"error": type(error).__name__})
        finally:
            LOCK.release()


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8090), Handler).serve_forever()
