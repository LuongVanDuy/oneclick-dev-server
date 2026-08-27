from pathlib import Path

from wpclean.transport.ftp import FTPConfig, FTPTransport, RemoteFile


def _transport() -> FTPTransport:
    return FTPTransport(
        FTPConfig(
            host="example.test",
            username="user",
            password="pass",
            tls=False,
        )
    )


def test_zero_byte_remote_file_is_created_without_network(tmp_path: Path):
    transport = _transport()

    status, transferred = transport._download_one(
        RemoteFile("/root/empty.txt", 0),
        "/root",
        tmp_path,
        False,
    )

    assert status == "downloaded"
    assert transferred == 0
    assert (tmp_path / "empty.txt").is_file()
    assert (tmp_path / "empty.txt").stat().st_size == 0


def test_zero_byte_resume_skips_existing_empty_file(tmp_path: Path):
    target = tmp_path / "empty.txt"
    target.write_bytes(b"")
    transport = _transport()

    status, transferred = transport._download_one(
        RemoteFile("/root/empty.txt", 0),
        "/root",
        tmp_path,
        True,
    )

    assert status == "skipped"
    assert transferred == 0
    assert target.stat().st_size == 0


def test_zero_byte_download_truncates_stale_local_file(tmp_path: Path):
    target = tmp_path / "empty.txt"
    target.write_bytes(b"stale")
    transport = _transport()

    status, transferred = transport._download_one(
        RemoteFile("/root/empty.txt", 0),
        "/root",
        tmp_path,
        True,
    )

    assert status == "downloaded"
    assert transferred == 0
    assert target.read_bytes() == b""


def test_zero_byte_wrapper_forwards_full_download_tree_signature(tmp_path: Path):
    class Client:
        def __init__(self):
            self.cwd_calls = []
            self.commands = []

        def cwd(self, path):
            self.cwd_calls.append(path)

        def retrbinary(self, command, callback, blocksize=8192, rest=None):
            self.commands.append(command)
            callback(b"abc")

        def close(self):
            pass

    client = Client()
    transport = _transport()
    transport._new_client = lambda: client  # type: ignore[method-assign]

    status, transferred = transport._download_one(
        RemoteFile("/root/file.bin", 3),
        "/root",
        tmp_path,
        False,
        None,
        True,
    )

    assert status == "downloaded"
    assert transferred == 3
    assert client.cwd_calls == ["/root"]
    assert client.commands == ["RETR file.bin"]
    assert (tmp_path / "file.bin").read_bytes() == b"abc"
