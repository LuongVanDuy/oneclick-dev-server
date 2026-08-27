from __future__ import annotations

from contextlib import contextmanager
import os
from pathlib import Path
import secrets
import shutil
from typing import Iterator


def io_path(path: Path) -> Path:
    r"""Return a Windows extended-length path for filesystem I/O.

    WordPress uploads can contain perfectly valid filenames whose full local path
    exceeds the legacy MAX_PATH limit.  The ``\\?\`` form keeps the original
    filename while allowing Python to create, hash, copy, and upload it.
    """
    if os.name != "nt":
        return path

    raw = os.path.abspath(os.fspath(path))
    if raw.startswith("\\\\?\\"):
        return Path(raw)
    if raw.startswith("\\\\"):
        return Path("\\\\?\\UNC\\" + raw[2:])
    return Path("\\\\?\\" + raw)


@contextmanager
def runtime_temporary_directory(*, prefix: str) -> Iterator[str]:
    """Create temporary files inside the project-owned runtime cache.

    GUI processes launched by managed desktop environments may not be allowed
    to write to the user's global Windows Temp directory. Keeping transient
    extraction data under the project also makes cleanup behavior predictable.
    """
    data_root = Path(os.environ.get("WPCLEAN_DATA_ROOT") or Path.cwd()).resolve()
    root = data_root / ".wpclean-cache" / "tmp"
    io_path(root).mkdir(parents=True, exist_ok=True)
    directory: Path | None = None
    for _attempt in range(10):
        candidate = root / f"{prefix}{secrets.token_hex(8)}"
        try:
            io_path(candidate).mkdir(mode=0o755)
            directory = candidate
            break
        except FileExistsError:
            continue
    if directory is None:
        raise RuntimeError("Không tạo được thư mục runtime tạm sau 10 lần thử.")
    try:
        yield str(directory)
    finally:
        # Cleanup must never hide the real workflow result. A stale ignored
        # runtime directory is safer than replacing the original exception.
        shutil.rmtree(io_path(directory), ignore_errors=True)


__all__ = ["io_path", "runtime_temporary_directory"]
