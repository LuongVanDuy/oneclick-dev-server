from __future__ import annotations

from contextlib import contextmanager
from typing import Callable, Iterator
import threading


ProgressCallback = Callable[[dict], None]

# Two site workflows may use the network together, but full-tree local scans and
# SHA passes are deliberately serialized. On one physical disk, running two of
# those jobs at once increases seeks and usually makes both finish later.
_DISK_HEAVY = threading.Semaphore(1)


@contextmanager
def disk_heavy(
    progress: ProgressCallback | None = None,
    *,
    operation: str,
) -> Iterator[None]:
    waited = 0
    while not _DISK_HEAVY.acquire(timeout=1.0):
        waited += 1
        if progress:
            progress(
                {
                    "phase": "resource_wait",
                    "location": "local",
                    "resource": "disk",
                    "operation": operation,
                    "wait_seconds": waited,
                }
            )
    try:
        if progress:
            progress(
                {
                    "phase": "resource_acquired",
                    "location": "local",
                    "resource": "disk",
                    "operation": operation,
                }
            )
        yield
    finally:
        _DISK_HEAVY.release()


__all__ = ["disk_heavy"]
