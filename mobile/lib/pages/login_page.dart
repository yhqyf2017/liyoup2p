import 'dart:convert';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../core/p2p_bridge.dart';
import '../core/theme.dart';
import 'work_page.dart';

export '../core/theme.dart' show LField;

/// 登录页：浅色模式为浅色底（顶部淡品牌蓝过渡）+ 白色表单卡；
/// 暗黑模式为深蓝渐变 + 柔光玻璃卡。表单（标题「登录」+ 租户/账号/密码/设备名称
/// 浮动标签 + 渐变按钮）居中承载，品牌区在卡片上方。
/// 键盘弹出：resizeToAvoidBottomInset:true 平台标准整体上移——所有元素尺寸、
/// 透明度、位置样式零变化，聚焦字段由框架自动滚动到可视区；键盘收起态一屏完整不滚动。
class LoginPage extends StatefulWidget {
  const LoginPage({super.key});

  @override
  State<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends State<LoginPage> {
  final _form = GlobalKey<FormState>();
  // 服务器/租户保持默认值（RELEASE 由桥接层强制默认地址），UI 不再展示，登录载荷不变
  final _server = TextEditingController(
    text: 'http://jyly8888.gnway.vip:58894',
  );
  final _tenant = TextEditingController();
  final _user = TextEditingController();
  final _pass = TextEditingController();
  final _devName = TextEditingController(text: 'Android设备');
  bool _busy = false;
  bool _obscure = true;
  String? _err;

  @override
  void initState() {
    super.initState();
    _loadSaved();
  }

  /// 回填上次登录的租户/账号/密码/设备名（记住登录）。
  Future<void> _loadSaved() async {
    final p = await SharedPreferences.getInstance();
    if (!mounted) return;
    setState(() {
      _tenant.text = p.getString('saved_tenant') ?? '';
      _user.text = p.getString('saved_user') ?? '';
      _pass.text = p.getString('saved_pass') ?? '';
      _devName.text = p.getString('saved_devName') ?? 'Android设备';
    });
  }

  @override
  void dispose() {
    for (final c in [_server, _tenant, _user, _pass, _devName]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _submit() async {
    if (!_form.currentState!.validate()) return;
    FocusScope.of(context).unfocus();
    setState(() {
      _busy = true;
      _err = null;
    });
    try {
      final session = await p2p.login(
        platformURL: _server.text.trim(),
        tenant: _tenant.text.trim(),
        user: _user.text.trim(),
        pass: _pass.text,
        devName: _devName.text.trim(),
      );
      final p = await SharedPreferences.getInstance();
      await p.setString('session', jsonEncode(session));
      // 记住登录：保存本次成功凭据，下次启动自动回填
      await p.setString('saved_tenant', _tenant.text.trim());
      await p.setString('saved_user', _user.text.trim());
      await p.setString('saved_pass', _pass.text);
      await p.setString('saved_devName', _devName.text.trim());
      if (!mounted) return;
      Navigator.of(context)
          .pushReplacement(FadeSlideRoute(page: WorkPage(session: session)));
    } on PlatformException catch (e) {
      setState(() => _err = e.message ?? '登录失败');
    } catch (e) {
      setState(() => _err = '登录失败：$e');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    // 浅色模式：浅色页面底 + 白色表单卡（状态栏深色图标）；
    // 暗黑模式：深蓝渐变底 + 玻璃卡（状态栏白图标）
    return AnnotatedRegion<SystemUiOverlayStyle>(
      value: (dark ? SystemUiOverlayStyle.light : SystemUiOverlayStyle.dark)
          .copyWith(
            statusBarColor: Colors.transparent,
            systemNavigationBarColor: dark ? const Color(0xFF0C1E3E) : T.lightBg,
          ),
      child: Scaffold(
        // 平台标准键盘行为：视口整体上移，元素样式零变化（无收缩/淡出/重排）
        resizeToAvoidBottomInset: true,
        // 底色与页面底部同色：键盘上方若露出空隙（厂商 inset 声明差异）无缝衔接
        backgroundColor: dark ? const Color(0xFF0C1E3E) : T.lightBg,
        body: Container(
          decoration: BoxDecoration(
            gradient: dark
                ? T.darkHeroGradient
                : const LinearGradient(
                    begin: Alignment.topCenter,
                    end: Alignment.bottomCenter,
                    colors: [T.brandTint, T.lightBg],
                  ),
          ),
          child: Stack(
            children: [
              // 微光晕装饰：浅色底上淡品牌蓝点缀 / 深蓝底上白色点缀
              Positioned(
                top: -60,
                right: -70,
                child: _GlowDot(
                  color: dark ? T.darkBrand : T.skyBlue,
                  alpha: dark ? .14 : .10,
                  size: 260,
                ),
              ),
              Positioned(
                bottom: -80,
                left: -70,
                child: _GlowDot(
                  color: dark ? T.darkBrand : T.brand,
                  alpha: dark ? .10 : .07,
                  size: 300,
                ),
              ),
              SafeArea(
                child: LayoutBuilder(
                  builder: (context, constraints) {
                    // 内容最小高度 = 视口 - 上下 padding：键盘收起且无溢出时
                    // maxScrollExtent 恰为 0，页面完全不可滑动（Clamp 物理）
                    const pad = EdgeInsets.fromLTRB(20, 12, 20, 14);
                    return SingleChildScrollView(
                      padding: pad,
                      child: ConstrainedBox(
                        constraints: BoxConstraints(
                          minHeight: math.max(
                            0,
                            constraints.maxHeight - pad.vertical,
                          ),
                        ),
                        child: IntrinsicHeight(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.stretch,
                            children: [
                              // ---- 品牌区：Logo + 产品名 + 副文案，居中紧凑 ----
                              Column(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  const _LogoBadge(),
                                  const SizedBox(height: 8),
                                  Text(
                                    '励联 LiLink',
                                    style: TextStyle(
                                      fontSize: 20,
                                      fontWeight: FontWeight.w700,
                                      color: dark
                                          ? Colors.white
                                          : T.inkStrong,
                                      letterSpacing: 1,
                                    ),
                                  ),
                                  const SizedBox(height: 4),
                                  Text(
                                    '远程内网穿透 · 安全直达',
                                    style: TextStyle(
                                      fontSize: 12,
                                      color: dark
                                          ? const Color(0xFFC7DCF5)
                                          : T.inkSub,
                                    ),
                                  ),
                                ],
                              ),
                              const SizedBox(height: 16),
                              // ---- 中央表单卡：单次淡入（无 stagger） ----
                              TweenAnimationBuilder<double>(
                                tween: Tween(begin: 0, end: 1),
                                duration: const Duration(milliseconds: 300),
                                curve: Curves.easeOutCubic,
                                builder: (context, opacity, child) =>
                                    Opacity(opacity: opacity, child: child),
                                child: _formCard(dark),
                              ),
                              // 版权行：视口有富余时贴底，键盘弹出时紧随卡片（Spacer 归零）
                              const Spacer(),
                              Text(
                                '© 济源市励友软件有限公司',
                                textAlign: TextAlign.center,
                                style: TextStyle(
                                  fontSize: 11,
                                  color: dark
                                      ? Colors.white.withValues(alpha: .55)
                                      : T.inkSub.withValues(alpha: .8),
                                ),
                              ),
                            ],
                          ),
                        ),
                      ),
                    );
                  },
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  /// 表单卡：浅色模式为白底企业卡（浅描边 + 品牌蓝柔和投影），
  /// 暗黑模式沿用柔光玻璃卡（blur 30 + 白 .18 覆层 + 1px 白 .18 描边）。
  Widget _formCard(bool dark) {
    final form = Form(
      key: _form,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            '登录',
            textAlign: TextAlign.center,
            style: TextStyle(
              fontSize: 20,
              fontWeight: FontWeight.w700,
              color: dark ? const Color(0xFFE2E8F0) : T.inkStrong,
              letterSpacing: 2,
            ),
          ),
          const SizedBox(height: T.s16),
          LField(
            label: '租户',
            controller: _tenant,
            textInputAction: TextInputAction.next,
            hint: '所属企业/租户标识',
            validator: (v) =>
                (v == null || v.trim().isEmpty) ? '请输入租户' : null,
          ),
          const SizedBox(height: 14),
          LField(
            label: '账号',
            controller: _user,
            keyboardType: TextInputType.emailAddress,
            textInputAction: TextInputAction.next,
            hint: '登录账号',
            validator: (v) =>
                (v == null || v.trim().isEmpty) ? '请输入账号' : null,
          ),
          const SizedBox(height: 14),
          LField(
            label: '密码',
            controller: _pass,
            obscure: _obscure,
            onToggleObscure: () => setState(() => _obscure = !_obscure),
            textInputAction: TextInputAction.next,
            hint: '登录密码',
            validator: (v) => (v == null || v.isEmpty) ? '请输入密码' : null,
          ),
          const SizedBox(height: 14),
          LField(
            label: '设备名称',
            controller: _devName,
            textInputAction: TextInputAction.done,
            onEditingComplete: _submit,
            hint: '在平台中显示的名称',
          ),
          if (_err != null) ...[
            const SizedBox(height: T.s8),
            Row(
              children: [
                Icon(
                  Icons.error_outline,
                  size: 15,
                  color: Theme.of(context).colorScheme.error,
                ),
                const SizedBox(width: 4),
                Expanded(
                  child: Text(
                    _err!,
                    style: TextStyle(
                      fontSize: 12,
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                ),
              ],
            ),
          ],
          const SizedBox(height: T.s16),
          PressableScale(
            onTap: _busy ? null : _submit,
            child: _GradientButton(label: _busy ? '登录中…' : '登录', busy: _busy),
          ),
        ],
      ),
    );
    if (dark) {
      return GlassCard(
        key: const Key('loginFormCard'),
        radius: T.rPanel,
        border: Border.all(color: Colors.white.withValues(alpha: .18)),
        padding: const EdgeInsets.fromLTRB(T.s20, 22, T.s20, 22),
        child: form,
      );
    }
    return Container(
      key: const Key('loginFormCard'),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(T.rPanel),
        border: Border.all(color: const Color(0xFFE6ECF5)),
        boxShadow: const [
          BoxShadow(
            color: Color(0x1A1667C8),
            blurRadius: 28,
            offset: Offset(0, 12),
          ),
        ],
      ),
      padding: const EdgeInsets.fromLTRB(T.s20, 22, T.s20, 22),
      child: form,
    );
  }
}

/// 极淡光晕装饰：径向渐变淡出圆形，无需模糊滤镜（渐变本身柔和、省性能）。
class _GlowDot extends StatelessWidget {
  final Color color;
  final double alpha;
  final double size;

  const _GlowDot({
    required this.color,
    required this.alpha,
    required this.size,
  });

  @override
  Widget build(BuildContext context) {
    return IgnorePointer(
      child: Container(
        width: size,
        height: size,
        decoration: BoxDecoration(
          shape: BoxShape.circle,
          gradient: RadialGradient(
            colors: [
              color.withValues(alpha: alpha),
              color.withValues(alpha: 0),
            ],
          ),
        ),
      ),
    );
  }
}

/// 无字纯图形 Logo 徽标（白底圆角卡 + 品牌图形）。
class _LogoBadge extends StatelessWidget {
  const _LogoBadge();

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Container(
        width: 48,
        height: 48,
        decoration: BoxDecoration(
          color: Colors.white,
          borderRadius: BorderRadius.circular(12),
          boxShadow: const [
            BoxShadow(
              color: Color(0x330F51A8),
              blurRadius: 16,
              offset: Offset(0, 6),
            ),
          ],
        ),
        padding: const EdgeInsets.all(5),
        child: Image.asset('assets/logo.png', fit: BoxFit.contain),
      ),
    );
  }
}

/// 品牌渐变整行按钮（48px，圆角 12，微阴影）。按压缩放由外层 PressableScale 承担。
class _GradientButton extends StatelessWidget {
  final String label;
  final bool busy;

  const _GradientButton({required this.label, this.busy = false});

  @override
  Widget build(BuildContext context) {
    return Opacity(
      opacity: busy ? .75 : 1,
      child: DecoratedBox(
        decoration: BoxDecoration(
          gradient: T.brandGradient,
          borderRadius: BorderRadius.circular(T.rField),
          boxShadow: const [
            BoxShadow(
              color: Color(0x331667C8),
              blurRadius: 14,
              offset: Offset(0, 6),
            ),
          ],
        ),
        child: SizedBox(
          height: 48,
          child: Row(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              if (busy)
                const SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    color: Colors.white,
                  ),
                )
              else
                const Icon(
                  Icons.arrow_forward_rounded,
                  size: 18,
                  color: Colors.white,
                ),
              const SizedBox(width: 8),
              Text(
                label,
                style: const TextStyle(
                  fontSize: 15,
                  fontWeight: FontWeight.w700,
                  color: Colors.white,
                  letterSpacing: .5,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
