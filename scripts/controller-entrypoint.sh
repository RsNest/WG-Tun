#!/bin/sh
# Drop to the transitforge user after ensuring /data is writable.
# Web UI is not required for startup; pass --ui-listen via CMD/compose if needed.
set -eu
bin=/usr/local/bin/transitforge-controller
if [ "$(id -u)" = "0" ]; then
  mkdir -p /data /data/certs
  chown -R transitforge:transitforge /data
  exec setpriv --reuid=transitforge --regid=transitforge --clear-groups -- "$bin" "$@"
fi
exec "$bin" "$@"
