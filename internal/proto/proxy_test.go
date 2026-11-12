package proto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyOpenRoundTrip(t *testing.T) {
	body, err := EncodeProxyOpen(&ProxyOpen{ID: 42, Addr: "192.168.1.8:445"})
	require.NoError(t, err)
	p, err := DecodeProxyOpen(body)
	require.NoError(t, err)
	assert.EqualValues(t, 42, p.ID)
	assert.Equal(t, "192.168.1.8:445", p.Addr)
}

func TestProxyDataRoundTrip(t *testing.T) {
	b := EncodeProxyData(7, []byte{1, 2, 3})
	id, data, err := DecodeProxyData(b)
	require.NoError(t, err)
	assert.EqualValues(t, 7, id)
	assert.Equal(t, []byte{1, 2, 3}, data)

	// 零长度数据合法
	b = EncodeProxyData(7, nil)
	id, data, err = DecodeProxyData(b)
	require.NoError(t, err)
	assert.EqualValues(t, 7, id)
	assert.Empty(t, data)

	// 超短负载报错
	_, _, err = DecodeProxyData([]byte{1, 2, 3})
	assert.ErrorIs(t, err, ErrShortPayload)
}

func TestProxyCloseAckRoundTrip(t *testing.T) {
	id, err := DecodeProxyClose(EncodeProxyClose(99))
	require.NoError(t, err)
	assert.EqualValues(t, 99, id)
	_, err = DecodeProxyClose([]byte{})
	assert.ErrorIs(t, err, ErrShortPayload)

	id, ok, err := DecodeProxyAck(EncodeProxyAck(5, true))
	require.NoError(t, err)
	assert.EqualValues(t, 5, id)
	assert.True(t, ok)

	id, ok, err = DecodeProxyAck(EncodeProxyAck(6, false))
	require.NoError(t, err)
	assert.EqualValues(t, 6, id)
	assert.False(t, ok)

	_, _, err = DecodeProxyAck([]byte{0, 0, 0})
	assert.ErrorIs(t, err, ErrShortPayload)
}

func TestSplitHostPort(t *testing.T) {
	h, p, err := SplitHostPort("nas.local:8080")
	require.NoError(t, err)
	assert.Equal(t, "nas.local", h)
	assert.EqualValues(t, 8080, p)

	_, _, err = SplitHostPort("no-port")
	assert.Error(t, err)
	_, _, err = SplitHostPort("host:99999")
	assert.Error(t, err)
}
