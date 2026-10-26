// The fixture of the tests (navigation_test.go): update.sh compiles it into classes.zip.
pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositories {
        google()
        mavenCentral()
        maven { url = uri("https://androidx.dev/snapshots/builds/15953794/artifacts/repository") }
    }
}
rootProject.name = "navapp"
include(":app")
