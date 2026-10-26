plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
}

android {
    namespace = "navapp"
    compileSdk = 37
    defaultConfig {
        applicationId = "navapp"
        minSdk = 24
        targetSdk = 36
    }
    buildFeatures { compose = true }
}

dependencies {
    implementation(platform("androidx.compose:compose-bom-alpha:2026.04.00"))
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.material3:material3:1.4.0")
    implementation("androidx.activity:activity-compose:1.13.0")
    implementation("androidx.lifecycle:lifecycle-viewmodel-ktx:2.11.0")
    implementation("androidx.navigation:navigation-compose:2.10.2")
    implementation("androidx.navigation:navigation-fragment-ktx:2.10.2")
    implementation("androidx.navigation3:navigation3-runtime:1.3.0-alpha02")
    implementation("androidx.navigation3:navigation3-ui:1.3.0-alpha02")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.11.0")
}
