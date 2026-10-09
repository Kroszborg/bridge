plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

// Release builds take their version from the git tag (BRIDGE_VERSION_NAME, set by
// .github/workflows/release.yml). 1.0.0-rc.1 → 1000001, 1.0.0 → 1000099: always increasing.
val releaseVersion: String = System.getenv("BRIDGE_VERSION_NAME") ?: "1.0.0"

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
        versionCode = versionCodeOf(releaseVersion)
        versionName = releaseVersion
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
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

    // Release signing comes from the environment so no key material lives in the repository.
    val releaseKeystore = System.getenv("BRIDGE_KEYSTORE_FILE")
    signingConfigs {
        if (releaseKeystore != null) {
            create("release") {
                storeFile = file(releaseKeystore)
                storePassword = System.getenv("BRIDGE_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("BRIDGE_KEY_ALIAS")
                keyPassword = System.getenv("BRIDGE_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            if (releaseKeystore != null) signingConfig = signingConfigs.getByName("release")
        }
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
