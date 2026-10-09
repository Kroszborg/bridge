# kotlinx.serialization ships its own consumer rules; keep the protocol models'
# names readable in crash reports.
-keepattributes SourceFile,LineNumberTable
-renamesourcefileattribute SourceFile
# OkHttp (including the session cookie jar) and the account API models need no
# extra rules either: both libraries ship consumer rules, and every model is
# @Serializable with a plugin-generated serializer.
# No -assumenosideeffects for android.util.Log: the gateway's connection log is mirrored to
# Logcat (tag BridgeGateway) and diagnosing a user's phone relies on it in release builds.
