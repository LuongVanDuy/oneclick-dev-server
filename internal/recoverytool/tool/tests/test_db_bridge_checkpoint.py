from wpclean.db_bridge import (
    _checkpoint_tuple,
    _directive_target,
    _map_php_bootstrap_path,
    _php_bridge,
)
from wpclean.site_config import SiteConnectionProfile


def _profile() -> SiteConnectionProfile:
    return SiteConnectionProfile(
        host="example.com",
        username="account",
        password="secret",
        protocol="ftp",
        port=21,
        remote_path="/domains/example.com/public_html",
    )


def test_export_bridge_is_checkpointed_and_header_authenticated():
    bridge = _php_bridge("token", "dump.dat", "dump.state")

    assert "HTTP_X_WPCLEAN_TOKEN" in bridge
    assert "flock($dump, LOCK_EX)" in bridge
    assert "ftruncate($dump, $checkpointBytes)" in bridge
    assert "WPCLEAN_ROWS_PER_QUERY" in bridge
    assert "save_state($statePath, $state)" in bridge


def test_absolute_php_bootstrap_path_maps_back_to_ftp_root():
    mapped = _map_php_bootstrap_path(
        _profile(),
        "/home/account/domains/example.com/public_html/wp-content/stale.php",
    )

    assert mapped == "/domains/example.com/public_html/wp-content/stale.php"


def test_php_bootstrap_directives_are_detected():
    assert _directive_target(
        'auto_prepend_file = "/home/account/public_html/stale.php"\n',
        htaccess=False,
    ) == "/home/account/public_html/stale.php"
    assert _directive_target(
        "php_value auto_append_file wp-content/stale.php\n",
        htaccess=True,
    ) == "wp-content/stale.php"


def test_checkpoint_comparison_uses_dump_and_database_progress():
    assert _checkpoint_tuple(
        {
            "dump_bytes": 1024,
            "table_index": 3,
            "row_offset": 50,
            "rows": 200,
            "statements": 20,
        }
    ) == (1024, 3, 50, 200, 20)
