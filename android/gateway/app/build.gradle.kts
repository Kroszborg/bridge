import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

// Release builds take their version from the git tag (BRIDGE_VERSION_NAME, set by
// .github/workflows/release.yml). 1.0.0-rc.1 → 1000001, 1.0.0 → 1000099: always increasing.
// Builds without it (F-Droid, local) use the literals in defaultConfig, which F-Droid's update
// checker reads from this file. Nothing else (no timestamp, no git hash) goes into the version.
fun versionCodeOf(version: String): Int {
    val match = Regex("""^(\d+)\.(\d+)\.(\d+)(?:-rc\.(\d+))?$""").matchEntire(version)
        ?: error("BRIDGE_VERSION_NAME must be X.Y.Z or X.Y.Z-rc.N, got \"$version\"")
    val (major, minor, patch, rc) = match.destructured
    return major.toInt() * 1_000_000 + minor.toInt() * 10_000 + patch.toInt() * 100 + (rc.toIntOrNull() ?: 99)
}

android {
    namespace = "dev.bridge.gateway"
    compileSdk = 37

    defaultConfig {
        applicationId = "dev.bridge.gateway"
        minSdk = 26
        targetSdk = 37
        // Bump both, and add fastlane/.../changelogs/<versionCode>.txt, in the release commit.
        versionCode = 1000099
        versionName = "1.0.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    check(versionCodeOf(defaultConfig.versionName!!) == defaultConfig.versionCode) {
        "defaultConfig.versionCode must be versionCodeOf(\"${defaultConfig.versionName}\") = " +
            "${versionCodeOf(defaultConfig.versionName!!)}"
    }
    System.getenv("BRIDGE_VERSION_NAME")?.takeIf { it.isNotBlank() }?.let { tag ->
        defaultConfig.versionName = tag
        defaultConfig.versionCode = versionCodeOf(tag)
    }

    // foss: 100% open source, UnifiedPush wake-ups. Published on GitHub Releases (and F-Droid later).
    // gms:  adds Firebase Cloud Messaging for servers that configure it.
    flavorDimensions += "distribution"
    productFlavors {
        create("foss") {
            dimension = "distribution"
            isDefault = true
        }
        create("gms") {
            dimension = "distribution"
            versionNameSuffix = "-gms"
        }
    }

    // Release signing: the BRIDGE_KEYSTORE_* environment (CI, see .github/workflows/release.yml), or
    // a git-ignored android/gateway/keystore.properties for local builds. With neither, release
    // builds are unsigned (F-Droid signs its own; Play needs the upload key). No key material
    // lives in the repository.
    val keystoreProperties = Properties().apply {
        val file = rootProject.file("keystore.properties")
        if (file.isFile) file.inputStream().use(::load)
    }
    fun signingValue(env: String, property: String): String? =
        System.getenv(env)?.takeIf { it.isNotBlank() } ?: keystoreProperties.getProperty(property)
    val releaseKeystore = System.getenv("BRIDGE_KEYSTORE_FILE")?.takeIf { it.isNotBlank() }?.let(::file)
        ?: keystoreProperties.getProperty("storeFile")?.let(rootProject::file)
    signingConfigs {
        if (releaseKeystore != null) {
            create("release") {
                storeFile = releaseKeystore
                storePassword = signingValue("BRIDGE_KEYSTORE_PASSWORD", "storePassword")
                keyAlias = signingValue("BRIDGE_KEY_ALIAS", "keyAlias")
                keyPassword = signingValue("BRIDGE_KEY_PASSWORD", "keyPassword")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            if (releaseKeystore != null) signingConfig = signingConfigs.getByName("release")
            // No git commit in the APK or bundle: reproducible from the tag's source alone.
            vcsInfo.include = false
        }
    }

    // The dependency list AGP embeds is encrypted with a Google key, so F-Droid rejects APKs that
    // carry it. Play Console reads it from the bundle for SDK warnings, so bundles keep it.
    dependenciesInfo {
        includeInApk = false
        includeInBundle = true
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    testOptions {
        unitTests.isReturnDefaultValues = true
        unitTests.isIncludeAndroidResources = true
    }

    packaging {
        resources.excludes += setOf("/META-INF/{AL2.0,LGPL2.1}", "/META-INF/versions/9/OSGI-INF/MANIFEST.MF")
    }

    lint {
        abortOnError = true
        warningsAsErrors = false
        checkDependencies = true
        disable += setOf("GradleDependency", "NewerVersionAvailable", "AndroidGradlePluginVersion")
    }
}

kotlin {
    // JDK 25 (LTS) runs the compiler and unit tests; Android bytecode stays at Java 17 (compileOptions).
    jvmToolchain(25)
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.lifecycle.service)
    implementation(libs.androidx.datastore.preferences)
    implementation(libs.androidx.work.runtime)
    // Home-screen widget (AndroidX, open source; in both flavors).
    implementation(libs.androidx.glance.appwidget)

    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.ui.tooling.preview)
    implementation(libs.compose.material3)
    debugImplementation(libs.compose.ui.tooling)

    implementation(libs.camera.camera2)
    implementation(libs.camera.lifecycle)
    implementation(libs.camera.view)
    implementation(libs.zxing.core)

    implementation(libs.okhttp)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.kotlinx.coroutines.android)

    "fossImplementation"(libs.unifiedpush.connector)
    "gmsImplementation"(libs.firebase.messaging)
    // Firebase pulls in an old Fragment; activity-result APIs need >= 1.3.
    "gmsImplementation"(libs.androidx.fragment)

    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
    testImplementation(libs.okhttp.mockwebserver)
    testImplementation(libs.robolectric)
    testImplementation(libs.androidx.test.core)
}

// Robolectric reaches into JDK internals that newer JDKs no longer export by default.
tasks.withType<Test>().configureEach {
    jvmArgs(
        "--add-exports=java.base/jdk.internal.access=ALL-UNNAMED",
        "--add-opens=java.base/jdk.internal.access=ALL-UNNAMED",
        "--add-opens=java.base/java.io=ALL-UNNAMED",
    )
}
