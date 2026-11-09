import 'dart:ui';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// 设计令牌：品牌蓝 135° 渐变 / 浅色·暗黑双体系 / 圆角与间距刻度。
abstract final class T {
  // ---- 品牌色 ----
  static const brand = Color(0xFF1667C8);
  static const brandDeep = Color(0xFF0F51A8);
  static const skyBlue = Color(0xFF2AA4E9); // 天蓝辅助（光晕/点缀）
  static const brandGradient = LinearGradient(
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
    colors: [brand, brandDeep],
  );

  // ---- 浅色体系 ----
  static const lightBg = Color(0xFFF6F8FC);
  static const fieldFill = Color(0xFFF1F5F9);
  static const inkStrong = Color(0xFF0F172A);
  static const inkSub = Color(0xFF64748B);
  static const brandTint = Color(0xFFEAF2FC); // 品牌浅底（角色卡）

  // ---- 暗黑体系 ----
  static const darkBg = Color(0xFF0F172A);
  static const darkCard = Color(0xFF1E293B);
  static const darkField = Color(0xFF334155);
  static const darkInkSub = Color(0xFF94A3B8);
  static const darkBrand = Color(0xFF7EB3F7); // 暗黑下的品牌亮蓝
  static const darkBrandTint = Color(0xFF1E3A5F); // 暗黑角色卡底
  static const darkHeroGradient = LinearGradient(
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
    colors: [Color(0xFF0C1E3E), Color(0xFF10336B)],
  ); // 登录页整页深蓝渐变

  // ---- 玻璃覆层 ----
  static const glassLight = Color(0x1FFFFFFF); // white .12
  static const glassDark = Color(0x14FFFFFF); // white .08

  // ---- 圆角刻度 ----
  static const rPanel = 28.0, rCard = 20.0, rItem = 14.0, rField = 12.0;

  // ---- 间距刻度 ----
  static const s4 = 4.0, s8 = 8.0, s12 = 12.0, s14 = 14.0, s16 = 16.0;
  static const s18 = 18.0, s20 = 20.0, s24 = 24.0;

  static ThemeData light() {
    final scheme = ColorScheme.fromSeed(seedColor: brand)
        .copyWith(primary: brand);
    return _base(scheme, dark: false).copyWith(
      scaffoldBackgroundColor: lightBg,
      appBarTheme: const AppBarTheme(
        backgroundColor: Colors.transparent,
        foregroundColor: inkStrong,
        elevation: 0,
        scrolledUnderElevation: 0,
        centerTitle: false,
        // 浅色页面状态栏深色图标（透明底），所有带 AppBar 的页面统一生效
        systemOverlayStyle: SystemUiOverlayStyle(
          statusBarColor: Colors.transparent,
          statusBarIconBrightness: Brightness.dark,
          statusBarBrightness: Brightness.light,
          systemNavigationBarColor: lightBg,
          systemNavigationBarIconBrightness: Brightness.dark,
        ),
        titleTextStyle: TextStyle(
          fontSize: 18,
          fontWeight: FontWeight.w600,
          color: inkStrong,
        ),
      ),
      cardTheme: CardThemeData(
        color: Colors.white,
        elevation: 0,
        margin: EdgeInsets.zero,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(rItem),
        ),
      ),
    );
  }

  static ThemeData dark() {
    const scheme = ColorScheme.dark(
      primary: darkBrand,
      onPrimary: Color(0xFF0B2A57),
      secondary: darkBrand,
      surface: darkCard,
      onSurface: Color(0xFFE2E8F0),
    );
    return _base(scheme, dark: true).copyWith(
      scaffoldBackgroundColor: darkBg,
      appBarTheme: const AppBarTheme(
        backgroundColor: Colors.transparent,
        foregroundColor: Color(0xFFE2E8F0),
        elevation: 0,
        scrolledUnderElevation: 0,
        centerTitle: false,
        systemOverlayStyle: SystemUiOverlayStyle(
          statusBarColor: Colors.transparent,
          statusBarIconBrightness: Brightness.light,
          statusBarBrightness: Brightness.dark,
          systemNavigationBarColor: darkBg,
          systemNavigationBarIconBrightness: Brightness.light,
        ),
        titleTextStyle: TextStyle(
          fontSize: 18,
          fontWeight: FontWeight.w600,
          color: Color(0xFFE2E8F0),
        ),
      ),
      cardTheme: CardThemeData(
        color: darkCard,
        elevation: 0,
        margin: EdgeInsets.zero,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(rItem),
        ),
      ),
    );
  }

  /// Apple 柔光玻璃滤镜：强模糊 + 饱和度提升 1.5（玻璃后的颜色鲜艳透亮，
  /// 这是 iOS「液态玻璃」观感的关键，比单纯 blur 更接近系统材质）。
  static ImageFilter frostFilter({double blur = 48}) => ImageFilter.compose(
        outer: ImageFilter.blur(sigmaX: blur, sigmaY: blur),
        inner: const ColorFilter.matrix([
          1.3935, -0.3575, -0.036, 0, 0, //
          -0.1065, 1.1425, -0.036, 0, 0, //
          -0.1065, -0.3575, 1.464, 0, 0, //
          0, 0, 0, 1, 0,
        ]),
      );

  /// AppBar 柔光玻璃背景（全局统一，与首页一致）：亮玻璃 + 自上而下品牌色晕染
  /// 渐变（状态栏区域到 AppBar 底部），内容滚过时仍有磨砂透光。
  /// 用法：AppBar(flexibleSpace: T.glassAppBar(dark: dark))。
  static Widget glassAppBar({required bool dark}) => ClipRect(
        child: BackdropFilter(
          filter: frostFilter(blur: 48),
          child: Container(
            decoration: BoxDecoration(
              gradient: LinearGradient(
                begin: Alignment.topCenter,
                end: Alignment.bottomCenter,
                // 全高线性渐变：状态栏到 AppBar 底部白→微蓝，无集中过渡带
                colors: dark
                    ? const [
                        Color(0x2EFFFFFF),
                        Color(0xFF1B2942),
                      ]
                    : const [
                        Color(0xFFFFFFFF),
                        Color(0xFFE9F0F9),
                      ],
              ),
              border: Border(
                bottom: BorderSide(
                  color: Colors.black.withValues(alpha: dark ? .20 : .05),
                  width: .5,
                ),
              ),
            ),
          ),
        ),
      );

  static ThemeData _base(ColorScheme scheme, {required bool dark}) {
    final fieldBorder = OutlineInputBorder(
      borderRadius: BorderRadius.circular(rField),
      borderSide: BorderSide.none,
    );
    final focusBorder = OutlineInputBorder(
      borderRadius: BorderRadius.circular(rField),
      borderSide: BorderSide(color: dark ? darkBrand : brand, width: 2),
    );
    return ThemeData(
      useMaterial3: true,
      colorScheme: scheme,
      brightness: dark ? Brightness.dark : Brightness.light,
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: dark ? darkField : fieldFill,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: 16,
          vertical: 14,
        ),
        enabledBorder: fieldBorder,
        focusedBorder: focusBorder,
        hintStyle: TextStyle(
          fontSize: 14,
          color: dark ? darkInkSub : inkSub.withValues(alpha: .7),
        ),
        labelStyle: TextStyle(fontSize: 14, color: dark ? darkInkSub : inkSub),
      ),
      elevatedButtonTheme: ElevatedButtonThemeData(
        style: ElevatedButton.styleFrom(
          backgroundColor: Colors.white,
          foregroundColor: brand,
          elevation: 2,
          shadowColor: const Color(0x330F51A8),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(rField),
          ),
          minimumSize: const Size(0, 44),
          padding: const EdgeInsets.symmetric(horizontal: 20),
          textStyle: const TextStyle(fontSize: 14, fontWeight: FontWeight.w700),
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(rField),
          ),
          minimumSize: const Size(0, 44),
          padding: const EdgeInsets.symmetric(horizontal: 20),
          textStyle: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(rField),
          ),
        ),
      ),
      iconButtonTheme: IconButtonThemeData(
        style: IconButton.styleFrom(
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(rField),
          ),
        ),
      ),
      dividerTheme: DividerThemeData(
        color: (dark ? Colors.white : inkStrong).withValues(alpha: .08),
        thickness: 1,
        space: 1,
      ),
      // Material 3 对话框：圆角 24、品牌主题面、统一标题/正文风格
      dialogTheme: DialogThemeData(
        backgroundColor: dark ? const Color(0xFF1E293B) : Colors.white,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(24)),
        titleTextStyle: TextStyle(
          fontSize: 17,
          fontWeight: FontWeight.w700,
          color: dark ? const Color(0xFFE2E8F0) : inkStrong,
        ),
        contentTextStyle: TextStyle(
          fontSize: 14,
          height: 1.5,
          color: dark ? T.darkInkSub : T.inkSub,
        ),
      ),
      // Material 3 底部导航：悬浮圆角胶囊指示器，品牌蓝选中态
      navigationBarTheme: NavigationBarThemeData(
        height: 68,
        backgroundColor: dark ? T.darkBg : T.lightBg,
        surfaceTintColor: Colors.transparent,
        indicatorColor: dark
            ? T.darkBrand.withValues(alpha: .30)
            : T.brandTint,
        elevation: 0,
        labelTextStyle: WidgetStatePropertyAll(
          TextStyle(
            fontSize: 12,
            fontWeight: FontWeight.w600,
            color: dark ? T.darkInkSub : T.inkSub,
          ),
        ),
        iconTheme: WidgetStateProperty.resolveWith((states) => IconThemeData(
              size: 24,
              color: states.contains(WidgetState.selected)
                  ? (dark ? T.darkBrand : T.brand)
                  : (dark ? T.darkInkSub : T.inkSub),
            )),
        labelBehavior: NavigationDestinationLabelBehavior.alwaysShow,
      ),
      // Material 3 SnackBar：悬浮圆角卡、深底白字（浅色模式）/ 亮卡暗字（暗黑模式），
      // 全局所有提示统一走此主题，页面只传 content 即可
      snackBarTheme: SnackBarThemeData(
        behavior: SnackBarBehavior.floating,
        // 浅色模式白底浮层 + 深色文字（企业级），暗黑模式深灰蓝
        backgroundColor: dark ? const Color(0xFF334155) : Colors.white,
        contentTextStyle: TextStyle(
          fontSize: 14,
          fontWeight: FontWeight.w500,
          color: dark ? const Color(0xFFF1F5F9) : T.inkStrong,
        ),
        actionTextColor: dark ? const Color(0xFF7EB3F7) : T.brand,
        closeIconColor: dark ? const Color(0xFFCBD5E1) : T.inkSub,
        elevation: 6,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
        // Scaffold 已自动避开底部选项卡，仅留 12px 呼吸间距，不顶到屏幕中部
        insetPadding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
      ),
    );
  }
}

/// 浮层提示语义类型：决定图标与颜色
enum ToastType { info, success, warn, error }

/// 危险操作统一确认弹窗：标题 + 说明 + 取消 / 红色实心确认按钮。
/// 删除规则、退出登录等破坏性操作共用，保证全局一致。
Future<bool> confirmDanger(
  BuildContext context, {
  required String title,
  required String message,
  String confirmLabel = '删除',
}) async {
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: Text(title),
      content: Text(message),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(ctx).pop(false),
          child: const Text('取消'),
        ),
        FilledButton(
          style: FilledButton.styleFrom(
            backgroundColor: Theme.of(ctx).colorScheme.error,
          ),
          onPressed: () => Navigator.of(ctx).pop(true),
          child: Text(confirmLabel),
        ),
      ],
    ),
  );
  return ok == true;
}

/// 带图标的底部浮层提示（M3 SnackBar）：信息蓝 / 成功绿 / 警告琥珀 / 错误红。
/// 全 App 统一入口，样式由 snackBarTheme 控制。
void showToast(
  BuildContext context,
  String msg, {
  ToastType type = ToastType.info,
}) {
  final dark = Theme.of(context).brightness == Brightness.dark;
  final (icon, color) = switch (type) {
    ToastType.info => (
      Icons.info_outline_rounded,
      dark ? const Color(0xFF7EB3F7) : T.brand,
    ),
    ToastType.success => (
      Icons.check_circle_outline_rounded,
      const Color(0xFF16A34A),
    ),
    ToastType.warn => (
      Icons.warning_amber_rounded,
      const Color(0xFFF5A623),
    ),
    ToastType.error => (
      Icons.error_outline_rounded,
      Theme.of(context).colorScheme.error,
    ),
  };
  ScaffoldMessenger.of(context).showSnackBar(
    SnackBar(
      duration: const Duration(seconds: 3),
      content: Row(
        children: [
          Icon(icon, size: 18, color: color),
          const SizedBox(width: 8),
          Expanded(child: Text(msg)),
        ],
      ),
    ),
  );
}

/// Material 3 浮动标签输入框（高 48、圆角 12）：label 默认显示在框内占位，
/// 聚焦/有内容时缩小上浮到描边线上（描边自动开窗避让）。浅灰填充，聚焦品牌蓝 2px。
class LField extends StatelessWidget {
  final String label;
  final String? hint;
  final TextEditingController? controller;
  final TextInputType? keyboardType;
  final bool obscure;
  final VoidCallback? onToggleObscure;
  final TextInputAction? textInputAction;
  final FocusNode? focusNode;
  final VoidCallback? onEditingComplete;
  final String? Function(String?)? validator;

  const LField({
    super.key,
    required this.label,
    this.hint,
    this.controller,
    this.keyboardType,
    this.obscure = false,
    this.onToggleObscure,
    this.textInputAction,
    this.focusNode,
    this.onEditingComplete,
    this.validator,
  });

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    final subColor = dark ? T.darkInkSub : T.inkSub;
    final brandColor = dark ? T.darkBrand : T.brand;
    final labelColor = WidgetStateColor.resolveWith(
      (states) => states.contains(WidgetState.focused) ? brandColor : subColor,
    );
    return TextFormField(
      controller: controller,
      keyboardType: keyboardType,
      obscureText: obscure,
      validator: validator,
      focusNode: focusNode,
      textInputAction: textInputAction,
      onEditingComplete: onEditingComplete,
      style: TextStyle(
        fontSize: 15,
        fontWeight: FontWeight.w500,
        color: dark ? const Color(0xFFE2E8F0) : T.inkStrong,
      ),
      decoration: InputDecoration(
        labelText: label,
        hintText: hint,
        hintStyle: TextStyle(
          fontSize: 14.5,
          fontWeight: FontWeight.w400,
          color: dark
              ? subColor.withValues(alpha: .55)
              : const Color(0xFFA9B1C0),
        ),
        isDense: true,
        filled: true,
        fillColor: dark ? T.darkField : const Color(0xFFF7F8FA),
        // 非对称 contentPadding：顶部空间是浮动标签上浮后的布局空间，
        // 过小会导致 label 被钳制裁切（实测过小时 label 仅剩 9px 高）
        contentPadding: const EdgeInsets.fromLTRB(14, 22, 14, 10),
        labelStyle: const TextStyle(
          fontSize: 14.5,
          fontWeight: FontWeight.w500,
        ).copyWith(color: labelColor),
        floatingLabelStyle: const TextStyle(
          fontSize: 13.5,
          fontWeight: FontWeight.w500,
        ).copyWith(color: labelColor),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(12),
          borderSide: BorderSide(
            color: dark
                ? Colors.white.withValues(alpha: .10)
                : const Color(0xFFE2E8F0),
          ),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(12),
          borderSide: BorderSide(color: brandColor, width: 2),
        ),
        // 校验失败：整个输入框红色描边（聚焦红边加粗），错误文案 12px 减少跳动
        errorStyle: TextStyle(
          fontSize: 12,
          height: 1.3,
          color: Theme.of(context).colorScheme.error,
        ),
        errorBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(12),
          borderSide: BorderSide(
            color: Theme.of(context).colorScheme.error,
            width: 1.5,
          ),
        ),
        focusedErrorBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(12),
          borderSide: BorderSide(
            color: Theme.of(context).colorScheme.error,
            width: 2,
          ),
        ),
        suffixIcon: onToggleObscure == null
            ? null
            : IconButton(
                onPressed: onToggleObscure,
                visualDensity: VisualDensity.compact,
                padding: EdgeInsets.zero,
                constraints: const BoxConstraints(minWidth: 36, minHeight: 36),
                icon: Icon(
                  obscure
                      ? Icons.visibility_off_outlined
                      : Icons.visibility_outlined,
                  size: 18,
                  color: subColor,
                ),
              ),
      ),
    );
  }
}

/// 柔光玻璃容器：ClipRRect + BackdropFilter(blur 30) + 白 .20/.14 覆层
/// + 1px 白 .35 内描边高光 + 底部微渐变，磨砂质感肉眼可见。
class GlassCard extends StatelessWidget {
  final Widget child;
  final EdgeInsetsGeometry padding;
  final double blur;
  final double radius;
  final Color? color;
  final Gradient? gradient;
  final Border? border;

  /// 为 true 时在整卡上叠一层顶部白 .18→透明的高光渐变
  /// （用于品牌渐变底，营造玻璃反光）。
  final bool highlight;

  const GlassCard({
    super.key,
    required this.child,
    this.padding = const EdgeInsets.all(T.s16),
    this.blur = 30,
    this.radius = T.rCard,
    this.color,
    this.gradient,
    this.border,
    this.highlight = false,
  });

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    final effectiveBorder =
        border ??
        Border.all(
          color: dark
              ? Colors.white.withValues(alpha: .24)
              : Colors.white.withValues(alpha: .50),
        );
    return DecoratedBox(
      // 阴影放在裁剪外层：ClipRRect 会把内部 boxShadow 裁掉
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(radius),
        boxShadow: [
          BoxShadow(
            color: dark ? const Color(0x66000000) : const Color(0x1E0F51A8),
            blurRadius: 24,
            offset: const Offset(0, 10),
          ),
        ],
      ),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(radius),
        child: BackdropFilter(
          // Apple 柔光玻璃：blur 44 + 饱和度提升
          filter: T.frostFilter(blur: blur < 44 ? 44 : blur),
          child: Container(
            decoration: BoxDecoration(
              // 斜向光斑：左上亮右下暗，模拟玻璃受光；覆盖尽量低保持透亮
              gradient: gradient ??
                  LinearGradient(
                    begin: Alignment.topLeft,
                    end: Alignment.bottomRight,
                    colors: dark
                        ? [
                            Colors.white.withValues(alpha: .14),
                            Colors.white.withValues(alpha: .04),
                          ]
                        : [
                            Colors.white.withValues(alpha: .42),
                            Colors.white.withValues(alpha: .14),
                          ],
                  ),
              borderRadius: BorderRadius.circular(radius),
              border: effectiveBorder,
            ),
            padding: EdgeInsets.zero,
            child: Stack(
              children: [
                Padding(padding: padding, child: child),
                // 顶部高光：上亮下透明的玻璃反光层
                IgnorePointer(
                  child: Container(
                    decoration: BoxDecoration(
                      borderRadius: BorderRadius.circular(radius),
                      gradient: LinearGradient(
                        begin: Alignment.topCenter,
                        end: Alignment.bottomCenter,
                        stops: const [0, .35, 1],
                        colors: [
                          Colors.white.withValues(alpha: dark ? .12 : .30),
                          Colors.white.withValues(alpha: dark ? .03 : .06),
                          Colors.transparent,
                        ],
                      ),
                    ),
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

/// 卡片进场 stagger 微动画：位移 + 淡入（200ms，delay 逐个错开）。
class StaggerIn extends StatelessWidget {
  final Widget child;
  final int index; // 序号，决定延迟
  final Duration base;

  const StaggerIn({
    super.key,
    required this.child,
    this.index = 0,
    this.base = const Duration(milliseconds: 200),
  });

  @override
  Widget build(BuildContext context) {
    final delay = base * index.clamp(0, 8);
    return TweenAnimationBuilder<double>(
      tween: Tween(begin: 0, end: 1),
      duration: base + delay,
      curve: Interval(
        (delay.inMilliseconds / (base + delay).inMilliseconds).clamp(0.0, 0.9),
        1,
        curve: Curves.easeOutCubic,
      ),
      builder: (context, t, _) => Opacity(
        opacity: t.clamp(0, 1),
        child: Transform.translate(
          offset: Offset(0, 14 * (1 - t)),
          child: child,
        ),
      ),
    );
  }
}

/// 按钮按压反馈：scale 0.97 按下 / 弹回（隐式动画 120ms）。
class PressableScale extends StatefulWidget {
  final Widget child;
  final VoidCallback? onTap;

  const PressableScale({super.key, required this.child, this.onTap});

  @override
  State<PressableScale> createState() => _PressableScaleState();
}

class _PressableScaleState extends State<PressableScale> {
  bool _down = false;

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTapDown: widget.onTap == null
          ? null
          : (_) => setState(() => _down = true),
      onTapUp: (_) => setState(() => _down = false),
      onTapCancel: () => setState(() => _down = false),
      onTap: widget.onTap,
      child: AnimatedScale(
        scale: _down ? 0.97 : 1,
        duration: const Duration(milliseconds: 120),
        curve: Curves.easeOut,
        child: widget.child,
      ),
    );
  }
}

/// 页面转场：淡入 + 轻微上滑（250ms easeOutCubic）。
class FadeSlideRoute<PageT> extends PageRouteBuilder<PageT> {
  final Widget page;

  FadeSlideRoute({required this.page})
    : super(
        transitionDuration: const Duration(milliseconds: 250),
        reverseTransitionDuration: const Duration(milliseconds: 200),
        pageBuilder: (context, anim, anim2) => page,
        transitionsBuilder: (context, anim, anim2, child) {
          final t = CurvedAnimation(parent: anim, curve: Curves.easeOutCubic);
          return FadeTransition(
            opacity: t,
            child: SlideTransition(
              position: Tween(
                begin: const Offset(0, .04),
                end: Offset.zero,
              ).animate(t),
              child: child,
            ),
          );
        },
      );
}
