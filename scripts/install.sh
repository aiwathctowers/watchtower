#!/bin/bash
#
# Watchtower Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/vadimtrunov/watchtower/main/scripts/install.sh | bash
#
set -euo pipefail

REPO="vadimtrunov/watchtower"
APP_NAME="Watchtower"
INSTALL_DIR="/Applications"
CLI_LINK="/usr/local/bin/watchtower"

# --- Helpers ---

info()  { printf "\033[1;34m==>\033[0m \033[1m%s\033[0m\n" "$1"; }
ok()    { printf "\033[1;32m  ✓\033[0m %s\n" "$1"; }
warn()  { printf "\033[1;33m  !\033[0m %s\n" "$1"; }
fail()  { printf "\033[1;31mError:\033[0m %s\n" "$1" >&2; exit 1; }

cleanup() {
    [ -n "${TMPDIR_INSTALL:-}" ] && rm -rf "$TMPDIR_INSTALL"
}
trap cleanup EXIT

# --- Pre-flight checks ---

info "Watchtower Installer"
echo ""

# macOS only
[ "$(uname -s)" = "Darwin" ] || fail "Watchtower is only supported on macOS."

# arm64 only for now
ARCH="$(uname -m)"
if [ "$ARCH" != "arm64" ]; then
    fail "Watchtower currently supports Apple Silicon (arm64) only. Detected: $ARCH"
fi

# Need curl
command -v curl >/dev/null 2>&1 || fail "curl is required but not found."

# --- Fetch latest release ---

info "Finding latest release..."

RELEASE_JSON=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest") \
    || fail "Could not fetch release info from GitHub. Check your internet connection."

VERSION=$(echo "$RELEASE_JSON" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')
[ -n "$VERSION" ] || fail "Could not determine latest version."

# Strip leading 'v' for asset name
VERSION_NUM="${VERSION#v}"
ASSET_NAME="${APP_NAME}-${VERSION_NUM}-arm64.zip"
CHECKSUMS_NAME="checksums.txt"

ok "Latest version: $VERSION"

# Find asset download URL
ASSET_URL=$(echo "$RELEASE_JSON" | grep "browser_download_url" | grep "$ASSET_NAME" | head -1 | sed 's/.*"browser_download_url": *"//;s/".*//')
[ -n "$ASSET_URL" ] || fail "Could not find $ASSET_NAME in release $VERSION."

# Find checksums URL (optional)
CHECKSUMS_URL=$(echo "$RELEASE_JSON" | grep "browser_download_url" | grep "$CHECKSUMS_NAME" | head -1 | sed 's/.*"browser_download_url": *"//;s/".*//' || true)

# --- Download ---

TMPDIR_INSTALL=$(mktemp -d)

info "Downloading $ASSET_NAME..."
curl -fSL --progress-bar -o "$TMPDIR_INSTALL/$ASSET_NAME" "$ASSET_URL" \
    || fail "Download failed."
ok "Downloaded $(du -h "$TMPDIR_INSTALL/$ASSET_NAME" | cut -f1 | xargs)"

# --- Verify checksum ---

if [ -n "$CHECKSUMS_URL" ]; then
    info "Verifying checksum..."
    curl -fsSL -o "$TMPDIR_INSTALL/$CHECKSUMS_NAME" "$CHECKSUMS_URL" \
        || { warn "Could not download checksums — skipping verification."; CHECKSUMS_URL=""; }
fi

if [ -n "$CHECKSUMS_URL" ]; then
    EXPECTED=$(grep "$ASSET_NAME" "$TMPDIR_INSTALL/$CHECKSUMS_NAME" | awk '{print $1}')
    if [ -n "$EXPECTED" ]; then
        ACTUAL=$(shasum -a 256 "$TMPDIR_INSTALL/$ASSET_NAME" | awk '{print $1}')
        if [ "$EXPECTED" = "$ACTUAL" ]; then
            ok "Checksum verified (SHA-256)"
        else
            fail "Checksum mismatch!\n  Expected: $EXPECTED\n  Got:      $ACTUAL"
        fi
    else
        warn "No checksum entry for $ASSET_NAME — skipping verification."
    fi
else
    warn "No checksums available — skipping verification."
fi

# --- Install ---

info "Installing to $INSTALL_DIR..."

# Unzip
ditto -x -k "$TMPDIR_INSTALL/$ASSET_NAME" "$TMPDIR_INSTALL/extracted" \
    || fail "Failed to unzip $ASSET_NAME."

# Find .app in extracted contents
APP_PATH=$(find "$TMPDIR_INSTALL/extracted" -name "*.app" -maxdepth 2 -type d | head -1)
[ -n "$APP_PATH" ] || fail "Could not find .app bundle in archive."

# Remove old version if present
if [ -d "$INSTALL_DIR/$APP_NAME.app" ]; then
    warn "Removing previous installation..."
    rm -rf "$INSTALL_DIR/$APP_NAME.app"
fi

# Move to Applications (may need sudo)
if cp -R "$APP_PATH" "$INSTALL_DIR/" 2>/dev/null; then
    ok "Installed $APP_NAME.app"
else
    info "Need administrator access to install to $INSTALL_DIR..."
    sudo cp -R "$APP_PATH" "$INSTALL_DIR/" \
        || fail "Could not install to $INSTALL_DIR."
    ok "Installed $APP_NAME.app"
fi

# Clear quarantine (since it's downloaded from the internet)
xattr -dr com.apple.quarantine "$INSTALL_DIR/$APP_NAME.app" 2>/dev/null || true

# --- CLI symlink ---

CLI_PATH="$INSTALL_DIR/$APP_NAME.app/Contents/MacOS/watchtower"
if [ -f "$CLI_PATH" ]; then
    info "Setting up CLI..."
    # Ensure /usr/local/bin exists
    if [ ! -d "$(dirname "$CLI_LINK")" ]; then
        sudo mkdir -p "$(dirname "$CLI_LINK")"
    fi

    if ln -sf "$CLI_PATH" "$CLI_LINK" 2>/dev/null; then
        ok "CLI available: watchtower"
    else
        sudo ln -sf "$CLI_PATH" "$CLI_LINK" 2>/dev/null \
            && ok "CLI available: watchtower" \
            || warn "Could not create symlink at $CLI_LINK. Add $CLI_PATH to your PATH manually."
    fi
fi

# --- Done ---

echo ""
info "Watchtower $VERSION installed successfully!"
echo ""
echo "  Open the app:   open -a Watchtower"
echo "  CLI:            watchtower --help"
echo "  Login:          watchtower login"
echo ""
