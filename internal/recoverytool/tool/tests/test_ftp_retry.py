from pathlib import Path
from ftplib import error_perm
from threading import Event

from wpclean.transport.ftp import FTPConfig, FTPTransport, RemoteFile


class ResetOnceClient:
    def __init__(self):
        self.closed = False
        self.cwd_calls = []

    def cwd(self, path):
        self.cwd_calls.append(path)

    def retrbinary(self, command, callback, blocksize=8192, rest=None):
        assert command == "RETR file.bin"
        assert rest is None
        callback(b"abc")
        raise ConnectionResetError(10054, "remote reset")

    def close(self):
        self.closed = True


class ResumeClient:
    def __init__(self):
        self.closed = False
        self.cwd_calls = []

    def cwd(self, path):
        self.cwd_calls.append(path)

    def retrbinary(self, command, callback, blocksize=8192, rest=None):
        assert command == "RETR file.bin"
        assert rest == 3
        callback(b"def")

    def close(self):
        self.closed = True


def test_download_reconnects_and_resumes_after_connection_reset(tmp_path: Path):
    transport = FTPTransport(
        FTPConfig(
            host="example.test",
            username="user",
            password="pass",
            tls=False,
            retries=2,
        )
    )
    clients = [ResetOnceClient(), ResumeClient()]
    transport._new_client = lambda: clients.pop(0)  # type: ignore[method-assign]

    events = []
    status, transferred = transport._download_one(
        RemoteFile("/root/file.bin", 6),
        "/root",
        tmp_path,
        True,
        events.append,
    )

    assert status == "downloaded"
    assert transferred == 6
    assert (tmp_path / "file.bin").read_bytes() == b"abcdef"
    assert len(events) == 1
    assert events[0]["phase"] == "retry"
    assert events[0]["current_file"] == "/root/file.bin"
    assert events[0]["attempt"] == 2
    assert events[0]["resume_offset"] == 3
    assert clients == []


def test_download_enters_remote_root_and_uses_relative_retr_path(tmp_path: Path):
    class RelativeOnlyClient:
        def __init__(self):
            self.cwd_calls = []
            self.commands = []

        def cwd(self, path):
            self.cwd_calls.append(path)

        def retrbinary(self, command, callback, blocksize=8192, rest=None):
            self.commands.append(command)
            if command.startswith("RETR /"):
                raise error_perm("550 Absolute RETR is not allowed")
            callback(b"image")

        def close(self):
            pass

    client = RelativeOnlyClient()
    transport = FTPTransport(FTPConfig("example.test", "user", "pass", tls=False, retries=0))
    transport._new_client = lambda: client  # type: ignore[method-assign]

    status, transferred = transport._download_one(
        RemoteFile("/public_html/wp-content/uploads/2025/image.jpg", 5),
        "/public_html/wp-content/uploads",
        tmp_path,
        False,
    )

    assert status == "downloaded"
    assert transferred == 5
    assert client.cwd_calls == ["/public_html/wp-content/uploads"]
    assert client.commands == ["RETR 2025/image.jpg"]
    assert (tmp_path / "2025" / "image.jpg").read_bytes() == b"image"


def test_download_falls_back_to_absolute_retr_for_legacy_server(tmp_path: Path):
    class AbsoluteOnlyClient:
        def __init__(self):
            self.commands = []

        def cwd(self, _path):
            pass

        def retrbinary(self, command, callback, blocksize=8192, rest=None):
            self.commands.append(command)
            if command == "RETR file.bin":
                raise error_perm("550 Relative RETR is not allowed")
            callback(b"abc")

        def close(self):
            pass

    client = AbsoluteOnlyClient()
    transport = FTPTransport(FTPConfig("example.test", "user", "pass", tls=False, retries=0))
    transport._new_client = lambda: client  # type: ignore[method-assign]
    events = []

    status, transferred = transport._download_one(
        RemoteFile("/root/file.bin", 3),
        "/root",
        tmp_path,
        False,
        events.append,
    )

    assert status == "downloaded"
    assert transferred == 3
    assert client.commands == ["RETR file.bin", "RETR /root/file.bin"]
    assert events[0]["phase"] == "ftp_download_path_fallback"
    assert "550 Relative RETR is not allowed" in events[0]["error"]


def test_download_failure_names_file_after_retry_budget_and_removes_partial(tmp_path: Path):
    class AlwaysResetClient:
        def cwd(self, _path):
            pass

        def retrbinary(self, command, callback, blocksize=8192, rest=None):
            callback(b"partial")
            raise ConnectionResetError(10054, "remote reset")

        def close(self):
            pass

    transport = FTPTransport(
        FTPConfig(
            host="example.test",
            username="user",
            password="pass",
            tls=False,
            retries=1,
        )
    )
    transport._new_client = lambda: AlwaysResetClient()  # type: ignore[method-assign]

    try:
        transport._download_one(
            RemoteFile("/root/problem.bin", 100),
            "/root",
            tmp_path,
            True,
        )
    except RuntimeError as exc:
        message = str(exc)
    else:
        raise AssertionError("Expected RuntimeError")

    assert "after 2 attempts" in message
    assert "/root/problem.bin" in message
    assert "ConnectionResetError" in message
    assert not (tmp_path / "problem.bin").exists()


def test_download_tree_records_failed_file_and_continues_other_files(tmp_path: Path):
    transport = FTPTransport(
        FTPConfig(
            host="example.test",
            username="user",
            password="pass",
            tls=False,
            workers=2,
        )
    )
    files = [
        RemoteFile("/root/bad.php", 10),
        RemoteFile("/root/good.php", 10),
    ]
    transport.iter_files_recursive = lambda remote_root, progress=None: iter(files)  # type: ignore[method-assign]

    def fake_download(
        item,
        remote_root,
        local_root,
        resume,
        progress=None,
        preserve_partial_on_failure=False,
    ):
        if item.path.endswith("bad.php"):
            raise RuntimeError("persistent FTP failure")
        return "downloaded", 10

    transport._download_one = fake_download  # type: ignore[method-assign]
    events = []
    stats = transport.download_tree("/root", tmp_path, progress=events.append)

    assert stats.files_total == 2
    assert stats.files_downloaded == 1
    assert stats.files_failed == 1
    assert stats.failures[0].path == "/root/bad.php"
    assert "persistent FTP failure" in stats.failures[0].error
    assert any(event.get("phase") == "file_failed" for event in events)
    assert any(event.get("phase") == "complete" for event in events)


def test_download_tree_starts_transfers_before_inventory_finishes(tmp_path: Path):
    transport = FTPTransport(
        FTPConfig(
            host="example.test",
            username="user",
            password="pass",
            tls=False,
            workers=1,
        )
    )
    first_downloaded = Event()
    inventory_finished = Event()

    def streaming_inventory(_remote_root, progress=None):
        yield RemoteFile("/root/first.bin", 3)
        assert first_downloaded.wait(1.0), "first file was not downloaded while inventory was active"
        yield RemoteFile("/root/second.bin", 3)
        inventory_finished.set()
        if progress:
            progress({
                "phase": "discovered",
                "remote_root": "/root",
                "dirs_scanned": 1,
                "files_found": 2,
                "bytes_total": 6,
            })

    def fake_download(
        item,
        remote_root,
        local_root,
        resume,
        progress=None,
        preserve_partial_on_failure=False,
    ):
        if item.path.endswith("first.bin"):
            assert not inventory_finished.is_set()
            first_downloaded.set()
        return "downloaded", 3

    transport.iter_files_recursive = streaming_inventory  # type: ignore[method-assign]
    transport._download_one = fake_download  # type: ignore[method-assign]
    events = []

    stats = transport.download_tree("/root", tmp_path, progress=events.append)

    assert stats.files_total == 2
    assert stats.files_downloaded == 2
    assert stats.bytes_downloaded == 6
    streaming = [event for event in events if event.get("phase") == "discover_transfer"]
    assert streaming
    assert any(event.get("discovery_complete") is False for event in streaming)
    assert any(event.get("files_completed", 0) >= 1 for event in streaming)
