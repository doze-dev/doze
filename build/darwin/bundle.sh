#!/bin/sh
# bundle.sh <doze-binary> <out-dir> [version]
#
# Assembles Doze.app around the one doze binary (CLI, daemon and app in one
# file, design D6) and signs it. Locally the signature is ad hoc, which is
# enough to run. In CI with a Developer ID in the keychain (DOZE_SIGN_ID set)
# it signs with the hardened runtime and, with notarytool credentials
# (NOTARY_KEY_ID, NOTARY_ISSUER_ID, NOTARY_KEY_PATH), notarises and staples.
set -eu
BIN=$1
OUT=$2
VERSION=${3:-dev}
HERE=$(cd "$(dirname "$0")" && pwd)
APP="$OUT/Doze.app"

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$BIN" "$APP/Contents/MacOS/doze"
sed "s|__VERSION__|$VERSION|g" "$HERE/Info.plist" > "$APP/Contents/Info.plist"
printf 'APPL????' > "$APP/Contents/PkgInfo"
if [ -f "$HERE/doze.icns" ]; then
  cp "$HERE/doze.icns" "$APP/Contents/Resources/doze.icns"
fi

if [ -n "${DOZE_SIGN_ID:-}" ]; then
  codesign --force --timestamp --options runtime \
    --entitlements "$HERE/entitlements.plist" \
    --sign "$DOZE_SIGN_ID" "$APP"
  echo "signed $APP with $DOZE_SIGN_ID"
  if [ -n "${NOTARY_KEY_PATH:-}" ]; then
    ZIP="$OUT/Doze.app.zip"
    ditto -c -k --keepParent "$APP" "$ZIP"
    xcrun notarytool submit "$ZIP" --wait \
      --key "$NOTARY_KEY_PATH" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER_ID"
    xcrun stapler staple "$APP"
    rm -f "$ZIP"
    ditto -c -k --keepParent "$APP" "$ZIP"
    echo "notarised and stapled; $ZIP"
  fi
else
  codesign --force --sign - "$APP"
  echo "ad-hoc signed $APP (set DOZE_SIGN_ID for a Developer ID)"
fi
codesign --verify --deep --strict "$APP"
