//go:build windows

package ui

import (
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// uiPath 统一界面配置保存位置(跟随工作目录)。
const uiPath = "configs/ui.yaml"

// legacyForwardPath 旧版规则文件(仅读取迁移，不再写入)。
const legacyForwardPath = "configs/forward.yaml"

// uiConnect 一份被控端连接信息(IP/端口/口令)。
type uiConnect struct {
	IP    string `yaml:"ip"`    // 被控端 IP
	Port  string `yaml:"port"`  // 被控端端口
	Token string `yaml:"token"` // 连接口令
}

// uiMap 单条端口映射: 本机端口 -> 远程地址，Enabled 控制
// 是否参与转发(未勾选仅存档)。
type uiMap struct {
	Local   int    `yaml:"local"`   // 本地监听端口(绑定 0.0.0.0，局域网可访问)
	Remote  string `yaml:"remote"`  // 远程目标 ip:port
	Enabled bool   `yaml:"enabled"` // 是否启用
}

// uiProxy 「内网代理」页配置: 映射列表。
type uiProxy struct {
	Maps []uiMap `yaml:"maps"`
}

// fwdRule 旧版多规则结构的单条(仅用于读取迁移)。
type fwdRule struct {
	Proto  string `yaml:"proto"`
	Local  int    `yaml:"local"`
	Remote string `yaml:"remote"`
}

// uiFile 统一界面配置文件结构。Map/Forwards 为旧版字段，仅读取迁移。
type uiFile struct {
	Connect  uiConnect `yaml:"connect"` // 「远程桌面」页连接
	Proxy    uiProxy   `yaml:"proxy"`   // 「内网代理」页映射列表
	Map      *uiMap    `yaml:"map,omitempty"`
	Forwards []fwdRule `yaml:"forwards,omitempty"`
}

// defaultMap 默认映射(本机 6000 -> 远程 192.168.1.61:3000)。
func defaultMap() uiMap {
	return uiMap{Local: 6000, Remote: "192.168.1.61:3000", Enabled: true}
}

// mapArg 转为 -fwd 命令行参数。绑定 0.0.0.0 使局域网内其他机器
// 可经本机 IP(如 192.168.60.129)的该端口访问映射。
func (m uiMap) mapArg() string {
	return "tcp:0.0.0.0:" + strconv.Itoa(m.Local) + "=" + m.Remote
}

// loadUIConfig 读取统一配置 ui.yaml，兼容迁移旧版单条 map 与旧版多规则
// forwards(默认启用)；任何缺失或损坏均回退默认值。
func loadUIConfig() *uiFile {
	c := &uiFile{
		Connect: uiConnect{Port: "7080"},
		Proxy:   uiProxy{Maps: []uiMap{defaultMap()}},
	}
	data, err := os.ReadFile(uiPath)
	if err != nil {
		if os.IsNotExist(err) {
			if r := firstLegacyRule(); r != nil {
				c.Proxy.Maps = []uiMap{*r}
			}
		}
		return c
	}
	var f uiFile
	if yaml.Unmarshal(data, &f) != nil {
		return c
	}
	switch {
	case len(f.Proxy.Maps) > 0: // 新格式: 映射列表
		c = &f
		c.Proxy.Maps = normalizeMaps(c.Proxy.Maps)
	case f.Map != nil && f.Map.Local > 0 && f.Map.Remote != "": // 旧格式: 单条
		c = &f
		c.Proxy.Maps = []uiMap{{Local: f.Map.Local, Remote: f.Map.Remote, Enabled: true}}
	case f.Map != nil && f.Map.Local > 0 && f.Map.Remote != "": // 更旧格式: 顶层单条
		c.Connect = f.Connect
		c.Proxy = uiProxy{Maps: []uiMap{{Local: f.Map.Local, Remote: f.Map.Remote, Enabled: true}}}
	}
	if len(c.Proxy.Maps) == 0 {
		c.Proxy.Maps = []uiMap{defaultMap()}
	}
	c.Map, c.Forwards = nil, nil
	return c
}

// normalizeMaps 过滤无效项(端口/地址缺失)。
func normalizeMaps(ms []uiMap) []uiMap {
	out := make([]uiMap, 0, len(ms))
	for _, m := range ms {
		if m.Local > 0 && m.Remote != "" {
			out = append(out, m)
		}
	}
	return out
}

// firstLegacyRule 读取旧版规则文件的第一条 tcp 映射(无则 nil)。
func firstLegacyRule() *uiMap {
	for _, path := range []string{legacyForwardPath, uiPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var f struct {
			Forwards []fwdRule `yaml:"forwards"`
		}
		if yaml.Unmarshal(data, &f) != nil {
			continue
		}
		for _, r := range f.Forwards {
			if r.Proto == "tcp" && r.Local > 0 && r.Remote != "" {
				return &uiMap{Local: r.Local, Remote: r.Remote, Enabled: true}
			}
		}
	}
	return nil
}

// saveUIConfig 把连接信息与映射列表保存到 ui.yaml(读取界面组件当前值)。
func (a *app) saveUIConfig() error {
	if err := os.MkdirAll("configs", 0o755); err != nil {
		return err
	}
	c := uiFile{
		Connect: uiConnect{
			IP:    a.ipIn.text,
			Port:  a.portIn.text,
			Token: a.tokenIn.text,
		},
		Proxy: uiProxy{Maps: normalizeMaps(a.maps)},
	}
	data, err := yaml.Marshal(&c)
	if err != nil {
		return err
	}
	return os.WriteFile(uiPath, data, 0o644)
}
