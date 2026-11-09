# gomobile 产物：Go 侧经 JNI 按名反射调用 Java 类与方法，一律保留原名
-keep class go.** { *; }
-keep class mobile.** { *; }

# Flutter 插件经反射注册，保留
-keep class io.flutter.plugin.** { *; }
-keep class io.flutter.embedding.** { *; }

# Flutter 内置 Play Store 分发组件引用的 play core 不在依赖里（本项目不使用 deferred components）
-dontwarn com.google.android.play.core.**
