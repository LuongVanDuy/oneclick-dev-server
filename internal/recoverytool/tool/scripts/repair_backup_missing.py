from __future__ import annotations

import argparse
import json
import os
import sys
import time
from collections import defaultdict
from datetime import datetime
from pathlib import Path, PurePosixPath


PROJECT_ROOT = Path(__file__).resolve().parents[1]
SRC_ROOT = PROJECT_ROOT / "src"
if str(SRC_ROOT) not in sys.path:
    sys.path.insert(0, str(SRC_ROOT))

from wpclean.backup import verify_manifest, write_manifest  # noqa: E402
from wpclean.clean_builder import build_clean_restore  # noqa: E402
from wpclean.site_config import load_site_profile  # noqa: E402
from wpclean.transport.ftp import FTPConfig, FTPTransport, RemoteFile  # noqa: E402
from wpclean.windows_paths import io_path  # noqa: E402


def _write_log(handle, message: str) -> None:
    stamp = datetime.now().strftime("%H:%M:%S")
    handle.write(f"[{stamp}] {message}\n")
    handle.flush()


def _safe_local_path(item: dict, remote_path: str, backup_root: Path) -> Path:
    local_root = Path(str(item["local_path"]))
    if not local_root.is_absolute():
        local_root = PROJECT_ROOT / local_root
    local_root = local_root.resolve()
    backup_resolved = backup_root.resolve()
    if not local_root.is_relative_to(backup_resolved):
        raise RuntimeError(f"Unsafe backup item path: {local_root}")

    if str(item.get("kind")) == "file":
        target = local_root
    else:
        relative = PurePosixPath(remote_path).relative_to(
            PurePosixPath(str(item["remote_path"]))
        )
        target = local_root.joinpath(*relative.parts)
    target = target.resolve()
    if not target.is_relative_to(backup_resolved):
        raise RuntimeError(f"Unsafe repaired file path: {target}")
    return target


def _collect_tasks(report: dict, backup_root: Path) -> list[dict]:
    tasks: list[dict] = []
    seen: set[str] = set()
    for item in report.get("items", []):
        for failure in item.get("failed_files", []):
            remote_path = str(failure.get("path") or "").strip()
            if not remote_path or remote_path in seen:
                continue
            seen.add(remote_path)
            tasks.append(
                {
                    "remote_path": remote_path,
                    "remote_root": str(item["remote_path"]),
                    "local_root": str(Path(str(item["local_path"])).resolve()),
                    "local_path": str(_safe_local_path(item, remote_path, backup_root)),
                    "size": None,
                }
            )
    return tasks


def _load_remote_sizes(transport: FTPTransport, tasks: list[dict], log) -> None:
    by_parent: dict[str, list[dict]] = defaultdict(list)
    for task in tasks:
        by_parent[str(PurePosixPath(task["remote_path"]).parent)].append(task)

    client = transport._new_client()
    try:
        for index, (parent, parent_tasks) in enumerate(sorted(by_parent.items()), start=1):
            wanted = {PurePosixPath(task["remote_path"]).name: task for task in parent_tasks}
            try:
                for name, facts in transport._mlsd(client, parent):
                    task = wanted.get(name)
                    raw_size = facts.get("size") if task is not None else None
                    if task is not None and raw_size and str(raw_size).isdigit():
                        task["size"] = int(raw_size)
            except Exception as exc:
                _write_log(log, f"Không đọc được size tại {parent}: {type(exc).__name__}: {exc}")
            _write_log(log, f"Đã kiểm kê thư mục {index}/{len(by_parent)}: {parent}")
    finally:
        try:
            client.quit()
        except Exception:
            client.close()


def _download_missing(
    transport: FTPTransport,
    tasks: list[dict],
    *,
    outer_attempts: int,
    log,
) -> tuple[set[str], dict[str, str], int]:
    remaining = {task["remote_path"]: task for task in tasks}
    completed: set[str] = set()
    errors: dict[str, str] = {}
    repaired_bytes = 0
    total = len(tasks)

    for outer_attempt in range(1, outer_attempts + 1):
        if not remaining:
            break
        _write_log(
            log,
            f"Lượt tải bù {outer_attempt}/{outer_attempts}: còn {len(remaining)}/{total} file",
        )
        for task in list(remaining.values()):
            remote_path = task["remote_path"]
            try:
                status, transferred = transport._download_one(
                    RemoteFile(path=remote_path, size=task["size"]),
                    task["remote_root"],
                    Path(task["local_root"]),
                    True,
                )
                local_path = io_path(Path(task["local_path"]))
                expected_size = task["size"]
                if not local_path.is_file():
                    raise RuntimeError("file local không tồn tại sau khi FTP báo thành công")
                if expected_size is not None and local_path.stat().st_size != expected_size:
                    raise RuntimeError(
                        f"size local {local_path.stat().st_size} khác hosting {expected_size}"
                    )
                completed.add(remote_path)
                repaired_bytes += transferred
                remaining.pop(remote_path, None)
                errors.pop(remote_path, None)
                done = len(completed)
                if done == 1 or done % 25 == 0 or done == total:
                    _write_log(log, f"Đã tải bù {done}/{total} file")
            except Exception as exc:
                transport._reset_thread_client()
                errors[remote_path] = f"{type(exc).__name__}: {exc}"

        if remaining and outer_attempt < outer_attempts:
            time.sleep(min(2**outer_attempt, 15))

    transport._reset_thread_client()
    return completed, errors, repaired_bytes


def _update_report(report: dict, repaired: set[str], repaired_bytes: int) -> None:
    for item in report.get("items", []):
        failures = list(item.get("failed_files", []))
        repaired_here = [entry for entry in failures if entry.get("path") in repaired]
        remaining = [entry for entry in failures if entry.get("path") not in repaired]
        item["failed_files"] = remaining
        item["files_failed"] = len(remaining)
        if repaired_here:
            total = int(item.get("files_total") or 0)
            skipped = int(item.get("files_skipped") or 0)
            downloaded = int(item.get("files_downloaded") or 0) + len(repaired_here)
            item["files_downloaded"] = min(downloaded, max(0, total - skipped)) if total else downloaded
        if failures and not remaining:
            item["status"] = "ok"
            item["error"] = None

    report["exclusions"] = [
        entry for entry in report.get("exclusions", []) if entry.get("path") not in repaired
    ]
    report["verified"] = False
    report["verified_with_exclusions"] = False
    report["verification_problems"] = []
    if repaired_bytes:
        report["repair_bytes_downloaded"] = int(report.get("repair_bytes_downloaded") or 0) + repaired_bytes
    report["repaired_at"] = datetime.now().astimezone().isoformat(timespec="seconds")


def main() -> int:
    parser = argparse.ArgumentParser(description="Tải bù file FTP bị thiếu trong backup-report.json")
    parser.add_argument("--domain", required=True)
    parser.add_argument("--retries", type=int, default=8)
    parser.add_argument("--outer-attempts", type=int, default=6)
    args = parser.parse_args()

    domain = args.domain.strip().lower()
    profile_path = PROJECT_ROOT / "sites" / f"{domain}.json"
    backup_root = (PROJECT_ROOT / "backups" / domain).resolve()
    report_path = backup_root / "backup-report.json"
    log_path = PROJECT_ROOT / "logs" / f"{domain}-backup-repair.log"
    result_path = PROJECT_ROOT / "reports" / f"{domain}-backup-repair.json"
    log_path.parent.mkdir(parents=True, exist_ok=True)
    result_path.parent.mkdir(parents=True, exist_ok=True)

    with log_path.open("a", encoding="utf-8", buffering=1) as log:
        _write_log(log, f"Bắt đầu tải bù backup; pid={os.getpid()}")
        try:
            profile = load_site_profile(profile_path)
            if not profile.password:
                raise RuntimeError("Profile không có mật khẩu FTP")
            report = json.loads(report_path.read_text(encoding="utf-8"))
            tasks = _collect_tasks(report, backup_root)
            if not tasks:
                _write_log(log, "Backup report không còn file thiếu")
                return 0

            transport = FTPTransport(
                FTPConfig(
                    host=profile.host,
                    username=profile.username,
                    password=profile.password,
                    port=profile.port,
                    tls=profile.use_tls,
                    passive=profile.passive,
                    timeout=60.0,
                    workers=1,
                    block_size=max(256 * 1024, profile.block_mb * 1024 * 1024),
                    retries=max(1, args.retries),
                )
            )
            _write_log(log, f"Cần tải bù {len(tasks)} file bằng 1 kết nối tuần tự")
            _load_remote_sizes(transport, tasks, log)
            repaired, errors, repaired_bytes = _download_missing(
                transport,
                tasks,
                outer_attempts=max(1, args.outer_attempts),
                log=log,
            )
            _update_report(report, repaired, repaired_bytes)
            remaining = len(tasks) - len(repaired)
            if remaining:
                result_path.write_text(
                    json.dumps(
                        {
                            "status": "incomplete",
                            "domain": domain,
                            "repaired": len(repaired),
                            "remaining": remaining,
                            "errors": errors,
                        },
                        indent=2,
                        ensure_ascii=False,
                    ),
                    encoding="utf-8",
                )
                _write_log(log, f"CHƯA ĐỦ: tải được {len(repaired)}, còn lỗi {remaining} file")
                return 2

            clean_root = backup_root / "clean"
            archived_clean: Path | None = None
            if clean_root.exists():
                archive_root = PROJECT_ROOT / "repairs" / domain
                archive_root.mkdir(parents=True, exist_ok=True)
                archived_clean = archive_root / f"clean-before-backup-repair-{datetime.now():%Y%m%d-%H%M%S}"
                clean_root.replace(archived_clean)
                _write_log(log, f"Đã lưu clean staging cũ tại {archived_clean}")

            report["manifest_path"] = str(backup_root / "manifest.json")
            report["verified"] = True
            report["verified_with_exclusions"] = False
            report["verification_problems"] = []
            report_path.write_text(json.dumps(report, indent=2, ensure_ascii=False), encoding="utf-8")

            _write_log(log, "Đang tạo lại manifest backup gốc")
            manifest = write_manifest(backup_root)
            _write_log(log, "Đang xác minh lại SHA-256 backup gốc")
            verified, problems = verify_manifest(backup_root, manifest)
            if not verified:
                raise RuntimeError("Backup verify failed: " + "; ".join(problems[:20]))
            _write_log(log, "Backup gốc đã đủ và vượt qua SHA-256")

            _write_log(log, "Đang tạo lại clean staging từ backup đã sửa")
            clean_report = build_clean_restore(
                backup_root,
                ftp_password=profile.password,
                host=profile.host,
            )
            if not clean_report.clean_verified:
                raise RuntimeError("Clean staging mới chưa vượt qua SHA-256")

            result = {
                "status": "complete",
                "domain": domain,
                "repaired": len(repaired),
                "remaining": 0,
                "backup_verified": True,
                "clean_verified": True,
                "archived_clean": str(archived_clean) if archived_clean else None,
                "finished_at": datetime.now().astimezone().isoformat(timespec="seconds"),
            }
            result_path.write_text(
                json.dumps(result, indent=2, ensure_ascii=False),
                encoding="utf-8",
            )
            _write_log(log, "HOÀN TẤT: backup và clean staging mới đã được xác minh")
            return 0
        except Exception as exc:
            result_path.write_text(
                json.dumps(
                    {
                        "status": "error",
                        "domain": domain,
                        "error": f"{type(exc).__name__}: {exc}",
                        "finished_at": datetime.now().astimezone().isoformat(timespec="seconds"),
                    },
                    indent=2,
                    ensure_ascii=False,
                ),
                encoding="utf-8",
            )
            _write_log(log, f"ERROR: {type(exc).__name__}: {exc}")
            return 1


if __name__ == "__main__":
    raise SystemExit(main())
