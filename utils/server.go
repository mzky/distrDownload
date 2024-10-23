package utils

import (
	"encoding/json"
	"fmt"
	"io"
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
func SplitAndSendTasks(filePath string, clientCount int, clients []string) (map[string]string, error) {
	resp, err := http.Head(filePath)
	if err != nil {
		return nil, err
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
		filename = filepath.Clean(filePath)
	}

	tasks := make(map[string]string)
	tasks["filename"] = filename
	for i := 0; i < clientCount; i++ {
		start := int64(i) * segmentSize
		end := start + segmentSize - 1
		if i == clientCount-1 {
			end = fileSize - 1
		}
		tasks[clients[i]] = fmt.Sprintf("http://%s/download?path=%s&start=%d&end=%d", filePath, clients[i], start, end)
	}

	return tasks, nil
}

// SendTaskToClient 发送任务给客户端
func SendTaskToClient(client string, task string) error {
	resp, err := http.Post(fmt.Sprintf("http://%s/task", client), "application/json", strings.NewReader(task))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// MonitorClientProgress 服务端探测客户端下载进度
func MonitorClientProgress(clients []string) error {
	var wg sync.WaitGroup
	progress := make(map[string]bool)

	for _, client := range clients {
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
func FetchSegmentsFromClients(clients []string) ([]string, error) {
	segments := make([]string, len(clients))
	for i, client := range clients {
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
func MergeSegments(segments []string, outputPath string) error {
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outputFile.Close()

	for _, segmentPath := range segments {
		segmentFile, err := os.Open(segmentPath)
		if err != nil {
			return err
		}

		if _, err = io.Copy(outputFile, segmentFile); err != nil {
			return err
		}
		segmentFile.Close()
	}

	return nil
}
