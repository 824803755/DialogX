// Command gosite 启动网站服务器。
//
// 本地运行：
//
//	go run .            然后浏览器打开 http://127.0.0.1:8080
//
// 换端口：
//
//	$env:PORT=9000; go run .
//
// 编译成可执行文件：
//
//	go build -o gosite.exe .
//
// 注意：部署到 Vercel 时不会用到这个文件，用的是 api/index.go。
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gosite/internal/site"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	if host := os.Getenv("HOST"); host != "" {
		addr = host + ":" + port
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           site.New(),
		ReadHeaderTimeout: 5 * time.Second, // 防止慢速请求占用连接
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// 在后台启动服务，主协程等待退出信号，这样可以优雅关闭。
	go func() {
		log.Printf("网站已启动： http://127.0.0.1:%s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("启动失败： %v", err)
		}
	}()

	// 等待 Ctrl+C 或系统终止信号
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("收到退出信号，正在关闭……")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("关闭时出错： %v", err)
	}
	log.Println("已退出")
}
