// FrpDeck Android — root settings.
//
// gomobile produces frpdeckmobile.aar at <repo>/build/frpdeckmobile.aar via
// `gomobile bind -target=android …` (see android/scripts/build-aar.sh). The
// app/libs/ directory is a flat-dir Maven repository so Gradle can resolve
// the aar directly without uploading it anywhere. Once we wire GitHub
// Packages publishing, this fallback stays as the offline development path.

pluginManagement {
    repositories {
        gradlePluginPortal()
        google()
        mavenCentral()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "FrpDeck"
include(":app")
