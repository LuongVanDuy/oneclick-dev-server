from __future__ import annotations

from io import BytesIO, StringIO
import json
import re
import time
from pathlib import Path

from typer.testing import CliRunner

from wpclean import gui_entry  # activates GUI runtime patches
from wpclean import gui_journal_entry
from wpclean import project_delete_command
from wpclean.secret_runtime import remember_runtime_secret
from wpclean import gui_server
from wpclean.rebuild_entry import app


def _sandbox_gui(tmp_path: Path, monkeypatch) -> None:
    sites = tmp_path / "sites"
    backups = tmp_path / "backups"
    reports = tmp_path / "reports"
    repairs = tmp_path / "repairs"
    for path in (sites, backups, reports, repairs):
        path.mkdir(parents=True, exist_ok=True)

    monkeypatch.setattr(gui_server, "PROJECT_ROOT", tmp_path)
    monkeypatch.setattr(gui_server, "SITES_DIR", sites)
    monkeypatch.setattr(gui_server, "BACKUPS_DIR", backups)
    monkeypatch.setattr(gui_server, "REPORTS_DIR", reports)
    monkeypatch.setattr(gui_server, "REPAIRS_DIR", repairs)
    monkeypatch.setattr(gui_server.wizard, "PROJECT_ROOT", tmp_path)
    monkeypatch.setattr(gui_server.wizard, "SITES_DIR", sites)
    monkeypatch.setattr(gui_server.wizard, "BACKUPS_DIR", backups)
    monkeypatch.setattr(gui_server.wizard, "REPORTS_DIR", reports)
    monkeypatch.setattr(project_delete_command, "PROJECT_ROOT", tmp_path)
    monkeypatch.setattr(project_delete_command, "SITES_DIR", sites)
    monkeypatch.setattr(project_delete_command, "BACKUPS_DIR", backups)
    monkeypatch.setattr(project_delete_command, "REPORTS_DIR", reports)
    monkeypatch.setattr(project_delete_command, "REPAIRS_DIR", repairs)
    gui_server.JOBS.clear()
    gui_server.ACTIVE_PROJECT = None


def test_gui_html_is_self_contained_vietnamese_and_readable() -> None:
    html = gui_journal_entry._render_app("test-token")

    assert "WP Clean Rebuild" not in html
    assert "Khôi phục WordPress" in html
    assert '<div class="top-title">' not in html
    assert "Danh sách dự án" in html
    assert "Tạo dự án mới" in html
    assert "Xác nhận rebuild WordPress" in html
    assert "Sửa kết nối FTP" in html
    assert "Lưu & kiểm tra kết nối" in html
    assert "wpclean-readability" in html
    assert "html,body{font-size:16px" in html
    assert ".layout{display:block!important}" in html
    assert ".sidebar{display:none!important}" in html
    assert "page-head h1{font-size:32px}" in html
    assert "project-name h3{font-size:17px}" in html
    assert "field input,.field select{font-size:16px}" in html
    assert "drawer{width:min(1380px,98vw)}" in html
    assert "detail-layout{display:grid" in html
    assert "PROJECT LOG" in html
    assert "terminalOutput" in html
    assert "systemHealth" in html
    assert "errorInfo" in html
    assert "Tín hiệu cuối" in html
    assert "(done+running)/p.steps.length" in html
    assert "Chi tiết kỹ thuật" in html
    assert 'aria-live="polite"' in html
    assert 'type="password"' not in html
    assert "test-token" in html
    assert "https://cdn" not in html
    assert "unpkg.com" not in html
    assert "wpclean-coder-style" in html
    assert "wpclean-oneclick-crm-style" in html
    assert "html{color-scheme:light!important}" in html
    assert ".content{width:100%;max-width:none" in html
    assert "#projects,.project-header,.project-row{width:100%;max-width:none}" in html
    assert '<header class="topbar">' not in html
    assert '<div class="summary-shell"><div id="stats" class="summary"></div>' in html
    assert '<div class="list-toolbar">' in html
    assert "Chọn một dự án để xem và tiếp tục." in html
    assert ".project-header{display:none!important}" in html
    assert "#projects{display:grid;gap:8px}" in html
    assert "border:1px solid var(--line)!important;border-radius:8px" in html
    assert 'id="systemHealth"' in html
    assert 'id="search"' in html
    assert 'onclick="refreshAll()"' in html
    assert 'onclick="openCreate()"' in html
    assert ".page-head h1{color:var(--text);font-size:26px" in html
    assert ".project-name p{color:var(--muted);font-size:14px}" in html
    assert "--mono:" in html
    assert ".field.third{grid-column:span 2}" in html
    assert "height:44px;min-height:44px" in html
    assert 'class="field third"><label for="e_port"' in html
    assert "running?'Đang chạy':'Tiếp theo'" in html
    assert '<aside class="sidebar">' not in html
    assert "Xóa dự án chỉ xóa dữ liệu local" not in html
    assert "Log phiên hiện tại ở đây" not in html
    assert 'id="confirmRebuild"' not in html
    assert "Nhập chính xác tên dự án để xác nhận" in html
    assert "website trên hosting không thay đổi" in html
    assert "startDeletePolling" in html
    assert "p.completed&&!deleting" not in html
    assert "PROJECT LOG" in html
    assert "Tự cuộn:" in html

    weights = [int(item) for item in re.findall(r"font-weight:(\d+)", html)]
    assert weights
    assert max(weights) <= 700

    oneclick_style = re.search(
        r'<style id="wpclean-oneclick-crm-style">(.*?)</style>',
        html,
        re.DOTALL,
    )
    assert oneclick_style
    final_sizes = [int(item) for item in re.findall(r"font-size:(\d+)px", oneclick_style.group(1))]
    assert final_sizes
    assert min(final_sizes) >= 14


def test_html_response_exposes_stable_private_oneclick_identity() -> None:
    class Handler:
        def __init__(self) -> None:
            self.status = 0
            self.headers: dict[str, str] = {}
            self.wfile = BytesIO()

        def send_response(self, status: int) -> None:
            self.status = status

        def send_header(self, name: str, value: str) -> None:
            self.headers[name] = value

        def end_headers(self) -> None:
            return

    handler = Handler()
    gui_server._send_html(handler, "<html>Khôi phục WordPress</html>")  # type: ignore[arg-type]

    assert handler.status == 200
    assert handler.headers["X-OneClick-Recovery"] == "1"
    assert b"WP Clean Rebuild" not in handler.wfile.getvalue()


def test_windows_one_click_launcher_is_committed() -> None:
    root = Path(__file__).resolve().parents[1]
    embedded_entry = root / "oneclick_entry.py"
    source = root / "launcher" / "WPCleanLauncher.cs"
    batch = root / "GIAODIEN.bat"

    assert embedded_entry.is_file()
    assert source.is_file()
    assert batch.is_file()
    embedded_text = embedded_entry.read_text(encoding="utf-8")
    source_text = source.read_text(encoding="utf-8")
    batch_text = batch.read_text(encoding="utf-8-sig")
    assert "webbrowser.open" in embedded_text
    assert "wpclean.gui_runtime_entry" in embedded_text
    assert "GIAODIEN.bat" in source_text
    assert "giaodien.ps1" not in source_text
    assert "cmd.exe" in source_text
    assert 'set "PYTHONUTF8=1"' in batch_text
    assert 'set "PYTHONIOENCODING=utf-8:replace"' in batch_text


def test_gui_terminal_stream_captures_stdout_and_keeps_history() -> None:
    job = gui_server.GuiJob(project="terminal-test")
    mirror = StringIO()
    stream = gui_entry._GuiTerminalStream(job, mirror)

    stream.write("Files discovered: 120\n")
    stream.write("\x1b[31mwarning line\x1b[0m\n")
    stream.flush()

    assert "Files discovered: 120" in mirror.getvalue()
    assert any("Files discovered: 120" in line for line in job.logs)
    assert any("warning line" in line for line in job.logs)
    assert all("\x1b" not in line for line in job.logs)

    for index in range(350):
        job.log(f"line-{index}")
    assert len(job.logs) >= 350
    payload = job.to_dict()
    assert len(payload["logs"]) == 300
    assert "line-349" in payload["logs"][-1]


def test_gui_create_project_writes_local_profile(tmp_path: Path, monkeypatch) -> None:
    _sandbox_gui(tmp_path, monkeypatch)

    project = gui_server.create_project(
        {
            "name": "khach-hang-a",
            "host": "example.test",
            "username": "ftp-user",
            "password": "ftp-pass",
            "protocol": "ftp",
            "port": 21,
            "remotePath": "/domains/example.test/public_html",
            "siteUrl": "https://example.test",
            "workers": 4,
            "blockMb": 1,
            "passive": True,
        }
    )

    profile = tmp_path / "sites" / "khach-hang-a.json"
    assert profile.is_file()
    assert project["name"] == "khach-hang-a"
    assert project["host"] == "example.test"
    assert project["nextStage"] == "backup-files"
    assert project["completed"] is False
    assert project["connection"]["username"] == "ftp-user"
    assert project["connection"]["passwordConfigured"] is True
    assert project["connection"]["password"] == ""


def test_embedded_gui_keeps_password_out_of_profile_and_payload(tmp_path: Path, monkeypatch) -> None:
    _sandbox_gui(tmp_path, monkeypatch)
    secret_dir = tmp_path / "runtime-secrets"
    monkeypatch.setenv("WPCLEAN_EMBEDDED", "1")
    monkeypatch.setenv("WPCLEAN_SECRET_DIR", str(secret_dir))

    project = gui_server.create_project(
        {
            "name": "embedded-site",
            "host": "example.test",
            "username": "ftp-user",
            "password": "runtime-only-secret",
            "remotePath": "/public_html",
        }
    )

    raw_text = (tmp_path / "sites" / "embedded-site.json").read_text(encoding="utf-8")
    assert "runtime-only-secret" not in raw_text
    assert '"password"' not in raw_text
    assert project["connection"]["password"] == ""
    assert project["connection"]["passwordConfigured"] is True
    assert (secret_dir / "embedded-site.secret").read_text(encoding="utf-8") == "runtime-only-secret"
    assert gui_server._profile_and_paths("embedded-site")[1].password == "runtime-only-secret"


def test_gui_can_update_ftp_and_keep_password_when_blank(tmp_path: Path, monkeypatch) -> None:
    _sandbox_gui(tmp_path, monkeypatch)
    gui_server.create_project(
        {
            "name": "edit-me",
            "host": "example.test",
            "username": "old-user",
            "password": "old-pass",
            "protocol": "ftp",
            "port": 21,
            "remotePath": "/public_html",
            "siteUrl": "https://example.test",
        }
    )

    updated = gui_server.create_project(
        {
            "_updateProject": "edit-me",
            "host": "example.test",
            "username": "new-user",
            "password": "",
            "protocol": "ftps",
            "port": 2121,
            "remotePath": "/domains/example.test/public_html",
            "siteUrl": "https://example.test",
            "workers": 6,
            "blockMb": 2,
            "passive": True,
        }
    )

    raw = json.loads((tmp_path / "sites" / "edit-me.json").read_text(encoding="utf-8"))
    assert raw["username"] == "new-user"
    assert raw["password"] == "old-pass"
    assert raw["protocol"] == "ftps"
    assert raw["port"] == 2121
    assert updated["connection"]["username"] == "new-user"
    assert updated["connection"]["port"] == 2121
    assert updated["connection"]["password"] == ""


def test_gui_can_replace_wrong_ftp_password(tmp_path: Path, monkeypatch) -> None:
    _sandbox_gui(tmp_path, monkeypatch)
    gui_server.create_project(
        {
            "name": "wrong-login",
            "host": "example.test",
            "username": "ftp-user",
            "password": "wrong-pass",
        }
    )

    updated = gui_server.create_project(
        {
            "_updateProject": "wrong-login",
            "username": "ftp-user",
            "password": "correct-pass",
        }
    )

    raw = json.loads((tmp_path / "sites" / "wrong-login.json").read_text(encoding="utf-8"))
    assert raw["password"] == "correct-pass"
    assert updated["connection"]["password"] == ""


def test_gui_rejects_duplicate_project(tmp_path: Path, monkeypatch) -> None:
    _sandbox_gui(tmp_path, monkeypatch)
    payload = {
        "name": "same",
        "host": "example.test",
        "username": "u",
        "password": "p",
    }
    gui_server.create_project(payload)

    try:
        gui_server.create_project(payload)
    except FileExistsError:
        pass
    else:
        raise AssertionError("duplicate GUI project should be rejected")


def test_gui_can_delete_incomplete_imported_project_locally(tmp_path: Path, monkeypatch) -> None:
    _sandbox_gui(tmp_path, monkeypatch)
    secret_dir = tmp_path / "runtime-secrets"
    monkeypatch.setenv("WPCLEAN_SECRET_DIR", str(secret_dir))
    gui_server.create_project(
        {
            "name": "old-import",
            "host": "old.example.test",
            "username": "ftp-user",
            "password": "ftp-pass",
        }
    )
    remember_runtime_secret("old-import", "runtime-only-secret")
    assert (secret_dir / "old-import.secret").is_file()
    for root in (tmp_path / "backups", tmp_path / "reports", tmp_path / "repairs"):
        data = root / "old.example.test"
        data.mkdir(parents=True)
        (data / "local-only.txt").write_text("safe to remove", encoding="utf-8")

    legacy_backup = tmp_path / "legacy-source" / "backups" / "old.example.test"
    legacy_backup.mkdir(parents=True)
    legacy_canary = legacy_backup / "must-not-delete.txt"
    legacy_canary.write_text("original legacy data", encoding="utf-8")
    operator_state = tmp_path / "reports" / "old.example.test" / "operator-state.json"
    operator_state.write_text(json.dumps({"backup_root": str(legacy_backup)}), encoding="utf-8")
    profile_path = tmp_path / "sites" / "old-import.json"
    profile_raw = json.loads(profile_path.read_text(encoding="utf-8"))
    profile_raw["localDeletion"] = {
        "status": "error",
        "targets": {
            "backup": str(legacy_backup),
            "reports": str(tmp_path / "reports" / "old.example.test"),
            "repairs": str(tmp_path / "repairs" / "old.example.test"),
        },
        "completedTargets": [],
    }
    profile_path.write_text(json.dumps(profile_raw), encoding="utf-8")

    repaired = gui_server._project_payload("old-import")
    assert repaired["localDeletion"]["status"] == "interrupted"
    assert repaired["localDeletion"]["error"] == ""
    assert repaired["localDeletion"]["targets"]["backup"] == str(tmp_path / "backups" / "old.example.test")
    repaired_operator_state = json.loads(operator_state.read_text(encoding="utf-8"))
    assert repaired_operator_state["backup_root"] == str(tmp_path / "backups" / "old.example.test")
    assert legacy_canary.read_text(encoding="utf-8") == "original legacy data"

    result = gui_server.delete_project_local("old-import", "old-import")

    assert result["status"] == "running"
    profile = tmp_path / "sites" / "old-import.json"
    deadline = time.monotonic() + 5
    while profile.exists() and time.monotonic() < deadline:
        time.sleep(0.02)
    assert not profile.exists()
    assert not (tmp_path / "backups" / "old.example.test").exists()
    assert not (tmp_path / "reports" / "old.example.test").exists()
    assert not (tmp_path / "repairs" / "old.example.test").exists()
    assert not (secret_dir / "old-import.secret").exists()
    assert legacy_canary.read_text(encoding="utf-8") == "original legacy data"


def test_gui_delete_requires_exact_name_and_blocks_active_workflow(tmp_path: Path, monkeypatch) -> None:
    _sandbox_gui(tmp_path, monkeypatch)
    gui_server.create_project(
        {
            "name": "keep-me",
            "host": "keep.example.test",
            "username": "ftp-user",
            "password": "ftp-pass",
        }
    )

    try:
        gui_server.delete_project_local("keep-me", "wrong-name")
    except ValueError as exc:
        assert "không khớp" in str(exc)
    else:
        raise AssertionError("delete must require the exact project name")

    gui_server.JOBS["keep-me"] = gui_server.GuiJob(project="keep-me", status="running")
    try:
        gui_server.delete_project_local("keep-me", "keep-me")
    except RuntimeError as exc:
        assert "đang chạy workflow" in str(exc)
    else:
        raise AssertionError("delete must be blocked while a workflow is running")
    assert (tmp_path / "sites" / "keep-me.json").is_file()


def test_gui_does_not_remove_existing_recovery_commands() -> None:
    runner = CliRunner()
    result = runner.invoke(app, ["--help"])

    assert result.exit_code == 0
    assert "rebuild-resume-db-config" in result.stdout
    assert "rebuild-theme-config" in result.stdout
    assert "rebuild-plugin-config" in result.stdout
    assert "verify-live-config" in result.stdout
