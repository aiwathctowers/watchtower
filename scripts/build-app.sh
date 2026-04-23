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

# Parse flags
DEV_MODE=false
VERSION=""
for arg in "$@"; do
    case "$arg" in
        --dev) DEV_MODE=true ;;
        *) VERSION="$arg" ;;
    esac
done
VERSION="${VERSION:-0.2.0}"

if $DEV_MODE; then
    SIGN_IDENTITY="-"
    NOTARIZE_PROFILE=""
    echo "==> Building Watchtower v$VERSION (arm64) [DEV MODE — no signing/notarization]"
else
    SIGN_IDENTITY="${CODESIGN_IDENTITY:--}"
    NOTARIZE_PROFILE="${NOTARIZE_PROFILE:-}"
    echo "==> Building Watchtower v$VERSION (arm64)"
fi
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
GOOGLE_ID="${WATCHTOWER_GOOGLE_CLIENT_ID:-}"
GOOGLE_SECRET="${WATCHTOWER_GOOGLE_CLIENT_SECRET:-}"
JIRA_ID="${WATCHTOWER_JIRA_CLIENT_ID:-}"
JIRA_SECRET="${WATCHTOWER_JIRA_CLIENT_SECRET:-}"
GOARCH=arm64 CGO_ENABLED=0 go build \
    -ldflags="-s -w -X watchtower/cmd.Version=${VERSION} -X watchtower/cmd.Commit=${COMMIT} -X watchtower/cmd.BuildDate=${BUILD_DATE} -X watchtower/internal/auth.DefaultClientID=${OAUTH_ID} -X watchtower/internal/auth.DefaultClientSecret=${OAUTH_SECRET} -X watchtower/internal/calendar.DefaultGoogleClientID=${GOOGLE_ID} -X watchtower/internal/calendar.DefaultGoogleClientSecret=${GOOGLE_SECRET} -X watchtower/internal/jira.DefaultJiraClientID=${JIRA_ID} -X watchtower/internal/jira.DefaultJiraClientSecret=${JIRA_SECRET}" \
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

# Copy SPM resource bundles into Contents/Resources/ (standard macOS .app location)
# AppBundle.resources searches here via Bundle.main.resourceURL
RESOURCE_BUNDLE_DIR=$(swift build -c release --arch arm64 --show-bin-path)
for bundle in "$RESOURCE_BUNDLE_DIR"/*.bundle; do
    if [ -d "$bundle" ]; then
        cp -R "$bundle" "$APP_BUNDLE/Contents/Resources/"
        echo "    Copied resource bundle: $(basename "$bundle")"
    fi
done

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
    <key>INIntentsSupported</key>
    <array/>
    <key>NSUserActivityTypes</key>
    <array/>
    <key>NSCoreSpotlightContinuation</key>
    <false/>
    <key>CSSupportsSearchableItems</key>
    <false/>
    <key>NSSupportsAutomaticTermination</key>
    <false/>
    <key>NSSupportsSuddenTermination</key>
    <false/>
</dict>
</plist>
PLIST

# Copy icon if exists
if [ -f "$DESKTOP_DIR/Resources/AppIcon.icns" ]; then
    cp "$DESKTOP_DIR/Resources/AppIcon.icns" "$APP_BUNDLE/Contents/Resources/"
    /usr/libexec/PlistBuddy -c "Add :CFBundleIconFile string AppIcon" "$APP_BUNDLE/Contents/Info.plist"
fi

# Code sign
# Even in dev mode: do ad-hoc sign with entitlements so TCC can identify the bundle
# by its signature. Without a signature, macOS Tahoe (26+) issues TCC prompts for
# Downloads/Documents/Desktop on every launch via AppIntents/Spotlight preflight.
if $DEV_MODE; then
    echo "==> Ad-hoc code signing (dev mode)..."
    codesign --force --sign - --entitlements "$ENTITLEMENTS" "$APP_BUNDLE/Contents/MacOS/watchtower"
    codesign --force --sign - --entitlements "$ENTITLEMENTS" "$APP_BUNDLE"
elif [ "$SIGN_IDENTITY" != "-" ] && security find-identity -v -p codesigning 2>/dev/null | grep -q "$SIGN_IDENTITY"; then
    echo "==> Code signing with: $SIGN_IDENTITY"
    codesign --force --options runtime --sign "$SIGN_IDENTITY" "$APP_BUNDLE/Contents/MacOS/watchtower"
    codesign --force --options runtime --entitlements "$ENTITLEMENTS" --sign "$SIGN_IDENTITY" "$APP_BUNDLE"
else
    echo "==> Ad-hoc code signing..."
    codesign --force --sign - --entitlements "$ENTITLEMENTS" "$APP_BUNDLE/Contents/MacOS/watchtower"
    codesign --force --sign - --entitlements "$ENTITLEMENTS" "$APP_BUNDLE"
fi

# In dev mode, skip DMG/ZIP/notarization — just output the .app
if $DEV_MODE; then
    echo ""
    echo "==> Done! (dev mode)"
    echo "    App: $APP_BUNDLE"
    echo ""
    echo "    To run: open $APP_BUNDLE"
    exit 0
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
        "$DMG_STAGING"
    dmg_rc=$?
    if [ "$dmg_rc" -ne 0 ] && [ "$dmg_rc" -ne 2 ]; then
        echo "ERROR: create-dmg failed with exit code $dmg_rc"
        exit 1
    fi
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

# Notarize if credentials are configured
if [ "$SIGN_IDENTITY" != "-" ] && [ -n "$NOTARIZE_PROFILE" ]; then
    echo "==> Notarizing ZIP..."
    xcrun notarytool submit "$ZIP_NAME" \
        --keychain-profile "$NOTARIZE_PROFILE" \
        --wait

    echo "==> Stapling app bundle..."
    xcrun stapler staple "$APP_BUNDLE"

    # Re-create DMG with stapled app
    echo "==> Re-creating DMG with stapled app..."
    rm -f "$DMG_PATH"
    DMG_STAGING="$BUILD_DIR/dmg-staging"
    rm -rf "$DMG_STAGING"
    mkdir -p "$DMG_STAGING"
    cp -R "$APP_BUNDLE" "$DMG_STAGING/"
    ln -s /Applications "$DMG_STAGING/Applications"

    if command -v create-dmg &>/dev/null; then
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
                [ $? -eq 2 ] || exit 1
            }
    else
        hdiutil create \
            -volname "Watchtower" \
            -srcfolder "$DMG_STAGING" \
            -ov \
            -format UDZO \
            "$DMG_PATH"
    fi
    rm -rf "$DMG_STAGING"

    echo "==> Notarizing DMG..."
    xcrun notarytool submit "$DMG_PATH" \
        --keychain-profile "$NOTARIZE_PROFILE" \
        --wait

    echo "==> Stapling DMG..."
    xcrun stapler staple "$DMG_PATH"

    # Re-create ZIP with stapled app
    echo "==> Re-creating ZIP with stapled app..."
    rm -f "$ZIP_NAME"
    ditto -c -k --keepParent "$APP_NAME.app" "$ZIP_NAME"

    DMG_SIZE=$(du -h "$DMG_PATH" | cut -f1)
    ZIP_SIZE=$(du -h "$ZIP_NAME" | cut -f1)
else
    if [ "$SIGN_IDENTITY" = "-" ]; then
        echo "==> Skipping notarization (ad-hoc signing)"
    else
        echo "==> Skipping notarization (NOTARIZE_PROFILE not set)"
    fi
fi

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
if [ -n "$NOTARIZE_PROFILE" ] && [ "$SIGN_IDENTITY" != "-" ]; then
    echo "    Notarized & stapled ✓"
fi
echo ""
echo "    Contents:"
echo "      - WatchtowerDesktop (GUI app)"
echo "      - watchtower (CLI — bundled)"
echo ""
echo "    To install: open DMG → drag Watchtower to Applications"
