from __future__ import annotations


ALLOWED_MU_PLUGIN_COMPONENT_KINDS = {
    "flatsome-custom-assets": "directory",
    "flatsome-custom-loader.php": "file",
}
ALLOWED_MU_PLUGIN_COMPONENTS = frozenset(ALLOWED_MU_PLUGIN_COMPONENT_KINDS)


def is_allowed_mu_plugin_component(name: str) -> bool:
    return name.strip().casefold() in ALLOWED_MU_PLUGIN_COMPONENTS


__all__ = [
    "ALLOWED_MU_PLUGIN_COMPONENT_KINDS",
    "ALLOWED_MU_PLUGIN_COMPONENTS",
    "is_allowed_mu_plugin_component",
]
