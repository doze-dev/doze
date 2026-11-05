#!/bin/sh
# Unload the profile while its file is still there: apparmor_parser -R
# reads the profile's name from the file.
if command -v apparmor_parser >/dev/null 2>&1 && [ -d /sys/kernel/security/apparmor ] && [ -e /etc/apparmor.d/doze ]; then
  apparmor_parser -R /etc/apparmor.d/doze 2>/dev/null || true
fi
exit 0
