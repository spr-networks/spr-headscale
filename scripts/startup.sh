#!/bin/bash
set -a
. /configs/base/config.sh
if [ -f /configs/spr-headscale/config.sh ]; then
    . /configs/spr-headscale/config.sh
fi
set +a

mkdir -p /state/plugins/spr-headscale/data /var/run/headscale
chmod 700 /state/plugins/spr-headscale/data

# headscale is supervised by the plugin binary (started, restarted on config
# change and watched for crashes); logs go to stdout/journald via docker.
exec /headscale_plugin
