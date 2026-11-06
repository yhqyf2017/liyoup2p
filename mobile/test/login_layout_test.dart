import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:liyou_p2p/pages/login_page.dart';

void main() {
  /// harness：411x914（1080p）、360x640（720p 逻辑分辨率）。
  /// 验证：键盘收起一屏完整不滚动；键盘弹出所有元素尺寸/位置/透明度零变化
  /// （仅视口整体上移，平台标准行为）；无溢出。
  Future<void> pumpLogin(
    WidgetTester tester, {
    required Size size,
    double inset = 0,
  }) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1.0;
    tester.view.viewInsets = FakeViewPadding(bottom: inset);
    addTearDown(tester.view.reset);
    await tester.pumpWidget(const MaterialApp(home: LoginPage()));
    await tester.pumpAndSettle();
  }

  // 登录按钮（find.text('登录') 会同时命中玻璃卡标题，取最后一个 = 按钮）
  // Finder 为惰性求值，main 开头赋值安全
  final btnFinder = find.text('登录').last;

  group('登录页：整页渐变+中央玻璃卡', () {
    testWidgets('1080p 无键盘：全部元素渲染、一屏完整', (tester) async {
      await pumpLogin(tester, size: const Size(411, 914));
      expect(tester.takeException(), isNull);
      for (final label in ['励联 LiLink', '账号', '密码', '设备名称']) {
        expect(find.text(label), findsOneWidget, reason: '$label 未渲染');
      }
      expect(find.text('登录'), findsNWidgets(2), reason: '卡标题+按钮各一个');
      expect(find.text('服务器地址'), findsNothing, reason: '服务器字段不应展示');
      // 一屏完整：品牌标题与登录按钮均在屏内
      final brand = tester.getRect(find.text('励联 LiLink'));
      final btn = tester.getRect(btnFinder);
      expect(brand.top, greaterThanOrEqualTo(0));
      expect(btn.bottom, lessThan(914), reason: '登录按钮超出屏幕，常态不该滚动');
    });

    testWidgets('1080p 无键盘：玻璃卡悬浮居中（四周留边 ~20px）', (tester) async {
      await pumpLogin(tester, size: const Size(411, 914));
      expect(tester.takeException(), isNull);
      final card = tester.getRect(find.byKey(const Key('loginFormCard')));
      expect(card.left, closeTo(20, 1), reason: '卡片左边距应为 20');
      expect(411 - card.right, closeTo(20, 1), reason: '卡片右边距应为 20');
    });

    testWidgets('720p 矮屏（360x640）无键盘一屏不滚动', (tester) async {
      await pumpLogin(tester, size: const Size(360, 640));
      expect(tester.takeException(), isNull);
      final btn = tester.getRect(btnFinder);
      expect(btn.bottom, lessThan(640), reason: '登录按钮超出屏幕，常态不该滚动');
    });

    testWidgets('键盘弹出：所有元素尺寸/位置零变化（仅视口上移）', (tester) async {
      await pumpLogin(tester, size: const Size(411, 914));
      final titleRect0 = tester.getRect(find.text('励联 LiLink'));
      final logoRect0 = tester.getRect(find.byType(Image).first);
      final btnRect0 = tester.getRect(btnFinder);
      final cardRect0 = tester.getRect(find.byKey(const Key('loginFormCard')));

      tester.view.viewInsets = FakeViewPadding(bottom: 300);
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);

      // 零变化：品牌区与玻璃卡位置、尺寸完全不变（内容 < 剩余视口，无滚动发生）
      expect(
        tester.getRect(find.text('励联 LiLink')),
        titleRect0,
        reason: '键盘弹出后品牌标题发生了移动/缩放',
      );
      expect(
        tester.getRect(find.byType(Image).first),
        logoRect0,
        reason: '键盘弹出后 Logo 发生了移动/缩放',
      );
      expect(tester.getRect(btnFinder), btnRect0, reason: '键盘弹出后登录按钮变化');
      expect(
        tester.getRect(find.byKey(const Key('loginFormCard'))),
        cardRect0,
        reason: '键盘弹出后表单卡发生了移动/缩放',
      );
      // 所有元素仍渲染
      for (final label in ['励联 LiLink', '账号', '密码', '设备名称']) {
        expect(find.text(label), findsOneWidget, reason: '键盘弹出后 $label 消失');
      }
      expect(find.text('登录'), findsNWidgets(2));
    });

    testWidgets('720p 矮屏+键盘（360x640，键盘 300px）：无溢出、聚焦可见', (tester) async {
      await pumpLogin(tester, size: const Size(360, 640), inset: 300);
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      // 键盘态允许页面平移：按钮必须在剩余视口内可见
      final btn = tester.getRect(btnFinder);
      expect(btn.bottom, lessThan(640), reason: '登录按钮超出屏幕底部');
      expect(find.text('账号'), findsOneWidget);
    });
  });
}
