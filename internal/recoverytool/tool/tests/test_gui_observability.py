from __future__ import annotations

from datetime import datetime, timezone

from wpclean import gui_server
from wpclean.gui_observability import classify_exception, error_payload, job_timing
from wpclean.project_journal import read_activity


def test_common_failures_have_stable_codes_and_vietnamese_recovery() -> None:
    auth = classify_exception(RuntimeError("530 Login authentication failed"), stage="ftp-test")
    timeout = classify_exception(TimeoutError("WinError 10060"), stage="ftp-test")
    permission = classify_exception(PermissionError("550 Permission denied"), stage="rebuild")
    reset = classify_exception(
        RuntimeError(
            "FTP delete failed after 5 attempts: /public_html/403.php "
            "(ConnectionResetError: [WinError 10054] forcibly closed by the remote host)"
        ),
        stage="rebuild",
    )

    assert auth.code == "FTP-AUTH-001"
    assert "mật khẩu" in auth.message
    assert auth.recovery
    assert timeout.code == "FTP-TIMEOUT-001"
    assert permission.code == "FTP-PERM-001"
    assert "wipe" in permission.recovery
    assert reset.code == "FTP-RESET-001"
    assert "FTP daemon" in reset.recovery


def test_wordpress_package_network_failure_is_not_reported_as_ftp() -> None:
    error = classify_exception(
        RuntimeError(
            "Không thể tải package WordPress sạch từ wordpress.org và máy chưa có package cache hợp lệ. "
            "ConnectionRefusedError: [WinError 10061] actively refused"
        ),
        stage="rebuild",
    )

    assert error.code == "WP-CORE-DOWNLOAD-001"
    assert "WordPress" in error.title


def test_local_temp_permission_failure_is_not_reported_as_ftp() -> None:
    error = classify_exception(
        PermissionError(13, "Permission denied", r"C:\\Temp\\wpclean-core\\wp-trackback.php"),
        stage="rebuild",
    )

    assert error.code == "LOCAL-FS-PERM-001"


def test_api_error_payload_is_structured() -> None:
    payload = error_payload(FileExistsError("duplicate"))

    assert payload["errorCode"] == "PROJECT-EXISTS-001"
    assert payload["errorTitle"]
    assert payload["recovery"]
    assert payload["retryable"] is False
    assert "FileExistsError" in payload["technical"]


def test_job_timing_distinguishes_waiting_slow_and_stalled() -> None:
    now = datetime(2026, 8, 15, 10, 5, tzinfo=timezone.utc)
    started = "2026-08-15T10:00:00+00:00"

    waiting = job_timing(started_at=started, updated_at="2026-08-15T10:04:30+00:00", status="running", now=now)
    slow = job_timing(started_at=started, updated_at="2026-08-15T10:03:30+00:00", status="running", now=now)
    stalled = job_timing(started_at=started, updated_at="2026-08-15T10:01:00+00:00", status="running", now=now)

    assert waiting["health"] == "waiting"
    assert slow["health"] == "slow"
    assert stalled["health"] == "stalled"
    assert stalled["isStalled"] is True
    assert stalled["elapsedSeconds"] == 300


def test_gui_job_persists_structured_error_and_progress(tmp_path) -> None:
    job = gui_server.GuiJob(project="demo", status="running", stage="ftp-test")
    job._journal_dir = tmp_path
    job._journal_secrets = ("secret-pass",)
    job.started_at = datetime.now().astimezone().isoformat(timespec="seconds")

    gui_server._progress(
        job,
        {
            "phase": "transfer",
            "files_completed": 12,
            "files_total": 40,
            "bytes_downloaded": 2048,
            "bytes_total": 8192,
            "bytes_per_second": 1024,
            "current_file": "/public_html/wp-content/photo.jpg",
        },
    )
    job.fail(TimeoutError("WinError 10060 secret-pass"))
    payload = job.to_dict()

    assert payload["progress"]["completed"] == 12
    assert payload["progress"]["total"] == 40
    assert payload["progress"]["bytesPerSecond"] == 1024
    assert payload["errorInfo"]["code"] == "FTP-TIMEOUT-001"
    assert payload["errorInfo"]["recovery"]
    assert "secret-pass" not in str(payload["errorInfo"])
    assert payload["health"] == "error"

    activity = read_activity(tmp_path)
    error = next(item for item in activity if item.get("level") == "error")
    assert error["code"] == "FTP-TIMEOUT-001"
    assert error["recovery"]
    assert "secret-pass" not in str(error)


def test_ftp_file_failure_keeps_sanitized_server_reason_in_progress() -> None:
    job = gui_server.GuiJob(project="demo", status="running", stage="backup-files")
    job._journal_secrets = ("secret-pass",)

    gui_server._progress(
        job,
        {
            "phase": "file_failed",
            "files_completed": 1,
            "files_total": 20,
            "files_failed": 1,
            "current_file": "/public_html/wp-content/photo.jpg",
            "error": "error_perm: 550 RETR denied secret-pass",
        },
    )

    assert "550 RETR denied" in job.message
    assert "secret-pass" not in job.message
    assert "***" in job.message


def test_streaming_ftp_progress_does_not_jump_backwards_before_inventory_finishes() -> None:
    job = gui_server.GuiJob(project="demo", status="running", stage="backup-files", percent=12)

    gui_server._progress(
        job,
        {
            "phase": "discover_transfer",
            "files_found": 8,
            "files_total": 8,
            "files_completed": 8,
            "bytes_downloaded": 4096,
            "bytes_total": 4096,
            "discovery_complete": False,
            "current_file": "/public_html/wp-content/themes/demo/style.css",
        },
    )

    assert job.percent == 12
    assert job.completed_items == 8
    assert job.total_items == 8
    assert job.message == "Đang quét và tải backup"


def test_rebuild_wipe_reports_real_weighted_progress() -> None:
    job = gui_server.GuiJob(project="demo", status="running", stage="rebuild", percent=5)

    gui_server._progress(
        job,
        {
            "phase": "wipe",
            "items_completed": 20,
            "items_total": 100,
            "unit": "mục",
            "current": "/public_html/wp-includes/style.css",
        },
    )

    payload = job.to_dict()
    assert payload["percent"] == 16
    assert payload["progress"]["completed"] == 20
    assert payload["progress"]["total"] == 100
    assert payload["progress"]["phase"] == "wipe"
    assert payload["progress"]["unit"] == "mục"
    assert payload["message"] == "Đang xóa code cũ"


def test_rebuild_streaming_upload_waits_for_final_inventory_before_weighting_progress() -> None:
    job = gui_server.GuiJob(project="demo", status="running", stage="rebuild", percent=32)

    gui_server._progress(
        job,
        {
            "phase": "upload_core",
            "files_completed": 1,
            "files_total": 1,
            "discovery_complete": False,
            "current": "/public_html/index.php",
        },
    )

    assert job.percent == 32
    assert job.completed_items == 1
    assert job.total_items == 1


def test_new_rebuild_phase_clears_stale_hash_pass_and_worker_metrics() -> None:
    job = gui_server.GuiJob(project="demo", status="running", stage="rebuild", percent=8)
    job.progress_phase = "manifest_verify"
    job.pass_index = 2
    job.pass_total = 2
    job.worker_count = 8

    gui_server._progress(
        job,
        {
            "phase": "wipe",
            "items_completed": 1,
            "items_total": 2,
            "current": "/public_html/index.php",
        },
    )

    payload = job.to_dict()["progress"]
    assert payload["passIndex"] == 0
    assert payload["passTotal"] == 0
    assert payload["workers"] == 0
