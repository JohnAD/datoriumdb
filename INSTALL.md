# Installing DatoriumDB

This guide covers installing the published binaries on Linux and using
DatoriumDB inside Docker. Releases are published from
[JohnAD/datoriumdb](https://github.com/JohnAD/datoriumdb).

Two binaries ship in each release:

| Binary | Purpose |
| --- | --- |
| `datoriumdb` | Database server |
| `datoriumctl` | Establishment / operator CLI |

Download pages:

- Latest release: https://github.com/JohnAD/datoriumdb/releases/latest
- Example tag used below: [v0.0.1](https://github.com/JohnAD/datoriumdb/releases/tag/v0.0.1)

## Install on Linux

### 1. Choose the archive for your CPU

| Architecture | `datoriumdb` archive | `datoriumctl` archive |
| --- | --- | --- |
| x86_64 (`amd64`) | `datoriumdb_<version>_linux_amd64.tar.gz` | `datoriumctl_<version>_linux_amd64.tar.gz` |
| aarch64 (`arm64`) | `datoriumdb_<version>_linux_arm64.tar.gz` | `datoriumctl_<version>_linux_arm64.tar.gz` |

Check your machine with `uname -m` (`x86_64` → amd64, `aarch64` → arm64).

### 2. Download, verify, and install

Replace `VERSION` if you want a specific tag; otherwise the latest tag
works the same way from the [latest release](https://github.com/JohnAD/datoriumdb/releases/latest)
page.

```text
VERSION=v0.0.1
ARCH=amd64   # or arm64

curl -fsSL -O "https://github.com/JohnAD/datoriumdb/releases/download/${VERSION}/datoriumdb_${VERSION}_linux_${ARCH}.tar.gz"
curl -fsSL -O "https://github.com/JohnAD/datoriumdb/releases/download/${VERSION}/datoriumctl_${VERSION}_linux_${ARCH}.tar.gz"
curl -fsSL -O "https://github.com/JohnAD/datoriumdb/releases/download/${VERSION}/checksums.txt"

sha256sum -c checksums.txt --ignore-missing

tar -xzf "datoriumdb_${VERSION}_linux_${ARCH}.tar.gz"
tar -xzf "datoriumctl_${VERSION}_linux_${ARCH}.tar.gz"
sudo install -m 0755 datoriumdb datoriumctl /usr/local/bin/
```

Confirm:

```text
command -v datoriumdb datoriumctl
```

### 3. Minimal single-node start (establishment server)

`datoriumdb` always needs:

1. its own server name
2. the establishment server base URL

On a single machine that is also the establishment server, both refer to
the same host. You also need an establishment config directory (schemas,
shard map, auth public keys, and so on). See
[tech-docs/ESTABLISHMENT-CONFIG.md](tech-docs/ESTABLISHMENT-CONFIG.md) and
[tech-docs/AUTHENTICATION.md](tech-docs/AUTHENTICATION.md).

Example layout:

```text
/var/lib/datoriumdb/          # data directory (--data-dir)
/var/lib/datoriumdb/.config/  # establishment config (--config-dir)
```

Example start for a local establishment node named `serverA`:

```text
export DATORIUMDB_SIGNING_KEY_FILE=/path/to/ed25519-signing-key.pem

datoriumdb serverA http://127.0.0.1:8080 \
  --listen 127.0.0.1:8080 \
  --config-dir /var/lib/datoriumdb/.config \
  --data-dir /var/lib/datoriumdb
```

Liveness check:

```text
curl -sS http://127.0.0.1:8080/datoriumdb/v1/health
```

Non-establishment servers set `DATORIUMDB_MACHINE_BOOTSTRAP_SECRET`
instead of holding the signing private key, and point the second startup
argument at the establishment server’s base URL.

Use `datoriumctl` against the establishment config directory to create
collections, issue client tokens, and manage auth keys. See
[tech-docs/COMMAND-LINE-TOOLS.md](tech-docs/COMMAND-LINE-TOOLS.md).

## Use with Docker

There is no published image on a registry yet. You can either build the
image from this repository’s `Dockerfile`, or copy the release binaries
into your own image.

### Option A — Build the project image

From a clone of the repository:

```text
git clone https://github.com/JohnAD/datoriumdb.git
cd datoriumdb
git checkout v0.0.1   # or another release tag
docker build -t datoriumdb:v0.0.1 .
```

The image:

- installs `datoriumdb` and `datoriumctl` under `/usr/local/bin`
- runs as a non-root `datorium` user
- expects persistent data at `/db` (mount a volume there)
- listens on port `8080` inside the container when started with
  `--listen 0.0.0.0:8080`
- exposes `GET /datoriumdb/v1/health` for health checks

Run a container (you must supply server name, establishment URL, config,
and secrets):

```text
docker run --rm \
  --name datoriumdb \
  -p 8080:8080 \
  -v /var/lib/datoriumdb:/db \
  -v /path/to/signing-key.pem:/secrets/signing-key.pem:ro \
  -e DATORIUMDB_SIGNING_KEY_FILE=/secrets/signing-key.pem \
  datoriumdb:v0.0.1 \
  serverA http://127.0.0.1:8080 --listen 0.0.0.0:8080
```

Put establishment config files in `/db/.config` on the volume (or bind-mount
them). For Compose examples, see `deploy/docker-compose.single-node.yml` in
the repository.

The entrypoint also accepts
`DATORIUMDB_MACHINE_BOOTSTRAP_SECRET_FILE` for Docker/Compose secrets: the
file contents are loaded into `DATORIUMDB_MACHINE_BOOTSTRAP_SECRET` before
the server starts.

### Option B — Add release binaries to your own image

Use this when you want a custom base image or an existing app image that
also runs DatoriumDB.

```dockerfile
FROM debian:bookworm-slim

ARG VERSION=v0.0.1
ARG ARCH=amd64

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL -o /tmp/datoriumdb.tar.gz \
      "https://github.com/JohnAD/datoriumdb/releases/download/${VERSION}/datoriumdb_${VERSION}_linux_${ARCH}.tar.gz" \
 && curl -fsSL -o /tmp/datoriumctl.tar.gz \
      "https://github.com/JohnAD/datoriumdb/releases/download/${VERSION}/datoriumctl_${VERSION}_linux_${ARCH}.tar.gz" \
 && curl -fsSL -o /tmp/checksums.txt \
      "https://github.com/JohnAD/datoriumdb/releases/download/${VERSION}/checksums.txt" \
 && cd /tmp \
 && sha256sum -c checksums.txt --ignore-missing \
 && tar -xzf datoriumdb.tar.gz -C /usr/local/bin \
 && tar -xzf datoriumctl.tar.gz -C /usr/local/bin \
 && chmod 0755 /usr/local/bin/datoriumdb /usr/local/bin/datoriumctl \
 && rm -f /tmp/datoriumdb.tar.gz /tmp/datoriumctl.tar.gz /tmp/checksums.txt

# Persist database files here.
VOLUME ["/db"]
WORKDIR /db
EXPOSE 8080

# Supply <serverName> <establishmentBaseURL> and flags at runtime.
ENTRYPOINT ["/usr/local/bin/datoriumdb"]
```

Build and run:

```text
docker build -t my-datoriumdb:v0.0.1 .
docker run --rm -p 8080:8080 -v datorium-data:/db my-datoriumdb:v0.0.1 \
  serverA http://127.0.0.1:8080 --listen 0.0.0.0:8080 \
  --config-dir /db/.config --data-dir /db
```

For Alpine-based images, use `apk add --no-cache ca-certificates curl` (or
`wget`) instead of `apt-get`. The published Linux binaries are statically
linked (`CGO_ENABLED=0`), so they run on typical glibc and musl images.

### Multi-node Compose

The repository’s `deploy/` directory has ready Compose topologies
(single-node, five-shard, split SOT/read, and others). Those files build
from the project `Dockerfile` and mount fixture configs for local testing.
Adapt them for production by replacing fixture configs and secrets with
your own.

## Next reading

- [tech-docs/ESTABLISHMENT-CONFIG.md](tech-docs/ESTABLISHMENT-CONFIG.md) — config files and startup
- [tech-docs/AUTHENTICATION.md](tech-docs/AUTHENTICATION.md) — signing keys, bootstrap secret, tokens
- [tech-docs/COMMAND-LINE-TOOLS.md](tech-docs/COMMAND-LINE-TOOLS.md) — `datoriumctl`
- [DEVELOPERS.md](DEVELOPERS.md) — building from source and running tests
