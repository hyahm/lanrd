//go:build windows

package agent

import (
	"log/slog"
	"net"
	"runtime"
	"sync"
	"time"

	"lanrd/internal/codec"
	"lanrd/internal/proto"
)

// pokeCapture 非阻塞唤醒抓屏循环，让输入后的画面反馈无需等待下一个采集节拍。
func (a *Agent) pokeCapture() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// captureLoop 抓屏循环：屏幕活动时按配置帧率采集；持续无变化时自动降频
// (8 帧无变化降到 10fps，长时间空闲降到 4fps)，避免 GDI BitBlt 空转吃满 CPU。
func (a *Agent) captureLoop() {
	base := time.Second / time.Duration(a.cfg.Capture.FPS)
	delay := base
	idle := 0
	for {
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-a.wake:
			idle = 0 // 输入到来，立即回到活动状态
			timer.Stop()
		case <-a.done:
			timer.Stop()
			return
		}
		if a.captureOnce() > 0 {
			idle = 0
			delay = base
			continue
		}
		idle++
		switch {
		case idle >= 60:
			delay = 250 * time.Millisecond // 深度空闲 4fps
		case idle >= 8:
			delay = 100 * time.Millisecond // 轻度空闲 10fps
		default:
			delay = base
		}
	}
}

// captureOnce 执行一次抓屏-检测-推送，返回成功推送的脏块数。
// 无会话时直接跳过，避免无观看者的空抓屏。
func (a *Agent) captureOnce() int {
	a.mu.Lock()
	s := a.cur
	a.mu.Unlock()
	if s == nil {
		return 0
	}
	a.capMu.Lock()
	buf, resized, err := a.cap.Capture()
	a.capMu.Unlock()
	if err != nil {
		a.log.Error("抓屏失败", "err", err)
		time.Sleep(time.Second)
		return 0
	}
	if resized {
		return a.onResize(s, buf)
	}
	if time.Since(s.lastKey) > keyframeInterval {
		s.tiler.Reset()
	}
	return a.pushTiles(s, buf)
}

// onResize 分辨率变化：重建 Tiler 并先发新的 HelloAck。
func (a *Agent) onResize(s *session, buf []byte) int {
	w, h := a.cap.Size()
	s.tiler = codec.New(w, h, a.cfg.Capture.Tile)
	s.lastKey = time.Now()
	body, _ := proto.EncodeHelloAck(a.ack())
	if !a.send(s, &proto.Message{Type: proto.MsgHelloAck, Body: body}) {
		return 0
	}
	a.log.Info("分辨率变化", "w", w, "h", h)
	return a.pushTiles(s, buf)
}

// tileResult 一个脏块的编码结果。
type tileResult struct {
	idx  int
	body []byte
}

// pushTiles 并行编码脏块并排队发送，返回成功推送的块数。
// 多核并行可把全屏关键帧的编码耗时压缩到 1/N。
func (a *Agent) pushTiles(s *session, buf []byte) int {
	dirty := s.tiler.Diff(buf, false)
	if len(dirty) == 0 {
		return 0
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > 8 {
		workers = 8
	}
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan int, len(dirty))
	results := make(chan tileResult, len(dirty))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				x, y, w, h, data, err := s.tiler.EncodeTile(buf, idx, a.cfg.Capture.Quality)
				if err != nil {
					continue
				}
				body := proto.EncodeTile(uint16(x), uint16(y), uint16(w), uint16(h), data)
				results <- tileResult{idx: idx, body: body}
			}
		}()
	}
	for _, idx := range dirty {
		jobs <- idx
	}
	close(jobs)
	go func() { wg.Wait(); close(results) }()

	sent := make([]int, 0, len(dirty))
	for r := range results {
		if a.send(s, &proto.Message{Type: proto.MsgTile, Body: r.body}) {
			sent = append(sent, r.idx)
		}
	}
	if len(sent) == len(dirty) {
		s.tiler.MarkClean(buf, sent)
		s.lastKey = time.Now()
	}
	return len(sent)
}

// statusLoop 周期广播抢占状态(远端物理用户 vs 控制端)。
func (a *Agent) statusLoop() {
	tick := time.NewTicker(statusInterval)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
		case <-a.done:
			return
		}
		a.pushStatus()
	}
}

// pushStatus 向当前会话广播抢占状态。
func (a *Agent) pushStatus() {
	a.mu.Lock()
	s := a.cur
	a.mu.Unlock()
	if s == nil {
		return
	}
	last, since := a.lastInput()
	body := proto.EncodeStatus(last, since, a.mon.Blocking())
	a.send(s, &proto.Message{Type: proto.MsgStatus, Body: body})
}

// lastInput 返回最近 10 秒内的输入来源及时间戳(毫秒)。
func (a *Agent) lastInput() (uint8, uint64) {
	loc := a.mon.LastLocal()
	cli := time.Unix(0, a.lastCli.Load())
	now := time.Now()
	if now.Sub(loc) > 10*time.Second && now.Sub(cli) > 10*time.Second {
		return proto.LastNone, 0
	}
	if loc.After(cli) {
		return proto.LastLocal, uint64(loc.UnixMilli())
	}
	return proto.LastClient, uint64(cli.UnixMilli())
}

// printLANIPs 打印本机内网 IPv4，方便对端连接。
func printLANIPs(log *slog.Logger) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil && !ipNet.IP.IsLoopback() {
			log.Info("内网地址", "ip", ipNet.IP.String())
		}
	}
}
