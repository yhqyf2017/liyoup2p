package reliable

import (
	"bytes"
	"crypto/rand"
	"net"
	"sync"
	"testing"
	"time"
)

func testKey() [32]byte {
	var k [32]byte
	_, _ = rand.Read(k[:])
	return k
}

// pipeSock 测试用 UDP 套接字替身：注入丢包/乱序/重复，模拟不可靠链路。
type pipeSock struct {
	mu      sync.Mutex
	in      chan []byte
	peer    *pipeSock
	dropP   float64
	reP     float64 // 乱序概率（延迟投递）
	dupP    float64
	closed  bool
	deadline time.Time
	seq     int64
}

func newSockPair(dropP, reP, dupP float64) (*pipeSock, *pipeSock) {
	a := &pipeSock{in: make(chan []byte, 4096), dropP: dropP, reP: reP, dupP: dupP}
	b := &pipeSock{in: make(chan []byte, 4096), dropP: dropP, reP: reP, dupP: dupP}
	a.peer, b.peer = b, a
	return a, b
}

func (s *pipeSock) WriteToUDP(b []byte, _ *net.UDPAddr) (int, error) {
	s.mu.Lock()
	closed, dropP, reP, dupP, n := s.closed, s.dropP, s.reP, s.dupP, s.seq
	s.seq++
	s.mu.Unlock()
	if closed {
		return 0, net.ErrClosed
	}
	send := func(delay time.Duration) {
		cp := append([]byte(nil), b...)
		if delay > 0 {
			time.AfterFunc(delay, func() {
				s.peer.mu.Lock()
				defer s.peer.mu.Unlock()
				if !s.peer.closed {
					select {
					case s.peer.in <- cp:
					default:
					}
				}
			})
		} else {
			s.peer.mu.Lock()
			defer s.peer.mu.Unlock()
			if !s.peer.closed {
				select {
				case s.peer.in <- cp:
				default:
				}
			}
		}
	}
	// 确定性伪随机：基于包序号，避免测试抖动
	r := frac(n*2654435761 + 1)
	if r < dropP {
		return len(b), nil // 丢包
	}
	if frac(n*40503+7) < dupP {
		send(0)
	}
	if frac(n*97+13) < reP {
		send(4 * time.Millisecond) // 乱序：延迟投递
	} else {
		send(0)
	}
	return len(b), nil
}

func frac(x int64) float64 {
	x = x%1000003 + 1000003
	return float64(x%1000) / 1000
}

func (s *pipeSock) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	s.mu.Lock()
	closed, deadline := s.closed, s.deadline
	s.mu.Unlock()
	if closed {
		return 0, nil, net.ErrClosed
	}
	var timeout <-chan time.Time
	if !deadline.IsZero() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case data := <-s.in:
		n := copy(b, data)
		return n, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000}, nil
	case <-timeout:
		return 0, nil, osDeadline()
	}
}

func (s *pipeSock) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	close(s.in)
	return nil
}

func (s *pipeSock) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	s.deadline = t
	s.mu.Unlock()
	return nil
}

func osDeadline() error { return errTimeout{} }

type errTimeout struct{}

func (errTimeout) Error() string { return "timeout" }

// TestReliableRoundTripLossy 丢包 15% + 乱序 30% + 重复 5% 的链路上双向传输 1MB。
func TestReliableRoundTripLossy(t *testing.T) {
	sa, sb := newSockPair(0.15, 0.30, 0.05)
	key := testKey()
	a := New(sa, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, key)
	b := New(sb, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2}, key)
	defer a.Close()
	defer b.Close()

	payload := make([]byte, 1<<20)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	go func() {
		got := make([]byte, len(payload))
		read, err := readFull(b, got)
		if err != nil {
			t.Errorf("对端读取失败: %v", err)
			return
		}
		_ = read
		// 回显
		if _, err := b.Write(got); err != nil {
			t.Errorf("对端回显失败: %v", err)
		}
	}()

	if _, err := a.Write(payload); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	echo := make([]byte, len(payload))
	if err := readDeadline(a, echo, 30*time.Second); err != nil {
		t.Fatalf("回显读取失败: %v", err)
	}
	if !bytes.Equal(echo, payload) {
		t.Fatalf("往返数据不一致")
	}
}

// TestReliableReadDeadline 空读超时。
func TestReliableReadDeadline(t *testing.T) {
	sa, sb := newSockPair(0, 0, 0)
	key := testKey()
	a := New(sa, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, key)
	_ = New(sb, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2}, key)
	defer a.Close()
	defer sb.Close()
	if err := a.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("设置截止时间失败: %v", err)
	}
	buf := make([]byte, 16)
	if _, err := a.Read(buf); err == nil {
		t.Fatalf("应超时")
	}
}

// TestReliableFIN 对端关闭后本端先读尽数据再得到 EOF。
func TestReliableFIN(t *testing.T) {
	sa, sb := newSockPair(0.1, 0.2, 0)
	key := testKey()
	a := New(sa, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, key)
	b := New(sb, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2}, key)
	defer b.Close()

	msg := []byte("理游 P2P 可靠流 FIN 测试")
	if _, err := a.Write(msg); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	// 给重传留出时间后关闭，FIN 应在 2 秒宽限内送达
	time.Sleep(300 * time.Millisecond)
	_ = a.Close()

	got := make([]byte, len(msg))
	if err := readDeadline(b, got, 5*time.Second); err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("FIN 前数据不一致")
	}
	if _, err := b.Read(got); err == nil {
		t.Fatalf("FIN 后应返回 EOF")
	}
}

// TestReliableGarbageDropped 打洞残留探测报文不应破坏流。
func TestReliableGarbageDropped(t *testing.T) {
	sa, sb := newSockPair(0, 0, 0)
	key := testKey()
	a := New(sa, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, key)
	b := New(sb, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2}, key)
	defer a.Close()
	defer b.Close()

	// 向 b 注入垃圾报文
	sb.mu.Lock()
	if !sb.closed {
		sb.in <- []byte("LYP2P-PROBE")
		sb.in <- []byte{1, 2, 3}
	}
	sb.mu.Unlock()

	msg := []byte("垃圾不干扰正常数据")
	if _, err := a.Write(msg); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	got := make([]byte, len(msg))
	if err := readDeadline(b, got, 5*time.Second); err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("数据不一致")
	}
}

func readFull(c *Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func readDeadline(c *Conn, buf []byte, d time.Duration) error {
	if err := c.SetReadDeadline(time.Now().Add(d)); err != nil {
		return err
	}
	defer c.SetReadDeadline(time.Time{})
	_, err := readFull(c, buf)
	return err
}
