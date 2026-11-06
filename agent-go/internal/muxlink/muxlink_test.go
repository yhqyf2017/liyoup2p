package muxlink

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"liyou-agent/internal/tunnel"
)

func testKey() [32]byte {
	var k [32]byte
	_, _ = rand.Read(k[:])
	return k
}

// legacyDatagram 按旧版 UDP 帧格式构造报文：[1B 类型][4B 长度][nonce||密文]。
func legacyDatagram(key [32]byte, t byte, payload []byte) []byte {
	sealed := tunnel.Seal(key, payload)
	hdr := [5]byte{t}
	binary.BigEndian.PutUint32(hdr[1:5], uint32(len(sealed)))
	return append(hdr[:], sealed...)
}

// TestStreamHandshakeMux 流式传输上完成魔数协商并建立 smux 会话，子流端口前缀往返。
func TestStreamHandshakeMux(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	serverDone := make(chan uint16, 1)
	go func() {
		sess, _, isMux, err := AcceptServer(a, 3*time.Second)
		if err != nil || !isMux {
			t.Errorf("服务端协商失败: isMux=%v err=%v", isMux, err)
			return
		}
		stream, err := sess.AcceptStream()
		if err != nil {
			t.Errorf("接受子流失败: %v", err)
			return
		}
		var prefix [2]byte
		if _, err := io.ReadFull(stream, prefix[:]); err != nil {
			t.Errorf("读端口前缀失败: %v", err)
			return
		}
		serverDone <- binary.BigEndian.Uint16(prefix[:])
		_, _ = stream.Write([]byte("ok"))
	}()

	sess, err := DialClient(b, 3*time.Second)
	if err != nil {
		t.Fatalf("客户端协商失败: %v", err)
	}
	stream, err := sess.OpenStream()
	if err != nil {
		t.Fatalf("开子流失败: %v", err)
	}
	var prefix [2]byte
	binary.BigEndian.PutUint16(prefix[:], 18080)
	if _, err := stream.Write(prefix[:]); err != nil {
		t.Fatalf("写端口前缀失败: %v", err)
	}
	buf := make([]byte, 2)
	if err := stream.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("设置截止时间失败: %v", err)
	}
	if _, err := io.ReadFull(stream, buf); err != nil {
		t.Fatalf("读回执失败: %v", err)
	}
	select {
	case got := <-serverDone:
		if got != 18080 {
			t.Fatalf("端口前缀不一致: %d", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("服务端未收到端口前缀")
	}
}

// TestStreamHandshakeLegacy 流式传输上非魔数首字节应判定为旧版并推回。
func TestStreamHandshakeLegacy(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	legacy := []byte{tunnel.FrameData, 0, 0, 0, 0}
	go func() {
		_, _ = b.Write(legacy)
		_, _ = b.Write([]byte("后续旧协议字节"))
	}()
	_, raw, isMux, err := AcceptServer(a, 3*time.Second)
	if err != nil {
		t.Fatalf("协商出错: %v", err)
	}
	if isMux {
		t.Fatalf("应判定为旧版")
	}
	got := make([]byte, len(legacy)+len("后续旧协议字节"))
	if _, err := io.ReadFull(raw, got); err != nil {
		t.Fatalf("推回读取失败: %v", err)
	}
	if !bytes.Equal(got, append(legacy, []byte("后续旧协议字节")...)) {
		t.Fatalf("推回字节不一致")
	}
}

// TestStreamHandshakeTimeout 静默客户端应按旧版处理（读超时）。
func TestStreamHandshakeTimeout(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() {
		time.Sleep(500 * time.Millisecond)
		a.Close()
	}()
	_, raw, isMux, err := AcceptServer(a, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if isMux {
		t.Fatalf("应判定为旧版")
	}
	if raw == nil {
		t.Fatalf("应返回原始流")
	}
}

// TestUDPMagicSealParse 魔数报文与旧版帧报文互不混淆。
func TestUDPMagicSealParse(t *testing.T) {
	key := testKey()
	magic := SealMagic(key)
	if _, err := tunnel.Open(key, magic); err != nil {
		t.Fatalf("魔数解密失败: %v", err)
	}
	if plain, err := tunnel.Open(key, magic); err != nil || !IsMagic(plain) {
		t.Fatalf("魔数识别失败")
	}
	// 旧版帧不应被识别为魔数
	legacy := legacyDatagram(key, tunnel.FrameData, []byte("hello"))
	if plain, err := tunnel.Open(key, legacy); err == nil && IsMagic(plain) {
		t.Fatalf("旧版帧被误判为魔数")
	}
	if tp, payload, ok := ParseLegacyUDP(key, legacy); !ok || tp != tunnel.FrameData || !bytes.Equal(payload, []byte("hello")) {
		t.Fatalf("旧版帧解析失败")
	}
	// 魔数报文不应被误判为旧版帧
	if _, _, ok := ParseLegacyUDP(key, magic); ok {
		t.Fatalf("魔数被误判为旧版帧")
	}
	// 探测残留报文两者皆非
	if _, _, ok := ParseLegacyUDP(key, []byte("LYP2P-PROBE")); ok {
		t.Fatalf("探测报文被误判为旧版帧")
	}
}
