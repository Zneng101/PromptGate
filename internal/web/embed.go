package web

import "embed"

// all:static 嵌入 webui 构建产物（Vite 输出到 internal/web/static）。
// 占位文件保证未构建前端时也可编译。
//
//go:embed all:static
var staticFS embed.FS
