import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/services.dart';

import 'p2p_bridge.dart';

/// 应用版本（与 pubspec.yaml version 保持一致，发布时同步更新）。
const String kAppVersion = '1.1.6';

/// 自动检查更新开关的持久化键（缺省为开启）。
const String kAutoUpdateKey = 'update.auto';

/// 更新信息（平台 /api/update/latest?ch=android 契约的 data 结构）。
/// 服务端契约无 force 字段；为兼容既有界面调用保留该可选字段（恒为 false）。
class AppUpdate {
  final String version;
  final String url;
  final String sha256;
  final String notes;
  final bool force;

  const AppUpdate({
    required this.version,
    required this.url,
    this.sha256 = '',
    this.notes = '',
    this.force = false,
  });

  factory AppUpdate.fromMap(Map<dynamic, dynamic> m) => AppUpdate(
    version: (m['version'] as String?) ?? '',
    url: (m['url'] as String?) ?? '',
    sha256: (m['sha256'] as String?) ?? '',
    notes: (m['notes'] as String?) ?? '',
    force: m['force'] == true,
  );
}

/// 解析平台更新接口响应体：{code:0, data:{version,url,sha256,notes,releasedAt}}。
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
  /// 平台契约：GET /api/update/latest?ch=android（免鉴权），
  /// url 为相对路径（/update/xxx.apk），在此拼上平台基址返回绝对地址。
  static Future<AppUpdate?> checkLatest(String base) async {
    HttpClient? client;
    try {
      final uri = Uri.parse(
        '$base/api/update/latest',
      ).replace(queryParameters: {'ch': 'android'});
      client = HttpClient()..connectionTimeout = const Duration(seconds: 8);
      final req = await client.getUrl(uri).timeout(const Duration(seconds: 8));
      final resp = await req.close().timeout(const Duration(seconds: 10));
      if (resp.statusCode != 200) return null;
      final body = await resp.transform(utf8.decoder).join();
      final u = parseLatestResponse(body);
      if (u == null || !isNewerVersion(u.version, kAppVersion)) return null;
      // 服务端契约：url 为相对路径（/update/xxx.apk），拼上平台基址返回绝对地址；
      // 若已是完整地址则原样使用。
      if (u.url.startsWith('/update/')) {
        return AppUpdate(
          version: u.version,
          url: '$base${u.url}',
          sha256: u.sha256,
          notes: u.notes,
        );
      }
      return u;
    } catch (_) {
      return null;
    } finally {
      client?.close();
    }
  }

  /// 下载 APK 到 [savePath]，进度流 0.0~1.0（无 content-length 时按已下载 MB 推进）。
  /// [expectedSha256] 非空时下载完成后强校验（服务端发布时实算下发），不匹配即删除并报错。
  static Stream<double> download(
    String url,
    String savePath, {
    String expectedSha256 = '',
  }) async* {
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
        // 网络停滞保护：45s 无新数据视为中断，避免弱网/切换网络时无限挂起
        await for (final chunk in resp.timeout(
          const Duration(seconds: 45),
          onTimeout: (s) => s.addError(
            TimeoutException('网络停滞，下载已中断'),
          ),
        )) {
          received += chunk.length;
          sink.add(chunk);
          yield total > 0 ? received / total : 0.0;
        }
        await sink.flush();
      } finally {
        await sink.close();
      }
      if (expectedSha256.trim().isNotEmpty) {
        final digest = await sha256HexOfFile(savePath);
        if (digest.toLowerCase() != expectedSha256.trim().toLowerCase()) {
          await file.delete();
          throw Exception('安装包完整性校验失败（SHA256 不匹配），已删除下载文件');
        }
      }
      yield 1.0;
    } finally {
      client.close();
    }
  }
}

/// 计算文件 SHA256（整文件读入，APK 量级内存可承受）。
/// 内置纯 Dart 实现，避免为校验单独引入第三方依赖。
Future<String> sha256HexOfFile(String path) async {
  final bytes = await File(path).readAsBytes();
  return sha256Hex(bytes);
}

/// 纯 Dart SHA-256（FIPS 180-4）。
String sha256Hex(List<int> data) {
  const k = <int>[
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1,
    0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
    0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786,
    0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147,
    0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
    0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b,
    0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a,
    0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
    0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
  ];
  var h = <int>[
    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c,
    0x1f83d9ab, 0x5be0cd19,
  ];
  final bitLen = data.length * 8;
  final padded = <int>[...data, 0x80];
  while (padded.length % 64 != 56) {
    padded.add(0);
  }
  for (var i = 7; i >= 0; i--) {
    padded.add((bitLen >> (8 * i)) & 0xff);
  }
  final w = List<int>.filled(64, 0);
  int rotr(int x, int n) => ((x >> n) | (x << (32 - n))) & 0xffffffff;
  for (var off = 0; off < padded.length; off += 64) {
    for (var t = 0; t < 16; t++) {
      w[t] = (padded[off + t * 4] << 24) |
          (padded[off + t * 4 + 1] << 16) |
          (padded[off + t * 4 + 2] << 8) |
          padded[off + t * 4 + 3];
    }
    for (var t = 16; t < 64; t++) {
      final s0 = rotr(w[t - 15], 7) ^ rotr(w[t - 15], 18) ^ (w[t - 15] >> 3);
      final s1 = rotr(w[t - 2], 17) ^ rotr(w[t - 2], 19) ^ (w[t - 2] >> 10);
      w[t] = (w[t - 16] + s0 + w[t - 7] + s1) & 0xffffffff;
    }
    var a = h[0], b = h[1], c = h[2], d = h[3];
    var e = h[4], f = h[5], g = h[6], hh = h[7];
    for (var t = 0; t < 64; t++) {
      final s1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
      final ch = (e & f) ^ ((~e & 0xffffffff) & g);
      final t1 = (hh + s1 + ch + k[t] + w[t]) & 0xffffffff;
      final s0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
      final maj = (a & b) ^ (a & c) ^ (b & c);
      final t2 = (s0 + maj) & 0xffffffff;
      hh = g;
      g = f;
      f = e;
      e = (d + t1) & 0xffffffff;
      d = c;
      c = b;
      b = a;
      a = (t1 + t2) & 0xffffffff;
    }
    h[0] = (h[0] + a) & 0xffffffff;
    h[1] = (h[1] + b) & 0xffffffff;
    h[2] = (h[2] + c) & 0xffffffff;
    h[3] = (h[3] + d) & 0xffffffff;
    h[4] = (h[4] + e) & 0xffffffff;
    h[5] = (h[5] + f) & 0xffffffff;
    h[6] = (h[6] + g) & 0xffffffff;
    h[7] = (h[7] + hh) & 0xffffffff;
  }
  return h.map((x) => x.toRadixString(16).padLeft(8, '0')).join();
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
