from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import sys
from typing import Iterable

from wpclean.clean_builder import wordpress_password_hash


INSERT_RE = re.compile(r"^INSERT INTO `([^`]+)` VALUES ")
PROFILE_META_KEYS = {
    "additional_profile_urls",
    "description",
    "facebook",
    "first_name",
    "last_name",
    "locale",
    "nickname",
    "twitter",
}


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _sql_tokens(line: str) -> list[str]:
    start = line.find("(")
    end = line.rfind(")")
    if start < 0 or end <= start:
        raise ValueError("SQL row does not contain a tuple")
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


def _unquote(token: str) -> str:
    token = token.strip()
    if len(token) >= 2 and token[0] == token[-1] == "'":
        token = token[1:-1]
    return token.replace("\\'", "'").replace("\\\\", "\\")


def _quote(value: str) -> str:
    escaped = (
        value.replace("\\", "\\\\")
        .replace("'", "\\'")
        .replace("\0", "\\0")
        .replace("\n", "\\n")
        .replace("\r", "\\r")
    )
    return f"'{escaped}'"


def _iter_table_rows(path: Path, table: str) -> Iterable[list[str]]:
    active = False
    with path.open("r", encoding="utf-8", errors="replace") as handle:
        for line in handle:
            match = INSERT_RE.match(line)
            if match:
                active = match.group(1) == table
            elif line.startswith(("-- Table:", "CREATE TABLE ", "DROP TABLE ", "SET ")):
                active = False
            if not active:
                continue
            stripped = line.strip()
            if "(" not in stripped or ")" not in stripped:
                continue
            yield _sql_tokens(stripped)
            if stripped.endswith(";"):
                active = False


def _find_prefix(path: Path) -> str:
    pattern = re.compile(r"^CREATE TABLE `(.+?)users` \(")
    with path.open("r", encoding="utf-8", errors="replace") as handle:
        for line in handle:
            match = pattern.match(line)
            if match:
                return match.group(1)
    raise ValueError("Could not detect WordPress users table")


def _current_admin_hash(clean_sql: Path, users_table: str) -> str:
    for tokens in _iter_table_rows(clean_sql, users_table):
        if len(tokens) >= 3 and _unquote(tokens[0]) == "1":
            return _unquote(tokens[2])
    raise ValueError("Clean SQL does not contain the injected administrator")


def _author_counts(original_sql: Path, posts_table: str) -> dict[int, int]:
    counts: dict[int, int] = {}
    for tokens in _iter_table_rows(original_sql, posts_table):
        if len(tokens) < 2:
            continue
        try:
            author_id = int(_unquote(tokens[1]))
        except ValueError:
            continue
        if author_id > 0:
            counts[author_id] = counts.get(author_id, 0) + 1
    return counts


def _build_author_rows(
    original_sql: Path,
    *,
    users_table: str,
    usermeta_table: str,
    posts_table: str,
    admin_hash: str,
    prefix: str,
) -> tuple[list[str], list[str], list[dict[str, object]]]:
    counts = _author_counts(original_sql, posts_table)
    author_ids = set(counts)
    user_rows: list[str] = []
    authors: list[dict[str, object]] = []

    for tokens in _iter_table_rows(original_sql, users_table):
        if len(tokens) != 10:
            continue
        user_id = int(_unquote(tokens[0]))
        if user_id not in author_ids:
            continue
        # Never restore an old credential or activation token from a compromised site.
        tokens[2] = _quote(admin_hash if user_id == 1 else wordpress_password_hash(secrets.token_urlsafe(48)))
        tokens[7] = "''"
        user_rows.append(f"INSERT INTO `{users_table}` VALUES ({','.join(tokens)});")
        authors.append(
            {
                "id": user_id,
                "login": _unquote(tokens[1]),
                "display_name": _unquote(tokens[9]),
                "email": _unquote(tokens[4]),
                "authored_rows": counts[user_id],
                "password_reset_required": user_id != 1,
            }
        )

    kept_ids = {int(item["id"]) for item in authors}
    role_keys = {f"{prefix}capabilities", f"{prefix}user_level"}
    allowed_keys = PROFILE_META_KEYS | role_keys
    meta_rows: list[str] = []
    for tokens in _iter_table_rows(original_sql, usermeta_table):
        if len(tokens) != 4:
            continue
        try:
            user_id = int(_unquote(tokens[1]))
        except ValueError:
            continue
        meta_key = _unquote(tokens[2])
        if user_id not in kept_ids or meta_key not in allowed_keys:
            continue
        # Let MySQL allocate fresh metadata IDs and avoid collisions.
        tokens[0] = "NULL"
        meta_rows.append(f"INSERT INTO `{usermeta_table}` VALUES ({','.join(tokens)});")

    if 1 not in kept_ids:
        raise ValueError("Original administrator ID 1 is not an authored user; refusing unsafe patch")
    return user_rows, meta_rows, sorted(authors, key=lambda item: int(item["id"]))


def _backup_once(source: Path, backup: Path) -> None:
    if backup.exists():
        return
    backup.parent.mkdir(parents=True, exist_ok=True)
    try:
        os.link(source, backup)
    except OSError:
        shutil.copy2(source, backup)


def _rewrite_clean_sql(clean_sql: Path, replacement_lines: list[str]) -> None:
    markers = {
        "-- WP Clean Rebuild: reset all WordPress users.",
        "-- WP Clean Rebuild: preserve users who own content; reset credentials.",
    }
    temp = clean_sql.with_suffix(clean_sql.suffix + ".author-patch.tmp")
    found = False
    try:
        with clean_sql.open("r", encoding="utf-8", errors="replace") as src, temp.open(
            "w", encoding="utf-8", newline="\n"
        ) as out:
            previous: str | None = None
            for line in src:
                if line.rstrip("\r\n") in markers:
                    if previous is not None and previous.strip().upper() != "SET FOREIGN_KEY_CHECKS=0;":
                        out.write(previous)
                    previous = None
                    found = True
                    break
                if previous is not None:
                    out.write(previous)
                previous = line
            if not found:
                if previous is not None:
                    out.write(previous)
                raise ValueError("Clean SQL user-reset marker was not found")
            out.write("-- WP Clean Rebuild: preserve users who own content; reset credentials.\n")
            for line in replacement_lines:
                out.write(line + "\n")
        os.replace(temp, clean_sql)
    finally:
        temp.unlink(missing_ok=True)


def _update_report(report_path: Path, authors: list[dict[str, object]]) -> None:
    if not report_path.is_file():
        return
    report = json.loads(report_path.read_text(encoding="utf-8"))
    report["user_policy"] = "preserve-content-authors-reset-credentials"
    report["preserved_authors"] = authors
    report["warnings"] = [
        warning
        for warning in report.get("warnings", [])
        if "Original users/usermeta" not in str(warning)
    ]
    report.setdefault("warnings", []).append(
        "Content authors were preserved with their original IDs; old passwords and sessions were not restored."
    )
    report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def _update_manifest(manifest_path: Path, changed: Iterable[Path], clean_root: Path) -> None:
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    entries = {entry["path"]: entry for entry in manifest.get("entries", [])}
    for path in changed:
        relative = path.relative_to(clean_root).as_posix()
        if relative not in entries:
            raise ValueError(f"Manifest has no entry for {relative}")
        entries[relative]["size"] = path.stat().st_size
        entries[relative]["sha256"] = _sha256(path)
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def patch_authors(backup_root: Path, backup_copy: Path) -> dict[str, object]:
    original_sql = backup_root / "database" / "original.sql"
    clean_root = backup_root / "clean"
    clean_sql = clean_root / "database" / "clean.sql"
    report_path = clean_root / "clean-report.json"
    manifest_path = clean_root / "manifest.json"
    for required in (original_sql, clean_sql, manifest_path):
        if not required.is_file():
            raise FileNotFoundError(required)

    prefix = _find_prefix(original_sql)
    users_table = f"{prefix}users"
    usermeta_table = f"{prefix}usermeta"
    posts_table = f"{prefix}posts"
    admin_hash = _current_admin_hash(clean_sql, users_table)
    user_rows, meta_rows, authors = _build_author_rows(
        original_sql,
        users_table=users_table,
        usermeta_table=usermeta_table,
        posts_table=posts_table,
        admin_hash=admin_hash,
        prefix=prefix,
    )

    _backup_once(clean_sql, backup_copy)
    replacement = [
        "SET FOREIGN_KEY_CHECKS=0;",
        f"DELETE FROM `{usermeta_table}`;",
        f"DELETE FROM `{users_table}`;",
        *user_rows,
        *meta_rows,
        "SET FOREIGN_KEY_CHECKS=1;",
    ]
    _rewrite_clean_sql(clean_sql, replacement)
    _update_report(report_path, authors)
    changed = [clean_sql]
    if report_path.is_file():
        changed.append(report_path)
    _update_manifest(manifest_path, changed, clean_root)

    return {
        "prefix": prefix,
        "authors": authors,
        "clean_sql": str(clean_sql),
        "clean_sql_size": clean_sql.stat().st_size,
        "clean_sql_sha256": _sha256(clean_sql),
        "backup_copy": str(backup_copy),
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("backup_root", type=Path)
    parser.add_argument("--backup-copy", required=True, type=Path)
    args = parser.parse_args()
    result = patch_authors(args.backup_root.resolve(), args.backup_copy.resolve())
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
