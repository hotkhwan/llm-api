#!/usr/bin/env python3
"""Private MuseTalk presenter lip-sync wrapper; not a product I2V endpoint."""

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

import yaml

AUTH = os.environ["MUSETALK_API_KEY"]
MODEL_DIR = os.environ.get("MUSETALK_MODEL_DIR", "/models")
SOURCE_ROOT = os.environ.get("MUSETALK_ROOT", "/runtime/MuseTalk")
TIMEOUT = int(os.environ.get("MUSETALK_GENERATION_TIMEOUT_SECONDS", "600"))
LOCK = threading.Lock()


def decode_data_url(value: str, allowed: dict[str, str], maximum: int) -> tuple[str, bytes]:
    prefix, encoded = value.split(",", 1)
    if prefix not in allowed:
        raise ValueError("unsupported presenter or audio content type")
    raw = base64.b64decode(encoded, validate=True)
    if not raw or len(raw) > maximum:
        raise ValueError("presenter or audio asset is empty or too large")
    return allowed[prefix], raw


class Handler(BaseHTTPRequestHandler):
    server_version = "kwanni-musetalk/0.1"

    def log_message(self, fmt, *args):
        print(json.dumps({"component": "kwanni-musetalk", "message": fmt % args}))

    def send_json(self, status: int, payload: dict):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/healthz":
            self.send_json(200, {"status": "ok", "busy": LOCK.locked(), "model": "MuseTalk-1.5"})
            return
        self.send_json(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/present":
            self.send_json(404, {"error": "not found"})
            return
        if not secrets.compare_digest(self.headers.get("Authorization", ""), "Bearer " + AUTH):
            self.send_json(401, {"error": "unauthorized"})
            return
        if not LOCK.acquire(blocking=False):
            self.send_json(409, {"error": "presenter worker is busy"})
            return
        try:
            size = int(self.headers.get("Content-Length", "0"))
            if size <= 0 or size > 96 * 1024 * 1024:
                raise ValueError("request body is empty or too large")
            payload = json.loads(self.rfile.read(size))
            video_ext, video = decode_data_url(str(payload.get("presenterVideo", "")), {"data:video/mp4;base64": ".mp4"}, 64 << 20)
            audio_ext, audio = decode_data_url(str(payload.get("audio", "")), {"data:audio/wav;base64": ".wav", "data:audio/mpeg;base64": ".mp3"}, 24 << 20)
            task_id = str(uuid.uuid4())
            with tempfile.TemporaryDirectory(prefix="kwanni-musetalk-") as directory:
                root = Path(directory)
                video_path, audio_path = root / ("presenter" + video_ext), root / ("speech" + audio_ext)
                video_path.write_bytes(video)
                audio_path.write_bytes(audio)
                config_path = root / "inference.yaml"
                config_path.write_text(yaml.safe_dump({"kwanni": {"video_path": str(video_path), "audio_path": str(audio_path), "result_name": "presenter.mp4"}}))
                result_dir = root / "results"
                command = [
                    "/runtime/venv/bin/python", "-m", "scripts.inference",
                    "--inference_config", str(config_path), "--result_dir", str(result_dir),
                    "--unet_model_path", str(Path(MODEL_DIR) / "musetalkV15/unet.pth"),
                    "--unet_config", str(Path(MODEL_DIR) / "musetalkV15/musetalk.json"),
                    "--whisper_dir", str(Path(MODEL_DIR) / "whisper"),
                    "--version", "v15", "--use_float16", "--ffmpeg_path", "/usr/bin",
                ]
                subprocess.run(command, check=True, timeout=TIMEOUT, cwd=SOURCE_ROOT)
                output = result_dir / "v15/presenter.mp4"
                result = output.read_bytes()
                if not result:
                    raise RuntimeError("MuseTalk produced an empty preview")
            self.send_response(200)
            self.send_header("Content-Type", "video/mp4")
            self.send_header("Content-Length", str(len(result)))
            self.send_header("X-Kwanni-Task-ID", task_id)
            self.end_headers()
            self.wfile.write(result)
        except (ValueError, json.JSONDecodeError) as error:
            self.send_json(400, {"error": str(error)})
        except subprocess.TimeoutExpired:
            self.send_json(504, {"error": "MuseTalk generation exceeded its bounded timeout"})
        except Exception as error:
            self.send_json(500, {"error": type(error).__name__})
        finally:
            LOCK.release()


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8093), Handler).serve_forever()
