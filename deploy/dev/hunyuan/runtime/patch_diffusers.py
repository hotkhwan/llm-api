#!/usr/bin/env python3
"""Patch the pinned diffusers logger ordering for NVIDIA's newer torchao."""

from pathlib import Path

target = Path("/runtime/python/diffusers/quantizers/torchao/torchao_quantizer.py")
source = target.read_text()
logger_line = "logger = logging.get_logger(__name__)\n"
anchor = "\n\nif TYPE_CHECKING:\n"
late = "\n\nlogger = logging.get_logger(__name__)\n\n\ndef _quantization_type"
if source.count(logger_line) != 1 or anchor not in source or late not in source:
    raise SystemExit("unexpected diffusers 0.35.0 torchao_quantizer source; refusing patch")
source = source.replace(anchor, "\n\n" + logger_line + anchor, 1)
source = source.replace(late, "\n\ndef _quantization_type", 1)
target.write_text(source)
