from __future__ import annotations

from pathlib import Path
import json
import shutil
import subprocess
import zipfile

import pytest

from wpclean import fresh_install
from wpclean.fresh_ui import render_fresh_app


def _form() -> dict[str, object]:
    return {
        "domain": "demo.kidgrow.site",
        "siteTitle": "Demo Kidgrow",
        "siteUrl": "https://demo.kidgrow.site",
        "host": "ftp.example.test",
        "username": "ftp-user",
        "password": "ftp-secret-2026!",
        "protocol": "ftps",
        "port": 21,
        "workers": 16,
        "remotePath": "/domains/demo.kidgrow.site/public_html",
        "dbHost": "localhost",
        "dbName": "demo_db",
        "dbUser": "demo_user",
        "dbPassword": "database-secret",
        "tablePrefix": "wp_demo_",
        "adminUser": "duyanh",
        "adminEmail": "admin@kidgrow.site",
        "adminPassword": "Admin-password-2026!",
        "clearRemote": True,
    }


def _workspace(monkeypatch, tmp_path: Path) -> None:
    root = tmp_path / "workspace"
    secrets_dir = tmp_path / "runtime-secrets"
    monkeypatch.setenv("WPCLEAN_SECRET_DIR", str(secrets_dir))
    monkeypatch.setattr(fresh_install, "DATA_ROOT", root)
    monkeypatch.setattr(fresh_install, "FRESH_ROOT", root / "fresh-installs")
    monkeypatch.setattr(fresh_install, "PROFILES_DIR", root / "fresh-installs" / "sites")
    monkeypatch.setattr(fresh_install, "HISTORY_FILE", root / "fresh-installs" / "history.jsonl")
    monkeypatch.setattr(fresh_install, "CACHE_DIR", root / ".wpclean-cache" / "fresh-wordpress")
    fresh_install.FRESH_JOBS.clear()


def test_create_install_keeps_all_passwords_out_of_profile(monkeypatch, tmp_path: Path) -> None:
    _workspace(monkeypatch, tmp_path)
    result = fresh_install.create_install(_form())

    profile_path = fresh_install.PROFILES_DIR / "demo.kidgrow.site.json"
    profile_text = profile_path.read_text(encoding="utf-8")
    assert result["name"] == "demo.kidgrow.site"
    assert "ftp-secret-2026!" not in profile_text
    assert "database-secret" not in profile_text
    assert "Admin-password-2026!" not in profile_text
    assert (tmp_path / "runtime-secrets" / "fresh-demo.kidgrow.site.secret").is_file()
    history = fresh_install.HISTORY_FILE.read_text(encoding="utf-8")
    assert "Đã lưu cấu hình cài mới" in history
    assert "secret" not in history.lower()


def test_delete_install_removes_local_profile_and_runtime_secret_only(monkeypatch, tmp_path: Path) -> None:
    _workspace(monkeypatch, tmp_path)
    fresh_install.create_install(_form())

    result = fresh_install.delete_install("demo.kidgrow.site", "demo.kidgrow.site")

    assert result["ok"] is True
    assert "hosting được giữ nguyên" in result["message"]
    assert not (fresh_install.PROFILES_DIR / "demo.kidgrow.site.json").exists()
    assert not (tmp_path / "runtime-secrets" / "fresh-demo.kidgrow.site.secret").exists()


def test_update_install_reuses_vault_passwords_and_keeps_domain(monkeypatch, tmp_path: Path) -> None:
    _workspace(monkeypatch, tmp_path)
    fresh_install.create_install(_form())
    edited = _form()
    edited.update({"password": "", "dbPassword": "", "adminPassword": "", "host": "ftp-new.example.test"})

    result = fresh_install.update_install("demo.kidgrow.site", edited)

    assert result["connection"]["host"] == "ftp-new.example.test"
    profile_text = (fresh_install.PROFILES_DIR / "demo.kidgrow.site.json").read_text(encoding="utf-8")
    assert "ftp-secret-2026!" not in profile_text
    assert result["history"][0]["message"] == "Đã cập nhật cấu hình cài mới"


def test_domain_and_ftp_credentials_supply_safe_defaults(monkeypatch, tmp_path: Path) -> None:
    _workspace(monkeypatch, tmp_path)
    form = _form()
    for field in ("siteTitle", "siteUrl", "host", "dbName", "dbUser", "dbPassword", "adminUser", "adminEmail"):
        form.pop(field)

    result = fresh_install.create_install(form)

    assert result["siteUrl"] == "https://demo.kidgrow.site"
    assert result["connection"]["host"] == "demo.kidgrow.site"
    assert result["connection"]["remotePath"] == "/domains/demo.kidgrow.site/public_html"
    assert result["database"]["name"] == "ftp-user_db"
    assert result["database"]["user"] == "ftp-user_user"
    assert result["admin"]["siteTitle"] == "demo.kidgrow.site"
    assert result["admin"]["username"] == "admin"
    assert result["admin"]["email"] == "admin@demo.kidgrow.site"
    secrets = fresh_install._secrets_for(result["name"])
    assert secrets["dbPassword"] == "ftp-secret-2026!"
    assert secrets["adminPassword"] == "ftp-secret-2026!"


def test_prepare_remote_root_wipes_content_but_not_root(monkeypatch) -> None:
    entries = iter([["cgi-bin", "index.html", ".well-known"], [".well-known"]])
    monkeypatch.setattr(fresh_install, "_remote_entries", lambda _transport, _root: next(entries))
    captured = {}

    def fake_wipe(transport, remote_root, *, progress=None):
        captured.update({"transport": transport, "remoteRoot": remote_root, "progress": progress})
        return 2, 1, [f"{remote_root}/.well-known"]

    monkeypatch.setattr(fresh_install.rebuild_execute, "_wipe_remote_root", fake_wipe)
    transport = object()
    result = fresh_install._prepare_remote_root(
        transport,
        "/domains/demo.kidgrow.site/public_html",
        clear_remote=True,
    )

    assert result[:2] == (2, 1)
    assert captured["transport"] is transport
    assert captured["remoteRoot"] == "/domains/demo.kidgrow.site/public_html"


def test_prepare_remote_root_requires_recorded_confirmation(monkeypatch) -> None:
    monkeypatch.setattr(fresh_install, "_remote_entries", lambda _transport, _root: ["index.html"])
    with pytest.raises(RuntimeError, match="xác nhận xóa"):
        fresh_install._prepare_remote_root(object(), "/domains/demo.kidgrow.site/public_html", clear_remote=False)


def test_profile_rejects_account_root_as_install_directory(monkeypatch, tmp_path: Path) -> None:
    _workspace(monkeypatch, tmp_path)
    form = _form()
    form["remotePath"] = "/"
    with pytest.raises(ValueError, match="FTP root"):
        fresh_install.create_install(form)


def test_probe_workers_returns_maximum_confirmed_value(monkeypatch) -> None:
    class Advice:
        def as_dict(self):
            return {"suggestedWorkers": 16, "confirmedConnections": 16, "probeCap": 16, "stages": []}

    captured = {}

    def fake_advisor(_open_client, **kwargs):
        captured.update(kwargs)
        return Advice()

    monkeypatch.setattr(fresh_install, "advise_ftp_workers", fake_advisor)
    result = fresh_install.probe_install_workers(_form())

    assert result["suggestedWorkers"] == 16
    assert captured["remote_path"] == "/domains/demo.kidgrow.site/public_html"
    assert captured["reserve_connections"] == 0


def _fake_wordpress_core(tmp_path: Path) -> Path:
    core = tmp_path / "wordpress.zip"
    with zipfile.ZipFile(core, "w", zipfile.ZIP_DEFLATED) as archive:
        archive.writestr("wordpress/index.php", "<?php require __DIR__ . '/wp-blog-header.php';")
        archive.writestr("wordpress/wp-admin/index.php", "<?php")
        archive.writestr("wordpress/wp-includes/version.php", "<?php $wp_version = '6.8.2';")
        archive.writestr("wordpress/wp-includes/load.php", "<?php")
        archive.writestr("wordpress/wp-content/themes/index.php", "<?php")
        archive.writestr("wordpress/wp-content/themes/twentytwentyfive/style.css", "default")
        archive.writestr("wordpress/wp-content/plugins/akismet/akismet.php", "<?php")
        archive.writestr("wordpress/wp-content/plugins/hello.php", "<?php")
        archive.writestr("wordpress/license.txt", "license")
        archive.writestr("wordpress/readme.html", "readme")
        archive.writestr("wordpress/wp-config-sample.php", "<?php")
    return core


def test_build_tree_has_final_paths_and_exact_worker_count(monkeypatch, tmp_path: Path) -> None:
    _workspace(monkeypatch, tmp_path)
    core = _fake_wordpress_core(tmp_path)
    monkeypatch.setattr(
        fresh_install, "_wordpress_core_package",
        lambda _progress=None: (core, fresh_install._sha256_file(core), "6.8.2"),
    )

    groups, version = fresh_install.build_install_tree(15)

    assert len(groups) == 15
    assert version == "6.8.2"
    assert all(len(group.control.sha256) == 64 and group.control.path.is_file() for group in groups)
    names = {str(item.relative_path) for group in groups for item in group.files}
    assert sum(len(group.files) for group in groups) == len(names)
    scores = [sum(item.size + 64 * 1024 for item in group.files) for group in groups]
    largest_item = max(item.size + 64 * 1024 for group in groups for item in group.files)
    assert max(scores) - min(scores) <= largest_item
    assert "wp-admin/index.php" in names
    assert "wp-includes/version.php" in names
    assert not any(name.startswith("wordpress/") for name in names)
    assert "wp-content/themes/bricks/style.css" in names
    assert "wp-content/themes/bricks-child/style.css" in names
    assert "wp-content/plugins/duyanhwebpro/duyanhwebpro.php" in names
    assert "wp-content/plugins/hello.php" not in names
    assert "wp-content/plugins/akismet/akismet.php" not in names
    assert "wp-content/themes/twentytwentyfive/style.css" not in names
    assert not any(name.startswith("wp-content/themes/bricks/languages/") and name.endswith(".po") for name in names)
    assert not any(name.startswith("wp-content/themes/bricks/languages/") and name.endswith(".mo") and not name.endswith("/vi.mo") for name in names)


def test_direct_upload_uses_one_persistent_connection_per_worker(tmp_path: Path) -> None:
    uploads: list[tuple[str, bytes]] = []

    class Client:
        def cwd(self, path):
            assert path == "/public_html/.stage"

        def storbinary(self, command, stream, blocksize, callback):
            data = stream.read()
            uploads.append((command, data))
            if data:
                callback(data)

        def quit(self):
            return None

        def close(self):
            return None

    class Config:
        workers = 15
        retries = 1
        block_size = 1024 * 1024

    class Transport:
        config = Config()

        def __init__(self):
            self.clients = []

        def _new_client(self):
            client = Client()
            self.clients.append(client)
            return client

    groups = []
    for index in range(15):
        path = tmp_path / f"file-{index}.txt"
        content = f"worker-{index}".encode()
        path.write_bytes(content)
        item = fresh_install.InstallFile(
            path, fresh_install.PurePosixPath(f"wp-content/file-{index}.txt"),
            fresh_install._sha256_file(path), len(content),
        )
        control = fresh_install.InstallShard(path, item.sha256, 1, item.size)
        groups.append(fresh_install.InstallGroup(index, (item,), control))
    events = []
    transport = Transport()

    fresh_install._upload_install_files(
        transport, "/public_html/.stage", groups,
        lambda *event: events.append(event),
    )

    assert len(transport.clients) == 15
    assert len(uploads) == 15
    assert {command for command, _data in uploads} == {
        f"STOR wp-content/file-{index}.txt" for index in range(15)
    }
    assert any(event[2:6] == (15, 15, 15, 15) for event in events)


def test_installer_bridge_is_authenticated_and_fail_closed() -> None:
    source = fresh_install._installer_bridge(
        "token-value", ".stage", ".oneclick-shard-test-", ["a" * 64] * 15,
    )
    assert "hash_equals(OC_TOKEN" in source
    assert "Thư mục hosting không còn trống" in source
    assert "hash_file('sha256'" in source
    assert "switch_theme('bricks-child')" in source
    assert "activate_plugin('duyanhwebpro/duyanhwebpro.php')" in source
    assert "const OC_SHARD_COUNT = 15;" in source
    assert "stream_copy_to_stream" not in source
    assert "$zip->extractTo($stage)" in source
    assert "$zip->getStream($raw)" not in source
    assert "Bộ cài sau giải nén thiếu WordPress" in source
    assert "$action === 'extract'" in source
    assert "promote-core" not in source
    assert "prepare-resource" not in source
    assert "require_shards" in source
    assert "$action === 'verify'" in source
    assert "verify_upload_group" in source
    assert "prepare_upload_directories" in source
    assert "hash_file('sha256', $target)" in source
    assert "https://wordpress.org/latest.zip" not in source
    assert "wordpress trung gian" in source
    assert "127.0.0.1:2222/CMD_API_DATABASES" in source
    assert "directadmin_create_database" in source
    assert "remove_tree(__DIR__ . DIRECTORY_SEPARATOR . $name)" in source
    assert "@$db->close()" not in source
    assert "normalize_wordpress_permissions($stage)" not in source
    assert "@chmod($target, 0644)" in source
    assert "@chmod($stage . '/wp-config.php', 0600)" in source
    assert "@unlink(__FILE__)" in source


def test_installer_bridge_has_valid_php_syntax(tmp_path: Path) -> None:
    php = shutil.which("php")
    laragon_php = Path(r"D:\laragon\bin\php\php-8.4.4\php.exe")
    if not php and laragon_php.is_file():
        php = str(laragon_php)
    if not php:
        pytest.skip("PHP CLI is not installed")
    bridge = tmp_path / "installer.php"
    bridge.write_text(
        fresh_install._installer_bridge("token", ".stage", ".oneclick-shard-test-", ["a" * 64] * 15),
        encoding="utf-8",
    )
    result = subprocess.run([php, "-l", str(bridge)], capture_output=True, text=True, timeout=15)
    assert result.returncode == 0, result.stdout + result.stderr


def test_fresh_ui_has_separate_site_list_history_and_ftp_form() -> None:
    html = render_fresh_app("local-token")
    assert "Cài mới WordPress" in html
    assert "<h1>Cài mới WordPress</h1>" not in html
    assert "Website mới, theme và plugin tiêu chuẩn." not in html
    assert "Danh sách website" in html
    assert "Lịch sử" in html
    assert "Máy chủ FTP" in html
    assert ">Sửa</button>" in html
    assert "Lưu và thử lại" in html
    assert "/api/fresh-installs" in html
    assert "/api/fresh-installs/probe-workers" in html
    assert "Xóa khỏi OneClick" in html
    assert "Đo tối đa" in html
    assert "admin@'+d" in html
    assert "data.clearRemote=q('#clearRemote').checked" in html
    assert "font:14px" in html
    assert ".btn{height:36px" in html
    assert "Mật khẩu FTP & quản trị" in html
    assert "database từ chối" in html
    assert "if(detailName)renderDetail()" in html
    assert "poll=setTimeout(refresh,1200)" in html
    assert "aria-live=\"polite\"" in html
