// Package handler 是 Vercel Serverless Function 的入口。
//
// Vercel 对 Go 的约定：
//   - 文件放在 api/ 目录下，包名必须是 handler
//   - 必须导出 Handler(w http.ResponseWriter, r *http.Request)
//   - 通过根目录的 vercel.json 把所有请求 rewrite 到这个函数
//
// 真正的业务逻辑在 gosite/site 里，这样本地独立运行（main.go）
// 和 Vercel 上运行（这个文件）用的是同一套代码。
//
// 注意：公共代码不能放在 internal/ 目录下。Vercel 会把本文件复制成
// main__vc__go__.go 并以「指定文件」的方式编译（go build main__vc__go__.go），
// 此时导入方的包名是 command-line-arguments，而 Go 规定 internal 包只能被
// 同一模块树内的包导入，会直接报「use of internal package ... not allowed」。
package handler

import (
	"net/http"

	"gosite/site"
)

// 函数实例被复用时，这个 handler 只初始化一次，
// 所以「运行时长」和「请求数」是「本实例」的，不是全站的。
var app = site.New()

// Handler 是 Vercel 调用的入口。
func Handler(w http.ResponseWriter, r *http.Request) {
	app.ServeHTTP(w, r)
}
