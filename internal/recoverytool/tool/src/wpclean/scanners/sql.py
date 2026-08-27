from __future__ import annotations

import re
from pathlib import Path
from typing import Callable
import time

from ..models import Finding, Signal
from ..risk import severity_for


RULES: list[tuple[re.Pattern[str], int, str, str]] = [
    (re.compile(r"\beval\s*\(", re.I), 30, "php.eval", "Contains eval(), often abused to execute injected code."),
    (re.compile(r"\bbase64_decode\s*\(", re.I), 15, "php.base64_decode", "Contains base64_decode(); suspicious only when combined with other signals."),
    (re.compile(r"\bgzinflate\s*\(", re.I), 20, "php.gzinflate", "Contains compressed-code expansion often used in obfuscation."),
    (re.compile(r"\bstr_rot13\s*\(", re.I), 15, "php.str_rot13", "Contains string obfuscation via str_rot13()."),
    (re.compile(r"<iframe[^>]+(?:display\s*:\s*none|width\s*=\s*[\"']?0)", re.I), 25, "html.hidden_iframe", "Contains a hidden iframe."),
    (re.compile(r"<script[^>]+src\s*=\s*[\"']https?://", re.I), 20, "html.remote_script", "Loads JavaScript from an external origin; requires domain review."),
    (re.compile(r"\b(?:shell_exec|passthru|system|exec)\s*\(", re.I), 25, "php.command_exec", "Contains OS command execution primitive."),
]


def scan_sql(path: Path, progress: Callable[[dict], None] | None = None) -> list[Finding]:
    findings: list[Finding] = []
    bytes_total = path.stat().st_size
    bytes_completed = 0
    started = time.monotonic()
    last_emit = started
    with path.open("r", encoding="utf-8", errors="replace") as fh:
        for line_no, line in enumerate(fh, start=1):
            bytes_completed += len(line.encode("utf-8", errors="replace"))
            signals: list[Signal] = []
            score = 0
            for regex, weight, name, reason in RULES:
                if regex.search(line):
                    signals.append(Signal(name=name, score=weight, reason=reason))
                    score += weight
            if len(signals) >= 2:
                score += 10
                signals.append(Signal("compound.obfuscation", 10, "Multiple independent suspicious signals appear in the same SQL record/line."))
            score = min(score, 100)
            if score >= 30:
                preview = line.strip()
                if len(preview) > 500:
                    preview = preview[:500] + "…"
                findings.append(Finding("database", f"{path}:{line_no}", score, severity_for(score), signals, preview, {"line": line_no}))
            now = time.monotonic()
            if progress and now - last_emit >= 0.8:
                elapsed = max(now - started, 0.001)
                rate = bytes_completed / elapsed
                progress(
                    {
                        "phase": "security_scan_database",
                        "location": "local",
                        "current_file": path.name,
                        "bytes_completed": min(bytes_completed, bytes_total),
                        "bytes_total": bytes_total,
                        "bytes_per_second": rate,
                        "eta_seconds": int((bytes_total - bytes_completed) / rate) if rate else None,
                        "unit": "byte",
                    }
                )
                last_emit = now
    return findings
