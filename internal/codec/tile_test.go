package codec

import (
	"bytes"
	"image/jpeg"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fillRect 在 BGRA 帧上填充颜色。
func fillRect(buf []byte, w, x, y, rw, h int, c byte) {
	for row := y; row < y+h; row++ {
		for col := x; col < x+rw; col++ {
			off := (row*w + col) * 4
			buf[off] = c
			buf[off+1] = c
			buf[off+2] = c
			buf[off+3] = 0
		}
	}
}

func TestDiffAndMarkClean(t *testing.T) {
	const w, h = 128, 64
	tr := New(w, h, 64)
	assert.Equal(t, 2, tr.Cols)
	assert.Equal(t, 1, tr.Rows)

	frame := make([]byte, w*h*4)
	fillRect(frame, w, 0, 0, w, h, 10)

	// 首帧无基准 => 全部为脏
	dirty := tr.Diff(frame, false)
	require.Len(t, dirty, 2)
	tr.MarkClean(frame, dirty)

	// 未变化 => 无脏块
	dirty = tr.Diff(frame, false)
	assert.Empty(t, dirty)

	// 修改第二块区域 => 只有索引 1 为脏
	fillRect(frame, w, 100, 10, 20, 20, 99)
	dirty = tr.Diff(frame, false)
	assert.Equal(t, []int{1}, dirty)

	// force => 全部为脏
	assert.Len(t, tr.Diff(frame, true), 2)
}

func TestEncodeTileJPEG(t *testing.T) {
	const w, h = 64, 64
	tr := New(w, h, 64)
	frame := make([]byte, w*h*4)
	fillRect(frame, w, 0, 0, w, h, 200)

	x, y, tw, th, data, err := tr.EncodeTile(frame, 0, 80)
	require.NoError(t, err)
	assert.Equal(t, 0, x)
	assert.Equal(t, 0, y)
	assert.Equal(t, 64, tw)
	assert.Equal(t, 64, th)

	img, err := jpeg.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, 64, img.Bounds().Dx())
	assert.Equal(t, 64, img.Bounds().Dy())
	// BGRA 填充值 200 对应 RGB(200,200,200)
	r, g, b, _ := img.At(32, 32).RGBA()
	assert.InDelta(t, r>>8, g>>8, 4)
	assert.InDelta(t, g>>8, b>>8, 4)
	assert.InDelta(t, 200, r>>8, 6)
}

func TestTileRectEdge(t *testing.T) {
	tr := New(100, 100, 64)      // 2x2 块，边缘块不满
	x, y, w, h := tr.TileRect(3) // 右下角
	assert.Equal(t, 64, x)
	assert.Equal(t, 64, y)
	assert.Equal(t, 36, w)
	assert.Equal(t, 36, h)
}
