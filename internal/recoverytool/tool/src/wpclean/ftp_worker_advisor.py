from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from threading import Condition, Event
from time import monotonic
from typing import Callable, Protocol, Sequence


class FTPProbeClient(Protocol):
    """Small part of ftplib.FTP used by the connection-capacity probe."""

    def cwd(self, dirname: str) -> str: ...

    def voidcmd(self, cmd: str) -> str: ...

    def quit(self) -> str: ...

    def close(self) -> None: ...


OpenClient = Callable[[float], FTPProbeClient]
ProgressCallback = Callable[[dict], None]


@dataclass(frozen=True, slots=True)
class FTPProbeStage:
    requested: int
    connected: int
    failed: int
    elapsed_seconds: float
    errors: tuple[str, ...] = ()
    attempt: int = 1


@dataclass(frozen=True, slots=True)
class FTPWorkerAdvice:
    suggested_workers: int
    confirmed_connections: int
    probe_cap: int
    reached_server_limit: bool
    exact_limit_known: bool
    reason: str
    stages: tuple[FTPProbeStage, ...]

    def as_dict(self) -> dict:
        return {
            "suggestedWorkers": self.suggested_workers,
            "confirmedConnections": self.confirmed_connections,
            "probeCap": self.probe_cap,
            "reachedServerLimit": self.reached_server_limit,
            "exactLimitKnown": self.exact_limit_known,
            "reason": self.reason,
            "stages": [
                {
                    "requested": stage.requested,
                    "connected": stage.connected,
                    "failed": stage.failed,
                    "elapsedSeconds": round(stage.elapsed_seconds, 3),
                    "errors": list(stage.errors),
                    "attempt": stage.attempt,
                }
                for stage in self.stages
            ],
        }


class FTPWorkerProbeError(RuntimeError):
    """The baseline FTP connection failed, so no worker advice is possible."""


_CAPACITY_ERROR_MARKERS = (
    "421",
    "too many connection",
    "too many user",
    "maximum connection",
    "max connection",
    "connection limit",
    "connections per ip",
    "connections from this ip",
)


def _close_client(client: FTPProbeClient | None) -> None:
    if client is None:
        return
    try:
        client.quit()
    except Exception:
        try:
            client.close()
        except Exception:
            pass


def _error_text(exc: Exception) -> str:
    text = f"{type(exc).__name__}: {exc}".replace("\r", " ").replace("\n", " ")
    return text[:240]


def _is_explicit_capacity_error(errors: Sequence[str]) -> bool:
    joined = " ".join(errors).lower()
    return any(marker in joined for marker in _CAPACITY_ERROR_MARKERS)


def _candidate_levels(probe_cap: int) -> tuple[int, ...]:
    standard = (1, 2, 4, 6, 8, 12, 16)
    levels = [level for level in standard if level <= probe_cap]
    if not levels or levels[-1] != probe_cap:
        levels.append(probe_cap)
    return tuple(dict.fromkeys(levels))


def _probe_stage(
    open_client: OpenClient,
    *,
    requested: int,
    remote_path: str,
    timeout: float,
    attempt: int,
) -> FTPProbeStage:
    """Hold successful sessions open until every peer attempted a login."""

    release = Event()
    condition = Condition()
    outcomes: list[tuple[bool, str] | None] = [None] * requested
    attempted = 0
    started = monotonic()

    def probe_one(index: int) -> None:
        nonlocal attempted
        client: FTPProbeClient | None = None
        success = False
        error = ""
        try:
            client = open_client(timeout)
            if remote_path:
                client.cwd(remote_path)
            client.voidcmd("NOOP")
            success = True
        except Exception as exc:
            error = _error_text(exc)
        finally:
            with condition:
                outcomes[index] = (success, error)
                attempted += 1
                condition.notify_all()

        if success:
            release.wait(timeout=max(1.0, timeout))
        _close_client(client)

    executor = ThreadPoolExecutor(max_workers=requested, thread_name_prefix="ftp-capacity")
    futures = [executor.submit(probe_one, index) for index in range(requested)]
    deadline = monotonic() + timeout + 1.0
    try:
        with condition:
            while attempted < requested:
                remaining = deadline - monotonic()
                if remaining <= 0:
                    break
                condition.wait(timeout=remaining)
    finally:
        release.set()
        executor.shutdown(wait=True, cancel_futures=True)

    # Retrieve worker exceptions so ThreadPoolExecutor does not hide programming
    # errors while still representing network failures through ``outcomes``.
    for future in futures:
        future.result()

    for index, outcome in enumerate(outcomes):
        if outcome is None:
            outcomes[index] = (False, f"TimeoutError: probe exceeded {timeout:.1f}s")

    connected = sum(1 for outcome in outcomes if outcome and outcome[0])
    errors = tuple(
        dict.fromkeys(outcome[1] for outcome in outcomes if outcome and not outcome[0] and outcome[1])
    )
    return FTPProbeStage(
        requested=requested,
        connected=connected,
        failed=requested - connected,
        elapsed_seconds=monotonic() - started,
        errors=errors,
        attempt=attempt,
    )


def advise_ftp_workers(
    open_client: OpenClient,
    *,
    remote_path: str,
    probe_cap: int = 16,
    timeout: float = 7.0,
    reserve_connections: int = 1,
    retry_unclear_failure: bool = True,
    progress: ProgressCallback | None = None,
) -> FTPWorkerAdvice:
    """Measure concurrent logins and return a conservative FTP worker count.

    The probe never transfers or mutates remote files. It authenticates, changes
    to ``remote_path``, sends ``NOOP`` and closes every session. A non-specific
    failed stage is retried once; explicit capacity responses such as FTP 421
    stop immediately.
    """

    probe_cap = max(1, min(int(probe_cap), 32))
    timeout = max(2.0, min(float(timeout), 30.0))
    reserve_connections = max(0, int(reserve_connections))

    stages: list[FTPProbeStage] = []
    confirmed = 0
    reached_limit = False
    failure_stage: FTPProbeStage | None = None

    for requested in _candidate_levels(probe_cap):
        stage = _probe_stage(
            open_client,
            requested=requested,
            remote_path=remote_path,
            timeout=timeout,
            attempt=1,
        )
        stages.append(stage)
        if progress:
            progress(
                {
                    "phase": "ftp_worker_probe",
                    "requested": requested,
                    "connected": stage.connected,
                    "failed": stage.failed,
                    "probe_cap": probe_cap,
                    "attempt": stage.attempt,
                }
            )

        if stage.connected == requested:
            confirmed = max(confirmed, requested)
            continue

        failure_stage = stage
        confirmed = max(confirmed, stage.connected)
        explicit_limit = _is_explicit_capacity_error(stage.errors)

        if retry_unclear_failure and not explicit_limit:
            retry = _probe_stage(
                open_client,
                requested=requested,
                remote_path=remote_path,
                timeout=timeout,
                attempt=2,
            )
            stages.append(retry)
            if progress:
                progress(
                    {
                        "phase": "ftp_worker_probe",
                        "requested": requested,
                        "connected": retry.connected,
                        "failed": retry.failed,
                        "probe_cap": probe_cap,
                        "attempt": retry.attempt,
                    }
                )
            if retry.connected == requested:
                confirmed = max(confirmed, requested)
                failure_stage = None
                continue
            confirmed = max(confirmed, retry.connected)
            failure_stage = retry

        if requested == 1 and confirmed == 0:
            detail = (
                failure_stage.errors[0]
                if failure_stage and failure_stage.errors
                else "unknown FTP connection error"
            )
            raise FTPWorkerProbeError(f"FTP baseline connection failed: {detail}")

        reached_limit = True
        break

    if reached_limit:
        reserve = min(reserve_connections, max(0, confirmed - 1))
        suggested = max(1, confirmed - reserve)
        refused_at = failure_stage.requested if failure_stage else confirmed + 1
        reason = (
            f"Hosting giữ ổn định {confirmed} kết nối FTP đồng thời nhưng không nhận đủ "
            f"{refused_at}; hệ thống chừa {reserve} kết nối dự phòng."
        )
    else:
        suggested = max(1, confirmed)
        reason = (
            f"Đã xác nhận ổn định ít nhất {confirmed} kết nối FTP đồng thời. "
            "Đây là trần đo của ứng dụng, không phải giới hạn tuyệt đối của hosting."
        )

    advice = FTPWorkerAdvice(
        suggested_workers=suggested,
        confirmed_connections=confirmed,
        probe_cap=probe_cap,
        reached_server_limit=reached_limit,
        # A refused stage proves the safe lower bound, not the hosting's exact
        # account/IP ceiling (other sessions may exist outside this process).
        exact_limit_known=False,
        reason=reason,
        stages=tuple(stages),
    )
    if progress:
        progress({"phase": "ftp_worker_advice", **advice.as_dict()})
    return advice


__all__ = [
    "FTPProbeStage",
    "FTPWorkerAdvice",
    "FTPWorkerProbeError",
    "advise_ftp_workers",
]
