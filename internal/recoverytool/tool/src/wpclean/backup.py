from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Callable
import hashlib
import json
import os
import threading
import time

from .windows_paths import io_path


ManifestProgressCallback = Callable[[dict], None]
VERIFICATION_RECEIPT = ".manifest-verified.json"


@dataclass(slots=True)
class ManifestEntry:
    path: str
    size: int
    sha256: str


@dataclass(slots=True)
class BackupManifest:
    created_at: str
    root: str
    entries: list[ManifestEntry]


@dataclass(slots=True)
class BackupCompleteness:
    complete: bool
    problems: list[str]


def _hash_workers(requested: int | None = None) -> int:
    """Keep a modern SSD busy while leaving enough room for the desktop UI."""
    if requested is not None:
        return max(1, min(16, int(requested)))
    configured = os.environ.get("WPCLEAN_HASH_WORKERS", "").strip()
    if configured.isdigit():
        return max(1, min(16, int(configured)))
    return max(2, min(8, os.cpu_count() or 4))


def sha256_file(path: Path, chunk_size: int = 2 * 1024 * 1024) -> str:
    digest = hashlib.sha256()
    with io_path(path).open("rb") as fh:
        while chunk := fh.read(chunk_size):
            digest.update(chunk)
    return digest.hexdigest()


def _save_verification_receipt(root: Path, manifest_path: Path, entries: int) -> None:
    receipt = {
        "verified_at": datetime.now(timezone.utc).isoformat(),
        "manifest_sha256": sha256_file(manifest_path),
        "entries": entries,
        "root": str(root.resolve()),
    }
    io_path(root / VERIFICATION_RECEIPT).write_text(
        json.dumps(receipt, indent=2, ensure_ascii=False),
        encoding="utf-8",
    )


def _parallel_hash(
    files: list[tuple[str, Path, int]],
    *,
    phase: str,
    progress: ManifestProgressCallback | None,
    workers: int | None,
    pass_index: int,
    pass_total: int,
) -> dict[str, str]:
    started = time.monotonic()
    completed = 0
    bytes_completed = 0
    total_bytes = sum(size for _relative, _path, size in files)
    result: dict[str, str] = {}
    last_emit = 0.0
    worker_count = _hash_workers(workers)
    state_lock = threading.Lock()

    def emit(relative: str, *, force: bool = False) -> None:
        nonlocal last_emit
        if progress is None:
            return
        now = time.monotonic()
        if not force and now - last_emit < 0.75:
            return
        last_emit = now
        elapsed = max(now - started, 0.001)
        rate = bytes_completed / elapsed
        remaining = max(0, total_bytes - bytes_completed)
        progress(
            {
                "phase": phase,
                "location": "local",
                "current_file": relative,
                "files_completed": completed,
                "files_total": len(files),
                "bytes_completed": bytes_completed,
                "bytes_total": total_bytes,
                "bytes_per_second": rate,
                "eta_seconds": int(remaining / rate) if rate > 0 else None,
                "pass_index": pass_index,
                "pass_total": pass_total,
                "workers": worker_count,
                "unit": "file",
            }
        )

    def hash_one(item: tuple[str, Path, int]) -> tuple[str, str, int]:
        nonlocal bytes_completed
        relative, path, expected_size = item
        digest = hashlib.sha256()
        with io_path(path).open("rb") as fh:
            while chunk := fh.read(2 * 1024 * 1024):
                digest.update(chunk)
                with state_lock:
                    bytes_completed += len(chunk)
                    emit(relative)
        return relative, digest.hexdigest(), expected_size

    emit("", force=True)
    with ThreadPoolExecutor(max_workers=worker_count, thread_name_prefix="wpclean-sha") as pool:
        futures = {pool.submit(hash_one, item): item[0] for item in files}
        for future in as_completed(futures):
            relative, digest, size = future.result()
            result[relative] = digest
            with state_lock:
                completed += 1
                emit(relative, force=completed == len(files))
    return result


def _manifest_files(
    root: Path,
    progress: ManifestProgressCallback | None = None,
    *,
    pass_index: int = 1,
    pass_total: int = 1,
) -> list[tuple[str, Path, int]]:
    filesystem_root = io_path(root)
    original_backup_root = (root / "database" / "original.sql").is_file()
    files: list[tuple[str, Path, int]] = []
    last_emit = time.monotonic()
    for path in filesystem_root.rglob("*"):
        if not path.is_file() or path in {
            filesystem_root / "manifest.json",
            filesystem_root / VERIFICATION_RECEIPT,
        }:
            continue
        relative = path.relative_to(filesystem_root).as_posix()
        if original_backup_root and relative.casefold().startswith("clean/"):
            continue
        files.append((relative, path, path.stat().st_size))
        now = time.monotonic()
        if progress and now - last_emit >= 0.8:
            progress(
                {
                    "phase": "manifest_inventory",
                    "location": "local",
                    "current_file": relative,
                    "files_completed": len(files),
                    "pass_index": pass_index,
                    "pass_total": pass_total,
                    "unit": "file",
                }
            )
            last_emit = now
    files.sort(key=lambda item: item[0])
    return files


def build_manifest(
    root: Path,
    *,
    progress: ManifestProgressCallback | None = None,
    workers: int | None = None,
    pass_index: int = 1,
    pass_total: int = 1,
) -> BackupManifest:
    files = _manifest_files(root, progress, pass_index=pass_index, pass_total=pass_total)
    hashes = _parallel_hash(
        files,
        phase="manifest_build",
        progress=progress,
        workers=workers,
        pass_index=pass_index,
        pass_total=pass_total,
    )
    entries = [
        ManifestEntry(path=relative, size=size, sha256=hashes[relative])
        for relative, _path, size in files
    ]
    return BackupManifest(
        created_at=datetime.now(timezone.utc).isoformat(),
        root=str(root.resolve()),
        entries=entries,
    )


def write_manifest(
    root: Path,
    *,
    progress: ManifestProgressCallback | None = None,
    workers: int | None = None,
    pass_index: int = 1,
    pass_total: int = 2,
) -> Path:
    io_path(root / VERIFICATION_RECEIPT).unlink(missing_ok=True)
    manifest = build_manifest(
        root,
        progress=progress,
        workers=workers,
        pass_index=pass_index,
        pass_total=pass_total,
    )
    out = io_path(root / "manifest.json")
    out.write_text(json.dumps(asdict(manifest), indent=2, ensure_ascii=False), encoding="utf-8")
    return root / "manifest.json"


def verify_manifest_metadata(root: Path, manifest_path: Path | None = None) -> tuple[bool, list[str]]:
    """Fast size/existence check between two already verified workflow stages."""
    manifest_path = manifest_path or root / "manifest.json"
    try:
        raw = json.loads(io_path(manifest_path).read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        return False, [f"manifest could not be read: {exc}"]
    problems: list[str] = []
    receipt_path = root / VERIFICATION_RECEIPT
    if receipt_path.is_file():
        try:
            receipt = json.loads(receipt_path.read_text(encoding="utf-8"))
            if receipt.get("manifest_sha256") == sha256_file(manifest_path):
                if (root / "database" / "original.sql").is_file():
                    completeness = verify_backup_completeness(root, require_database=True)
                    problems.extend(f"incomplete: {problem}" for problem in completeness.problems)
                return (not problems, problems)
        except (OSError, json.JSONDecodeError):
            pass

    # Backward-compatible fast path: older versions recorded the completed full
    # verification in their report but did not create a receipt. This shortcut
    # is used only for non-destructive intermediate stages; execute_rebuild still
    # performs a complete SHA pass immediately before remote wipe.
    legacy_verified = False
    for report_path, flag in (
        (root / "backup-report.json", "verified"),
        (root / "clean-report.json", "clean_verified"),
    ):
        if not report_path.is_file():
            continue
        try:
            legacy_verified = bool(json.loads(report_path.read_text(encoding="utf-8")).get(flag))
        except (OSError, json.JSONDecodeError):
            legacy_verified = False
        if legacy_verified:
            break
    if legacy_verified:
        if (root / "database" / "original.sql").is_file():
            completeness = verify_backup_completeness(root, require_database=True)
            problems.extend(f"incomplete: {problem}" for problem in completeness.problems)
        if not problems:
            _save_verification_receipt(root, manifest_path, len(raw.get("entries", [])))
        return (not problems, problems)

    for entry in raw.get("entries", []):
        relative = str(entry.get("path") or "")
        path = io_path(root / relative)
        if not path.is_file():
            problems.append(f"missing: {relative}")
            continue
        if path.stat().st_size != int(entry.get("size") or 0):
            problems.append(f"size mismatch: {relative}")
    if (root / "database" / "original.sql").is_file():
        completeness = verify_backup_completeness(root, require_database=True)
        problems.extend(f"incomplete: {problem}" for problem in completeness.problems)
    ok = not problems
    if ok:
        _save_verification_receipt(root, manifest_path, len(raw.get("entries", [])))
    else:
        io_path(root / VERIFICATION_RECEIPT).unlink(missing_ok=True)
    return (ok, problems)


def verify_manifest(
    root: Path,
    manifest_path: Path | None = None,
    *,
    progress: ManifestProgressCallback | None = None,
    workers: int | None = None,
    pass_index: int = 2,
    pass_total: int = 2,
) -> tuple[bool, list[str]]:
    """Verify file integrity and minimum recovery-set completeness in parallel."""
    manifest_path = manifest_path or root / "manifest.json"
    try:
        raw = json.loads(io_path(manifest_path).read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        return False, [f"manifest could not be read: {exc}"]

    problems: list[str] = []
    files: list[tuple[str, Path, int]] = []
    expected_hashes: dict[str, str] = {}
    for entry in raw.get("entries", []):
        relative = str(entry.get("path") or "")
        path = io_path(root / relative)
        if not path.is_file():
            problems.append(f"missing: {relative}")
            continue
        expected_size = int(entry.get("size") or 0)
        if path.stat().st_size != expected_size:
            problems.append(f"size mismatch: {relative}")
            continue
        files.append((relative, path, expected_size))
        expected_hashes[relative] = str(entry.get("sha256") or "")

    actual_hashes = _parallel_hash(
        files,
        phase="manifest_verify",
        progress=progress,
        workers=workers,
        pass_index=pass_index,
        pass_total=pass_total,
    )
    for relative, actual in actual_hashes.items():
        if actual != expected_hashes[relative]:
            problems.append(f"hash mismatch: {relative}")

    if (root / "database" / "original.sql").is_file():
        completeness = verify_backup_completeness(root, require_database=True)
        problems.extend(f"incomplete: {problem}" for problem in completeness.problems)
    ok = not problems
    if ok:
        _save_verification_receipt(root, manifest_path, len(raw.get("entries", [])))
    else:
        io_path(root / VERIFICATION_RECEIPT).unlink(missing_ok=True)
    return (ok, problems)


def verify_backup_completeness(root: Path, *, require_database: bool = True) -> BackupCompleteness:
    """Check whether a backup contains every required artifact for rebuild."""
    problems: list[str] = []

    if require_database:
        database = root / "database" / "original.sql"
        if not database.is_file() or database.stat().st_size == 0:
            problems.append("database/original.sql is missing or empty")

    uploads = root / "uploads"
    legacy_uploads = root / "wp-content" / "uploads"
    if not uploads.is_dir() and not legacy_uploads.is_dir():
        problems.append("uploads backup directory is missing")

    config = root / "config" / "wp-config.php"
    if not config.is_file() or config.stat().st_size == 0:
        problems.append("config/wp-config.php is missing or empty")

    report_path = root / "backup-report.json"
    if not report_path.is_file():
        problems.append("backup-report.json is missing")
    else:
        try:
            report = json.loads(report_path.read_text(encoding="utf-8"))
            for item in report.get("items", []):
                remote = str(item.get("remote_path", ""))
                status = str(item.get("status", ""))
                optional = (
                    remote.endswith("/wp-content/mu-plugins")
                    or remote.endswith("/php.ini")
                    or remote.endswith("/.user.ini")
                    or remote.endswith("/robots.txt")
                    or remote.endswith("/.htaccess")
                )
                if status != "ok" and not optional:
                    problems.append(
                        f"backup stage incomplete: {remote or 'unknown'} ({status or 'unknown'})"
                    )
        except Exception as exc:
            problems.append(f"backup-report.json could not be parsed: {exc}")

    return BackupCompleteness(complete=not problems, problems=problems)
