package tunnel

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func testKey() [32]byte {
	var k [32]byte
	for i := range k {
		k[i] = byte(i + 1)
	}
	return k
}

// TestSealOpenRoundTrip 加解密往返。
func TestSealOpenRoundTrip(t *testing.T) {
	key := testKey()
	plain := []byte("理游 P2P 隧道数据 hello 123")
	sealed := Seal(key, plain)
	got, err := Open(key, sealed)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("往返数据不一致")
	}
	if bytes.Equal(sealed, plain) {
		t.Fatalf("密文不应等于明文")
	}
	// 篡改检测
	bad := append([]byte{}, sealed...)
	bad[len(bad)-1] ^= 0xff
	if _, err := Open(key, bad); err == nil {
		t.Fatalf("篡改密文应解密失败")
	}
}

// TestFrameRoundTrip TCP 帧编解码往返。
func TestFrameRoundTrip(t *testing.T) {
	key := testKey()
	var buf bytes.Buffer
	payload := []byte("GET / HTTP/1.1\r\nHost: 127.0.0.1:18080\r\n\r\n")
	if err := WriteFrame(&buf, FrameData, payload, key); err != nil {
		t.Fatalf("写帧失败: %v", err)
	}
	t2, got, err := ReadFrame(&buf, key)
	if err != nil {
		t.Fatalf("读帧失败: %v", err)
	}
	if t2 != FrameData || !bytes.Equal(got, payload) {
		t.Fatalf("帧往返不一致")
	}
	// close 帧
	buf.Reset()
	_ = WriteFrame(&buf, FrameClose, nil, key)
	t3, got3, err := ReadFrame(&buf, key)
	if err != nil || t3 != FrameClose || len(got3) != 0 {
		t.Fatalf("close 帧往返失败: %v", err)
	}
	// 多帧连读
	buf.Reset()
	for i := 0; i < 3; i++ {
		_ = WriteFrame(&buf, FrameData, []byte{byte(i)}, key)
	}
	for i := 0; i < 3; i++ {
		_, got, err := ReadFrame(&buf, key)
		if err != nil || len(got) != 1 || got[0] != byte(i) {
			t.Fatalf("第 %d 帧读取失败", i)
		}
	}
}

// TestUDPFrameRoundTrip UDP 帧收发往返。
func TestUDPFrameRoundTrip(t *testing.T) {
	key := testKey()
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

	payload := []byte("udp 帧数据")
	go func() {
		_ = WriteUDPFrame(a, b.LocalAddr().(*net.UDPAddr), FrameData, payload, key)
	}()
	done := make(chan error, 1)
	go func() {
		tt, got, from, err := ReadUDPFrame(b, key)
		if err != nil {
			done <- err
			return
		}
		if tt != FrameData || !bytes.Equal(got, payload) || from == nil {
			done <- errFrameMismatch
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("UDP 帧往返失败: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("UDP 帧往返超时")
	}
}

var errFrameMismatch = &frameError{"帧内容不匹配"}

type frameError struct{ msg string }

func (e *frameError) Error() string { return e.msg }
