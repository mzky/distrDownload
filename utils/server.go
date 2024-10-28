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
	maxRetries   = 3               // 最大重试次数
	retryDelay   = 1 * time.Second // 重试间隔时间
)

// SplitAndSendTasks 服务端分段文件并发送任务
func (c *Config) SplitAndSendTasks() (chan JsonRes, error) {
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
	segmentSize := int64(4 * 1024 * 1024) // 4M

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

	taskQueue := make(chan JsonRes, jr.FileSize/segmentSize+1)
	go func() {
		for start := int64(0); start < jr.FileSize; start += segmentSize {
			end := start + segmentSize - 1
			if end >= jr.FileSize {
				end = jr.FileSize - 1
			}
			task := JsonRes{
				Url:      jr.Url,
				Start:    start,
				End:      end,
				Filename: jr.Filename,
				FileSize: jr.FileSize,
			}
			taskQueue <- task
		}
		close(taskQueue)
	}()

	return taskQueue, nil
}

// SendTaskToClient 发送任务给客户端
func (c *Config) SendTaskToClient(clientUrl string, task JsonRes, taskQueue chan JsonRes) {
	marshal, err := json.Marshal(task)
	if err != nil {
		log.Printf("任务序列化失败: %v", err)
		return
	}
	resp, err := http.Post(fmt.Sprintf("http://%s:%s/task", clientUrl, c.Addr),
		"application/json", bytes.NewBuffer(marshal))
	if err != nil {
		log.Fatalf("发送任务给客户端 %s 失败: %v", clientUrl, err)
	}
	defer resp.Body.Close()
	c.MonitorClientProgress(clientUrl, marshal, taskQueue)
}

// MonitorClientProgress 服务端探测客户端下载进度
func (c *Config) MonitorClientProgress(client string, marshal []byte, taskQueue chan JsonRes) {
	var wg sync.WaitGroup
	var mu sync.Mutex // 用于并发安全的打印
	beginTime := time.Now().Unix()
	wg.Add(1)
	go func(client string, c *Config, marshal []byte, taskQueue chan JsonRes) {
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
				if len(taskQueue) > 0 {
					newTask := <-taskQueue
					c.SendTaskToClient(client, newTask, taskQueue)
				}
				break
			}

			time.Sleep(pollInterval)
		}
	}(client, c, marshal, taskQueue)
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
	var jsonRes JsonRes
	if err := json.Unmarshal(marshal, &jsonRes); err != nil {
		log.Printf("任务反序列化失败: %v", err)
		return
	}

	// 创建文件
	fileName := fmt.Sprintf(SegmentFileName, jsonRes.Filename, jsonRes.Start, jsonRes.End)
	file, err := os.Create(fileName)
	if err != nil {
		log.Printf("创建文件失败: %v", err)
		return
	}
	defer file.Close()

	// 下载分段文件
	req, err := http.NewRequest("GET", jsonRes.Url, nil)
	if err != nil {
		log.Printf("创建请求失败: %v", err)
		return
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", jsonRes.Start, jsonRes.End))

	var resp *http.Response
	var retries int
	for retries = 0; retries < maxRetries; retries++ {
		resp, err = http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
		log.Printf("下载文件 %s 异常: %v, 重试次数: %d", jsonRes.Url, err, retries+1)
		time.Sleep(retryDelay)
	}

	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("下载文件 %s 失败, 达到最大重试次数: %d", jsonRes.Url, maxRetries)
		return
	}
	defer resp.Body.Close()

	// 将响应体写入文件
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		log.Printf("写入文件失败: %v", err)
		return
	}

	fmt.Printf("文件 %s 下载成功\n", fileName)
}

// MergeSegments 服务端合并分段文件
func (c *Config) MergeSegments() {
	if c.Url == "" {
		log.Fatal("未设置下载地址")
	}
	taskQueue, err := c.SplitAndSendTasks()
	if err != nil {
		log.Fatalf("文件分段异常: %v", err)
	}

	var wg sync.WaitGroup
	for _, clientUrl := range c.Clients {
		wg.Add(1)
		go func(url string, taskQueue chan JsonRes) {
			defer wg.Done()
			task := <-taskQueue
			c.SendTaskToClient(url, task, taskQueue)
		}(clientUrl, taskQueue)
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

	// 从任务队列中逐个处理任务
	for task := range taskQueue {
		segmentFilePath := fmt.Sprintf(SegmentFileName, task.Filename, task.Start, task.End)
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
