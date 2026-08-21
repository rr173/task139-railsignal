# task139-railsignal — Benzhi Evaluation

## 业务说明

本项目是一个**铁路信号联锁与进路控制引擎**（Railway Signal Interlocking & Route Control Engine）。面向铁路车站/枢纽信号楼，把"站场布局 + 进路联锁 + 列车占用走行 + 信号员操控 + 故障降级 + 重启恢复"收进一个后端引擎：信号员选定始端信号机与终端，引擎自动展开进路元素、做联锁冲突校核、转换并定标道岔、开放信号机，并在列车走行时逐段解锁进路，全程遵守铁路信号"安全倾向"原则（任一开放条件失守信号机立即回红）。

主要输入：节点、轨道区段（track/block/ladder）、道岔（heel/normal/reverse + 防挤压区段）、信号机、进路请求（始端信号机 + 终端区段）、占用/出清事件、模拟时钟推进、道岔旁路、取消请求。

主要输出：进路元素序列与联锁校核结论、信号机显示、逐段解锁进度、冲突明细、故障/旁路审计、事件序列、重启重算结果。

## 本地命令

```bash
go build ./...          # 编译
go run .                # 启动（默认 :8080，DB 默认 railsignal.db）
go test ./...           # 测试
go run . --smoke-test    # 自检（执行后自行退出，退出码 0=通过）
```

服务启动后浏览器访问 `http://localhost:8080/` 即操作页面。页面覆盖"建站场 → 建道岔/信号机 → 排进路 → 上报占用走行 → 查看联锁状态与逐段解锁 → 取消进路"的真实读写流程。

## 前端

- 原生 HTML/CSS/JS，无构建步骤；`//go:embed` 打进二进制（`internal/webfs/web/{index.html,app.js,style.css}`）。
- 构建工具：无（`go build ./...` 即产出含前端的单二进制）。
- 构建产物路径：无独立产物（嵌入二进制）。
- 页面 URL：`http://<host>:8080/`（静态资源 `/static/app.js`、`/static/style.css`）。

## Docker 构建（benzhi）

`build_benzhi_docker.sh` 接受两个参数：镜像名、目标平台。

```bash
# amd64
bash ./build_benzhi_docker.sh go-task-benzhi:amd64 linux/amd64
docker run --rm go-task-benzhi:amd64 go version

# arm64
bash ./build_benzhi_docker.sh go-task-benzhi:arm64 linux/arm64
docker run --rm go-task-benzhi:arm64 go version
```

每次构建后检查前端产物存在且非空（已嵌入二进制，`go build ./...` 成功即代表前端可用），并在镜像内运行 Go 启动入口的 `--smoke-test`：

```bash
docker run --rm go-task-benzhi:amd64 bash -lc 'cd /app && go run . --smoke-test'
docker run --rm go-task-benzhi:arm64 bash -lc 'cd /app && go run . --smoke-test'
```

`--smoke-test` 真实请求核心业务 API（建站场→排进路→联锁校核→占用走行→逐段解锁→防迎面解锁→重启重算）并校验联锁安全不变量，执行后自行退出。

进入容器：`docker run -it go-task-benzhi:amd64`

## 运行时 Dockerfile（双架构）

```bash
docker buildx build --platform linux/amd64 --load -t go-task-check:amd64 .
docker run --rm go-task-check:amd64 --smoke-test
docker buildx build --platform linux/arm64 --load -t go-task-check:arm64 .
docker run --rm go-task-check:arm64 --smoke-test
```

## 技术栈

- Go 1.26.3，`GOTOOLCHAIN=local`
- 持久化：`modernc.org/sqlite v1.52.0`（纯 Go，`CGO_ENABLED=0`），SQLite 3.46.1
- 依赖下载：`GOPROXY=https://goproxy.cn,direct`、`GOSUMDB=sum.golang.google.cn`
- Docker builder：`docker.m.daocloud.io/library/golang:1.26.3-bookworm`；运行时：`docker.m.daocloud.io/library/alpine:3.20`
- 双架构：`linux/amd64` + `linux/arm64`
