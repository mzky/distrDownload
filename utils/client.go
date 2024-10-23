package utils

import (
	"fmt"
	"github.com/labstack/echo/v4"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
)

// ClientHandler 客户端 HTTP 处理器
func ClientHandler(clientAddr string) {
	e := echo.New()
	e.POST("/task", func(c echo.Context) error {
		body, _ := io.ReadAll(c.Request().Body)
		if err := downloadSegment(string(body)); err != nil {
			log.Printf("Error downloading segment: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return c.NoContent(http.StatusOK)
	})

	e.GET("/progress", func(c echo.Context) error {
		// 假设下载完成后设置进度为 true
		progress := true
		return c.JSON(http.StatusOK, progress)
	})

	e.GET("/segment", func(c echo.Context) error {
		segmentPath := "segment_0_0" // 假设只有一个分段文件
		segmentFile, err := os.Open(segmentPath)
		if err != nil {
			log.Printf("Error opening segment file: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		defer segmentFile.Close()
		return c.Stream(http.StatusOK, "application/octet-stream", segmentFile)
	})
	if err := e.Start(clientAddr); err != nil {
		log.Fatalf("Error starting client: %v", err)
	}
}

// 客户端接收任务并下载分段文件
func downloadSegment(task string) error {
	u, err := url.Parse(task)
	if err != nil {
		return err
	}
	query := u.Query()
	start, _ := strconv.ParseInt(query.Get("start"), 10, 64)
	end, _ := strconv.ParseInt(query.Get("end"), 10, 64)
	urlPath := query.Get("path")

	req, err := http.NewRequest("GET", urlPath, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	segmentPath := fmt.Sprintf("segment_%d_%d", start, end)
	segmentFile, err := os.Create(segmentPath)
	if err != nil {
		return err
	}
	defer segmentFile.Close()

	if _, err = io.Copy(segmentFile, resp.Body); err != nil {
		return err
	}

	return nil
}
