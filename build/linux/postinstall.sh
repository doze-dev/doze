#!/bin/sh
# The app's WebKit sandbox needs a user namespace, which Ubuntu 24.04 and
# later grant only through a profile; load it now rather than at next boot.
if command -v apparmor_parser >/dev/null 2>&1 && [ -d /sys/kernel/security/apparmor ]; then
  apparmor_parser -r /etc/apparmor.d/doze || true
fi
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database -q /usr/share/applications || true
fi
exit 0
