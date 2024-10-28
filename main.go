package main

import (
	"distrDownload/utils"
	"github.com/gookit/goutil/cflag"
	"log"
)

func main() {
	var opts utils.Config
	c := cflag.New(func(c *cflag.CFlags) {
		c.Desc = "多终端联合下载"
		c.Version = "0.0.1"
	})
	c.BoolVar(&opts.Server, "server", false, "启动下载服务;;s")
	c.BoolVar(&opts.Client, "client", false, "启动分发客户端;;c")
	c.StringVar(&opts.Addr, "port", "9999", "设置客户端监控端口;;p")
	c.StringVar(&opts.Url, "url", "", "设置下载文件地址;;u")
	c.StringVar(&opts.List, "list", "", "客户端列表,以逗号分隔;;l")
	c.StringVar(&opts.CfgFile, "cfgfile", "config.toml", "客户端列表,以逗号分隔;;f")
	c.StringVar(&opts.OutputPath, "outputPath", "./temp", "下载文件存放目录;;o")
	_ = c.Parse(nil)
	opts.LoadConfig()

	if opts.Server == opts.Client {
		log.Fatal("服务端和客户端需选择其中一种模式")
	}

	if opts.Client {
		opts.ClientHandler()
		return
	}

	// 服务端
	opts.MergeSegments()

}
