from __future__ import annotations

from concurrent.futures import FIRST_COMPLETED, Future, ThreadPoolExecutor, as_completed, wait
from contextlib import contextmanager
from dataclasses import dataclass, field
from ftplib import FTP, FTP_TLS, error_perm, error_proto, error_reply, error_temp
from pathlib import Path, PurePosixPath
from typing import Callable, Iterator
import os
import threading
import time

from ..windows_paths import io_path


ProgressCallback = Callable[[dict], None]
# One project is capped by its saved ``workers`` value (maximum 16). The
# process-wide ceiling allows two different sites to use their measured FTP
# capacity concurrently instead of silently squeezing both through eight
# shared sessions. Operators can lower this on constrained networks.
_FTP_TRANSFER_SLOT_LIMIT = max(8, min(64, int(os.getenv("WPCLEAN_FTP_TRANSFER_SLOTS", "32"))))
_FTP_TRANSFER_SLOTS = threading.BoundedSemaphore(_FTP_TRANSFER_SLOT_LIMIT)


@contextmanager
def ftp_transfer_slot():
    """Cap concurrent FTP data sessions across all active site workflows."""
    _FTP_TRANSFER_SLOTS.acquire()
    try:
        yield
    finally:
        _FTP_TRANSFER_SLOTS.release()


@dataclass(slots=True)
class FTPConfig:
    host: str
    username: str
    password: str
    port: int = 21
    tls: bool = True
    passive: bool = True
    timeout: float = 30.0
    workers: int = 6
    block_size: int = 1024 * 1024
    retries: int = 4


@dataclass(slots=True)
class RemoteFile:
    path: str
    size: int | None


@dataclass(slots=True)
class TransferFailure:
    path: str
    error: str


@dataclass(slots=True)
class TransferStats:
    files_total: int
    files_downloaded: int
    files_skipped: int
    bytes_downloaded: int
    elapsed_seconds: float
    files_failed: int = 0
    failures: list[TransferFailure] = field(default_factory=list)

    @property
    def bytes_per_second(self) -> float:
        if self.elapsed_seconds <= 0:
            return 0.0
        return self.bytes_downloaded / self.elapsed_seconds


class FTPTransport:
    """High-throughput FTP/FTPS downloader with bounded parallelism, retry, and resume support."""

    def __init__(self, config: FTPConfig):
        self.config = config
        self._local = threading.local()

    def _new_client(self) -> FTP:
        client: FTP
        if self.config.tls:
            tls = FTP_TLS(timeout=self.config.timeout)
            tls.connect(self.config.host, self.config.port)
            tls.login(self.config.username, self.config.password)
            tls.prot_p()
            client = tls
        else:
            ftp = FTP(timeout=self.config.timeout)
            ftp.connect(self.config.host, self.config.port)
            ftp.login(self.config.username, self.config.password)
            client = ftp
        client.set_pasv(self.config.passive)
        return client

    def _thread_client(self) -> FTP:
        client = getattr(self._local, "client", None)
        if client is None:
            client = self._new_client()
            self._local.client = client
            self._local.remote_root = None
        return client

    def _thread_client_at_root(self, remote_root: str) -> FTP:
        """Return this worker's client positioned at the requested tree root.

        Shared-hosting FTP servers are inconsistent about absolute ``RETR``
        paths even when ``MLSD`` accepts the same absolute directory.  Keep the
        control session inside the configured root and download by a relative
        name.  The cached root avoids one extra ``CWD`` for every file while a
        worker remains on the same backup tree.
        """

        normalized_root = str(PurePosixPath(remote_root))
        client = self._thread_client()
        if getattr(self._local, "remote_root", None) != normalized_root:
            client.cwd(normalized_root)
            self._local.remote_root = normalized_root
        return client

    def _reset_thread_client(self) -> None:
        client = getattr(self._local, "client", None)
        if client is not None:
            try:
                client.close()
            except Exception:
                pass
        self._local.client = None
        self._local.remote_root = None

    def test_connection(self) -> str:
        client = self._new_client()
        try:
            return client.pwd()
        finally:
            try:
                client.quit()
            except Exception:
                client.close()

    def directory_exists(self, remote_path: str) -> bool:
        """Verify one configured remote directory without recursively listing it."""
        client = self._new_client()
        current = None
        try:
            current = client.pwd()
            client.cwd(str(PurePosixPath(remote_path)))
            return True
        except error_perm:
            return False
        finally:
            if current is not None:
                try:
                    client.cwd(current)
                except Exception:
                    pass
            try:
                client.quit()
            except Exception:
                client.close()

    def _mlsd(self, client: FTP, remote_dir: str):
        try:
            yield from client.mlsd(remote_dir, facts=["type", "size"])
            return
        except (error_perm, AttributeError):
            pass

        current = client.pwd()
        try:
            client.cwd(remote_dir)
            for name in client.nlst():
                name = PurePosixPath(name).name
                if name in {".", ".."}:
                    continue
                item_path = str(PurePosixPath(remote_dir) / name)
                try:
                    client.cwd(item_path)
                    client.cwd(remote_dir)
                    yield name, {"type": "dir"}
                except error_perm:
                    size = None
                    try:
                        size = client.size(item_path)
                    except Exception:
                        pass
                    facts = {"type": "file"}
                    if size is not None:
                        facts["size"] = str(size)
                    yield name, facts
        finally:
            try:
                client.cwd(current)
            except Exception:
                pass

    def iter_files_recursive(
        self,
        remote_root: str,
        progress: ProgressCallback | None = None,
    ) -> Iterator[RemoteFile]:
        """Yield remote files while the directory inventory is still running.

        FTP has no portable recursive count command.  Keeping inventory as an
        iterator lets ``download_tree`` submit every discovered file to the
        transfer pool immediately instead of waiting for a full second pass.
        Consumers that need a stable complete inventory can keep using
        ``list_files_recursive`` below.
        """

        remote_root = str(PurePosixPath(remote_root))
        client = self._new_client()
        stack = [remote_root]
        dirs_scanned = 0
        files_found = 0
        bytes_found = 0
        try:
            while stack:
                current = stack.pop()
                dirs_scanned += 1
                if progress:
                    progress({
                        "phase": "discover",
                        "remote_root": remote_root,
                        "current_dir": current,
                        "dirs_scanned": dirs_scanned,
                        "files_found": files_found,
                        "bytes_total": bytes_found,
                    })
                for name, facts in self._mlsd(client, current):
                    if name in {".", ".."}:
                        continue
                    path = str(PurePosixPath(current) / name)
                    kind = (facts.get("type") or "").lower()
                    if kind in {"dir", "cdir", "pdir"}:
                        if kind == "dir":
                            stack.append(path)
                        continue
                    if kind == "file" or not kind:
                        raw_size = facts.get("size")
                        size = int(raw_size) if raw_size and raw_size.isdigit() else None
                        files_found += 1
                        bytes_found += size or 0
                        yield RemoteFile(path=path, size=size)
            if progress:
                progress({
                    "phase": "discovered",
                    "remote_root": remote_root,
                    "dirs_scanned": dirs_scanned,
                    "files_found": files_found,
                    "bytes_total": bytes_found,
                })
        finally:
            try:
                client.quit()
            except Exception:
                client.close()

    def list_files_recursive(self, remote_root: str, progress: ProgressCallback | None = None) -> list[RemoteFile]:
        """Return a complete stable inventory for callers that require one."""

        return list(self.iter_files_recursive(remote_root, progress=progress))

    def _download_one(
        self,
        remote_file: RemoteFile,
        remote_root: str,
        local_root: Path,
        resume: bool,
        progress: ProgressCallback | None = None,
        preserve_partial_on_failure: bool = False,
    ) -> tuple[str, int]:
        with ftp_transfer_slot():
            return self._download_one_impl(
                remote_file,
                remote_root,
                local_root,
                resume,
                progress,
                preserve_partial_on_failure,
            )

    def _download_one_impl(
        self,
        remote_file: RemoteFile,
        remote_root: str,
        local_root: Path,
        resume: bool,
        progress: ProgressCallback | None = None,
        preserve_partial_on_failure: bool = False,
    ) -> tuple[str, int]:
        rel = PurePosixPath(remote_file.path).relative_to(PurePosixPath(remote_root))
        remote_command_path = rel.as_posix()
        if remote_command_path in {"", "."}:
            raise ValueError(f"FTP download path is not a file below {remote_root}: {remote_file.path}")
        local_path = io_path(local_root.joinpath(*rel.parts))
        local_path.parent.mkdir(parents=True, exist_ok=True)

        initial_size = local_path.stat().st_size if resume and local_path.exists() else 0
        max_attempts = max(1, int(self.config.retries) + 1)
        last_error: BaseException | None = None

        for attempt in range(1, max_attempts + 1):
            offset = local_path.stat().st_size if resume and local_path.exists() else 0
            if remote_file.size is not None and offset == remote_file.size:
                status = "skipped" if attempt == 1 and initial_size == remote_file.size else "downloaded"
                return status, max(0, offset - initial_size)
            if remote_file.size is not None and offset > remote_file.size:
                local_path.unlink(missing_ok=True)
                offset = 0
                if attempt == 1:
                    initial_size = 0

            mode = "ab" if offset else "wb"
            try:
                client = self._thread_client_at_root(remote_root)
                with local_path.open(mode) as fh:
                    try:
                        client.retrbinary(
                            f"RETR {remote_command_path}",
                            fh.write,
                            blocksize=self.config.block_size,
                            rest=offset if offset else None,
                        )
                    except error_perm as relative_error:
                        # Relative RETR after CWD is the most portable mode. A
                        # small number of FTP daemons require the inverse, so
                        # fall back once to the original absolute path without
                        # turning a permanent 550 into a long retry loop.
                        absolute_command_path = str(PurePosixPath(remote_file.path))
                        if absolute_command_path == remote_command_path:
                            raise
                        fh.seek(offset)
                        fh.truncate()
                        if progress:
                            progress(
                                {
                                    "phase": "ftp_download_path_fallback",
                                    "remote_root": remote_root,
                                    "current_file": remote_file.path,
                                    "error": f"{type(relative_error).__name__}: {relative_error}",
                                }
                            )
                        client.retrbinary(
                            f"RETR {absolute_command_path}",
                            fh.write,
                            blocksize=self.config.block_size,
                            rest=offset if offset else None,
                        )

                final_size = local_path.stat().st_size
                if remote_file.size is not None and final_size != remote_file.size:
                    raise EOFError(
                        f"incomplete FTP transfer for {remote_file.path}: "
                        f"received {final_size} of {remote_file.size} bytes"
                    )
                return "downloaded", max(0, final_size - initial_size)
            except (ConnectionError, OSError, TimeoutError, EOFError, error_temp, error_reply, error_proto) as exc:
                last_error = exc
                self._reset_thread_client()
                if attempt >= max_attempts:
                    break

                resume_offset = local_path.stat().st_size if resume and local_path.exists() else 0
                if progress:
                    progress(
                        {
                            "phase": "retry",
                            "remote_root": remote_root,
                            "current_file": remote_file.path,
                            "attempt": attempt + 1,
                            "max_attempts": max_attempts,
                            "resume_offset": resume_offset,
                            "error": f"{type(exc).__name__}: {exc}",
                        }
                    )
                time.sleep(min(0.75 * (2 ** (attempt - 1)), 4.0))
            except Exception:
                self._reset_thread_client()
                raise

        # Tree downloads keep a partial only until the low-concurrency
        # reconciliation pass has had a chance to resume it. The caller removes
        # every still-failed partial before any manifest can be built.
        if not preserve_partial_on_failure:
            local_path.unlink(missing_ok=True)
        raise RuntimeError(
            f"FTP download failed after {max_attempts} attempts: {remote_file.path} "
            f"({type(last_error).__name__}: {last_error})"
        ) from last_error

    def download_tree(
        self,
        remote_root: str,
        local_root: Path,
        resume: bool = True,
        progress: ProgressCallback | None = None,
    ) -> TransferStats:
        local_root.mkdir(parents=True, exist_ok=True)
        started = time.monotonic()
        files: list[RemoteFile] = []
        downloaded = 0
        skipped = 0
        bytes_downloaded = 0
        completed = 0
        total_bytes = 0
        completed_bytes = 0
        failures: list[TransferFailure] = []
        discovery_complete = False

        def emit_progress(phase: str, *, current_file: str = "", error: str = "") -> None:
            if not progress:
                return
            elapsed = max(time.monotonic() - started, 0.001)
            payload = {
                "phase": phase,
                "remote_root": remote_root,
                "files_found": len(files),
                # While inventory is active this is the total known so far.
                # ``discovery_complete`` tells the UI not to present it as a
                # final denominator yet.
                "files_total": len(files),
                "files_completed": completed,
                "files_downloaded": downloaded,
                "files_skipped": skipped,
                "files_failed": len(failures),
                "bytes_downloaded": bytes_downloaded,
                "bytes_completed": completed_bytes,
                "bytes_total": total_bytes,
                "elapsed_seconds": elapsed,
                "bytes_per_second": bytes_downloaded / elapsed,
                "discovery_complete": discovery_complete,
            }
            if current_file:
                payload["current_file"] = current_file
            if error:
                payload["error"] = error
            progress(payload)

        def inventory_progress(event: dict) -> None:
            nonlocal discovery_complete
            phase = str(event.get("phase") or "")
            if phase == "discovered":
                discovery_complete = True
                if progress:
                    progress({
                        **event,
                        "files_total": len(files),
                        "files_completed": completed,
                        "files_downloaded": downloaded,
                        "files_skipped": skipped,
                        "files_failed": len(failures),
                        "bytes_downloaded": bytes_downloaded,
                        "bytes_completed": completed_bytes,
                        "bytes_total": total_bytes,
                        "discovery_complete": True,
                    })
                return
            if progress:
                progress({
                    **event,
                    "phase": "discover_transfer",
                    "files_found": len(files),
                    "files_total": len(files),
                    "files_completed": completed,
                    "files_downloaded": downloaded,
                    "files_skipped": skipped,
                    "files_failed": len(failures),
                    "bytes_downloaded": bytes_downloaded,
                    "bytes_completed": completed_bytes,
                    "bytes_total": total_bytes,
                    "discovery_complete": False,
                })

        pending: dict[Future[tuple[str, int]], RemoteFile] = {}

        def consume(done: set[Future[tuple[str, int]]]) -> None:
            nonlocal completed, downloaded, skipped, bytes_downloaded, completed_bytes
            for future in done:
                item = pending.pop(future)
                try:
                    status, transferred = future.result()
                except Exception as exc:
                    completed += 1
                    failure = TransferFailure(
                        path=item.path,
                        error=f"{type(exc).__name__}: {exc}",
                    )
                    failures.append(failure)
                    emit_progress("file_failed", current_file=item.path, error=failure.error)
                    emit_progress(
                        "transfer" if discovery_complete else "discover_transfer",
                        current_file=item.path,
                    )
                    continue

                completed += 1
                bytes_downloaded += transferred
                completed_bytes += item.size or 0
                if status == "skipped":
                    skipped += 1
                else:
                    downloaded += 1
                emit_progress(
                    "transfer" if discovery_complete else "discover_transfer",
                    current_file=item.path,
                )

        worker_count = max(1, self.config.workers)
        # Bound queued futures so a 100k-file site cannot allocate one Future
        # per file.  Backpressure still leaves enough work queued to keep every
        # transfer worker busy while the inventory connection continues.
        max_pending = max(worker_count, worker_count * 4)
        with ThreadPoolExecutor(max_workers=worker_count, thread_name_prefix="ftp") as pool:
            for item in self.iter_files_recursive(remote_root, progress=inventory_progress):
                files.append(item)
                total_bytes += item.size or 0
                future = pool.submit(
                    self._download_one,
                    item,
                    remote_root,
                    local_root,
                    resume,
                    progress,
                    True,
                )
                pending[future] = item
                emit_progress("discover_transfer", current_file=item.path)

                done = {candidate for candidate in pending if candidate.done()}
                if len(pending) >= max_pending and not done:
                    done, _ = wait(pending, return_when=FIRST_COMPLETED)
                if done:
                    consume(done)

            # ``iter_files_recursive`` emits ``discovered`` before exhaustion.
            # Keep this assignment as a defensive guarantee for a custom test
            # or adapter iterator that does not emit progress.
            discovery_complete = True
            while pending:
                done, _ = wait(pending, return_when=FIRST_COMPLETED)
                consume(done)

        # A busy shared host may reject one of many concurrent data sessions but
        # accept it immediately afterwards. Reconcile only failed paths with at
        # most two workers. This is separate from the per-file reconnect budget.
        failed_by_path = {failure.path: failure for failure in failures}
        remote_by_path = {item.path: item for item in files}
        successful_paths = set(remote_by_path) - set(failed_by_path)
        for round_index in range(1, 3):
            retry_items = [remote_by_path[path] for path in failed_by_path if path in remote_by_path]
            if not retry_items:
                break
            if progress:
                progress(
                    {
                        "phase": "reconcile",
                        "remote_root": remote_root,
                        "files_total": len(files),
                        "files_completed": downloaded + skipped,
                        "files_failed": len(retry_items),
                        "attempt": round_index,
                        "max_attempts": 2,
                        "location": "hosting",
                    }
                )
            next_failures: dict[str, TransferFailure] = {}
            with ThreadPoolExecutor(
                max_workers=min(2, len(retry_items)),
                thread_name_prefix="ftp-reconcile",
            ) as retry_pool:
                retry_futures = {
                    retry_pool.submit(
                        self._download_one,
                        item,
                        remote_root,
                        local_root,
                        True,
                        progress,
                        True,
                    ): item
                    for item in retry_items
                }
                for future in as_completed(retry_futures):
                    item = retry_futures[future]
                    try:
                        status, transferred = future.result()
                    except Exception as exc:
                        next_failures[item.path] = TransferFailure(
                            path=item.path,
                            error=f"{type(exc).__name__}: {exc}",
                        )
                        continue
                    bytes_downloaded += transferred
                    if status == "skipped":
                        skipped += 1
                    else:
                        downloaded += 1
                    successful_paths.add(item.path)
                    if progress:
                        elapsed_now = max(time.monotonic() - started, 0.001)
                        completed_bytes = sum(
                            remote_by_path[path].size or 0
                            for path in successful_paths
                        )
                        progress(
                            {
                                "phase": "reconcile",
                                "remote_root": remote_root,
                                "current_file": item.path,
                                "files_total": len(files),
                                "files_completed": len(successful_paths),
                                "files_failed": len(next_failures),
                                "bytes_completed": completed_bytes,
                                "bytes_total": total_bytes,
                                "bytes_per_second": bytes_downloaded / elapsed_now,
                                "attempt": round_index,
                                "max_attempts": 2,
                                "location": "hosting",
                            }
                        )
            failed_by_path = next_failures
            if failed_by_path and round_index < 2:
                time.sleep(1.0 * round_index)

        failures = list(failed_by_path.values())
        for failure in failures:
            item = remote_by_path.get(failure.path)
            if item is None:
                continue
            rel = PurePosixPath(item.path).relative_to(PurePosixPath(remote_root))
            io_path(local_root.joinpath(*rel.parts)).unlink(missing_ok=True)

        elapsed = time.monotonic() - started
        completed_bytes = sum(
            item.size or 0 for item in files if item.path not in failed_by_path
        )
        if progress:
            progress({
                "phase": "complete",
                "remote_root": remote_root,
                "files_total": len(files),
                "files_downloaded": downloaded,
                "files_skipped": skipped,
                "files_failed": len(failures),
                "bytes_downloaded": bytes_downloaded,
                "bytes_completed": completed_bytes,
                "bytes_total": total_bytes,
                "elapsed_seconds": elapsed,
                "bytes_per_second": bytes_downloaded / elapsed if elapsed > 0 else 0.0,
            })

        return TransferStats(
            files_total=len(files),
            files_downloaded=downloaded,
            files_skipped=skipped,
            bytes_downloaded=bytes_downloaded,
            elapsed_seconds=elapsed,
            files_failed=len(failures),
            failures=failures,
        )

    def download_file(self, remote_path: str, local_path: Path, resume: bool = True) -> TransferStats:
        client = self._new_client()
        try:
            size = None
            try:
                size = client.size(remote_path)
            except Exception:
                pass
        finally:
            try:
                client.quit()
            except Exception:
                client.close()

        remote = RemoteFile(path=remote_path, size=size)
        remote_root = str(PurePosixPath(remote_path).parent)
        local_root = local_path.parent
        expected_name = PurePosixPath(remote_path).name
        if local_path.name != expected_name:
            temp = local_root / expected_name
            status, transferred = self._download_one(remote, remote_root, local_root, resume)
            if temp != local_path:
                os.replace(temp, local_path)
        else:
            status, transferred = self._download_one(remote, remote_root, local_root, resume)
        return TransferStats(
            files_total=1,
            files_downloaded=0 if status == "skipped" else 1,
            files_skipped=1 if status == "skipped" else 0,
            bytes_downloaded=transferred,
            elapsed_seconds=0.0,
        )
