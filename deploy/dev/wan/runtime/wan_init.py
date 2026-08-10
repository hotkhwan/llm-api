"""Mission Zero TI2V-only import surface for the pinned Wan2.2 source.

Upstream imports speech/animate runtimes eagerly, which pulls optional packages
that are irrelevant to TI2V and often unavailable on ARM64. This bounded shim
keeps only the exact runtime used by KWANNI.
"""

from pkgutil import extend_path

__path__ = extend_path(__path__, __name__)

from . import configs, distributed, modules
from .textimage2video import WanTI2V

__all__ = ["WanTI2V", "configs", "distributed", "modules"]
