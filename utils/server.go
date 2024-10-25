package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type JsonRes struct {
	Url         string
	Client      string
	Start       int64
	End         int64
	Filename    string
	FileSize    int64
	SegmentPath string
}

type ProgressRes struct {
	Status   string
	Progress string
	Size     string
}

const (
	pollInterval = 1 * time.Second
)

// SplitAndSendTasks 服务端分段文件并发送任务
func (c *Config) SplitAndSendTasks() (map[string]JsonRes, error) {
	clientCount := len(c.Clients)
	resp, err := http.Head(c.Url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var jr JsonRes
	fileSizeStr := resp.Header.Get("Content-Length")
	jr.FileSize, err = strconv.ParseInt(fileSizeStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("无法解析文件大小: %w", err)
	}
	segmentSize := jr.FileSize / int64(clientCount)
	if jr.FileSize%int64(clientCount) != 0 {
		segmentSize++
	}

	// 从响应头中获取 Content-Disposition
	_, params, _ := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	if len(params) > 0 && params["filename"] != "" {
		jr.Filename = params["filename"]
	}
	if jr.Filename == "" {
		jr.Filename = filepath.Base(c.Url)
	}
	c.FileName = jr.Filename
	jr.Url = c.Url

	tasks := make(map[string]JsonRes)
	for i := 0; i < clientCount; i++ {
		jr.Start = int64(i) * segmentSize
		jr.End = jr.Start + segmentSize - 1
		if i == clientCount-1 {
			jr.End = jr.FileSize - 1
		}
		tasks[c.Clients[i]] = jr
	}

	return tasks, nil
}

// SendTaskToClient 发送任务给客户端
func (c *Config) SendTaskToClient(clientUrl string, task JsonRes) {
	marshal, err := json.Marshal(task)
	if err != nil {
		log.Printf("任务序列化失败: %v", err)
		return
	}
	resp, err := http.Post(fmt.Sprintf("http://%s:%s/task", clientUrl, c.Addr),
		"application/json", bytes.NewBuffer(marshal))
	if err != nil {
		log.Printf("发送任务给客户端失败: %v", err)
		return
	}
	defer resp.Body.Close()
	c.MonitorClientProgress(clientUrl, marshal)
}

// MonitorClientProgress 服务端探测客户端下载进度
func (c *Config) MonitorClientProgress(client string, marshal []byte) {
	var wg sync.WaitGroup
	var mu sync.Mutex // 用于并发安全的打印
	beginTime := time.Now().Unix()
	wg.Add(1)
	go func(client string, c *Config, marshal []byte) {
		defer wg.Done()

		for {
			resp, err := http.Post(fmt.Sprintf("http://%s:%s/progress", client, c.Addr),
				"application/json", bytes.NewBuffer(marshal))
			if err != nil {
				mu.Lock()
				log.Printf("获取客户端下载信息失败 %s: %v", client, err)
				mu.Unlock()
				continue
			}
			defer resp.Body.Close()

			var result ProgressRes
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				mu.Lock()
				log.Printf("解析客户端 %s 的响应失败: %v", client, err)
				mu.Unlock()
				continue
			}

			mu.Lock()
			log.Printf("客户端 %s 下载进度: %#v", client, result)
			mu.Unlock()

			if result.Status == "done" {
				c.FetchSegmentsFromClients(client, marshal)
				break
			}

			time.Sleep(pollInterval)
		}
	}(client, c, marshal)
	wg.Wait()
	endTime := time.Now().Unix()
	t := endTime - beginTime
	var jsonRes JsonRes
	if err := json.Unmarshal(marshal, &jsonRes); err != nil {
		log.Printf("任务反序列化失败: %v", err)
		return
	}
	size := jsonRes.End - jsonRes.Start + 1
	log.Printf("总耗时: %v s,文件大小: %d Bytes,平均每秒 %d KB/s", t, size, (size/t)/1024)
}

// FetchSegmentsFromClients 服务端抓取客户端分段文件
func (c *Config) FetchSegmentsFromClients(client string, marshal []byte) {
	resp, err := http.Post(fmt.Sprintf("http://%s:%s/segment", client, c.Addr),
		"application/json", bytes.NewBuffer(marshal))
	if err != nil {
		log.Printf("请求客户端文件失败 %s: %v", client, err)
		return
	}
	defer resp.Body.Close()
	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("请求失败，状态码: %d\n", resp.StatusCode)
		return
	}

	var jsonRes JsonRes
	if err := json.Unmarshal(marshal, &jsonRes); err != nil {
		log.Printf("任务反序列化失败: %v", err)
		return
	}

	_, params, _ := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	fileName := params["filename"]
	if fileName == "" {
		fmt.Println("无法获取文件名")
		return
	}

	// 创建文件
	file, err := os.Create(fileName)
	if err != nil {
		fmt.Println("创建文件失败:", err)
		return
	}
	defer file.Close()

	// 将响应体写入文件
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		fmt.Println("写入文件失败:", err)
		return
	}

	fmt.Printf("文件 %s 下载成功\n", fileName)
}

// MergeSegments 服务端合并分段文件
func (c *Config) MergeSegments() {
	if c.Url == "" {
		log.Fatal("未设置下载地址")
	}
	tasks, err := c.SplitAndSendTasks()
	if err != nil {
		log.Fatalf("文件分段异常: %v", err)
	}
	var wg sync.WaitGroup
	for clientUrl, task := range tasks {
		wg.Add(1)
		go func(url string, t JsonRes) {
			defer wg.Done()
			c.SendTaskToClient(url, t)
		}(clientUrl, task)
	}

	// 检查文件是否存在，避免覆盖已有文件
	if _, err := os.Stat(c.FileName); !os.IsNotExist(err) {
		log.Fatalf("文件已存在: %s", c.FileName)
	}

	outputFile, err := os.Create(c.FileName)
	if err != nil {
		log.Fatalf("创建输出文件失败: %v", err)
	}
	defer outputFile.Close()
	// 等待 c.SendTaskToClient 执行完毕，再进行文件合并
	wg.Wait()
	for _, task := range tasks {
		segmentFilePath := fmt.Sprintf("%s_%d_%d", task.Filename, task.Start, task.End)
		segmentFile, err := os.Open(segmentFilePath)
		if err != nil {
			log.Fatalf("打开分段文件失败: %v", err)
		}
		defer segmentFile.Close()

		if _, err := io.Copy(outputFile, segmentFile); err != nil {
			log.Fatalf("写入分段文件失败: %v", err)
		}
	}

	log.Println("文件下载成功:", c.FileName)
}
