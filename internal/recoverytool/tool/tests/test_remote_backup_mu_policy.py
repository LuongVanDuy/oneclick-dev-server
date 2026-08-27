from pathlib import Path

from wpclean.remote_backup import RemoteBackupReport, _backup_allowed_mu_plugins
from wpclean.transport import TransferStats


class _Client:
    def quit(self):
        return None

    def close(self):
        return None


class _Transport:
    def __init__(self):
        self.downloaded: list[str] = []

    def _new_client(self):
        return _Client()

    def _mlsd(self, _client, _remote):
        return iter(
            [
                ("flatsome-custom-assets", {"type": "dir"}),
                ("flatsome-custom-loader.php", {"type": "file"}),
            ]
        )

    def download_tree(self, remote, local: Path, **_kwargs):
        self.downloaded.append(remote)
        local.mkdir(parents=True, exist_ok=True)
        (local / "asset.css").write_text("body{}", encoding="utf-8")
        return TransferStats(1, 1, 0, 6, 0.01)

    def download_file(self, remote, local: Path, **_kwargs):
        self.downloaded.append(remote)
        local.parent.mkdir(parents=True, exist_ok=True)
        local.write_text("<?php", encoding="utf-8")
        return TransferStats(1, 1, 0, 5, 0.01)


def test_mu_backup_keeps_only_allowlisted_components(tmp_path: Path, monkeypatch):
    backup = tmp_path / "backup"
    stale = backup / "mu-plugins" / "unknown.php"
    stale.parent.mkdir(parents=True)
    stale.write_text("<?php // stale", encoding="utf-8")
    transport = _Transport()
    report = RemoteBackupReport("ftp", "/public_html", str(backup))

    monkeypatch.setattr(
        "wpclean.remote_backup.prune_remote_directory_allowlist",
        lambda *_args, **_kwargs: ["/public_html/wp-content/mu-plugins/unknown.php"],
    )

    _backup_allowed_mu_plugins(
        transport,
        "/public_html",
        backup,
        report,
        resume=True,
        progress=None,
    )

    assert not stale.exists()
    assert transport.downloaded == [
        "/public_html/wp-content/mu-plugins/flatsome-custom-assets",
        "/public_html/wp-content/mu-plugins/flatsome-custom-loader.php",
    ]
    assert report.removed_remote_mu_plugins == [
        "/public_html/wp-content/mu-plugins/unknown.php"
    ]
