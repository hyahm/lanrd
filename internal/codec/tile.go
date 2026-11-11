// Package codec 实现屏幕帧的分块脏检测与 JPEG 编码。
package codec

import (
	"bytes"
	"image"
	"image/jpeg"
	"sync"
)

// 复用编码中间缓冲，避免高频小分配加重 GC。
var (
	tileImgPool = sync.Pool{New: func() any { return new(image.RGBA) }}
	tileBufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}
)

// Tiler 维护上一帧副本，对 BGRA 帧做分块比较并编码脏块。
type Tiler struct {
	W, H, Tile int
	Cols, Rows int
	prev       []byte // 上一帧 BGRA，nil 表示尚未建立基准(全部视为脏)
}

// New 创建 Tiler。w/h 为帧尺寸，tile 为块边长。
func New(w, h, tile int) *Tiler {
	t := &Tiler{W: w, H: h, Tile: tile}
	t.Cols = (w + tile - 1) / tile
	t.Rows = (h + tile - 1) / tile
	return t
}

// Reset 丢弃基准帧，令下次 Diff 返回全部块(关键帧)。
func (t *Tiler) Reset() { t.prev = nil }

// TileRect 返回第 idx 块的坐标与尺寸。
func (t *Tiler) TileRect(idx int) (x, y, w, h int) {
	x = (idx % t.Cols) * t.Tile
	y = (idx / t.Cols) * t.Tile
	if w = t.Tile; x+w > t.W {
		w = t.W - x
	}
	if h = t.Tile; y+h > t.H {
		h = t.H - y
	}
	return
}

// Diff 比较当前帧与基准帧，返回脏块索引。force 为 true 时返回全部块。
func (t *Tiler) Diff(cur []byte, force bool) []int {
	if t.prev == nil || force {
		all := make([]int, t.Cols*t.Rows)
		for i := range all {
			all[i] = i
		}
		return all
	}
	var dirty []int
	for idx := 0; idx < t.Cols*t.Rows; idx++ {
		if t.tileDirty(cur, idx) {
			dirty = append(dirty, idx)
		}
	}
	return dirty
}

// tileDirty 逐行比较一个块是否发生变化。
func (t *Tiler) tileDirty(cur []byte, idx int) bool {
	x, y, w, h := t.TileRect(idx)
	stride := t.W * 4
	for row := 0; row < h; row++ {
		off := (y+row)*stride + x*4
		if !bytes.Equal(cur[off:off+w*4], t.prev[off:off+w*4]) {
			return true
		}
	}
	return false
}

// MarkClean 将 cur 中指定块写入基准帧，必须在对应脏块成功编码发送后调用。
func (t *Tiler) MarkClean(cur []byte, idxs []int) {
	if t.prev == nil {
		t.prev = make([]byte, len(cur))
	}
	stride := t.W * 4
	for _, idx := range idxs {
		x, y, w, h := t.TileRect(idx)
		for row := 0; row < h; row++ {
			off := (y+row)*stride + x*4
			copy(t.prev[off:off+w*4], cur[off:off+w*4])
		}
	}
}

// EncodeTile 将 BGRA 帧中的一个块编码为 JPEG，返回坐标尺寸与数据。
func (t *Tiler) EncodeTile(cur []byte, idx, quality int) (x, y, w, h int, data []byte, err error) {
	x, y, w, h = t.TileRect(idx)
	img := tileImgPool.Get().(*image.RGBA)
	need := w * h * 4
	var pix []byte
	if cap(img.Pix) >= need {
		pix = img.Pix[:need]
	} else {
		pix = make([]byte, need)
	}
	img.Pix, img.Stride, img.Rect = pix, w*4, image.Rect(0, 0, w, h)

	stride := t.W * 4
	for row := 0; row < h; row++ {
		src := cur[(y+row)*stride+x*4:]
		dst := img.Pix[row*img.Stride:]
		for i := 0; i < w; i++ {
			dst[i*4+0] = src[i*4+2] // R <- B位(BGRA)
			dst[i*4+1] = src[i*4+1] // G
			dst[i*4+2] = src[i*4+0] // B
			dst[i*4+3] = 0xFF
		}
	}

	buf := tileBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	err = jpeg.Encode(buf, img, &jpeg.Options{Quality: quality})
	tileImgPool.Put(img)
	if err != nil {
		tileBufPool.Put(buf)
		return 0, 0, 0, 0, nil, err
	}
	data = append([]byte(nil), buf.Bytes()...)
	tileBufPool.Put(buf)
	return x, y, w, h, data, nil
}
