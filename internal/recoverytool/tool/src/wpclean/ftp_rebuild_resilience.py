from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor, as_completed
from ftplib import error_temp
from pathlib import Path, PurePosixPath
from queue import SimpleQueue
from typing import Callable, TypeVar
import io
import threading
import time

from . import rebuild_execute as rebuild_engine
from .permission_recovery import _missing_error, _permission_error
from .transport import FTPTransport
from .transport.ftp import ftp_transfer_slot
from .windows_paths import io_path


ProgressCallback = Callable[[dict], None]
_T = TypeVar("_T")


def _is_transient_ftp_error(exc: BaseException) -> bool:
    """Return True for connection failures that are safe to reconnect/retry.

    Permission/login errors are deliberately excluded. A retry must never turn
    a real authorization problem into an endless reconnect loop.
    """
    if isinstance(exc, error_temp):
        return True
    if isinstance(exc, (ConnectionError, TimeoutError, EOFError)):
        return True
    if isinstance(exc, OSError):
        winerror = getattr(exc, "winerror", None)
        errno = getattr(exc, "errno", None)
        if winerror in {10053, 10054, 10060, 10061}:
            return True
        if errno in {32, 54, 60, 104, 110, 111}:
            return True
    text = str(exc).lower()
    markers = (
        "connection reset",
        "forcibly closed by the remote host",
        "broken pipe",
        "connection aborted",
        "connection timed out",
        "timed out",
        "winerror 10053",
        "winerror 10054",
        "winerror 10060",
        "winerror 10061",
    )
    return any(marker in text for marker in markers)


def _retry_count(transport: FTPTransport) -> int:
    config = getattr(transport, "config", None)
    return max(1, int(getattr(config, "retries", 4)) + 1)


def _backoff(attempt: int) -> float:
    return min(0.5 * (2 ** max(0, attempt - 1)), 3.0)


class _ReconnectSession:
    def __init__(
        self,
        transport: FTPTransport,
        *,
        progress: ProgressCallback | None = None,
    ) -> None:
        self.transport = transport
        self.progress = progress
        self.client = transport._new_client()

    def close(self) -> None:
        client = self.client
        if client is None:
            return
        try:
            client.quit()
        except Exception:
            try:
                client.close()
            except Exception:
                pass

    def reconnect(self) -> None:
        old = self.client
        try:
            old.close()
        except Exception:
            pass
        self.client = self.transport._new_client()

    def call(
        self,
        operation: str,
        func: Callable[[object], _T],
        *,
        path: str = "",
        missing_ok: bool = False,
        attempts: int | None = None,
    ) -> _T | None:
        max_attempts = max(1, int(attempts)) if attempts is not None else _retry_count(self.transport)
        last_error: BaseException | None = None
        for attempt in range(1, max_attempts + 1):
            try:
                return func(self.client)
            except Exception as exc:
                if missing_ok and _missing_error(exc):
                    return None
                if not _is_transient_ftp_error(exc):
                    raise
                last_error = exc
                if attempt >= max_attempts:
                    break
                if self.progress:
                    self.progress(
                        {
                            "phase": "ftp_reconnect",
                            "operation": operation,
                            "current": path,
                            "attempt": attempt + 1,
                            "max_attempts": max_attempts,
                            "error": f"{type(exc).__name__}: {exc}",
                        }
                    )
                time.sleep(_backoff(attempt))
                self.reconnect()
        raise RuntimeError(
            f"FTP {operation} failed after {max_attempts} attempts"
            + (f": {path}" if path else "")
            + f" ({type(last_error).__name__}: {last_error})"
        ) from last_error


def _chmod(session: _ReconnectSession, path: str, mode: str) -> tuple[bool, str]:
    try:
        response = session.call(
            "chmod",
            lambda client: client.sendcmd(f"SITE CHMOD {mode} {path}"),
            path=path,
        )
        return True, str(response)
    except Exception as exc:
        return False, f"{type(exc).__name__}: {exc}"


def _normalize_remote_path(path: str) -> str:
    """Normalize an FTP path lexically without allowing it to climb above root."""
    raw = str(path or "").strip().replace("\\", "/")
    if any(ord(char) < 32 or ord(char) == 127 for char in raw):
        raise RuntimeError(f"Đường dẫn FTP chứa ký tự điều khiển không an toàn: {path!r}")
    absolute = raw.startswith("/")
    parts: list[str] = []
    for part in raw.split("/"):
        if part in {"", "."}:
            continue
        if part == "..":
            if not parts:
                raise RuntimeError(f"Đường dẫn FTP không an toàn: {path}")
            parts.pop()
            continue
        parts.append(part)
    normalized = "/".join(parts)
    return f"/{normalized}" if absolute else normalized


def _assert_within_remote_root(
    remote_root: str,
    path: str,
    *,
    allow_root: bool = False,
    allow_account_root: bool = False,
) -> str:
    root = _normalize_remote_path(remote_root)
    candidate = _normalize_remote_path(path)
    if root == "" or (root == "/" and not allow_account_root):
        raise RuntimeError("Từ chối wipe FTP root rỗng hoặc '/'. Hãy cấu hình đúng thư mục WordPress.")

    root_path = PurePosixPath(root)
    candidate_path = PurePosixPath(candidate)
    try:
        relative = candidate_path.relative_to(root_path)
    except ValueError as exc:
        raise RuntimeError(
            f"Từ chối thao tác ngoài WordPress root {root}: {candidate}"
        ) from exc
    if not allow_root and str(relative) in {"", "."}:
        raise RuntimeError(f"Từ chối xóa trực tiếp WordPress root: {root}")
    return candidate


def _safe_child_path(
    remote_root: str,
    parent: str,
    name: str,
    *,
    allow_account_root: bool = False,
) -> str:
    raw_name = str(name or "")
    if (
        raw_name in {"", ".", ".."}
        or "/" in raw_name
        or "\\" in raw_name
        or any(ord(char) < 32 or ord(char) == 127 for char in raw_name)
        or PurePosixPath(raw_name).name != raw_name
    ):
        raise RuntimeError(f"FTP trả về tên file không an toàn trong {parent}: {raw_name!r}")
    child = str(PurePosixPath(parent) / raw_name)
    return _assert_within_remote_root(
        remote_root,
        child,
        allow_account_root=allow_account_root,
    )


def _relative_remote_call(client, path: str, method: str):
    """Run DELETE/RMD from the parent directory for strict FTP servers."""
    remote = PurePosixPath(path)
    parent = str(remote.parent)
    name = remote.name
    previous = None
    try:
        previous = client.pwd()
    except Exception:
        pass
    client.cwd(parent)
    try:
        return getattr(client, method)(name)
    finally:
        if previous is not None:
            try:
                client.cwd(previous)
            except Exception:
                pass


def _entry_exists(session: _ReconnectSession, path: str) -> bool:
    remote = PurePosixPath(path)
    parent = str(remote.parent)
    name = remote.name
    entries = session.call(
        "verify-delete",
        lambda client: list(session.transport._mlsd(client, parent)),
        path=path,
        missing_ok=True,
    )
    for entry_name, _facts in entries or []:
        raw_name = str(entry_name)
        if (
            raw_name in {"", ".", ".."}
            or "/" in raw_name
            or "\\" in raw_name
            or any(ord(char) < 32 or ord(char) == 127 for char in raw_name)
        ):
            raise RuntimeError(f"Không thể xác minh lệnh xóa vì FTP trả về tên không an toàn: {raw_name!r}")
        if raw_name == name:
            return True
    return False


def _emit_path_fallback(
    session: _ReconnectSession,
    *,
    operation: str,
    path: str,
    error: BaseException,
) -> None:
    if session.progress:
        session.progress(
            {
                "phase": "ftp_path_fallback",
                "operation": operation,
                "strategy": "absolute_path",
                "current": path,
                "error": f"{type(error).__name__}: {error}",
            }
        )


def _remove_entry_compat(
    session: _ReconnectSession,
    path: str,
    *,
    method: str,
    operation: str,
) -> None:
    """Remove one entry relative to its parent, with absolute-path fallback.

    DirectAdmin/shared-hosting FTP daemons may accept MLSD with an absolute path
    but reset the control socket on ``DELE /absolute/path``. Prefer the broadly
    compatible ``CWD parent`` + ``DELE/RMD basename`` form proven by a live FTP
    probe. An absolute path remains a fallback for servers that reject CWD.
    """
    relative_error: Exception | None = None
    try:
        session.call(
            f"{operation}-relative",
            lambda client: _relative_remote_call(client, path, method),
            path=path,
            missing_ok=True,
            attempts=1,
        )
        return
    except Exception as exc:
        relative_error = exc
        if _missing_error(exc):
            return
        if not (_is_transient_ftp_error(exc) or _permission_error(exc)):
            raise

    assert relative_error is not None

    if _is_transient_ftp_error(relative_error):
        # DELE/RMD may have completed before the reply socket was reset. Open a
        # fresh control connection and verify before issuing another mutation.
        try:
            if session.progress:
                session.progress(
                    {
                        "phase": "ftp_reconnect",
                        "operation": operation,
                        "current": path,
                        "attempt": 2,
                        "max_attempts": _retry_count(session.transport),
                        "error": f"{type(relative_error).__name__}: {relative_error}",
                    }
                )
            session.reconnect()
            if not _entry_exists(session, path):
                return
        except Exception as verify_exc:
            if not _is_transient_ftp_error(verify_exc):
                raise
            # The regular bounded retry below also renews a broken connection.

        session.call(
            f"{operation}-relative",
            lambda client: _relative_remote_call(client, path, method),
            path=path,
            missing_ok=True,
        )
        return

    _emit_path_fallback(session, operation=operation, path=path, error=relative_error)
    session.call(
        operation,
        lambda client: getattr(client, method)(path),
        path=path,
        missing_ok=True,
    )


def _delete_file(
    session: _ReconnectSession,
    path: str,
    *,
    remote_root: str,
    progress: ProgressCallback | None = None,
    allow_account_root: bool = False,
) -> None:
    path = _assert_within_remote_root(
        remote_root,
        path,
        allow_account_root=allow_account_root,
    )
    try:
        _remove_entry_compat(session, path, method="delete", operation="delete")
        return
    except Exception as exc:
        if not _permission_error(exc):
            raise
        first_error = exc

    parent = str(PurePosixPath(path).parent)
    attempts: list[str] = []
    last_exc: Exception = first_error
    for file_mode, parent_mode in (("666", "755"), ("777", "777")):
        file_ok, file_detail = _chmod(session, path, file_mode)
        parent_ok, parent_detail = _chmod(session, parent, parent_mode)
        attempts.append(
            f"file CHMOD {file_mode}={file_ok} ({file_detail}); "
            f"parent CHMOD {parent_mode}={parent_ok} ({parent_detail})"
        )
        if progress:
            progress(
                {
                    "phase": "permission_recovery",
                    "kind": "file",
                    "path": path,
                    "file_mode": file_mode,
                    "parent_mode": parent_mode,
                }
            )
        try:
            _remove_entry_compat(session, path, method="delete", operation="delete")
            return
        except Exception as retry_exc:
            if not _permission_error(retry_exc):
                raise
            last_exc = retry_exc

    raise RuntimeError(
        "FTP không thể xóa file dù đã thử đổi quyền tạm thời đến 777: "
        f"{path}. Có thể file/thư mục thuộc owner khác, bị ACL/immutable hoặc hosting chặn SITE CHMOD. "
        "Cần xử lý ownership/permission bằng File Manager, SSH hoặc hỗ trợ hosting. "
        f"Chi tiết: {' | '.join(attempts)} | lỗi cuối: {last_exc}"
    )


def _remove_dir(
    session: _ReconnectSession,
    path: str,
    *,
    remote_root: str,
    progress: ProgressCallback | None = None,
    allow_account_root: bool = False,
) -> None:
    path = _assert_within_remote_root(
        remote_root,
        path,
        allow_account_root=allow_account_root,
    )
    try:
        _remove_entry_compat(session, path, method="rmd", operation="rmd")
        return
    except Exception as exc:
        if not _permission_error(exc):
            raise
        first_error = exc

    parent = str(PurePosixPath(path).parent)
    attempts: list[str] = []
    last_exc: Exception = first_error
    for dir_mode, parent_mode in (("755", "755"), ("777", "777")):
        dir_ok, dir_detail = _chmod(session, path, dir_mode)
        parent_ok, parent_detail = _chmod(session, parent, parent_mode)
        attempts.append(
            f"dir CHMOD {dir_mode}={dir_ok} ({dir_detail}); "
            f"parent CHMOD {parent_mode}={parent_ok} ({parent_detail})"
        )
        if progress:
            progress(
                {
                    "phase": "permission_recovery",
                    "kind": "directory",
                    "path": path,
                    "dir_mode": dir_mode,
                    "parent_mode": parent_mode,
                }
            )
        try:
            _remove_entry_compat(session, path, method="rmd", operation="rmd")
            return
        except Exception as retry_exc:
            if not _permission_error(retry_exc):
                raise
            last_exc = retry_exc

    raise RuntimeError(
        "FTP không thể xóa thư mục dù đã thử đổi quyền tạm thời đến 777: "
        f"{path}. Có thể thư mục thuộc owner khác, bị ACL/immutable hoặc hosting chặn SITE CHMOD. "
        "Cần xử lý ownership/permission bằng File Manager, SSH hoặc hỗ trợ hosting. "
        f"Chi tiết: {' | '.join(attempts)} | lỗi cuối: {last_exc}"
    )


def wipe_remote_root_with_reconnect(
    transport: FTPTransport,
    remote_root: str,
    *,
    progress: ProgressCallback | None = None,
    allow_account_root: bool = False,
) -> tuple[int, int, list[str]]:
    """Permission-aware, reconnect-safe destructive wipe.

    ``allow_account_root`` is intentionally opt-in. It is used only after the
    rebuild safety reports and typed host confirmation have been validated for
    FTP accounts jailed directly into the site's document root.
    """
    remote_root = _assert_within_remote_root(
        remote_root,
        remote_root,
        allow_root=True,
        allow_account_root=allow_account_root,
    )
    session = _ReconnectSession(transport, progress=progress)
    deleted_files = 0
    deleted_dirs = 0
    preserved: list[str] = []
    discovered_items = 0

    def emit(current: str, *, discovery_complete: bool = False) -> None:
        if progress:
            progress(
                {
                    "phase": "wipe",
                    "deleted_files": deleted_files,
                    "deleted_dirs": deleted_dirs,
                    "items_completed": deleted_files + deleted_dirs,
                    "items_total": discovered_items,
                    "items_discovered": discovered_items,
                    "discovery_complete": discovery_complete,
                    "unit": "mục",
                    "current": current,
                }
            )

    def list_dir(path: str) -> list[tuple[str, dict]]:
        try:
            result = session.call(
                "list",
                lambda client: list(transport._mlsd(client, path)),
                path=path,
                missing_ok=True,
            )
            return list(result or [])
        except Exception as exc:
            if not _permission_error(exc):
                raise
            first_error = exc

        last_exc: Exception = first_error
        for mode in ("755", "777"):
            _chmod(session, path, mode)
            try:
                result = session.call(
                    "list",
                    lambda client: list(transport._mlsd(client, path)),
                    path=path,
                    missing_ok=True,
                )
                return list(result or [])
            except Exception as retry_exc:
                if not _permission_error(retry_exc):
                    raise
                last_exc = retry_exc
        raise RuntimeError(
            f"FTP không thể đọc thư mục để xóa: {path}. "
            "Đã thử CHMOD 755/777 nhưng tài khoản FTP vẫn không đủ quyền; "
            f"cần File Manager/SSH/hosting xử lý ownership hoặc ACL. Lỗi cuối: {last_exc}"
        )

    def remove_tree(path: str, *, root: bool = False) -> None:
        nonlocal deleted_files, deleted_dirs, discovered_items
        for name, facts in list_dir(path):
            if name in {".", ".."}:
                continue
            if root and name == ".well-known":
                preserved.append(str(PurePosixPath(path) / name))
                continue
            child = _safe_child_path(
                remote_root,
                path,
                name,
                allow_account_root=allow_account_root,
            )
            kind = (facts.get("type") or "").lower()
            if kind in {"dir", "cdir", "pdir"}:
                if kind == "dir":
                    discovered_items += 1
                    remove_tree(child)
                    _remove_dir(
                        session,
                        child,
                        remote_root=remote_root,
                        progress=progress,
                        allow_account_root=allow_account_root,
                    )
                    deleted_dirs += 1
                    emit(child)
                continue
            discovered_items += 1
            _delete_file(
                session,
                child,
                remote_root=remote_root,
                progress=progress,
                allow_account_root=allow_account_root,
            )
            deleted_files += 1
            emit(child)

    try:
        if progress:
            progress(
                {
                    "phase": "wipe",
                    "deleted_files": 0,
                    "deleted_dirs": 0,
                    "items_completed": 0,
                    "items_total": 0,
                    "items_discovered": 0,
                    "discovery_complete": False,
                    "unit": "mục",
                    "current": remote_root,
                }
            )
        remove_tree(remote_root, root=True)
        remaining = [
            name
            for name, _facts in list_dir(remote_root)
            if name not in {".", "..", ".well-known"}
        ]
        if remaining:
            raise RuntimeError(
                "Sau khi wipe vẫn còn file/thư mục trong WordPress root: "
                + ", ".join(remaining[:20])
            )
        emit(remote_root, discovery_complete=True)
        return deleted_files, deleted_dirs, preserved
    finally:
        session.close()


def prune_remote_directory_allowlist(
    transport: FTPTransport,
    remote_dir: str,
    allowed_names: set[str] | frozenset[str],
    *,
    progress: ProgressCallback | None = None,
) -> list[str]:
    """Delete every top-level component not explicitly allowed."""
    remote_dir = _assert_within_remote_root(remote_dir, remote_dir, allow_root=True)
    allowed = {name.casefold() for name in allowed_names}
    session = _ReconnectSession(transport, progress=progress)
    removed: list[str] = []

    def list_dir(path: str) -> list[tuple[str, dict]]:
        result = session.call(
            "list",
            lambda client: list(transport._mlsd(client, path)),
            path=path,
            missing_ok=True,
        )
        return list(result or [])

    def remove_tree(path: str) -> None:
        for name, facts in list_dir(path):
            if name in {".", ".."}:
                continue
            child = _safe_child_path(remote_dir, path, name)
            kind = (facts.get("type") or "").lower()
            if kind in {"dir", "cdir", "pdir"}:
                if kind == "dir":
                    remove_tree(child)
                    _remove_dir(session, child, remote_root=remote_dir, progress=progress)
                continue
            _delete_file(session, child, remote_root=remote_dir, progress=progress)

    try:
        entries = list_dir(remote_dir)
        candidates = [
            (name, facts)
            for name, facts in entries
            if name not in {".", ".."} and name.casefold() not in allowed
        ]
        for index, (name, facts) in enumerate(candidates, 1):
            path = _safe_child_path(remote_dir, remote_dir, name)
            kind = (facts.get("type") or "").lower()
            if kind in {"dir", "cdir", "pdir"}:
                if kind == "dir":
                    remove_tree(path)
                    _remove_dir(session, path, remote_root=remote_dir, progress=progress)
            else:
                _delete_file(session, path, remote_root=remote_dir, progress=progress)
            removed.append(path)
            if progress:
                progress(
                    {
                        "phase": "mu_plugin_prune",
                        "items_completed": index,
                        "items_total": len(candidates),
                        "unit": "mục",
                        "current": path,
                    }
                )

        remaining = [
            name
            for name, _facts in list_dir(remote_dir)
            if name not in {".", ".."} and name.casefold() not in allowed
        ]
        if remaining:
            raise RuntimeError(
                "MU-plugin cleanup left non-allowlisted entries: " + ", ".join(remaining[:20])
            )
        return removed
    finally:
        session.close()


def _upload_tree_with_reconnect(
    transport: FTPTransport,
    local_root: Path,
    remote_root: str,
    *,
    progress_phase: str,
    progress: ProgressCallback | None = None,
    excluded_paths: set[str] | None = None,
) -> int:
    filesystem_root = io_path(local_root)
    excluded = {path.replace("\\", "/").casefold() for path in (excluded_paths or set())}
    workers = max(1, int(transport.config.workers))
    work_queue: SimpleQueue[tuple[Path, int] | None] = SimpleQueue()
    discovered = 0
    discovered_bytes = 0
    discovery_complete = False
    completed = 0
    completed_bytes = 0
    started = time.monotonic()
    lock = threading.Lock()

    def worker() -> int:
        nonlocal completed, completed_bytes
        uploaded = 0
        first = work_queue.get()
        if first is None:
            return 0
        with ftp_transfer_slot():
            session = _ReconnectSession(transport, progress=progress)
            try:
                item: tuple[Path, int] | None = first
                while item is not None:
                    path, file_size = item
                    rel = path.relative_to(filesystem_root)
                    remote_path = str(PurePosixPath(remote_root).joinpath(*rel.parts))
                    remote_parent = str(PurePosixPath(remote_path).parent)

                    def do_upload(client):
                        rebuild_engine._ensure_remote_dir(client, remote_parent)
                        with io_path(path).open("rb") as fh:
                            return client.storbinary(
                                f"STOR {remote_path}",
                                fh,
                                blocksize=transport.config.block_size,
                            )

                    session.call("upload", do_upload, path=remote_path)
                    uploaded += 1
                    with lock:
                        completed += 1
                        completed_bytes += file_size
                        current_completed = completed
                        current_bytes = completed_bytes
                        current_discovered = discovered
                        current_total_bytes = discovered_bytes
                        current_discovery_complete = discovery_complete
                    if progress:
                        elapsed = max(time.monotonic() - started, 0.001)
                        rate = current_bytes / elapsed
                        progress(
                            {
                                "phase": progress_phase,
                                "location": "hosting",
                                "files_completed": current_completed,
                                "files_total": current_discovered,
                                "files_discovered": current_discovered,
                                "discovery_complete": current_discovery_complete,
                                "workers": workers,
                                "bytes_completed": current_bytes,
                                "bytes_total": current_total_bytes,
                                "bytes_per_second": rate,
                                "eta_seconds": (
                                    int((current_total_bytes - current_bytes) / rate)
                                    if current_discovery_complete and rate
                                    else None
                                ),
                                "current": remote_path,
                            }
                        )
                    item = work_queue.get()
                return uploaded
            finally:
                session.close()

    producer_error: BaseException | None = None
    futures = []
    with ThreadPoolExecutor(max_workers=workers, thread_name_prefix="wpclean-upload") as pool:
        futures = [pool.submit(worker) for _ in range(workers)]
        try:
            for path in filesystem_root.rglob("*"):
                if not path.is_file():
                    continue
                rel = path.relative_to(filesystem_root).as_posix().casefold()
                if rel in excluded:
                    continue
                file_size = path.stat().st_size
                with lock:
                    discovered += 1
                    discovered_bytes += file_size
                work_queue.put((path, file_size))
        except BaseException as exc:
            producer_error = exc
        finally:
            with lock:
                discovery_complete = True
            for _ in range(workers):
                work_queue.put(None)

        total = 0
        for future in as_completed(futures):
            total += future.result()
    if producer_error is not None:
        raise producer_error
    if progress:
        elapsed = max(time.monotonic() - started, 0.001)
        rate = completed_bytes / elapsed
        progress(
            {
                "phase": progress_phase,
                "location": "hosting",
                "files_completed": completed,
                "files_total": discovered,
                "files_discovered": discovered,
                "discovery_complete": True,
                "workers": workers,
                "bytes_completed": completed_bytes,
                "bytes_total": discovered_bytes,
                "bytes_per_second": rate,
                "eta_seconds": 0,
                "current": remote_root,
            }
        )
    return total


def _upload_text_with_reconnect(transport: FTPTransport, remote_path: str, content: str) -> None:
    payload = content.encode("utf-8")
    session = _ReconnectSession(transport)
    remote_parent = str(PurePosixPath(remote_path).parent)
    try:
        def do_upload(client):
            rebuild_engine._ensure_remote_dir(client, remote_parent)
            return client.storbinary(
                f"STOR {remote_path}",
                io.BytesIO(payload),
                blocksize=transport.config.block_size,
            )

        session.call("upload", do_upload, path=remote_path)
    finally:
        session.close()


def _upload_file_with_reconnect(transport: FTPTransport, remote_path: str, local_path: Path) -> None:
    session = _ReconnectSession(transport)
    remote_parent = str(PurePosixPath(remote_path).parent)
    try:
        def do_upload(client):
            rebuild_engine._ensure_remote_dir(client, remote_parent)
            with local_path.open("rb") as fh:
                return client.storbinary(
                    f"STOR {remote_path}",
                    fh,
                    blocksize=transport.config.block_size,
                )

        session.call("upload", do_upload, path=remote_path)
    finally:
        session.close()


def _delete_remote_file_with_reconnect(transport: FTPTransport, remote_path: str) -> bool:
    session = _ReconnectSession(transport)
    try:
        try:
            session.call("delete", lambda client: client.delete(remote_path), path=remote_path, missing_ok=True)
            return True
        except Exception:
            return False
    finally:
        session.close()


# Apply only to rebuild FTP primitives. Backup/downloader already has its own
# mature resumable retry implementation and is intentionally left untouched.
rebuild_engine._wipe_remote_root = wipe_remote_root_with_reconnect
rebuild_engine._upload_tree = _upload_tree_with_reconnect
rebuild_engine._upload_text = _upload_text_with_reconnect
rebuild_engine._upload_file = _upload_file_with_reconnect
rebuild_engine._delete_remote_file = _delete_remote_file_with_reconnect


__all__ = [
    "prune_remote_directory_allowlist",
    "wipe_remote_root_with_reconnect",
    "_is_transient_ftp_error",
]
