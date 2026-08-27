from __future__ import annotations

import base64
import hashlib
import hmac
import json
import os
import re
import secrets
import shutil
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Callable
import time

import bcrypt

from .backup import verify_manifest, verify_manifest_metadata, write_manifest
from .scanners.uploads import EXECUTABLE_SUFFIXES, scan_uploads
from .windows_paths import io_path


CleanProgressCallback = Callable[[dict], None]


CREATE_TABLE_RE = re.compile(
    r"CREATE TABLE(?: IF NOT EXISTS)?\s+`?([A-Za-z0-9_]+)`?\s*\(", re.I
)
INSERT_START_RE = re.compile(
    r"^(?:INSERT|REPLACE)\s+INTO\s+`?([A-Za-z0-9_]+)`?\s+VALUES\b", re.I
)


@dataclass(slots=True)
class CleanBuildReport:
    backup_root: str
    clean_root: str
    uploads_source: str
    uploads_clean: str
    uploads_restore_mode: str = "copy"
    uploads_copied: int = 0
    uploads_dropped: int = 0
    dropped_files: list[dict[str, str]] = field(default_factory=list)
    database_source: str = ""
    database_clean: str = ""
    table_prefix: str = ""
    admin_username: str = "admin"
    admin_email: str = ""
    password_source: str = "ftp-profile"
    user_policy: str = "preserve-content-authors-reset-credentials"
    preserved_authors: list[dict[str, object]] = field(default_factory=list)
    warnings: list[str] = field(default_factory=list)
    clean_manifest: str = ""
    clean_verified: bool = False


def _mysql_escape(value: str) -> str:
    return (
        value.replace("\\", "\\\\")
        .replace("\x00", "\\0")
        .replace("\n", "\\n")
        .replace("\r", "\\r")
        .replace("\x1a", "\\Z")
        .replace("'", "\\'")
    )


def wordpress_password_hash(password: str, *, rounds: int = 12) -> str:
    """Create a WordPress 6.8+ compatible bcrypt password hash.

    WordPress pre-hashes the trimmed password with HMAC-SHA384 using the
    domain-separation key ``wp-sha384``, base64-encodes it, bcrypts that value,
    then prefixes the bcrypt hash with ``$wp``.
    """
    trimmed = password.strip().encode("utf-8")
    digest = hmac.new(b"wp-sha384", trimmed, hashlib.sha384).digest()
    prehash = base64.b64encode(digest)
    encoded = bcrypt.hashpw(prehash, bcrypt.gensalt(rounds=rounds)).decode("ascii")
    return "$wp" + encoded


AUTHOR_PROFILE_META_KEYS = {
    "additional_profile_urls",
    "description",
    "facebook",
    "first_name",
    "last_name",
    "locale",
    "nickname",
    "twitter",
}


def _sql_row_tokens(line: str) -> list[str]:
    """Split one single-row MySQL VALUES tuple while respecting quoted commas."""
    start = line.find("(")
    end = line.rfind(")")
    if start < 0 or end <= start:
        raise ValueError("SQL row does not contain a VALUES tuple.")
    body = line[start + 1 : end]
    tokens: list[str] = []
    token: list[str] = []
    quoted = False
    escaped = False
    for char in body:
        if escaped:
            token.append(char)
            escaped = False
            continue
        if char == "\\" and quoted:
            token.append(char)
            escaped = True
            continue
        if char == "'":
            quoted = not quoted
            token.append(char)
            continue
        if char == "," and not quoted:
            tokens.append("".join(token).strip())
            token = []
            continue
        token.append(char)
    tokens.append("".join(token).strip())
    return tokens


def _sql_unquote(token: str) -> str:
    token = token.strip()
    if len(token) >= 2 and token[0] == token[-1] == "'":
        token = token[1:-1]
    return token.replace("\\'", "'").replace("\\\\", "\\")


def _sql_quote(value: str) -> str:
    return f"'{_mysql_escape(value)}'"


def _iter_table_rows(sql_path: Path, table: str):
    """Yield tokenized rows from bridge dumps that store one tuple per line."""
    active = False
    with sql_path.open("r", encoding="utf-8", errors="replace") as handle:
        for line in handle:
            match = INSERT_START_RE.match(line)
            if match:
                active = match.group(1) == table
            elif line.startswith(("-- Table:", "CREATE TABLE ", "DROP TABLE ", "SET ")):
                active = False
            if not active:
                continue
            stripped = line.strip()
            if "(" in stripped and ")" in stripped:
                yield _sql_row_tokens(stripped)
            if stripped.endswith(";"):
                active = False


def _preserve_content_authors(
    source: Path,
    *,
    prefix: str,
    admin_username: str,
    admin_email: str,
    admin_hash: str,
) -> tuple[list[str], list[str], list[dict[str, object]]]:
    users_table = f"{prefix}users"
    usermeta_table = f"{prefix}usermeta"
    posts_table = f"{prefix}posts"

    author_counts: dict[int, int] = {}
    for tokens in _iter_table_rows(source, posts_table):
        if len(tokens) < 2:
            continue
        try:
            author_id = int(_sql_unquote(tokens[1]))
        except ValueError:
            continue
        if author_id > 0:
            author_counts[author_id] = author_counts.get(author_id, 0) + 1

    disabled_author_hash = wordpress_password_hash(secrets.token_urlsafe(48))
    user_rows: list[str] = []
    authors: list[dict[str, object]] = []
    kept_ids: set[int] = set()
    for tokens in _iter_table_rows(source, users_table):
        if len(tokens) != 10:
            continue
        try:
            user_id = int(_sql_unquote(tokens[0]))
        except ValueError:
            continue
        if user_id not in author_counts:
            continue
        # Keep identity and ownership, but never restore old credentials or sessions.
        tokens[2] = _sql_quote(admin_hash if user_id == 1 else disabled_author_hash)
        tokens[7] = "''"
        user_rows.append(f"INSERT INTO `{users_table}` VALUES ({','.join(tokens)});")
        kept_ids.add(user_id)
        authors.append(
            {
                "id": user_id,
                "login": _sql_unquote(tokens[1]),
                "display_name": _sql_unquote(tokens[9]),
                "email": _sql_unquote(tokens[4]),
                "authored_rows": author_counts[user_id],
                "password_reset_required": user_id != 1,
            }
        )

    # ID 1 is the recovery administrator. Preserve it when it owns content;
    # otherwise create a clean administrator without restoring the old account.
    if 1 not in kept_ids:
        registered = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
        nicename = re.sub(r"[^a-z0-9_-]+", "-", admin_username.lower()).strip("-") or "admin"
        user_rows.insert(
            0,
            f"INSERT INTO `{users_table}` VALUES "
            f"('1',{_sql_quote(admin_username)},{_sql_quote(admin_hash)},{_sql_quote(nicename)},"
            f"{_sql_quote(admin_email)},'',{_sql_quote(registered)},'','0','Administrator');",
        )
        kept_ids.add(1)

    role_keys = {f"{prefix}capabilities", f"{prefix}user_level"}
    allowed_keys = AUTHOR_PROFILE_META_KEYS | role_keys
    meta_rows: list[str] = []
    roles_found: dict[int, set[str]] = {user_id: set() for user_id in kept_ids}
    for tokens in _iter_table_rows(source, usermeta_table):
        if len(tokens) != 4:
            continue
        try:
            user_id = int(_sql_unquote(tokens[1]))
        except ValueError:
            continue
        meta_key = _sql_unquote(tokens[2])
        if user_id not in kept_ids or meta_key not in allowed_keys:
            continue
        # Administrator privileges are rebuilt below instead of trusting backup metadata.
        if user_id == 1 and meta_key in role_keys:
            continue
        tokens[0] = "NULL"
        meta_rows.append(f"INSERT INTO `{usermeta_table}` VALUES ({','.join(tokens)});")
        if meta_key in role_keys:
            roles_found[user_id].add(meta_key)

    admin_capabilities = _mysql_escape('a:1:{s:13:"administrator";b:1;}')
    meta_rows.extend(
        [
            f"INSERT INTO `{usermeta_table}` VALUES "
            f"(NULL,'1','{_mysql_escape(prefix + 'capabilities')}','{admin_capabilities}');",
            f"INSERT INTO `{usermeta_table}` VALUES "
            f"(NULL,'1','{_mysql_escape(prefix + 'user_level')}','10');",
        ]
    )
    default_author_capabilities = _mysql_escape('a:1:{s:6:"author";b:1;}')
    for user_id in sorted(kept_ids - {1}):
        if f"{prefix}capabilities" not in roles_found[user_id]:
            meta_rows.append(
                f"INSERT INTO `{usermeta_table}` VALUES "
                f"(NULL,'{user_id}','{_mysql_escape(prefix + 'capabilities')}',"
                f"'{default_author_capabilities}');"
            )
        if f"{prefix}user_level" not in roles_found[user_id]:
            meta_rows.append(
                f"INSERT INTO `{usermeta_table}` VALUES "
                f"(NULL,'{user_id}','{_mysql_escape(prefix + 'user_level')}','2');"
            )

    authors.sort(key=lambda item: int(item["id"]))
    return user_rows, meta_rows, authors


def _tables_in_dump(sql_path: Path) -> set[str]:
    tables: set[str] = set()
    with sql_path.open("r", encoding="utf-8", errors="replace") as fh:
        for line in fh:
            match = CREATE_TABLE_RE.search(line)
            if match:
                tables.add(match.group(1))
    return tables


def detect_wordpress_prefix(sql_path: Path) -> tuple[str, set[str]]:
    tables = _tables_in_dump(sql_path)
    candidates: list[tuple[int, str]] = []
    for table in tables:
        if not table.endswith("users"):
            continue
        prefix = table[: -len("users")]
        score = 0
        for required in ("users", "usermeta", "options", "posts"):
            if f"{prefix}{required}" in tables:
                score += 1
        if score >= 3:
            candidates.append((score, prefix))

    if not candidates:
        raise ValueError("Could not detect the WordPress table prefix from the SQL dump.")

    candidates.sort(reverse=True)
    best_score = candidates[0][0]
    best = sorted(prefix for score, prefix in candidates if score == best_score)
    if len(best) != 1:
        raise ValueError(f"Ambiguous WordPress table prefix candidates: {', '.join(best)}")

    prefix = best[0]
    if f"{prefix}blogs" in tables or f"{prefix}sitemeta" in tables:
        raise ValueError(
            "WordPress multisite detected. Automatic single-admin reset is intentionally blocked "
            "until multisite super-admin semantics are implemented."
        )
    return prefix, tables


def _verify_standard_user_schema(sql_path: Path, prefix: str) -> None:
    text = sql_path.read_text(encoding="utf-8", errors="replace")
    users_marker = re.search(
        rf"CREATE TABLE(?: IF NOT EXISTS)?\s+`?{re.escape(prefix)}users`?\s*\((.*?)\)\s*ENGINE=",
        text,
        re.I | re.S,
    )
    meta_marker = re.search(
        rf"CREATE TABLE(?: IF NOT EXISTS)?\s+`?{re.escape(prefix)}usermeta`?\s*\((.*?)\)\s*ENGINE=",
        text,
        re.I | re.S,
    )
    if not users_marker or not meta_marker:
        raise ValueError("Could not verify wp_users/wp_usermeta table schemas.")

    users_sql = users_marker.group(1).lower()
    required_users = (
        "`id`",
        "`user_login`",
        "`user_pass`",
        "`user_nicename`",
        "`user_email`",
        "`user_url`",
        "`user_registered`",
        "`user_activation_key`",
        "`user_status`",
        "`display_name`",
    )
    if any(column not in users_sql for column in required_users):
        raise ValueError("wp_users schema is non-standard; refusing to inject an assumed row layout.")

    meta_sql = meta_marker.group(1).lower()
    required_meta = ("`umeta_id`", "`user_id`", "`meta_key`", "`meta_value`")
    if any(column not in meta_sql for column in required_meta):
        raise ValueError("wp_usermeta schema is non-standard; refusing to inject an assumed row layout.")


def _copy_upload_file_resilient(path: Path, target: Path, relative: Path) -> None:
    """Copy one clean upload, tolerating a transient Windows path race once.

    Antivirus software can inspect/quarantine files in the backup directory while
    clean staging is being built. If the source still exists, recreate the target
    parent and retry once. If the source itself disappeared, stop with an operator-
    friendly integrity error instead of silently producing an incomplete restore.
    """
    for attempt in range(2):
        try:
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(path, target)
            return
        except FileNotFoundError as exc:
            if not path.is_file():
                rel = relative.as_posix()
                raise RuntimeError(
                    f"File backup đã biến mất khi tạo dữ liệu sạch: uploads/{rel}. "
                    "Có thể antivirus đã cách ly/xóa file trong thư mục backups. "
                    "Backup gốc không còn nguyên vẹn; hãy khôi phục file từ quarantine "
                    "hoặc chạy backup lại rồi bấm Thử lại."
                ) from exc
            if attempt == 0:
                continue
            rel = relative.as_posix()
            raise RuntimeError(
                f"Windows không tạo được đường dẫn clean staging cho uploads/{rel}. "
                "File nguồn vẫn tồn tại; hãy kiểm tra quyền ghi local, antivirus hoặc giới hạn đường dẫn rồi thử lại."
            ) from exc


def _copy_clean_uploads(
    source: Path,
    destination: Path,
    progress: CleanProgressCallback | None = None,
) -> tuple[int, list[dict[str, str]]]:
    filesystem_source = io_path(source)
    filesystem_destination = io_path(destination)
    findings = scan_uploads(filesystem_source, progress=progress)
    drop_reasons: dict[Path, str] = {}
    for finding in findings:
        finding_path = Path(finding.location).resolve()
        if finding.score >= 60 or "DROP FROM CLEAN RESTORE" in finding.recommended_action:
            reasons = "; ".join(signal.name for signal in finding.signals) or "scanner policy"
            drop_reasons[finding_path] = reasons

    if filesystem_destination.exists():
        shutil.rmtree(filesystem_destination)
    filesystem_destination.mkdir(parents=True, exist_ok=True)

    copied = 0
    dropped: list[dict[str, str]] = []
    files = [path for path in filesystem_source.rglob("*") if path.is_file()]
    total_bytes = sum(path.stat().st_size for path in files)
    completed_bytes = 0
    for index, path in enumerate(files, 1):
        if not path.is_file():
            continue
        relative = path.relative_to(filesystem_source)
        reason: str | None = None
        suffix = path.suffix.lower()

        if suffix == ".zip":
            reason = "archive policy: ZIP is not restored"
        elif suffix in EXECUTABLE_SUFFIXES:
            reason = "executable file under uploads"
        elif path.resolve() in drop_reasons:
            reason = drop_reasons[path.resolve()]

        if reason:
            dropped.append({"path": relative.as_posix(), "reason": reason})
        else:
            target = filesystem_destination / relative
            _copy_upload_file_resilient(path, target, relative)
            copied += 1
        completed_bytes += path.stat().st_size
        if progress:
            progress(
                {
                    "phase": "clean_uploads",
                    "location": "local",
                    "current_file": relative.as_posix(),
                    "files_completed": index,
                    "files_total": len(files),
                    "bytes_completed": completed_bytes,
                    "bytes_total": total_bytes,
                    "unit": "file",
                }
            )

    return copied, dropped


def _plan_clean_uploads(
    source: Path,
    progress: CleanProgressCallback | None = None,
) -> tuple[int, list[dict[str, str]]]:
    """Build a restore allowlist without duplicating the complete uploads tree."""
    filesystem_source = io_path(source)
    findings = scan_uploads(filesystem_source)
    drop_reasons: dict[Path, str] = {}
    for finding in findings:
        finding_path = Path(finding.location).resolve()
        if finding.score >= 60 or "DROP FROM CLEAN RESTORE" in finding.recommended_action:
            drop_reasons[finding_path] = (
                "; ".join(signal.name for signal in finding.signals) or "scanner policy"
            )

    files = [path for path in filesystem_source.rglob("*") if path.is_file()]
    total_bytes = sum(path.stat().st_size for path in files)
    completed_bytes = 0
    kept = 0
    dropped: list[dict[str, str]] = []
    for index, path in enumerate(files, 1):
        relative = path.relative_to(filesystem_source)
        reason: str | None = None
        if path.suffix.lower() == ".zip":
            reason = "archive policy: ZIP is not restored"
        elif path.suffix.lower() in EXECUTABLE_SUFFIXES:
            reason = "executable file under uploads"
        elif path.resolve() in drop_reasons:
            reason = drop_reasons[path.resolve()]
        if reason:
            dropped.append({"path": relative.as_posix(), "reason": reason})
        else:
            kept += 1
        completed_bytes += path.stat().st_size
        if progress:
            progress(
                {
                    "phase": "clean_uploads_plan",
                    "location": "local",
                    "current_file": relative.as_posix(),
                    "files_completed": index,
                    "files_total": len(files),
                    "bytes_completed": completed_bytes,
                    "bytes_total": total_bytes,
                    "unit": "file",
                }
            )
    return kept, dropped


def _write_clean_database(
    source: Path,
    destination: Path,
    *,
    prefix: str,
    tables: set[str],
    admin_username: str,
    admin_email: str,
    admin_password: str,
    progress: CleanProgressCallback | None = None,
) -> list[dict[str, object]]:
    _verify_standard_user_schema(source, prefix)
    destination.parent.mkdir(parents=True, exist_ok=True)

    users_table = f"{prefix}users"
    usermeta_table = f"{prefix}usermeta"
    skip_statement = False
    password_hash = wordpress_password_hash(admin_password)
    user_rows, meta_rows, preserved_authors = _preserve_content_authors(
        source,
        prefix=prefix,
        admin_username=admin_username,
        admin_email=admin_email,
        admin_hash=password_hash,
    )

    with source.open("r", encoding="utf-8", errors="replace") as src, destination.open(
        "w", encoding="utf-8", newline="\n"
    ) as out:
        out.write("-- WP Clean Rebuild sanitized database\n")
        out.write("-- Content authors preserved; old credentials and sessions removed.\n")

        bytes_completed = 0
        bytes_total = source.stat().st_size
        last_emit = time.monotonic()
        for line in src:
            bytes_completed += len(line.encode("utf-8", errors="replace"))
            if not skip_statement:
                match = INSERT_START_RE.match(line)
                if match and match.group(1) in {users_table, usermeta_table}:
                    skip_statement = not line.rstrip().endswith(";")
                    continue
                out.write(line)
            else:
                if line.rstrip().endswith(";"):
                    skip_statement = False
                continue
            now = time.monotonic()
            if progress and now - last_emit >= 0.8:
                progress(
                    {
                        "phase": "clean_database",
                        "location": "local",
                        "current_file": source.name,
                        "bytes_completed": min(bytes_completed, bytes_total),
                        "bytes_total": bytes_total,
                        "unit": "byte",
                    }
                )
                last_emit = now

        # The dump may have restored FOREIGN_KEY_CHECKS=1 immediately before
        # this appended policy block. Plugin tables commonly reference wp_users,
        # so reset identities under an explicit FK guard and restore checks last.
        out.write("\nSET FOREIGN_KEY_CHECKS=0;\n")
        out.write("-- WP Clean Rebuild: preserve users who own content; reset credentials.\n")
        out.write(f"DELETE FROM `{usermeta_table}`;\n")
        out.write(f"DELETE FROM `{users_table}`;\n")
        for row in user_rows:
            out.write(row + "\n")
        for row in meta_rows:
            out.write(row + "\n")

        if f"{prefix}comments" in tables:
            out.write(f"UPDATE `{prefix}comments` SET `user_id`=0 WHERE `user_id`<>0;\n")
        if f"{prefix}links" in tables:
            out.write(f"UPDATE `{prefix}links` SET `link_owner`=1 WHERE `link_owner`<>0;\n")
        out.write("SET FOREIGN_KEY_CHECKS=1;\n")
    return preserved_authors


def build_clean_restore(
    backup_root: Path,
    *,
    ftp_password: str,
    host: str,
    admin_username: str = "admin",
    admin_email: str | None = None,
    progress: CleanProgressCallback | None = None,
    verify_original: bool = True,
    reference_uploads: bool = False,
) -> CleanBuildReport:
    manifest = backup_root / "manifest.json"
    if not manifest.is_file():
        raise ValueError("Original backup manifest.json is missing; clean staging is blocked.")
    verifier = verify_manifest if verify_original else verify_manifest_metadata
    ok, problems = verifier(backup_root, manifest)
    if not ok:
        raise ValueError("Original backup verification failed: " + "; ".join(problems))

    if not ftp_password:
        raise ValueError("FTP password is empty; cannot create the requested administrator password.")

    uploads_source = backup_root / "uploads"
    db_source = backup_root / "database" / "original.sql"
    if not uploads_source.is_dir():
        raise ValueError("Original uploads backup is missing.")
    if not db_source.is_file():
        raise ValueError("Original database/original.sql is missing.")

    clean_root = backup_root / "clean"
    uploads_clean = clean_root / "uploads"
    database_clean = clean_root / "database" / "clean.sql"
    report_path = clean_root / "clean-report.json"

    prefix, tables = detect_wordpress_prefix(db_source)
    resolved_email = admin_email or f"admin@{host}"

    if reference_uploads:
        if io_path(uploads_clean).exists():
            shutil.rmtree(io_path(uploads_clean))
        copied, dropped = _plan_clean_uploads(uploads_source, progress)
        uploads_restore = uploads_source
        restore_mode = "reference"
    else:
        copied, dropped = _copy_clean_uploads(uploads_source, uploads_clean, progress)
        uploads_restore = uploads_clean
        restore_mode = "copy"
    preserved_authors = _write_clean_database(
        db_source,
        database_clean,
        prefix=prefix,
        tables=tables,
        admin_username=admin_username,
        admin_email=resolved_email,
        admin_password=ftp_password,
        progress=progress,
    )

    report = CleanBuildReport(
        backup_root=str(backup_root),
        clean_root=str(clean_root),
        uploads_source=str(uploads_source),
        uploads_clean=str(uploads_restore),
        uploads_restore_mode=restore_mode,
        uploads_copied=copied,
        uploads_dropped=len(dropped),
        dropped_files=dropped,
        database_source=str(db_source),
        database_clean=str(database_clean),
        table_prefix=prefix,
        admin_username=admin_username,
        admin_email=resolved_email,
        preserved_authors=preserved_authors,
        warnings=[
            "Administrator password intentionally reuses the FTP credential as requested; rotate both after recovery.",
            "Content authors keep their IDs/profile names; old passwords and sessions are not restored.",
            "The original backup is immutable evidence and was not modified by this clean staging operation.",
        ],
    )

    clean_root.mkdir(parents=True, exist_ok=True)
    report.clean_manifest = str(clean_root / "manifest.json")
    report.clean_verified = True
    report_path.write_text(json.dumps(asdict(report), indent=2, ensure_ascii=False), encoding="utf-8")
    clean_manifest = write_manifest(clean_root, progress=progress)
    clean_ok, clean_problems = verify_manifest(clean_root, clean_manifest, progress=progress)
    if not clean_ok:
        report.clean_verified = False
        report_path.write_text(json.dumps(asdict(report), indent=2, ensure_ascii=False), encoding="utf-8")
        raise RuntimeError("Clean staging manifest verification failed: " + "; ".join(clean_problems))
    report.clean_manifest = str(clean_manifest)
    report.clean_verified = True
    return report
