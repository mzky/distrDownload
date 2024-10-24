package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SplitAndSendTasks 服务端分段文件并发送任务
func (c *Config) SplitAndSendTasks() (string, map[string]string, error) {
	clientCount := len(c.Clients)
	resp, err := http.Head(c.Url)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	fileSize, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	segmentSize := fileSize / int64(clientCount)
	if fileSize%int64(clientCount) != 0 {
		segmentSize++
	}

	// 从响应头中获取 Content-Disposition
	contentDisposition := resp.Header.Get("Content-Disposition")
	var filename string
	if contentDisposition != "" {
		if _, params, err := mime.ParseMediaType(contentDisposition); err == nil {
			filename = params["filename"]
		}
	} else {
		filename = filepath.Clean(c.Url)
	}

	tasks := make(map[string]string)
	for i := 0; i < clientCount; i++ {
		start := int64(i) * segmentSize
		end := start + segmentSize - 1
		if i == clientCount-1 {
			end = fileSize - 1
		}
		tasks[c.Clients[i]] = fmt.Sprintf("http://%s/download?path=%s&start=%d&end=%d", c.Url, c.Clients[i], start, end)
	}

	return filename, tasks, nil
}

// SendTaskToClient 发送任务给客户端
func SendTaskToClient(clientUrl, task string) error {
	resp, err := http.Post(fmt.Sprintf("http://%s/task", clientUrl), "application/json", strings.NewReader(task))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// MonitorClientProgress 服务端探测客户端下载进度
func (c *Config) MonitorClientProgress() error {
	var wg sync.WaitGroup
	progress := make(map[string]bool)

	for _, client := range c.Clients {
		wg.Add(1)
		go func(client string) {
			defer wg.Done()
			for {
				resp, err := http.Get(fmt.Sprintf("http://%s/progress", client))
				if err != nil {
					fmt.Printf("Error checking progress for %s: %v\n", client, err)
					continue
				}

				var clientProgress bool
				err = json.NewDecoder(resp.Body).Decode(&clientProgress)
				if err != nil {
					fmt.Printf("Error decoding progress for %s: %v\n", client, err)
					continue
				}

				progress[client] = clientProgress
				fmt.Printf("Client %s progress: %v\n", client, clientProgress)

				if clientProgress {
					break
				}
				resp.Body.Close()
				time.Sleep(1 * time.Second)
			}
		}(client)
	}

	wg.Wait()
	return nil
}

// FetchSegmentsFromClients 服务端抓取客户端分段文件
func (c *Config) FetchSegmentsFromClients() ([]string, error) {
	segments := make([]string, len(c.Clients))
	for i, client := range c.Clients {
		resp, err := http.Get(fmt.Sprintf("http://%s/segment", client))
		if err != nil {
			return nil, err
		}

		segmentPath := fmt.Sprintf("segment_%d", i)
		segmentFile, err := os.Create(segmentPath)
		if err != nil {
			return nil, err
		}

		_, err = io.Copy(segmentFile, resp.Body)
		if err != nil {
			return nil, err
		}

		segments[i] = segmentPath
		segmentFile.Close()
		resp.Body.Close()
	}
	return segments, nil
}

// MergeSegments 服务端合并分段文件
func (c *Config) MergeSegments() {
	outputPath, tasks, err := c.SplitAndSendTasks()
	if err != nil {
		log.Fatalf("Error splitting and sending tasks: %v", err)
	}

	for clientUrl, task := range tasks {
		if e := SendTaskToClient(clientUrl, task); e != nil {
			log.Fatalf("Error sending task to %s: %v", clientUrl, e)
		}
	}

	if e := c.MonitorClientProgress(); e != nil {
		log.Fatalf("Error monitoring client progress: %v", err)
	}

	segments, err := c.FetchSegmentsFromClients()
	if err != nil {
		log.Fatalf("Error fetching segments from clients: %v", err)
	}

	outputFile, err := os.Create(outputPath)
	if err != nil {
		log.Fatal(err)
	}
	defer outputFile.Close()

	for _, segmentPath := range segments {
		segmentFile, e := os.Open(segmentPath)
		if e != nil {
			log.Fatal(e)
		}

		if _, err = io.Copy(outputFile, segmentFile); err != nil {
			log.Fatal(err)
		}
		segmentFile.Close()
	}

	fmt.Println("文件下载成功:", outputFile)
}
