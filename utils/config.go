package utils

import (
	"github.com/BurntSushi/toml"
	"os"
	"strings"
)

type Config struct {
	Server     bool
	Client     bool
	Addr       string
	Url        string
	List       string
	Clients    []string
	CfgFile    string
	FileName   string
	OutputPath string
}

var SegmentFileName = "./temp/%s_%d_%d"

func initConfig(c Config) Config {
	if _, err := os.Stat(c.CfgFile); err != nil {
		create, _ := os.Create(c.CfgFile)
		create.Close()
	}
	file, err := os.ReadFile(c.CfgFile)
	if err != nil {
		return c
	}
	var config Config
	_ = toml.Unmarshal(file, &config)
	return config
}

func (c *Config) LoadConfig() {
	config := initConfig(*c)
	if c.Addr == "" {
		c.Addr = config.Addr
	}
	if c.Url == "" && c.Client == false {
		c.Url = config.Url
	}
	if c.List == "" {
		c.List = config.List
	}
	if c.List != "" {
		c.Clients = strings.Split(c.List, ",")
	}
	marshal, err := toml.Marshal(c)
	if err != nil {
		return
	}
	_ = os.WriteFile(c.CfgFile, marshal, 0644)
}
