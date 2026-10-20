plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

android {
    namespace = "net.aegisvpn"
    compileSdk = 35

    defaultConfig {
        applicationId = "net.aegisvpn"
        minSdk = 24
        targetSdk = 35
        versionCode = 1
        versionName = "1.0.0"
    }

    signingConfigs {
        create("release") {
            // Real signing material comes from the environment; see
            // scripts/build-android.sh and android/README.md.
            val keystorePath = System.getenv("AEGIS_KEYSTORE")
            if (!keystorePath.isNullOrBlank()) {
                storeFile = file(keystorePath)
                storePassword = System.getenv("AEGIS_KEYSTORE_PASSWORD") ?: ""
                keyAlias = System.getenv("AEGIS_KEY_ALIAS") ?: ""
                keyPassword = System.getenv("AEGIS_KEY_PASSWORD") ?: ""
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false // keep the build simple; AAR keeps gomobile JNI glue
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            // Signed with AEGIS_KEYSTORE when provided, debug key otherwise so the
            // APK always installs during development.
            signingConfig = if (!System.getenv("AEGIS_KEYSTORE").isNullOrBlank()) {
                signingConfigs.getByName("release")
            } else {
                signingConfigs.getByName("debug")
            }
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
    }

    lint {
        abortOnError = false
    }
}

dependencies {
    // gomobile binding of engine/cmd/aegislib (package aegislib) — built by
    // scripts/build-android.sh via: gomobile bind -target=android -androidapi 24
    //   -o android/app/libs/aegis.aar ./cmd/aegislib
    implementation(files("libs/aegis.aar"))

    implementation(platform("androidx.compose:compose-bom:2024.09.03"))
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.material:material-icons-core")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.activity:activity-compose:1.9.2")
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.8.6")
    implementation("androidx.security:security-crypto:1.1.0-alpha06")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.8.1")
}
