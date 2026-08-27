from __future__ import annotations

from dataclasses import asdict, dataclass, field
from ftplib import error_perm
from pathlib import Path, PurePosixPath
from typing import Callable
import json
import shutil

from .backup import verify_manifest, write_manifest
from .ftp_rebuild_resilience import prune_remote_directory_allowlist
from .mu_plugin_policy import (
    ALLOWED_MU_PLUGIN_COMPONENT_KINDS,
    ALLOWED_MU_PLUGIN_COMPONENTS,
)
from .transport import FTPTransport, TransferStats


BackupProgressCallback = Callable[[dict], None]


@dataclass(slots=True)
class BackupItemReport:
    remote_path: str
    local_path: str
    kind: str
    status: str
    files_total: int = 0
    files_downloaded: int = 0
    files_skipped: int = 0
    files_failed: int = 0
    bytes_downloaded: int = 0
    failed_files: list[dict] = field(default_factory=list)
    error: str | None = None


@dataclass(slots=True)
class RemoteBackupReport:
    transport: str
    remote_root: str
    backup_root: str
    items: list[BackupItemReport] = field(default_factory=list)
    exclusions: list[dict] = field(default_factory=list)
    removed_remote_mu_plugins: list[str] = field(default_factory=list)
    manifest_path: str | None = None
    verified: bool = False
    verified_with_exclusions: bool = False
    verification_problems: list[str] = field(default_factory=list)


REFERENCE_DIRS = (
    ("wp-content/uploads", "uploads"),
    ("wp-content/themes", "themes"),
    ("wp-content/plugins", "plugins"),
)

CONFIG_FILES = (
    "wp-config.php",
    ".htaccess",
    ".user.ini",
    "php.ini",
    "robots.txt",
)


def _join(root: str, child: str) -> str:
    return str(PurePosixPath(root) / child)


def _item_from_stats(remote: str, local: Path, kind: str, stats: TransferStats) -> BackupItemReport:
    failed_files = [asdict(item) for item in stats.failures]
    if stats.files_failed:
        status = "transfer-failed"
        error = f"{stats.files_failed} file(s) failed after FTP retry and reconciliation"
    else:
        status = "ok"
        error = None

    return BackupItemReport(
        remote_path=remote,
        local_path=str(local),
        kind=kind,
        status=status,
        files_total=stats.files_total,
        files_downloaded=stats.files_downloaded,
        files_skipped=stats.files_skipped,
        files_failed=stats.files_failed,
        bytes_downloaded=stats.bytes_downloaded,
        failed_files=failed_files,
        error=error,
    )


def _record_transfer_exclusions(report: RemoteBackupReport, stage: str, item: BackupItemReport) -> None:
    if item.status != "ok-with-exclusions":
        return
    for failure in item.failed_files:
        report.exclusions.append(
            {
                "stage": stage,
                "path": failure.get("path"),
                "error": failure.get("error"),
                "policy": "excluded-from-verification-and-restore",
            }
        )


def _backup_allowed_mu_plugins(
    transport: FTPTransport,
    remote_root: str,
    backup_root: Path,
    report: RemoteBackupReport,
    *,
    resume: bool,
    progress: BackupProgressCallback | None,
) -> None:
    remote_mu = _join(remote_root, "wp-content/mu-plugins")
    local_mu = backup_root / "mu-plugins"
    local_mu.mkdir(parents=True, exist_ok=True)
    for child in list(local_mu.iterdir()):
        expected_kind = ALLOWED_MU_PLUGIN_COMPONENT_KINDS.get(child.name.casefold())
        if expected_kind == "directory" and child.is_dir() and not child.is_symlink():
            continue
        if expected_kind == "file" and child.is_file() and not child.is_symlink():
            continue
        if child.is_dir() and not child.is_symlink():
            shutil.rmtree(child)
        else:
            child.unlink(missing_ok=True)
    if progress:
        progress({"phase": "stage", "stage": "mu-plugins", "remote_path": remote_mu})

    try:
        removed = prune_remote_directory_allowlist(
            transport,
            remote_mu,
            ALLOWED_MU_PLUGIN_COMPONENTS,
            progress=progress,
        )
        report.removed_remote_mu_plugins.extend(removed)

        client = transport._new_client()
        try:
            entries = list(transport._mlsd(client, remote_mu))
        finally:
            try:
                client.quit()
            except Exception:
                client.close()
    except error_perm as exc:
        report.items.append(
            BackupItemReport(
                remote_path=remote_mu,
                local_path=str(local_mu),
                kind="directory",
                status="missing-or-denied",
                error=str(exc),
            )
        )
        if progress:
            progress({"phase": "stage_skipped", "stage": "mu-plugins", "error": str(exc)})
        return
    except Exception as exc:
        report.items.append(
            BackupItemReport(
                remote_path=remote_mu,
                local_path=str(local_mu),
                kind="directory",
                status="transfer-failed",
                error=f"{type(exc).__name__}: {exc}",
            )
        )
        if progress:
            progress({"phase": "stage_failed", "stage": "mu-plugins", "error": str(exc)})
        return

    actual = {
        name.casefold(): (name, (facts.get("type") or "").lower())
        for name, facts in entries
        if name not in {".", ".."}
    }
    invalid_names = {
        name
        for key, (name, kind) in actual.items()
        if key in ALLOWED_MU_PLUGIN_COMPONENT_KINDS
        and kind
        != ("dir" if ALLOWED_MU_PLUGIN_COMPONENT_KINDS[key] == "directory" else "file")
    }
    if invalid_names:
        keep = ALLOWED_MU_PLUGIN_COMPONENTS - {name.casefold() for name in invalid_names}
        removed = prune_remote_directory_allowlist(
            transport,
            remote_mu,
            keep,
            progress=progress,
        )
        report.removed_remote_mu_plugins.extend(
            path for path in removed if path not in report.removed_remote_mu_plugins
        )
        actual = {
            key: value for key, value in actual.items() if value[0] not in invalid_names
        }
        for invalid_name in invalid_names:
            local_invalid = local_mu / invalid_name
            if local_invalid.is_dir() and not local_invalid.is_symlink():
                shutil.rmtree(local_invalid)
            else:
                local_invalid.unlink(missing_ok=True)

    for allowed_name, expected_kind in ALLOWED_MU_PLUGIN_COMPONENT_KINDS.items():
        entry = actual.get(allowed_name)
        if entry is None:
            continue
        remote_name, _kind = entry
        remote = _join(remote_mu, remote_name)
        local = local_mu / allowed_name

        def transfer_progress(event: dict) -> None:
            if progress:
                progress({**event, "stage": "mu-plugins"})

        try:
            if expected_kind == "directory":
                stats = transport.download_tree(
                    remote,
                    local,
                    resume=resume,
                    progress=transfer_progress,
                )
            else:
                stats = transport.download_file(remote, local, resume=resume)
            item = _item_from_stats(remote, local, expected_kind, stats)
            report.items.append(item)
            _record_transfer_exclusions(report, "mu-plugins", item)
        except Exception as exc:
            report.items.append(
                BackupItemReport(
                    remote_path=remote,
                    local_path=str(local),
                    kind=expected_kind,
                    status="transfer-failed",
                    files_failed=1,
                    failed_files=[{"path": remote, "error": f"{type(exc).__name__}: {exc}"}],
                    error=f"{type(exc).__name__}: {exc}",
                )
            )
            if progress:
                progress({"phase": "stage_failed", "stage": "mu-plugins", "error": str(exc)})


def backup_wordpress_ftp(
    transport: FTPTransport,
    remote_root: str,
    backup_root: Path,
    *,
    resume: bool = True,
    progress: BackupProgressCallback | None = None,
    finalize_manifest: bool = True,
) -> RemoteBackupReport:
    """Back up WordPress user data/reference code over FTP/FTPS.

    Every required remote file must be present locally. Missing files are never
    downgraded to exclusions: the backup stays blocked after retry/reconciliation.
    The GUI can defer the expensive full-tree SHA pass until the database has also
    been downloaded, avoiding an otherwise duplicate manifest cycle.
    """
    backup_root.mkdir(parents=True, exist_ok=True)
    report = RemoteBackupReport(
        transport="ftps" if transport.config.tls else "ftp",
        remote_root=remote_root,
        backup_root=str(backup_root),
    )

    for remote_rel, local_name in REFERENCE_DIRS:
        remote = _join(remote_root, remote_rel)
        local = backup_root / local_name
        if progress:
            progress({"phase": "stage", "stage": local_name, "remote_path": remote})

        def transfer_progress(event: dict, stage: str = local_name) -> None:
            if progress:
                progress({**event, "stage": stage})

        try:
            stats = transport.download_tree(remote, local, resume=resume, progress=transfer_progress)
            item = _item_from_stats(remote, local, "directory", stats)
            report.items.append(item)
            _record_transfer_exclusions(report, local_name, item)
        except error_perm as exc:
            report.items.append(BackupItemReport(
                remote_path=remote,
                local_path=str(local),
                kind="directory",
                status="missing-or-denied",
                error=str(exc),
            ))
            if progress:
                progress({"phase": "stage_skipped", "stage": local_name, "error": str(exc)})
        except Exception as exc:
            report.items.append(BackupItemReport(
                remote_path=remote,
                local_path=str(local),
                kind="directory",
                status="transfer-failed",
                error=f"{type(exc).__name__}: {exc}",
            ))
            if progress:
                progress({"phase": "stage_failed", "stage": local_name, "error": str(exc)})

    _backup_allowed_mu_plugins(
        transport,
        remote_root,
        backup_root,
        report,
        resume=resume,
        progress=progress,
    )

    config_dir = backup_root / "config"
    config_dir.mkdir(parents=True, exist_ok=True)
    if progress:
        progress({"phase": "stage", "stage": "config", "remote_path": remote_root})
    for name in CONFIG_FILES:
        remote = _join(remote_root, name)
        local = config_dir / name
        try:
            stats = transport.download_file(remote, local, resume=resume)
            report.items.append(_item_from_stats(remote, local, "file", stats))
            if progress:
                progress({"phase": "config_file", "stage": "config", "file": name, "status": "ok"})
        except error_perm as exc:
            report.items.append(BackupItemReport(
                remote_path=remote,
                local_path=str(local),
                kind="file",
                status="missing-or-denied",
                error=str(exc),
            ))
            if progress:
                progress({"phase": "config_file", "stage": "config", "file": name, "status": "missing", "error": str(exc)})
        except Exception as exc:
            report.items.append(BackupItemReport(
                remote_path=remote,
                local_path=str(local),
                kind="file",
                status="transfer-failed",
                files_failed=1,
                failed_files=[{"path": remote, "error": f"{type(exc).__name__}: {exc}"}],
                error=f"{type(exc).__name__}: {exc}",
            ))
            if progress:
                progress({"phase": "config_file", "stage": "config", "file": name, "status": "failed", "error": str(exc)})

    transfer_problems: list[str] = []
    for item in report.items:
        if item.status == "transfer-failed":
            transfer_problems.append(
                f"Incomplete remote backup: {item.remote_path} ({item.files_failed or 1} failed file(s))"
            )
            for failure in item.failed_files[:20]:
                transfer_problems.append(f"  - {failure.get('path')}: {failure.get('error')}")

        optional = (
            item.remote_path.endswith("/wp-content/mu-plugins")
            or item.remote_path.endswith("/php.ini")
            or item.remote_path.endswith("/.user.ini")
            or item.remote_path.endswith("/robots.txt")
            or item.remote_path.endswith("/.htaccess")
        )
        if item.status != "ok" and not optional and item.status != "transfer-failed":
            transfer_problems.append(
                f"Incomplete remote backup: {item.remote_path} ({item.status})"
            )

    report_file = backup_root / "backup-report.json"
    report.verified = not transfer_problems
    report.verified_with_exclusions = False
    report.verification_problems = list(transfer_problems)

    if finalize_manifest:
        manifest = backup_root / "manifest.json"
        report.manifest_path = str(manifest)
        # Write the final success fields before hashing so backup-report.json is
        # part of the exact manifest that is verified; no second regeneration.
        report_file.write_text(json.dumps(asdict(report), indent=2, ensure_ascii=False), encoding="utf-8")
        if progress:
            progress({"phase": "manifest_inventory", "stage": "manifest", "pass_index": 1, "pass_total": 2})
        manifest = write_manifest(backup_root, progress=progress)
        integrity_ok, integrity_problems = verify_manifest(
            backup_root,
            manifest,
            progress=progress,
        )
        if not integrity_ok:
            report.verified = False
            report.verification_problems = [*integrity_problems, *transfer_problems]
            report_file.write_text(json.dumps(asdict(report), indent=2, ensure_ascii=False), encoding="utf-8")
    else:
        report.manifest_path = None
        report_file.write_text(json.dumps(asdict(report), indent=2, ensure_ascii=False), encoding="utf-8")

    if progress:
        progress(
            {
                "phase": "verified",
                "stage": "manifest",
                "verified": report.verified,
                "manifest_deferred": not finalize_manifest,
                "files_failed": sum(item.files_failed for item in report.items),
            }
        )
    return report
