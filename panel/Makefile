# User-level install: the binary in ~/.local/bin, the icon and the menu
# entry under ~/.local/share. No root. Builds with two jobs.

PREFIX ?= $(HOME)/.local
BIN := $(PREFIX)/bin
APPS := $(PREFIX)/share/applications
ICONS := $(PREFIX)/share/icons/hicolor

.PHONY: build check install uninstall coverage

build:
	cargo build --release -j 2

check:
	cargo fmt --check
	cargo clippy -j 2 --all-targets -- -D warnings
	cargo test -j 2

coverage:
	CARGO_BUILD_JOBS=2 cargo llvm-cov --lib --tests --summary-only --ignore-filename-regex 'src/(main|ui/view/|ui/runtime)'

install: build
	install -Dm755 target/release/axlr-bots-panel $(BIN)/axlr-bots-panel
	install -Dm644 assets/brand/axlr-bots-icon.svg $(ICONS)/scalable/apps/axlr-bots-panel.svg
	install -Dm644 assets/brand/axlr-bots-icon-128.png $(ICONS)/128x128/apps/axlr-bots-panel.png
	sed 's|^Exec=axlr-bots-panel|Exec=$(BIN)/axlr-bots-panel|' packaging/axlr-bots-panel.desktop > packaging/.desktop.tmp
	install -Dm644 packaging/.desktop.tmp $(APPS)/axlr-bots-panel.desktop
	rm -f packaging/.desktop.tmp
	-update-desktop-database $(APPS) 2>/dev/null
	-gtk-update-icon-cache -q -t $(ICONS) 2>/dev/null

uninstall:
	rm -f $(BIN)/axlr-bots-panel $(APPS)/axlr-bots-panel.desktop
	rm -f $(ICONS)/scalable/apps/axlr-bots-panel.svg $(ICONS)/128x128/apps/axlr-bots-panel.png
