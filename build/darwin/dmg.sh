#!/bin/sh
# dmg.sh <dir-with-Doze.app> [version]
#
# Wraps a built, signed Doze.app in Doze-<version>.dmg: the app and a link to
# /Applications, so installing is a drag. With DOZE_SIGN_ID the image is
# signed too, and with notarytool credentials (NOTARY_KEY_ID,
# NOTARY_ISSUER_ID, NOTARY_KEY_PATH) notarised and stapled, so Gatekeeper
# opens it without a word. Without them it is a local, unsigned image.
set -eu
DIR=$1
VERSION=${2:-dev}
APP="$DIR/Doze.app"
DMG="$DIR/Doze-$VERSION.dmg"
[ -d "$APP" ] || { echo "dmg.sh: no $APP; run bundle.sh first" >&2; exit 1; }

STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
rm -f "$DMG"
hdiutil create -quiet -volname "doze" -srcfolder "$STAGE" -fs HFS+ -format ULFO -ov "$DMG"

if [ -n "${DOZE_SIGN_ID:-}" ]; then
  codesign --force --timestamp --sign "$DOZE_SIGN_ID" "$DMG"
  if [ -n "${NOTARY_KEY_PATH:-}" ]; then
    xcrun notarytool submit "$DMG" --wait \
      --key "$NOTARY_KEY_PATH" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER_ID"
    xcrun stapler staple "$DMG"
  fi
fi
hdiutil verify -quiet "$DMG"
echo "$DMG"
