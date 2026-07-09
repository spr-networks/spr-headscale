#!/bin/bash
# Command line install alternative to the UI (+ New Plugin)
echo "Please enter your SPR path (/home/spr/super/)"
read -r SUPERDIR

if [ -z "$SUPERDIR" ]; then
    SUPERDIR="/home/spr/super/"
fi

export SUPERDIR

echo "Please enter your SPR API token:"
read -r SPR_API_TOKEN

if [ -z "$SPR_API_TOKEN" ]; then
  echo "need api token, generate one on the auth keys page"
  exit 1
fi

mkdir -p "$SUPERDIR/configs/plugins/spr-headscale"

# Token used by SPR to authorize the plugin (InstallTokenPath)
printf '%s' "$SPR_API_TOKEN" > "$SUPERDIR/configs/plugins/spr-headscale/api-token"
chmod 600 "$SUPERDIR/configs/plugins/spr-headscale/api-token"

docker compose build
docker compose up -d

CONTAINER_IP=$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "spr-headscale")
API=127.0.0.1

# Register the plugin's bridge interface with the SPR firewall:
# wan+dns lets headscale fetch the upstream DERP map, lan lets SPR LAN
# devices reach headscale on ${CONTAINER_IP}:8080.
curl "http://${API}/firewall/custom_interface" \
-H "Authorization: Bearer ${SPR_API_TOKEN}" \
-X 'PUT' \
--data-raw "{\"SrcIP\":\"${CONTAINER_IP}\",\"Interface\":\"spr-headscale\",\"Policies\":[\"wan\",\"dns\",\"lan\"]}"

docker compose restart

echo ""
echo "spr-headscale is up. Point tailscale clients at http://${CONTAINER_IP}:8080"
echo "(or set a custom Server URL in the plugin UI)."
