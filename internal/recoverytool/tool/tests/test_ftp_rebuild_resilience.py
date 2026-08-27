from __future__ import annotations

from pathlib import Path
from types import SimpleNamespace
import time

import pytest

from wpclean import rebuild_execute
from wpclean.ftp_rebuild_resilience import _is_transient_ftp_error, wipe_remote_root_with_reconnect


class ResetState:
    def __init__(self) -> None:
        self.deleted = False
        self.uploaded: dict[str, bytes] = {}
        self.reset_delete_once = True
        self.reset_upload_once = True
        self.delete_before_reset = False
        self.delete_calls: list[str] = []


class ResetClient:
    def __init__(self, state: ResetState) -> None:
        self.state = state
        self.closed = False

    def delete(self, path: str):
        self.state.delete_calls.append(path)
        if self.state.reset_delete_once:
            self.state.reset_delete_once = False
            if self.state.delete_before_reset:
                self.state.deleted = True
            raise ConnectionResetError(10054, "An existing connection was forcibly closed by the remote host")
        self.state.deleted = True
        return "250 deleted"

    def storbinary(self, command: str, fh, blocksize: int = 8192):
        if self.state.reset_upload_once:
            self.state.reset_upload_once = False
            raise ConnectionResetError(10054, "An existing connection was forcibly closed by the remote host")
        remote_path = command.removeprefix("STOR ")
        self.state.uploaded[remote_path] = fh.read()
        return "226 transfer complete"

    def cwd(self, path: str):
        return "250 cwd"

    def pwd(self):
        return "/"

    def mkd(self, path: str):
        return path

    def rmd(self, path: str):
        return "250 removed"

    def sendcmd(self, command: str):
        return "200 ok"

    def quit(self):
        self.closed = True
        return "221 bye"

    def close(self):
        self.closed = True


class ResetTransport:
    def __init__(self, state: ResetState) -> None:
        self.state = state
        self.config = SimpleNamespace(retries=2, workers=1, block_size=1024)
        self.clients: list[ResetClient] = []

    def _new_client(self):
        client = ResetClient(self.state)
        self.clients.append(client)
        return client

    def _mlsd(self, client, path: str):
        if path == "/public_html" and not self.state.deleted:
            return iter([("index.php", {"type": "file", "size": "5"})])
        return iter([])


def test_wipe_reconnects_after_winerror_10054_and_continues() -> None:
    state = ResetState()
    state.reset_upload_once = False
    transport = ResetTransport(state)
    events: list[dict] = []

    files, dirs, preserved = wipe_remote_root_with_reconnect(
        transport,  # type: ignore[arg-type]
        "/public_html",
        progress=events.append,
    )

    assert files == 1
    assert dirs == 0
    assert preserved == []
    assert state.deleted is True
    assert state.delete_calls == ["index.php", "index.php"]
    assert len(transport.clients) >= 2
    assert any(event.get("phase") == "ftp_reconnect" for event in events)
    assert not any(event.get("phase") == "wipe_inventory_complete" for event in events)
    wipe = next(event for event in events if event.get("phase") == "wipe" and event.get("items_completed") == 1)
    assert wipe["items_completed"] == 1
    assert wipe["items_total"] == 1
    assert wipe["unit"] == "mục"


def test_rebuild_upload_reconnects_and_restarts_file_from_beginning(tmp_path: Path) -> None:
    state = ResetState()
    state.reset_delete_once = False
    transport = ResetTransport(state)
    local = tmp_path / "core"
    local.mkdir()
    payload = b"<?php echo 'ok';\n"
    (local / "index.php").write_bytes(payload)
    events: list[dict] = []

    uploaded = rebuild_execute._upload_tree(
        transport,  # type: ignore[arg-type]
        local,
        "/public_html",
        progress_phase="upload_core",
        progress=events.append,
    )

    assert uploaded == 1
    assert state.uploaded["/public_html/index.php"] == payload
    assert len(transport.clients) >= 2
    assert any(event.get("phase") == "ftp_reconnect" for event in events)
    assert any(event.get("phase") == "upload_core" for event in events)
    assert events[-1]["workers"] == transport.config.workers
    assert events[-1]["discovery_complete"] is True


def test_rebuild_upload_starts_before_local_inventory_finishes(tmp_path: Path, monkeypatch) -> None:
    state = ResetState()
    state.reset_delete_once = False
    state.reset_upload_once = False
    transport = ResetTransport(state)
    local = tmp_path / "core"
    local.mkdir()
    first = local / "first.php"
    second = local / "second.php"
    first.write_bytes(b"first")
    second.write_bytes(b"second")

    original_rglob = Path.rglob

    def delayed_inventory(path: Path, pattern: str):
        if path != local:
            yield from original_rglob(path, pattern)
            return
        yield first
        deadline = time.monotonic() + 2
        while "/public_html/first.php" not in state.uploaded and time.monotonic() < deadline:
            time.sleep(0.01)
        assert "/public_html/first.php" in state.uploaded, "upload must start before inventory yields the next file"
        yield second

    monkeypatch.setattr(Path, "rglob", delayed_inventory)

    uploaded = rebuild_execute._upload_tree(
        transport,  # type: ignore[arg-type]
        local,
        "/public_html",
        progress_phase="upload_core",
    )

    assert uploaded == 2
    assert set(state.uploaded) == {"/public_html/first.php", "/public_html/second.php"}


class DeleteWhileWalkingTransport(ResetTransport):
    def _mlsd(self, client, path: str):
        if path == "/public_html":
            if self.state.deleted:
                return iter([])
            return iter(
                [
                    ("index.php", {"type": "file", "size": "5"}),
                    ("wp-content", {"type": "dir"}),
                ]
            )
        if path == "/public_html/wp-content":
            assert self.state.deleted is True, "wipe must delete discovered files before scanning the next directory"
        return iter([])


def test_wipe_deletes_while_walking_instead_of_inventorying_the_whole_tree() -> None:
    state = ResetState()
    state.reset_delete_once = False
    transport = DeleteWhileWalkingTransport(state)

    files, dirs, preserved = wipe_remote_root_with_reconnect(
        transport,  # type: ignore[arg-type]
        "/public_html",
    )

    assert (files, dirs, preserved) == (1, 1, [])


def test_connection_reset_is_transient_but_login_error_is_not() -> None:
    assert _is_transient_ftp_error(
        ConnectionResetError(10054, "An existing connection was forcibly closed by the remote host")
    )
    assert not _is_transient_ftp_error(RuntimeError("530 Login authentication failed"))


def test_wipe_verifies_ambiguous_delete_before_sending_it_again() -> None:
    state = ResetState()
    state.reset_upload_once = False
    state.delete_before_reset = True
    transport = ResetTransport(state)

    files, dirs, preserved = wipe_remote_root_with_reconnect(
        transport,  # type: ignore[arg-type]
        "/public_html",
    )

    assert (files, dirs, preserved) == (1, 0, [])
    assert state.delete_calls == ["index.php"]


class UnsafeListingTransport(ResetTransport):
    def _mlsd(self, client, path: str):
        if path == "/public_html":
            return iter([("../outside.php", {"type": "file", "size": "5"})])
        return iter([])


def test_wipe_rejects_unsafe_names_returned_by_ftp_server() -> None:
    state = ResetState()
    state.reset_delete_once = False
    transport = UnsafeListingTransport(state)

    with pytest.raises(RuntimeError, match="không an toàn"):
        wipe_remote_root_with_reconnect(
            transport,  # type: ignore[arg-type]
            "/public_html",
        )

    assert state.delete_calls == []


class AccountRootTransport(ResetTransport):
    def _mlsd(self, client, path: str):
        if path == "/" and not self.state.deleted:
            return iter([("index.php", {"type": "file", "size": "5"})])
        return iter([])


def test_wipe_refuses_ftp_account_root() -> None:
    state = ResetState()
    transport = ResetTransport(state)

    with pytest.raises(RuntimeError, match="Từ chối wipe FTP root"):
        wipe_remote_root_with_reconnect(
            transport,  # type: ignore[arg-type]
            "/",
        )

    assert transport.clients == []


def test_wipe_allows_explicit_jailed_ftp_account_root() -> None:
    state = ResetState()
    state.reset_delete_once = False
    state.reset_upload_once = False
    transport = AccountRootTransport(state)

    files, dirs, preserved = wipe_remote_root_with_reconnect(
        transport,  # type: ignore[arg-type]
        "/",
        allow_account_root=True,
    )

    assert (files, dirs, preserved) == (1, 0, [])
    assert state.deleted is True
    assert state.delete_calls == ["index.php"]
