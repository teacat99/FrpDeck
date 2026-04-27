// FrpDeck app module — the Android shell that hosts the Vue WebView and
// embeds the gomobile-produced frpdeckmobile.aar.
//
// P6′/P7′ note: the native UI was a Compose tab strip in v1; it has
// been replaced by a single full-screen WebView that loads the same
// Vue SPA the desktop / Docker builds ship. The shell is now mostly:
//   - MainActivity      → WebView host + JS bridge (`window.frpdeck`)
//   - PrepareActivity   → transparent VpnService.prepare() trampoline
//   - FrpDeckVpnService → catch-all tun + tun2socks dispatcher
//   - FrpDeckForegroundService → frpc engine lifecycle owner
// No Compose / Retrofit dependencies remain.
//
// Build flavours / variants:
//   - debug   : assembleDebug, signed with the default debug keystore
//   - release : assembleRelease, signed via env-driven keystore (CI),
//               minified + R8-shrunk; falls back to debug signing when
//               FRPDECK_SIGNING_KEYSTORE is unset so dev builds stay easy
//
// AAR provisioning:
//   - Local dev: drop frpdeckmobile.aar into app/libs/ (see ../scripts/build-aar.sh)
//   - CI / Release: same path; CI step copies <repo>/build/frpdeckmobile.aar
//     into app/libs/ before invoking gradle.
plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "io.teacat.frpdeck"
    compileSdk = 34

    defaultConfig {
        applicationId = "io.teacat.frpdeck"
        minSdk = 29
        targetSdk = 34
        versionCode = 1
        versionName = "0.1.0"

        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    signingConfigs {
        create("releaseEnv") {
            val ks = System.getenv("FRPDECK_SIGNING_KEYSTORE")
            if (!ks.isNullOrBlank()) {
                storeFile = file(ks)
                storePassword = System.getenv("FRPDECK_SIGNING_STORE_PASSWORD")
                keyAlias = System.getenv("FRPDECK_SIGNING_KEY_ALIAS")
                keyPassword = System.getenv("FRPDECK_SIGNING_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            // R8 disabled for v0.1 — gomobile-generated bridge classes
            // need explicit -keep rules we have not yet written. We keep
            // shrinkResources=false so app/build/outputs/apk/release/
            // ships the full bundle a tester can install.
            isMinifyEnabled = false
            isShrinkResources = false
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            // Pick env-signed if available, otherwise fall back so
            // `gradle assembleRelease` still produces an installable APK
            // during local development.
            signingConfig = if (System.getenv("FRPDECK_SIGNING_KEYSTORE") != null)
                signingConfigs.getByName("releaseEnv")
            else
                signingConfigs.getByName("debug")
        }
        debug {
            applicationIdSuffix = ".debug"
            isDebuggable = true
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        buildConfig = true
    }

    // ABI splits — emit one APK per native ABI plus a universal fat
    // APK. Rationale: gomobile bundles the full Go runtime as
    // libgojni.so per ABI (~50 MB each). A single fat APK shipping all
    // four ABIs balloons to ~170 MB, but a per-ABI APK is closer to
    // ~25 MB. Sideloaders pick the per-ABI build matching their
    // device, while CI publishes the universal APK as the
    // "compatible with anything" fallback.
    //
    // Naming: gradle emits files like `app-arm64-v8a-release.apk` and
    // `app-universal-release.apk` so each artefact is self-describing.
    splits {
        abi {
            isEnable = true
            reset()
            include("armeabi-v7a", "arm64-v8a", "x86", "x86_64")
            isUniversalApk = true
        }
    }

    packaging {
        resources {
            // gomobile aar bundles META-INF/{LICENSE,NOTICE} from frp +
            // gin + gorm + many transitive deps; merge instead of fail
            // when duplicate paths surface.
            excludes += setOf(
                "META-INF/LICENSE-LGPL-2.1.txt",
                "META-INF/LICENSE-LGPL-3.txt",
                "META-INF/LICENSE-W3C-TEST",
                "META-INF/DEPENDENCIES",
                "META-INF/INDEX.LIST",
            )
            pickFirsts += setOf(
                "META-INF/LICENSE",
                "META-INF/NOTICE",
                "META-INF/LICENSE.md",
                "META-INF/LICENSE.txt",
                "META-INF/NOTICE.md",
                "META-INF/NOTICE.txt",
                "META-INF/AL2.0",
                "META-INF/LGPL2.1",
            )
        }
    }
}

dependencies {
    // gomobile-produced AAR. The build is reproducible: run
    //   ./android/scripts/build-aar.sh
    // which calls gomobile bind and copies the aar into app/libs/.
    implementation(files("libs/frpdeckmobile.aar"))

    // AndroidX core / lifecycle / activity.
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.8.6")
    implementation("androidx.activity:activity-ktx:1.9.2")

    // AppCompat for the WebView host activity. The XML theme inherits
    // from MaterialComponents (no Compose deps required).
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("com.google.android.material:material:1.12.0")

    // Foreground-service helper (NotificationCompat etc.) + SAF helpers.
    implementation("androidx.documentfile:documentfile:1.0.1")

    // Coroutines — used by the bridge to dispatch SAF results back to
    // the WebView without blocking the main thread on IO.
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.8.1")

    // Tests.
    testImplementation("junit:junit:4.13.2")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
    androidTestImplementation("androidx.test.espresso:espresso-core:3.6.1")
}
