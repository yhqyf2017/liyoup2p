import 'package:flutter_test/flutter_test.dart';
import 'package:liyou_p2p/core/p2p_bridge.dart';

void main() {
  const prefixed =
      '已连接信令 ws://jyly8888.gnway.vip:58894/ws 启动：platform=http://jyly8888.gnway.vip:58894';
  const bare = '平台 jyly8888.gnway.vip:58894 就绪';

  test('正式包（enabled=true）：前缀与裸域名形式均替换为「服务器」', () {
    final a = sanitizeServerText(prefixed, enabled: true);
    expect(a.contains('jyly8888'), isFalse, reason: '地址未脱敏：$a');
    expect(a.contains('服务器'), isTrue);
    // 前缀形式应整体替换，不残留「http://」等残缺前缀
    expect(a.contains('http://'), isFalse, reason: '前缀未整体替换：$a');
    expect(sanitizeServerText(bare, enabled: true), '平台 服务器 就绪');
  });

  test('调试构建（kHideServerAddr=false）：日志原样保留地址', () {
    // flutter test 运行于非 release 虚拟机，kHideServerAddr 恒为 false
    expect(kHideServerAddr, isFalse);
    expect(sanitizeServerText(prefixed), prefixed);
    expect(sanitizeServerText(bare), bare);
  });

  test('空文本原样返回', () {
    expect(sanitizeServerText('', enabled: true), '');
  });
}
