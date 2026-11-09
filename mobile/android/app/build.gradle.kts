plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

android {
    namespace = "com.liyou.liyou_p2p"
    compileSdk = 36
    ndkVersion = "28.2.13676358"

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "com.liyou.liyou_p2p"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = 24
        targetSdk = 36
        // Uses the version code from pubspec.yaml. When using split APKs, 1000 * ABI_VERSION
        // is added automatically by Flutter. (https://developer.android.com/studio/build/configure-apk-splits#configure-APK-versions)
        // You can force using the value of versionCode by specifying the `-P force-version-code-ignoring-abi=true`
        // flag during build.
        versionCode = flutter.versionCode
        versionName = flutter.versionName
        // 仅打包手机架构（arm64 + armv7a）：Go 网络栈 .so 占 APK 大头，
        // 剔除 x86/x86_64（仅模拟器用）可将体积砍掉近半，保持单 APK 可直接安装
        ndk {
            abiFilters += listOf("arm64-v8a", "armeabi-v7a")
        }
    }

    // 仅打包手机架构（arm64 + armv7a）：Go 网络栈与 Flutter 引擎 .so 占 APK 大头，
    // 剔除 x86/x86_64（仅模拟器用）保持单 APK 可直接安装且体积减半
    packaging {
        jniLibs {
            excludes += setOf("lib/x86/**", "lib/x86_64/**")
            // .so 在 APK 内压缩存储（安装时解压）：分发体积优先，下载更快
            useLegacyPackaging = true
        }
    }

    buildTypes {
        release {
            // TODO: Add your own signing config for the release build.
            // Signing with the debug keys for now, so `flutter run --release` works.
            signingConfig = signingConfigs.getByName("debug")
            // R8 代码收缩 + 资源收缩：dex/资源瘦身；gomobile JNI 反射类在 proguard-rules.pro 全量保留
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    // gomobile bind 产物：Go Agent 网络栈（ws 信令/UDP 打洞/smux 复用/AES-GCM 可靠层）
    implementation(files("libs/liyou.aar"))
}
