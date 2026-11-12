//go:build windows

// genicon 生成应用图标 assets/app.ico(多尺寸 PNG 压缩条目)，
// 供 rsrc 制作 .syso 嵌入 exe 资源。用法: go run ./tools/genicon
package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
	"path/filepath"

	"lanrd/internal/ui"
)

// sizes ICO 内包含的尺寸(覆盖资源管理器/任务栏/快捷方式各档)。
var sizes = []int{256, 48, 32, 16}

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

// run 生成 ICO 文件。
func run() error {
	if err := os.MkdirAll("assets", 0o755); err != nil {
		return err
	}
	var pngs [][]byte
	for _, s := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, ui.IconImage(s)); err != nil {
			return err
		}
		pngs = append(pngs, buf.Bytes())
	}
	f, err := os.Create(filepath.Join("assets", "app.ico"))
	if err != nil {
		return err
	}
	defer f.Close()

	// ICONDIR
	if err := binary.Write(f, binary.LittleEndian, uint16(0)); err != nil { // reserved
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint16(1)); err != nil { // type: icon
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint16(len(pngs))); err != nil {
		return err
	}
	// ICONDIRENTRY
	offset := uint32(6 + 16*len(pngs))
	for i, data := range pngs {
		s := sizes[i]
		dim := uint8(s)
		if s >= 256 {
			dim = 0 // 256 用 0 表示
		}
		f.Write([]byte{dim, dim, 0, 0, 1, 0, 32, 0}) // w,h,colors,res,planes,bpp
		if err := binary.Write(f, binary.LittleEndian, uint32(len(data))); err != nil {
			return err
		}
		if err := binary.Write(f, binary.LittleEndian, offset); err != nil {
			return err
		}
		offset += uint32(len(data))
	}
	// PNG 数据
	for _, data := range pngs {
		if _, err := f.Write(data); err != nil {
			return err
		}
	}
	return nil
}
