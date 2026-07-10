# spr-headscale

Run [headscale](https://github.com/juanfont/headscale) — a self-hosted, open
source implementation of the Tailscale control server — as an
[SPR](https://github.com/spr-networks/super) plugin. Your tailnet's
coordination server lives on your own router: no Tailscale account, no cloud
control plane.

The plugin ships headscale built from source at a pinned release, supervises
it, and adds a small REST API + web UI (embedded in the SPR interface under
*Plugins*) for managing users, preauth keys and nodes.

## Features

- **Headscale v0.29.2 built from source**, pinned to the release tag's full
  commit hash, reproducible container build
- **Status card** — daemon state, version, server URL, node/user counts
- **Users** — create / delete headscale users
- **Preauth keys** — per-user generator (reusable / ephemeral / expiration);
  the key value is displayed exactly once, in a copy-once modal, and only a
  masked prefix is ever listed afterwards
- **Nodes** — name, IPs, owner, online dot, last seen, expire / remove actions
- **Settings** — server URL, MagicDNS on/off + base domain, DERP relay map
  on/off; config changes regenerate headscale's `config.yaml` and restart the
  daemon
- **Topology** — contributes its nodes (with online state and tailnet IPs) to
  SPR's router topology view via `GET /topology`
- **No host ports.** headscale listens on the container IP `:8080` on the
  plugin's own docker bridge (`spr-headscale`); the management API is only
  reachable over the SPR plugin unix socket

## How it integrates with SPR

SPR proxies `/plugins/spr-headscale/…` to the plugin's unix socket at
`/state/plugins/spr-headscale/socket` and embeds the UI (served from the same
socket) as an iframe under **Plugins → spr-headscale**. The headscale daemon
itself is only exposed on the `spr-headscale` docker bridge; SPR policies
decide who can reach it.

## Exposing headscale to clients

headscale binds `CONTAINER_IP:8080` (plain HTTP) on the `spr-headscale`
bridge. Two ways to let tailscale clients reach it:

1. **LAN-only tailnet (default).** The plugin interface carries the
   `lan` policy (plus the `headscale` group), so devices on the SPR LAN can
   reach `http://CONTAINER_IP:8080` directly. Leave *Server URL* empty — the
   plugin derives `http://CONTAINER_IP:8080` automatically. Join devices while
   they are on the LAN:

   ```sh
   tailscale up --login-server http://CONTAINER_IP:8080 --authkey <key>
   ```

   Devices coordinate through headscale when at home; note that roaming
   clients cannot reach the control server from outside the LAN with this
   setup.

2. **Public server URL (out of MVP scope, documented only).** To serve
   roaming clients, put a TLS reverse proxy (or an SPR port forward to a
   proxy) in front of the container and set *Server URL* to the public
   `https://headscale.example.com` address. The URL you configure **must**
   match what clients use — it is baked into every client's registration.
   DERP and Tailscale clients expect TLS for public deployments; terminating
   TLS is the proxy's job, headscale keeps speaking HTTP on the bridge.

## Install (UI)

In the SPR UI: **Plugins → + New Plugin** and enter this repository's GitHub
URL (e.g. `https://github.com/USER/spr-headscale`). SPR clones the repo,
builds the container and starts the plugin. The `plugin.json`
`NetworkCapabilities` register the `spr-headscale` interface with the
`wan`, `dns` and `lan` policies automatically.

## Install (CLI)

```sh
git clone https://github.com/USER/spr-headscale
cd spr-headscale
./install.sh    # prompts for SUPERDIR and an SPR API token
```

`install.sh` writes the API token, builds and starts the container, and
registers the container IP with SPR's firewall
(`PUT /firewall/custom_interface`, policies `wan dns lan`).

## API

All endpoints are served over the plugin unix socket and reachable (with SPR
auth) at `/plugins/spr-headscale/<path>`.

| Method | Path | Description |
| --- | --- | --- |
| GET | `/status` | Daemon state, headscale version/commit, server URL, listen address |
| GET | `/config` | Plugin configuration (no secrets stored) |
| PUT | `/config` | Validate + save config, regenerate `config.yaml`, restart headscale |
| GET | `/users` | List users |
| POST | `/users` | Create user — body `{"Name": "alice"}` |
| DELETE | `/users/{id}` | Destroy user by numeric id |
| GET | `/preauthkeys?user=<id>` | List preauth keys (optionally per user). Key values are **masked to a prefix** |
| POST | `/preauthkeys` | Create key — body `{"User": 1, "Reusable": false, "Ephemeral": false, "Expiration": "1h"}`. The **full key appears only in this response** |
| GET | `/nodes` | List nodes (name, IPs, user, online, last seen, expiry) |
| DELETE | `/nodes/{id}` | Delete node |
| POST | `/nodes/{id}/expire` | Expire node key (forces re-authentication) |
| POST | `/restart` | Regenerate config and restart the headscale daemon |
| GET | `/topology` | Topology graph for SPR's network map — see below |

### Topology

`plugin.json` sets `"HasTopology": true`, so SPR merges this plugin's graph
into the router-wide topology view. `GET /topology` returns
`{"Nodes": [...], "Edges": [...]}` with one `Kind: "device"` node per
headscale node (hostname, tailnet IP, live online state, `ConnType:
"tailscale"`) plus the `root` anchor node SPR grafts the graph onto; each
device has an edge toward `root` on the `tailscale` layer. When the headscale
daemon is down the endpoint degrades to the bare root anchor instead of
erroring.

## Configuration reference

`/configs/plugins/spr-headscale/config.json` (managed via the UI / API):

| Field | Default | Meaning |
| --- | --- | --- |
| `ServerURL` | `""` (derive `http://CONTAINER_IP:8080`) | headscale `server_url`; what clients dial. `http(s)://host[:port]` only |
| `BaseDomain` | `headscale.internal` | MagicDNS base domain; must differ from the `ServerURL` host |
| `MagicDNS` | `true` | Toggle headscale MagicDNS |
| `DERPEnabled` | `true` | `true`: use Tailscale's default public DERP relay map; `false`: no relays (direct connections only). The embedded DERP server stays off (it would require TLS) |

The headscale `config.yaml` is regenerated from a vendored template
(`code/templates/config.yaml.tmpl`) on every start and config change — do not
edit it by hand. Fixed choices: SQLite at
`/state/plugins/spr-headscale/data/db.sqlite`, noise private key at
`/configs/spr-headscale/noise_private.key` (0600), CLI over the in-container
unix socket, metrics/debug listener disabled, remote gRPC disabled, logtail
(client log upload to Tailscale Inc) disabled, update checks disabled.

## Security model

- **No published host ports**; `network_mode: host` is not used. The only
  listeners are the plugin unix socket (0770) and headscale on the container
  IP `:8080` on the dedicated `spr-headscale` bridge, gated by SPR
  policies/groups (`wan`/`dns` egress for the DERP map, `lan` so LAN devices
  can coordinate).
- **No extra capabilities** (`cap_add` empty — headscale is a plain userspace
  process), no devices, `security_opt: no-new-privileges:true`.
- **Secrets**: the noise private key and headscale database stay in mounted
  volumes; the key is kept 0600 inside `/configs`. The plugin's own config
  contains no secrets and its API never echoes key material: preauth keys are
  masked to a prefix everywhere except the single create response.
- **Input validation**: every user-supplied value (usernames, ids, durations,
  URLs, domains) is allow-list validated server-side, and the headscale CLI is
  invoked with argv arrays only — no shell interpolation. The generated YAML
  only receives canonicalized, character-restricted values.
- remote gRPC is never enabled; the CLI talks to headscale over
  `/var/run/headscale/headscale.sock` inside the container.

## Reproducible builds

All build inputs are pinned in `reproducible.env`: base images by digest,
the Go toolchain by version + sha256 (both amd64 and arm64), Ubuntu packages
from a dated `snapshot.ubuntu.com` snapshot, and headscale by release tag +
full commit hash (the Dockerfile verifies the tag still resolves to the
pinned commit before building).

- `./build_docker_compose.sh` — reproducible local build (buildx +
  `rewrite-timestamp`, pins injected as build args)
- `./update-pins.sh` — re-resolve every pin (image digests, latest Go patch
  release + checksums, latest stable headscale release + commit) and sync the
  Dockerfile ARG defaults

## Upstream

- [juanfont/headscale](https://github.com/juanfont/headscale) — BSD-3-Clause
  license. This plugin builds unmodified headscale from source at a pinned
  release; it is not affiliated with the headscale project or Tailscale Inc.
- Wishlist context: [spr-networks/super#341](https://github.com/spr-networks/super/issues/341)

## License

MIT — see [LICENSE](LICENSE).
