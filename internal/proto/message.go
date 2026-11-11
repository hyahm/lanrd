// Package proto 定义控制端与被控端之间的二进制消息协议。
//
// 帧格式: 1 字节消息类型 + 4 字节负载长度(小端) + 负载。
// 详见 docs/protocol.md。
package proto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxPayload 单条消息负载上限。
const MaxPayload = 4 << 20

// ProtocolVersion 协议版本号，握手双方不一致时拒绝连接。
const ProtocolVersion = 1

// Type 消息类型。
type Type byte

// 消息类型定义。0x01~0x2F 为控制端->被控端，0x80~0xAF 为被控端->控制端。
const (
	MsgHello     Type = 0x01 // 握手(JSON Hello)
	MsgMouseMove Type = 0x10 // 鼠标移动
	MsgMouseBtn  Type = 0x11 // 鼠标按键
	MsgWheel     Type = 0x12 // 滚轮
	MsgKey       Type = 0x13 // 键盘按键(扫描码)
	MsgInputLock Type = 0x14 // 锁定/解锁远端物理键鼠
	MsgPing      Type = 0x2F // 心跳
	MsgHelloAck  Type = 0x81 // 握手应答/分辨率变化(JSON HelloAck)
	MsgTile      Type = 0x82 // 图像分块(JPEG)
	MsgPong      Type = 0x8F // 心跳应答
	MsgStatus    Type = 0x90 // 抢占状态广播
)

// 鼠标按键编号。
const (
	BtnLeft   uint8 = 0
	BtnRight  uint8 = 1
	BtnMiddle uint8 = 2
)

// 输入来源(抢占状态)。
const (
	LastNone   uint8 = 0
	LastLocal  uint8 = 1 // 远端机器前的物理用户
	LastClient uint8 = 2 // 远程控制端
)

// ErrShortPayload 负载长度不足。
var ErrShortPayload = errors.New("lanrd/proto: payload too short")

// Message 一条完整的协议消息。
type Message struct {
	Type Type
	Body []byte
}

// Hello 控制端握手信息。
type Hello struct {
	Version int    `json:"version"`
	Token   string `json:"token,omitempty"`
	Name    string `json:"name,omitempty"`
}

// HelloAck 被控端握手应答，分辨率变化时会重发以通知控制端重置画布。
type HelloAck struct {
	Version int    `json:"version"`
	Host    string `json:"host"`
	W       int    `json:"w"`
	H       int    `json:"h"`
	Tile    int    `json:"tile"`
}

// Write 将一条消息写入 w。
func Write(w io.Writer, t Type, body []byte) error {
	if len(body) > MaxPayload {
		return fmt.Errorf("lanrd/proto: payload too large: %d", len(body))
	}
	buf := make([]byte, 5+len(body))
	buf[0] = byte(t)
	binary.LittleEndian.PutUint32(buf[1:5], uint32(len(body)))
	copy(buf[5:], body)
	_, err := w.Write(buf)
	return err
}

// Read 从 r 读取一条完整消息。
func Read(r io.Reader) (*Message, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(hdr[1:5])
	if n > MaxPayload {
		return nil, fmt.Errorf("lanrd/proto: message too large: %d", n)
	}
	m := &Message{Type: Type(hdr[0])}
	if n > 0 {
		m.Body = make([]byte, n)
		if _, err := io.ReadFull(r, m.Body); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// EncodeHello 编码握手消息。
func EncodeHello(h *Hello) ([]byte, error) { return json.Marshal(h) }

// DecodeHello 解码握手消息。
func DecodeHello(b []byte) (*Hello, error) {
	h := &Hello{}
	if err := json.Unmarshal(b, h); err != nil {
		return nil, err
	}
	return h, nil
}

// EncodeHelloAck 编码握手应答。
func EncodeHelloAck(a *HelloAck) ([]byte, error) { return json.Marshal(a) }

// DecodeHelloAck 解码握手应答。
func DecodeHelloAck(b []byte) (*HelloAck, error) {
	a := &HelloAck{}
	if err := json.Unmarshal(b, a); err != nil {
		return nil, err
	}
	return a, nil
}

// EncodeMouseMove 编码鼠标移动: [u16 x][u16 y]，远端屏幕像素坐标。
func EncodeMouseMove(x, y uint16) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint16(b[0:2], x)
	binary.LittleEndian.PutUint16(b[2:4], y)
	return b
}

// DecodeMouseMove 解码鼠标移动。
func DecodeMouseMove(b []byte) (x, y uint16, err error) {
	if len(b) < 4 {
		return 0, 0, ErrShortPayload
	}
	return binary.LittleEndian.Uint16(b[0:2]), binary.LittleEndian.Uint16(b[2:4]), nil
}

// EncodeMouseBtn 编码鼠标按键: [u8 button][u8 down][u16 x][u16 y]。
func EncodeMouseBtn(btn uint8, down bool, x, y uint16) []byte {
	b := make([]byte, 6)
	b[0] = btn
	if down {
		b[1] = 1
	}
	binary.LittleEndian.PutUint16(b[2:4], x)
	binary.LittleEndian.PutUint16(b[4:6], y)
	return b
}

// DecodeMouseBtn 解码鼠标按键。
func DecodeMouseBtn(b []byte) (btn uint8, down bool, x, y uint16, err error) {
	if len(b) < 6 {
		return 0, false, 0, 0, ErrShortPayload
	}
	return b[0], b[1] == 1, binary.LittleEndian.Uint16(b[2:4]), binary.LittleEndian.Uint16(b[4:6]), nil
}

// EncodeWheel 编码滚轮: [i32 delta][u16 x][u16 y]，delta 单位为 1/120 槽位。
func EncodeWheel(delta int32, x, y uint16) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:4], uint32(delta))
	binary.LittleEndian.PutUint16(b[4:6], x)
	binary.LittleEndian.PutUint16(b[6:8], y)
	return b
}

// DecodeWheel 解码滚轮。
func DecodeWheel(b []byte) (delta int32, x, y uint16, err error) {
	if len(b) < 8 {
		return 0, 0, 0, ErrShortPayload
	}
	return int32(binary.LittleEndian.Uint32(b[0:4])),
		binary.LittleEndian.Uint16(b[4:6]), binary.LittleEndian.Uint16(b[6:8]), nil
}

// ScanExtended 扫描码扩展标志位(E0 前缀键)。
const ScanExtended uint16 = 0x0100

// EncodeKey 编码键盘事件: [u16 scancode|ScanExtended][u8 down]。
func EncodeKey(scan uint16, down bool) []byte {
	b := make([]byte, 3)
	binary.LittleEndian.PutUint16(b[0:2], scan)
	if down {
		b[2] = 1
	}
	return b
}

// DecodeKey 解码键盘事件。
func DecodeKey(b []byte) (scan uint16, down bool, err error) {
	if len(b) < 3 {
		return 0, false, ErrShortPayload
	}
	return binary.LittleEndian.Uint16(b[0:2]), b[2] == 1, nil
}

// EncodeInputLock 编码锁定远端键鼠开关: [u8 on]。
func EncodeInputLock(on bool) []byte {
	if on {
		return []byte{1}
	}
	return []byte{0}
}

// DecodeInputLock 解码锁定开关。
func DecodeInputLock(b []byte) (on bool, err error) {
	if len(b) < 1 {
		return false, ErrShortPayload
	}
	return b[0] == 1, nil
}

// EncodePing 编码心跳: [u64 发送时刻毫秒]。
func EncodePing(tsMs uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, tsMs)
	return b
}

// DecodePing 解码心跳时间戳。
func DecodePing(b []byte) (tsMs uint64, err error) {
	if len(b) < 8 {
		return 0, ErrShortPayload
	}
	return binary.LittleEndian.Uint64(b), nil
}

// EncodeTile 编码图像分块: [u16 x][u16 y][u16 w][u16 h][jpeg]。
func EncodeTile(x, y, w, h uint16, jpeg []byte) []byte {
	b := make([]byte, 8+len(jpeg))
	binary.LittleEndian.PutUint16(b[0:2], x)
	binary.LittleEndian.PutUint16(b[2:4], y)
	binary.LittleEndian.PutUint16(b[4:6], w)
	binary.LittleEndian.PutUint16(b[6:8], h)
	copy(b[8:], jpeg)
	return b
}

// DecodeTile 解码图像分块，返回坐标尺寸与 JPEG 数据。
func DecodeTile(b []byte) (x, y, w, h uint16, jpeg []byte, err error) {
	if len(b) < 8 {
		return 0, 0, 0, 0, nil, ErrShortPayload
	}
	return binary.LittleEndian.Uint16(b[0:2]), binary.LittleEndian.Uint16(b[2:4]),
		binary.LittleEndian.Uint16(b[4:6]), binary.LittleEndian.Uint16(b[6:8]), b[8:], nil
}

// EncodeStatus 编码抢占状态: [u8 lastInput][u64 sinceUnixMs][u8 locked]。
func EncodeStatus(last uint8, sinceMs uint64, locked bool) []byte {
	b := make([]byte, 10)
	b[0] = last
	binary.LittleEndian.PutUint64(b[1:9], sinceMs)
	if locked {
		b[9] = 1
	}
	return b
}

// DecodeStatus 解码抢占状态。
func DecodeStatus(b []byte) (last uint8, sinceMs uint64, locked bool, err error) {
	if len(b) < 10 {
		return 0, 0, false, ErrShortPayload
	}
	return b[0], binary.LittleEndian.Uint64(b[1:9]), b[9] == 1, nil
}
