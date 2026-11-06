/// 端口映射规则模型与校验（纯 Dart，无 Flutter 依赖，便于单元测试）。
/// host 选填：空 = 服务端本机；填则须为精确 IPv4/IPv6/主机名。
class MapRule {
  final String host;
  final int remote;
  final String local;

  const MapRule({this.host = '', required this.remote, required this.local});

  MapRule copyWith({String? host, int? remote, String? local}) => MapRule(
    host: host ?? this.host,
    remote: remote ?? this.remote,
    local: local ?? this.local,
  );

  factory MapRule.fromMap(Map<dynamic, dynamic> m) => MapRule(
    host: (m['host'] as String?) ?? '',
    remote: (m['remote'] as num?)?.toInt() ?? 0,
    local: (m['local'] as String?) ?? '',
  );

  Map<String, dynamic> toMap() => {
    'host': host,
    'remote': remote,
    'local': local,
  };

  Map<String, dynamic> toGoMap() => {
    if (host.trim().isNotEmpty) 'host': host.trim(),
    'remote': remote,
    'local': local.trim(),
  };

  /// 规则描述串（两段式/三段式）。
  String get desc =>
      host.trim().isEmpty ? '$remote:$local' : '${host.trim()}:$remote:$local';

  /// 校验：合法返回 null，否则返回错误文案。
  String? validate() {
    final h = host.trim();
    if (h.isNotEmpty) return validateHost(h);
    if (remote < 1 || remote > 65535) return '远端端口 $remote 非法（1-65535）';
    final l = local.trim();
    if (l.isEmpty) return '本地端口不能为空';
    if (!l.contains(':')) {
      final p = int.tryParse(l);
      if (p == null || p < 1 || p > 65535) return '本地端口 "$l" 非法（1-65535）';
    }
    return null;
  }

  /// 校验目标主机：精确 IPv4/IPv6/主机名；禁 0.0.0.0/::、通配、网段写法。
  static String? validateHost(String input) {
    var h = input.trim();
    if (h.endsWith('.')) h = h.substring(0, h.length - 1);
    if (h.isEmpty) return '主机不能为空';
    if (h.length > 253) return '主机名过长（超过 253 字符）';
    if (isIPv4(h) || isIPv6(h)) {
      if (h == '0.0.0.0' || h == '::') return '不允许 0.0.0.0 / :: 等未指定地址';
      return null;
    }
    if (h.contains(':')) return '格式非法：含冒号但不是合法 IPv6 地址';
    if (h.contains(RegExp(r'[^0-9a-zA-Z._-]'))) {
      return '仅支持精确 IP 或主机名，不允许通配或网段写法';
    }
    for (final label in h.split('.')) {
      if (label.isEmpty || label.length > 63) return '主机名 "$input" 非法';
      final ok =
          RegExp(r'^[a-zA-Z0-9][a-zA-Z0-9_-]*$').hasMatch(label) &&
          RegExp(r'^[a-zA-Z0-9_]').hasMatch(label[label.length - 1]);
      if (!ok) return '主机名 "$input" 非法：标签须以字母数字开头与结尾';
    }
    return null;
  }

  static bool isIPv4(String s) {
    final parts = s.split('.');
    if (parts.length != 4) return false;
    for (final p in parts) {
      final n = int.tryParse(p);
      if (n == null || n < 0 || n > 255) return false;
      if (p.length > 1 && p.startsWith('0')) return false;
    }
    return true;
  }

  static bool isIPv6(String s) {
    if (!s.contains(':') || RegExp(r'[^0-9a-fA-F:.]').hasMatch(s)) return false;
    final groups = s.split(':');
    return groups.length >= 3 && groups.length <= 9;
  }

  /// 同一性比较（主机不区分大小写，本地地址纯端口补 127.0.0.1）。
  bool sameAs(MapRule b) =>
      remote == b.remote &&
      host.trim().toLowerCase() == b.host.trim().toLowerCase() &&
      _normLocal(local) == _normLocal(b.local);

  static String _normLocal(String local) {
    final l = local.trim();
    return l.contains(':') ? l : '127.0.0.1:$l';
  }
}

/// 本地入口地址归一化展示。
String localAddr(String local) {
  final l = local.trim();
  return l.contains(':') ? l : '127.0.0.1:$l';
}
