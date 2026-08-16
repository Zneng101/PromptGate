package web

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// staticExists 检查嵌入 FS 中是否存在某文件。
func staticExists(name string) bool {
	name = strings.TrimPrefix(name, "/")
	f, err := staticFS.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return !st.IsDir()
}

// serveFileFS 从嵌入 FS 提供单个文件，并设置合适的 Content-Type。
func serveFileFS(w http.ResponseWriter, r *http.Request, name string) {
	name = strings.TrimPrefix(name, "/")
	data, err := staticFS.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ext := path.Ext(name)
	switch ext {
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".ico":
		w.Header().Set("Content-Type", "image/x-icon")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

// subFS 返回嵌入 FS 的子树（用于 http.FileServer 风格，此处保留备用）。
func subFS(sub string) (fs.FS, error) {
	return fs.Sub(staticFS, sub)
}
