// Package site 提供整套网站的 HTTP 处理逻辑。
//
// 设计要点：
//   - 只用 Go 标准库，没有任何第三方依赖，不需要 go mod download 就能编译；
//   - 模板和静态资源用 embed 打进二进制，所以部署时只需要一个文件；
//   - 同时被 main.go（独立服务器）和 api/index.go（Vercel 函数）复用。
package site

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

//go:embed templates/*.html public/*
var assets embed.FS

// 站点使用的时区：固定 UTC+8。中国不使用夏令时，写死偏移最省事，
// 也免得在精简容器里还去找 tzdata。
var cst = time.FixedZone("CST", 8*60*60)

// Server 实现了 http.Handler，可以被 http.Server 或 Vercel 直接调用。
type Server struct {
	started time.Time
	hits    atomic.Int64
	tpl     *template.Template
	handler http.Handler
	logger  *log.Logger
}

// New 构造网站处理器，并把内嵌资源挂到 /static/ 下。
func New() *Server {
	tpl, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		panic("解析模板失败：" + err.Error())
	}
	pub, err := fs.Sub(assets, "public")
	if err != nil {
		panic("读取内嵌静态资源失败：" + err.Error())
	}

	s := &Server{
		started: time.Now(),
		tpl:     tpl,
		logger:  log.New(os.Stdout, "[gosite] ", log.LstdFlags),
	}

	static := http.StripPrefix("/static/", cacheStatic(http.FileServer(http.FS(pub))))

	mux := http.NewServeMux()
	mux.Handle("/static/", static)
	mux.HandleFunc("/", s.handleHome) // "/" 是兜底路由，未匹配的路径都会进来
	mux.HandleFunc("/request", s.handleRequestInfo)
	mux.HandleFunc("/api/info", s.handleAPIInfo)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/echo", s.handleEcho)
	s.handler = mux

	return s
}

// ServeHTTP 是所有请求的入口：计数 → 安全响应头 → 路由 → 日志，并用 recover 兜住 panic。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	s.hits.Add(1)

	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	setSecurityHeaders(rec.Header())

	defer func() {
		if p := recover(); p != nil {
			s.logger.Printf("处理 %s %s 时 panic：%v", r.Method, r.URL.Path, p)
			if rec.status == http.StatusOK {
				http.Error(rec, "500 服务器内部错误", http.StatusInternalServerError)
			}
		}
		s.logger.Printf("%s %s -> %d %s ip=%s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond), clientIP(r))
	}()

	s.handler.ServeHTTP(rec, r)
}

// ---------- 页面 ----------

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.renderError(w, r, http.StatusNotFound, "页面不存在", "这个地址没有对应的页面，可能是链接写错了。")
		return
	}
	s.render(w, r, http.StatusOK, "home.html", "用 Go 写的动态网站")
}

func (s *Server) handleRequestInfo(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "request.html", "请求信息检测")
}

// ---------- JSON 接口 ----------

func (s *Server) handleAPIInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, infoResponse{
		Server:  s.serverInfo(),
		Request: collectReqInfo(r),
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"go_version":     runtime.Version(),
		"uptime_seconds": time.Since(s.started).Seconds(),
		"requests_total": s.hits.Load(),
		"server_time":    time.Now().In(cst).Format(time.RFC3339),
	})
}

// handleEcho 回显 POST 过来的 JSON，用来验证服务端真的在处理请求。
func (s *Server) handleEcho(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"ok":    false,
			"error": "这个接口只接受 POST 请求",
		})
		return
	}

	// 限制请求体大小，避免有人塞一个超大 JSON 把内存吃满。
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)

	var payload any
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":    false,
			"error": "请求体不是合法 JSON：" + err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"received":    payload,
		"server_time": time.Now().In(cst).Format(time.RFC3339),
		"note":        "上面的内容由 Go 服务端解析后回显，不是前端 JavaScript 拼出来的。",
	})
}

// ---------- 渲染与工具 ----------

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, tmpl, title string) {
	data := pageData{
		Title:  title,
		GoVer:  runtime.Version(),
		Now:    time.Now().In(cst).Format("2006-01-02 15:04:05"),
		Uptime: humanDuration(time.Since(s.started)),
		Hits:   s.hits.Load(),
		Year:   time.Now().In(cst).Year(),
		Region: s.region(),
		Host:   r.Host,
	}
	if tmpl == "request.html" {
		data.Req = collectReqInfo(r)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.tpl.ExecuteTemplate(w, tmpl, data); err != nil {
		// 此时响应头已经发出，只能记日志。
		s.logger.Printf("渲染模板 %s 失败：%v", tmpl, err)
	}
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, title, msg string) {
	data := pageData{
		Title:  title,
		GoVer:  runtime.Version(),
		Now:    time.Now().In(cst).Format("2006-01-02 15:04:05"),
		Hits:   s.hits.Load(),
		Year:   time.Now().In(cst).Year(),
		Region: s.region(),
		Host:   r.Host,
		ErrMsg: msg,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.tpl.ExecuteTemplate(w, "error.html", data); err != nil {
		s.logger.Printf("渲染错误页失败：%v", err)
	}
}

func (s *Server) serverInfo() serverInfo {
	return serverInfo{
		GoVersion:     runtime.Version(),
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		CPUCores:      runtime.NumCPU(),
		UptimeSeconds: time.Since(s.started).Seconds(),
		UptimeHuman:   humanDuration(time.Since(s.started)),
		StartedAt:     s.started.In(cst).Format(time.RFC3339),
		RequestsTotal: s.hits.Load(),
		Region:        s.region(),
		ServerTime:    time.Now().In(cst).Format(time.RFC3339),
	}
}

func (s *Server) region() string {
	if r := os.Getenv("VERCEL_REGION"); r != "" {
		return "Vercel · " + r
	}
	if host, err := os.Hostname(); err == nil {
		return "自建服务器 · " + host
	}
	return "未知"
}

// ---------- 请求信息 ----------

type headerKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type reqInfo struct {
	Method    string     `json:"method"`
	Path      string     `json:"path"`
	Query     string     `json:"query,omitempty"`
	Proto     string     `json:"proto"`
	Host      string     `json:"host"`
	ClientIP  string     `json:"client_ip"`
	UserAgent string     `json:"user_agent"`
	Referer   string     `json:"referer,omitempty"`
	Time      string     `json:"received_at"`
	Headers   []headerKV `json:"headers"`
}

type serverInfo struct {
	GoVersion     string  `json:"go_version"`
	GOOS          string  `json:"go_os"`
	GOARCH        string  `json:"go_arch"`
	CPUCores      int     `json:"cpu_cores"`
	UptimeSeconds float64 `json:"uptime_seconds"`
	UptimeHuman   string  `json:"uptime_human"`
	StartedAt     string  `json:"started_at"`
	RequestsTotal int64   `json:"requests_total"`
	Region        string  `json:"region"`
	ServerTime    string  `json:"server_time"`
}

type infoResponse struct {
	Server  serverInfo `json:"server"`
	Request reqInfo    `json:"request"`
}

// 这些头可能带凭据，不回显。
var sensitiveHeaders = map[string]bool{
	"cookie":              true,
	"authorization":       true,
	"proxy-authorization": true,
	"set-cookie":          true,
}

func collectReqInfo(r *http.Request) reqInfo {
	headers := make([]headerKV, 0, len(r.Header))
	for k, v := range r.Header {
		if sensitiveHeaders[strings.ToLower(k)] {
			continue
		}
		headers = append(headers, headerKV{Key: k, Value: strings.Join(v, ", ")})
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].Key < headers[j].Key })

	return reqInfo{
		Method:    r.Method,
		Path:      r.URL.Path,
		Query:     r.URL.RawQuery,
		Proto:     r.Proto,
		Host:      r.Host,
		ClientIP:  clientIP(r),
		UserAgent: r.UserAgent(),
		Referer:   r.Referer(),
		Time:      time.Now().In(cst).Format(time.RFC3339),
		Headers:   headers,
	}
}

// clientIP 依次从反向代理头里取真实 IP。Vercel、Nginx 都会设这些头。
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(xff)
	}
	if ip := r.Header.Get("X-Real-Ip"); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// ---------- 页面数据 ----------

type pageData struct {
	Title  string
	GoVer  string
	Now    string
	Uptime string
	Hits   int64
	Year   int
	Region string
	Host   string
	ErrMsg string
	Req    reqInfo
}

// ---------- 基础工具 ----------

// statusRecorder 记录实际写出的状态码，供访问日志使用。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func setSecurityHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	// 页面里没有内联脚本和样式，所以可以用比较严格的 CSP。
	h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'self'")
}

// cacheStatic 给静态资源加缓存头。
//
// 注意：这个头必须由函数自己设置。实测 Vercel 的 vercel.json headers 配置
// 不会覆盖 Serverless Function 显式写出的响应头，写在配置里是无效的。
// s-maxage 是给 Vercel 边缘节点（CDN）看的，max-age 是给浏览器看的。
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600, s-maxage=86400")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		log.Printf("写 JSON 失败：%v", err)
	}
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%d 毫秒", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1f 秒", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%d 分 %d 秒", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时 %d 分", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%d 天 %d 小时", int(d.Hours())/24, int(d.Hours())%24)
	}
}
