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
| `@kroszborg/bridge` (TypeScript SDK) | npm (`latest`, or `next` for release candidates) | npm, when `NPM_PUBLISH` is `true` |

Every binary, archive, image and APK gets a signed build provenance attestation, so anyone can check
it was built from this repository by this workflow:

```bash
gh attestation verify bridgectl_1.1.0_linux_amd64.tar.gz --repo kroszborg/bridge
gh attestation verify oci://ghcr.io/kroszborg/bridge-api:1.1.0 --repo kroszborg/bridge
```

GitHub creates attestations only for public repositories (or organizations on a paid plan).
While the repository is private, the workflow skips them, and `checksums.txt` and
`apk-checksums.txt` are the integrity check.

Images are tagged `1.1.0` and `1.1`, plus `latest` for final releases (not release candidates).
Image names are always lowercase, whatever the case of the GitHub account.

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

### npm

The SDK is published by the `SDK on npm` job, which runs only when the repository **variable**
`NPM_PUBLISH` is `true`. npm lets you set up trusted publishing only for a package that already
exists, so the first release needs a token:

1. Create a granular access token on npmjs.com that can publish to `@kroszborg`, and add it as the
   secret `NPM_TOKEN`. Set the variable `NPM_PUBLISH=true`, then cut a release.
2. On npmjs.com, open `@kroszborg/bridge` → **Settings** → **Trusted publishing**, and add GitHub
   Actions with repository `kroszborg/bridge` and workflow `release.yml`.
3. Delete the `NPM_TOKEN` secret. Later releases publish through OIDC, with no stored token.

Provenance (`--provenance`) is added while the repository is public. The job skips a version that
is already on npm, so re-running a release is safe.

## Cutting a release

1. Make sure `main` is green.
2. In `CHANGELOG.md`, rename `## [Unreleased]` to `## [1.1.0] - 2026-11-02` (today's date) and add a
   fresh empty `## [Unreleased]` above it. The release notes are taken from this section; the
   workflow stops if it is missing.
3. Commit, then tag and push:

   ```bash
   git commit -am "Release 1.1.0"
   git tag -a v1.1.0 -m "Bridge 1.1.0"
   git push origin main v1.1.0
   ```

4. Watch the **Release** workflow. When it finishes, the release is published with every artifact
   attached.

Release candidates use `-rc.N`: `v1.1.0-rc.1` is marked as a pre-release on GitHub, does not move
`latest` (npm publishes it under `next`), and its APK version code (1010001) is lower than the final
release's (1010099), so phones can update from an RC to the release. Only `vX.Y.Z` and `vX.Y.Z-rc.N` tags are accepted.

The Android version comes from the tag: `versionName` is the version and `versionCode` is
`major × 1,000,000 + minor × 10,000 + patch × 100 + (rc number, or 99 for a final release)`, so
1.0.0 is 1000099. In the release commit, set both `versionCode` and `versionName` in
`defaultConfig` in `android/gateway/app/build.gradle.kts` (the build fails if they disagree with
the formula) and add `android/gateway/app/fastlane/metadata/android/en-US/changelogs/<versionCode>.txt`,
so builds without the tag (F-Droid) get the same version. F-Droid reads those two literal values to
detect new releases; see [F-Droid](android/f-droid.md).

## When a release fails

The release page stays a draft, visible only to maintainers. Fix the problem on `main`, then move
the tag and push it again. GoReleaser replaces the existing draft:

```bash
git tag -d v1.1.0 && git push origin :refs/tags/v1.1.0
git tag -a v1.1.0 -m "Bridge 1.1.0" && git push origin v1.1.0
```

Images already pushed for that version are overwritten by the new run.

## Trying it locally

Build every binary without publishing anything:

```bash
docker run --rm -v "$PWD:/src" -w /src goreleaser/goreleaser:v2.18.2 release --snapshot --clean
ls dist/
```
