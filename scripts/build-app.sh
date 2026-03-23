#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Source .env if present (for OAuth credentials etc.)
if [ -f "$PROJECT_ROOT/.env" ]; then
    set -a
    . "$PROJECT_ROOT/.env"
    set +a
fi
DESKTOP_DIR="$PROJECT_ROOT/WatchtowerDesktop"
BUILD_DIR="$PROJECT_ROOT/build"
APP_NAME="Watchtower"
APP_BUNDLE="$BUILD_DIR/$APP_NAME.app"
ENTITLEMENTS="$SCRIPT_DIR/Watchtower.entitlements"
VERSION="${1:-0.2.0}"
SIGN_IDENTITY="${CODESIGN_IDENTITY:--}"

echo "==> Building Watchtower v$VERSION (arm64)"
echo ""

# Clean previous build
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"

# 1. Build Go CLI
echo "==> Building Go CLI..."
cd "$PROJECT_ROOT"
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
OAUTH_ID="${WATCHTOWER_OAUTH_CLIENT_ID:-}"
OAUTH_SECRET="${WATCHTOWER_OAUTH_CLIENT_SECRET:-}"
GOARCH=arm64 CGO_ENABLED=0 go build \
    -ldflags="-s -w -X watchtower/cmd.Version=${VERSION} -X watchtower/cmd.Commit=${COMMIT} -X watchtower/cmd.BuildDate=${BUILD_DATE} -X watchtower/internal/auth.DefaultClientID=${OAUTH_ID} -X watchtower/internal/auth.DefaultClientSecret=${OAUTH_SECRET}" \
    -o "$BUILD_DIR/watchtower" .
echo "    Go CLI built ($(du -h "$BUILD_DIR/watchtower" | cut -f1))"

# 2. Build Swift desktop app
echo "==> Building Desktop app..."
cd "$DESKTOP_DIR"
swift build -c release --arch arm64 2>&1

BINARY=$(swift build -c release --arch arm64 --show-bin-path)/WatchtowerDesktop

if [ ! -f "$BINARY" ]; then
    echo "ERROR: Desktop binary not found at $BINARY"
    exit 1
fi

echo "==> Creating app bundle..."

# Create .app structure
mkdir -p "$APP_BUNDLE/Contents/MacOS"
mkdir -p "$APP_BUNDLE/Contents/Resources"

# Copy desktop binary
cp "$BINARY" "$APP_BUNDLE/Contents/MacOS/WatchtowerDesktop"

# Copy Go CLI into bundle
cp "$BUILD_DIR/watchtower" "$APP_BUNDLE/Contents/MacOS/watchtower"

# Create Info.plist
cat > "$APP_BUNDLE/Contents/Info.plist" << PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>WatchtowerDesktop</string>
    <key>CFBundleIdentifier</key>
    <string>com.watchtower.desktop</string>
    <key>CFBundleName</key>
    <string>Watchtower</string>
    <key>CFBundleDisplayName</key>
    <string>Watchtower</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleVersion</key>
    <string>$VERSION</string>
    <key>CFBundleShortVersionString</key>
    <string>$VERSION</string>
    <key>LSMinimumSystemVersion</key>
    <string>14.0</string>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>LSUIElement</key>
    <false/>
    <key>NSAppTransportSecurity</key>
    <dict>
        <key>NSAllowsArbitraryLoads</key>
        <false/>
    </dict>
    <key>CFBundleURLTypes</key>
    <array>
        <dict>
            <key>CFBundleURLName</key>
            <string>Watchtower OAuth Callback</string>
            <key>CFBundleURLSchemes</key>
            <array>
                <string>watchtower-auth</string>
            </array>
        </dict>
    </array>
</dict>
</plist>
PLIST

# Copy icon if exists
if [ -f "$DESKTOP_DIR/Resources/AppIcon.icns" ]; then
    cp "$DESKTOP_DIR/Resources/AppIcon.icns" "$APP_BUNDLE/Contents/Resources/"
    /usr/libexec/PlistBuddy -c "Add :CFBundleIconFile string AppIcon" "$APP_BUNDLE/Contents/Info.plist"
fi

# Code sign (ad-hoc for local use, developer cert for distribution)
if [ "$SIGN_IDENTITY" != "-" ] && security find-identity -v -p codesigning 2>/dev/null | grep -q "$SIGN_IDENTITY"; then
    echo "==> Code signing with: $SIGN_IDENTITY"
    codesign --force --options runtime --sign "$SIGN_IDENTITY" "$APP_BUNDLE/Contents/MacOS/watchtower"
    codesign --force --options runtime --entitlements "$ENTITLEMENTS" --sign "$SIGN_IDENTITY" "$APP_BUNDLE"
else
    echo "==> Ad-hoc code signing..."
    codesign --force --sign - --entitlements "$ENTITLEMENTS" "$APP_BUNDLE/Contents/MacOS/watchtower"
    codesign --force --sign - --entitlements "$ENTITLEMENTS" "$APP_BUNDLE"
fi

# Create DMG
echo "==> Creating DMG..."
DMG_NAME="Watchtower-arm64.dmg"
DMG_PATH="$BUILD_DIR/$DMG_NAME"
DMG_STAGING="$BUILD_DIR/dmg-staging"

rm -rf "$DMG_STAGING"
mkdir -p "$DMG_STAGING"
cp -R "$APP_BUNDLE" "$DMG_STAGING/"
ln -s /Applications "$DMG_STAGING/Applications"

if command -v create-dmg &>/dev/null; then
    # Pretty DMG with window layout (brew install create-dmg)
    create-dmg \
        --volname "Watchtower" \
        --volicon "$APP_BUNDLE/Contents/Resources/AppIcon.icns" \
        --window-pos 200 120 \
        --window-size 600 400 \
        --icon-size 100 \
        --icon "$APP_NAME.app" 150 185 \
        --icon "Applications" 450 185 \
        --hide-extension "$APP_NAME.app" \
        --app-drop-link 450 185 \
        --no-internet-enable \
        "$DMG_PATH" \
        "$DMG_STAGING" || {
            # create-dmg returns 2 if icon positioning fails (non-fatal)
            [ $? -eq 2 ] || exit 1
        }
else
    # Fallback: hdiutil (always available on macOS)
    hdiutil create \
        -volname "Watchtower" \
        -srcfolder "$DMG_STAGING" \
        -ov \
        -format UDZO \
        "$DMG_PATH"
fi

rm -rf "$DMG_STAGING"

DMG_SIZE=$(du -h "$DMG_PATH" | cut -f1)

# Create ZIP (used by auto-update + install script)
echo "==> Creating ZIP..."
ZIP_NAME="Watchtower-${VERSION}-arm64.zip"
cd "$BUILD_DIR"
ditto -c -k --keepParent "$APP_NAME.app" "$ZIP_NAME"
ZIP_SIZE=$(du -h "$ZIP_NAME" | cut -f1)

# Generate checksums
echo "==> Generating checksums..."
CHECKSUMS="$BUILD_DIR/checksums.txt"
shasum -a 256 "$DMG_NAME" "$ZIP_NAME" > "$CHECKSUMS"

echo ""
echo "==> Done!"
echo "    App:  $APP_BUNDLE"
echo "    DMG:  $DMG_PATH ($DMG_SIZE)"
echo "    ZIP:  $BUILD_DIR/$ZIP_NAME ($ZIP_SIZE)  ← auto-update"
echo "    SHA:  $CHECKSUMS"
echo ""
echo "    Contents:"
echo "      - WatchtowerDesktop (GUI app)"
echo "      - watchtower (CLI — bundled)"
echo ""
echo "    To install: open DMG → drag Watchtower to Applications"
