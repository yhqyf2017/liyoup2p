import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'core/theme.dart';
import 'core/theme_controller.dart';
import 'pages/login_page.dart';
import 'pages/work_page.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  // 状态栏透明，具体图标明暗由各页 AnnotatedRegion 按主题设置
  SystemChrome.setSystemUIOverlayStyle(
    const SystemUiOverlayStyle(statusBarColor: Colors.transparent),
  );
  runApp(const LiLinkApp());
}

/// PREVIEW=true：跳过登录直接进工作页假数据（仅 UI 迭代用，release 不带此 define）。
const _preview = bool.fromEnvironment('PREVIEW');

class LiLinkApp extends StatefulWidget {
  const LiLinkApp({super.key});

  @override
  State<LiLinkApp> createState() => _LiLinkAppState();
}

class _LiLinkAppState extends State<LiLinkApp> {
  final _theme = ThemeController.i;
  bool _restored = false;
  Map<String, dynamic>? _session;

  @override
  void initState() {
    super.initState();
    _theme.addListener(_onTheme);
    _restore();
  }

  void _onTheme() => setState(() {});

  Future<void> _restore() async {
    await _theme.load();
    // 已有会话直接进工作页
    try {
      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString('session');
      if (raw != null) {
        final decoded = jsonDecode(raw);
        if (decoded is Map) {
          _session = Map<String, dynamic>.from(decoded);
        }
      }
    } catch (_) {}
    if (mounted) setState(() => _restored = true);
  }

  @override
  void dispose() {
    _theme.removeListener(_onTheme);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: '励联 LiLink',
      debugShowCheckedModeBanner: false,
      theme: T.light(),
      darkTheme: T.dark(),
      themeMode: _theme.mode,
      home: !_restored
          ? const Scaffold(body: Center(child: CircularProgressIndicator()))
          : (_session != null
                ? WorkPage(session: _session!)
                : (_preview
                      ? WorkPage(
                          session: const {
                            'deviceToken': 'preview-token',
                            'platformWs': 'ws://jyly8888.gnway.vip:58894/ws',
                            'deviceUid': 'preview-device-uid',
                          },
                        )
                      : const LoginPage())),
    );
  }
}
