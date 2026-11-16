import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../core/p2p_bridge.dart' show LogEntry, p2p, sanitizeServerText;
import '../core/theme.dart';
import '../core/update_service.dart';
import '../models/map_rule.dart';
import 'map_page.dart';
import 'login_page.dart';
import 'settings_page.dart';

/// 客户端工作页：品牌渐变状态卡 + 映射规则入口 + 暗色日志块。
/// 映射规则与设置已拆分为独立页面（AppBar 右上入口）。
class WorkPage extends StatefulWidget {
  final Map<String, dynamic> session;

  const WorkPage({super.key, required this.session});

  @override
  State<WorkPage> createState() => _WorkPageState();
}

class _WorkPageState extends State<WorkPage> {
  /// PREVIEW=true 时用假数据渲染（仅 UI 截图迭代，不出现在 release 逻辑路径）。
  static const _preview = bool.fromEnvironment('PREVIEW');

  final _logs = <LogEntry>[];
  final _logBuffer = <LogEntry>[];
  Timer? _logFlush;
  Timer? _statusTimer;
  StreamSubscription<LogEntry>? _logSub;

  List<MapRule> _maps = [];
  Map<String, dynamic> _status = {};
  bool _busy = false;
  final _scroll = ScrollController();

  /// 底部选项卡：0=工作台，1=设置
  int _tab = 0;

  String get _token => (widget.session['deviceToken'] ?? '') as String;
  String get _platformWS => (widget.session['platformWs'] ?? '') as String;
  String get _uid => ((widget.session['deviceUid'] ?? '') as String);

  bool get _running => _status['running'] == true;
  bool get _wsOK => _status['wsConnected'] == true;

  @override
  void initState() {
    super.initState();
    if (_preview) {
      _seedPreview();
    } else {
      _loadMaps();
      _logSub = p2p.logs().listen(_onLog);
      _refreshStatus();
      _statusTimer = Timer.periodic(
        const Duration(seconds: 2),
        (_) => _refreshStatus(),
      );
      _autoUpdateCheck();
    }
  }

  /// 启动静默检查更新（用户开启「自动检查更新」时）：
  /// 发现新版本仅弹提示条引导去设置页，绝不阻塞、绝不报错。
  Future<void> _autoUpdateCheck() async {
    try {
      await Future.delayed(const Duration(seconds: 3));
      final p = await SharedPreferences.getInstance();
      if (p.getBool(kAutoUpdateKey) == false) return;
      final u = await UpdateService.checkLatest(
        platformBaseFromSession(widget.session),
      );
      if (u == null || !mounted) return;
      final dark = Theme.of(context).brightness == Brightness.dark;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          duration: const Duration(seconds: 6),
          content: Row(
            children: [
              Icon(
                Icons.system_update_rounded,
                size: 18,
                color: dark ? const Color(0xFF7EB3F7) : T.brand,
              ),
              const SizedBox(width: 8),
              Expanded(child: Text('发现新版本 v${u.version}，建议尽快更新')),
            ],
          ),
          action: SnackBarAction(label: '去更新', onPressed: _goSettings),
        ),
      );
    } catch (_) {}
  }

  void _seedPreview() {
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
    void log(String level, String text) =>
        _logs.add(LogEntry(level: level, text: text));
    log('info', '已连接到服务器');
    log('info', '已建立直连，线路质量良好');
    log('info', '已开通：本机端口 18080 ↔ 服务器 80 端口');
    log('info', '已开通：本机端口 13306 ↔ 服务器 3306 端口');
    log('warn', '本机端口 14445 暂时无法直连，已切换到中转线路');
    log('info', '已建立中转连接');
    log('error', '与服务器的连接中断，正在自动恢复…');
  }

  @override
  void dispose() {
    _statusTimer?.cancel();
    _logSub?.cancel();
    _logFlush?.cancel();
    _scroll.dispose();
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

  /// 平台返回 401（设备令牌无效/已过期）：提示并跳转登录页（仅触发一次）
  static final _authFailRe = RegExp(r'\b401\b|令牌无效|令牌已失效|令牌已过期');
  bool _authFailHandled = false;

  void _onLog(LogEntry e) {
    if (!_preview && !_authFailHandled && _authFailRe.hasMatch(e.text)) {
      _authFailHandled = true;
      _handleAuthFail();
    }
    _logBuffer.add(e);
    // 150ms 节流批量渲染，避免日志风暴冲爆 UI
    _logFlush ??= Timer(const Duration(milliseconds: 150), () {
      _logFlush = null;
      if (!mounted) return;
      setState(() {
        _logs.addAll(List.of(_logBuffer));
        _logBuffer.clear();
        if (_logs.length > 500) {
          _logs.removeRange(0, _logs.length - 500);
        }
      });
      if (_scroll.hasClients) {
        _scroll.jumpTo(_scroll.position.maxScrollExtent);
      }
    });
  }

  /// 2s 轮询状态：仅在实际变化时 setState，避免空转重建拖累帧率。
  Future<void> _refreshStatus() async {
    try {
      final st = await p2p.status();
      if (!mounted || _statusEquals(st, _status)) return;
      setState(() => _status = st);
    } catch (_) {}
  }

  bool _statusEquals(Map<String, dynamic> a, Map<String, dynamic> b) {
    final am = (a['maps'] as List?) ?? const [];
    final bm = (b['maps'] as List?) ?? const [];
    return a['running'] == b['running'] &&
        a['wsConnected'] == b['wsConnected'] &&
        am.length == bm.length;
  }

  Future<void> _start() async {
    if (_preview) {
      setState(() {
        _status = {..._status, 'running': true, 'wsConnected': true};
      });
      _logs.add(LogEntry(level: 'info', text: '已连接到服务器'));
      return;
    }
    setState(() => _busy = true);
    try {
      if (_maps.isEmpty) {
        _toast('请先在「映射规则」中添加至少一条规则');
        return;
      }
      await p2p.setForeground(true, '连接运行中');
      await p2p.startClient(
        deviceToken: _token,
        platformWS: _platformWS,
        maps: _maps,
      );
    } catch (e) {
      _toast('启动失败：$e', type: ToastType.error);
      await p2p.setForeground(false, '');
    } finally {
      if (mounted) setState(() => _busy = false);
      _refreshStatus();
    }
  }

  Future<void> _stop() async {
    if (_preview) {
      setState(() {
        _status = {..._status, 'running': false, 'wsConnected': false};
      });
      _logs.add(LogEntry(level: 'info', text: '已断开连接'));
      return;
    }
    setState(() => _busy = true);
    try {
      await p2p.stop();
      await p2p.setForeground(false, '');
    } catch (_) {
    } finally {
      if (mounted) setState(() => _busy = false);
      _refreshStatus();
    }
  }

  /// 打开映射规则页，返回后刷新本地规则副本（供启动时使用）。
  Future<void> _openMaps() async {
    await Navigator.of(context).push(FadeSlideRoute(page: const MapPage()));
    if (mounted) _loadMaps();
  }

  /// 切到「设置」选项卡（检查更新等入口用）。
  void _goSettings() {
    if (mounted) setState(() => _tab = 1);
  }

  void _toast(String s, {ToastType type = ToastType.info}) =>
      showToast(context, s, type: type);

  /// 登录失效统一处理：停止连接 → 清除会话 → 回登录页
  Future<void> _handleAuthFail() async {
    try {
      await p2p.stop();
      await p2p.setForeground(false, '');
    } catch (_) {}
    final p = await SharedPreferences.getInstance();
    await p.remove('session');
    if (!mounted) return;
    showToast(context, '登录已失效，请重新登录', type: ToastType.error);
    Navigator.of(context)
        .pushReplacement(MaterialPageRoute(builder: (_) => const LoginPage()));
  }

  /// 系统返回键：不退出应用，退到后台保持隧道存活，并给出提示。
  Future<void> _backToBackground() async {
    _toast('应用已进入后台运行，连接保持中', type: ToastType.success);
    // 给提示条留出展示时间再退后台
    await Future<void>.delayed(const Duration(milliseconds: 600));
    if (!_preview) {
      try {
        await p2p.moveToBack();
      } catch (_) {}
    }
  }

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    // 状态栏随主题：浅色深图标 / 暗黑白图标，透明底
    return AnnotatedRegion<SystemUiOverlayStyle>(
      value: (dark ? SystemUiOverlayStyle.light : SystemUiOverlayStyle.dark)
          .copyWith(
            statusBarColor: Colors.transparent,
            systemNavigationBarColor: dark ? T.darkBg : T.lightBg,
          ),
      child: PopScope(
        canPop: false,
        onPopInvokedWithResult: (didPop, _) {
          if (didPop) return;
          _backToBackground();
        },
        child: Scaffold(
          // 设置页自带 AppBar；工作台 AppBar 仅在第一个选项卡显示
          appBar: _tab == 0
              ? AppBar(
                  flexibleSpace: T.glassAppBar(dark: dark),
                  title: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text('励联 LiLink'),
                      Text(
                        _uid.isEmpty
                            ? '客户端模式'
                            : '客户端 · 设备 ${_uid.length > 14 ? _uid.substring(0, 14) : _uid}',
                        style: TextStyle(
                          fontSize: 11,
                          fontWeight: FontWeight.w400,
                          color: dark ? T.darkInkSub : T.inkSub,
                        ),
                      ),
                    ],
                  ),
                  actions: [
                    IconButton(
                      tooltip: '映射规则',
                      onPressed: _openMaps,
                      icon: const Icon(Icons.rule_rounded),
                    ),
                    const SizedBox(width: T.s4),
                  ],
                )
              : null,
          body: IndexedStack(
            index: _tab,
            children: [
              // ---- 选项卡 1：工作台 ----
              // 上部固定内容 + 日志块 Expanded 自适应：占满底部选项卡上方全部空间
              Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Padding(
                    padding: const EdgeInsets.fromLTRB(T.s16, T.s16, T.s16, 0),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        // ---- 状态玻璃卡 ----
                        StaggerIn(
                          index: 0,
                          child: RepaintBoundary(
                            child: _StatusCard(
                              running: _running,
                              wsOK: _wsOK,
                              busy: _busy,
                              host: _platformWS.isEmpty
                                  ? '-'
                                  : sanitizeServerText(
                                      Uri.parse(_platformWS).host,
                                    ),
                              uid: _uid,
                              onToggle: _running ? _stop : _start,
                            ),
                          ),
                        ),
                        const SizedBox(height: T.s20),
                        // ---- 映射规则入口（功能在独立页面） ----
                        StaggerIn(
                          index: 1,
                          child: _MapsEntryRow(
                            dark: dark,
                            count: _maps.length,
                            running: _running,
                            onTap: _openMaps,
                          ),
                        ),
                        const SizedBox(height: T.s20),
                        // ---- 运行日志 ----
                        StaggerIn(
                          index: 2,
                          child: _SectionHeader(
                            title: '运行日志',
                            count: _logs.length,
                            live: _running,
                          ),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(height: T.s12),
                  Expanded(
                    child: Padding(
                      padding: const EdgeInsets.symmetric(horizontal: T.s16),
                      child: StaggerIn(
                        index: 3,
                        child: RepaintBoundary(
                          child: _LogBlock(
                            logs: _logs,
                            scrollController: _scroll,
                            dark: dark,
                          ),
                        ),
                      ),
                    ),
                  ),
                  // 悬浮胶囊选项卡上方留白（选项卡 74px 高 + 12px 呼吸间距）
                  const SizedBox(height: 96),
                ],
              ),
              // ---- 选项卡 2：设置（复用独立页，含检查更新/帮助/外观） ----
              SettingsPage(session: widget.session),
            ],
          ),
          // ---- iOS 风格悬浮胶囊玻璃选项卡：工作台 / 设置 ----
          bottomNavigationBar: _GlassTabBar(
            index: _tab,
            onChanged: (i) => setState(() => _tab = i),
          ),
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// 映射规则入口行：规则数 + 状态摘要，点击进入映射规则页
// ---------------------------------------------------------------------------
class _MapsEntryRow extends StatelessWidget {
  final bool dark;
  final int count;
  final bool running;
  final VoidCallback onTap;

  const _MapsEntryRow({
    required this.dark,
    required this.count,
    required this.running,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final accent = dark ? T.darkBrand : T.brand;
    return PressableScale(
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: T.s16, vertical: 14),
        decoration: BoxDecoration(
          color: dark ? T.darkCard : Colors.white,
          borderRadius: BorderRadius.circular(T.rItem),
          border: Border.all(
            color: (dark ? Colors.white : T.inkStrong).withValues(alpha: .07),
          ),
        ),
        child: Row(
          children: [
            Icon(Icons.rule_rounded, size: 22, color: accent),
            const SizedBox(width: T.s12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '映射规则',
                    style: TextStyle(
                      fontSize: 15,
                      fontWeight: FontWeight.w600,
                      color: dark ? const Color(0xFFE2E8F0) : T.inkStrong,
                    ),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    count == 0
                        ? '尚未配置，点击添加第一条'
                        : '已配置 $count 条${running ? ' · 修改即时生效' : ''}',
                    style: TextStyle(
                      fontSize: 12,
                      color: dark ? T.darkInkSub : T.inkSub,
                    ),
                  ),
                ],
              ),
            ),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
              decoration: BoxDecoration(
                color: dark ? Colors.white.withValues(alpha: .08) : T.brandTint,
                borderRadius: BorderRadius.circular(999),
              ),
              child: Text(
                '$count',
                style: TextStyle(
                  fontSize: 11,
                  fontWeight: FontWeight.w700,
                  color: accent,
                ),
              ),
            ),
            const SizedBox(width: T.s4),
            Icon(
              Icons.chevron_right_rounded,
              color: dark ? T.darkInkSub : T.inkSub,
            ),
          ],
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// 状态玻璃卡：品牌渐变 + BackdropFilter(blur20)+白.12，白底按钮品牌蓝字
// ---------------------------------------------------------------------------
class _StatusCard extends StatelessWidget {
  final bool running;
  final bool wsOK;
  final bool busy;
  final String host;
  final String uid;
  final VoidCallback onToggle;

  const _StatusCard({
    required this.running,
    required this.wsOK,
    required this.busy,
    required this.host,
    required this.uid,
    required this.onToggle,
  });

  @override
  Widget build(BuildContext context) {
    final stateText = !running ? '未运行' : (wsOK ? '已连接平台' : '连接中…');
    return GlassCard(
      radius: T.rCard,
      gradient: T.brandGradient,
      highlight: true,
      border: Border.all(color: Colors.white.withValues(alpha: .18)),
      padding: const EdgeInsets.all(T.s20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 40,
                height: 40,
                decoration: BoxDecoration(
                  color: Colors.white.withValues(alpha: .14),
                  borderRadius: BorderRadius.circular(T.rField),
                ),
                child: Icon(
                  running
                      ? Icons.sync_alt_rounded
                      : Icons.power_settings_new_rounded,
                  color: Colors.white,
                  size: 22,
                ),
              ),
              const SizedBox(width: T.s12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      stateText,
                      style: const TextStyle(
                        fontSize: 18,
                        fontWeight: FontWeight.w700,
                        color: Colors.white,
                      ),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      running ? '平台 $host' : '启动后自动连接平台并建立隧道',
                      style: TextStyle(
                        fontSize: 12,
                        color: Colors.white.withValues(alpha: .75),
                      ),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ],
                ),
              ),
              if (running)
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 10,
                    vertical: 5,
                  ),
                  decoration: BoxDecoration(
                    color: Colors.white.withValues(alpha: .14),
                    borderRadius: BorderRadius.circular(999),
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(
                        wsOK ? Icons.check_circle : Icons.autorenew,
                        size: 12,
                        color: wsOK ? const Color(0xFF86EFAC) : Colors.white,
                      ),
                      const SizedBox(width: T.s4),
                      Text(
                        wsOK ? '已就绪' : '连接中',
                        style: const TextStyle(
                          fontSize: 11,
                          fontWeight: FontWeight.w600,
                          color: Colors.white,
                        ),
                      ),
                    ],
                  ),
                ),
            ],
          ),
          const SizedBox(height: T.s16),
          Row(
            children: [
              PressableScale(
                onTap: busy ? null : onToggle,
                child: ElevatedButton.icon(
                  onPressed: busy ? null : onToggle,
                  style: ElevatedButton.styleFrom(
                    fixedSize: const Size(132, 44),
                  ),
                  icon: Icon(
                    running ? Icons.stop_rounded : Icons.play_arrow_rounded,
                    size: 20,
                  ),
                  label: Text(running ? '停止连接' : '启动连接'),
                ),
              ),
              const Spacer(),
              Text(
                'AES-GCM · smux · P2P直连',
                style: TextStyle(
                  fontSize: 11,
                  color: Colors.white.withValues(alpha: .6),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// iOS 风格悬浮胶囊玻璃选项卡：blur 24 磨砂 + 半透明底 + 1px 高光描边 + 悬浮投影；
// 选中项品牌蓝胶囊指示器 + elasticOut 弹性缩放（Q 弹）
// ---------------------------------------------------------------------------
class _GlassTabBar extends StatefulWidget {
  final int index;
  final ValueChanged<int> onChanged;

  const _GlassTabBar({required this.index, required this.onChanged});

  @override
  State<_GlassTabBar> createState() => _GlassTabBarState();
}

class _GlassTabBarState extends State<_GlassTabBar>
    with SingleTickerProviderStateMixin {
  static const _items = [
    (Icons.home_outlined, Icons.home_rounded, '工作台'),
    (Icons.settings_outlined, Icons.settings_rounded, '设置'),
  ];

  late final AnimationController _bounce = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 560),
  )..forward();

  /// 正在播放弹性动画的选项卡序号：只有新选中的项回弹，
  /// 未选中项不参与动画（否则切换时旁边项会闪一下）。
  int _animIndex = 0;

  @override
  void didUpdateWidget(_GlassTabBar old) {
    super.didUpdateWidget(old);
    // 切换选项卡时仅对新选中项重新播放弹性动画
    if (old.index != widget.index) {
      _animIndex = widget.index;
      _bounce.forward(from: 0);
    }
  }

  @override
  void dispose() {
    _bounce.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    // 居中定宽短胶囊：2×110 + 内边距 ≈ 232px，短巧悬浮而非通栏长条
    return SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.only(top: 6, bottom: 14),
        // 槽位宽度是紧约束：Align 放宽宽度约束让胶囊居中（232px），
        // heightFactor:1 让槽位高度只占胶囊自身，不把 body 挤成 0
        child: Align(
          alignment: Alignment.bottomCenter,
          heightFactor: 1,
          child: GestureDetector(
            // 吞掉横滑手势：胶囊固定居中，不可左右拖动
            onHorizontalDragStart: (_) {},
            onHorizontalDragUpdate: (_) {},
            onHorizontalDragEnd: (_) {},
            child: SizedBox(
              // 显式定宽定高：BackdropFilter 在松约束下会撑满可用空间
              width: 232,
              height: 60,
              child: DecoratedBox(
                // iOS 27 参考材质：极轻投影，胶囊靠「比背景更亮」呈现层次
                decoration: BoxDecoration(
                  borderRadius: BorderRadius.circular(999),
                  boxShadow: [
                    BoxShadow(
                      color: Colors.black.withValues(alpha: dark ? .20 : .08),
                      blurRadius: 24,
                      offset: const Offset(0, 6),
                    ),
                  ],
                ),
                child: ClipRRect(
                  borderRadius: BorderRadius.circular(999),
                  child: BackdropFilter(
                    // Apple 透光玻璃：blur 60 + 饱和度提升
                    filter: T.frostFilter(blur: 60),
                    child: Stack(
                      children: [
                        // 亮玻璃基底：比灰背景更亮的近白色，无灰底
                        Container(
                          color: Colors.white.withValues(
                            alpha: dark ? .14 : .74,
                          ),
                        ),
                        // 极淡高光：左上一点反光即可（参考图几乎无光斑）
                        IgnorePointer(
                          child: Container(
                            decoration: BoxDecoration(
                              gradient: LinearGradient(
                                begin: Alignment.topLeft,
                                end: Alignment.bottomRight,
                                stops: const [0, .5, 1],
                                colors: [
                                  Colors.white.withValues(
                                    alpha: dark ? .06 : .22,
                                  ),
                                  Colors.white.withValues(
                                    alpha: dark ? .01 : .02,
                                  ),
                                  Colors.white.withValues(
                                    alpha: dark ? .04 : .10,
                                  ),
                                ],
                              ),
                            ),
                          ),
                        ),
                        Container(
                          padding: const EdgeInsets.all(6),
                          decoration: BoxDecoration(
                            borderRadius: BorderRadius.circular(999),
                            // 参考图几乎无边框：仅留极淡边缘过渡
                            border: Border.all(
                              color: Colors.white.withValues(
                                alpha: dark ? .14 : .60,
                              ),
                            ),
                          ),
                          child: Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              for (var i = 0; i < _items.length; i++)
                                _buildItem(i, dark),
                            ],
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
            ),
          ),
        ),
      ),
      ),
    );
  }

  Widget _buildItem(int i, bool dark) {
    final selected = widget.index == i;
    final accent = dark ? T.darkBrand : T.brand;
    final (idleIcon, activeIcon, label) = _items[i];
    final icon = AnimatedSwitcher(
      duration: const Duration(milliseconds: 220),
      child: Icon(
        selected ? activeIcon : idleIcon,
        key: ValueKey(selected),
        size: 23,
        color: selected ? accent : (dark ? T.darkInkSub : T.inkSub),
      ),
    );
    final text = AnimatedDefaultTextStyle(
      duration: const Duration(milliseconds: 220),
      style: TextStyle(
        fontSize: 11.5,
        fontWeight: selected ? FontWeight.w700 : FontWeight.w500,
        color: selected ? accent : (dark ? T.darkInkSub : T.inkSub),
      ),
      child: Text(label),
    );
    return GestureDetector(
      behavior: HitTestBehavior.opaque,
      onTap: selected ? null : () => widget.onChanged(i),
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 300),
        curve: Curves.easeOutCubic,
        width: 110,
        height: 48,
        decoration: BoxDecoration(
          // 参考图选中态：浅灰副胶囊（iOS secondarySystemFill），图标文字为蓝色
          color: selected
              ? (dark
                  ? Colors.white.withValues(alpha: .14)
                  : Colors.black.withValues(alpha: .055))
              : Colors.transparent,
          borderRadius: BorderRadius.circular(999),
        ),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            // 仅新选中项做弹性缩放（elasticOut 回弹），旁边项保持静止不闪
            if (selected && i == _animIndex)
              ScaleTransition(
                scale: CurvedAnimation(
                  parent: _bounce,
                  curve: Curves.elasticOut,
                ).drive(Tween(begin: 0.72, end: 1.0)),
                child: icon,
              )
            else
              icon,
            const SizedBox(height: 1),
            text,
          ],
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// 区块标题
// ---------------------------------------------------------------------------
class _SectionHeader extends StatelessWidget {
  final String title;
  final int count;
  final bool live;

  const _SectionHeader({
    required this.title,
    required this.count,
    this.live = false,
  });

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    return Row(
      children: [
        Text(
          title,
          style: TextStyle(
            fontSize: 15,
            fontWeight: FontWeight.w700,
            color: dark ? const Color(0xFFE2E8F0) : T.inkStrong,
          ),
        ),
        const SizedBox(width: T.s8),
        Container(
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
          decoration: BoxDecoration(
            color: dark ? Colors.white.withValues(alpha: .08) : T.brandTint,
            borderRadius: BorderRadius.circular(999),
          ),
          child: Text(
            '$count',
            style: TextStyle(
              fontSize: 11,
              fontWeight: FontWeight.w700,
              color: dark ? T.darkBrand : T.brand,
            ),
          ),
        ),
        if (live) ...[
          const SizedBox(width: T.s8),
          Icon(
            Icons.fiber_manual_record,
            size: 8,
            color: const Color(0xFF4ADE80),
          ),
        ],
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// 暗色代码块日志：#0F172A 底、等宽 11、圆角 14
// ---------------------------------------------------------------------------
class _LogBlock extends StatelessWidget {
  final List<LogEntry> logs;
  final ScrollController scrollController;
  final bool dark;

  const _LogBlock({
    required this.logs,
    required this.scrollController,
    required this.dark,
  });

  Color _levelColor(String level) {
    switch (level) {
      case 'error':
        return const Color(0xFFF87171);
      case 'warn':
        return const Color(0xFFFBBF24);
      default:
        return const Color(0xFF94A3B8);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      // 高度由外层 Expanded 决定：自适应填满底部选项卡上方空间
      decoration: BoxDecoration(
        color: dark ? const Color(0xFF0B1220) : const Color(0xFF0F172A),
        borderRadius: BorderRadius.circular(T.rItem),
      ),
      padding: const EdgeInsets.all(T.s12),
      child: logs.isEmpty
          ? Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Icon(
                  Icons.terminal_rounded,
                  size: 30,
                  color: const Color(0xFF475569),
                ),
                const SizedBox(height: T.s8),
                const Text(
                  '暂无日志',
                  style: TextStyle(fontSize: 12, color: Color(0xFF64748B)),
                ),
              ],
            )
          : ListView.builder(
              controller: scrollController,
              itemCount: logs.length,
              itemBuilder: (_, i) {
                final e = logs[i];
                final c = _levelColor(e.level);
                final time = TimeOfDay.fromDateTime(e.time).format(context);
                // 新行淡入（每行首次构建 200ms 淡入）
                return TweenAnimationBuilder<double>(
                  tween: Tween(begin: 0, end: 1),
                  duration: const Duration(milliseconds: 200),
                  curve: Curves.easeOut,
                  builder: (context, t, child) =>
                      Opacity(opacity: t, child: child),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 1.5),
                    child: Text.rich(
                      TextSpan(
                        style: TextStyle(
                          fontSize: 11,
                          fontFamily: 'monospace',
                          height: 1.4,
                          color: c,
                        ),
                        children: [
                          TextSpan(
                            text: '$time ',
                            style: const TextStyle(color: Color(0xFF475569)),
                          ),
                          TextSpan(text: e.text),
                        ],
                      ),
                    ),
                  ),
                );
              },
            ),
    );
  }
}
