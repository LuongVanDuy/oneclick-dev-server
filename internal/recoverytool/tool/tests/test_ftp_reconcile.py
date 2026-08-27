from pathlib import Path

from wpclean.transport.ftp import FTPConfig, FTPTransport, RemoteFile


def test_download_tree_reconciles_a_transiently_failed_file(tmp_path: Path, monkeypatch) -> None:
    transport = FTPTransport(FTPConfig("example.test", "user", "pass", workers=2, retries=0))
    remote = RemoteFile("/public_html/uploads/image.jpg", 5)
    attempts = 0

    monkeypatch.setattr(transport, "iter_files_recursive", lambda *_args, **_kwargs: iter([remote]))

    def fake_download(*_args, **_kwargs):
        nonlocal attempts
        attempts += 1
        if attempts == 1:
            raise ConnectionResetError("temporary reset")
        return "downloaded", 5

    monkeypatch.setattr(transport, "_download_one", fake_download)

    stats = transport.download_tree("/public_html", tmp_path / "backup")

    assert attempts == 2
    assert stats.files_failed == 0
    assert stats.files_downloaded == 1
    assert stats.bytes_downloaded == 5
