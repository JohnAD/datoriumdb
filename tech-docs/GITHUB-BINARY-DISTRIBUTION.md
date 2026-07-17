# GitHub Binary Distribution

This document explains how to use GitHub to build, publish, and host distributable binaries for DatoriumDB.

DatoriumDB ships two separate binaries (see [COMMAND-LINE-TOOLS.md](COMMAND-LINE-TOOLS.md)):

- `datoriumdb` — the database server
- `datoriumctl` — the establishment / operator CLI

They must remain separate artifacts. Do not publish a single binary that switches roles by argument or subcommand.

Container images are covered briefly at the end. The primary distribution path for operators who are not using Docker is **GitHub Releases** with attached archive assets.

## Recommended Approach

Use this combination:

1. **Git tags** for versioned releases (for example `v0.1.0`)
2. **GitHub Actions** to cross-compile the two binaries for the target platforms
3. **GitHub Releases** to host the downloadable archives and checksums

That keeps distribution inside the same repository that already runs CI (`.github/workflows/ci.yml`). No separate artifact server is required for MVP.

An optional, widely used alternative is [GoReleaser](https://goreleaser.com/) driving the same GitHub Releases target. The steps below work with either a hand-written workflow or GoReleaser; the GitHub-side setup is the same.

## One-Time Repository Setup

### 1. Confirm the default branch and permissions

In the GitHub repository:

1. Open **Settings → Actions → General**
2. Under **Workflow permissions**, choose **Read and write permissions**
3. Enable **Allow GitHub Actions to create and approve pull requests** only if you also want automation that opens PRs; it is not required for binary publishing

The release workflow needs `contents: write` so it can create a Release and upload assets. Keep that permission scoped to the release workflow, not to every CI job.

### 2. Decide who may cut releases

Pick one policy and stick to it:

- **Tag-push releases** (recommended): anyone with permission to push tags to the default remote can cut a release. The workflow runs on `push` of tags matching `v*`.
- **Manual `workflow_dispatch`**: maintainers run the workflow from the Actions UI and pass a version input. Useful if tags are protected or signed elsewhere.

Protect the default branch with required CI checks so only green commits are tagged.

### 3. Optional: protect version tags

Under **Settings → Rules → Rulesets** (or classic branch/tag protection):

- Restrict who can create tags matching `v*`
- Require that the tagged commit has passed CI

This prevents accidental or unauthorized publishes.

### 4. Optional: GitHub Packages (container images)

If you also want to publish the multi-stage `Dockerfile` image:

1. The default `GITHUB_TOKEN` can push to **GitHub Container Registry** (`ghcr.io`) when the workflow has `packages: write`
2. Package visibility defaults to private for private repos; set the package to public under **Packages** if anonymous pulls are desired

Binary archives on Releases and images on GHCR are complementary; operators can use either.

## Versioning And Tags

Use semantic version tags with a leading `v`:

```text
v0.1.0
v0.1.1
v0.2.0
```

Suggested rules:

- Tag only commits that have already passed the unit, contract, and integration CI jobs
- Prefer annotated tags: `git tag -a v0.1.0 -m "v0.1.0"`
- Never move or delete a published tag once assets are attached; cut a new patch version instead
- Embed the version into the binaries at build time with `-ldflags` (see below) so `datoriumctl` / future version commands can report it

Pre-releases use a suffix GitHub understands, for example `v0.2.0-rc.1`. Mark those Releases as **pre-release** so Tools and Dependabot-style consumers can ignore them.

## What To Publish Per Release

For each version, publish at least:

| Asset pattern | Contents |
| --- | --- |
| `datoriumdb_<version>_<os>_<arch>.tar.gz` | `datoriumdb` binary |
| `datoriumctl_<version>_<os>_<arch>.tar.gz` | `datoriumctl` binary |
| `checksums.txt` | SHA-256 digests of every archive |

MVP platform matrix (adjust as needed):

| OS | Arch | Notes |
| --- | --- | --- |
| `linux` | `amd64` | Primary server target; Compose / CI |
| `linux` | `arm64` | Common cloud / Raspberry-class hosts |
| `darwin` | `amd64` | Intel macOS operator machines |
| `darwin` | `arm64` | Apple Silicon operator machines |

Windows is optional for MVP. If added later, use `.zip` instead of `.tar.gz`.

Build with:

```text
CGO_ENABLED=0
-trimpath
-ldflags="-s -w -X main.version=<version>"
```

`CGO_ENABLED=0` matches the production `Dockerfile` and produces static binaries that do not depend on a local libc on the target host.

## Example Release Workflow

Create `.github/workflows/release.yml` (this file is not required to exist yet; add it when you are ready to publish). The sketch below is enough for GitHub-hosted binaries without GoReleaser:

```yaml
name: Release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        include:
          - goos: linux
            goarch: amd64
          - goos: linux
            goarch: arm64
          - goos: darwin
            goarch: amd64
          - goos: darwin
            goarch: arm64
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true

      - name: Build binaries
        env:
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
          CGO_ENABLED: "0"
          VERSION: ${{ github.ref_name }}
        run: |
          mkdir -p dist
          ldflags="-s -w -X main.version=${VERSION}"
          go build -trimpath -ldflags="$ldflags" -o "dist/datoriumdb" ./cmd/datoriumdb
          go build -trimpath -ldflags="$ldflags" -o "dist/datoriumctl" ./cmd/datoriumctl
          tar -C dist -czf "datoriumdb_${VERSION}_${GOOS}_${GOARCH}.tar.gz" datoriumdb
          tar -C dist -czf "datoriumctl_${VERSION}_${GOOS}_${GOARCH}.tar.gz" datoriumctl

      - name: Upload archives
        uses: softprops/action-gh-release@v2
        with:
          files: |
            datoriumdb_*.tar.gz
            datoriumctl_*.tar.gz
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

Add a final job (or a follow-up step after the matrix) that concatenates SHA-256 checksums into `checksums.txt` and uploads that file to the same Release. Matrix jobs uploading the same Release work with `softprops/action-gh-release` as long as each job attaches only its own archives.

### GoReleaser alternative

If you prefer GoReleaser:

1. Add a `.goreleaser.yaml` that builds `cmd/datoriumdb` and `cmd/datoriumctl` for the matrix above
2. Add a workflow that runs `goreleaser release --clean` on `v*` tags
3. Grant the job `contents: write` (and `packages: write` if also pushing images)

GoReleaser will create the GitHub Release, attach archives, and generate checksums. Keep the binary names and archive layout consistent with the table above so install docs stay stable.

## Cutting A Release (Operator Steps)

1. Ensure `main` (or the release branch) is green in CI
2. Update any release notes / changelog you maintain
3. Create and push an annotated tag:

   ```text
   git checkout main
   git pull
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

4. Open the **Actions** tab and confirm the Release workflow succeeded
5. Open **Releases** for the repository and verify:
   - the Release exists for `v0.1.0`
   - both binaries are present for each OS/arch
   - `checksums.txt` is attached
   - the Release body lists notable changes

Edit the Release body on GitHub if the workflow only created a stub description.

## How Operators Download Binaries

Public repository example:

```text
https://github.com/<owner>/datoriumdb/releases/latest
https://github.com/<owner>/datoriumdb/releases/download/v0.1.0/datoriumdb_v0.1.0_linux_amd64.tar.gz
https://github.com/<owner>/datoriumdb/releases/download/v0.1.0/datoriumctl_v0.1.0_linux_amd64.tar.gz
```

Install sketch for Linux amd64:

```text
VERSION=v0.1.0
curl -fsSL -O "https://github.com/<owner>/datoriumdb/releases/download/${VERSION}/datoriumdb_${VERSION}_linux_amd64.tar.gz"
curl -fsSL -O "https://github.com/<owner>/datoriumdb/releases/download/${VERSION}/datoriumctl_${VERSION}_linux_amd64.tar.gz"
curl -fsSL -O "https://github.com/<owner>/datoriumdb/releases/download/${VERSION}/checksums.txt"
sha256sum -c checksums.txt --ignore-missing
tar -xzf "datoriumdb_${VERSION}_linux_amd64.tar.gz"
tar -xzf "datoriumctl_${VERSION}_linux_amd64.tar.gz"
sudo install -m 0755 datoriumdb datoriumctl /usr/local/bin/
```

For private repositories, downloads require authentication (a personal access token or `gh release download`). Document that requirement for internal operators.

The GitHub CLI shortcut:

```text
gh release download v0.1.0 --repo <owner>/datoriumdb --pattern 'datorium*_linux_amd64.tar.gz'
```

## Container Images On GitHub (Optional)

The repository already has a production-like multi-stage `Dockerfile` that builds both binaries. To host images beside Releases:

1. Add a workflow job that builds and pushes to `ghcr.io/<owner>/datoriumdb:<version>` and `:latest` (only for non-pre-release tags)
2. Set job permissions:

   ```yaml
   permissions:
     contents: read
     packages: write
   ```

3. Authenticate with:

   ```yaml
   - uses: docker/login-action@v3
     with:
       registry: ghcr.io
       username: ${{ github.actor }}
       password: ${{ secrets.GITHUB_TOKEN }}
   ```

4. Tag images with the same git version used for binary Releases so binary and image lines stay aligned

Pull example:

```text
docker pull ghcr.io/<owner>/datoriumdb:v0.1.0
```

Compose topologies under `deploy/` can keep building from the local `Dockerfile` for development, and pin `image: ghcr.io/<owner>/datoriumdb:v…` for published environments.

## Relationship To Existing CI

`.github/workflows/ci.yml` validates code quality, tests, coverage, crash suites, Compose E2E, and a `docker build` smoke check. It does **not** publish binaries.

Keep those concerns separate:

| Workflow | Trigger | Publishes artifacts? |
| --- | --- | --- |
| `ci.yml` | PR, `main`, nightly, manual | No (test artifacts only) |
| `release.yml` (to add) | `v*` tags | Yes — GitHub Release assets (and optionally GHCR) |

Do not attach release binaries from the PR CI workflow. Always publish from an immutable tag.

## Security Notes

- Prefer the built-in `GITHUB_TOKEN` over a long-lived personal access token for Release uploads
- If you later sign binaries or checksums with cosign/GPG, store the private key in GitHub Actions **secrets** or **OIDC**-backed key management; never commit signing material
- Establishment / JWT signing keys (`DATORIUMDB_SIGNING_KEY_FILE`, bootstrap secrets) are **runtime** secrets for a deployment. They are unrelated to release artifact signing and must never be baked into published binaries or container layers
- Publish checksums so operators can verify downloads even when GitHub serves the files over HTTPS

## Checklist Before The First Public Binary Release

- [ ] Release workflow added and tested with a pre-release tag (for example `v0.0.0-test.1`)
- [ ] Archives contain only the intended binary (no config, keys, or `/db` data)
- [ ] `checksums.txt` is attached
- [ ] Release notes describe upgrade / config expectations for that version
- [ ] README links to the latest Release download page
- [ ] (Optional) GHCR image published with the same version tag
- [ ] Pre-release test tag deleted or clearly marked so it is not mistaken for production

## Out Of Scope For This Document

- Linux distribution packages (deb/rpm/Homebrew taps)
- Automatic update clients inside `datoriumdb`
- Publishing to Docker Hub or third-party registries other than GHCR
- Code signing for macOS notarization or Windows Authenticode

Those can be layered on top of the same GitHub Release version line later.
