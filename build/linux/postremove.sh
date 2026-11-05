#!/bin/sh
if command -v apparmor_parser >/dev/null 2>&1 && [ -d /sys/kernel/security/apparmor ] && [ ! -e /etc/apparmor.d/doze ]; then
  apparmor_parser -R doze 2>/dev/null || true
fi
exit 0
