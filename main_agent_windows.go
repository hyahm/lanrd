//go:build windows

package main

import (
	"flag"
	"log/slog"
	"os"

	"lanrd/internal/agent"
	"lanrd/internal/config"
)

// runAgent 启动被控端。
func runAgent(args []string) {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	cfgPath := fs.String("config", config.DefaultPath(), "配置文件路径")
	_ = fs.Parse(args)

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Error("加载配置失败", "err", err)
		os.Exit(1)
	}
	a, err := agent.New(cfg, log)
	if err != nil {
		log.Error("初始化被控端失败", "err", err)
		os.Exit(1)
	}
	defer a.Close()
	if err := a.Run(); err != nil {
		log.Error("被控端退出", "err", err)
		os.Exit(1)
	}
}
