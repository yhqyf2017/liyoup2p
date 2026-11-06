package reliable

import (
	"crypto/rand"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// TestLoopbackHighThroughput 环回大流量：2MB 双向，验证大窗口下不死锁。
func TestLoopbackHighThroughput(t *testing.T) {
	la, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	lb, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	// 与真实数据面一致：tunnel.SetUDPBuffer 的保守值
	_ = la.SetReadBuffer(256 << 10)
	_ = la.SetWriteBuffer(256 << 10)
	_ = lb.SetReadBuffer(256 << 10)
	_ = lb.SetWriteBuffer(256 << 10)
	aAddr := la.LocalAddr().(*net.UDPAddr)
	bAddr := lb.LocalAddr().(*net.UDPAddr)
	var key [32]byte
	_, _ = rand.Read(key[:])
	a := New(la, bAddr, key)
	b := New(lb, aAddr, key)
	defer a.Close()
	defer b.Close()

	payload := make([]byte, 2<<20)
	for i := range payload {
		payload[i] = byte(i * 13)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		got := make([]byte, len(payload))
		if _, err := io.ReadFull(b, got); err != nil {
			t.Errorf("接收失败: %v", err)
			return
		}
		for i := range got {
			if got[i] != payload[i] {
				t.Errorf("数据不一致 @%d", i)
				return
			}
		}
	}()
	start := time.Now()
	if _, err := a.Write(payload); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		t.Logf("2MB 环回耗时 %v", time.Since(start))
	case <-time.After(20 * time.Second):
		t.Fatalf("20s 未完成：大窗口环回死锁复现")
	}
}
