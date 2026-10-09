# Release builds are shrunk, optimized and obfuscated by R8 in full mode (the AGP default;
# nothing here or in gradle.properties turns it off). Never add -dontobfuscate.
#
# Keep rules stay minimal: kotlinx.serialization, OkHttp, CameraX, WorkManager, Glance,
# UnifiedPush and Firebase all ship consumer rules, every network model is @Serializable
# with a plugin-generated serializer, and manifest components are kept by AAPT's rules.

# Readable stack traces through the mapping file (keep each release's mapping.txt; see
# docs/android/README.md), without the original source file names in the APK.
-keepattributes SourceFile,LineNumberTable
-renamesourcefileattribute SourceFile

# Move every obfuscated class into one unnamed package. -allowaccessmodification comes
# from proguard-android-optimize.txt.
-repackageclasses

# No verbose, debug or info logging in release builds. Warnings and errors stay in Logcat
# (tag BridgeGateway); the full connection log is kept in the app (Connection log > Copy).
# R8 also drops the strings built only for these calls.
-assumenosideeffects class android.util.Log {
    public static int v(...);
    public static int d(...);
    public static int i(...);
}
