# F-Droid

F-Droid builds the **`foss`** flavor from source itself and signs it with its own key, unless the
build is reproducible (below). The recipe is a YAML file in the
[fdroiddata](https://gitlab.com/fdroid/fdroiddata) repository; a ready draft lives in this
repository at `android/gateway/fdroid/dev.bridge.gateway.yml`.

## What F-Droid reads from this repository

| | Where |
| --- | --- |
| Build | `android/gateway/app` (the recipe's `subdir`), `gradle: [foss]`, from the tag `v1.0.0` |
| Version | `versionCode` and `versionName` literals in `defaultConfig` of `android/gateway/app/build.gradle.kts` (1000099, 1.0.0). F-Droid's update checker reads them at each new `vX.Y.Z` tag. |
| Listing | `android/gateway/app/fastlane/metadata/android/en-US/`: `title.txt`, `short_description.txt`, `full_description.txt`, `changelogs/<versionCode>.txt`, `images/icon.png`, `images/featureGraphic.png`, `images/phoneScreenshots/` |

F-Droid looks for `fastlane/metadata/android/<locale>/` in the build's `subdir` and at the
repository root; the files sit in the subdir.

## Prerequisites

* **Public repository** at `https://github.com/kroszborg/bridge`.
* **Tag `v1.0.0` pushed**, pointing at the release commit (the commit with
  `versionCode = 1000099` and `changelogs/1000099.txt`).
* **No prebuilt binaries in the source.** The only committed binary is
  `android/gateway/gradle/wrapper/gradle-wrapper.jar`, which F-Droid's scanner accepts (it builds
  with its own Gradle). Fonts in `res/font/` are OFL-licensed sources, not binaries.
* **No non-free dependencies in `foss`.** Check with:

  ```bash
  cd android/gateway
  ./gradlew :app:dependencies --configuration fossReleaseRuntimeClasspath | grep -iE 'gms|firebase|play-services|crashlytics|analytics'
  ```

  It prints nothing. The Google-namespaced libraries that remain are open source: ZXing, Guava,
  Dagger (from CameraX) and Tink, Protobuf, Gson (from UnifiedPush).
* **No dependency metadata in the APK.** `dependenciesInfo.includeInApk = false` in
  `build.gradle.kts`; F-Droid rejects APKs carrying Google's encrypted dependency block.

## Submitting

1. Fork [fdroiddata](https://gitlab.com/fdroid/fdroiddata) on GitLab and create a branch
   `dev.bridge.gateway`.
2. Copy `android/gateway/fdroid/dev.bridge.gateway.yml` to `metadata/dev.bridge.gateway.yml`.
3. Optionally, test locally with [fdroidserver](https://f-droid.org/docs/Installing_the_Server_and_Repo_Tools/)
   (Linux, or its Docker image):

   ```bash
   fdroid readmeta
   fdroid rewritemeta dev.bridge.gateway   # normalises formatting
   fdroid lint dev.bridge.gateway
   fdroid build -v -l dev.bridge.gateway   # needs an Android SDK; slow
   fdroid checkupdates dev.bridge.gateway  # finds v1.0.0 and reads the version
   ```

   Or skip this and rely on the merge request's CI pipeline, which runs the same checks and a build.
4. Commit (`New app: Bridge`), push and open a merge request against `fdroiddata` `master`, using
   the *App inclusion* template. Mention that the app is an SMS gateway for a self-hostable,
   AGPL-licensed server, that hosted Bridge runs the same open-source server, and that the `gms`
   flavor (Firebase) is excluded.
5. Answer reviewer questions in the MR. After merge the app appears in the F-Droid repository with
   the next index update, usually within a few days.

Things the pipeline may flag:

* **JDK 25.** The Kotlin toolchain is `jvmToolchain(25)`. The recipe's `sudo` block installs
  `openjdk-25-jdk-headless`; if the build server's Debian release does not ship it, the
  maintainers will suggest a backports source, or Gradle's foojay resolver can download it.
* **Gradle 9.8.** F-Droid builds with its own copy of the version in `gradle-wrapper.properties`;
  a very recent version may need F-Droid's `gradlew-fdroid` to catch up.

## Reproducible builds (optional, later)

By default F-Droid signs with its own key, so F-Droid and GitHub installs cannot update each other
(switching means uninstalling and pairing again). If F-Droid's build of the tag is byte-identical
to the GitHub APK (apart from the signature), F-Droid can ship the GitHub signature instead:

1. Uncomment `binary:` in the recipe (it points at
   `releases/download/v%v/bridge-gateway-%v-foss.apk`).
2. Add `AllowedAPKSigningKeys:` with the release certificate's SHA-256:

   ```bash
   apksigner verify --print-certs bridge-gateway-1.0.0-foss.apk | grep SHA-256
   # lowercase, without colons
   ```

The build already avoids the usual differences: no timestamps or git hashes (`vcsInfo.include =
false`), a version code computed only from the version, and no dependency metadata. Remaining risks
are a different JDK or build-tools version on F-Droid's server; compare with
`apksigcopier compare` or `diffoscope` if verification fails.

## Updates

After inclusion, `AutoUpdateMode: Version` and `UpdateCheckMode: Tags ^v[0-9.]+$` make F-Droid pick
up every final `vX.Y.Z` tag (release candidates do not match) and add a build entry for it. Each
release commit must bump the `defaultConfig` literals and add `changelogs/<versionCode>.txt`.
