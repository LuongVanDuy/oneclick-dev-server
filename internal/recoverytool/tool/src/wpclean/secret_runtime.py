from __future__ import annotations

import os
from pathlib import Path
import re
import json


PROJECT_RE = re.compile(r"^[a-zA-Z0-9._-]+$")


def _secret_path(project_name: str) -> Path | None:
    root = os.environ.get("WPCLEAN_SECRET_DIR", "").strip()
    if not root or not PROJECT_RE.fullmatch(project_name):
        return None
    directory = Path(root).resolve()
    candidate = (directory / f"{project_name.lower()}.secret").resolve()
    candidate.relative_to(directory)
    return candidate


def remember_runtime_secret(project_name: str, password: str) -> None:
    path = _secret_path(project_name)
    if path is None or not password:
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(".tmp")
    temporary.write_text(password, encoding="utf-8")
    try:
        os.chmod(temporary, 0o600)
    except OSError:
        pass
    temporary.replace(path)


def load_runtime_secret(project_name: str) -> str | None:
    path = _secret_path(project_name)
    if path is None:
        return None
    try:
        value = path.read_text(encoding="utf-8")
    except OSError:
        return None
    return value if value else None


def forget_runtime_secret(project_name: str) -> None:
    path = _secret_path(project_name)
    if path is None:
        return
    path.unlink(missing_ok=True)


def remember_runtime_secret_bundle(project_name: str, values: dict[str, str]) -> None:
    """Store a short-lived JSON secret bundle inside OneClick's protected runtime.

    Fresh WordPress installs need three unrelated passwords.  The persistent
    project JSON intentionally never receives them; the Go proxy moves this
    bundle into the operating-system credential vault after the request.
    """
    cleaned = {str(key): str(value) for key, value in values.items() if value}
    if not cleaned:
        return
    remember_runtime_secret(project_name, json.dumps(cleaned, ensure_ascii=False))


def load_runtime_secret_bundle(project_name: str) -> dict[str, str]:
    value = load_runtime_secret(project_name)
    if not value:
        return {}
    try:
        raw = json.loads(value)
    except json.JSONDecodeError:
        return {}
    if not isinstance(raw, dict):
        return {}
    return {str(key): str(item) for key, item in raw.items() if item is not None}
