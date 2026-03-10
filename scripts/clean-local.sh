#!/bin/bash
# Clean all local Watchtower data (config, DB, caches, preferences)
set -euo pipefail

echo "==> Killing Watchtower processes..."
pkill -f WatchtowerDesktop 2>/dev/null || true
pkill -f "watchtower.*daemon" 2>/dev/null || true
sleep 1

echo "==> Removing config & database..."
rm -rf ~/.config/watchtower/
rm -rf ~/.local/share/watchtower/

echo "==> Removing macOS preferences & caches..."
rm -f ~/Library/Preferences/com.watchtower.desktop.plist
rm -f ~/Library/Preferences/WatchtowerDesktop.plist
rm -rf ~/Library/Caches/WatchtowerDesktop/
rm -rf ~/Library/HTTPStorages/WatchtowerDesktop/
rm -f ~/Library/Application\ Support/CrashReporter/WatchtowerDesktop_*.plist
rm -f ~/Library/Logs/DiagnosticReports/WatchtowerDesktop-*.ips
defaults delete com.watchtower.desktop 2>/dev/null || true
defaults delete WatchtowerDesktop 2>/dev/null || true

echo "==> Done! Clean slate."
