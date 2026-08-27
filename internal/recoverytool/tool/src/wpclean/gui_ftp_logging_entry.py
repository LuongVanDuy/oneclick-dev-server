from __future__ import annotations

from datetime import datetime
import threading

from . import gui_journal_entry  # noqa: F401 - activate normal GUI + persistent project journal
from .ftp_worker_advisor import advise_ftp_workers
from .gui_observability import OperationError, classify_exception
from . import gui_server as server
from . import gui_ui
from .transport.ftp import FTPConfig, FTPTransport


_BASE_RENDER = gui_ui.render_app
_BASE_TEST_CONNECTION = server.test_connection
_BASE_PROJECT_PAYLOAD = server._project_payload
_FTP_TEST_LOCK = threading.Lock()
_FTP_TEST_ACTIVE: set[str] = set()
_FTP_WORKER_PROBE_CAP = 16
_FTP_WORKER_PROBE_TIMEOUT = 5.0


_FTP_TEST_CSS = r'''<style id="wpclean-ftp-worker-advice-style">
.ftp-worker-advice{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:12px;align-items:center;margin-top:9px;padding:10px 12px;border:1px solid #295a3c;border-radius:6px;background:#0d2118}
.ftp-worker-advice-main{min-width:0}.ftp-worker-advice-title{display:flex;align-items:center;gap:8px;color:#86efac;font:600 12px/1.35 var(--mono,Consolas,monospace)}.ftp-worker-advice-value{display:inline-flex;align-items:center;min-height:23px;padding:2px 7px;border:1px solid #327149;border-radius:4px;background:#12301f;color:#bbf7d0;font-variant-numeric:tabular-nums}
.ftp-worker-advice p{margin:5px 0 0;color:#9fb5a8;font:11px/1.5 var(--mono,Consolas,monospace)}.ftp-worker-advice .btn{white-space:nowrap}
@media(max-width:720px){.ftp-worker-advice{grid-template-columns:1fr}.ftp-worker-advice .btn{width:100%}}
</style>'''


_FTP_TEST_JS = r'''
const ftpTestsInFlight=new Set();
const connectionHtmlBeforeFtpWorkerAdvice=connectionHtml;
connectionHtml=function(p){
  const base=connectionHtmlBeforeFtpWorkerAdvice(p),a=p.ftpWorkerAdvice||null;
  if(!a||!a.available)return base;
  const suggested=Math.max(1,Number(a.suggestedWorkers||1));
  const confirmed=Math.max(suggested,Number(a.confirmedConnections||suggested));
  const current=Math.max(1,Number((p.connection||{}).workers||p.workers||1));
  const bound=a.exactLimitKnown?`${confirmed} kết nối đồng thời`:`ít nhất ${confirmed} kết nối đồng thời`;
  const button=suggested===current
    ?`<button class="btn btn-success" type="button" disabled>Đang dùng ${suggested} Workers</button>`
    :`<button class="btn btn-primary" type="button" onclick="applyFtpWorkerAdvice('${esc(p.name)}',${suggested})">Dùng ${suggested} Workers</button>`;
  return `${base}<div class="ftp-worker-advice" role="status"><div class="ftp-worker-advice-main"><div class="ftp-worker-advice-title">GỢI Ý LUỒNG FTP <span class="ftp-worker-advice-value">${suggested} WORKERS</span></div><p>Đã đo ${bound}. ${esc(a.reason||'')}</p></div>${button}</div>`;
}
async function applyFtpWorkerAdvice(name,workers){
  await openEditFtp(name);
  const input=qs('#e_workers');
  if(input){input.value=String(workers);input.focus();input.select()}
  toast(`Đã điền ${workers} Workers. Nhấn Lưu để áp dụng.`);
}
function ftpTestClientLine(text){
  const out=qs('#terminalOutput');
  if(!out)return;
  if(out.classList.contains('terminal-empty')){
    out.textContent='';
    out.classList.remove('terminal-empty');
  }
  const current=out.textContent||'';
  out.textContent=current+(current?'\n':'')+text;
  out.scrollTop=out.scrollHeight;
}
async function ftpRefreshProject(name){
  try{
    const p=await api('/api/projects/'+encodeURIComponent(name));
    state.currentProject=p;
    if(state.selected===name)renderDrawer(p);
    return p;
  }catch(_e){return null}
}
testFtp=async function(name){
  if(ftpTestsInFlight.has(name)){
    toast('FTP đang được kiểm tra. Vui lòng chờ kết quả hiện tại.');
    return;
  }
  ftpTestsInFlight.add(name);
  const done=beginBusy('Đang kiểm tra...');
  const p=(state.currentProject&&state.currentProject.name===name)?state.currentProject:null;
  const c=(p&&p.connection)||{};
  const host=c.host||(p&&p.host)||'FTP';
  const port=c.port||'';
  const now=new Date().toLocaleTimeString('vi-VN',{hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false});
  ftpTestClientLine(`[${now}] FTP TEST · đang kết nối ${host}${port?':'+port:''} ...`);
  toast('Đang kiểm tra FTP...');
  try{
    const d=await api('/api/projects/'+encodeURIComponent(name)+'/test',{method:'POST',body:'{}'});
    await ftpRefreshProject(name);
    const workers=d.workerAdvice&&d.workerAdvice.available?` · đề xuất ${d.workerAdvice.suggestedWorkers} Workers`:'';
    toast('Kết nối FTP thành công · '+d.remotePath+workers);
  }catch(e){
    await ftpRefreshProject(name);
    toast(e.message,true);
    if(authError(e.message)||e.code==='FTP-AUTH-001')openEditFtp(name,true);
  }finally{
    ftpTestsInFlight.delete(name);
    done();
  }
}
'''


def _render_with_ftp_test_log(token: str) -> str:
    html = _BASE_RENDER(token)
    html = html.replace("</head>", _FTP_TEST_CSS + "\n</head>", 1)
    return html.replace("</script>\n</body>", _FTP_TEST_JS + "\n</script>\n</body>", 1)


def _concise_job_log(self, text: str) -> None:
    """Compatibility helper; GuiJob now owns concise, structured logging."""
    self.log(text)


def _ftp_error_message(exc: Exception, *, host: str, port: int) -> str:
    info = classify_exception(exc, stage="ftp-test")
    return f"{info.operator_line()} · Hướng xử lý: {info.recovery} · Đích: {host}:{port}"


def _diagnostic_job(name: str, profile):
    report_dir = server.REPORTS_DIR / profile.host
    with server.JOBS_LOCK:
        job = server.JOBS.get(name)
        if job is None:
            job = server.GuiJob(project=name, status="idle", stage="ftp-test", title="Kiểm tra FTP", message="Sẵn sàng")
            server.JOBS[name] = job
    job._journal_dir = report_dir
    job._journal_secrets = tuple(value for value in (profile.password or "",) if value)
    job._journal_session = datetime.now().astimezone().strftime("%Y%m%d-%H%M%S-ftp")
    return job


def _claim_ftp_test(name: str) -> bool:
    with _FTP_TEST_LOCK:
        if name in _FTP_TEST_ACTIVE:
            return False
        _FTP_TEST_ACTIVE.add(name)
        return True


def _release_ftp_test(name: str) -> None:
    with _FTP_TEST_LOCK:
        _FTP_TEST_ACTIVE.discard(name)


def _open_probe_client(profile, timeout: float):
    transport = FTPTransport(
        FTPConfig(
            host=profile.host,
            username=profile.username,
            password=profile.password or "",
            port=profile.port,
            tls=profile.use_tls,
            passive=profile.passive,
            timeout=timeout,
            workers=1,
            block_size=profile.block_mb * 1024 * 1024,
        )
    )
    return transport._new_client()


def _ftp_worker_advice_path(profile):
    return server.REPORTS_DIR / profile.host / "ftp-worker-advice.json"


def _measure_worker_advice(profile, job) -> dict:
    def progress(event: dict) -> None:
        if event.get("phase") != "ftp_worker_probe":
            return
        requested = int(event.get("requested") or 1)
        connected = int(event.get("connected") or 0)
        attempt = int(event.get("attempt") or 1)
        percent = min(92, 20 + round(requested / _FTP_WORKER_PROBE_CAP * 70))
        suffix = f" · lần {attempt}" if attempt > 1 else ""
        job.set(
            title="Đang đo số luồng FTP",
            message=f"Đã giữ đồng thời {connected}/{requested} kết nối{suffix}",
            percent=percent,
            current=f"{profile.host}:{profile.port}",
        )
        result = "PASS" if connected == requested else "GIỚI HẠN"
        job.log(f"FTP WORKERS · {connected}/{requested} kết nối đồng thời · {result}{suffix}")

    advice = advise_ftp_workers(
        lambda timeout: _open_probe_client(profile, timeout),
        remote_path=profile.remote_path,
        probe_cap=_FTP_WORKER_PROBE_CAP,
        timeout=_FTP_WORKER_PROBE_TIMEOUT,
        reserve_connections=1,
        progress=progress,
    ).as_dict()
    advice.update(
        {
            "available": True,
            "measuredAt": datetime.now().astimezone().isoformat(timespec="seconds"),
            "host": profile.host,
            "port": profile.port,
        }
    )
    return advice


def _project_payload_with_ftp_advice(name: str):
    payload = _BASE_PROJECT_PAYLOAD(name)
    try:
        _profile_path, profile, _paths = server._profile_and_paths(name)
        advice = server._json_read(_ftp_worker_advice_path(profile))
    except Exception:
        advice = {}
    if advice:
        payload["ftpWorkerAdvice"] = advice
    return payload


def _test_connection_with_log(name: str):
    _profile_path, profile, _paths = server._profile_and_paths(name)
    job = _diagnostic_job(name, profile)

    if job.status == "running" and server.ACTIVE_PROJECT == name:
        raise RuntimeError("Dự án đang chạy workflow. Hãy chờ bước hiện tại dừng rồi kiểm tra FTP riêng.")
    if not _claim_ftp_test(name):
        raise RuntimeError("FTP TEST đang chạy cho dự án này. Vui lòng chờ kết quả hiện tại.")

    try:
        job.started_at = datetime.now().astimezone().isoformat(timespec="seconds")
        job.set(
            status="running",
            stage="ftp-test",
            title="Kiểm tra kết nối FTP",
            message=f"Đang kết nối {profile.host}:{profile.port}",
            percent=10,
            current=profile.remote_path,
            error="",
            error_code="",
            error_title="",
            recovery="",
            technical_error="",
        )
        job.log(f"FTP TEST · bắt đầu kết nối {profile.host}:{profile.port} · {profile.protocol.upper()}")
        result = _BASE_TEST_CONNECTION(name)
        job.log(f"FTP TEST · PASS · remote {profile.remote_path} · cwd {result.get('cwd') or '/'}")
        try:
            advice = _measure_worker_advice(profile, job)
            server._json_write(_ftp_worker_advice_path(profile), advice)
            result["workerAdvice"] = advice
            job.log(
                "FTP WORKERS · khuyến nghị "
                f"{advice['suggestedWorkers']} · đã xác nhận {advice['confirmedConnections']} kết nối"
            )
            message = f"FTP OK · đề xuất {advice['suggestedWorkers']} Workers."
        except Exception as probe_exc:
            advice = {
                "available": False,
                "measuredAt": datetime.now().astimezone().isoformat(timespec="seconds"),
                "host": profile.host,
                "port": profile.port,
                "error": f"{type(probe_exc).__name__}: {probe_exc}"[:500],
            }
            server._json_write(_ftp_worker_advice_path(profile), advice)
            result["workerAdvice"] = advice
            job.log("FTP WORKERS · chưa đo được số luồng; kết nối FTP chính vẫn PASS")
            message = "FTP OK · chưa đo được số luồng trong lần này."
        job.set(status="idle", title="Kiểm tra FTP thành công", message=message, percent=100)
        return result
    except Exception as exc:
        info = classify_exception(exc, stage="ftp-test")
        job.fail(OperationError(info))
        raise OperationError(info) from exc
    finally:
        _release_ftp_test(name)


gui_ui.render_app = _render_with_ftp_test_log
server._project_payload = _project_payload_with_ftp_advice
server.test_connection = _test_connection_with_log


def main() -> None:
    server.main()


if __name__ == "__main__":
    main()
