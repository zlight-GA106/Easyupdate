plugins { id("com.android.application"); id("org.jetbrains.kotlin.android") }

android {
    namespace = "com.zlight106.easyupdate.demo"
    compileSdk = 35
    defaultConfig {
        applicationId = "com.zlight106.easyupdate.demo"
        minSdk = 19
        targetSdk = 35
        testInstrumentationRunner = "com.zlight106.easyupdate.demo.SmokeInstrumentation"
        versionCode = (project.findProperty("demoVersionCode") as String?)?.toInt() ?: 1
        versionName = (project.findProperty("demoVersionName") as String?) ?: "1.0.0"
    }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_1_8; targetCompatibility = JavaVersion.VERSION_1_8 }
    kotlinOptions { jvmTarget = "1.8" }
}
dependencies { implementation("androidx.core:core:1.12.0") }
