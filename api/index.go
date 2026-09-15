// Package handler 是 Vercel Serverless Function 的入口。
//
// Vercel 对 Go 的约定：
//   - 文件放在 api/ 目录下，包名必须是 handler
//   - 必须导出 Handler(w http.ResponseWriter, r *http.Request)
//   - 通过根目录的 vercel.json 把所有请求 rewrite 到这个函数
//
// 真正的业务逻辑在 gosite/internal/site 里，这样本地独立运行（main.go）
// 和 Vercel 上运行（这个文件）用的是同一套代码。
package handler

import (
	"net/http"

	"gosite/internal/site"
)

// 函数实例被复用时，这个 handler 只初始化一次，
// 所以「运行时长」和「请求数」是「本实例」的，不是全站的。
var app = site.New()

// Handler 是 Vercel 调用的入口。
func Handler(w http.ResponseWriter, r *http.Request) {
	app.ServeHTTP(w, r)
}
