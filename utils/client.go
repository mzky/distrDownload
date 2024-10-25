package utils

import (
	"context"
	"fmt"
	"github.com/labstack/echo/v4"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
)

// ClientHandler 客户端 HTTP 处理器
func (c *Config) ClientHandler() {
	r := echo.New()
	var wg sync.WaitGroup

	r.POST("/task", func(e echo.Context) error {
		var jsonRes JsonRes
		if err := e.Bind(&jsonRes); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "无效的 JSON 数据"})
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.DownloadSegment(jsonRes)
		}()
		return e.NoContent(http.StatusOK)
	})

	r.POST("/progress", func(e echo.Context) error {
		var jsonRes JsonRes
		if err := e.Bind(&jsonRes); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "无效的 JSON 数据"})
		}
		// 根据下载的文件大小，对比源文件大小，判断是否下载完成
		var progressRes ProgressRes
		stat, err := os.Stat(fmt.Sprintf("%s_%d_%d", jsonRes.Filename, jsonRes.Start, jsonRes.End))
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		log.Printf("/progress 文件大小: %d end: %d\n", stat.Size(), jsonRes.End-jsonRes.Start+1)
		if stat.Size() == jsonRes.End-jsonRes.Start+1 {
			progressRes.Status = "done"
			progressRes.Progress = "100%"
			progressRes.Size = fmt.Sprintf("%d", jsonRes.FileSize)
			return e.JSON(http.StatusOK, progressRes)
		}
		// 获取文件下载的进度，百分比返回
		progressRes.Status = "downloading"
		progressRes.Progress = fmt.Sprintf("%0.2f%%", float64(stat.Size())/float64(jsonRes.End-jsonRes.Start+1)*100)
		progressRes.Size = fmt.Sprintf("%d", stat.Size())
		return e.JSON(http.StatusOK, progressRes)
	})

	r.POST("/segment", func(e echo.Context) error {
		var jsonRes JsonRes
		if err := e.Bind(&jsonRes); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "无效的 JSON 数据"})
		}
		segmentFile := fmt.Sprintf("%s_%d_%d", jsonRes.Filename, jsonRes.Start, jsonRes.End)
		e.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, segmentFile))
		return e.File(segmentFile)
	})

	fmt.Println("客户端已启动，监听地址为:", c.Addr)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-ctx.Done()
		wg.Wait()
	}()
	log.Fatal(r.Start(":" + c.Addr))
	cancel()
}

// DownloadSegment 客户端接收任务并下载分段文件
func (c *Config) DownloadSegment(jsonRes JsonRes) {
	req, err := http.NewRequest("GET", c.Url, nil)
	if err != nil {
		log.Printf("探测下载文件异常: %v", err)
		return
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", jsonRes.Start, jsonRes.End))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("下载文件 %s 异常: %v", c.Url, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		log.Printf("下载文件 %s 状态码错误: %d", c.Url, resp.StatusCode)
		return
	}

	jsonRes.SegmentPath = fmt.Sprintf("%s_%d_%d", jsonRes.Filename, jsonRes.Start, jsonRes.End)
	_ = os.Remove(jsonRes.SegmentPath)
	segmentFile, err := os.Create(jsonRes.SegmentPath)
	if err != nil {
		log.Printf("下载分段文件异常: %v", err)
		return
	}
	defer segmentFile.Close()

	if _, err = io.Copy(segmentFile, resp.Body); err != nil {
		log.Printf("保存下载文件异常: %v", err)
		return
	}
}
