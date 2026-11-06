import 'package:flutter/material.dart';

import '../core/theme.dart';

/// 帮助中心：图文步骤卡片（操作者语言，无技术词）。
class HelpPage extends StatelessWidget {
  const HelpPage({super.key});

  static const _items = <({IconData icon, String title, String desc})>[
    (
      icon: Icons.notifications_active_rounded,
      title: '保持后台常驻',
      desc: '连接运行时，下拉通知栏可以看到「连接运行中」常驻通知。请勿划掉这条通知，划掉后隧道会断开，需要重新启动连接。',
    ),
    (
      icon: Icons.battery_saver_rounded,
      title: '厂商电池优化',
      desc: '建议在手机设置中允许励联后台运行：\n· 华为：设置 → 电池 → 启动管理 → 励联 LiLink → 开启「允许自启动 / 关联启动 / 后台活动」\n· 小米：设置 → 应用设置 → 应用管理 → 励联 LiLink → 省电策略 → 「无限制」\n· OPPO/vivo：设置 → 电池 → 更多 → 关闭「睡眠待机优化」，并在自启动管理中允许励联自启动',
    ),
    (
      icon: Icons.push_pin_outlined,
      title: '后台锁定',
      desc: '打开最近任务（从屏幕底部上滑停顿，或按菜单键），找到励联 LiLink 卡片，下拉卡片并点击锁形图标锁定，防止系统清理后台。',
    ),
    (
      icon: Icons.notifications_rounded,
      title: '通知权限',
      desc: 'Android 13 及以上手机，首次启动会弹出通知权限申请，请选择「允许」。若之前拒绝了：系统设置 → 应用 → 励联 LiLink → 通知 → 允许。',
    ),
    (
      icon: Icons.help_outline_rounded,
      title: '常见问题',
      desc: '连不上服务器怎么办？\n1. 检查手机网络是否可用（可尝试打开网页）。\n2. 确认账号没有被顶下线：同一账号同一时间只能登录一部手机或一台电脑，另一端登录会把当前设备挤下线。\n3. 若仍无法连接，请联系管理员确认平台服务状态。',
    ),
  ];

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    return Scaffold(
      appBar: AppBar(
        flexibleSpace: T.glassAppBar(dark: dark),
        // 默认返回按钮（无圆圈底衬）
        automaticallyImplyLeading: true,
        title: const Text('帮助中心'),
      ),
      body: ListView.separated(
        padding: const EdgeInsets.fromLTRB(T.s16, T.s12, T.s16, T.s24),
        itemCount: _items.length,
        separatorBuilder: (_, _) => const SizedBox(height: T.s12),
        itemBuilder: (_, i) {
          final it = _items[i];
          return StaggerIn(
            index: i,
            child: Card(
              margin: EdgeInsets.zero,
              color: dark ? T.darkCard : Colors.white,
              child: Padding(
                padding: const EdgeInsets.all(T.s16),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Container(
                      width: 44,
                      height: 44,
                      decoration: BoxDecoration(
                        color: dark
                            ? Colors.white.withValues(alpha: .06)
                            : T.brandTint,
                        borderRadius: BorderRadius.circular(T.rField),
                      ),
                      child: Icon(
                        it.icon,
                        size: 22,
                        color: dark ? T.darkBrand : T.brand,
                      ),
                    ),
                    const SizedBox(width: T.s12),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            '${i + 1}. ${it.title}',
                            style: TextStyle(
                              fontSize: 15,
                              fontWeight: FontWeight.w700,
                              color: dark
                                  ? const Color(0xFFE2E8F0)
                                  : T.inkStrong,
                            ),
                          ),
                          const SizedBox(height: T.s4),
                          Text(
                            it.desc,
                            style: TextStyle(
                              fontSize: 13,
                              height: 1.6,
                              color: dark ? T.darkInkSub : T.inkSub,
                            ),
                          ),
                        ],
                      ),
                    ),
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
