#!/bin/sh
# Drop to the proxyctl user after ensuring /data is writable.
# Web UI is not required for startup; pass --ui-listen via CMD/compose if needed.
set -eu
bin=/usr/local/bin/proxyctl-controller
if [ "$(id -u)" = "0" ]; then
  mkdir -p /data /data/certs
  chown -R proxyctl:proxyctl /data
  exec setpriv --reuid=proxyctl --regid=proxyctl --clear-groups -- "$bin" "$@"
fi
exec "$bin" "$@"
