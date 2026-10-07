# Releasing Bridge

A release is cut by pushing a tag. [`.github/workflows/release.yml`](../.github/workflows/release.yml)
runs the full CI suite first, then builds and publishes everything below. The GitHub release stays a
**draft** until every part has succeeded, so a failed run never leaves a half-finished release
page. (Images are pushed as soon as they are built, so a failed run can leave images behind;
re-running the release overwrites them.)

| Artifact | Where | Built by |
| --- | --- | --- |
| `bridgectl` for Linux, macOS, Windows (amd64, arm64) | GitHub release | GoReleaser ([`.goreleaser.yaml`](../.goreleaser.yaml)) |
| `bridge` server binary for Linux, macOS (amd64, arm64) | GitHub release | GoReleaser |
| `bridge-api` and `bridge-dashboard` images (amd64, arm64) | `ghcr.io/kroszborg/…` and Docker Hub `kroszborg/…` | Docker Buildx, native runner per architecture |
| `bridge-gateway-<version>-foss.apk` and `-gms.apk`, signed | GitHub release | Gradle |
| `checksums.txt`, `apk-checksums.txt` | GitHub release | GoReleaser, the workflow |

Every binary, archive, image and APK gets a signed build provenance attestation, so anyone can check
it was built from this repository by this workflow:

```bash
gh attestation verify bridgectl_0.3.0_linux_amd64.tar.gz --repo kroszborg/bridge
gh attestation verify oci://ghcr.io/kroszborg/bridge-api:0.3.0 --repo kroszborg/bridge
```

GitHub creates attestations only for public repositories (or organizations on a paid plan).
While the repository is private, the workflow skips them, and `checksums.txt` and
`apk-checksums.txt` are the integrity check.

Images are tagged `0.3.0` and `0.3`, plus `latest` for releases that are not pre-releases. Image
names are always lowercase, whatever the case of the GitHub account.

## One-time setup

### Docker Hub

Create an access token (Read & Write) on Docker Hub, then add these repository secrets under
**Settings → Secrets and variables → Actions**:

| Secret | Value |
| --- | --- |
| `DOCKERHUB_USERNAME` | Your Docker Hub username |
| `DOCKERHUB_TOKEN` | The access token |

Images go to `docker.io/<repository owner>/bridge-*`. If your Docker Hub namespace is different, add
a repository **variable** `DOCKERHUB_NAMESPACE`. Without the secrets, releases publish to GHCR only
and the run shows a warning.

### GHCR

Nothing to configure: the workflow pushes with its own `GITHUB_TOKEN`. New packages on GHCR are
**private** until you change them. After the first release, open each package
(`bridge-api`, `bridge-dashboard`) under your profile's **Packages**, choose **Package settings →
Change visibility → Public**. The images are linked to this repository automatically.

### Android signing key

Android only installs an update when it is signed with the same key as the installed app. **If the
key is lost, every user has to uninstall and pair again.** Generate it once, on your own machine:

```bash
keytool -genkeypair -keystore bridge-release.jks -storetype PKCS12 \
  -alias bridge -keyalg RSA -keysize 4096 -validity 10000 \
  -dname "CN=Bridge Gateway"
```

Keep `bridge-release.jks` and its password in your password manager and an offline backup. Never
commit it (`*.jks` and `*.keystore` are git-ignored). Then add these secrets:

| Secret | Value |
| --- | --- |
| `BRIDGE_KEYSTORE_BASE64` | `base64 -w0 bridge-release.jks` (macOS: `base64 -i bridge-release.jks`) |
| `BRIDGE_KEYSTORE_PASSWORD` | The keystore password |
| `BRIDGE_KEY_ALIAS` | `bridge` |
| `BRIDGE_KEY_PASSWORD` | The key password (the same as the keystore password for PKCS12) |

Without `BRIDGE_KEYSTORE_BASE64`, a release has no APKs and the run shows a warning.

## Cutting a release

1. Make sure `main` is green.
2. In `CHANGELOG.md`, rename `## [Unreleased]` to `## [0.3.0] - 2026-10-07` (today's date) and add a
   fresh empty `## [Unreleased]` above it. The release notes are taken from this section; the
   workflow stops if it is missing.
3. Commit, then tag and push:

   ```bash
   git commit -am "Release 0.3.0"
   git tag -a v0.3.0 -m "Bridge 0.3.0"
   git push origin main v0.3.0
   ```

4. Watch the **Release** workflow. When it finishes, the release is published with every artifact
   attached.

Pre-releases use `-rc.N`: `v0.3.0-rc.1` is marked as a pre-release, does not move `latest`, and its
APK version code (30001) is lower than the final release's (30099), so phones can update from an
RC to the release. Only `vX.Y.Z` and `vX.Y.Z-rc.N` tags are accepted.

The Android version comes from the tag: `versionName` is the version and `versionCode` is
`major × 1,000,000 + minor × 10,000 + patch × 100 + (rc number, or 99 for a final release)`.

## When a release fails

The release page stays a draft, visible only to maintainers. Fix the problem on `main`, then move
the tag and push it again. GoReleaser replaces the existing draft:

```bash
git tag -d v0.3.0 && git push origin :refs/tags/v0.3.0
git tag -a v0.3.0 -m "Bridge 0.3.0" && git push origin v0.3.0
```

Images already pushed for that version are overwritten by the new run.

## Trying it locally

Build every binary without publishing anything:

```bash
docker run --rm -v "$PWD:/src" -w /src goreleaser/goreleaser:v2.18.2 release --snapshot --clean
ls dist/
```
