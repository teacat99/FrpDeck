// FrpDeck app module — the Android shell that hosts the Compose UI and
// embeds the gomobile-produced frpdeckmobile.aar.
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
    id("org.jetbrains.kotlin.plugin.compose")
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
        compose = true
        buildConfig = true
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

    // AndroidX core / lifecycle.
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.8.6")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.8.6")
    implementation("androidx.activity:activity-compose:1.9.2")

    // Compose BOM keeps every Compose artefact on the same release.
    val composeBom = platform("androidx.compose:compose-bom:2024.09.02")
    implementation(composeBom)
    androidTestImplementation(composeBom)
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-graphics")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")
    implementation("androidx.navigation:navigation-compose:2.8.0")

    // Foreground-service helper (NotificationCompat etc.).
    implementation("androidx.core:core:1.13.1")
    implementation("androidx.documentfile:documentfile:1.0.1")

    // Networking — Retrofit + Moshi for typed API access; OkHttp shared
    // for the WebView's cookie jar later.
    implementation("com.squareup.retrofit2:retrofit:2.11.0")
    implementation("com.squareup.retrofit2:converter-moshi:2.11.0")
    implementation("com.squareup.moshi:moshi-kotlin:1.15.1")
    implementation("com.squareup.okhttp3:logging-interceptor:4.12.0")

    // Coroutines.
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.8.1")

    // Tests.
    testImplementation("junit:junit:4.13.2")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
    androidTestImplementation("androidx.test.espresso:espresso-core:3.6.1")
    debugImplementation("androidx.compose.ui:ui-tooling")
    debugImplementation("androidx.compose.ui:ui-test-manifest")
}
