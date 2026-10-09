# Store listing metadata

F-Droid (and other fastlane-aware stores) read the app's listing from `metadata/android/<locale>/`.
This directory sits in the Gradle module, which is the `subdir` an F-Droid build recipe points at
(`android/gateway/app`), so F-Droid finds it there.

| File | Limit | Notes |
| --- | --- | --- |
| `en-US/title.txt` | 50 characters (Google Play: 30) | |
| `en-US/short_description.txt` | 80 characters | |
| `en-US/full_description.txt` | 4000 characters | Simple HTML (`<p>`, `<b>`, `<ul>`, `<li>`) is allowed. |
| `en-US/changelogs/<versionCode>.txt` | 500 characters | One per release. The version code is computed from the tag, see below. |

`versionCode` is `major × 1,000,000 + minor × 10,000 + patch × 100 + (rc number, or 99 for a final
release)`, so 1.0.0 is `1000099` and 1.0.1 is `1000199` (see `docs/releasing.md`). Add the changelog
file in the same commit that bumps the version.

## F-Droid build recipe

F-Droid builds the `foss` flavor from a tag, without the release workflow's environment. Two
things follow:

* `BRIDGE_VERSION_NAME` is not set, so the version comes from the `versionCode` and `versionName`
  literals in `defaultConfig` (`app/build.gradle.kts`), which F-Droid's update checker also reads.
  Bump both in the release commit; the build fails if they disagree with the formula.
* F-Droid signs with its own key (unless the build is reproducible and F-Droid publishes ours), so
  the F-Droid and GitHub APKs cannot update each other. Switching means uninstalling and pairing
  again.

The fdroiddata recipe is `android/gateway/fdroid/dev.bridge.gateway.yml`; submission steps are in
`docs/android/f-droid.md`.

The `foss` flavor has no Google Play services or Firebase. Its Google-namespaced libraries are all
open source: ZXing (QR decoding), CameraX's Guava and Dagger, and UnifiedPush's Tink, Protobuf and
Gson. Firebase is only in `gmsImplementation`.

## Screenshots and graphics

```text
metadata/android/en-US/images/
  icon.png                 512 × 512, the launcher icon (teal tile, dark mark)
  featureGraphic.png       1024 × 500, mark, wordmark and tagline on the dark background
  phoneScreenshots/        1.png, 2.png, … in display order; see the README.txt there
```

Google Play and F-Droid both use these. Take screenshots on a test project with made-up numbers;
never publish real phone numbers or message text.
