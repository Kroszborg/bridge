# Store listing metadata

F-Droid (and other fastlane-aware stores) read the app's listing from `metadata/android/<locale>/`.
This directory sits in the Gradle module, which is the `subdir` an F-Droid build recipe points at
(`android/gateway/app`), so F-Droid finds it there.

| File | Limit | Notes |
| --- | --- | --- |
| `en-US/title.txt` | 50 characters | |
| `en-US/short_description.txt` | 80 characters | |
| `en-US/full_description.txt` | 4000 characters | Simple HTML (`<p>`, `<b>`, `<ul>`, `<li>`) is allowed. |
| `en-US/changelogs/<versionCode>.txt` | 500 characters | One per release. The version code is computed from the tag, see below. |

`versionCode` is `major × 1,000,000 + minor × 10,000 + patch × 100 + (rc number, or 99 for a final
release)`, so 1.0.0 is `1000099` and 1.0.1 is `1000199` (see `docs/releasing.md`). Add the changelog
file in the same commit that bumps the version.

## F-Droid build recipe

F-Droid builds the `foss` flavor from a tag, without the release workflow's environment. Two
things follow:

* `BRIDGE_VERSION_NAME` is not set, so the version comes from the default in
  `app/build.gradle.kts` (`?: "1.0.0"`). Bump that default in the release commit so the tag, the
  default and the changelog file agree.
* F-Droid signs with its own key (unless the build is reproducible and F-Droid publishes ours), so
  the F-Droid and GitHub APKs cannot update each other. Switching means uninstalling and pairing
  again.

A starting point for `metadata/dev.bridge.gateway.yml` in fdroiddata:

```yaml
Categories: [Connectivity, System]
License: AGPL-3.0-only
SourceCode: https://github.com/kroszborg/bridge
IssueTracker: https://github.com/kroszborg/bridge/issues
Builds:
  - versionName: 1.0.0
    versionCode: 1000099
    commit: v1.0.0
    subdir: android/gateway/app
    gradle: [foss]
AutoUpdateMode: Version
UpdateCheckMode: Tags ^v[0-9]+\.[0-9]+\.[0-9]+$
```

`versionCode` is computed rather than written literally in `build.gradle.kts`, so F-Droid's update
checker may not read it; if not, the fdroiddata maintainers add each build entry by hand.

The `foss` flavor has no Google Play services or Firebase. Its Google-namespaced libraries are all
open source: ZXing (QR decoding), CameraX's Guava and Dagger, and UnifiedPush's Tink, Protobuf and
Gson. Firebase is only in `gmsImplementation`.

## Screenshots and graphics

Not committed yet. Add PNG or JPEG files here (F-Droid picks them up as is):

```text
metadata/android/en-US/images/
  icon.png                 512 × 512; without it F-Droid uses the launcher icon from the APK
  featureGraphic.png       1024 × 500, optional
  phoneScreenshots/1.png   portrait phone screenshots, in display order (1.png, 2.png, …)
```

Suggested screenshots: the welcome screen, the gateway status with its reliability checklist, the
Messages tab, the Send tab with a delivered message, and the Phones tab. Take them on a test
project with made-up numbers; never publish real phone numbers or message text.
