from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path, PurePosixPath
from typing import Callable
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen
import hashlib
import io
import json
import re
import secrets
import time

from .site_config import SiteConnectionProfile
from .transport import FTPTransport


ProgressCallback = Callable[[dict], None]
_EXPORT_TEMP = re.compile(r"^wpclean-db-[0-9a-f]{16}\.(?:php|dat|state(?:\.tmp)?)$")
_INI_BOOTSTRAP = re.compile(
    r"^\s*auto_(?:pre|ap)pend_file\s*=\s*(?:\"([^\"]+)\"|'([^']+)'|([^;\r\n]+))",
    re.I,
)
_HTACCESS_BOOTSTRAP = re.compile(
    r"^\s*php_(?:admin_)?value\s+auto_(?:pre|ap)pend_file\s+(?:\"([^\"]+)\"|'([^']+)'|([^#\r\n]+))",
    re.I,
)
_BOOTSTRAP_CONFIG_NAMES = (".user.ini", "php.ini", ".htaccess")
_PLACEHOLDER = "<?php\n/* WP Clean Rebuild: harmless placeholder for a stale PHP bootstrap cache. */\n"


@dataclass(slots=True)
class DatabaseBridgeResult:
    sql_path: Path
    sha256: str
    bytes_downloaded: int
    elapsed_seconds: float
    bridge_removed: bool
    data_removed: bool = False
    state_removed: bool = False
    bootstrap_placeholders: list[str] = field(default_factory=list)


def _php_bridge(token: str, data_name: str, state_name: str) -> str:
    """Build a resumable database dumper that writes SQL on the hosting disk."""
    return r'''<?php
@set_time_limit(0);
@ini_set('memory_limit', '256M');
@ini_set('display_errors', '0');
@ignore_user_abort(true);
header('Content-Type: application/json; charset=utf-8');
header('Cache-Control: no-store');

const WPCLEAN_TOKEN = '__TOKEN__';
const WPCLEAN_DATA = '__DATA__';
const WPCLEAN_STATE = '__STATE__';
const WPCLEAN_ROWS_PER_QUERY = 25;
const WPCLEAN_MAX_BATCH_ROWS = 100;
const WPCLEAN_MAX_BATCH_SECONDS = 7.0;

$GLOBALS['wpclean_responded'] = false;
$GLOBALS['wpclean_step'] = 'bootstrap';

function encode_payload($payload) {
    $flags = defined('JSON_INVALID_UTF8_SUBSTITUTE') ? JSON_INVALID_UTF8_SUBSTITUTE : 0;
    $encoded = json_encode($payload, $flags);
    return $encoded === false ? '{"ok":false,"message":"response encoding failed"}' : $encoded;
}

function respond($ok, $message, $extra = array(), $status = 200) {
    $GLOBALS['wpclean_responded'] = true;
    http_response_code($status);
    echo encode_payload(array_merge(array('ok' => $ok, 'message' => $message), $extra));
    exit;
}

register_shutdown_function(function () {
    if (!empty($GLOBALS['wpclean_responded'])) { return; }
    $error = error_get_last();
    http_response_code(500);
    echo encode_payload(array(
        'ok' => false,
        'message' => 'PHP stopped during database export',
        'step' => $GLOBALS['wpclean_step'],
        'error' => $error ? $error['message'] : 'request stopped without a PHP fatal error',
        'line' => $error ? (int) $error['line'] : 0,
    ));
});

function save_state($path, $state) {
    $temporary = $path . '.tmp';
    if (@file_put_contents($temporary, encode_payload($state), LOCK_EX) === false) { return false; }
    if (@rename($temporary, $path)) { return true; }
    @unlink($path);
    if (@rename($temporary, $path)) { return true; }
    @unlink($temporary);
    return false;
}

function append_chunk($fh, $chunk) {
    $length = strlen($chunk);
    $written = 0;
    while ($written < $length) {
        $count = fwrite($fh, substr($chunk, $written));
        if ($count === false || $count === 0) { return false; }
        $written += $count;
    }
    return true;
}

function read_define($source, $name) {
    $quoted = preg_quote($name, '/');
    $pattern = '/define\s*\(\s*[\'\"]' . $quoted . '[\'\"]\s*,\s*([\'\"])(.*?)\1\s*\)\s*;/s';
    if (!preg_match($pattern, $source, $matches)) { return null; }
    return stripcslashes($matches[2]);
}

function sql_value($db, $field, $value) {
    if ($value === null) { return 'NULL'; }
    if ($field && $field->type === MYSQLI_TYPE_BIT) {
        $bitValue = (string) $value;
        return preg_match('/^[0-9]+$/D', $bitValue) ? $bitValue : '0x' . bin2hex($bitValue);
    }
    return "'" . $db->real_escape_string($value) . "'";
}

function checkpoint($fh, $statePath, &$state) {
    @fflush($fh);
    $position = ftell($fh);
    if ($position === false) { respond(false, 'failed to read dump position', array('step' => $GLOBALS['wpclean_step']), 500); }
    $state['dump_bytes'] = (int) $position;
    if (!save_state($statePath, $state)) {
        respond(false, 'failed to save database export checkpoint', array('step' => $GLOBALS['wpclean_step']), 500);
    }
}

$provided = isset($_SERVER['HTTP_X_WPCLEAN_TOKEN']) ? (string) $_SERVER['HTTP_X_WPCLEAN_TOKEN'] : '';
if ($provided === '' || !hash_equals(WPCLEAN_TOKEN, $provided)) {
    respond(false, 'unauthorized', array(), 403);
}

$GLOBALS['wpclean_step'] = 'wp-config';
$config = @file_get_contents(__DIR__ . '/wp-config.php');
if ($config === false) { respond(false, 'wp-config.php not readable', array(), 500); }
$dbName = read_define($config, 'DB_NAME');
$dbUser = read_define($config, 'DB_USER');
$dbPass = read_define($config, 'DB_PASSWORD');
$dbHost = read_define($config, 'DB_HOST');
if ($dbName === null || $dbUser === null || $dbPass === null || $dbHost === null) {
    respond(false, 'unsupported wp-config database credential format', array(), 500);
}

$host = $dbHost;
$port = 3306;
$socket = null;
if (strpos($dbHost, ':') !== false) {
    if (preg_match('/^(.+):(\d+)$/', $dbHost, $m)) {
        $host = $m[1];
        $port = (int) $m[2];
    } elseif (preg_match('/^(.+):(.+\.sock)$/', $dbHost, $m)) {
        $host = $m[1];
        $socket = $m[2];
    }
}

$GLOBALS['wpclean_step'] = 'database-connect';
mysqli_report(MYSQLI_REPORT_OFF);
$db = @new mysqli($host, $dbUser, $dbPass, $dbName, $port, $socket);
if ($db->connect_errno) {
    respond(false, 'database connection failed', array('errno' => $db->connect_errno), 500);
}
$db->set_charset('utf8mb4');

$GLOBALS['wpclean_step'] = 'table-inventory';
$tablesResult = $db->query("SHOW FULL TABLES WHERE Table_type = 'BASE TABLE'");
if (!$tablesResult) {
    respond(false, 'failed to enumerate tables', array('errno' => $db->errno, 'error' => $db->error), 500);
}
$tables = array();
while ($tableRow = $tablesResult->fetch_row()) { $tables[] = $tableRow[0]; }
$tablesResult->free();

$dataPath = __DIR__ . '/' . WPCLEAN_DATA;
$statePath = __DIR__ . '/' . WPCLEAN_STATE;
$state = array(
    'table_index' => 0,
    'row_offset' => 0,
    'table_started' => false,
    'rows' => 0,
    'statements' => 0,
    'dump_bytes' => 0,
    'header_written' => false,
    'done' => false,
    'total_tables' => count($tables),
    'current_table' => '',
);
if (is_file($statePath)) {
    $rawState = @file_get_contents($statePath);
    $loadedState = $rawState === false ? null : json_decode($rawState, true);
    if (!is_array($loadedState) || !isset($loadedState['table_index']) || !isset($loadedState['dump_bytes'])) {
        respond(false, 'database export checkpoint is invalid', array(), 500);
    }
    $state = array_merge($state, $loadedState);
}
$state['total_tables'] = count($tables);

$dump = @fopen($dataPath, 'c+b');
if (!$dump) { respond(false, 'temporary database dump is not writable', array(), 500); }
if (!@flock($dump, LOCK_EX)) {
    fclose($dump);
    respond(false, 'failed to lock temporary database dump', array(), 500);
}
$checkpointBytes = max(0, (int) $state['dump_bytes']);
if (!@ftruncate($dump, $checkpointBytes) || @fseek($dump, $checkpointBytes, SEEK_SET) !== 0) {
    fclose($dump);
    respond(false, 'failed to restore database dump checkpoint', array('dump_bytes' => $checkpointBytes), 500);
}
if (!empty($state['done'])) {
    fclose($dump);
    $db->close();
    respond(true, 'database export completed', $state);
}

if (empty($state['header_written'])) {
    $GLOBALS['wpclean_step'] = 'dump-header';
    $header = "-- WP Clean Rebuild database backup\n"
        . "-- Generated: " . gmdate('c') . "\n"
        . "SET NAMES utf8mb4;\n"
        . "SET FOREIGN_KEY_CHECKS=0;\n\n";
    if (!append_chunk($dump, $header)) { respond(false, 'failed to write dump header', array(), 500); }
    $state['header_written'] = true;
    $state['statements'] = (int) $state['statements'] + 2;
    checkpoint($dump, $statePath, $state);
}

$startedAt = microtime(true);
$batchRows = 0;
while ((int) $state['table_index'] < count($tables)) {
    if ($batchRows >= WPCLEAN_MAX_BATCH_ROWS || (microtime(true) - $startedAt) >= WPCLEAN_MAX_BATCH_SECONDS) { break; }

    $tableIndex = (int) $state['table_index'];
    $table = $tables[$tableIndex];
    $escapedTable = str_replace('`', '``', $table);
    $state['current_table'] = $table;

    if (empty($state['table_started'])) {
        $GLOBALS['wpclean_step'] = 'table-structure';
        $createResult = $db->query('SHOW CREATE TABLE `' . $escapedTable . '`');
        if (!$createResult) {
            respond(false, 'failed to read table structure', array('table' => $table, 'errno' => $db->errno, 'error' => $db->error), 500);
        }
        $createRow = $createResult->fetch_row();
        $createResult->free();
        $schema = "-- Table: `" . $escapedTable . "`\n"
            . "DROP TABLE IF EXISTS `" . $escapedTable . "`;\n"
            . $createRow[1] . ";\n\n";
        if (!append_chunk($dump, $schema)) {
            respond(false, 'failed to write table structure', array('table' => $table), 500);
        }
        $state['table_started'] = true;
        $state['statements'] = (int) $state['statements'] + 2;
        checkpoint($dump, $statePath, $state);
    }

    $GLOBALS['wpclean_step'] = 'table-data';
    $offset = max(0, (int) $state['row_offset']);
    $query = 'SELECT * FROM `' . $escapedTable . '` LIMIT ' . WPCLEAN_ROWS_PER_QUERY . ' OFFSET ' . $offset;
    $dataResult = $db->query($query, MYSQLI_STORE_RESULT);
    if (!$dataResult) {
        respond(false, 'failed to read table data', array('table' => $table, 'offset' => $offset, 'errno' => $db->errno, 'error' => $db->error), 500);
    }
    $fields = $dataResult->fetch_fields();
    $rows = array();
    while ($row = $dataResult->fetch_row()) {
        $values = array();
        for ($i = 0; $i < count($row); $i++) {
            $values[] = sql_value($db, isset($fields[$i]) ? $fields[$i] : null, $row[$i]);
        }
        $rows[] = '(' . implode(',', $values) . ')';
    }
    $dataResult->free();
    $rowCount = count($rows);

    if ($rowCount > 0) {
        $insert = 'INSERT INTO `' . $escapedTable . '` VALUES ' . implode(",\n", $rows) . ";\n";
        if (!append_chunk($dump, $insert)) {
            respond(false, 'failed to write table data', array('table' => $table, 'offset' => $offset), 500);
        }
        $state['row_offset'] = $offset + $rowCount;
        $state['rows'] = (int) $state['rows'] + $rowCount;
        $state['statements'] = (int) $state['statements'] + 1;
        $batchRows += $rowCount;
        checkpoint($dump, $statePath, $state);
    }

    if ($rowCount < WPCLEAN_ROWS_PER_QUERY) {
        if (!append_chunk($dump, "\n")) {
            respond(false, 'failed to finish table dump', array('table' => $table), 500);
        }
        $state['table_index'] = $tableIndex + 1;
        $state['row_offset'] = 0;
        $state['table_started'] = false;
        $state['current_table'] = '';
        checkpoint($dump, $statePath, $state);
    }
}

if ((int) $state['table_index'] >= count($tables)) {
    $GLOBALS['wpclean_step'] = 'dump-footer';
    if (!append_chunk($dump, "SET FOREIGN_KEY_CHECKS=1;\n")) {
        respond(false, 'failed to write dump footer', array(), 500);
    }
    $state['statements'] = (int) $state['statements'] + 1;
    $state['done'] = true;
    $state['current_table'] = '';
    checkpoint($dump, $statePath, $state);
}

fclose($dump);
$db->close();
respond(true, !empty($state['done']) ? 'database export completed' : 'database export batch completed', array_merge($state, array('batch_rows' => $batchRows)));
'''.replace("__TOKEN__", token).replace("__DATA__", data_name).replace("__STATE__", state_name)


def _ensure_remote_dir(client, remote_dir: str) -> None:
    path = PurePosixPath(remote_dir)
    if str(path) in {"", ".", "/"}:
        return
    current = "/" if str(path).startswith("/") else client.pwd()
    for part in path.parts:
        if part in {"", "/"}:
            continue
        current = str(PurePosixPath(current) / part)
        try:
            client.cwd(current)
        except Exception:
            try:
                client.mkd(current)
            except Exception:
                pass
            client.cwd(current)


def _upload_text(transport: FTPTransport, remote_path: str, content: str) -> None:
    client = transport._new_client()
    try:
        _ensure_remote_dir(client, str(PurePosixPath(remote_path).parent))
        client.storbinary(
            f"STOR {remote_path}",
            io.BytesIO(content.encode("utf-8")),
            blocksize=transport.config.block_size,
        )
    finally:
        try:
            client.quit()
        except Exception:
            client.close()


def _delete_remote_file(transport: FTPTransport, remote_path: str) -> bool:
    client = transport._new_client()
    try:
        try:
            client.delete(remote_path)
        except Exception as exc:
            text = str(exc).lower()
            if not any(marker in text for marker in ("not found", "no such", "does not exist", "can't find", "550")):
                raise
        return True
    except Exception:
        return False
    finally:
        try:
            client.quit()
        except Exception:
            client.close()


def _read_remote_bytes(transport: FTPTransport, remote_path: str) -> bytes | None:
    client = transport._new_client()
    try:
        buffer = io.BytesIO()
        client.retrbinary(
            f"RETR {remote_path}",
            buffer.write,
            blocksize=transport.config.block_size,
        )
        return buffer.getvalue()
    except Exception:
        return None
    finally:
        try:
            client.quit()
        except Exception:
            client.close()


def _read_remote_state(
    transport: FTPTransport,
    remote_state: str,
    *,
    attempts: int = 3,
) -> dict[str, object] | None:
    for attempt in range(max(1, attempts)):
        raw = _read_remote_bytes(transport, remote_state)
        if raw:
            try:
                payload = json.loads(raw.decode("utf-8"))
            except (UnicodeDecodeError, json.JSONDecodeError):
                payload = None
            if isinstance(payload, dict) and payload.get("dump_bytes") is not None:
                return payload
        if attempt + 1 < attempts:
            time.sleep(0.25)
    return None


def _remote_file_exists(transport: FTPTransport, remote_path: str) -> bool | None:
    parent = str(PurePosixPath(remote_path).parent)
    expected = PurePosixPath(remote_path).name
    client = transport._new_client()
    try:
        for name, facts in transport._mlsd(client, parent):
            if name == expected:
                return (facts.get("type") or "").lower() not in {"dir", "cdir", "pdir"}
        return False
    except Exception:
        return None
    finally:
        try:
            client.quit()
        except Exception:
            client.close()


def _map_php_bootstrap_path(profile: SiteConnectionProfile, configured_path: str) -> str | None:
    value = configured_path.strip().strip('"\'').replace("\\", "/")
    if not value or value.lower() in {"none", "off", "0"}:
        return None
    remote_root = str(PurePosixPath(profile.remote_path))
    index = value.find(remote_root)
    if index >= 0:
        return str(PurePosixPath(value[index:]))
    if not value.startswith("/") and "$" not in value:
        return str(PurePosixPath(remote_root) / value)
    return None


def _directive_target(line: str, *, htaccess: bool) -> str | None:
    match = (_HTACCESS_BOOTSTRAP if htaccess else _INI_BOOTSTRAP).match(line)
    if not match:
        return None
    return next((part.strip() for part in match.groups() if part is not None), None)


def _repair_broken_php_bootstrap(
    profile: SiteConnectionProfile,
    transport: FTPTransport,
    *,
    backup_config_dir: Path | None,
    progress: ProgressCallback | None,
) -> list[str]:
    placeholders: list[str] = []
    for name in _BOOTSTRAP_CONFIG_NAMES:
        remote_config = str(PurePosixPath(profile.remote_path) / name)
        remote_raw = _read_remote_bytes(transport, remote_config)
        source = remote_raw.decode("utf-8", errors="replace") if remote_raw is not None else None
        if source is None and backup_config_dir is not None:
            local_config = backup_config_dir / name
            if local_config.is_file():
                source = local_config.read_text(encoding="utf-8", errors="replace")
        if source is None:
            continue

        kept: list[str] = []
        changed = False
        for line in source.splitlines(keepends=True):
            target = _directive_target(line, htaccess=name == ".htaccess")
            remote_target = _map_php_bootstrap_path(profile, target) if target else None
            target_exists = _remote_file_exists(transport, remote_target) if remote_target else None
            if remote_target and target_exists is False:
                if remote_target not in placeholders:
                    _upload_text(transport, remote_target, _PLACEHOLDER)
                    placeholders.append(remote_target)
                    if progress:
                        progress({"phase": "db_export_bootstrap_repair", "current": remote_target})
                changed = True
                continue
            kept.append(line)

        if changed and remote_raw is not None:
            _upload_text(transport, remote_config, "".join(kept))
    return placeholders


def _cleanup_stale_export_files(transport: FTPTransport, remote_root: str) -> list[str]:
    client = transport._new_client()
    removed: list[str] = []
    try:
        try:
            entries = list(transport._mlsd(client, remote_root))
        except Exception:
            return removed
        for name, facts in entries:
            if not _EXPORT_TEMP.fullmatch(name):
                continue
            if (facts.get("type") or "").lower() in {"dir", "cdir", "pdir"}:
                continue
            path = str(PurePosixPath(remote_root) / name)
            try:
                client.delete(path)
                removed.append(path)
            except Exception:
                pass
        return removed
    finally:
        try:
            client.quit()
        except Exception:
            client.close()


def _bridge_error_detail(body: bytes, *, status: int | None = None) -> str:
    text = body.decode("utf-8", errors="replace").strip()
    prefix = f"HTTP {status}: " if status is not None else ""
    if not text:
        return prefix + "empty response body"
    try:
        payload = json.loads(text)
    except json.JSONDecodeError:
        return prefix + " ".join(text.split())[:1200]
    parts = [str(payload.get("message") or "database export failed")]
    for key in ("step", "table", "offset", "errno", "error", "line"):
        if payload.get(key) not in {None, ""}:
            parts.append(f"{key}={payload.get(key)}")
    return prefix + "; ".join(parts)


def _checkpoint_tuple(state: dict[str, object]) -> tuple[int, int, int, int, int]:
    return (
        int(state.get("dump_bytes", 0)),
        int(state.get("table_index", 0)),
        int(state.get("row_offset", 0)),
        int(state.get("rows", 0)),
        int(state.get("statements", 0)),
    )


def export_database_via_php_bridge(
    profile: SiteConnectionProfile,
    transport: FTPTransport,
    out_path: Path,
    *,
    progress: ProgressCallback | None = None,
    timeout: float = 600.0,
    bootstrap_config_dir: Path | None = None,
) -> DatabaseBridgeResult:
    token = secrets.token_hex(32)
    nonce = secrets.token_hex(8)
    bridge_name = f"wpclean-db-{nonce}.php"
    data_name = f"wpclean-db-{nonce}.dat"
    state_name = f"wpclean-db-{nonce}.state"
    remote_bridge = str(PurePosixPath(profile.remote_path) / bridge_name)
    remote_data = str(PurePosixPath(profile.remote_path) / data_name)
    remote_state = str(PurePosixPath(profile.remote_path) / state_name)
    remote_state_tmp = remote_state + ".tmp"
    bridge_url = f"{profile.web_base_url}/{bridge_name}"
    started = time.monotonic()
    out_path.parent.mkdir(parents=True, exist_ok=True)
    partial = out_path.with_suffix(out_path.suffix + ".part")
    partial.unlink(missing_ok=True)

    if bootstrap_config_dir is None:
        inferred = out_path.parent.parent / "config"
        bootstrap_config_dir = inferred if inferred.is_dir() else None
    placeholders = _repair_broken_php_bootstrap(
        profile,
        transport,
        backup_config_dir=bootstrap_config_dir,
        progress=progress,
    )
    _cleanup_stale_export_files(transport, profile.remote_path)

    if progress:
        progress({"phase": "upload_bridge", "remote_path": remote_bridge})
    _upload_text(transport, remote_data, "")
    _upload_text(
        transport,
        remote_state,
        json.dumps(
            {
                "table_index": 0,
                "row_offset": 0,
                "table_started": False,
                "rows": 0,
                "statements": 0,
                "dump_bytes": 0,
                "header_written": False,
                "done": False,
                "total_tables": 0,
                "current_table": "",
            }
        ),
    )
    _upload_text(transport, remote_bridge, _php_bridge(token, data_name, state_name))

    bridge_removed = False
    data_removed = False
    state_removed = False
    execution_error: Exception | None = None
    downloaded = 0
    sha256 = ""

    try:
        batch = 0
        wait_retries = 0
        max_wait_retries = 12
        last_checkpoint = (0, 0, 0, 0, 0)
        while True:
            request = Request(
                f"{bridge_url}?batch={batch}&retry={wait_retries}",
                headers={
                    "User-Agent": "WP-Clean-Rebuild/0.7",
                    "X-WPClean-Token": token,
                    "Accept": "application/json",
                    "Cache-Control": "no-cache",
                },
                method="GET",
            )
            raw: bytes | None = None
            request_error: Exception | None = None
            try:
                with urlopen(request, timeout=max(15.0, min(float(timeout), 180.0))) as response:
                    raw = response.read()
            except HTTPError as exc:
                body = exc.read()
                if not (500 <= exc.code < 600 and not body.strip()):
                    raise RuntimeError(
                        "Database export bridge failed: "
                        + _bridge_error_detail(body, status=exc.code)
                    ) from exc
                request_error = exc
            except (URLError, TimeoutError, ConnectionError, OSError) as exc:
                request_error = exc

            if request_error is not None:
                time.sleep(0.75)
                state = _read_remote_state(transport, remote_state)
                if state is not None:
                    checkpoint = _checkpoint_tuple(state)
                    if checkpoint > last_checkpoint or bool(state.get("done")):
                        last_checkpoint = checkpoint
                        wait_retries = 0
                        if progress:
                            progress(
                                {
                                    "phase": "db_export_execute",
                                    "items_completed": int(state.get("table_index", 0)),
                                    "items_total": int(state.get("total_tables", 0)),
                                    "bytes_completed": int(state.get("dump_bytes", 0)),
                                    "current": str(state.get("current_table") or "Hoàn tất dump database"),
                                }
                            )
                        if state.get("done"):
                            break
                        batch += 1
                        continue

                wait_retries += 1
                if wait_retries <= max_wait_retries:
                    if progress:
                        progress(
                            {
                                "phase": "db_export_retry",
                                "attempt": wait_retries,
                                "max_attempts": max_wait_retries,
                                "current": "HTTP ngắt; đang đọc checkpoint export qua FTP",
                            }
                        )
                    time.sleep(float(min(wait_retries, 3)))
                    continue
                raise RuntimeError(
                    "Database export bridge stopped and its FTP checkpoint did not advance after "
                    f"{max_wait_retries} checks ({type(request_error).__name__}: {request_error})."
                ) from request_error

            assert raw is not None
            try:
                payload = json.loads(raw.decode("utf-8", errors="replace"))
            except json.JSONDecodeError as exc:
                raise RuntimeError(
                    "Database export bridge returned invalid JSON: " + _bridge_error_detail(raw)
                ) from exc
            if not payload.get("ok"):
                raise RuntimeError("Database export bridge failed: " + _bridge_error_detail(raw))

            checkpoint = _checkpoint_tuple(payload)
            if progress:
                progress(
                    {
                        "phase": "db_export_execute",
                        "items_completed": int(payload.get("table_index", 0)),
                        "items_total": int(payload.get("total_tables", 0)),
                        "bytes_completed": int(payload.get("dump_bytes", 0)),
                        "current": str(payload.get("current_table") or "Hoàn tất dump database"),
                    }
                )
            if payload.get("done"):
                break
            if checkpoint <= last_checkpoint:
                raise RuntimeError("Database export bridge did not advance its checkpoint.")
            last_checkpoint = checkpoint
            wait_retries = 0
            batch += 1
            if batch > 100000:
                raise RuntimeError("Database export exceeded the safe batch request limit.")

        stats = transport.download_file(remote_data, partial, resume=False)
        if stats.files_failed or not partial.is_file() or partial.stat().st_size == 0:
            raise RuntimeError("Database dump file could not be downloaded from hosting.")
        downloaded = partial.stat().st_size
        digest = hashlib.sha256()
        with partial.open("rb") as fh:
            while chunk := fh.read(1024 * 1024):
                digest.update(chunk)
        sha256 = digest.hexdigest()
        with partial.open("rb") as fh:
            head = fh.read(256)
        if b"WP Clean Rebuild database backup" not in head:
            raise RuntimeError("Database dump did not contain the expected SQL header.")
        if progress:
            progress(
                {
                    "phase": "db_export_download",
                    "bytes_downloaded": downloaded,
                    "bytes_total": downloaded,
                    "current": out_path.name,
                }
            )
        partial.replace(out_path)
    except Exception as exc:
        execution_error = exc
        partial.unlink(missing_ok=True)
    finally:
        bridge_removed = _delete_remote_file(transport, remote_bridge)
        data_removed = _delete_remote_file(transport, remote_data)
        state_removed = _delete_remote_file(transport, remote_state)
        _delete_remote_file(transport, remote_state_tmp)
        if progress:
            progress(
                {
                    "phase": "remove_bridge",
                    "removed": bridge_removed and data_removed and state_removed,
                    "remote_path": remote_bridge,
                }
            )

    cleanup_ok = bridge_removed and data_removed and state_removed
    if execution_error is not None:
        if not cleanup_ok:
            raise RuntimeError(
                f"Database export failed ({execution_error}) and temporary cleanup was incomplete: "
                f"bridge_removed={bridge_removed}, data_removed={data_removed}, state_removed={state_removed}"
            ) from execution_error
        raise execution_error
    if not cleanup_ok:
        raise RuntimeError(
            "Database export completed but temporary cleanup was incomplete: "
            f"bridge_removed={bridge_removed}, data_removed={data_removed}, state_removed={state_removed}"
        )

    return DatabaseBridgeResult(
        sql_path=out_path,
        sha256=sha256,
        bytes_downloaded=downloaded,
        elapsed_seconds=time.monotonic() - started,
        bridge_removed=bridge_removed,
        data_removed=data_removed,
        state_removed=state_removed,
        bootstrap_placeholders=placeholders,
    )


__all__ = ["DatabaseBridgeResult", "export_database_via_php_bridge"]
