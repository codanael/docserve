#!/bin/sh
set -e

# Create dedicated system user (no login shell, no home directory)
if ! getent passwd docserve >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin docserve
fi

# Create data directory owned by the service user
mkdir -p /var/lib/docserve
chown docserve:docserve /var/lib/docserve

# Reload systemd to pick up the new service file
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload
fi
