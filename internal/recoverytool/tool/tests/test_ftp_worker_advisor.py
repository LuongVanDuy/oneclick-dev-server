from __future__ import annotations

from ftplib import error_temp
import threading
import time

import pytest

from wpclean.ftp_worker_advisor import FTPWorkerProbeError, advise_ftp_workers


class FakeFTPClient:
    def __init__(self, server: "FakeFTPServer") -> None:
        self.server = server
        self.closed = False

    def cwd(self, dirname: str) -> str:
        assert dirname == "/domains/example.test/public_html"
        return "250 cwd ok"

    def voidcmd(self, cmd: str) -> str:
        assert cmd == "NOOP"
        return "200 noop ok"

    def quit(self) -> str:
        self.close()
        return "221 bye"

    def close(self) -> None:
        if not self.closed:
            self.closed = True
            self.server.release()


class FakeFTPServer:
    def __init__(self, limit: int, *, one_transient_failure: bool = False) -> None:
        self.limit = limit
        self.active = 0
        self.peak = 0
        self.lock = threading.Lock()
        self.one_transient_failure = one_transient_failure

    def open_client(self, timeout: float) -> FakeFTPClient:
        assert timeout >= 2
        with self.lock:
            if self.one_transient_failure:
                self.one_transient_failure = False
                raise TimeoutError("temporary login timeout")
            if self.active >= self.limit:
                raise error_temp("421 Too many connections from this IP")
            self.active += 1
            self.peak = max(self.peak, self.active)
        time.sleep(0.005)
        return FakeFTPClient(self)

    def release(self) -> None:
        with self.lock:
            self.active -= 1


REMOTE = "/domains/example.test/public_html"


def test_advice_stops_at_server_limit_and_keeps_one_spare() -> None:
    server = FakeFTPServer(limit=6)

    result = advise_ftp_workers(
        server.open_client,
        remote_path=REMOTE,
        probe_cap=16,
        timeout=2,
    )

    assert result.confirmed_connections == 6
    assert result.suggested_workers == 5
    assert result.reached_server_limit is True
    assert server.peak == 6
    assert server.active == 0
    assert result.stages[-1].requested == 8


def test_passing_probe_cap_is_reported_as_a_lower_bound() -> None:
    server = FakeFTPServer(limit=50)

    result = advise_ftp_workers(
        server.open_client,
        remote_path=REMOTE,
        probe_cap=8,
        timeout=2,
    )

    assert result.confirmed_connections == 8
    assert result.suggested_workers == 8
    assert result.reached_server_limit is False
    assert result.exact_limit_known is False
    assert server.active == 0


def test_unclear_failure_is_retried_once() -> None:
    server = FakeFTPServer(limit=8, one_transient_failure=True)

    result = advise_ftp_workers(
        server.open_client,
        remote_path=REMOTE,
        probe_cap=4,
        timeout=2,
    )

    assert result.suggested_workers == 4
    assert any(stage.attempt == 2 for stage in result.stages)
    assert server.active == 0


def test_baseline_failure_has_a_specific_error() -> None:
    server = FakeFTPServer(limit=0)

    with pytest.raises(FTPWorkerProbeError, match="421 Too many connections"):
        advise_ftp_workers(
            server.open_client,
            remote_path=REMOTE,
            probe_cap=4,
            timeout=2,
        )

    assert server.active == 0
