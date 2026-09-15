# 用 Docker 部署时用这个文件构建镜像（Vercel 不需要它）
#
# 构建：docker build -t gosite .
# 运行：docker run -d -p 8080:8080 --name gosite --restart unless-stopped gosite

# ---------- 构建阶段 ----------
FROM golang:1-alpine AS build
WORKDIR /src

# 本项目只用标准库，没有第三方依赖，所以不需要 go.sum
COPY go.mod ./
COPY . .

ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/gosite .

# ---------- 运行阶段 ----------
# 用 scratch（空镜像）就够了：静态编译的 Go 程序不需要任何运行库。
# 模板和 CSS 已经用 embed 打进二进制了。
FROM scratch
COPY --from=build /out/gosite /gosite

ENV PORT=8080
EXPOSE 8080
USER 65534:65534
ENTRYPOINT ["/gosite"]
