from __future__ import annotations

import webbrowser


def main() -> None:
    # OneClick renders the dashboard inside its own WebView.  Suppressing the
    # browser here keeps the original engine/UI intact without opening a
    # second employee-facing window.
    webbrowser.open = lambda *_args, **_kwargs: True

    from wpclean.gui_runtime_entry import main as run_gui

    run_gui()


if __name__ == "__main__":
    main()
