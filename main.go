package main

import (
	"distrDownload/utils"
	"fmt"
	"github.com/gookit/goutil/cflag"
	"log"
)

var opts = struct {
	server bool
	client bool
	addr   string
	url    string
}{}

func main() {
	c := cflag.New(func(c *cflag.CFlags) {
		c.Desc = "多终端联合下载"
		c.Version = "0.0.1"
	})
	c.BoolVar(&opts.server, "server", false, "启动下载服务;;s")
	c.BoolVar(&opts.client, "client", false, "启动分发客户端;;c")
	c.StringVar(&opts.addr, "port", "9999", "设置客户端监控端口;;p")
	c.StringVar(&opts.url, "url", "", "设置下载地址;;u")

	c.MustParse(nil)

	if opts.server && opts.client {
		log.Fatal("不能同时运行服务和客户端")
	}

	if opts.server {
		filePath := "https://go.p2hp.com/dl/go1.17.3.src.tar.gz"
		clients := []string{"127.0.0.1:8080", "192.168.0.99:8080"}

		tasks, err := utils.SplitAndSendTasks(filePath, len(clients), clients)
		if err != nil {
			log.Fatalf("Error splitting and sending tasks: %v", err)
		}

		for client, task := range tasks {
			if client != "filename" {
				if err := utils.SendTaskToClient(client, task); err != nil {
					log.Fatalf("Error sending task to %s: %v", client, err)
				}
			}
		}

		if err := utils.MonitorClientProgress(clients); err != nil {
			log.Fatalf("Error monitoring client progress: %v", err)
		}

		segments, err := utils.FetchSegmentsFromClients(clients)
		if err != nil {
			log.Fatalf("Error fetching segments from clients: %v", err)
		}

		if err := utils.MergeSegments(segments, tasks["filename"]); err != nil {
			log.Fatalf("Error merging segments: %v", err)
		}

		fmt.Printf("File successfully merged to %s\n", tasks["filename"])
	} else if opts.client {
		if opts.addr == "" {
			log.Fatal("Client address is required in client mode")
		}
		utils.ClientHandler(opts.addr)
	} else {
		log.Fatal("Please specify either -server or -client")
	}
}
