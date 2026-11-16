import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import '../models/map_rule.dart';

/// 正式包判定：release 构建（kReleaseMode）且显式注入 --dart-define=RELEASE=true。
/// 此时登录页隐藏服务器地址输入、固定内置平台地址，日志与界面文本脱敏。
const bool kHideServerAddr = kReleaseMode && bool.fromEnvironment('RELEASE');

/// 内置默认平台地址（正式包固定使用）。58894 为 TCP 直射端点，WS 可用；
/// gnway.org:880 HTTPS 代理不转发 WS 升级头（信令 400），暂不可用。
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

/// 纯内部日志：对用户没有意义，直接丢弃。
const List<String> _logNoise = [
  '管理通道已就绪',
  '管理通道启动失败',
  '演示页已启动',
  'STUN 公网探测',
  '打洞目标已就绪',
  '更换套接字',
  '等待对端候选',
];

final RegExp _logTimePrefix = RegExp(r'^\[\d{2}:\d{2}:\d{2}\]\s*(\[[^\]]*\]\s*)?');
final RegExp _urlRe = RegExp(r'(https?|wss?)://\S+');
final RegExp _tokenRe = RegExp(r'[（(]?\btoken=\S*?[）)]?');
final RegExp _spaceRe = RegExp(r'[ \t]{2,}');

/// 一条技术日志 → 用户语言的改写规则（与桌面端 gui-walk/gui-wails 的 userlog.go 对齐）。
class _LogRule {
  final RegExp re;
  final String Function(Match) repl;
  const _LogRule(this.re, this.repl);
}

final List<_LogRule> _logRules = [
  _LogRule(RegExp(r'已连接(平台)?信令通道.*'), (_) => '已连接到服务器'),
  _LogRule(RegExp(r'信令通道就绪.*'), (_) => '已连接到服务器，服务已就绪'),
  _LogRule(RegExp(r'信令连接断开.*'), (_) => '与服务器的连接中断，正在自动重连…'),
  _LogRule(RegExp(r'连接平台失败.*'), (_) => '暂时无法连接服务器，正在重试…'),
  _LogRule(
    RegExp(r'映射 (\S+) 已监听 \S+：本机访问 \S+ 即达服务端内网主机 (\S+) 的端口 (\d+)'),
    (m) => '已开通：本机端口 ${m[1]} ↔ 服务器内网主机 ${m[2]} 的 ${m[3]} 端口',
  ),
  _LogRule(
    RegExp(r'映射 (\S+) 已监听 \S+：本机访问 \S+ 即达服务端端口 (\d+)'),
    (m) => '已开通：本机端口 ${m[1]} ↔ 服务器 ${m[2]} 端口',
  ),
  _LogRule(RegExp(r'映射 (\S+) 已监听.*'), (m) => '已开通：本机端口 ${m[1]}'),
  _LogRule(RegExp(r'映射 (\S+) 已停止监听.*'), (m) => '已断开：本机端口 ${m[1]}'),
  _LogRule(
    RegExp(r'映射 (\S+) 入口 \S+ 监听异常.*'),
    (m) => '本机端口 ${m[1]} 无法继续使用（可能已被其他程序占用），可删除后重新添加',
  ),
  _LogRule(RegExp(r'对端不支持多路复用.*'), (_) => '对方版本较旧，已自动使用兼容方式连接'),
  _LogRule(RegExp(r'直连升级第 \d+ 次尝试未获.*'), (_) => '正在优化连接线路，稍后自动重试'),
  _LogRule(RegExp(r'请求未得到响应.*'), (_) => '请求超时，请确认服务器与对端设备在线'),
  _LogRule(RegExp(r'目标主机未被服务器允许.*'), (_) => '服务器未允许访问该内网主机，请联系管理员'),
  _LogRule(RegExp(r'连接中继失败.*'), (_) => '线路切换中，正在尝试其他路径…'),
  _LogRule(RegExp(r'回退中继模式.*'), (_) => '已切换到中转线路'),
  _LogRule(RegExp(r'打洞成功.*'), (_) => '已建立直连'),
  _LogRule(RegExp(r'^会话 \S+[：:]\s*'), (_) => ''),
];

/// 把底层连接程序的一行日志转成面向用户的文字；返回 null 表示该行应丢弃。
String? userizeLog(String line) {
  var s = line.trim();
  if (s.isEmpty) return null;
  for (final noise in _logNoise) {
    if (s.contains(noise)) return null;
  }
  s = s.replaceFirst(_logTimePrefix, '');
  for (final r in _logRules) {
    if (r.re.hasMatch(s)) {
      s = s.replaceAllMapped(r.re, r.repl);
      break;
    }
  }
  s = s.replaceAll(_urlRe, '服务器');
  s = s.replaceAll(_tokenRe, '');
  s = s.replaceAll('Agent', '连接程序');
  s = s.replaceAll('WebSocket', '网络连接');
  s = s.replaceAll('sha256', '完整性校验');
  s = s.replaceAll(_spaceRe, ' ').trim();
  s = s.replaceAll(RegExp(r'^[：:，,\s]+|[：:，,\s]+$'), '');
  return s.isEmpty ? null : s;
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
  /// 底层日志含大量技术细节：先翻译成用户语言（内部行直接丢弃），再脱敏平台地址。
  Stream<LogEntry> logs() {
    return _logStream ??= _logs.receiveBroadcastStream().map((e) {
      final raw = (e as Map)['text'] as String? ?? '';
      final text = userizeLog(raw);
      if (text == null) return null;
      return LogEntry(
        level: (e)['level'] as String? ?? 'info',
        text: sanitizeServerText(text),
      );
    }).where((e) => e != null).cast<LogEntry>();
  }
}

final p2p = P2PBridge();
