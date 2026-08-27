from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import asdict, dataclass, field
from datetime import datetime
from ftplib import error_perm
from pathlib import Path, PurePosixPath
from typing import Any, Callable
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlsplit
from urllib.request import Request, urlopen
import hashlib
import json
import os
import re
import secrets
import shutil
import threading
import time
import zipfile

from . import rebuild_execute
from .ftp_worker_advisor import advise_ftp_workers
from .rebuild_execute import _ensure_remote_dir
from .secret_runtime import forget_runtime_secret, load_runtime_secret_bundle, remember_runtime_secret_bundle
from .transport import FTPTransport
from .transport.ftp import FTPConfig, ftp_transfer_slot
from .windows_paths import io_path


DATA_ROOT = Path(os.environ.get("WPCLEAN_DATA_ROOT") or Path.cwd()).resolve()
FRESH_ROOT = DATA_ROOT / "fresh-installs"
PROFILES_DIR = FRESH_ROOT / "sites"
HISTORY_FILE = FRESH_ROOT / "history.jsonl"
CACHE_DIR = DATA_ROOT / ".wpclean-cache" / "fresh-wordpress"
ASSETS_DIR = Path(__file__).resolve().parents[2] / "assets" / "fresh-wordpress"
ASSET_MANIFEST = ASSETS_DIR / "manifest.json"
INSTALL_SCHEMA = "oneclick-fresh-v6-local-extract-direct-parallel-upload"
NAME_RE = re.compile(r"^[a-z0-9][a-z0-9._-]{0,95}$")
DOMAIN_RE = re.compile(r"^(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$", re.I)
PREFIX_RE = re.compile(r"^[A-Za-z][A-Za-z0-9_]{0,31}$")
MAX_ARCHIVE_FILES = 30_000
MAX_UNPACKED_BYTES = 512 * 1024 * 1024
_LOCK = threading.RLock()


@dataclass(frozen=True, slots=True)
class InstallShard:
    path: Path
    sha256: str
    files: int
    unpacked_bytes: int


@dataclass(frozen=True, slots=True)
class _PackageEntry:
    source: Path
    member: str
    destination: PurePosixPath
    size: int


@dataclass(frozen=True, slots=True)
class InstallFile:
    path: Path
    relative_path: PurePosixPath
    sha256: str
    size: int


@dataclass(frozen=True, slots=True)
class InstallGroup:
    index: int
    files: tuple[InstallFile, ...]
    control: InstallShard


def _now() -> str:
    return datetime.now().astimezone().isoformat(timespec="seconds")


def _slug(value: str) -> str:
    cleaned = re.sub(r"[^a-z0-9._-]+", "-", value.strip().lower()).strip("-._")
    return cleaned[:96]


def _as_bool(value: Any, *, default: bool = False) -> bool:
    if value is None:
        return default
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        return value.strip().lower() in {"1", "true", "yes", "on"}
    return bool(value)


def _remote_root(value: Any, *, domain: str) -> str:
    remote_path = str(value or f"/domains/{domain}/public_html").strip().replace("\\", "/")
    remote_parts = PurePosixPath(remote_path)
    if (
        not remote_path.startswith("/")
        or "\x00" in remote_path
        or ".." in remote_parts.parts
        or str(remote_parts) in {"", ".", "/"}
    ):
        raise ValueError("Thư mục website trên hosting không hợp lệ hoặc đang trỏ vào FTP root.")
    return str(remote_parts)


def _secret_name(name: str) -> str:
    return f"fresh-{name}"


def _atomic_json(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    temporary.replace(path)


def _safe_profile(name: str, *, must_exist: bool = True) -> Path:
    normalized = _slug(name)
    if normalized != name or not NAME_RE.fullmatch(name):
        raise ValueError("Tên website cài mới không hợp lệ.")
    root = PROFILES_DIR.resolve()
    candidate = (root / f"{name}.json").resolve()
    candidate.relative_to(root)
    if must_exist and not candidate.is_file():
        raise FileNotFoundError(f"Không tìm thấy website cài mới: {name}")
    return candidate


def _read_profile(name: str) -> dict[str, Any]:
    path = _safe_profile(name)
    raw = json.loads(path.read_text(encoding="utf-8-sig"))
    if not isinstance(raw, dict):
        raise ValueError("Cấu hình cài WordPress không hợp lệ.")
    return raw


def _append_history(profile: dict[str, Any], *, status: str, message: str) -> None:
    event = {
        "id": secrets.token_hex(8),
        "createdAt": _now(),
        "status": status,
        "message": message,
    }
    history = profile.get("history") if isinstance(profile.get("history"), list) else []
    profile["history"] = [event, *history][:60]
    HISTORY_FILE.parent.mkdir(parents=True, exist_ok=True)
    with HISTORY_FILE.open("a", encoding="utf-8") as stream:
        stream.write(json.dumps({"site": profile.get("name"), **event}, ensure_ascii=False) + "\n")


def _save_profile(profile: dict[str, Any]) -> None:
    profile["updatedAt"] = _now()
    _atomic_json(_safe_profile(str(profile["name"]), must_exist=False), profile)


@dataclass
class FreshJob:
    site: str
    status: str = "idle"
    stage: str = ""
    message: str = ""
    percent: int = 0
    current: str = ""
    bytes_completed: int = 0
    bytes_total: int = 0
    error: str = ""
    started_at: str = ""
    updated_at: str = ""
    logs: list[str] = field(default_factory=list)

    def update(self, *, stage: str, message: str, percent: int, current: str = "") -> None:
        self.stage = stage
        self.message = message
        self.percent = max(0, min(100, int(percent)))
        self.current = current
        self.updated_at = _now()
        clock = datetime.now().astimezone().strftime("%H:%M:%S")
        line = f"{clock} · {stage} · {message} · {self.percent}%"
        if current:
            line += f" · {current}"
        if not self.logs or self.logs[-1] != line:
            self.logs = [*self.logs[-79:], line]

    def to_dict(self) -> dict[str, Any]:
        result = asdict(self)
        now = datetime.now().astimezone()
        def seconds_since(value: str) -> int:
            try:
                return max(0, int((now - datetime.fromisoformat(value)).total_seconds()))
            except (TypeError, ValueError):
                return 0
        return {
            "site": result["site"],
            "status": result["status"],
            "stage": result["stage"],
            "message": result["message"],
            "percent": result["percent"],
            "current": result["current"],
            "bytesCompleted": result["bytes_completed"],
            "bytesTotal": result["bytes_total"],
            "error": result["error"],
            "startedAt": result["started_at"],
            "updatedAt": result["updated_at"],
            "elapsedSeconds": seconds_since(result["started_at"]),
            "idleSeconds": seconds_since(result["updated_at"]),
            "progress": {
                "completed": result["bytes_completed"],
                "total": result["bytes_total"],
                "unit": "byte",
            },
            "logs": result["logs"],
        }


FRESH_JOBS: dict[str, FreshJob] = {}


def _public_payload(profile: dict[str, Any]) -> dict[str, Any]:
    name = str(profile["name"])
    job = FRESH_JOBS.get(name)
    return {
        "name": name,
        "domain": profile["domain"],
        "siteUrl": profile["siteUrl"],
        "status": profile.get("status", "ready"),
        "createdAt": profile.get("createdAt", ""),
        "updatedAt": profile.get("updatedAt", ""),
        "installedAt": profile.get("installedAt", ""),
        "wordpressVersion": profile.get("wordpressVersion", ""),
        "lastError": profile.get("lastError", ""),
        "clearRemote": bool(profile.get("clearRemote", False)),
        "connection": dict(profile.get("connection") or {}),
        "database": {**dict(profile.get("database") or {}), "passwordConfigured": True},
        "admin": {**dict(profile.get("admin") or {}), "passwordConfigured": True},
        "packages": list(profile.get("packages") or []),
        "history": list(profile.get("history") or []),
        "job": job.to_dict() if job else None,
    }


def list_installs() -> list[dict[str, Any]]:
    PROFILES_DIR.mkdir(parents=True, exist_ok=True)
    items: list[dict[str, Any]] = []
    with _LOCK:
        for path in sorted(PROFILES_DIR.glob("*.json"), key=lambda item: item.name.lower()):
            try:
                profile = json.loads(path.read_text(encoding="utf-8-sig"))
                name = str(profile.get("name") or path.stem)
                if profile.get("status") == "running" and not (
                    FRESH_JOBS.get(name) and FRESH_JOBS[name].status == "running"
                ):
                    profile["status"] = "error"
                    profile["lastError"] = "Lần cài trước bị gián đoạn khi OneClick đóng. Có thể thử lại an toàn."
                    _append_history(profile, status="error", message="Phiên cài đặt bị gián đoạn")
                    _save_profile(profile)
                items.append(_public_payload(profile))
            except Exception as exc:
                items.append({"name": path.stem, "domain": path.stem, "status": "error", "lastError": str(exc), "history": []})
    return items


def get_install(name: str) -> dict[str, Any]:
    with _LOCK:
        return _public_payload(_read_profile(name))


def delete_install(name: str, confirmation: str) -> dict[str, Any]:
    with _LOCK:
        profile = _read_profile(name)
        job = FRESH_JOBS.get(name)
        if job and job.status == "running":
            raise RuntimeError("Website đang được cài; chưa thể xóa khỏi OneClick.")
        domain = str(profile.get("domain") or name)
        if confirmation.strip().lower() != domain.lower():
            raise ValueError("Nhập đúng domain để xác nhận xóa khỏi OneClick.")
        _safe_profile(name).unlink()
        forget_runtime_secret(_secret_name(name))
        FRESH_JOBS.pop(name, None)
    return {
        "ok": True,
        "name": name,
        "domain": domain,
        "message": "Đã xóa khỏi OneClick; source và database trên hosting được giữ nguyên.",
    }


def _validate_input(data: dict[str, Any]) -> tuple[str, dict[str, Any], dict[str, str]]:
    domain = str(data.get("domain") or "").strip().lower().rstrip(".")
    if not DOMAIN_RE.fullmatch(domain):
        raise ValueError("Domain không hợp lệ, ví dụ: demo.kidgrow.site")
    name = _slug(str(data.get("name") or domain))
    if not NAME_RE.fullmatch(name):
        raise ValueError("Tên website không hợp lệ.")
    site_url = str(data.get("siteUrl") or f"https://{domain}").strip().rstrip("/")
    parsed = urlsplit(site_url)
    if parsed.scheme.lower() != "https" or not parsed.hostname:
        raise ValueError("URL website phải dùng HTTPS để bảo vệ mật khẩu khi cài đặt.")
    if parsed.hostname.lower().rstrip(".") != domain:
        raise ValueError("Domain và URL HTTPS phải trỏ tới cùng một website.")
    host = str(data.get("host") or domain).strip()
    username = str(data.get("username") or "").strip()
    ftp_password = str(data.get("password") or "")
    protocol = str(data.get("protocol") or "ftp").strip().lower()
    if not host or not username or not ftp_password:
        raise ValueError("Máy chủ, tài khoản và mật khẩu FTP là bắt buộc.")
    if any(ord(char) < 32 for value in (host, username, ftp_password) for char in value):
        raise ValueError("Thông tin FTP chứa ký tự điều khiển không được hỗ trợ.")
    if protocol not in {"ftp", "ftps", "ftp+tls", "ftp-tls"}:
        raise ValueError("Giao thức chỉ hỗ trợ FTP hoặc FTPS.")
    remote_path = _remote_root(data.get("remotePath"), domain=domain)
    db_name = str(data.get("dbName") or f"{username}_db").strip()
    db_user = str(data.get("dbUser") or f"{username}_user").strip()
    db_password = ftp_password
    db_host = str(data.get("dbHost") or "localhost").strip()
    prefix = str(data.get("tablePrefix") or "wp_").strip()
    if not db_name or not db_user or not db_password or not db_host:
        raise ValueError("Thông tin database không được để trống.")
    if any(ord(char) < 32 for value in (db_name, db_user, db_password, db_host) for char in value):
        raise ValueError("Thông tin database chứa ký tự điều khiển không được hỗ trợ.")
    if not PREFIX_RE.fullmatch(prefix):
        raise ValueError("Tiền tố bảng chỉ gồm chữ, số, dấu gạch dưới và phải bắt đầu bằng chữ.")
    admin_user = str(data.get("adminUser") or "admin").strip()
    admin_email = str(data.get("adminEmail") or f"admin@{domain}").strip()
    admin_password = ftp_password
    if len(admin_user) < 3 or "@" not in admin_email:
        raise ValueError("Tài khoản quản trị hoặc email chưa hợp lệ.")
    if len(admin_password) < 12:
        raise ValueError("Mật khẩu FTP phải có ít nhất 12 ký tự vì được dùng cho tài khoản quản trị.")
    if any(ord(char) < 32 for value in (admin_user, admin_email, admin_password) for char in value):
        raise ValueError("Thông tin quản trị chứa ký tự điều khiển không được hỗ trợ.")
    if any(len(value) > 1024 for value in (ftp_password, db_password, admin_password)):
        raise ValueError("Mật khẩu vượt quá giới hạn 1024 ký tự.")
    profile = {
        "schema": 1,
        "name": name,
        "domain": domain,
        "siteUrl": site_url,
        "status": "ready",
        "createdAt": _now(),
        "updatedAt": _now(),
        "installedAt": "",
        "wordpressVersion": "",
        "lastError": "",
        "installId": secrets.token_hex(16),
        "connection": {
            "host": host,
            "username": username,
            "protocol": protocol,
            "port": max(1, min(65535, int(data.get("port") or 21))),
            "remotePath": remote_path,
            "passive": _as_bool(data.get("passive"), default=True),
            "workers": max(1, min(16, int(data.get("workers") or 8))),
            "blockMb": max(1, min(8, int(data.get("blockMb") or 1))),
        },
        "clearRemote": _as_bool(data.get("clearRemote"), default=False),
        "database": {"host": db_host, "name": db_name, "user": db_user, "tablePrefix": prefix},
        "admin": {"siteTitle": str(data.get("siteTitle") or domain).strip(), "username": admin_user, "email": admin_email},
        "packages": _package_summary(),
        "history": [],
    }
    secret_bundle = {"ftpPassword": ftp_password, "dbPassword": db_password, "adminPassword": admin_password}
    return name, profile, secret_bundle


def create_install(data: dict[str, Any]) -> dict[str, Any]:
    name, profile, secret_bundle = _validate_input(data)
    with _LOCK:
        path = _safe_profile(name, must_exist=False)
        if path.exists():
            raise FileExistsError(f"Website {name} đã có trong danh sách.")
        remember_runtime_secret_bundle(_secret_name(name), secret_bundle)
        _append_history(profile, status="success", message="Đã lưu cấu hình cài mới")
        _save_profile(profile)
        return _public_payload(profile)


def update_install(name: str, data: dict[str, Any]) -> dict[str, Any]:
    with _LOCK:
        current = _read_profile(name)
        job = FRESH_JOBS.get(name)
        if current.get("status") == "success":
            raise RuntimeError("Website đã cài thành công; không sửa cấu hình cài ban đầu.")
        if job and job.status == "running":
            raise RuntimeError("Website đang được cài đặt; chưa thể sửa cấu hình.")
        stored = _secrets_for(name)
        merged = dict(data)
        for field, key in (("password", "ftpPassword"), ("dbPassword", "dbPassword"), ("adminPassword", "adminPassword")):
            if not str(merged.get(field) or ""):
                merged[field] = stored[key]
        validated_name, replacement, secret_bundle = _validate_input(merged)
        if validated_name != name or replacement["domain"] != current["domain"]:
            raise ValueError("Không thể đổi domain của hồ sơ đã tạo; hãy tạo website mới.")
        for field in ("createdAt", "installId", "attemptCount", "lastAttemptAt", "history"):
            if field in current:
                replacement[field] = current[field]
        replacement["status"] = "ready"
        replacement["lastError"] = ""
        remember_runtime_secret_bundle(_secret_name(name), secret_bundle)
        _append_history(replacement, status="success", message="Đã cập nhật cấu hình cài mới")
        _save_profile(replacement)
        return _public_payload(replacement)


def _package_summary() -> list[dict[str, str]]:
    manifest = json.loads(ASSET_MANIFEST.read_text(encoding="utf-8"))
    return [
        {key: str(item[key]) for key in ("kind", "slug", "version") if item.get(key)}
        for item in manifest.get("packages", [])
    ]


def _secrets_for(name: str) -> dict[str, str]:
    values = load_runtime_secret_bundle(_secret_name(name))
    missing = [key for key in ("ftpPassword", "dbPassword", "adminPassword") if not values.get(key)]
    if missing:
        raise ValueError("Thiếu mật khẩu đã lưu an toàn. Hãy tạo lại cấu hình website.")
    return values


def _transport(profile: dict[str, Any], password: str) -> FTPTransport:
    connection = profile["connection"]
    protocol = str(connection["protocol"]).lower()
    return FTPTransport(
        FTPConfig(
            host=str(connection["host"]), username=str(connection["username"]), password=password,
            port=int(connection["port"]), tls=protocol != "ftp", passive=bool(connection.get("passive", True)),
            workers=int(connection.get("workers", 8)), block_size=int(connection.get("blockMb", 1)) * 1024 * 1024,
            timeout=45.0, retries=4,
        )
    )


def probe_install_workers(data: dict[str, Any]) -> dict[str, Any]:
    domain = str(data.get("domain") or "").strip().lower().rstrip(".")
    if not DOMAIN_RE.fullmatch(domain):
        raise ValueError("Nhập domain hợp lệ trước khi đo luồng FTP.")
    host = str(data.get("host") or domain).strip()
    username = str(data.get("username") or "").strip()
    password = str(data.get("password") or "")
    profile_name = str(data.get("profileName") or "").strip()
    if not password and profile_name:
        profile = _read_profile(profile_name)
        if str(profile.get("domain") or "") != domain:
            raise ValueError("Domain đang sửa không khớp hồ sơ đã lưu.")
        password = _secrets_for(profile_name)["ftpPassword"]
    if not host or not username or not password:
        raise ValueError("Nhập tài khoản và mật khẩu FTP trước khi đo số luồng.")
    protocol = str(data.get("protocol") or "ftp").strip().lower()
    if protocol not in {"ftp", "ftps", "ftp+tls", "ftp-tls"}:
        raise ValueError("Giao thức chỉ hỗ trợ FTP hoặc FTPS.")
    port = max(1, min(65535, int(data.get("port") or 21)))
    remote_path = _remote_root(data.get("remotePath"), domain=domain)

    def open_client(timeout: float):
        transport = FTPTransport(
            FTPConfig(
                host=host,
                username=username,
                password=password,
                port=port,
                tls=protocol != "ftp",
                passive=_as_bool(data.get("passive"), default=True),
                workers=1,
                block_size=1024 * 1024,
                timeout=timeout,
                retries=1,
            )
        )
        return transport._new_client()

    advice = advise_ftp_workers(
        open_client,
        remote_path=remote_path,
        probe_cap=16,
        timeout=7.0,
        reserve_connections=0,
    ).as_dict()
    return {"ok": True, "host": host, "port": port, "remotePath": remote_path, **advice}


def _remote_entries(transport: FTPTransport, remote_root: str) -> list[str]:
    client = transport._new_client()
    try:
        client.cwd(str(PurePosixPath(remote_root)))
        try:
            rows = list(client.mlsd(facts=["type"]))
            names = [str(name) for name, _facts in rows]
        except (error_perm, AttributeError):
            names = [PurePosixPath(item).name for item in client.nlst()]
        return sorted({name for name in names if name not in {"", ".", ".."}}, key=str.casefold)
    finally:
        try:
            client.quit()
        except Exception:
            client.close()


def _blocking_remote_entries(entries: list[str]) -> list[str]:
    return [entry for entry in entries if entry not in {".well-known", ".ftpquota"}]


def _prepare_remote_root(
    transport: FTPTransport,
    remote_root: str,
    *,
    clear_remote: bool,
    progress: Callable[[dict[str, Any]], None] | None = None,
) -> tuple[int, int, list[str]]:
    blocking = _blocking_remote_entries(_remote_entries(transport, remote_root))
    if not blocking:
        return 0, 0, []
    if not clear_remote:
        raise RuntimeError(
            "Hosting đang có nội dung cũ. Hãy sửa cấu hình và xác nhận xóa toàn bộ nội dung trong thư mục website."
        )
    deleted_files, deleted_dirs, preserved = rebuild_execute._wipe_remote_root(
        transport,
        remote_root,
        progress=progress,
    )
    remaining = _blocking_remote_entries(_remote_entries(transport, remote_root))
    if remaining:
        raise RuntimeError("Không dọn hết nội dung hosting: " + ", ".join(remaining[:20]))
    return deleted_files, deleted_dirs, preserved


def test_install_connection(name: str) -> dict[str, Any]:
    profile = _read_profile(name)
    transport = _transport(profile, _secrets_for(name)["ftpPassword"])
    connection = profile["connection"]
    cwd = transport.test_connection()
    if not transport.directory_exists(str(connection["remotePath"])):
        raise RuntimeError(f"Kết nối FTP được nhưng không truy cập được {connection['remotePath']}")
    entries = _remote_entries(transport, str(connection["remotePath"]))
    blocking = _blocking_remote_entries(entries)
    with _LOCK:
        current = _read_profile(name)
        _append_history(current, status="success", message="Kiểm tra FTP thành công")
        _save_profile(current)
    return {
        "ok": True,
        "cwd": cwd,
        "remotePath": connection["remotePath"],
        "empty": not blocking,
        "willDelete": bool(blocking) and bool(profile.get("clearRemote", False)),
        "blockingEntries": blocking[:20],
    }


def _verify_asset(item: dict[str, Any]) -> Path:
    path = (ASSETS_DIR / str(item["file"])).resolve()
    path.relative_to(ASSETS_DIR.resolve())
    if not path.is_file():
        raise FileNotFoundError(f"Thiếu bộ cài đi kèm: {item['file']}")
    digest = hashlib.sha256(path.read_bytes()).hexdigest().upper()
    if digest != str(item["sha256"]).upper():
        raise RuntimeError(f"Bộ cài {item['file']} không còn nguyên vẹn.")
    return path


def _safe_zip_member(member: zipfile.ZipInfo, *, required_root: str | None = None) -> PurePosixPath | None:
    name = member.filename.replace("\\", "/")
    if not name or name.startswith("/") or "\x00" in name:
        raise RuntimeError(f"Đường dẫn ZIP không an toàn: {member.filename}")
    path = PurePosixPath(name)
    if any(part in {"", ".", ".."} for part in path.parts):
        raise RuntimeError(f"Đường dẫn ZIP không an toàn: {member.filename}")
    if required_root and (not path.parts or path.parts[0] != required_root):
        raise RuntimeError(f"Sai cấu trúc bộ cài {required_root}: {member.filename}")
    if (member.external_attr >> 16) & 0o170000 == 0o120000:
        raise RuntimeError(f"Không chấp nhận symbolic link trong bộ cài: {member.filename}")
    return None if member.is_dir() else path


def _write_zip_member(target: zipfile.ZipFile, source: zipfile.ZipFile, member: zipfile.ZipInfo, output: PurePosixPath) -> None:
    info = zipfile.ZipInfo(str(output), date_time=member.date_time)
    info.compress_type = zipfile.ZIP_DEFLATED
    info._compresslevel = 9
    info.external_attr = (0o100644 & 0xFFFF) << 16
    with source.open(member) as reader, target.open(info, "w", force_zip64=True) as writer:
        shutil.copyfileobj(reader, writer, length=1024 * 1024)


def _include_asset_member(item: dict[str, Any], path: PurePosixPath) -> bool:
    if str(item.get("slug") or "") != "bricks":
        return True
    lowered = str(path).lower()
    if lowered.startswith("bricks/languages/"):
        if lowered.endswith(".po"):
            return False
        if lowered.endswith(".mo") and not lowered.endswith("/vi.mo"):
            return False
    if "/tests/" in lowered or "/test-data/" in lowered:
        return False
    return True


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with io_path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def _validate_wordpress_core(path: Path) -> str:
    if path.stat().st_size < 5 * 1024 * 1024 or path.stat().st_size > 100 * 1024 * 1024:
        raise RuntimeError("Bộ cài WordPress chính thức có dung lượng không hợp lệ.")
    files = 0
    unpacked = 0
    with zipfile.ZipFile(path) as archive:
        names = set(archive.namelist())
        required = {
            "wordpress/index.php",
            "wordpress/wp-admin/index.php",
            "wordpress/wp-includes/version.php",
        }
        if not required.issubset(names):
            raise RuntimeError("Bộ cài WordPress chính thức thiếu file lõi.")
        for member in archive.infolist():
            safe_path = _safe_zip_member(member, required_root="wordpress")
            if safe_path is None:
                continue
            files += 1
            unpacked += int(member.file_size)
            if files > MAX_ARCHIVE_FILES or unpacked > MAX_UNPACKED_BYTES:
                raise RuntimeError("Bộ cài WordPress vượt giới hạn an toàn.")
        source = archive.read("wordpress/wp-includes/version.php").decode("utf-8", errors="replace")
    match = re.search(r"\$wp_version\s*=\s*['\"]([^'\"]+)", source)
    return match.group(1) if match else "latest"


def _wordpress_core_package(
    progress: Callable[[str, str, int, str], None] | None = None,
) -> tuple[Path, str, str]:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    target = CACHE_DIR / "wordpress-latest.zip"
    try:
        version = _validate_wordpress_core(target)
        if progress:
            progress("package", f"Dùng lại WordPress {version}", 9, target.name)
        return target, _sha256_file(target), version
    except (OSError, RuntimeError, zipfile.BadZipFile):
        target.unlink(missing_ok=True)
    if progress:
        progress("package", "Đang tải WordPress chính thức", 6, "wordpress.org/latest.zip")
    temporary = target.with_suffix(".zip.part")
    temporary.unlink(missing_ok=True)
    request = Request(
        "https://wordpress.org/latest.zip",
        headers={"User-Agent": "OneClick-Fresh/5.0", "Accept": "application/zip"},
    )
    try:
        with urlopen(request, timeout=240) as response, temporary.open("wb") as output:
            final_host = (urlsplit(response.geturl()).hostname or "").lower()
            if final_host not in {"wordpress.org", "downloads.wordpress.org"}:
                raise RuntimeError("WordPress chuyển hướng sang máy chủ không được phép.")
            total = int(response.headers.get("Content-Length") or 0)
            downloaded = 0
            while True:
                block = response.read(1024 * 1024)
                if not block:
                    break
                downloaded += len(block)
                if downloaded > 100 * 1024 * 1024:
                    raise RuntimeError("Bộ cài WordPress tải về vượt giới hạn an toàn.")
                output.write(block)
                if progress and total > 0:
                    progress(
                        "package", "Đang tải WordPress chính thức",
                        min(9, 6 + int(3 * downloaded / total)),
                        f"{downloaded / 1048576:.1f}/{total / 1048576:.1f} MB",
                    )
        version = _validate_wordpress_core(temporary)
        temporary.replace(target)
    except (HTTPError, URLError) as exc:
        raise RuntimeError(f"Không tải được WordPress chính thức: {exc}") from exc
    finally:
        temporary.unlink(missing_ok=True)
    return target, _sha256_file(target), version


def _skip_wordpress_core_file(destination: PurePosixPath) -> bool:
    path = str(destination)
    if path in {"license.txt", "readme.html", "wp-config-sample.php"}:
        return True
    if path == "wp-content/plugins/hello.php" or path.startswith("wp-content/plugins/akismet/"):
        return True
    if path.startswith("wp-content/themes/") and path != "wp-content/themes/index.php":
        return True
    return False


def _collect_install_entries(
    core_path: Path,
    assets: list[tuple[dict[str, Any], Path]],
) -> list[_PackageEntry]:
    entries: list[_PackageEntry] = []
    seen: set[str] = set()
    files = 0
    unpacked = 0

    def append(source: Path, member: zipfile.ZipInfo, destination: PurePosixPath) -> None:
        nonlocal files, unpacked
        key = str(destination).casefold()
        if key in seen:
            raise RuntimeError(f"Bộ cài có file trùng: {destination}")
        seen.add(key)
        files += 1
        unpacked += int(member.file_size)
        if files > MAX_ARCHIVE_FILES or unpacked > MAX_UNPACKED_BYTES:
            raise RuntimeError("Bộ cài vượt giới hạn an toàn.")
        entries.append(_PackageEntry(source, member.filename, destination, int(member.file_size)))

    with zipfile.ZipFile(core_path) as source:
        for member in source.infolist():
            path = _safe_zip_member(member, required_root="wordpress")
            if path is None or len(path.parts) < 2:
                continue
            destination = PurePosixPath(*path.parts[1:])
            if not _skip_wordpress_core_file(destination):
                append(core_path, member, destination)
    for item, asset_path in assets:
        prefix = PurePosixPath("wp-content", "themes" if item["kind"] == "theme" else "plugins")
        with zipfile.ZipFile(asset_path) as source:
            for member in source.infolist():
                path = _safe_zip_member(member, required_root=str(item["slug"]))
                if path is not None and _include_asset_member(item, path):
                    append(asset_path, member, prefix / path)
    required = {
        "wp-admin/index.php",
        "wp-includes/version.php",
        "wp-content/themes/bricks/style.css",
        "wp-content/themes/bricks-child/style.css",
        "wp-content/plugins/duyanhwebpro/duyanhwebpro.php",
    }
    if not required.issubset({str(entry.destination) for entry in entries}):
        raise RuntimeError("Bộ cài tổng hợp thiếu WordPress, Bricks hoặc plugin bắt buộc.")
    return entries


def _validate_cached_shards(directory: Path, workers: int, fingerprint: str) -> tuple[list[InstallShard], str] | None:
    try:
        manifest = json.loads((directory / "manifest.json").read_text(encoding="utf-8"))
        if manifest.get("fingerprint") != fingerprint or int(manifest.get("workers") or 0) != workers:
            return None
        result: list[InstallShard] = []
        for index, item in enumerate(manifest.get("shards") or []):
            if index >= workers:
                return None
            path = directory / f"shard-{index:03d}.zip"
            digest = str(item.get("sha256") or "")
            if not path.is_file() or path.stat().st_size <= 0 or _sha256_file(path) != digest:
                return None
            with zipfile.ZipFile(path) as archive:
                if archive.testzip() is not None:
                    return None
            result.append(InstallShard(path, digest, int(item["files"]), int(item["unpackedBytes"])))
        if len(result) != workers:
            return None
        return result, str(manifest.get("wordpressVersion") or "latest")
    except (OSError, ValueError, KeyError, TypeError, zipfile.BadZipFile, json.JSONDecodeError):
        return None


def build_install_shards(
    workers: int,
    progress: Callable[[str, str, int, str], None] | None = None,
) -> tuple[list[InstallShard], str]:
    workers = max(1, min(16, int(workers)))
    manifest_bytes = ASSET_MANIFEST.read_bytes()
    manifest = json.loads(manifest_bytes.decode("utf-8"))
    assets = [(item, _verify_asset(item)) for item in manifest.get("packages", [])]
    core_path, core_sha, wordpress_version = _wordpress_core_package(progress)
    fingerprint = hashlib.sha256(
        INSTALL_SCHEMA.encode() + manifest_bytes + core_sha.encode() + str(workers).encode()
    ).hexdigest()
    target_dir = CACHE_DIR / f"{fingerprint[:20]}-w{workers}"
    cached = _validate_cached_shards(target_dir, workers, fingerprint)
    if cached:
        if progress:
            progress("package", f"Dùng lại {workers} gói cài đặt", 24, f"WordPress {cached[1]}")
        return cached
    if progress:
        progress("package", f"Đang chia đều bộ cài thành {workers} gói", 11, "Không ghép ZIP trên hosting")
    entries = _collect_install_entries(core_path, assets)
    buckets: list[list[_PackageEntry]] = [[] for _ in range(workers)]
    bucket_bytes = [0] * workers
    for entry in sorted(entries, key=lambda item: item.size, reverse=True):
        index = min(range(workers), key=lambda candidate: (bucket_bytes[candidate], len(buckets[candidate]), candidate))
        buckets[index].append(entry)
        bucket_bytes[index] += entry.size
    target_dir.mkdir(parents=True, exist_ok=True)
    completed = 0
    completed_lock = threading.Lock()

    def build_one(index: int) -> InstallShard:
        nonlocal completed
        path = target_dir / f"shard-{index:03d}.zip"
        temporary = path.with_suffix(".zip.part")
        temporary.unlink(missing_ok=True)
        sources: dict[Path, zipfile.ZipFile] = {}
        try:
            with zipfile.ZipFile(temporary, "w", zipfile.ZIP_DEFLATED, compresslevel=6, allowZip64=True) as output:
                for entry in buckets[index]:
                    source = sources.get(entry.source)
                    if source is None:
                        source = zipfile.ZipFile(entry.source)
                        sources[entry.source] = source
                    _write_zip_member(output, source, source.getinfo(entry.member), entry.destination)
            temporary.replace(path)
        finally:
            for source in sources.values():
                source.close()
            temporary.unlink(missing_ok=True)
        shard = InstallShard(path, _sha256_file(path), len(buckets[index]), bucket_bytes[index])
        with completed_lock:
            completed += 1
            if progress:
                progress("package", f"Đang tạo {workers} gói độc lập", 11 + int(12 * completed / workers), f"{completed}/{workers} gói")
        return shard

    with ThreadPoolExecutor(max_workers=workers, thread_name_prefix="oneclick-build-shard") as pool:
        shards = list(pool.map(build_one, range(workers)))
    payload = {
        "schema": 1,
        "fingerprint": fingerprint,
        "workers": workers,
        "wordpressVersion": wordpress_version,
        "shards": [
            {"sha256": shard.sha256, "files": shard.files, "unpackedBytes": shard.unpacked_bytes}
            for shard in shards
        ],
    }
    _atomic_json(target_dir / "manifest.json", payload)
    if progress:
        progress("package", f"Đã sẵn sàng {workers} gói độc lập", 24, f"WordPress {wordpress_version}")
    return shards, wordpress_version


def _load_cached_install_tree(
    directory: Path,
    workers: int,
    fingerprint: str,
) -> tuple[list[InstallGroup], str] | None:
    try:
        payload = json.loads((directory / "manifest.json").read_text(encoding="utf-8"))
        if payload.get("fingerprint") != fingerprint or int(payload.get("workers") or 0) != workers:
            return None
        raw_groups = payload.get("groups")
        if not isinstance(raw_groups, list) or len(raw_groups) != workers:
            return None

        def load_group(index: int) -> InstallGroup:
            raw_group = raw_groups[index]
            if int(raw_group.get("index") or 0) != index:
                raise ValueError("Sai index nhóm upload.")
            files: list[InstallFile] = []
            for raw_file in raw_group.get("files") or []:
                relative = PurePosixPath(str(raw_file["path"]))
                local = directory / "files" / Path(*relative.parts)
                size = int(raw_file["size"])
                digest = str(raw_file["sha256"])
                filesystem_local = io_path(local)
                if not filesystem_local.is_file() or filesystem_local.stat().st_size != size or _sha256_file(local) != digest:
                    raise ValueError(f"Cache file không còn nguyên vẹn: {relative}")
                files.append(InstallFile(local, relative, digest, size))
            control_path = directory / f"control-{index:03d}.zip"
            control_sha = str(raw_group["controlSha256"])
            if not control_path.is_file() or _sha256_file(control_path) != control_sha:
                raise ValueError("Control package không còn nguyên vẹn.")
            control = InstallShard(
                control_path, control_sha, 1,
                sum(item.size for item in files),
            )
            return InstallGroup(index, tuple(files), control)

        with ThreadPoolExecutor(max_workers=workers, thread_name_prefix="oneclick-validate-tree") as pool:
            groups = list(pool.map(load_group, range(workers)))
        return groups, str(payload.get("wordpressVersion") or "latest")
    except (OSError, ValueError, KeyError, TypeError, json.JSONDecodeError):
        return None


def build_install_tree(
    workers: int,
    progress: Callable[[str, str, int, str], None] | None = None,
) -> tuple[list[InstallGroup], str]:
    workers = max(1, min(16, int(workers)))
    manifest_bytes = ASSET_MANIFEST.read_bytes()
    manifest = json.loads(manifest_bytes.decode("utf-8"))
    assets = [(item, _verify_asset(item)) for item in manifest.get("packages", [])]
    core_path, core_sha, wordpress_version = _wordpress_core_package(progress)
    fingerprint = hashlib.sha256(
        INSTALL_SCHEMA.encode() + manifest_bytes + core_sha.encode() + str(workers).encode()
    ).hexdigest()
    target_dir = CACHE_DIR / f"{fingerprint[:20]}-direct-w{workers}"
    cached = _load_cached_install_tree(target_dir, workers, fingerprint)
    if cached:
        if progress:
            total_files = sum(len(group.files) for group in cached[0])
            progress("package", f"Dùng lại {total_files} file đã chuẩn bị", 24, f"WordPress {cached[1]}")
        return cached

    entries = _collect_install_entries(core_path, assets)
    buckets: list[list[_PackageEntry]] = [[] for _ in range(workers)]
    scores = [0] * workers
    # A small-file FTP command costs much more than its bytes. This weight keeps
    # both bytes and STOR count balanced instead of leaving wp-includes on one worker.
    command_weight = 64 * 1024
    for entry in sorted(entries, key=lambda item: item.size, reverse=True):
        index = min(range(workers), key=lambda candidate: (scores[candidate], len(buckets[candidate]), candidate))
        buckets[index].append(entry)
        scores[index] += entry.size + command_weight

    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    temporary_dir = CACHE_DIR / f".build-{fingerprint[:16]}-{secrets.token_hex(4)}"
    shutil.rmtree(io_path(temporary_dir), ignore_errors=True)
    temporary_dir.mkdir(parents=True)
    files_root = temporary_dir / "files"
    files_root.mkdir()
    completed = 0
    completed_lock = threading.Lock()

    def build_group(index: int) -> dict[str, Any]:
        nonlocal completed
        files: list[dict[str, Any]] = []
        sources: dict[Path, zipfile.ZipFile] = {}
        try:
            for entry in buckets[index]:
                source = sources.get(entry.source)
                if source is None:
                    source = zipfile.ZipFile(entry.source)
                    sources[entry.source] = source
                local = files_root / Path(*entry.destination.parts)
                filesystem_local = io_path(local)
                filesystem_local.parent.mkdir(parents=True, exist_ok=True)
                digest = hashlib.sha256()
                with source.open(source.getinfo(entry.member)) as reader, filesystem_local.open("wb") as writer:
                    while True:
                        block = reader.read(1024 * 1024)
                        if not block:
                            break
                        digest.update(block)
                        writer.write(block)
                files.append({
                    "path": str(entry.destination),
                    "size": entry.size,
                    "sha256": digest.hexdigest(),
                })
        finally:
            for source in sources.values():
                source.close()
        upload_manifest = {
            "schema": 1,
            "index": index,
            "workers": workers,
            "files": files,
        }
        manifest_name = f".oneclick-upload-manifest-{index:03d}.json"
        control_path = temporary_dir / f"control-{index:03d}.zip"
        with zipfile.ZipFile(control_path, "w", zipfile.ZIP_DEFLATED, compresslevel=6) as control:
            control.writestr(manifest_name, json.dumps(upload_manifest, separators=(",", ":")))
        with completed_lock:
            completed += 1
            if progress:
                progress(
                    "package", f"Đang giải nén WordPress trên máy · {completed}/{workers} phần",
                    10 + int(13 * completed / workers), f"{len(entries)} file",
                )
        return {
            "index": index,
            "controlSha256": _sha256_file(control_path),
            "files": files,
        }

    try:
        if progress:
            progress("package", f"Đang giải nén {len(entries)} file trên máy", 10, f"{workers} luồng")
        with ThreadPoolExecutor(max_workers=workers, thread_name_prefix="oneclick-build-tree") as pool:
            groups_payload = list(pool.map(build_group, range(workers)))
        _atomic_json(temporary_dir / "manifest.json", {
            "schema": 1,
            "fingerprint": fingerprint,
            "workers": workers,
            "wordpressVersion": wordpress_version,
            "groups": groups_payload,
        })
        if target_dir.exists():
            shutil.rmtree(io_path(target_dir))
        temporary_dir.replace(target_dir)
    finally:
        shutil.rmtree(io_path(temporary_dir), ignore_errors=True)
    loaded = _load_cached_install_tree(target_dir, workers, fingerprint)
    if not loaded:
        raise RuntimeError("Không xác minh được bộ WordPress đã giải nén trên máy.")
    if progress:
        progress("package", f"Đã chuẩn bị {len(entries)} file trên máy", 24, f"WordPress {wordpress_version}")
    return loaded


def _php_quote(value: str) -> str:
    return value.replace("\\", "\\\\").replace("'", "\\'")


def _installer_bridge(
    token: str,
    stage_name: str,
    shard_prefix: str,
    shard_hashes: list[str],
) -> str:
    template = r'''<?php
@set_time_limit(0);
@ini_set('memory_limit', '512M');
@ini_set('display_errors', '0');
@ignore_user_abort(true);
header('Content-Type: application/json; charset=utf-8');
header('Cache-Control: no-store');
const OC_TOKEN = '__TOKEN__';
const OC_STAGE = '__STAGE__';
const OC_SHARD_PREFIX = '__SHARD_PREFIX__';
const OC_SHARD_COUNT = __SHARD_COUNT__;
const OC_SHARD_HASHES = '__SHARD_HASHES__';
function answer($ok, $message, $extra = array(), $status = 200) {
    http_response_code($status);
    echo json_encode(array_merge(array('ok'=>$ok,'message'=>$message),$extra));
    exit;
}
function remove_tree($path) {
    if (!file_exists($path)) return;
    if (is_link($path) || is_file($path)) { @unlink($path); return; }
    foreach (scandir($path) as $name) if ($name !== '.' && $name !== '..') remove_tree($path . DIRECTORY_SEPARATOR . $name);
    @rmdir($path);
}
function config_quote($value) { return str_replace(array('\\', "'"), array('\\\\', "\\'"), (string)$value); }
function shard_name($index) { return OC_SHARD_PREFIX . sprintf('%03d', $index) . '.zip'; }
function shard_path($index) { return __DIR__ . DIRECTORY_SEPARATOR . shard_name($index); }
function shard_hash($index) {
    $hashes = json_decode(OC_SHARD_HASHES, true);
    if (!is_array($hashes) || count($hashes) !== OC_SHARD_COUNT || !isset($hashes[$index])) throw new Exception('Danh sách checksum bộ cài không hợp lệ.');
    return (string)$hashes[$index];
}
function remove_shards() { for ($index=0; $index<OC_SHARD_COUNT; $index++) @unlink(shard_path($index)); }
function validate_shard($zipPath, $stage, $prepareDirectories) {
    $zip = new ZipArchive();
    if ($zip->open($zipPath) !== true) throw new Exception('Không mở được gói ZIP độc lập trên hosting.');
    $files = 0; $bytes = 0; $directories = array();
    try {
        for ($i=0; $i<$zip->numFiles; $i++) {
            $raw = str_replace('\\', '/', (string)$zip->getNameIndex($i));
            if ($raw === '' || $raw[0] === '/' || strpos($raw, "\0") !== false || preg_match('#(^|/)\.\.(/|$)#', $raw)) throw new Exception('Package chứa đường dẫn không an toàn.');
            if (strpos($raw, 'wordpress/') === 0) throw new Exception('Bộ cài còn thư mục wordpress trung gian.');
            if ($prepareDirectories) {
                $directory = substr($raw, -1) === '/' ? rtrim($raw, '/') : dirname($raw);
                if ($directory !== '' && $directory !== '.') $directories[$directory] = true;
            }
            if (substr($raw, -1) === '/') continue;
            $stat = $zip->statIndex($i);
            $files++; $bytes += is_array($stat) ? (int)$stat['size'] : 0;
            if ($files > 30000 || $bytes > 536870912) throw new Exception('Package vượt giới hạn an toàn.');
            if (method_exists($zip, 'getExternalAttributesIndex')) {
                $opsys = 0; $attributes = 0;
                if ($zip->getExternalAttributesIndex($i, $opsys, $attributes) && ((($attributes >> 16) & 0170000) === 0120000)) throw new Exception('Package chứa symbolic link.');
            }
        }
    } finally { $zip->close(); }
    if ($prepareDirectories) {
        uksort($directories, function($left, $right) { return strlen($left) <=> strlen($right); });
        foreach ($directories as $directory=>$unused) {
            $target = $stage . '/' . $directory;
            if (!is_dir($target) && !@mkdir($target, 0700, true) && !is_dir($target)) throw new Exception('Không tạo được cây thư mục giải nén.');
        }
    }
    return $files;
}
function shard_marker($stage, $index) { return $stage . '/.oneclick-shard-' . sprintf('%03d', $index) . '.ok'; }
function shard_signature($index) { return hash_hmac('sha256', 'shard:' . $index . ':' . OC_SHARD_COUNT, OC_TOKEN); }
function extract_shard($stage, $index) {
    $zipPath = shard_path($index);
    if (!is_file($zipPath) || !hash_equals(shard_hash($index), hash_file('sha256', $zipPath))) throw new Exception('Gói cài ' . ($index + 1) . '/' . OC_SHARD_COUNT . ' không còn nguyên vẹn.');
    $files = validate_shard($zipPath, $stage, true);
    $zip = new ZipArchive();
    if ($zip->open($zipPath) !== true) throw new Exception('Không mở được ZIP để giải nén song song.');
    try {
        if (!$zip->extractTo($stage)) throw new Exception('Worker không giải nén được gói ZIP đã nhận.');
    } finally { $zip->close(); }
    if (file_put_contents(shard_marker($stage, $index), shard_signature($index), LOCK_EX) === false) throw new Exception('Không lưu được checkpoint worker giải nén.');
    return $files;
}
function require_shards($stage, $remove) {
    for ($index=0; $index<OC_SHARD_COUNT; $index++) {
        $marker = shard_marker($stage, $index);
        $value = is_file($marker) ? (string)file_get_contents($marker) : '';
        if (!hash_equals(shard_signature($index), $value)) throw new Exception('Thiếu checkpoint worker ' . ($index + 1) . '/' . OC_SHARD_COUNT . '.');
        if ($remove) @unlink($marker);
    }
}
function upload_manifest_path($stage, $index) { return $stage . '/.oneclick-upload-manifest-' . sprintf('%03d', $index) . '.json'; }
function upload_marker($stage, $index) { return $stage . '/.oneclick-upload-' . sprintf('%03d', $index) . '.ok'; }
function upload_signature($index) { return hash_hmac('sha256', 'upload:' . $index . ':' . OC_SHARD_COUNT, OC_TOKEN); }
function prepare_upload_directories($stage) {
    $directories = array(); $seen = array(); $count = 0;
    for ($index=0; $index<OC_SHARD_COUNT; $index++) {
        $manifestPath = upload_manifest_path($stage, $index);
        $manifest = is_file($manifestPath) ? json_decode((string)file_get_contents($manifestPath), true) : null;
        if (!is_array($manifest) || (int)($manifest['schema'] ?? 0) !== 1 || (int)($manifest['index'] ?? -1) !== $index || (int)($manifest['workers'] ?? 0) !== OC_SHARD_COUNT || !isset($manifest['files']) || !is_array($manifest['files'])) throw new Exception('Manifest upload không hợp lệ.');
        foreach ($manifest['files'] as $file) {
            if (!is_array($file) || !isset($file['path'],$file['size'],$file['sha256'])) throw new Exception('Manifest upload thiếu thông tin file.');
            $raw = str_replace('\\', '/', (string)$file['path']);
            if ($raw === '' || $raw[0] === '/' || strpos($raw, "\0") !== false || preg_match('#(^|/)\.\.(/|$)#', $raw) || strpos($raw, 'wordpress/') === 0) throw new Exception('Manifest upload chứa đường dẫn không an toàn.');
            $key = strtolower($raw);
            if (isset($seen[$key])) throw new Exception('Manifest upload có file trùng: ' . $raw);
            $seen[$key] = true; $count++;
            if ($count > 30000) throw new Exception('Manifest upload vượt giới hạn file.');
            $directory = dirname($raw);
            if ($directory !== '' && $directory !== '.') $directories[$directory] = true;
        }
    }
    uksort($directories, function($left, $right) { return strlen($left) <=> strlen($right); });
    foreach ($directories as $directory=>$unused) {
        $target = $stage . '/' . $directory;
        if (!is_dir($target) && !@mkdir($target, 0755, true) && !is_dir($target)) throw new Exception('Không tạo được cây thư mục upload.');
    }
    return $count;
}
function verify_upload_group($stage, $index) {
    $manifestPath = upload_manifest_path($stage, $index);
    $manifest = is_file($manifestPath) ? json_decode((string)file_get_contents($manifestPath), true) : null;
    if (!is_array($manifest) || (int)($manifest['schema'] ?? 0) !== 1 || (int)($manifest['index'] ?? -1) !== $index || (int)($manifest['workers'] ?? 0) !== OC_SHARD_COUNT || !isset($manifest['files']) || !is_array($manifest['files'])) throw new Exception('Manifest upload không hợp lệ.');
    $count = 0; $bytes = 0;
    foreach ($manifest['files'] as $file) {
        if (!is_array($file) || !isset($file['path'],$file['size'],$file['sha256'])) throw new Exception('Manifest upload thiếu thông tin file.');
        $raw = str_replace('\\', '/', (string)$file['path']);
        if ($raw === '' || $raw[0] === '/' || strpos($raw, "\0") !== false || preg_match('#(^|/)\.\.(/|$)#', $raw) || strpos($raw, 'wordpress/') === 0) throw new Exception('Manifest upload chứa đường dẫn không an toàn.');
        $target = $stage . '/' . $raw;
        $size = (int)$file['size']; $hash = (string)$file['sha256'];
        if (!is_file($target) || is_link($target) || (int)filesize($target) !== $size || !hash_equals($hash, hash_file('sha256', $target))) throw new Exception('File upload không còn nguyên vẹn: ' . $raw);
        @chmod($target, 0644);
        $count++; $bytes += $size;
        if ($count > 30000 || $bytes > 536870912) throw new Exception('Nhóm upload vượt giới hạn an toàn.');
    }
    @unlink($manifestPath);
    if (file_put_contents(upload_marker($stage, $index), upload_signature($index), LOCK_EX) === false) throw new Exception('Không lưu được checkpoint upload.');
    return $count;
}
function require_uploads($stage, $remove) {
    for ($index=0; $index<OC_SHARD_COUNT; $index++) {
        $marker = upload_marker($stage, $index);
        $value = is_file($marker) ? (string)file_get_contents($marker) : '';
        if (!hash_equals(upload_signature($index), $value)) throw new Exception('Thiếu checkpoint upload ' . ($index + 1) . '/' . OC_SHARD_COUNT . '.');
        if ($remove) @unlink($marker);
    }
}
function directadmin_create_database($data, &$detail) {
    $detail = '';
    if (!function_exists('curl_init') || empty($data['panelUser']) || empty($data['panelPassword'])) return false;
    $account = preg_replace('/[^A-Za-z0-9_]/', '', (string)$data['panelUser']);
    $dbName = (string)$data['dbName']; $dbUser = (string)$data['dbUser'];
    $prefix = $account . '_';
    $dbSuffix = strpos($dbName, $prefix) === 0 ? substr($dbName, strlen($prefix)) : $dbName;
    $userSuffix = strpos($dbUser, $prefix) === 0 ? substr($dbUser, strlen($prefix)) : $dbUser;
    $dbSuffix = substr(preg_replace('/[^A-Za-z0-9_]/', '', $dbSuffix), 0, 40);
    $userSuffix = substr(preg_replace('/[^A-Za-z0-9_]/', '', $userSuffix), 0, 40);
    if ($account === '' || $dbSuffix === '' || $userSuffix === '') { $detail = 'Tên database không hợp lệ.'; return false; }
    $body = http_build_query(array('action'=>'create','name'=>$dbSuffix,'user'=>$userSuffix,'passwd'=>(string)$data['dbPassword'],'passwd2'=>(string)$data['dbPassword']));
    foreach (array('https://127.0.0.1:2222/CMD_API_DATABASES','http://127.0.0.1:2222/CMD_API_DATABASES') as $url) {
        $curl = curl_init($url);
        curl_setopt($curl, CURLOPT_POST, true); curl_setopt($curl, CURLOPT_POSTFIELDS, $body);
        curl_setopt($curl, CURLOPT_RETURNTRANSFER, true); curl_setopt($curl, CURLOPT_HEADER, false);
        curl_setopt($curl, CURLOPT_HTTPAUTH, CURLAUTH_BASIC); curl_setopt($curl, CURLOPT_USERPWD, $data['panelUser'] . ':' . $data['panelPassword']);
        curl_setopt($curl, CURLOPT_CONNECTTIMEOUT, 4); curl_setopt($curl, CURLOPT_TIMEOUT, 20);
        if (strpos($url, 'https://') === 0) { curl_setopt($curl, CURLOPT_SSL_VERIFYPEER, false); curl_setopt($curl, CURLOPT_SSL_VERIFYHOST, 0); }
        $response = curl_exec($curl); $status = (int)curl_getinfo($curl, CURLINFO_RESPONSE_CODE); $error = (string)curl_error($curl); curl_close($curl);
        if ($response === false || $status < 200 || $status >= 300) { $detail = $error !== '' ? $error : 'HTTP ' . $status; continue; }
        $parsed = array(); parse_str((string)$response, $parsed);
        if (isset($parsed['error']) && (string)$parsed['error'] === '0') { $detail = 'DirectAdmin đã tạo database.'; return true; }
        $json = json_decode((string)$response, true);
        if (is_array($json) && isset($json['error']) && !$json['error']) { $detail = 'DirectAdmin đã tạo database.'; return true; }
        $message = isset($parsed['details']) ? $parsed['details'] : (isset($parsed['text']) ? $parsed['text'] : (string)$response);
        $detail = substr(trim(strip_tags((string)$message)), 0, 240);
    }
    return false;
}
$action = '';
try {
    $supplied = isset($_SERVER['HTTP_X_ONECLICK_TOKEN']) ? $_SERVER['HTTP_X_ONECLICK_TOKEN'] : '';
    if (!hash_equals(OC_TOKEN, $supplied)) answer(false, 'Phiên cài đặt không hợp lệ.', array(), 403);
    $data = json_decode(file_get_contents('php://input'), true);
    if (!is_array($data)) answer(false, 'Dữ liệu cài đặt không hợp lệ.', array(), 400);
    $action = isset($data['action']) ? (string)$data['action'] : 'prepare';
    $workers = isset($data['extractWorkers']) ? (int)$data['extractWorkers'] : 1;
    if (OC_SHARD_COUNT < 1 || OC_SHARD_COUNT > 16 || $workers !== OC_SHARD_COUNT) answer(false, 'Số worker giải nén không khớp bộ cài.', array(), 400);
    $stage = __DIR__ . DIRECTORY_SEPARATOR . OC_STAGE;
    if ($action === 'cleanup') {
        remove_tree($stage); remove_shards(); @unlink(__FILE__);
        answer(true, 'Đã dọn phiên cài đặt tạm.');
    }
    if (!class_exists('ZipArchive')) answer(false, 'Hosting chưa bật PHP ZipArchive.', array(), 500);
    if ($action === 'extract') {
        $index = isset($data['workerIndex']) ? (int)$data['workerIndex'] : -1;
        if ($index < 0 || $index >= OC_SHARD_COUNT) answer(false, 'Worker giải nén không hợp lệ.', array(), 400);
        if (!is_dir($stage)) answer(false, 'Thư mục tạm không tồn tại.', array(), 409);
        $count = extract_shard($stage, $index);
        answer(true, 'Worker giải nén hoàn tất.', array('workerIndex'=>$index,'files'=>$count));
    }
    if ($action === 'prepare-upload') {
        if (!is_dir($stage)) answer(false, 'Thư mục tạm không tồn tại.', array(), 409);
        require_shards($stage, false);
        $count = prepare_upload_directories($stage);
        answer(true, 'Đã tạo cây thư mục upload.', array('files'=>$count));
    }
    if ($action === 'verify') {
        $index = isset($data['workerIndex']) ? (int)$data['workerIndex'] : -1;
        if ($index < 0 || $index >= OC_SHARD_COUNT) answer(false, 'Worker kiểm tra upload không hợp lệ.', array(), 400);
        if (!is_dir($stage)) answer(false, 'Thư mục tạm không tồn tại.', array(), 409);
        $count = verify_upload_group($stage, $index);
        answer(true, 'Worker kiểm tra upload hoàn tất.', array('workerIndex'=>$index,'files'=>$count));
    }
    if (!in_array($action, array('prepare','finalize'), true)) answer(false, 'Thao tác cài đặt không hợp lệ.', array(), 400);
    if ($action === 'finalize') {
        if (!is_dir($stage)) throw new Exception('Phiên cài đặt tạm không còn đầy đủ.');
        require_shards($stage, false);
        require_uploads($stage, false);
    }
    if ($action === 'prepare') {
    $allowed = array(basename(__FILE__), '.well-known', '.ftpquota');
    for ($shardIndex=0; $shardIndex<OC_SHARD_COUNT; $shardIndex++) $allowed[] = shard_name($shardIndex);
    $clearRemote = !empty($data['clearRemote']);
    foreach (scandir(__DIR__) as $name) {
        if ($name === '.' || $name === '..' || in_array($name, $allowed, true)) continue;
        if (!$clearRemote) answer(false, 'Thư mục hosting không còn trống: ' . $name, array(), 409);
        remove_tree(__DIR__ . DIRECTORY_SEPARATOR . $name);
    }
    foreach (scandir(__DIR__) as $name) {
        if ($name === '.' || $name === '..' || in_array($name, $allowed, true)) continue;
        answer(false, 'Không dọn được nội dung cũ trên hosting: ' . $name, array(), 500);
    }
    remove_tree($stage);
    if (!mkdir($stage, 0755, true)) answer(false, 'Không tạo được thư mục tạm trên hosting.', array(), 500);
    for ($shardIndex=0; $shardIndex<OC_SHARD_COUNT; $shardIndex++) {
        if (!is_file(shard_path($shardIndex))) answer(false, 'Thiếu gói cài ' . ($shardIndex + 1) . '/' . OC_SHARD_COUNT . '.', array(), 400);
    }
    } else {
        if (!is_dir($stage)) throw new Exception('Phiên cài đặt tạm không còn đầy đủ.');
    }
    foreach (array('dbName','dbUser','dbPassword','dbHost','tablePrefix','siteTitle','adminUser','adminEmail','adminPassword','siteUrl','installId') as $key) {
        if (!isset($data[$key]) || (string)$data[$key] === '') { remove_tree($stage); answer(false, 'Thiếu thông tin ' . $key, array(), 400); }
    }
    if (!preg_match('/^[A-Za-z][A-Za-z0-9_]{0,31}$/', (string)$data['tablePrefix'])) { remove_tree($stage); answer(false, 'Tiền tố bảng không hợp lệ.', array(), 400); }
    if (!preg_match('/^[a-f0-9]{32}$/', (string)$data['installId'])) { remove_tree($stage); answer(false, 'Mã phiên cài đặt không hợp lệ.', array(), 400); }
    if (!class_exists('mysqli')) { remove_tree($stage); answer(false, 'Hosting chưa bật PHP mysqli.', array(), 500); }
    mysqli_report(MYSQLI_REPORT_OFF);
    $candidates = array(array('name'=>$data['dbName'],'user'=>$data['dbUser'],'password'=>$data['dbPassword'],'host'=>$data['dbHost']));
    if (isset($data['databaseCandidates']) && is_array($data['databaseCandidates'])) {
        foreach ($data['databaseCandidates'] as $candidate) {
            if (!is_array($candidate)) continue;
            if (!isset($candidate['name'],$candidate['user'],$candidate['password'],$candidate['host'])) continue;
            $candidates[] = $candidate;
        }
    }
    $mysqli = null; $selectedDb = null; $connectError = '';
    foreach ($candidates as $candidate) {
        $db = @new mysqli((string)$candidate['host'], (string)$candidate['user'], (string)$candidate['password'], (string)$candidate['name']);
        if (!$db->connect_errno) { $mysqli = $db; $selectedDb = $candidate; break; }
        $connectError = (string)$db->connect_error;
    }
    $panelDetail = '';
    if (!$mysqli || !$selectedDb) {
        directadmin_create_database($data, $panelDetail);
        $candidate = array('name'=>$data['dbName'],'user'=>$data['dbUser'],'password'=>$data['dbPassword'],'host'=>$data['dbHost']);
        $db = @new mysqli((string)$candidate['host'], (string)$candidate['user'], (string)$candidate['password'], (string)$candidate['name']);
        if (!$db->connect_errno) { $mysqli = $db; $selectedDb = $candidate; }
        else $connectError = (string)$db->connect_error;
    }
    if (!$mysqli || !$selectedDb) {
        remove_tree($stage);
        $panelMessage = $panelDetail !== '' ? ' DirectAdmin: ' . $panelDetail : '';
        answer(false, 'Không tự tạo/kết nối được database.' . $panelMessage . ' MySQL: ' . $connectError, array(), 500);
    }
    $data['dbName'] = (string)$selectedDb['name'];
    $data['dbUser'] = (string)$selectedDb['user'];
    $data['dbPassword'] = (string)$selectedDb['password'];
    $data['dbHost'] = (string)$selectedDb['host'];
    $markerTable = (string)$data['tablePrefix'] . 'oneclick_install_marker';
    $matchingTables = array();
    $tableResult = $mysqli->query('SHOW TABLES');
    if (!$tableResult) { remove_tree($stage); answer(false, 'Không kiểm tra được database.', array(), 500); }
    while ($row = $tableResult->fetch_row()) if (strncmp((string)$row[0], (string)$data['tablePrefix'], strlen((string)$data['tablePrefix'])) === 0) $matchingTables[] = (string)$row[0];
    $markerExists = in_array($markerTable, $matchingTables, true);
    if (count($matchingTables) > 0 && !$markerExists) { remove_tree($stage); answer(false, 'Database đã có bảng dùng tiền tố này.', array(), 409); }
    if ($markerExists) {
        $markerResult = $mysqli->query('SELECT install_id FROM `' . $markerTable . '` LIMIT 1');
        $markerRow = $markerResult ? $markerResult->fetch_assoc() : null;
        if (!$markerRow || !hash_equals((string)$data['installId'], (string)$markerRow['install_id'])) { remove_tree($stage); answer(false, 'Database thuộc một phiên cài đặt khác.', array(), 409); }
    } else {
        if (!$mysqli->query('CREATE TABLE `' . $markerTable . '` (install_id VARCHAR(64) NOT NULL PRIMARY KEY) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4')) { remove_tree($stage); answer(false, 'Không tạo được checkpoint database.', array(), 500); }
        $installId = $mysqli->real_escape_string((string)$data['installId']);
        if (!$mysqli->query("INSERT INTO `" . $markerTable . "` (install_id) VALUES ('" . $installId . "')")) { remove_tree($stage); answer(false, 'Không lưu được checkpoint database.', array(), 500); }
    }
    if ($action === 'prepare') {
        answer(true, 'Đã chuẩn bị các gói độc lập; sẵn sàng giải nén song song.', array(
            'extractWorkers'=>$workers,
            'shards'=>OC_SHARD_COUNT,
            'database'=>array('name'=>$data['dbName'],'user'=>$data['dbUser'],'host'=>$data['dbHost'])
        ));
    }
    require_shards($stage, true);
    require_uploads($stage, true);
    if (!is_file($stage . '/wp-includes/version.php') || !is_file($stage . '/wp-content/themes/bricks/style.css') || !is_file($stage . '/wp-content/plugins/duyanhwebpro/duyanhwebpro.php')) throw new Exception('Bộ cài sau giải nén thiếu WordPress, Bricks hoặc plugin.');
    $config = "<?php\n/** OneClick generated configuration. */\n";
    foreach (array('DB_NAME'=>'dbName','DB_USER'=>'dbUser','DB_PASSWORD'=>'dbPassword','DB_HOST'=>'dbHost') as $constant=>$key) $config .= "define('".$constant."', '".config_quote($data[$key])."');\n";
    $config .= "define('DB_CHARSET', 'utf8mb4');\ndefine('DB_COLLATE', '');\n";
    foreach (array('AUTH_KEY','SECURE_AUTH_KEY','LOGGED_IN_KEY','NONCE_KEY','AUTH_SALT','SECURE_AUTH_SALT','LOGGED_IN_SALT','NONCE_SALT') as $key) $config .= "define('".$key."', '".config_quote(base64_encode(random_bytes(48)))."');\n";
    $config .= "\n\$table_prefix = '".config_quote($data['tablePrefix'])."';\ndefine('WP_DEBUG', false);\ndefine('DISALLOW_FILE_EDIT', true);\nif (!defined('ABSPATH')) define('ABSPATH', __DIR__ . '/');\nrequire_once ABSPATH . 'wp-settings.php';\n";
    if (file_put_contents($stage . '/wp-config.php', $config, LOCK_EX) === false) { remove_tree($stage); answer(false, 'Không ghi được wp-config.php.', array(), 500); }
    @chmod($stage . '/wp-config.php', 0600);
    define('WP_INSTALLING', true);
    require $stage . '/wp-load.php';
    require_once $stage . '/wp-admin/includes/upgrade.php';
    require_once $stage . '/wp-admin/includes/plugin.php';
    $alreadyInstalled = is_blog_installed();
    if (!$alreadyInstalled) wp_install((string)$data['siteTitle'], (string)$data['adminUser'], (string)$data['adminEmail'], true, '', (string)$data['adminPassword'], 'vi');
    update_option('siteurl', (string)$data['siteUrl']);
    update_option('home', (string)$data['siteUrl']);
    update_option('timezone_string', 'Asia/Ho_Chi_Minh');
    update_option('permalink_structure', '/%postname%/');
    switch_theme('bricks-child');
    $activated = activate_plugin('duyanhwebpro/duyanhwebpro.php');
    if (is_wp_error($activated)) throw new Exception('Không kích hoạt được Duy Anh Web Pro: ' . $activated->get_error_message());
    $path = parse_url((string)$data['siteUrl'], PHP_URL_PATH);
    $base = '/' . trim((string)$path, '/') . '/'; if ($base === '//') $base = '/';
    $htaccess = "# BEGIN WordPress\n<IfModule mod_rewrite.c>\nRewriteEngine On\nRewriteRule .* - [E=HTTP_AUTHORIZATION:%{HTTP:Authorization}]\nRewriteBase ".$base."\nRewriteRule ^index\\.php$ - [L]\nRewriteCond %{REQUEST_FILENAME} !-f\nRewriteCond %{REQUEST_FILENAME} !-d\nRewriteRule . ".$base."index.php [L]\n</IfModule>\n# END WordPress\n";
    file_put_contents($stage . '/.htaccess', $htaccess, LOCK_EX);
    @chmod($stage . '/.htaccess', 0644);
    foreach (scandir($stage) as $name) {
        if ($name === '.' || $name === '..') continue;
        if (!rename($stage . DIRECTORY_SEPARATOR . $name, __DIR__ . DIRECTORY_SEPARATOR . $name)) throw new Exception('Không chuyển được file ' . $name . ' vào website.');
    }
    $mysqli->query('DROP TABLE IF EXISTS `' . $markerTable . '`');
    @rmdir($stage); remove_shards(); @unlink(__FILE__);
    answer(true, 'Đã cài WordPress, Bricks và plugin.', array(
        'version'=>isset($GLOBALS['wp_version'])?$GLOBALS['wp_version']:'',
        'database'=>array('name'=>$data['dbName'],'user'=>$data['dbUser'],'host'=>$data['dbHost'])
    ));
} catch (Throwable $error) {
    if (!in_array($action, array('extract','verify'), true) && defined('OC_STAGE')) {
        remove_tree(__DIR__ . DIRECTORY_SEPARATOR . OC_STAGE);
    }
    answer(false, $error->getMessage(), array(), 500);
}
'''
    return (
        template.replace("__TOKEN__", _php_quote(token))
        .replace("__STAGE__", _php_quote(stage_name))
        .replace("__SHARD_PREFIX__", _php_quote(shard_prefix))
        .replace("__SHARD_COUNT__", str(max(1, min(16, len(shard_hashes)))))
        .replace("__SHARD_HASHES__", _php_quote(json.dumps(shard_hashes, separators=(",", ":"))))
    )


def _upload_install_shards(
    transport: FTPTransport,
    remote_root: str,
    shard_prefix: str,
    shards: list[InstallShard],
    callback: Callable[[int, int, int, int, str], None],
) -> list[str]:
    worker_count = max(1, min(16, int(transport.config.workers)))
    if len(shards) != worker_count:
        raise RuntimeError(f"Bộ cài phải có đúng {worker_count} gói độc lập.")
    sizes = [shard.path.stat().st_size for shard in shards]
    total = sum(sizes)
    remote_shards = [
        str(PurePosixPath(remote_root) / f"{shard_prefix}{index:03d}.zip")
        for index in range(worker_count)
    ]
    transferred = [0] * worker_count
    finished = 0
    progress_lock = threading.Lock()

    def report(index: int, completed: int, *, complete: bool = False) -> None:
        nonlocal finished
        with progress_lock:
            transferred[index] = completed
            if complete:
                finished += 1
            callback(sum(transferred), total, finished, worker_count, PurePosixPath(remote_shards[index]).name)

    def upload(index: int) -> None:
        last_error: Exception | None = None
        for attempt in range(1, transport.config.retries + 1):
            completed = 0
            report(index, 0)
            try:
                with ftp_transfer_slot():
                    client = transport._new_client()
                    try:
                        _ensure_remote_dir(client, str(PurePosixPath(remote_shards[index]).parent))
                        with shards[index].path.open("rb") as stream:
                            def sent(block: bytes) -> None:
                                nonlocal completed
                                completed += len(block)
                                report(index, completed)
                            client.storbinary(
                                f"STOR {remote_shards[index]}", stream,
                                blocksize=transport.config.block_size, callback=sent,
                            )
                    finally:
                        try:
                            client.quit()
                        except Exception:
                            client.close()
                report(index, sizes[index], complete=True)
                return
            except Exception as exc:
                last_error = exc
                if attempt < transport.config.retries:
                    time.sleep(attempt)
        raise RuntimeError(f"Không upload được gói {index + 1}/{worker_count}: {last_error}")

    try:
        with ThreadPoolExecutor(max_workers=worker_count, thread_name_prefix="oneclick-fresh-upload") as pool:
            futures = [pool.submit(upload, index) for index in range(worker_count)]
            for future in futures:
                future.result()
    except Exception:
        for remote_shard in remote_shards:
            rebuild_execute._delete_remote_file(transport, remote_shard)
        raise
    return remote_shards


def _upload_install_files(
    transport: FTPTransport,
    remote_stage: str,
    groups: list[InstallGroup],
    callback: Callable[[int, int, int, int, int, int, str], None],
) -> None:
    worker_count = max(1, min(16, int(transport.config.workers)))
    if len(groups) != worker_count:
        raise RuntimeError(f"Bộ cài phải có đúng {worker_count} nhóm file.")
    total_bytes = sum(item.size for group in groups for item in group.files)
    total_files = sum(len(group.files) for group in groups)
    transferred = [0] * worker_count
    files_completed = 0
    workers_completed = 0
    progress_lock = threading.Lock()

    def report(index: int, completed: int, current: str, *, file_done: bool = False, worker_done: bool = False) -> None:
        nonlocal files_completed, workers_completed
        with progress_lock:
            transferred[index] = completed
            if file_done:
                files_completed += 1
            if worker_done:
                workers_completed += 1
            callback(
                sum(transferred), total_bytes, files_completed, total_files,
                workers_completed, worker_count, current,
            )

    def upload_group(group: InstallGroup) -> None:
        group_completed = 0
        client = None

        def connect():
            fresh = transport._new_client()
            fresh.cwd(remote_stage)
            return fresh

        try:
            with ftp_transfer_slot():
                client = connect()
                for item in group.files:
                    last_error: Exception | None = None
                    for attempt in range(1, transport.config.retries + 1):
                        current_bytes = 0
                        try:
                            if client is None:
                                client = connect()
                            with io_path(item.path).open("rb") as stream:
                                def sent(block: bytes) -> None:
                                    nonlocal current_bytes
                                    current_bytes += len(block)
                                    report(group.index, group_completed + current_bytes, str(item.relative_path))
                                client.storbinary(
                                    f"STOR {item.relative_path}", stream,
                                    blocksize=transport.config.block_size, callback=sent,
                                )
                            group_completed += item.size
                            report(
                                group.index, group_completed, str(item.relative_path),
                                file_done=True,
                            )
                            last_error = None
                            break
                        except Exception as exc:
                            last_error = exc
                            report(group.index, group_completed, str(item.relative_path))
                            if client is not None:
                                try:
                                    client.close()
                                except Exception:
                                    pass
                                client = None
                            if attempt < transport.config.retries:
                                time.sleep(min(2, attempt))
                    if last_error is not None:
                        raise RuntimeError(
                            f"Không upload được {item.relative_path} sau {transport.config.retries} lần: {last_error}"
                        )
                report(group.index, group_completed, f"worker-{group.index + 1}", worker_done=True)
        finally:
            if client is not None:
                try:
                    client.quit()
                except Exception:
                    try:
                        client.close()
                    except Exception:
                        pass

    with ThreadPoolExecutor(max_workers=worker_count, thread_name_prefix="oneclick-direct-upload") as pool:
        futures = [pool.submit(upload_group, group) for group in groups]
        for future in futures:
            future.result()


def _invoke_bridge(site_url: str, bridge_name: str, token: str, payload: dict[str, Any]) -> dict[str, Any]:
    request = Request(
        f"{site_url.rstrip('/')}/{quote(bridge_name)}",
        data=json.dumps(payload, ensure_ascii=False).encode("utf-8"),
        headers={"Content-Type": "application/json", "X-OneClick-Token": token, "User-Agent": "OneClick-Fresh/1.0"},
        method="POST",
    )
    try:
        with urlopen(request, timeout=360) as response:
            body = response.read(1024 * 1024)
    except HTTPError as exc:
        body = exc.read(1024 * 1024)
    except URLError as exc:
        raise RuntimeError(f"Không gọi được file cài tạm qua HTTPS: {exc.reason}") from exc
    try:
        result = json.loads(body.decode("utf-8-sig"))
    except (UnicodeError, json.JSONDecodeError) as exc:
        raise RuntimeError("Hosting không trả về kết quả JSON hợp lệ khi cài WordPress.") from exc
    if not result.get("ok"):
        raise RuntimeError(str(result.get("message") or "Hosting không hoàn tất cài WordPress."))
    return result


def _run_install(name: str, job: FreshJob) -> None:
    # Do not prefix the bridge with a dot: many Apache/LiteSpeed templates deny
    # HTTP access to dotfiles.  The random name plus 256-bit header token keeps
    # the short-lived endpoint unguessable and authenticated.
    bridge_name = f"oneclick-install-{secrets.token_hex(8)}.php"
    shard_prefix = f".oneclick-shard-{secrets.token_hex(8)}-"
    stage_name = f".oneclick-stage-{secrets.token_hex(6)}"
    transport: FTPTransport | None = None
    remote_root = ""
    remote_shards: list[str] = []
    try:
        profile = _read_profile(name)
        secret_bundle = _secrets_for(name)
        transport = _transport(profile, secret_bundle["ftpPassword"])
        remote_root = str(profile["connection"]["remotePath"])
        job.update(stage="ftp", message="Đang kiểm tra thư mục hosting", percent=5)
        def wipe_progress(event: dict[str, Any]) -> None:
            completed = int(event.get("items_completed") or 0)
            current = str(event.get("current") or "")
            job.update(
                stage="wipe",
                message=f"Đang dọn nội dung website cũ · {completed} mục",
                percent=min(20, 7 + completed // 25),
                current=current,
            )

        clear_remote = bool(profile.get("clearRemote", False))
        if clear_remote:
            if not transport.directory_exists(remote_root):
                raise RuntimeError("Thư mục website trên hosting không tồn tại.")
            job.update(
                stage="wipe", message="Hosting sẽ tự dọn nội dung cũ", percent=9,
                current=remote_root,
            )
        else:
            _prepare_remote_root(
                transport, remote_root, clear_remote=False, progress=wipe_progress,
            )
        extract_workers = max(1, min(16, int(transport.config.workers)))
        groups, wordpress_version = build_install_tree(
            extract_workers,
            lambda stage, message, percent, current: job.update(
                stage=stage, message=message, percent=percent, current=current
            ),
        )
        controls = [group.control for group in groups]
        job.update(
            stage="prepare", message="Đang gửi manifest kiểm tra",
            percent=25, current=f"0/{extract_workers} phần",
        )
        def control_progress(done: int, total: int, finished: int, parts: int, current_part: str) -> None:
            job.update(
                stage="prepare", message="Đang gửi manifest kiểm tra",
                percent=25 + int(2 * done / max(1, total)),
                current=f"{finished}/{parts} phần · {current_part}",
            )
        remote_shards = _upload_install_shards(
            transport, remote_root, shard_prefix, controls, control_progress
        )
        token = secrets.token_hex(32)
        remote_bridge = str(PurePosixPath(remote_root) / bridge_name)
        rebuild_execute._upload_text(
            transport, remote_bridge,
            _installer_bridge(
                token, stage_name, shard_prefix,
                [control.sha256 for control in controls],
            ),
        )
        ftp_user = str(profile["connection"]["username"])
        ftp_password = secret_bundle["ftpPassword"]
        install_payload: dict[str, Any] = {
            "dbName": profile["database"]["name"], "dbUser": profile["database"]["user"],
            "dbPassword": ftp_password, "dbHost": profile["database"]["host"],
            "panelUser": ftp_user, "panelPassword": ftp_password,
            "databaseCandidates": [
                {"name": f"{ftp_user}_db", "user": f"{ftp_user}_user", "password": ftp_password, "host": profile["database"]["host"]},
                {"name": ftp_user, "user": ftp_user, "password": ftp_password, "host": profile["database"]["host"]},
            ],
            "tablePrefix": profile["database"]["tablePrefix"], "siteTitle": profile["admin"]["siteTitle"],
            "adminUser": profile["admin"]["username"], "adminEmail": profile["admin"]["email"],
            "adminPassword": ftp_password, "siteUrl": profile["siteUrl"],
            "installId": profile["installId"], "clearRemote": clear_remote,
            "extractWorkers": extract_workers,
        }

        def invoke(action: str, **extra: Any) -> dict[str, Any]:
            return _invoke_bridge(
                str(profile["siteUrl"]), bridge_name, token,
                {**install_payload, "action": action, **extra},
            )

        job.update(
            stage="prepare", message="Đang chuẩn bị thư mục hosting", percent=27,
            current=f"{extract_workers} nhóm upload trực tiếp",
        )
        heartbeat_stop = threading.Event()
        install_started = time.monotonic()
        def install_heartbeat() -> None:
            while not heartbeat_stop.wait(5):
                waited = int(time.monotonic() - install_started)
                job.update(
                    stage="prepare", message="Đang chuẩn bị thư mục hosting",
                    percent=27, current=f"Đã chờ {waited} giây",
                )
        threading.Thread(target=install_heartbeat, daemon=True, name=f"oneclick-fresh-heartbeat-{name}").start()
        try:
            prepared = invoke("prepare")
        finally:
            heartbeat_stop.set()
        prepared_database = prepared.get("database") if isinstance(prepared.get("database"), dict) else {}
        for payload_key, database_key in (("dbName", "name"), ("dbUser", "user"), ("dbHost", "host")):
            if prepared_database.get(database_key):
                install_payload[payload_key] = str(prepared_database[database_key])

        def worker_action(action: str, index: int) -> dict[str, Any]:
            last_error: Exception | None = None
            for attempt in range(2):
                try:
                    return invoke(action, workerIndex=index)
                except Exception as exc:
                    last_error = exc
                    if attempt == 0:
                        time.sleep(0.5)
            raise RuntimeError(f"Worker {action} {index + 1}/{extract_workers} lỗi: {last_error}")

        def parallel_action(action: str, label: str, start_percent: int, end_percent: int) -> None:
            completed = 0
            with ThreadPoolExecutor(max_workers=extract_workers, thread_name_prefix=f"oneclick-{action}") as pool:
                futures = [pool.submit(worker_action, action, index) for index in range(extract_workers)]
                for future in as_completed(futures):
                    future.result()
                    completed += 1
                    percent = start_percent + int((end_percent - start_percent) * completed / extract_workers)
                    job.update(
                        stage=action, message=label,
                        percent=percent, current=f"{completed}/{extract_workers} worker hoàn tất",
                    )

        try:
            # These tiny control ZIPs contain only signed JSON manifests. Source
            # WordPress is already extracted locally and is never unzipped here.
            parallel_action("extract", "Đang mở manifest kiểm tra", 28, 30)
            invoke("prepare-upload")
            remote_stage = str(PurePosixPath(remote_root) / stage_name)

            def file_progress(
                done: int, total: int, files_done: int, files_total: int,
                workers_done: int, workers_total: int, current_file: str,
            ) -> None:
                job.bytes_completed = done
                job.bytes_total = total
                job.update(
                    stage="upload", message=f"Đang upload trực tiếp bằng {workers_total} luồng",
                    percent=31 + int(51 * done / max(1, total)),
                    current=(
                        f"{files_done}/{files_total} file · {workers_done}/{workers_total} worker · "
                        f"{done / 1048576:.1f}/{total / 1048576:.1f} MB · {current_file}"
                    ),
                )

            _upload_install_files(transport, remote_stage, groups, file_progress)
            parallel_action("verify", "Đang kiểm tra file đã upload", 84, 94)
            job.update(stage="finalize", message="Đang hoàn tất WordPress", percent=96, current=profile["domain"])
            result = invoke("finalize")
        except Exception:
            try:
                invoke("cleanup")
            except Exception:
                pass
            raise
        job.status = "success"
        job.update(stage="complete", message="Cài WordPress hoàn tất", percent=100)
        with _LOCK:
            current = _read_profile(name)
            selected_database = result.get("database") if isinstance(result.get("database"), dict) else {}
            for key in ("name", "user", "host"):
                if selected_database.get(key):
                    current["database"][key] = str(selected_database[key])
            current["status"] = "success"
            current["installedAt"] = _now()
            current["wordpressVersion"] = str(result.get("version") or wordpress_version)
            current["lastError"] = ""
            _append_history(current, status="success", message="Cài WordPress, Bricks và plugin thành công")
            _save_profile(current)
    except Exception as exc:
        job.status = "error"
        job.error = str(exc)
        job.update(stage="error", message="Cài WordPress chưa hoàn tất", percent=max(1, job.percent))
        with _LOCK:
            current = _read_profile(name)
            current["status"] = "error"
            current["lastError"] = str(exc)
            _append_history(current, status="error", message=str(exc))
            _save_profile(current)
    finally:
        if transport is not None and remote_root:
            rebuild_execute._delete_remote_file(transport, str(PurePosixPath(remote_root) / bridge_name))
            for remote_shard in remote_shards:
                rebuild_execute._delete_remote_file(transport, remote_shard)


def start_install(name: str) -> dict[str, Any]:
    with _LOCK:
        profile = _read_profile(name)
        existing = FRESH_JOBS.get(name)
        if existing and existing.status == "running":
            raise RuntimeError("Website này đang được cài đặt.")
        job = FreshJob(site=name, status="running", stage="queued", message="Đang bắt đầu", percent=1, started_at=_now(), updated_at=_now())
        FRESH_JOBS[name] = job
        profile["status"] = "running"
        profile["lastError"] = ""
        profile["lastAttemptAt"] = _now()
        profile["attemptCount"] = int(profile.get("attemptCount") or 0) + 1
        _append_history(profile, status="running", message="Bắt đầu cài WordPress mới")
        _save_profile(profile)
        thread = threading.Thread(target=_run_install, args=(name, job), daemon=True, name=f"oneclick-fresh-{name}")
        thread.start()
        return job.to_dict()
