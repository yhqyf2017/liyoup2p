import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// 主题模式控制器：跟随系统 + 手动切换，持久化到 shared_preferences。
class ThemeController extends ChangeNotifier {
  ThemeController._();
  static final i = ThemeController._();

  static const _key = 'theme.mode';
  ThemeMode mode = ThemeMode.system;

  Future<void> load() async {
    // PREVIEW 迭代模式：主题由 DARK define 决定，不读持久化
    if (const bool.fromEnvironment('PREVIEW')) {
      mode = const bool.fromEnvironment('DARK')
          ? ThemeMode.dark
          : ThemeMode.light;
      notifyListeners();
      return;
    }
    final p = await SharedPreferences.getInstance();
    mode = parse(p.getString(_key));
    notifyListeners();
  }

  Future<void> set(ThemeMode m) async {
    mode = m;
    notifyListeners();
    final p = await SharedPreferences.getInstance();
    await p.setString(_key, name(m));
  }

  /// 循环切换 system → light → dark → system。
  Future<void> cycle() {
    const order = [ThemeMode.system, ThemeMode.light, ThemeMode.dark];
    return set(order[(order.indexOf(mode) + 1) % order.length]);
  }

  /// 持久化字符串 → ThemeMode（未知值回落 system）。
  static ThemeMode parse(String? v) {
    switch (v) {
      case 'light':
        return ThemeMode.light;
      case 'dark':
        return ThemeMode.dark;
      default:
        return ThemeMode.system;
    }
  }

  static String name(ThemeMode m) {
    switch (m) {
      case ThemeMode.light:
        return 'light';
      case ThemeMode.dark:
        return 'dark';
      default:
        return 'system';
    }
  }
}
