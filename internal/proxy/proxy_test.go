package proxy

import (
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"lanrd/internal/proto"
)

// startEcho 启动一个原样回显的 TCP 服务。
func startEcho(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return ln.Addr().String()
}

// startUDPEcho 启动一个原样回显的 UDP 服务。
func startUDPEcho(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, udpMaxDatagram)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteTo(buf[:n], addr)
		}
	}()
	return pc.LocalAddr().String()
}

// wirePair 把 ClientProxy 与 AgentProxy 用两条消息队列直连，
// 模拟经远程控制连接的代理消息交互。
type wirePair struct {
	mu       sync.Mutex
	toAgent  chan *proto.Message
	toClient chan *proto.Message
}

// link 建立直连并启动转发泵。
func link(t *testing.T) (*ClientProxy, *AgentProxy) {
	t.Helper()
	w := &wirePair{
		toAgent:  make(chan *proto.Message, 4096),
		toClient: make(chan *proto.Message, 4096),
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	ap := NewAgentProxy(func(ty proto.Type, b []byte) bool {
		w.toClient <- &proto.Message{Type: ty, Body: b}
		return true
	}, true, quiet)
	cp := NewClientProxy(func(ty proto.Type, b []byte) error {
		w.toAgent <- &proto.Message{Type: ty, Body: b}
		return nil
	}, quiet)
	t.Cleanup(func() {
		cp.Stop()
		ap.Close()
	})
	go func() {
		for m := range w.toAgent {
			ap.HandleClient(m)
		}
	}()
	go func() {
		for m := range w.toClient {
			cp.HandleAgent(m)
		}
	}()
	return cp, ap
}

// TestForwardTCPBigChunks 静态端口映射 TCP 全链路回显(大块数据跨多条 chunk 消息)。
func TestForwardTCPBigChunks(t *testing.T) {
	echo := startEcho(t)
	cp, _ := link(t)
	local := freePort(t)
	require.NoError(t, cp.StartForward([]ForwardRule{
		{Proto: ProtoTCP, Local: local, Remote: echo},
	}))

	c, err := net.Dial("tcp", local)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.SetDeadline(time.Now().Add(5*time.Second)))

	payload := []byte("hello lanrd proxy")
	_, err = c.Write(payload)
	require.NoError(t, err)
	got := make([]byte, len(payload))
	_, err = io.ReadFull(c, got)
	require.NoError(t, err)
	assert.Equal(t, payload, got)

	// 大块数据跨多条 chunk 消息回显
	big := make([]byte, chunkSize*3+17)
	for i := range big {
		big[i] = byte(i)
	}
	go func() { _, _ = c.Write(big) }()
	gotBig := make([]byte, len(big))
	_, err = io.ReadFull(c, gotBig)
	require.NoError(t, err)
	assert.Equal(t, big, gotBig)
}

// TestForwardTCP 静态端口映射 TCP 全链路回显。
func TestForwardTCP(t *testing.T) {
	echo := startEcho(t)
	cp, _ := link(t)
	local := freePort(t)
	require.NoError(t, cp.StartForward([]ForwardRule{
		{Proto: ProtoTCP, Local: local, Remote: echo},
	}))

	c, err := net.Dial("tcp", local)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.SetDeadline(time.Now().Add(5*time.Second)))

	payload := []byte("forward me")
	_, err = c.Write(payload)
	require.NoError(t, err)
	got := make([]byte, len(payload))
	_, err = io.ReadFull(c, got)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

// TestForwardUDP 静态端口映射 UDP 全链路回显(含多对端)。
func TestForwardUDP(t *testing.T) {
	echo := startUDPEcho(t)
	cp, _ := link(t)
	localUDP := freeUDP(t)
	require.NoError(t, cp.StartForward([]ForwardRule{
		{Proto: ProtoUDP, Local: localUDP, Remote: echo},
	}))

	send := func(pc net.PacketConn, msg string) string {
		_, err := pc.WriteTo([]byte(msg), mustResolve(t, localUDP))
		require.NoError(t, err)
		buf := make([]byte, 1024)
		require.NoError(t, pc.SetReadDeadline(time.Now().Add(5*time.Second)))
		n, _, err := pc.ReadFrom(buf)
		require.NoError(t, err)
		return string(buf[:n])
	}
	pc1, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer pc1.Close()
	assert.Equal(t, "udp-hello", send(pc1, "udp-hello"))
	assert.Equal(t, "udp-again", send(pc1, "udp-again"))

	// 第二个对端: 独立会话同样可用
	pc2, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer pc2.Close()
	assert.Equal(t, "from-peer-2", send(pc2, "from-peer-2"))
}

// TestForwardDialFailure 静态映射目标不可达时连接被拒绝。
func TestForwardDialFailure(t *testing.T) {
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := dead.Addr().String()
	dead.Close()

	cp, _ := link(t)
	local := freePort(t)
	require.NoError(t, cp.StartForward([]ForwardRule{
		{Proto: ProtoTCP, Local: local, Remote: addr},
	}))
	c, err := net.Dial("tcp", local)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.SetDeadline(time.Now().Add(5*time.Second)))
	// 被控端拨号失败后通道关闭，本地连接随即断开
	buf := make([]byte, 16)
	_, err = c.Read(buf)
	assert.Error(t, err)
}

// TestDisabled 被控端禁用代理时，静态映射的目标连接随即被断开。
func TestDisabled(t *testing.T) {
	echo := startEcho(t)
	cp, ap := link(t)
	ap.enabled = false
	local := freePort(t)
	require.NoError(t, cp.StartForward([]ForwardRule{
		{Proto: ProtoTCP, Local: local, Remote: echo},
	}))
	c, err := net.Dial("tcp", local)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.SetDeadline(time.Now().Add(5*time.Second)))
	// 被控端拒绝建立通道，本地连接读失败
	buf := make([]byte, 16)
	_, err = c.Read(buf)
	assert.Error(t, err)
}

// TestClosePropagation 本地连接关闭后被控端通道随之清理。
func TestClosePropagation(t *testing.T) {
	echo := startEcho(t)
	cp, ap := link(t)
	local := freePort(t)
	require.NoError(t, cp.StartForward([]ForwardRule{
		{Proto: ProtoTCP, Local: local, Remote: echo},
	}))

	c, err := net.Dial("tcp", local)
	require.NoError(t, err)
	// 先完成一次往返确认通道建立
	payload := []byte("ping")
	_, err = c.Write(payload)
	require.NoError(t, err)
	require.NoError(t, c.SetDeadline(time.Now().Add(5*time.Second)))
	got := make([]byte, len(payload))
	_, err = io.ReadFull(c, got)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cp.Active() == 0 && len(ap.chans) == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("通道未随本地连接关闭而清理")
}

// TestParseForward 验证规则解析与格式化。
func TestParseForward(t *testing.T) {
	r, err := ParseForward("tcp:127.0.0.1:33890=192.168.1.8:3389")
	require.NoError(t, err)
	assert.Equal(t, ForwardRule{Proto: "tcp", Local: "127.0.0.1:33890", Remote: "192.168.1.8:3389"}, r)
	assert.Equal(t, "tcp:127.0.0.1:33890=192.168.1.8:3389", r.String())

	// 本地地址省略主机
	r, err = ParseForward("udp:5353=192.168.1.1:53")
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:5353", r.Local)

	_, err = ParseForward("tcp:127.0.0.1:33890") // 缺 =
	assert.Error(t, err)
	_, err = ParseForward("sctp:1.1.1.1:9=2.2.2.2:9") // 协议不支持
	assert.Error(t, err)
	_, err = ParseForward("tcp:127.0.0.1:0=192.168.1.8:3389") // 本地地址非法
	assert.Error(t, err)
	_, err = ParseForward("tcp:127.0.0.1:33890=192.168.1.8") // 远端缺端口
	assert.Error(t, err)
}

// TestFrameReader 验证 UDP 数据报帧的拆分(含跨块到达)。
func TestFrameReader(t *testing.T) {
	var fr frameReader
	a, b, c := []byte("aaa"), []byte("bbbbbbbbbb"), []byte("cc")
	var stream []byte
	stream = append(stream, udpFrame(a)...)
	stream = append(stream, udpFrame(b)...)
	stream = append(stream, udpFrame(c)...)

	// 一次给一半，再给剩余，验证跨块重组
	got := append([][]byte{}, fr.feed(stream[:5])...)
	got = append(got, fr.feed(stream[5:])...)
	require.Len(t, got, 3)
	assert.Equal(t, a, got[0])
	assert.Equal(t, b, got[1])
	assert.Equal(t, c, got[2])
}

// freePort 取一个空闲 TCP 端口(先占后放，测试内直接使用)。
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// freeUDP 取一个空闲 UDP 端口地址。
func freeUDP(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := pc.LocalAddr().String()
	pc.Close()
	return addr
}

// mustResolve 解析 UDP 地址。
func mustResolve(t *testing.T, addr string) *net.UDPAddr {
	t.Helper()
	ua, err := net.ResolveUDPAddr("udp", addr)
	require.NoError(t, err)
	return ua
}
