package utils

import (
	"github.com/BurntSushi/toml"
	"os"
	"strings"
)

type Config struct {
	Server  bool
	Client  bool
	Addr    string
	Url     string
	List    string
	Clients []string
}

var cfg = "config.toml"

func initConfig() *Config {
	if _, err := os.Stat(cfg); err != nil {
		create, _ := os.Create(cfg)
		create.Close()
	}
	file, err := os.ReadFile(cfg)
	if err != nil {
		return &Config{}
	}
	var config Config
	_ = toml.Unmarshal(file, &config)
	return &config
}
func (c *Config) LoadConfig() {
	config := initConfig()
	if c.Server {
		c.Server = config.Server
	}
	if c.Client {
		c.Client = config.Client
	}
	if c.List == "" {
		c.List = config.List
	}
	if c.Url == "" {
		c.Url = config.Url
	}
	if c.List != "" {
		c.Clients = strings.Split(c.List, ",")
	}
	marshal, err := toml.Marshal(c)
	if err != nil {
		return
	}
	_ = os.WriteFile(cfg, marshal, 0644)
}
