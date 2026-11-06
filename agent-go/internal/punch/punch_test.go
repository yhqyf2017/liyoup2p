package punch

import (
	"net"
	"testing"
	"time"
)

// testKey 回环打洞测试共用的会话密钥。
var testKey = [32]byte{1, 2, 3, 4, 5, 6, 7, 8}

// TestPunchLoopback 本机回环 UDP 打洞：两个套接字互发探测，双方都应成功。
func TestPunchLoopback(t *testing.T) {
	a, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer a.Close()
	b, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer b.Close()

	bAddr := b.LocalAddr().(*net.UDPAddr)
	aAddr := a.LocalAddr().(*net.UDPAddr)

	errA := make(chan error, 1)
	errB := make(chan error, 1)
	var peerA, peerB *net.UDPAddr
	go func() {
		p, err := Punch(a, []*net.UDPAddr{bAddr}, 3*time.Second, testKey, nil)
		peerA = p
		errA <- err
	}()
	go func() {
		p, err := Punch(b, []*net.UDPAddr{aAddr}, 3*time.Second, testKey, nil)
		peerB = p
		errB <- err
	}()
	select {
	case err := <-errA:
		if err != nil {
			t.Fatalf("A 打洞失败: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("A 打洞超时")
	}
	select {
	case err := <-errB:
		if err != nil {
			t.Fatalf("B 打洞失败: %v", err)
		}
		if peerB == nil {
			t.Fatalf("B 未获得对端地址")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("B 打洞超时")
	}
	if peerA == nil {
		t.Fatalf("A 未获得对端地址")
	}
}

// TestPunchForgedProbe 无有效 HMAC 的伪造探测必须被忽略：Punch 不得选定伪造来源。
func TestPunchForgedProbe(t *testing.T) {
	a, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer a.Close()
	forger, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer forger.Close()

	// 持续发送只有明文前缀、无 HMAC 的伪造探测
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = forger.WriteToUDP([]byte(probePrefix), a.LocalAddr().(*net.UDPAddr))
			time.Sleep(50 * time.Millisecond)
		}
	}()

	done := make(chan error, 1)
	go func() {
		_, err := Punch(a, []*net.UDPAddr{forger.LocalAddr().(*net.UDPAddr)}, 700*time.Millisecond, testKey, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("伪造探测被误接受，路径选择被污染")
		}
	case <-time.After(4 * time.Second):
		t.Fatalf("伪造探测测试超时")
	}
}

// TestSortTargets 目标排序：与本机私网同 /24 的对端地址最优先，其余私网次之，公网最后。
func TestSortTargets(t *testing.T) {
	locals := []net.IP{net.ParseIP("192.168.5.8")}
	targets := []*net.UDPAddr{
		{IP: net.ParseIP("222.140.66.39"), Port: 53636}, // 公网 srflx
		{IP: net.ParseIP("10.0.0.5"), Port: 5000},      // 异网段私网
		{IP: net.ParseIP("192.168.5.66"), Port: 53636}, // 同 /24 内网直达
		{IP: net.ParseIP("172.16.0.9"), Port: 6000},    // 异网段私网
	}
	got := sortTargets(targets, locals)
	want := []string{"192.168.5.66:53636", "10.0.0.5:5000", "172.16.0.9:6000", "222.140.66.39:53636"}
	for i, w := range want {
		if i >= len(got) || got[i].String() != w {
			t.Fatalf("排序结果不符，第 %d 项期望 %s，实际 %v", i, w, got)
		}
	}
	// 同优先级应保持原顺序（稳定排序）
	same := []*net.UDPAddr{
		{IP: net.ParseIP("222.140.66.39"), Port: 1},
		{IP: net.ParseIP("222.140.66.40"), Port: 2},
	}
	if stable := sortTargets(same, locals); stable[0].Port != 1 || stable[1].Port != 2 {
		t.Fatalf("同优先级顺序应保持稳定: %v", stable)
	}
	// 输入不应被原地修改
	if targets[0].String() != "222.140.66.39:53636" {
		t.Fatalf("排序不应修改原切片: %v", targets)
	}
}

// TestParseCandidates 候选解析：数组去空去杂质，兼容旧版单 endpoint。
func TestParseCandidates(t *testing.T) {
	got := ParseCandidates(map[string]any{
		"candidates": []any{"192.168.1.2:5000", "  ", "1.2.3.4:5000", 42},
	})
	if len(got) != 2 || got[0] != "192.168.1.2:5000" || got[1] != "1.2.3.4:5000" {
		t.Fatalf("解析结果不符: %v", got)
	}
	legacy := ParseCandidates(map[string]any{"endpoint": "127.0.0.1:9999"})
	if len(legacy) != 1 || legacy[0] != "127.0.0.1:9999" {
		t.Fatalf("兼容 endpoint 失败: %v", legacy)
	}
	if none := ParseCandidates(map[string]any{}); none != nil {
		t.Fatalf("空数据应返回 nil: %v", none)
	}
}
