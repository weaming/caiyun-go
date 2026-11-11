package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	h5Base     = "https://h5.caiyunapp.com"
	weatherAPI = "https://api.caiyunapp.com"
	geoAPI     = "https://starplucker.cyapi.cn"

	// 服务端按 UA 绑定 ticket，必须与取票时一致
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36"

	ticketTTL = 30 * time.Minute

	dailySteps  = 16
	hourlySteps = 120
)

// Client 走 h5 站点的匿名链路：先取 ticket，再经同源 /api/ 网关转发上游请求。
// 它不保存任何用户位置——位置由每个请求自带，缓存交给上层按坐标分桶。
type Client struct {
	http     *http.Client
	cacheDir string
	ticket   string
}

func NewClient() (*Client, error) {
	dir := filepath.Join(os.Getenv("HOME"), ".cache", "caiyun")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建缓存目录失败: %w", err)
	}
	return &Client{
		http:     &http.Client{Timeout: 20 * time.Second},
		cacheDir: dir,
	}, nil
}

type ticketCache struct {
	Ticket    string  `json:"ticket"`
	UA        string  `json:"ua"`
	UpdatedAt float64 `json:"updated_at"`
}

// locationCache 是 python 版写下的默认位置文件，Go 只读不写
type locationCache struct {
	Lng  float64 `json:"lng"`
	Lat  float64 `json:"lat"`
	Name string  `json:"name"`
}

func (c *Client) path(name string) string { return filepath.Join(c.cacheDir, name) }

// ensureTicket 复用缓存里的 ticket，过期或换 UA 时重新取
func (c *Client) ensureTicket() (string, error) {
	if c.ticket != "" {
		return c.ticket, nil
	}

	var saved ticketCache
	if raw, err := os.ReadFile(c.path("ticket.json")); err == nil && json.Unmarshal(raw, &saved) == nil {
		if saved.Ticket != "" && saved.UA == userAgent && time.Since(time.Unix(int64(saved.UpdatedAt), 0)) < ticketTTL {
			c.ticket = saved.Ticket
			return c.ticket, nil
		}
	}
	return c.refreshTicket()
}

func (c *Client) refreshTicket() (string, error) {
	req, err := http.NewRequest(http.MethodGet, h5Base+"/h5", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("取 ticket 失败: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	for _, cookie := range resp.Cookies() {
		if cookie.Name != "ticket" || cookie.Value == "" {
			continue
		}
		c.ticket = cookie.Value
		raw, _ := json.MarshalIndent(ticketCache{Ticket: c.ticket, UA: userAgent, UpdatedAt: float64(time.Now().Unix())}, "", "  ")
		_ = os.WriteFile(c.path("ticket.json"), raw, 0o644)
		return c.ticket, nil
	}
	return "", fmt.Errorf("未取到 ticket，可能被风控或需要更新 UA")
}

var errUnauthorized = fmt.Errorf("auth failed")

// call 经同源 /api/ 网关转发；401 时续票重试一次
func (c *Client) call(rawURL string) ([]byte, error) {
	body, err := c.callOnce(rawURL)
	if err == errUnauthorized {
		c.ticket = ""
		_ = os.Remove(c.path("ticket.json"))
		if _, refreshErr := c.refreshTicket(); refreshErr != nil {
			return nil, refreshErr
		}
		return c.callOnce(rawURL)
	}
	return body, err
}

func (c *Client) callOnce(rawURL string) ([]byte, error) {
	ticket, err := c.ensureTicket()
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(map[string]string{"url": rawURL, "method": http.MethodGet})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, h5Base+"/api/", strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json;charset=utf-8")
	req.Header.Set("Referer", h5Base+"/h5")
	req.Header.Set("Cookie", "ticket="+ticket)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("网关请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("网关 %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}
	return body, nil
}

// Weather 一个坐标一次请求拿全量：realtime + hourly + daily + alert
func (c *Client) Weather(lng, lat float64) (*Payload, error) {
	begin := time.Now().Add(-24 * time.Hour).Unix()
	rawURL := fmt.Sprintf(
		"%s/v2.5/<t2.5>/%s,%s/weather?dailysteps=%d&hourlysteps=%d&alert=true&begin=%d",
		weatherAPI, trimFloat(lng), trimFloat(lat), dailySteps, hourlySteps, begin,
	)
	body, err := c.call(rawURL)
	if err != nil {
		return nil, err
	}

	var payload Payload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析天气响应失败: %w", err)
	}
	if payload.Status != "ok" {
		return nil, fmt.Errorf("接口返回异常: %s", strings.TrimSpace(string(body[:min(len(body), 200)])))
	}
	return &payload, nil
}

// reverseName 反查地名，失败时返回空串（前端会退回显示坐标）
func (c *Client) reverseName(lng, lat float64) string {
	rawURL := fmt.Sprintf("%s/v3/geo/reverse?latitude=%s&longitude=%s&lang=zh-CN", geoAPI, trimFloat(lat), trimFloat(lng))
	body, err := c.call(rawURL)
	if err != nil {
		return ""
	}
	var data struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	}
	if json.Unmarshal(body, &data) != nil {
		return ""
	}
	if data.Address != "" {
		return data.Address
	}
	return data.Name
}

// localLocation 只给 -once 用：读本机缓存的位置，没有就用深圳
func localLocation() (float64, float64, string) {
	var saved locationCache
	raw, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".cache", "caiyun", "location.json"))
	if err == nil && json.Unmarshal(raw, &saved) == nil && saved.Lng != 0 && saved.Lat != 0 {
		return saved.Lng, saved.Lat, saved.Name
	}
	return defaultLng, defaultLat, defaultName
}

func trimFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
