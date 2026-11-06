import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:liyou_p2p/core/theme.dart';
import 'package:liyou_p2p/core/theme_controller.dart';

void main() {
  group('ThemeController 解析与序列化', () {
    test('字符串 → ThemeMode 双向往返', () {
      for (final m in [ThemeMode.system, ThemeMode.light, ThemeMode.dark]) {
        expect(ThemeController.parse(ThemeController.name(m)), m);
      }
    });

    test('未知值回落 system', () {
      expect(ThemeController.parse(null), ThemeMode.system);
      expect(ThemeController.parse('bogus'), ThemeMode.system);
      expect(ThemeController.parse(''), ThemeMode.system);
    });
  });

  group('设计令牌与双主题', () {
    test('浅色/暗黑两套 ThemeData 均为 Material 3 且品牌蓝主色', () {
      final light = T.light();
      final dark = T.dark();
      expect(light.useMaterial3, isTrue);
      expect(dark.useMaterial3, isTrue);
      expect(T.brand, const Color(0xFF1667C8));
      expect(T.brandDeep, const Color(0xFF0F51A8));
      // 暗黑底色/卡片
      expect(dark.scaffoldBackgroundColor, const Color(0xFF0F172A));
      expect(dark.cardTheme.color, const Color(0xFF1E293B));
      // 浅色主色与暗黑主色不同（品牌蓝 vs 亮蓝）
      expect(light.colorScheme.primary, isNot(dark.colorScheme.primary));
      expect(light.cardTheme.shape, isA<RoundedRectangleBorder>());
    });
  });
}
