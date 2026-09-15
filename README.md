# 星野 Go 站

一个只用 **Go 标准库**写成的动态网站：服务端渲染 HTML、内嵌静态资源、自带 JSON 接口。
既可独立运行，也可直接作为 **Vercel Serverless Function** 部署。

## 它有什么

| 路由 | 说明 |
| --- | --- |
| `GET /` | 首页，每次请求都由 Go 实时渲染（服务器时间、运行时长、请求数都是算出来的） |
| `GET /request` | 请求信息检测：把你的 IP、UA、请求头整理成表格（Cookie / Authorization 已过滤） |
| `GET /api/info` | 上面两部分数据的 JSON 版本 |
| `GET /api/health` | 健康检查，适合接 Uptime 监控 |
| `POST /api/echo` | 解析并回显 JSON，用来验证后端真的在处理请求 |
| `/static/*` | 内嵌的 CSS 与图标，由 Go 直接返回 |

技术上用到的：`net/http`（路由与中间件）、`html/template`（服务端渲染、自动转义防 XSS）、
`embed`（模板和 CSS 打进二进制）、`encoding/json`、`log` + `recover`（访问日志与 panic 兜底）、
以及优雅关闭（`signal` + `srv.Shutdown`）。

**零第三方依赖**：没有 go.sum，克隆下来直接 `go build` 就能编译。

## 目录结构

```
goserver/
├─ main.go                 # 独立服务器的入口（本地运行 / 自己的服务器用这个）
├─ api/index.go            # Vercel Serverless Function 入口（Vercel 用这个）
├─ site/
│  ├─ site.go              # 全部业务逻辑：路由、渲染、JSON 接口、日志
│  ├─ templates/           # HTML 模板（head/foot 在 partials.html）
│  └─ public/              # CSS、favicon（被 embed 打进二进制）
├─ public/robots.txt       # 必须保留：告诉 Vercel 只把 public/ 当静态目录，
│                          # 否则 Vercel 会把项目根目录所有文件当静态资源公开发布
├─ vercel.json             # 把所有请求 rewrite 到 api/index.go
├─ Dockerfile              # 用 Docker / 云服务器部署时用
└─ go.mod
```

两份入口共用 `site` 这一套代码，所以本地和线上行为完全一致。

> 公共代码刻意**没有**放在 `internal/` 目录下：Vercel 会把 `api/index.go` 复制成
> `main__vc__go__.go` 并以「指定文件」方式编译（`go build main__vc__go__.go`），
> 此时导入方的包名是 `command-line-arguments`，而 Go 规定 `internal` 包只能被同一
> 模块树内的包导入，会直接报 `use of internal package ... not allowed`。

## 本地运行

```powershell
go run .                 # 默认 http://127.0.0.1:8080
$env:PORT=9000; go run . # 换端口
go build -o gosite.exe . # 编译成单个可执行文件（约 12MB）
go vet ./...             # 静态检查
```

---

## 部署方式一：Vercel CLI（最快，不需要 GitHub）

```powershell
npx vercel login    # 浏览器里授权一次
npx vercel          # 首次部署，一路回车
npx vercel --prod   # 正式发布，输出 https://xxx.vercel.app
```

首次 `npx vercel` 的问答这么选：

| 提问 | 回答 |
| --- | --- |
| Set up and deploy? | **Y** |
| Which scope? | 你的账号 |
| Link to existing project? | **N** |
| Project name? | 英文名，比如 `gosite`（决定网址） |
| In which directory is your code located? | **./** |
| Want to modify these settings? | **N** |

以后改完代码，再执行一次 `npx vercel --prod` 就更新了。

## 部署方式二：GitHub 仓库 + Vercel 自动部署

```powershell
git init -b main
git add .
git commit -m "Go 网站初版"
git remote add origin https://github.com/你的用户名/仓库名.git
git push -u origin main
```

然后打开 <https://vercel.com/new> → 选中这个仓库 → **Import** → **Deploy**（框架选 **Other**，
不要填 Build Command 和 Output Directory）。之后每次 `git push`，Vercel 会自动重新部署。

> Vercel 的 Go 约定：`api/` 目录下的文件包名必须是 `handler`，并导出 `Handler(w, r)`；
> 再靠 `vercel.json` 的 rewrites 把请求都交给它。本项目的 `api/index.go` 已经照这个写好了。

## 部署方式三：自己的服务器（Docker 或直接跑二进制）

**Docker（推荐，镜像只有十几 MB，基于 scratch）：**

```bash
docker build -t gosite .
docker run -d -p 8080:8080 --name gosite --restart unless-stopped gosite
```

**不用 Docker，直接跑二进制：**

```powershell
# 在 Windows 上编译 Linux 版（丢到 Linux 服务器用）
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -trimpath -ldflags="-s -w" -o gosite .
```

Linux 上用 systemd 常驻（`/etc/systemd/system/gosite.service`）：

```ini
[Unit]
Description=gosite
After=network.target

[Service]
ExecStart=/opt/gosite/gosite
Environment=PORT=8080
Restart=always
User=www-data

[Install]
WantedBy=multi-user.target
```

Windows 上想开机自启，可以用 [NSSM](https://nssm.cc/) 或「任务计划程序」把 `gosite.exe` 注册成服务。

前面套一层 Nginx / Caddy 反代并签 HTTPS 证书即可对外服务。

---

## ⚠️ 两个必须知道的点

**1. 中国大陆访问**

`*.vercel.app` 免费域名在大陆经常打不开或很慢，这是 Vercel 官方承认的现状，见
[Accessing Vercel-hosted sites from mainland China](https://vercel.com/kb/guide/accessing-vercel-hosted-sites-from-mainland-china)。

### 让国内访问变快的三步

**第一步：函数区域改到香港（收益最大，本项目已配好）**

`vercel.json` 里的 `"regions": ["hkg1"]` 就是在做这件事。为什么关键：

响应头 `x-vercel-id` 的格式是 `边缘节点::函数实际执行区域`。本项目整站都由 Go 函数渲染，
如果函数留在默认的 `iad1`（美国华盛顿），每次访问都要「国内 → 香港边缘 → 华盛顿 → 返回」，
实测 `x-vercel-id: hkg1::iad1`，单次请求 340～760ms。改成 `hkg1` 后函数就在香港执行，
实测变成 `hkg1::hkg1`。

也可以在控制台改：**Project → Settings → Functions → Function Region → Hong Kong (hkg1)**，改完必须 Redeploy。

**第二步：绑定自己的域名**

`*.vercel.app` 解析到的是 Google Cloud 的 IP，在大陆历史上经常被 DNS 污染或阻断。
自有域名（`.com` 约 ¥60/年）通常更稳：

1. Vercel 项目 → **Settings → Domains** → 添加你的域名
2. 去域名商处按提示加解析：`A 76.76.21.21`，或者 `CNAME cname.vercel-dns.com`

**第三步（可选）：让页面走边缘缓存**

本项目的页面是实时渲染的（显示服务器时间、请求数），所以默认不缓存（`max-age=0`），
每次访问都会执行函数。如果你更在意速度、不介意数字最多滞后一分钟，可以在 `vercel.json`
的 `headers` 里加一条：

```json
{
  "source": "/",
  "headers": [
    { "key": "Cache-Control", "value": "public, s-maxage=60, stale-while-revalidate=300" }
  ]
}
```

这样 Vercel 香港边缘可以直接返回缓存，不用每次回源执行函数。

### 如果要求「国内飞快」，Vercel 不是最优解

Vercel 在中国大陆没有节点，上面三步只能改善，不能根治。真正快的是：

- **国内云 + CDN**：腾讯云 EdgeOne Pages、阿里云 OSS/CDN、华为云等。最快最稳，
  但域名必须 **ICP 备案**（需要一台国内服务器，通常 1～2 周）。
- **香港/新加坡轻量服务器**：不用备案，国内访问比 Vercel 默认线路稳，
  自己有运维成本。本项目自带 `Dockerfile`，一条 `docker run` 就能跑起来，迁移成本很低。

**2. Serverless 是无状态的**

Vercel 上每个函数实例各自独立，而且随时会被回收。所以：

- 首页的「本实例请求数 / 运行时长」会时不时**归零**，这是正常的，不是 Bug；
- 想在线上保存数据（留言、用户、订单），要接外部存储：Vercel Postgres / Redis / KV，
  或你自己的数据库。函数本地文件系统是只读的。

## 常见问题

**Q：`/api/echo` 直接浏览器打开报 405？**
正常，它只接受 POST。用 README 里的 `curl` 或 `Invoke-RestMethod` 测试。

**Q：改了模板 / CSS 怎么生效？**
改 `site/templates/` 或 `site/public/` 里的文件，然后重新部署
（`npx vercel --prod`，或 `git push`）。资源是编译时 embed 进去的，必须重新构建。

**Q：为什么访问 `/static/style.css` 是 Go 返回的，不是 Vercel 的 CDN 返回的？**
因为 CSS 内嵌在二进制里。想让它走 CDN，把 `site/public/` 里的文件复制一份到根目录
`public/` 下即可（Vercel 会优先返回静态文件）。

**Q：`public/` 目录能删吗？**
不建议。若项目根目录没有 `public/`，Vercel 会把根目录所有文件当静态资源发布，`main.go`、
`vercel.json` 这些源码就都能被外人下载。`public/robots.txt` 的存在就是为了避免这个问题。
