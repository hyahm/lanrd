package proxy

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDirectTCP 直连端口映射 TCP 全链路回显。
func TestDirectTCP(t *testing.T) {
	target := startEcho(t)
	fwd, err := StartDirect([]ForwardRule{
		{Proto: ProtoTCP, Local: "127.0.0.1:0", Remote: target},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(fwd.Stop)

	c, err := net.Dial("tcp", fwd.Local(0))
	require.NoError(t, err)
	defer c.Close()

	msg := []byte("hello direct")
	_, err = c.Write(msg)
	require.NoError(t, err)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, len(msg))
	_, err = io.ReadFull(c, buf)
	require.NoError(t, err)
	assert.Equal(t, msg, buf)
}

// TestDirectUDP 直连端口映射 UDP 全链路回显。
func TestDirectUDP(t *testing.T) {
	target := startUDPEcho(t)
	fwd, err := StartDirect([]ForwardRule{
		{Proto: ProtoUDP, Local: "127.0.0.1:0", Remote: target},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(fwd.Stop)

	pc, err := net.Dial("udp", fwd.Local(0))
	require.NoError(t, err)
	defer pc.Close()

	msg := []byte("hello udp direct")
	_, err = pc.Write(msg)
	require.NoError(t, err)
	_ = pc.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, udpMaxDatagram)
	n, err := pc.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, msg, buf[:n])
}

// TestDirectMultiRules 多规则并行直连转发(不同端口映射到不同目标)。
func TestDirectMultiRules(t *testing.T) {
	t1 := startEcho(t)
	t2 := startEcho(t)
	fwd, err := StartDirect([]ForwardRule{
		{Proto: ProtoTCP, Local: "127.0.0.1:0", Remote: t1},
		{Proto: ProtoTCP, Local: "127.0.0.1:0", Remote: t2},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(fwd.Stop)
	assert.NotEqual(t, fwd.Local(0), fwd.Local(1))

	for _, addr := range []string{fwd.Local(0), fwd.Local(1)} {
		c, err := net.Dial("tcp", addr)
		require.NoError(t, err)
		msg := []byte("ping")
		_, err = c.Write(msg)
		require.NoError(t, err)
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, len(msg))
		_, err = io.ReadFull(c, buf)
		require.NoError(t, err)
		assert.Equal(t, msg, buf)
		_ = c.Close()
	}
}
