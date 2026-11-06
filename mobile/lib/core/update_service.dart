import 'dart:convert';
import 'dart:io';

import 'package:flutter/services.dart';

import 'p2p_bridge.dart';

/// 应用版本（与 pubspec.yaml version 保持一致，发布时同步更新）。
const String kAppVersion = '1.1.0';

/// 自动检查更新开关的持久化键（缺省为开启）。
const String kAutoUpdateKey = 'update.auto';

/// 更新信息（平台「应用更新」接口约定的 data 结构）。
class AppUpdate {
  final String version;
  final String url;
  final String notes;
  final bool force;

  const AppUpdate({
    required this.version,
    required this.url,
    required this.notes,
    this.force = false,
  });

  factory AppUpdate.fromMap(Map<dynamic, dynamic> m) => AppUpdate(
    version: (m['version'] as String?) ?? '',
    url: (m['url'] as String?) ?? '',
    notes: (m['notes'] as String?) ?? '',
    force: m['force'] == true,
  );
}

/// 解析平台更新接口响应体：{code:0, data:{version,url,notes,force}}。
/// 任何结构不符均返回 null（优雅降级，绝不抛异常）。
AppUpdate? parseLatestResponse(String body) {
  try {
    final json = jsonDecode(body);
    if (json is! Map || json['code'] != 0 || json['data'] is! Map) return null;
    final u = AppUpdate.fromMap(json['data'] as Map);
    if (u.version.isEmpty || u.url.isEmpty) return null;
    return u;
  } catch (_) {
    return null;
  }
}

/// 语义化版本比较：远端是否比当前新（按「.」分段数值比较，缺段补 0）。
bool isNewerVersion(String remote, String current) {
  List<int> parts(String v) => v
      .trim()
      .replaceFirst(RegExp(r'^[vV]'), '')
      .split('.')
      .map((p) => int.tryParse(p) ?? 0)
      .toList();
  final a = parts(remote);
  final b = parts(current);
  final n = a.length > b.length ? a.length : b.length;
  for (var i = 0; i < n; i++) {
    final x = i < a.length ? a[i] : 0;
    final y = i < b.length ? b[i] : 0;
    if (x != y) return x > y;
  }
  return false;
}

/// 从登录会话推导平台 HTTP 地址（用于更新检查）：
/// platformWs: ws://host:port/ws → http://host:port；缺失回落内置地址。
String platformBaseFromSession(Map<String, dynamic> session) {
  final ws = ((session['platformWs'] ?? '') as String).trim();
  if (ws.isEmpty) return kDefaultServerURL;
  var base = ws.startsWith('wss://')
      ? ws.replaceFirst('wss://', 'https://')
      : ws.replaceFirst('ws://', 'http://');
  final i = base.indexOf('/', base.indexOf('://') + 3);
  if (i > 0) base = base.substring(0, i);
  return base;
}

/// 更新服务：检查（失败/404/结构不符一律视为「无更新」）+ 下载（进度流）。
class UpdateService {
  UpdateService._();

  /// 检查更新；无新版本、接口未上线（404）、网络失败一律返回 null。
  static Future<AppUpdate?> checkLatest(String base) async {
    HttpClient? client;
    try {
      final uri = Uri.parse('$base/api/app/update/latest').replace(
        queryParameters: {'platform': 'android', 'version': kAppVersion},
      );
      client = HttpClient()..connectionTimeout = const Duration(seconds: 8);
      final req = await client.getUrl(uri).timeout(const Duration(seconds: 8));
      final resp = await req.close().timeout(const Duration(seconds: 10));
      if (resp.statusCode != 200) return null;
      final body = await resp.transform(utf8.decoder).join();
      final u = parseLatestResponse(body);
      if (u == null || !isNewerVersion(u.version, kAppVersion)) return null;
      return u;
    } catch (_) {
      return null;
    } finally {
      client?.close();
    }
  }

  /// 下载 APK 到 [savePath]，进度流 0.0~1.0（无 content-length 时按已下载 MB 推进）。
  static Stream<double> download(String url, String savePath) async* {
    final client = HttpClient()
      ..connectionTimeout = const Duration(seconds: 15);
    try {
      final req = await client.getUrl(Uri.parse(url));
      final resp = await req.close();
      if (resp.statusCode != 200) {
        throw Exception('下载失败：HTTP ${resp.statusCode}');
      }
      final total = resp.contentLength;
      final file = File(savePath);
      final sink = file.openWrite();
      var received = 0;
      try {
        await for (final chunk in resp) {
          received += chunk.length;
          sink.add(chunk);
          yield total > 0 ? received / total : 0.0;
        }
        await sink.flush();
      } finally {
        await sink.close();
      }
      yield 1.0;
    } finally {
      client.close();
    }
  }
}

/// 应用级系统能力通道：取应用专属目录 + 拉起 APK 安装器。
class AppInstaller {
  AppInstaller._();

  static const _ch = MethodChannel('liyou.app/system');

  /// 应用专属外部文件目录（无需存储权限）。
  static Future<String> filesDir() async =>
      await _ch.invokeMethod<String>('getFilesDir') ?? '';

  /// 拉起系统安装器（FileProvider + ACTION_VIEW）。
  static Future<void> install(String apkPath) =>
      _ch.invokeMethod('installApk', {'path': apkPath});
}
