plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
}
android {
    namespace = "com.zflip5.modem"
    compileSdk = 37
    defaultConfig {
        applicationId = "com.zflip5.modem"
        minSdk = 33
        targetSdk = 34
        versionCode = 2
        versionName = "0.2.0"
    }
    buildFeatures { compose = true }
    signingConfigs {
        create("zf5") {
            storeFile = rootProject.file("../../dist/debug.keystore")
            storePassword = "android"; keyAlias = "zf5"; keyPassword = "android"
        }
    }
    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"))
            signingConfig = signingConfigs.getByName("zf5")
        }
    }
    testOptions.unitTests.isIncludeAndroidResources = true
}
androidComponents {
    beforeVariants(selector().withBuildType("release")) {
        it.hostTests[com.android.build.api.variant.HostTestBuilder.UNIT_TEST_TYPE]?.enable = false
    }
}
kotlin { jvmToolchain(21) }
dependencies {
    implementation(platform(libs.compose.bom))
    implementation(libs.material3)
    implementation(libs.ui)
    implementation(libs.activity.compose)
    implementation(libs.navigation.compose)
    implementation(libs.zxing.core)
    testImplementation(libs.ui.test.junit4)
    testImplementation(libs.junit)
    testImplementation(libs.robolectric)
    testImplementation(libs.coroutines.test)
    debugImplementation(libs.ui.test.manifest)
}
