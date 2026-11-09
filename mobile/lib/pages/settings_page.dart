import 'dart:io';

import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../core/p2p_bridge.dart';
import '../core/theme.dart';
import '../core/theme_controller.dart';
import '../core/update_service.dart';
import 'help_page.dart';
import 'login_page.dart';

/// 设置页：iOS 风格分组卡（外观 / 关于 / 检查更新 / 帮助中心）。
/// 更新功能优雅降级：接口未上线（404）、网络失败、结构不符一律提示「已是最新」。
class SettingsPage extends StatefulWidget {
  final Map<String, dynamic> session;

  const SettingsPage({super.key, required this.session});

  @override
  State<SettingsPage> createState() => _SettingsPageState();
}

class _SettingsPageState extends State<SettingsPage> {
  bool _checking = false;
  bool _auto = true;

  // 更新检查的平台地址（脱敏展示，不暴露具体服务器）
  String get _updateBase => platformBaseFromSession(widget.session);

  @override
  void initState() {
    super.initState();
    SharedPreferences.getInstance().then((p) {
      if (!mounted) return;
      setState(() => _auto = p.getBool(kAutoUpdateKey) != false);
    });
  }

  void _toast(String s, {ToastType type = ToastType.error}) =>
      showToast(context, s, type: type);

  /// 退出登录：二次确认，停止隧道并清空会话，回到登录页。
  Future<void> _logout() async {
    final ok = await confirmDanger(
      context,
      title: '退出登录',
      message: '退出后正在运行的映射连接将被停止，确定要退出吗？',
      confirmLabel: '退出登录',
    );
    if (ok != true || !mounted) return;
    try {
      await p2p.stop();
      await p2p.setForeground(false, '');
    } catch (_) {}
    final p = await SharedPreferences.getInstance();
    await p.remove('session');
    if (!mounted) return;
    Navigator.of(context)
        .pushReplacement(MaterialPageRoute(builder: (_) => const LoginPage()));
  }

  Future<void> _setAuto(bool v) async {
    setState(() => _auto = v);
    final p = await SharedPreferences.getInstance();
    await p.setBool(kAutoUpdateKey, v);
  }

  // -----------------------------------------------------------------------
  // 检查更新 → 底部面板 → 下载（进度条） → 自动拉起安装器
  // -----------------------------------------------------------------------
  Future<void> _checkUpdate() async {
    if (_checking) return;
    setState(() => _checking = true);
    try {
      final u = await UpdateService.checkLatest(_updateBase);
      if (!mounted) return;
      if (u == null) {
        // 无更新 / 接口未上线 / 网络失败：统一优雅提示
        _toast('当前已是最新版本', type: ToastType.success);
        return;
      }
      await _showUpdateSheet(u);
    } finally {
      if (mounted) setState(() => _checking = false);
    }
  }

  Future<void> _showUpdateSheet(AppUpdate u) async {
    final progress = ValueNotifier<double?>(null); // null=未开始下载
    await showModalBottomSheet<void>(
      context: context,
      // 下载期间不可下拉/点外部关闭，进度不会中途消失；未开始下载时由内容区按钮/返回关闭
      isDismissible: false,
      enableDrag: false,
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      builder: (ctx) {
        final dark = Theme.of(ctx).brightness == Brightness.dark;
        return PopScope(
          // 下载中禁止返回键关闭（防止后台静默下载、进度丢失）；
          // 未开始下载时放行系统返回 = 关闭卡片。
          canPop: false,
          onPopInvokedWithResult: (didPop, _) {
            if (didPop) return;
            if (progress.value == null) Navigator.of(ctx).pop();
          },
          child: SafeArea(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(T.s20, T.s20, T.s20, T.s20),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                // 把手
                Center(
                  child: Container(
                    width: 36,
                    height: 4,
                    decoration: BoxDecoration(
                      color: (dark ? Colors.white : T.inkStrong).withValues(
                        alpha: .2,
                      ),
                      borderRadius: BorderRadius.circular(999),
                    ),
                  ),
                ),
                const SizedBox(height: T.s16),
                Row(
                  children: [
                    Container(
                      width: 42,
                      height: 42,
                      decoration: BoxDecoration(
                        gradient: T.brandGradient,
                        borderRadius: BorderRadius.circular(T.rField),
                      ),
                      child: const Icon(
                        Icons.system_update_alt_rounded,
                        color: Colors.white,
                        size: 22,
                      ),
                    ),
                    const SizedBox(width: T.s12),
                    Expanded(
                      child: Text(
                        '发现新版本 v${u.version}',
                        style: TextStyle(
                          fontSize: 17,
                          fontWeight: FontWeight.w700,
                          color: dark ? const Color(0xFFE2E8F0) : T.inkStrong,
                        ),
                      ),
                    ),
                    Text(
                      '当前 v$kAppVersion',
                      style: TextStyle(
                        fontSize: 11,
                        color: dark ? T.darkInkSub : T.inkSub,
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: T.s14),
                // 更新说明（多行，可滚动）
                ConstrainedBox(
                  constraints: const BoxConstraints(maxHeight: 180),
                  child: SingleChildScrollView(
                    child: Text(
                      u.notes.isEmpty ? '本次更新带来体验优化与问题修复。' : u.notes,
                      style: TextStyle(
                        fontSize: 13,
                        height: 1.6,
                        color: dark ? T.darkInkSub : T.inkSub,
                      ),
                    ),
                  ),
                ),
                const SizedBox(height: T.s18),
                // 下载进度 / 更新按钮
                ValueListenableBuilder<double?>(
                  valueListenable: progress,
                  builder: (context, p, _) {
                    if (p == null) {
                      return Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          PressableScale(
                            onTap: () async {
                              await _downloadAndInstall(u, progress, ctx);
                            },
                            child: const _GradientAction(label: '立即更新'),
                          ),
                          if (!u.force) ...[
                            const SizedBox(height: T.s8),
                            TextButton(
                              onPressed: () => Navigator.of(ctx).pop(),
                              child: const Text('稍后再说'),
                            ),
                          ],
                        ],
                      );
                    }
                    return Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        ClipRRect(
                          borderRadius: BorderRadius.circular(999),
                          child: LinearProgressIndicator(
                            value: p <= 0 ? null : p,
                            minHeight: 6,
                          ),
                        ),
                        const SizedBox(height: T.s8),
                        Text(
                          p >= 1 ? '下载完成，正在打开安装程序…' : '正在下载 ${_pct(p)}',
                          style: TextStyle(
                            fontSize: 12,
                            color: dark ? T.darkInkSub : T.inkSub,
                          ),
                        ),
                      ],
                    );
                  },
                ),
              ],
            ),
          ),
          ),
        );
      },
    );
  }

  static String _pct(double p) => '${(p * 100).toStringAsFixed(0)}%';

  Future<void> _downloadAndInstall(
    AppUpdate u,
    ValueNotifier<double?> progress,
    BuildContext sheetCtx,
  ) async {
    try {
      progress.value = 0.0;
      final dir = await AppInstaller.filesDir();
      if (dir.isEmpty) throw Exception('无法获取存储目录');
      final sep = Platform.pathSeparator;
      final path =
          '${dir.endsWith(sep) ? dir : '$dir$sep'}LiLink_${u.version}.apk';
      // 先下载到 .part 临时文件，哈希校验通过后才改名落地，
      // 避免中断/被篡改的半截文件被当作有效安装包拉起安装器。
      final partPath = '$path.part';
      final part = File(partPath);
      if (await part.exists()) await part.delete();
      await for (final p in UpdateService.download(
        u.url,
        partPath,
        expectedSha256: u.sha256,
      )) {
        progress.value = p;
      }
      final target = File(path);
      if (await target.exists()) await target.delete();
      await part.rename(path);
      await AppInstaller.install(path);
      if (sheetCtx.mounted) Navigator.of(sheetCtx).pop();
    } catch (e) {
      progress.value = null;
      if (sheetCtx.mounted) {
        ScaffoldMessenger.of(sheetCtx)
            .showSnackBar(
              SnackBar(
                duration: const Duration(seconds: 4),
                content: Row(
                  children: [
                    Icon(
                      Icons.error_outline_rounded,
                      size: 18,
                      color: Theme.of(sheetCtx).colorScheme.error,
                    ),
                    const SizedBox(width: 8),
                    Expanded(child: Text('下载或安装失败：$e')),
                  ],
                ),
              ),
            );
      }
    }
  }

  // -----------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    return Scaffold(
      appBar: AppBar(
        flexibleSpace: T.glassAppBar(dark: dark),
        // 选项卡页面：无返回按钮
        automaticallyImplyLeading: false,
        title: const Text('设置'),
      ),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(T.s16, T.s12, T.s16, T.s24),
        children: [
          // ---- 外观 ----
          _GroupLabel(title: '外观', dark: dark),
          _GroupCard(
            dark: dark,
            children: [
              AnimatedBuilder(
                animation: ThemeController.i,
                builder: (context, _) => Column(
                  children: [
                    for (final m in ThemeMode.values)
                      _RadioTile(
                        dark: dark,
                        title: switch (m) {
                          ThemeMode.system => '跟随系统',
                          ThemeMode.light => '浅色模式',
                          ThemeMode.dark => '深色模式',
                        },
                        icon: switch (m) {
                          ThemeMode.system => Icons.brightness_auto_rounded,
                          ThemeMode.light => Icons.light_mode_rounded,
                          ThemeMode.dark => Icons.dark_mode_rounded,
                        },
                        selected: ThemeController.i.mode == m,
                        onTap: () => ThemeController.i.set(m),
                      ),
                  ],
                ),
              ),
            ],
          ),
          // ---- 检查更新 ----
          _GroupLabel(title: '检查更新', dark: dark),
          _GroupCard(
            dark: dark,
            children: [
              _SettingTile(
                dark: dark,
                icon: Icons.system_update_alt_rounded,
                title: '检查更新',
                subtitle: '服务器',
                trailing: _checking
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : Icon(
                        Icons.chevron_right_rounded,
                        color: dark ? T.darkInkSub : T.inkSub,
                      ),
                onTap: _checking ? null : _checkUpdate,
              ),
              _Divider(dark: dark),
              _SettingTile(
                dark: dark,
                icon: Icons.notifications_active_rounded,
                title: '自动检查更新',
                subtitle: '启动时在后台静默检查，发现新版本时提醒',
                trailing: Switch(value: _auto, onChanged: _setAuto),
              ),
            ],
          ),
          // ---- 帮助中心 ----
          _GroupLabel(title: '帮助中心', dark: dark),
          _GroupCard(
            dark: dark,
            children: [
              _SettingTile(
                dark: dark,
                icon: Icons.help_rounded,
                title: '帮助中心',
                subtitle: '后台常驻、电池优化、常见问题',
                trailing: Icon(
                  Icons.chevron_right_rounded,
                  color: dark ? T.darkInkSub : T.inkSub,
                ),
                onTap: () =>
                    Navigator.of(context)
                        .push(FadeSlideRoute(page: const HelpPage())),
              ),
            ],
          ),
          // ---- 关于 ----
          _GroupLabel(title: '关于', dark: dark),
          _GroupCard(
            dark: dark,
            children: [
              _SettingTile(
                dark: dark,
                icon: Icons.info_rounded,
                title: '版本',
                subtitle: 'v$kAppVersion',
              ),
              _Divider(dark: dark),
              _SettingTile(
                dark: dark,
                icon: Icons.business_rounded,
                title: '济源市励友软件有限公司',
                subtitle: '© 济源市励友软件有限公司',
              ),
            ],
          ),
          const SizedBox(height: T.s16),
          // ---- 账号：退出登录（危险操作独立分组） ----
          _GroupLabel(title: '账号', dark: dark),
          _GroupCard(
            dark: dark,
            children: [
              _SettingTile(
                dark: dark,
                icon: Icons.logout_rounded,
                iconColor: Theme.of(context).colorScheme.error,
                title: '退出登录',
                titleColor: Theme.of(context).colorScheme.error,
                subtitle: '停止连接并返回登录页',
                onTap: _logout,
              ),
            ],
          ),
          const SizedBox(height: T.s16),
          Text(
            '励联 LiLink · 远程内网穿透',
            textAlign: TextAlign.center,
            style: TextStyle(
              fontSize: 11,
              color: dark ? T.darkInkSub.withValues(alpha: .7) : T.inkSub,
            ),
          ),
        ],
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// 分组部件
// ---------------------------------------------------------------------------
class _GroupLabel extends StatelessWidget {
  final String title;
  final bool dark;

  const _GroupLabel({required this.title, required this.dark});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(T.s4, T.s14, T.s4, T.s8),
      child: Text(
        title,
        style: TextStyle(
          fontSize: 12,
          fontWeight: FontWeight.w700,
          letterSpacing: 1,
          color: dark ? T.darkInkSub : T.inkSub,
        ),
      ),
    );
  }
}

class _GroupCard extends StatelessWidget {
  final bool dark;
  final List<Widget> children;

  const _GroupCard({required this.dark, required this.children});

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: dark ? T.darkCard : Colors.white,
        borderRadius: BorderRadius.circular(T.rItem),
        border: Border.all(
          color: (dark ? Colors.white : T.inkStrong).withValues(alpha: .07),
        ),
        boxShadow: [
          BoxShadow(
            color: dark
                ? Colors.black.withValues(alpha: .2)
                : T.inkStrong.withValues(alpha: .05),
            blurRadius: 10,
            offset: const Offset(0, 3),
          ),
        ],
      ),
      clipBehavior: Clip.antiAlias,
      child: Column(children: children),
    );
  }
}

class _Divider extends StatelessWidget {
  final bool dark;

  const _Divider({required this.dark});

  @override
  Widget build(BuildContext context) {
    return Divider(
      height: 1,
      indent: 52,
      color: (dark ? Colors.white : T.inkStrong).withValues(alpha: .08),
    );
  }
}

class _SettingTile extends StatelessWidget {
  final bool dark;
  final IconData icon;
  final String title;
  final String? subtitle;
  final Widget? trailing;
  final VoidCallback? onTap;
  final Color? iconColor;
  final Color? titleColor;

  const _SettingTile({
    required this.dark,
    required this.icon,
    required this.title,
    this.subtitle,
    this.trailing,
    this.onTap,
    this.iconColor,
    this.titleColor,
  });

  @override
  Widget build(BuildContext context) {
    final subInk = dark ? T.darkInkSub : T.inkSub;
    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: T.s16, vertical: 13),
        child: Row(
          children: [
            Icon(
              icon,
              size: 21,
              color: iconColor ?? (dark ? T.darkBrand : T.brand),
            ),
            const SizedBox(width: T.s14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    title,
                    style: TextStyle(
                      fontSize: 15,
                      fontWeight: FontWeight.w500,
                      color: titleColor ??
                          (dark ? const Color(0xFFE2E8F0) : T.inkStrong),
                    ),
                  ),
                  if (subtitle != null && subtitle!.isNotEmpty) ...[
                    const SizedBox(height: 2),
                    Text(
                      subtitle!,
                      style: TextStyle(fontSize: 12, color: subInk),
                    ),
                  ],
                ],
              ),
            ),
            if (trailing != null) ...[const SizedBox(width: T.s8), trailing!],
          ],
        ),
      ),
    );
  }
}

class _RadioTile extends StatelessWidget {
  final bool dark;
  final String title;
  final IconData icon;
  final bool selected;
  final VoidCallback onTap;

  const _RadioTile({
    required this.dark,
    required this.title,
    required this.icon,
    required this.selected,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final accent = dark ? T.darkBrand : T.brand;
    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: T.s16, vertical: 11),
        child: Row(
          children: [
            Icon(
              icon,
              size: 20,
              color: selected ? accent : (dark ? T.darkInkSub : T.inkSub),
            ),
            const SizedBox(width: T.s14),
            Expanded(
              child: Text(
                title,
                style: TextStyle(
                  fontSize: 15,
                  fontWeight: selected ? FontWeight.w600 : FontWeight.w400,
                  color: dark ? const Color(0xFFE2E8F0) : T.inkStrong,
                ),
              ),
            ),
            AnimatedSwitcher(
              duration: const Duration(milliseconds: 150),
              child: selected
                  ? Icon(Icons.check_circle_rounded, size: 20, color: accent)
                  : Icon(
                      Icons.circle_outlined,
                      size: 20,
                      color: (dark ? Colors.white : T.inkStrong).withValues(
                        alpha: .18,
                      ),
                    ),
            ),
          ],
        ),
      ),
    );
  }
}

/// 品牌渐变整行按钮（44px）。
class _GradientAction extends StatelessWidget {
  final String label;

  const _GradientAction({required this.label});

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        gradient: T.brandGradient,
        borderRadius: BorderRadius.circular(T.rField),
        boxShadow: const [
          BoxShadow(
            color: Color(0x331667C8),
            blurRadius: 12,
            offset: Offset(0, 5),
          ),
        ],
      ),
      child: SizedBox(
        height: 44,
        child: Center(
          child: Text(
            label,
            style: const TextStyle(
              fontSize: 15,
              fontWeight: FontWeight.w700,
              color: Colors.white,
            ),
          ),
        ),
      ),
    );
  }
}
