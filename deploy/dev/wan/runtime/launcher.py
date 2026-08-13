#!/usr/bin/env python3
"""Run pinned Wan generation while preserving the TI2V-only namespace shim."""

import os
import runpy
import sys
from pathlib import Path


wan_root = Path(os.environ.get("WAN_ROOT", "/runtime/Wan2.2")).resolve()
overlay = Path("/runtime/overlay").resolve()

# Executing generate.py directly prepends Wan2.2 to sys.path and bypasses the
# reviewed TI2V-only overlay, which eagerly imports unrelated speech packages.
sys.path = [entry for entry in sys.path if Path(entry or ".").resolve() != wan_root]
sys.path.insert(0, str(overlay))
sys.path.append(str(wan_root))

runpy.run_path(str(wan_root / "generate.py"), run_name="__main__")
