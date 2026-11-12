// Package config 提供 agent 端配置的加载、默认值与环境变量覆盖。
//
// 环境变量: LANRD_ADDR / LANRD_TOKEN / LANRD_FPS / LANRD_TILE /
// LANRD_QUALITY / LANRD_BLOCK_LOCAL / LANRD_PROXY，优先级高于 yaml。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// DefaultPath 默认配置文件路径(工作目录下)。
func DefaultPath() string { return filepath.Join("configs", "config.yaml") }

// Config agent 完整配置，与 configs/config.yaml 一一对应。
type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Capture CaptureConfig `yaml:"capture"`
	Input   InputConfig   `yaml:"input"`
	Proxy   ProxyConfig   `yaml:"proxy"`
}

// ServerConfig 网络服务配置。
type ServerConfig struct {
	Addr  string `yaml:"addr"`  // 监听地址，如 0.0.0.0:7080
	Token string `yaml:"token"` // 接入口令，为空表示不校验(仅建议内网使用)
}

// CaptureConfig 屏幕采集配置。
type CaptureConfig struct {
	FPS     int `yaml:"fps"`     // 采集帧率 5~60
	Tile    int `yaml:"tile"`    // 脏检测分块尺寸 32/64/128
	Quality int `yaml:"quality"` // JPEG 质量 30~95
}

// InputConfig 键鼠行为配置。
type InputConfig struct {
	BlockLocal bool `yaml:"blockLocal"` // 会话期间是否屏蔽远端物理键鼠
}

// ProxyConfig 内网代理配置(控制端经被控端访问其所在内网)。
type ProxyConfig struct {
	Enabled bool `yaml:"enabled"` // 是否允许控制端建立代理通道
}

// Default 返回默认配置。
func Default() *Config {
	return &Config{
		Server:  ServerConfig{Addr: "0.0.0.0:7080", Token: ""},
		Capture: CaptureConfig{FPS: 30, Tile: 64, Quality: 80},
		Input:   InputConfig{BlockLocal: false},
		Proxy:   ProxyConfig{Enabled: true},
	}
}

// Load 读取 yaml 配置文件(不存在时使用默认值)，再做环境变量覆盖与校验。
func Load(path string) (*Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if err == nil {
		if err := yaml.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("lanrd/config: 解析 %s 失败: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("lanrd/config: 读取 %s 失败: %w", path, err)
	}
	applyEnv(c)
	c.Normalize()
	return c, nil
}

// applyEnv 用环境变量覆盖配置项。
func applyEnv(c *Config) {
	if v := os.Getenv("LANRD_ADDR"); v != "" {
		c.Server.Addr = v
	}
	if v := os.Getenv("LANRD_TOKEN"); v != "" {
		c.Server.Token = v
	}
	if v := os.Getenv("LANRD_FPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Capture.FPS = n
		}
	}
	if v := os.Getenv("LANRD_TILE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Capture.Tile = n
		}
	}
	if v := os.Getenv("LANRD_QUALITY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Capture.Quality = n
		}
	}
	if v := os.Getenv("LANRD_BLOCK_LOCAL"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Input.BlockLocal = b
		}
	}
	if v := os.Getenv("LANRD_PROXY"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Proxy.Enabled = b
		}
	}
}

// Normalize 将非法配置值收敛到合法范围。
func (c *Config) Normalize() {
	if c.Server.Addr == "" {
		c.Server.Addr = "0.0.0.0:7080"
	}
	if c.Capture.FPS < 5 {
		c.Capture.FPS = 5
	}
	if c.Capture.FPS > 60 {
		c.Capture.FPS = 60
	}
	switch c.Capture.Tile {
	case 32, 64, 128:
	default:
		c.Capture.Tile = 64
	}
	if c.Capture.Quality < 30 {
		c.Capture.Quality = 30
	}
	if c.Capture.Quality > 95 {
		c.Capture.Quality = 95
	}
}
