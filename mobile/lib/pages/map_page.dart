import 'dart:async';
import 'dart:convert';
import 'dart:ui';

import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../core/p2p_bridge.dart';
import '../core/theme.dart';
import '../models/map_rule.dart';

/// 映射规则页：从工作页迁移的完整映射列表功能（增删改、host 选填、持久化、
/// 运行中热生效、状态 Chip、空状态）。编辑器对话框复用登录页 LField 样式。
class MapPage extends StatefulWidget {
  const MapPage({super.key});

  @override
  State<MapPage> createState() => _MapPageState();
}

class _MapPageState extends State<MapPage> {
  /// PREVIEW=true 时用假数据渲染（仅 UI 截图迭代，不出现在 release 逻辑路径）。
  static const _preview = bool.fromEnvironment('PREVIEW');

  List<MapRule> _maps = [];
  Map<String, dynamic> _status = {};
  Timer? _statusTimer;

  bool get _running => _status['running'] == true;

  @override
  void initState() {
    super.initState();
    if (_preview) {
      _maps = [
        const MapRule(host: '192.168.5.20', remote: 80, local: '18080'),
        const MapRule(host: '', remote: 3306, local: '13306'),
        const MapRule(host: '192.168.5.21', remote: 445, local: '14445'),
      ];
      _status = {
        'running': true,
        'wsConnected': true,
        'maps': [
          {'remote': 80, 'local': '18080', 'listening': true},
          {'remote': 3306, 'local': '13306', 'listening': true},
          {'remote': 445, 'local': '14445', 'listening': false},
        ],
      };
    } else {
      _loadMaps();
      _refreshStatus();
      _statusTimer = Timer.periodic(
        const Duration(seconds: 2),
        (_) => _refreshStatus(),
      );
    }
  }

  @override
  void dispose() {
    _statusTimer?.cancel();
    super.dispose();
  }

  Future<void> _loadMaps() async {
    final p = await SharedPreferences.getInstance();
    final raw = p.getString('maps');
    if (!mounted) return;
    if (raw != null) {
      try {
        _maps = (jsonDecode(raw) as List)
            .map((e) => MapRule.fromMap(e as Map))
            .toList();
      } catch (_) {}
      setState(() {});
    }
  }

  Future<void> _saveMaps() async {
    final p = await SharedPreferences.getInstance();
    await p.setString('maps', jsonEncode(_maps.map((m) => m.toMap()).toList()));
  }

  Future<void> _refreshStatus() async {
    try {
      final st = await p2p.status();
      if (mounted) setState(() => _status = st);
    } catch (_) {}
  }

  void _toast(String s) => showToast(context, s, type: ToastType.error);

  Future<void> _addOrEdit([MapRule? old]) async {
    // iOS 风格：从底部弹出编辑面板（键盘弹出时贴合键盘上方）
    final rule = await showModalBottomSheet<MapRule>(
      context: context,
      isScrollControlled: true,
      builder: (_) => _MapEditSheet(initial: old),
    );
    if (rule == null || !mounted) return;
    if (_preview) {
      if (old != null) {
        _maps[_maps.indexWhere((m) => m.sameAs(old))] = rule;
      } else {
        _maps.add(rule);
      }
      setState(() {});
      return;
    }
    if (old != null) {
      // 修改 = 运行中热删旧 + 热增新；未运行则仅改持久化列表
      if (_running) {
        try {
          await p2p.removeMapping(old.host, old.remote, old.local);
        } catch (_) {}
        await p2p.addMapping(rule.host, rule.remote, rule.local);
      }
      _maps[_maps.indexWhere((m) => m.sameAs(old))] = rule;
    } else {
      if (_running) {
        try {
          await p2p.addMapping(rule.host, rule.remote, rule.local);
        } catch (e) {
          _toast('热生效失败：$e');
        }
      }
      if (!_maps.any((m) => m.sameAs(rule))) _maps.add(rule);
    }
    setState(() {});
    await _saveMaps();
    _refreshStatus();
  }

  Future<void> _delete(MapRule m) async {
    if (_preview) {
      setState(() => _maps.removeWhere((x) => x.sameAs(m)));
      return;
    }
    // 危险操作二次确认：删除会立刻关闭运行中的连接入口
    final ok = await confirmDanger(
      context,
      title: '删除映射规则',
      message: '${m.host.isEmpty ? "服务端本机" : m.host}:${m.remote} 的入口将被删除'
          '${_running ? "，正在使用的连接会立即断开" : ""}。',
      confirmLabel: '删除',
    );
    if (ok != true || !mounted) return;
    if (_running) {
      try {
        await p2p.removeMapping(m.host, m.remote, m.local);
      } catch (e) {
        _toast('热删除失败：$e');
      }
    }
    _maps.removeWhere((x) => x.sameAs(m));
    setState(() {});
    await _saveMaps();
    _refreshStatus();
  }

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    final mapsStatus = (_status['maps'] as List?) ?? const [];
    return Scaffold(
      appBar: AppBar(
        flexibleSpace: T.glassAppBar(dark: dark),
        // 默认返回按钮（无圆圈底衬）
        automaticallyImplyLeading: true,
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('映射规则'),
            Text(
              '已配置 ${_maps.length} 条 · 运行中修改即时生效',
              style: TextStyle(
                fontSize: 11,
                fontWeight: FontWeight.w400,
                color: dark ? T.darkInkSub : T.inkSub,
              ),
            ),
          ],
        ),
      ),
      floatingActionButton: GlassFab(label: '添加规则', onTap: () => _addOrEdit()),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(T.s16, T.s12, T.s16, 96),
        children: [
          if (_maps.isEmpty)
            StaggerIn(
              index: 0,
              child: EmptyState(
                icon: Icons.rule_rounded,
                title: '暂无映射规则',
                subtitle: '点击右下角「添加规则」创建第一条，\n本机访问 127.0.0.1:<本地端口> 即达远端服务。',
              ),
            )
          else
            ..._maps.indexed.map((e) {
              final (i, m) = e;
              final s = mapsStatus
                  .cast<Map>()
                  .where(
                    (e) =>
                        e['remote'] == m.remote &&
                        (e['local'] ?? '') == m.local,
                  )
                  .toList();
              final listening = s.isNotEmpty && s.first['listening'] == true;
              return StaggerIn(
                index: i,
                child: MapCard(
                  rule: m,
                  running: _running,
                  listening: listening,
                  onEdit: () => _addOrEdit(m),
                  onDelete: () => _delete(m),
                ),
              );
            }),
        ],
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// 映射卡片：圆角14，左侧远端端口徽标（大号等宽），右侧状态 Chip + 编辑/删除
// ---------------------------------------------------------------------------
class MapCard extends StatelessWidget {
  final MapRule rule;
  final bool running;
  final bool listening;
  final VoidCallback onEdit;
  final VoidCallback onDelete;

  const MapCard({
    super.key,
    required this.rule,
    required this.running,
    required this.listening,
    required this.onEdit,
    required this.onDelete,
  });

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    final subInk = dark ? T.darkInkSub : T.inkSub;
    // 未运行：灰；运行中监听：绿；运行中未监听：琥珀
    final chipColor = !running
        ? (dark ? const Color(0xFF64748B) : const Color(0xFF94A3B8))
        : (listening ? const Color(0xFF22C55E) : const Color(0xFFF59E0B));
    final chipText = !running ? '未生效' : (listening ? '监听中' : '待生效');

    return Card(
      margin: const EdgeInsets.only(bottom: T.s8),
      color: dark ? T.darkCard : Colors.white,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: T.s12, vertical: T.s12),
        child: Row(
          children: [
            // 远端端口徽标
            Container(
              width: 54,
              height: 46,
              decoration: BoxDecoration(
                color: dark ? Colors.white.withValues(alpha: .06) : T.brandTint,
                borderRadius: BorderRadius.circular(T.rField - 2),
              ),
              alignment: Alignment.center,
              child: Text(
                '${rule.remote}',
                style: TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.w800,
                  fontFamily: 'monospace',
                  color: dark ? T.darkBrand : T.brand,
                ),
              ),
            ),
            const SizedBox(width: T.s12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    rule.host.trim().isEmpty ? '服务端本机' : rule.host.trim(),
                    style: TextStyle(
                      fontSize: 14,
                      fontWeight: FontWeight.w600,
                      color: dark ? const Color(0xFFE2E8F0) : T.inkStrong,
                    ),
                    overflow: TextOverflow.ellipsis,
                  ),
                  const SizedBox(height: 2),
                  Text(
                    '本机入口 ${localAddr(rule.local)}',
                    style: TextStyle(
                      fontSize: 11,
                      fontFamily: 'monospace',
                      color: subInk,
                    ),
                    overflow: TextOverflow.ellipsis,
                  ),
                ],
              ),
            ),
            const SizedBox(width: T.s4),
            // 状态 Chip（切换 AnimatedSwitcher 淡入缩放）
            AnimatedSwitcher(
              duration: const Duration(milliseconds: 200),
              transitionBuilder: (child, anim) => FadeTransition(
                opacity: anim,
                child: ScaleTransition(
                  scale: Tween(begin: .8, end: 1.0).animate(anim),
                  child: child,
                ),
              ),
              child: Container(
                key: ValueKey(chipText),
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                decoration: BoxDecoration(
                  color: chipColor.withValues(alpha: .14),
                  borderRadius: BorderRadius.circular(999),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(
                      listening && running
                          ? Icons.check_circle
                          : Icons.schedule,
                      size: 11,
                      color: chipColor,
                    ),
                    const SizedBox(width: 3),
                    Text(
                      chipText,
                      style: TextStyle(
                        fontSize: 10.5,
                        fontWeight: FontWeight.w700,
                        color: chipColor,
                      ),
                    ),
                  ],
                ),
              ),
            ),
            const SizedBox(width: T.s4),
            IconButton(
              visualDensity: VisualDensity.compact,
              padding: EdgeInsets.zero,
              constraints: const BoxConstraints(minWidth: 36, minHeight: 36),
              onPressed: onEdit,
              icon: Icon(Icons.edit_outlined, size: 18, color: subInk),
            ),
            IconButton(
              visualDensity: VisualDensity.compact,
              padding: EdgeInsets.zero,
              constraints: const BoxConstraints(minWidth: 36, minHeight: 36),
              onPressed: onDelete,
              icon: Icon(Icons.delete_outline_rounded, size: 18, color: subInk),
            ),
          ],
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// 空状态
// ---------------------------------------------------------------------------
class EmptyState extends StatelessWidget {
  final IconData icon;
  final String title;
  final String subtitle;

  const EmptyState({
    super.key,
    required this.icon,
    required this.title,
    required this.subtitle,
  });

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    final ink = dark ? T.darkInkSub : T.inkSub;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(vertical: T.s20, horizontal: T.s16),
      decoration: BoxDecoration(
        color: dark
            ? T.darkCard.withValues(alpha: .5)
            : Colors.white.withValues(alpha: .6),
        borderRadius: BorderRadius.circular(T.rItem),
        border: Border.all(
          color: (dark ? Colors.white : T.inkStrong).withValues(alpha: .06),
        ),
      ),
      child: Column(
        children: [
          Container(
            width: 52,
            height: 52,
            decoration: BoxDecoration(
              color: dark ? Colors.white.withValues(alpha: .06) : T.brandTint,
              shape: BoxShape.circle,
            ),
            child: Icon(icon, size: 26, color: dark ? T.darkBrand : T.brand),
          ),
          const SizedBox(height: T.s12),
          Text(
            title,
            style: TextStyle(
              fontSize: 14,
              fontWeight: FontWeight.w600,
              color: dark ? const Color(0xFFE2E8F0) : T.inkStrong,
            ),
          ),
          const SizedBox(height: T.s4),
          Text(
            subtitle,
            textAlign: TextAlign.center,
            style: TextStyle(fontSize: 12, height: 1.5, color: ink),
          ),
        ],
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// 玻璃化 FAB：BackdropFilter + 白玻璃 + 品牌字
// ---------------------------------------------------------------------------
class GlassFab extends StatelessWidget {
  final String label;
  final VoidCallback onTap;

  const GlassFab({super.key, required this.label, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    return PressableScale(
      onTap: onTap,
      child: ClipRRect(
        borderRadius: BorderRadius.circular(18),
        child: BackdropFilter(
          filter: ImageFilter.blur(sigmaX: 30, sigmaY: 30),
          child: Container(
            height: 52,
            padding: const EdgeInsets.symmetric(horizontal: T.s20),
            decoration: BoxDecoration(
              color: dark
                  ? T.darkCard.withValues(alpha: .85)
                  : Colors.white.withValues(alpha: .85),
              borderRadius: BorderRadius.circular(18),
              border: Border.all(
                color: dark
                    ? Colors.white.withValues(alpha: .24)
                    : Colors.white.withValues(alpha: .35),
              ),
              gradient: dark
                  ? LinearGradient(
                      begin: Alignment.topCenter,
                      end: Alignment.bottomCenter,
                      colors: [
                        Colors.white.withValues(alpha: .14),
                        Colors.white.withValues(alpha: .04),
                      ],
                    )
                  : LinearGradient(
                      begin: Alignment.topCenter,
                      end: Alignment.bottomCenter,
                      colors: [
                        Colors.white.withValues(alpha: .20),
                        Colors.white.withValues(alpha: .08),
                      ],
                    ),
              boxShadow: const [
                BoxShadow(
                  color: Color(0x221667C8),
                  blurRadius: 18,
                  offset: Offset(0, 8),
                ),
              ],
            ),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(
                  Icons.add_rounded,
                  size: 20,
                  color: dark ? T.darkBrand : T.brand,
                ),
                const SizedBox(width: T.s4),
                Text(
                  label,
                  style: TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w700,
                    color: dark ? T.darkBrand : T.brand,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// 映射新增/编辑对话框
// ---------------------------------------------------------------------------
/// 底部弹出编辑面板（iOS 风格 sheet）：拖拽把手 + 圆角顶边 + 键盘贴合，
/// 字段每项独占一行，全宽渐变保存按钮。
class _MapEditSheet extends StatefulWidget {
  final MapRule? initial;
  const _MapEditSheet({this.initial});

  @override
  State<_MapEditSheet> createState() => _MapEditSheetState();
}

class _MapEditSheetState extends State<_MapEditSheet> {
  late final _host = TextEditingController(text: widget.initial?.host ?? '');
  late final _remote = TextEditingController(
    text: widget.initial == null ? '' : '${widget.initial!.remote}',
  );
  late final _local = TextEditingController(text: widget.initial?.local ?? '');
  String? _err;

  @override
  void dispose() {
    _host.dispose();
    _remote.dispose();
    _local.dispose();
    super.dispose();
  }

  void _ok() {
    final rule = MapRule(
      host: _host.text.trim(),
      remote: int.tryParse(_remote.text.trim()) ?? 0,
      local: _local.text.trim(),
    );
    final err = rule.validate();
    setState(() => _err = err);
    if (err == null) Navigator.of(context).pop(rule);
  }

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    return Padding(
      // 键盘弹出时面板贴在键盘上方
      padding: EdgeInsets.only(
        bottom: MediaQuery.of(context).viewInsets.bottom,
      ),
      child: Container(
        decoration: BoxDecoration(
          color: dark ? T.darkCard : Colors.white,
          borderRadius: const BorderRadius.vertical(top: Radius.circular(24)),
        ),
        padding: const EdgeInsets.fromLTRB(T.s20, 10, T.s20, 20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // 拖拽指示条
            Center(
              child: Container(
                width: 36,
                height: 4,
                decoration: BoxDecoration(
                  color: dark
                      ? Colors.white.withValues(alpha: .20)
                      : Colors.black.withValues(alpha: .12),
                  borderRadius: BorderRadius.circular(2),
                ),
              ),
            ),
            const SizedBox(height: 14),
            Text(
              widget.initial == null ? '添加映射规则' : '编辑映射规则',
              textAlign: TextAlign.center,
              style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: T.s16),
            LField(
              label: '目标主机（选填，空 = 服务端本机）',
              controller: _host,
              hint: '如 192.168.5.20',
            ),
            const SizedBox(height: T.s12),
            LField(
              label: '远端端口',
              controller: _remote,
              keyboardType: TextInputType.number,
              hint: '如 80',
            ),
            const SizedBox(height: T.s12),
            LField(
              label: '本地端口',
              controller: _local,
              keyboardType: TextInputType.number,
              hint: '如 18080',
            ),
            if (_err != null)
              Padding(
                padding: const EdgeInsets.only(top: T.s12),
                child: Text(
                  _err!,
                  style: TextStyle(
                    color: Theme.of(context).colorScheme.error,
                    fontSize: 12,
                  ),
                ),
              ),
            const SizedBox(height: 18),
            // 全宽渐变保存按钮
            DecoratedBox(
              decoration: BoxDecoration(
                gradient: T.brandGradient,
                borderRadius: BorderRadius.circular(T.rField),
              ),
              child: Material(
                color: Colors.transparent,
                child: InkWell(
                  onTap: _ok,
                  borderRadius: BorderRadius.circular(T.rField),
                  child: const SizedBox(
                    height: 46,
                    child: Center(
                      child: Text(
                        '保存',
                        style: TextStyle(
                          fontSize: 15,
                          fontWeight: FontWeight.w700,
                          color: Colors.white,
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ),
            const SizedBox(height: T.s4),
            TextButton(
              onPressed: () => Navigator.of(context).pop(),
              child: const Text('取消'),
            ),
          ],
        ),
      ),
    );
  }
}
