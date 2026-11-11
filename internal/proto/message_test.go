package proto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteReadRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	body := EncodeMouseMove(1920, 1080)
	require.NoError(t, Write(&buf, MsgMouseMove, body))

	m, err := Read(&buf)
	require.NoError(t, err)
	assert.Equal(t, MsgMouseMove, m.Type)
	x, y, err := DecodeMouseMove(m.Body)
	require.NoError(t, err)
	assert.EqualValues(t, 1920, x)
	assert.EqualValues(t, 1080, y)
}

func TestReadTooLarge(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, Write(&buf, MsgTile, make([]byte, 16)))
	// 篡改长度字段为超限值
	raw := buf.Bytes()
	raw[1], raw[2], raw[3], raw[4] = 0xFF, 0xFF, 0xFF, 0x00
	_, err := Read(bytes.NewReader(raw))
	assert.Error(t, err)
}

func TestMouseBtn(t *testing.T) {
	b := EncodeMouseBtn(BtnRight, true, 100, 200)
	btn, down, x, y, err := DecodeMouseBtn(b)
	require.NoError(t, err)
	assert.Equal(t, BtnRight, btn)
	assert.True(t, down)
	assert.EqualValues(t, 100, x)
	assert.EqualValues(t, 200, y)
}

func TestWheelAndKey(t *testing.T) {
	d, x, y, err := DecodeWheel(EncodeWheel(-240, 5, 6))
	require.NoError(t, err)
	assert.EqualValues(t, -240, d)
	assert.EqualValues(t, 5, x)
	assert.EqualValues(t, 6, y)

	scan, down, err := DecodeKey(EncodeKey(0x1D|ScanExtended, false))
	require.NoError(t, err)
	assert.EqualValues(t, 0x1D|ScanExtended, scan)
	assert.False(t, down)
}

func TestTile(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xD9}
	b := EncodeTile(64, 128, 32, 16, jpeg)
	x, y, w, h, got, err := DecodeTile(b)
	require.NoError(t, err)
	assert.EqualValues(t, 64, x)
	assert.EqualValues(t, 128, y)
	assert.EqualValues(t, 32, w)
	assert.EqualValues(t, 16, h)
	assert.Equal(t, jpeg, got)
}

func TestStatusAndHello(t *testing.T) {
	last, since, locked, err := DecodeStatus(EncodeStatus(LastClient, 12345, true))
	require.NoError(t, err)
	assert.Equal(t, LastClient, last)
	assert.EqualValues(t, 12345, since)
	assert.True(t, locked)

	hb, err := EncodeHello(&Hello{Version: ProtocolVersion, Token: "t"})
	require.NoError(t, err)
	h, err := DecodeHello(hb)
	require.NoError(t, err)
	assert.Equal(t, ProtocolVersion, h.Version)
	assert.Equal(t, "t", h.Token)
}
