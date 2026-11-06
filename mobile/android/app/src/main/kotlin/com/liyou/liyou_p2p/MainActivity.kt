package com.liyou.liyou_p2p

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import androidx.core.content.ContextCompat
import androidx.core.content.FileProvider
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel
import mobile.LogListener
import mobile.Mobile
import java.io.File

/// Flutter ↔ Go Agent 桥：
///  - MethodChannel liyou.p2p/agent：login/startClient/stop/getStatus/addMapping/removeMapping/setForeground
///  - MethodChannel liyou.app/system：getFilesDir/installApk（应用更新下载安装）
///  - EventChannel  liyou.p2p/logs：Go 日志回调 → 主线程 → Dart 流
class MainActivity : FlutterActivity() {
    private val mainHandler by lazy { Handler(Looper.getMainLooper()) }
    private var logSink: EventChannel.EventSink? = null

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)

        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "liyou.p2p/agent").setMethodCallHandler { call, result ->
            try {
                when (call.method) {
                    "login" -> result.success(
                        Mobile.login(
                            call.argument<String>("platformURL"),
                            call.argument<String>("tenant"),
                            call.argument<String>("user"),
                            call.argument<String>("pass"),
                            call.argument<String>("devName"),
                        )
                    )
                    "startClient" -> {
                        Mobile.startClient(
                            call.argument<String>("deviceToken"),
                            call.argument<String>("platformWS"),
                            call.argument<String>("mapsJSON"),
                        )
                        result.success(true)
                    }
                    "stop" -> {
                        Mobile.stop()
                        result.success(true)
                    }
                    "getStatus" -> result.success(Mobile.getStatus())
                    "addMapping" -> {
                        Mobile.addMapping(
                            call.argument<String>("host") ?: "",
                            (call.argument<Number>("remote") ?: 0).toLong(),
                            call.argument<String>("local") ?: "",
                        )
                        result.success(true)
                    }
                    "removeMapping" -> {
                        Mobile.removeMapping(
                            call.argument<String>("host") ?: "",
                            (call.argument<Number>("remote") ?: 0).toLong(),
                            call.argument<String>("local") ?: "",
                        )
                        result.success(true)
                    }
                    "setForeground" -> {
                        val on = call.argument<Boolean>("on") ?: false
                        val text = call.argument<String>("text") ?: "连接运行中"
                        if (on) {
                            requestNotifPermissionIfNeeded()
                            ForegroundService.text = text
                            ContextCompat.startForegroundService(this, Intent(this, ForegroundService::class.java))
                        } else {
                            stopService(Intent(this, ForegroundService::class.java))
                        }
                        result.success(true)
                    }
                    // 退出应用 = 退到后台（不杀进程，保持 P2P 连接存活）
                    "moveToBack" -> {
                        moveTaskToBack(true)
                        result.success(true)
                    }
                    else -> result.notImplemented()
                }
            } catch (e: Throwable) {
                result.error("P2P_ERROR", e.message ?: e.toString(), null)
            }
        }

        // 应用级系统能力：更新 APK 下载目录 + 拉起安装器
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "liyou.app/system").setMethodCallHandler { call, result ->
            try {
                when (call.method) {
                    "getFilesDir" -> result.success(
                        getExternalFilesDir(null)?.absolutePath ?: filesDir.absolutePath
                    )
                    "installApk" -> {
                        installApk(call.argument<String>("path") ?: "")
                        result.success(true)
                    }
                    else -> result.notImplemented()
                }
            } catch (e: Throwable) {
                result.error("APP_ERROR", e.message ?: e.toString(), null)
            }
        }

        EventChannel(flutterEngine.dartExecutor.binaryMessenger, "liyou.p2p/logs").setStreamHandler(
            object : EventChannel.StreamHandler {
                override fun onListen(args: Any?, events: EventChannel.EventSink?) {
                    logSink = events
                    Mobile.setLogCallback(object : LogListener {
                        override fun onLog(level: String?, text: String?) {
                            mainHandler.post {
                                logSink?.success(mapOf("level" to (level ?: "info"), "text" to (text ?: "")))
                            }
                        }
                    })
                }

                override fun onCancel(args: Any?) {
                    Mobile.setLogCallback(null)
                    logSink = null
                }
            }
        )
    }

    private fun requestNotifPermissionIfNeeded() {
        if (Build.VERSION.SDK_INT >= 33 &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 1001)
        }
    }

    /// 拉起系统安装器（FileProvider 授权读取，Android 8+ 需用户授予「安装未知应用」）。
    private fun installApk(path: String) {
        val file = File(path)
        if (!file.exists()) throw IllegalArgumentException("APK 文件不存在")
        val uri: Uri = FileProvider.getUriForFile(this, "$packageName.fileprovider", file)
        val intent = Intent(Intent.ACTION_VIEW).setDataAndType(uri, "application/vnd.android.package.archive")
        intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
        startActivity(intent)
    }
}
