import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import '../models/map_rule.dart';

/// 正式包判定：release 构建（kReleaseMode）且显式注入 --dart-define=RELEASE=true。
/// 此时登录页隐藏服务器地址输入、固定内置平台地址，日志与界面文本脱敏。
const bool kHideServerAddr = kReleaseMode && bool.fromEnvironment('RELEASE');

/// 内置默认平台地址（正式包固定使用）。
const String kDefaultServerURL = 'http://jyly8888.gnway.vip:58894';

const String _serverHost = 'jyly8888.gnway.vip:58894';
const String _serverHostBare = 'jyly8888.gnway.vip';

/// 正式包文本脱敏：把平台地址（http/https/ws/wss 前缀与裸域名形式）替换为「服务器」。
/// [enabled] 供单测注入；默认随 [kHideServerAddr] 生效（调试构建原样保留）。
String sanitizeServerText(String text, {bool enabled = kHideServerAddr}) {
  if (!enabled || text.isEmpty) return text;
  return text
      .replaceAll('https://$_serverHost', '服务器')
      .replaceAll('wss://$_serverHost', '服务器')
      .replaceAll('http://$_serverHost', '服务器')
      .replaceAll('ws://$_serverHost', '服务器')
      .replaceAll(_serverHost, '服务器')
      .replaceAll(_serverHostBare, '服务器');
}

/// 日志条目。
class LogEntry {
  final String level; // info | warn | error
  final String text;
  final DateTime time = DateTime.now();

  LogEntry({required this.level, required this.text});
}

/// Go Agent 桥接：MethodChannel 调用 + EventChannel 日志流。
class P2PBridge {
  static const _method = MethodChannel('liyou.p2p/agent');
  static const _logs = EventChannel('liyou.p2p/logs');

  Stream<LogEntry>? _logStream;

  /// 登录平台并注册设备，返回会话 JSON（platformWs/deviceToken/deviceUid 等）。
  Future<Map<String, dynamic>> login({
    required String platformURL,
    required String tenant,
    required String user,
    required String pass,
    required String devName,
  }) async {
    final raw = await _method.invokeMethod<String>('login', {
      'platformURL': kHideServerAddr ? kDefaultServerURL : platformURL,
      'tenant': tenant,
      'user': user,
      'pass': pass,
      'devName': devName,
    });
    return Map<String, dynamic>.from(jsonDecode(raw!) as Map);
  }

  /// 启动客户端。
  Future<void> startClient({
    required String deviceToken,
    required String platformWS,
    required List<MapRule> maps,
  }) {
    return _method.invokeMethod('startClient', {
      'deviceToken': deviceToken,
      'platformWS': platformWS,
      'mapsJSON': jsonEncode(maps.map((m) => m.toGoMap()).toList()),
    });
  }

  Future<void> stop() => _method.invokeMethod('stop');

  /// 状态：{running, wsConnected, maps:[{host,remote,local,addr,listening}]}。
  Future<Map<String, dynamic>> status() async {
    final raw = await _method.invokeMethod<String>('getStatus');
    return Map<String, dynamic>.from(jsonDecode(raw!) as Map);
  }

  Future<void> addMapping(String host, int remote, String local) =>
      _method.invokeMethod('addMapping', {
        'host': host,
        'remote': remote,
        'local': local,
      });

  Future<void> removeMapping(String host, int remote, String local) =>
      _method.invokeMethod('removeMapping', {
        'host': host,
        'remote': remote,
        'local': local,
      });

  /// 前台服务开关。
  Future<void> setForeground(bool on, String text) =>
      _method.invokeMethod('setForeground', {'on': on, 'text': text});

  /// 退到后台不杀进程（Android moveTaskToBack），保持隧道连接存活。
  Future<void> moveToBack() => _method.invokeMethod('moveToBack');

  /// 日志流（Go 回调 → Android 主线程 → Dart 广播流）。
  Stream<LogEntry> logs() {
    return _logStream ??= _logs.receiveBroadcastStream().map(
      (e) => LogEntry(
        level: (e as Map)['level'] as String? ?? 'info',
        text: sanitizeServerText((e)['text'] as String? ?? ''),
      ),
    );
  }
}

final p2p = P2PBridge();
