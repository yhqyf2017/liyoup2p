import 'package:flutter_test/flutter_test.dart';
import 'package:liyou_p2p/models/map_rule.dart';

void main() {
  group('MapRule.validate', () {
    test('合法规则（无目标主机）', () {
      const r = MapRule(remote: 80, local: '18080');
      expect(r.validate(), isNull);
      expect(r.desc, '80:18080');
    });

    test('合法规则（带目标主机）', () {
      const r = MapRule(host: '192.168.5.20', remote: 80, local: '18080');
      expect(r.validate(), isNull);
      expect(r.desc, '192.168.5.20:80:18080');
    });

    test('远端端口越界', () {
      expect(const MapRule(remote: 0, local: '18080').validate(), isNotNull);
      expect(
        const MapRule(remote: 65536, local: '18080').validate(),
        isNotNull,
      );
    });

    test('本地端口非法', () {
      expect(const MapRule(remote: 80, local: '').validate(), isNotNull);
      expect(const MapRule(remote: 80, local: '0').validate(), isNotNull);
      expect(const MapRule(remote: 80, local: 'abc').validate(), isNotNull);
    });

    test('目标主机：拒绝 0.0.0.0/:: 与通配网段', () {
      expect(
        const MapRule(host: '0.0.0.0', remote: 80, local: '1').validate(),
        isNotNull,
      );
      expect(
        const MapRule(host: '::', remote: 80, local: '1').validate(),
        isNotNull,
      );
      expect(
        const MapRule(host: '192.168.5.*', remote: 80, local: '1').validate(),
        isNotNull,
      );
      expect(
        const MapRule(
          host: '192.168.5.0/24',
          remote: 80,
          local: '1',
        ).validate(),
        isNotNull,
      );
      expect(
        const MapRule(host: '', remote: 80, local: '1').validate(),
        isNull,
      );
    });

    test('目标主机：合法 IPv4/IPv6/主机名', () {
      expect(
        const MapRule(host: '10.0.0.1', remote: 80, local: '1').validate(),
        isNull,
      );
      expect(
        const MapRule(host: 'fe80::1', remote: 80, local: '1').validate(),
        isNull,
      );
      expect(
        const MapRule(host: 'nas.home.arpa', remote: 80, local: '1').validate(),
        isNull,
      );
      expect(
        const MapRule(host: 'my-server_1', remote: 80, local: '1').validate(),
        isNull,
      );
    });
  });

  group('MapRule.sameAs', () {
    test('主机大小写与本地端口归一化不敏感', () {
      const a = MapRule(host: 'NAS.local', remote: 3306, local: '13306');
      const b = MapRule(
        host: 'nas.LOCAL',
        remote: 3306,
        local: '127.0.0.1:13306',
      );
      expect(a.sameAs(b), isTrue);
      expect(
        a.sameAs(const MapRule(host: '', remote: 3306, local: '13306')),
        isFalse,
      );
      expect(
        a.sameAs(
          const MapRule(host: 'nas.local', remote: 3307, local: '13306'),
        ),
        isFalse,
      );
    });
  });

  group('localAddr', () {
    test('纯端口补 127.0.0.1，带地址保持原样', () {
      expect(localAddr('18080'), '127.0.0.1:18080');
      expect(localAddr('0.0.0.0:18080'), '0.0.0.0:18080');
    });
  });
}
