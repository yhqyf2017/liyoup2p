import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:liyou_p2p/core/p2p_bridge.dart';
import 'package:liyou_p2p/core/update_service.dart';
import 'package:liyou_p2p/pages/help_page.dart';
import 'package:liyou_p2p/pages/settings_page.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  group('更新响应解析（优雅降级）', () {
    test('合法响应 {code:0,data:{...}} → AppUpdate', () {
      final u = parseLatestResponse(
        '{"code":0,"data":{"version":"1.2.0","url":"http://x/a.apk","notes":"修复若干问题","force":false}}',
      );
      expect(u, isNotNull);
      expect(u!.version, '1.2.0');
      expect(u.url, 'http://x/a.apk');
      expect(u.notes, '修复若干问题');
      expect(u.force, isFalse);
    });

    test('force=true 正确解析', () {
      final u = parseLatestResponse(
        '{"code":0,"data":{"version":"2.0.0","url":"u","notes":"n","force":true}}',
      );
      expect(u!.force, isTrue);
    });

    test('code!=0 / data 缺失 / 非法 JSON / 字段为空 → 全部 null', () {
      expect(parseLatestResponse('{"code":1,"data":{}}'), isNull);
      expect(parseLatestResponse('{"code":0}'), isNull);
      expect(parseLatestResponse('not json'), isNull);
      expect(
        parseLatestResponse('{"code":0,"data":{"version":"","url":"x"}}'),
        isNull,
      );
      expect(
        parseLatestResponse('{"code":0,"data":{"version":"1.2","url":""}}'),
        isNull,
      );
      expect(parseLatestResponse('404 page'), isNull);
    });
  });

  group('版本比较', () {
    test('相同版本不更新', () {
      expect(isNewerVersion('1.1.0', '1.1.0'), isFalse);
      expect(isNewerVersion('v1.1.0', '1.1.0'), isFalse);
    });
    test('高位/低位/位数差异', () {
      expect(isNewerVersion('1.2.0', '1.1.0'), isTrue);
      expect(isNewerVersion('1.1.10', '1.1.9'), isTrue);
      expect(isNewerVersion('2.0.0', '1.9.9'), isTrue);
      expect(isNewerVersion('1.1.0', '1.1'), isFalse); // 缺段补 0
      expect(isNewerVersion('1.0.9', '1.1.0'), isFalse);
    });
  });

  group('平台地址推导（脱敏）', () {
    test('ws://host:port/ws → http://host:port', () {
      expect(
        platformBaseFromSession({'platformWs': 'ws://10.0.0.1:58894/ws'}),
        'http://10.0.0.1:58894',
      );
    });
    test('wss:// → https://', () {
      expect(
        platformBaseFromSession({'platformWs': 'wss://a.b.com/ws'}),
        'https://a.b.com',
      );
    });
    test('缺失回落内置地址', () {
      expect(platformBaseFromSession({}), kDefaultServerURL);
    });
  });

  group('设置项持久化', () {
    test('自动检查更新：默认开启，可关闭并读取', () async {
      SharedPreferences.setMockInitialValues({});
      var p = await SharedPreferences.getInstance();
      // 缺省为开启（null != false）
      expect(p.getBool(kAutoUpdateKey) != false, isTrue);
      await p.setBool(kAutoUpdateKey, false);
      p = await SharedPreferences.getInstance();
      expect(p.getBool(kAutoUpdateKey) != false, isFalse);
    });
  });

  group('页面渲染', () {
    testWidgets('设置页渲染：外观三选 + 检查更新 + 帮助入口 + 关于', (tester) async {
      SharedPreferences.setMockInitialValues({});
      await tester.pumpWidget(
        const MaterialApp(home: SettingsPage(session: {})),
      );
      await tester.pumpAndSettle();
      expect(find.text('外观'), findsOneWidget);
      expect(find.text('跟随系统'), findsOneWidget);
      expect(find.text('浅色模式'), findsOneWidget);
      expect(find.text('深色模式'), findsOneWidget);
      // 分组标签与条目标题同名 → 至少各出现一次
      expect(find.text('检查更新'), findsWidgets);
      expect(find.text('自动检查更新'), findsOneWidget);
      expect(find.text('帮助中心'), findsWidgets);
      expect(find.text('版本'), findsOneWidget);
      expect(find.text('v$kAppVersion'), findsOneWidget);
      expect(find.text('© 济源市励友软件有限公司'), findsOneWidget);
    });

    testWidgets('帮助中心渲染：五节图文步骤', (tester) async {
      await tester.pumpWidget(const MaterialApp(home: HelpPage()));
      await tester.pumpAndSettle();
      for (final t in [
        '1. 保持后台常驻',
        '2. 厂商电池优化',
        '3. 后台锁定',
        '4. 通知权限',
        '5. 常见问题',
      ]) {
        expect(find.text(t), findsOneWidget, reason: '缺少 $t');
      }
    });
  });
}
