# GitHub Binary Distribution

This document explains how to use GitHub to build, publish, and host distributable binaries for DatoriumDB.

DatoriumDB ships two separate binaries (see [COMMAND-LINE-TOOLS.md](COMMAND-LINE-TOOLS.md)):

- `datoriumdb` — the database server
- `datoriumctl` — the establishment / operator CLI

They must remain separate artifacts. Do not publish a single binary that switches roles by argument or subcommand.

The distribution path is **GitHub Releases** with attached archive assets, triggered by pushing a version tag.

## Approach

Use this combination:

1. **Git tags** for versioned releases (for example `v0.1.0`)
2. **GitHub Actions** to cross-compile the two binaries for the target platforms
3. **GitHub Releases** to host the downloadable archives and checksums

That keeps distribution inside the same repository that already runs CI (`.github/workflows/ci.yml`). No separate artifact server is required for MVP.

An optional alternative is [GoReleaser](https://goreleaser.com/) driving the same GitHub Releases target. The steps below work with either a hand-written workflow or GoReleaser; the GitHub-side setup is the same.

Publishing container images to GitHub Packages / GHCR is out of scope for now. The local `Dockerfile` remains for development and Compose; operators download binaries from Releases.

## One-Time Repository Setup

### 1. Confirm Actions permissions

In the GitHub repository:

1. Open **Settings → Actions → General**
2. Under **Workflow permissions**, choose **Read and write permissions**
3. Leave **Allow GitHub Actions to create and approve pull requests** disabled — not needed for binary publishing, and this project does not use automation that opens PRs

The release workflow needs `contents: write` so it can create a Release and upload assets. Keep that permission scoped to the release workflow, not to every CI job.

### 2. Tag-push releases

There is no separate GitHub toggle labeled “tag-push releases.” You enable this model by (A) adding a release workflow that runs on version tags, and (B) protecting `main` so you only tag commits that already passed CI. There is no manual **Actions → Run workflow** release path.

#### A. Confirm the release workflow is on the default branch

The workflow file already lives in the repo at
`.github/workflows/release.yml`.

1. Commit and push it on `main` if it is not already there
2. Open the repo’s **Actions** tab and confirm a workflow named **Release**
   appears (it will show no runs until the first `v*` tag is pushed)

Until that file is on the default branch, pushing a tag will not publish
binaries.

#### B. Protect `main` with required CI checks

Do this so maintainers only tag commits that CI has already validated.

1. Open the repository on GitHub
2. Click **Settings**
3. In the left sidebar, click **Rules** → **Rulesets**
4. Click **New ruleset** → **New branch ruleset**
5. Set **Ruleset name** to something like `protect-main`
6. Set **Enforcement status** to **Active**
7. Under **Target branches**, click **Add target** → **Include default branch**
   (or **Include by pattern** and enter `main`)
8. Under **Rules**, enable:
   - **Restrict deletions**
   - **Block force pushes**
   - **Require a pull request before merging** — leave this **off** if you
     push straight to `main` as maintainers and do not use PRs (this project’s
     policy). If you later adopt internal PRs among maintainers, turn it on then.
   - **Require status checks to pass** — turn this **on**, then:
     1. Enable **Require branches to be up to date before merging** if you
        want the tip of `main` itself always green before further pushes land
     2. Click **Add checks**
     3. Add the CI job names from `.github/workflows/ci.yml` that must be
        green before you treat a commit as releasable. At minimum add the
        jobs that always run on `main`, for example:
        - `format / vet / deps`
        - `unit tests (-race)`
        - `contract + integration tests`
        - `crash tests (SIGKILL, Linux)`
        - `docker build`
     4. Exact names must match the `name:` fields in `ci.yml` (GitHub’s
        picker lists them after those jobs have run at least once on the
        branch)
9. Click **Create** (or **Save changes**)

**Note:** If maintainers push commits directly to `main` (no pull requests),
GitHub’s “require status checks” rule mainly applies when something is
*merged* into the branch. It will not always block a direct `git push` to
`main`. In that workflow, the practical gate is still: open **Actions**,
confirm the latest run on `main` is green, then create and push the tag.
The ruleset is still worth having for **no force-push** and **no branch
deletion**.

After CI is green on `main`, create and push an annotated tag (see
[Cutting A Release](#cutting-a-release-operator-steps)). That tag push is
what starts the **Release** workflow.

### 3. Restrict who can create tags and open PRs

This repository is not open to public contribution via pull requests. Keep contribution and tag creation limited to trusted maintainers.

**Tags** — under **Settings → Rules → Rulesets** (or classic tag protection):

- Restrict who can create tags matching `v*` to maintainers (or a named team)
- Optionally require that the tagged commit has passed CI

Only people allowed to push those tags can cut a release.

**Pull requests** — under **Settings → General → Features** (and related access settings):

- Do not enable or solicit public pull requests
- Keep the contributor set to repository collaborators / the owning organization only
- If the repository is public for download visibility, still disable or ignore unsolicited PRs; do not merge changes from unknown contributors

Code changes land through maintainers with push access (or an internal process), not through public PR review.

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

## Release Workflow

The canonical workflow is `.github/workflows/release.yml`. On each `v*` tag
push it:

1. Cross-compiles `datoriumdb` and `datoriumctl` for linux/darwin × amd64/arm64
2. Builds per-platform `.tar.gz` archives
3. Writes `checksums.txt` (SHA-256)
4. Creates a GitHub Release and attaches every archive plus the checksums file

Prefer editing that file in place over maintaining a second copy here.

### GoReleaser alternative

If you later prefer GoReleaser instead of the hand-written workflow:

1. Add a `.goreleaser.yaml` that builds `cmd/datoriumdb` and `cmd/datoriumctl` for the same OS/arch matrix
2. Replace `.github/workflows/release.yml` with a workflow that runs `goreleaser release --clean` on `v*` tags
3. Keep job permission `contents: write`

Keep the binary names and archive layout consistent with the table above so install docs stay stable.

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

## Relationship To Existing CI

`.github/workflows/ci.yml` validates code quality, tests, coverage, crash suites, Compose E2E, and a `docker build` smoke check. It does **not** publish binaries.

Keep those concerns separate:

| Workflow | Trigger | Publishes artifacts? |
| --- | --- | --- |
| `ci.yml` | `main`, nightly, manual (and any internal CI triggers you keep) | No (test artifacts only) |
| `release.yml` | `v*` tags | Yes — GitHub Release assets |

Do not attach release binaries from the CI workflow. Always publish from an immutable tag.

## Security Notes

- Prefer the built-in `GITHUB_TOKEN` over a long-lived personal access token for Release uploads
- If you later sign binaries or checksums with cosign/GPG, store the private key in GitHub Actions **secrets** or **OIDC**-backed key management; never commit signing material
- Establishment / JWT signing keys (`DATORIUMDB_SIGNING_KEY_FILE`, bootstrap secrets) are **runtime** secrets for a deployment. They are unrelated to release artifact signing and must never be baked into published binaries or container layers
- Publish checksums so operators can verify downloads even when GitHub serves the files over HTTPS

## Checklist Before The First Public Binary Release

- [ ] Release workflow on `main` tested with a pre-release tag (for example `v0.0.0-test.1`)
- [ ] Archives contain only the intended binary (no config, keys, or `/db` data)
- [ ] `checksums.txt` is attached
- [ ] Release notes describe upgrade / config expectations for that version
- [ ] README links to the latest Release download page
- [ ] Tag creation restricted to maintainers; public PRs not accepted
- [ ] Pre-release test tag deleted or clearly marked so it is not mistaken for production

## Out Of Scope For This Document

- Publishing container images to GitHub Packages / GHCR (may be added later)
- Linux distribution packages (deb/rpm/Homebrew taps)
- Automatic update clients inside `datoriumdb`
- Publishing to Docker Hub or other third-party registries
- Code signing for macOS notarization or Windows Authenticode

Those can be layered on top of the same GitHub Release version line later.
