package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"log"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 没带坐标时统一回落到深圳（多用户场景下服务器不保存任何人的位置）
const (
	defaultLng  = 114.0579
	defaultLat  = 22.5431
	defaultName = "广东省 深圳市 福田区"
)

// cacheTTL 同时用于服务端报告缓存、前端 localStorage 缓存和 HTTP Cache-Control
const cacheTTL = 10 * time.Minute

// gridStep 是坐标取整粒度：0.01° ≈ 1.1km，天气数据在这个尺度上几乎不变，
// 把所有坐标先吸附到网格再算缓存 key，用户小幅移动就不会把缓存打穿
const gridStep = 0.01

// 报告范围：小时固定 12，天数可由 -days / ?days= 覆盖（上游最多 16 天）
const (
	reportHours  = 12
	defaultDays  = 7
	maxReportDay = 16
)

//go:embed page.html
var htmlPage string

//go:embed favicon.svg
var faviconSVG []byte

// 与 https://bitsflow.org/css/style.css 的 code/pre 字体栈相同，只把 "Maple Mono NF CN"
// 挪到中文字体之后：Maple 的拉丁进宽是 0.6em、中文 1.2em，和 Iosevka 的 0.5em 不匹配
// （实测中文占 2.4 格），表格空格补齐就对不齐；交给 PingFang 等 1em 的中文字体正好 2 格。
const fontStack = `"Iosevka Web Code", "Menlo", "Monaco", "Apple Color Emoji",
    "Noto Sans SC", "InputMono", "SFMono-Regular", "Consolas",
    "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei",
    "Maple Mono NF CN", "Liberation Mono", monospace`

// entry 是带过期时间的缓存项，用于多用户共享的文本/地名缓存
type entry[T any] struct {
	value  T
	expire time.Time
}

type ttlMap[T any] struct {
	mu    sync.Mutex
	ttl   time.Duration
	limit int
	items map[string]entry[T]
}

func newTTLMap[T any](ttl time.Duration, limit int) *ttlMap[T] {
	return &ttlMap[T]{ttl: ttl, limit: limit, items: make(map[string]entry[T])}
}

func (m *ttlMap[T]) get(key string) (T, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[key]
	if !ok || time.Now().After(item.expire) {
		var zero T
		return zero, false
	}
	return item.value, true
}

func (m *ttlMap[T]) set(key string, value T) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.items) >= m.limit {
		for existing := range m.items {
			delete(m.items, existing)
			break
		}
	}
	m.items[key] = entry[T]{value: value, expire: time.Now().Add(m.ttl)}
}

type report struct {
	text  string
	cells int
	name  string
}

// flight 让同一坐标的并发请求只打一次上游
type flight struct {
	mu sync.Mutex
	m  map[string]*flightCall
}

type flightCall struct {
	wg    sync.WaitGroup
	value report
	err   error
}

func (f *flight) do(key string, fn func() (report, error)) (report, error) {
	f.mu.Lock()
	if f.m == nil {
		f.m = make(map[string]*flightCall)
	}
	if existing, ok := f.m[key]; ok {
		f.mu.Unlock()
		existing.wg.Wait()
		return existing.value, existing.err
	}
	current := &flightCall{}
	current.wg.Add(1)
	f.m[key] = current
	f.mu.Unlock()

	current.value, current.err = fn()
	current.wg.Done()

	f.mu.Lock()
	delete(f.m, key)
	f.mu.Unlock()
	return current.value, current.err
}

type server struct {
	client      *Client
	texts       *ttlMap[report]
	names       *ttlMap[string]
	group       flight
	defaultDays int
}

func newServer(client *Client, days int) *server {
	return &server{
		client:      client,
		texts:       newTTLMap[report](cacheTTL, 256),
		names:       newTTLMap[string](6*time.Hour, 512),
		defaultDays: clampDays(days),
	}
}

// clampDays 把天数收进 1~16
func clampDays(days int) int {
	if days < 1 {
		return 1
	}
	if days > maxReportDay {
		return maxReportDay
	}
	return days
}

// days 取请求里的 ?days=，缺失或非法时用服务端默认值
func (s *server) days(r *http.Request) int {
	raw := r.URL.Query().Get("days")
	if raw == "" {
		return s.defaultDays
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return s.defaultDays
	}
	return clampDays(value)
}

func main() {
	addr := flag.String("addr", ":1314", "监听地址")
	once := flag.Bool("once", false, "只打印报告后退出")
	days := flag.Int("days", defaultDays, "逐日/生活指数天数 1~16")
	tlsCrt := flag.String("tls-crt", "", "TLS 证书路径，与 -tls-key 一起给出才启用 HTTPS")
	tlsKey := flag.String("tls-key", "", "TLS 私钥路径")
	httpAddr := flag.String("http-addr", "", "额外的 HTTP 监听地址，仅 TLS 模式下可用，请求全部 308 跳转到 HTTPS")
	flag.Parse()

	if (*tlsCrt == "") != (*tlsKey == "") {
		log.Fatal("-tls-crt 与 -tls-key 必须同时给出")
	}
	if *httpAddr != "" && *tlsCrt == "" {
		log.Fatal("-http-addr 需要配合 -tls-crt / -tls-key 使用")
	}

	client, err := NewClient()
	if err != nil {
		log.Fatal(err)
	}

	if *once {
		lng, lat, name := localLocation()
		text, err := buildReport(client, lng, lat, name, clampDays(*days))
		if err != nil {
			log.Fatal(err)
		}
		fmt.Print(text)
		return
	}

	app := newServer(client, *days)
	http.HandleFunc("/", app.handlePage)
	http.HandleFunc("/text", app.handleText)
	http.HandleFunc("/geo", app.handleGeo)
	http.HandleFunc("/favicon.svg", handleFavicon)
	http.HandleFunc("/favicon.ico", handleFavicon)

	srv := &http.Server{Addr: *addr, ReadHeaderTimeout: 5 * time.Second}
	if *tlsCrt != "" {
		if *httpAddr != "" {
			go serveRedirect(*httpAddr, *addr)
		}
		log.Printf("caiyun 服务启动: https://localhost%s/", *addr)
		log.Fatal(srv.ListenAndServeTLS(*tlsCrt, *tlsKey))
	}
	log.Printf("caiyun 服务启动: http://localhost%s/", *addr)
	log.Fatal(srv.ListenAndServe())
}

// locNote 给出定位可用性的初始提示：非安全上下文（HTTP + 非 localhost）Chrome 会禁止定位
func locNote(r *http.Request) string {
	if r.TLS != nil {
		return ""
	}
	host := r.Host
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	switch host {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return ""
	}
	return " · 定位需 HTTPS"
}

// handleFavicon 直接吐内嵌的 svg（.ico 也回同一份，省一次 404）
func handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	w.Write(faviconSVG)
}

// serveRedirect 在独立的 HTTP 端口上把请求 308 跳到同一主机的 HTTPS
func serveRedirect(httpAddr, tlsAddr string) {
	httpsPort := portOf(tlsAddr)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if name, _, err := net.SplitHostPort(r.Host); err == nil {
			host = name
		}
		target := "https://" + host
		if httpsPort != "" && httpsPort != "443" {
			target += ":" + httpsPort
		}
		http.Redirect(w, r, target+r.URL.RequestURI(), http.StatusPermanentRedirect)
	})

	log.Printf("HTTP 跳转已启用: http://localhost%s/ -> https://localhost%s/", httpAddr, tlsAddr)
	srv := &http.Server{Addr: httpAddr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Printf("HTTP 跳转服务退出: %v", err)
	}
}

// portOf 从 ":1314" / "127.0.0.1:1314" 里取端口
func portOf(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return port
	}
	return ""
}

// coords 取请求里的 lng/lat，缺失或非法时回落到深圳
func coords(r *http.Request) (float64, float64, bool) {
	query := r.URL.Query()
	lng, errLng := strconv.ParseFloat(query.Get("lng"), 64)
	lat, errLat := strconv.ParseFloat(query.Get("lat"), 64)
	if errLng != nil || errLat != nil || lng == 0 || lat == 0 {
		return snapCoord(defaultLng), snapCoord(defaultLat), false
	}
	return snapCoord(lng), snapCoord(lat), true
}

// snapCoord 把坐标吸到 0.01° 网格上，保证缓存 key 与展示坐标都稳定。
// 先乘后除（而不是乘 0.01），否则会得到 114.49000000000001 这类浮点噪声
func snapCoord(value float64) float64 {
	return math.Round(value/gridStep) / (1 / gridStep)
}

// coordKey 是报告/地名缓存的 key：网格坐标 + 天数
func coordKey(lng, lat float64) string {
	return fmt.Sprintf("%.2f,%.2f", snapCoord(lng), snapCoord(lat))
}

func (s *server) report(r *http.Request) (report, error) {
	lng, lat, custom := coords(r)
	days := s.days(r)
	key := fmt.Sprintf("%s,%d", coordKey(lng, lat), days)
	if cached, ok := s.texts.get(key); ok {
		return cached, nil
	}

	return s.group.do(key, func() (report, error) {
		if cached, ok := s.texts.get(key); ok {
			return cached, nil
		}
		payload, err := s.client.Weather(lng, lat)
		if err != nil {
			return report{}, err
		}
		name := s.name(lng, lat, custom)
		text, err := RenderReport(payload, name, lng, lat, reportHours, days)
		if err != nil {
			return report{}, err
		}
		result := report{text: text, cells: maxLineCells(text), name: name}
		s.texts.set(key, result)
		return result, nil
	})
}

// name 反查地名；默认位置直接用常量，避免每次请求都打上游
func (s *server) name(lng, lat float64, custom bool) string {
	if !custom {
		return defaultName
	}
	key := coordKey(lng, lat)
	if cached, ok := s.names.get(key); ok {
		return cached
	}
	name := s.client.reverseName(lng, lat)
	s.names.set(key, name)
	return name
}

func (s *server) handlePage(w http.ResponseWriter, r *http.Request) {
	item, err := s.report(r)
	if err != nil {
		http.Error(w, "错误: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Cells", strconv.Itoa(item.cells))
	w.Write([]byte(renderHTML(item, s.days(r), locNote(r))))
}

func (s *server) handleText(w http.ResponseWriter, r *http.Request) {
	item, err := s.report(r)
	if err != nil {
		http.Error(w, "错误: "+err.Error(), http.StatusBadGateway)
		return
	}
	tag := etagOf(item.text)
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d", int(cacheTTL.Seconds())))
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Cells", strconv.Itoa(item.cells))
	fmt.Fprint(w, item.text)
}

func (s *server) handleGeo(w http.ResponseWriter, r *http.Request) {
	lng, lat, custom := coords(r)
	body, _ := json.Marshal(map[string]string{"name": s.name(lng, lat, custom)})
	tag := etagOf(string(body))
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(body)
}

// etagOf 用内容 hash 做 ETag，浏览器可带 If-None-Match 换 304
func etagOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

// renderHTML 用占位符替换而不是 printf，避免正文里的 % 被当成格式化动词
func renderHTML(item report, days int, note string) string {
	text := item.text
	cells := item.cells
	pairs := []string{
		"{{FONT_STACK}}", fontStack,
		"{{CELLS}}", strconv.Itoa(cells),
		"{{HALF_CELLS}}", strconv.FormatFloat(float64(cells)/2, 'f', 1, 64),
		"{{BODY}}", html.EscapeString(text),
		"{{LOCATION}}", html.EscapeString(item.name),
		"{{DEFAULT_NAME_JSON}}", strconv.Quote(defaultName),
		"{{DEFAULT_LNG}}", strconv.FormatFloat(defaultLng, 'f', -1, 64),
		"{{DEFAULT_LAT}}", strconv.FormatFloat(defaultLat, 'f', -1, 64),
		"{{TTL_MS}}", strconv.FormatInt(cacheTTL.Milliseconds(), 10),
		"{{DAYS}}", strconv.Itoa(days),
		"{{LOC_NOTE}}", html.EscapeString(note),
	}
	return strings.NewReplacer(pairs...).Replace(htmlPage)
}

func buildReport(client *Client, lng, lat float64, name string, days int) (string, error) {
	payload, err := client.Weather(lng, lat)
	if err != nil {
		return "", err
	}
	return RenderReport(payload, name, lng, lat, reportHours, days)
}
