package config

import "testing"

func setMap(t *testing.T, v string) (PortMap, error) {
	t.Helper()
	c := &Config{}
	return func() (PortMap, error) {
		if err := (mapFlag{c}).Set(v); err != nil {
			return PortMap{}, err
		}
		return c.Maps[0], nil
	}()
}

// TestMapFlagLegacy 两段式与旧式「本地地址自带冒号」写法保持不变。
func TestMapFlagLegacy(t *testing.T) {
	m, err := setMap(t, "8080:18080")
	if err != nil || m.Host != "" || m.Remote != 8080 || m.Local != "18080" {
		t.Fatalf("两段式解析错误: %+v err=%v", m, err)
	}
	if m.String() != "8080:18080" {
		t.Fatalf("两段式字符串化错误: %s", m.String())
	}
	m, err = setMap(t, "80:127.0.0.1:18080")
	if err != nil || m.Host != "" || m.Remote != 80 || m.Local != "127.0.0.1:18080" {
		t.Fatalf("旧式带地址映射解析错误: %+v err=%v", m, err)
	}
}

// TestMapFlagHost 三段式「主机:远端端口:本机端口」。
func TestMapFlagHost(t *testing.T) {
	m, err := setMap(t, "192.168.5.20:9123:19124")
	if err != nil || m.Host != "192.168.5.20" || m.Remote != 9123 || m.Local != "19124" {
		t.Fatalf("三段式解析错误: %+v err=%v", m, err)
	}
	if m.String() != "192.168.5.20:9123:19124" {
		t.Fatalf("三段式字符串化错误: %s", m.String())
	}
	m, err = setMap(t, "jyerp.local:80:18080")
	if err != nil || m.Host != "jyerp.local" || m.Remote != 80 {
		t.Fatalf("主机名三段式解析错误: %+v err=%v", m, err)
	}
	if _, err := setMap(t, "192.168.5.*:80:18080"); err == nil {
		t.Fatalf("通配主机应被拒绝")
	}
	if _, err := setMap(t, "0.0.0.0:80:18080"); err == nil {
		t.Fatalf("0.0.0.0 应被拒绝")
	}
}

// TestValidateHost 白名单主机校验。
func TestValidateHost(t *testing.T) {
	valid := []string{"127.0.0.1", "192.168.5.20", "::1", "2001:db8::1", "jyerp.local", "PC-01", "a_b.cn", "xn--fiq228c.example"}
	for _, h := range valid {
		if err := ValidateHost(h); err != nil {
			t.Errorf("%q 应合法，报错: %v", h, err)
		}
	}
	invalid := []string{"", "0.0.0.0", "::", "192.168.5.0/24", "192.168.5.*", "*.local", "a..b", "-abc", "abc-", "a b", "1.2.3.4:80", "http://a.com"}
	for _, h := range invalid {
		if err := ValidateHost(h); err == nil {
			t.Errorf("%q 应被拒绝", h)
		}
	}
}
