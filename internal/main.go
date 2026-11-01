package asterism

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// 运行配置
type Config struct {
	Token   string
	Sources []string
	Port    int
}

// 数据源项
type Source struct {
	Name       string
	Label      string
	Command    string
	InstallURL string
	Default    bool
	Parse      func() ([]*Package, error)
}

// 源状态
type State struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Count    int    `json:"count"`
	Gain     int    `json:"gain"`
	Loss     int    `json:"loss"`
	Span     int64  `json:"span"`
	OK       bool   `json:"ok"`
	Software int    `json:"software"`
	Solo     int    `json:"solo"`
	Merged   int    `json:"merged"`
}

// 运行状态
type Status struct {
	Build  string  `json:"build"`
	Source []State `json:"source"`
}

// 日志输出
var logWriter io.Writer = os.Stdout

// 数据目录
func OpenData() error {
	if err := os.MkdirAll("data/cache", 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile("data/asterism.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	logWriter = io.MultiWriter(os.Stdout, file)
	return nil
}

// 写入日志
func Log(format string, args ...any) {
	stamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(logWriter, "[%s]%s\n", stamp, fmt.Sprintf(format, args...))
}

// 获取失败
var ErrFailed = errors.New("failed")

// 启动服务
func Serve(port int, sources []Source) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/meta", func(writer http.ResponseWriter, request *http.Request) {
		stats, err := LoadSummary()
		if err != nil {
			http.Error(writer, "查询失败："+err.Error(), http.StatusInternalServerError)
			return
		}
		install := make([]map[string]string, 0, len(sources))
		for _, item := range sources {
			if item.Command != "" || item.InstallURL != "" {
				install = append(install, map[string]string{"source": item.Name, "label": item.Label})
			}
		}
		stats["install"] = install
		respond(writer, stats)
	})
	mux.HandleFunc("/api/entities", func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		limit, _ := strconv.Atoi(query.Get("limit"))
		if limit <= 0 || limit > 500 {
			limit = 120
		}
		offset, _ := strconv.Atoi(query.Get("offset"))
		if offset < 0 {
			offset = 0
		}
		minimum, _ := strconv.Atoi(query.Get("min"))
		if minimum < 1 {
			minimum = 1
		}
		items, total, err := QuerySoftware(
			strings.TrimSpace(query.Get("q")),
			query.Get("platforms"), query.Get("sort"), minimum, offset, limit)
		if err != nil {
			http.Error(writer, "查询失败："+err.Error(), http.StatusInternalServerError)
			return
		}
		respond(writer, map[string]any{"total": total, "items": items})
	})
	mux.HandleFunc("/api/entity", func(writer http.ResponseWriter, request *http.Request) {
		key := request.URL.Query().Get("key")
		if key == "" {
			http.Error(writer, "查询失败：缺少参数", http.StatusBadRequest)
			return
		}
		item, err := GetSoftware(key, sources)
		if err != nil {
			http.Error(writer, "查询失败："+err.Error(), http.StatusInternalServerError)
			return
		}
		if item == nil {
			http.Error(writer, "查询失败：软件不存在", http.StatusNotFound)
			return
		}
		respond(writer, item)
	})
	files := http.FileServer(http.Dir("public"))
	mux.Handle("/", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(writer, request)
	}))

	address := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	Log("服务：http://127.0.0.1:%d", port)
	return http.Serve(listener, mux)
}

// 输出结果
func respond(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	data, err := json.Marshal(value)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = writer.Write(data)
}
