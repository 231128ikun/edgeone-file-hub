# cloudsync —— 云端文本/文件推送同步工具

把文本/文件推送到可公开访问的云端存储（Cloudflare KV、腾讯 EdgeOne KV/Blob），
并拿到一个稳定公开 URL 供分发/订阅拉取。形态为 **Go 库 + CLI**，仓库内附带
可直接部署的服务端边缘函数。

> 与旧方案（CF-Workers-TEXT2KV）的区别：正文走 HTTP body（不再有 65 行限制）、
> 读写分离（公开读、写入必须带 token）、token 只走 header 不进 URL、
> 上传后回读校验内容一致。

## 功能

- 统一 `Target` 接口：`Push / Read / Delete`，返回公开 URL
- 三个开箱即用的 Provider：
  - `cfkv` — Cloudflare KV（服务端：`server/cf-worker`）
  - `edgeone-kv` — EdgeOne KV（key 自动转义为 `[A-Za-z0-9_]`，超长用短哈希）
  - `edgeone-blob` — EdgeOne Blob（保留目录/中文原文件名）
- 配置化目标（YAML/JSON），密钥用 `${ENV_VAR}` / `${ENV_VAR:-default}` 注入
- 上传后默认回读校验，容忍 KV 最终一致性（自动重试）
- 网络抖动 / 429 / 5xx 指数退避重试
- 文件名消毒（路径穿越/控制字符/512B 上限）、密钥日志脱敏
- CLI：`push / read / delete / list / config-check / version / help`，支持多目标广播（`--all`）

## 目录结构

```text
cloudsync/
├── go.mod / go.sum
├── cmd/cloudsync/          # CLI 入口（main.go + 集成测试）
├── internal/
│   ├── cloudsync/          # 核心库：接口、配置、重试、回读校验、脱敏、sanitize
│   └── provider/
│       ├── httpprov/       # §7 HTTP 协议客户端（cfkv / edgeone 共用）
│       ├── cfkv/           # Cloudflare KV provider
│       └── edgeone/        # EdgeOne KV / Blob provider
├── server/
│   ├── cf-worker/          # Cloudflare Worker（KV）部署交付物
│   ├── edgeone-kv/         # EdgeOne KV 边缘函数
│   └── edgeone-blob/       # EdgeOne Blob 边缘函数
├── cloudsync.yaml.example  # 配置示例
├── testdata/               # 测试数据（含 1000 行文件）
└── plan.md                 # 原始实现计划
```

## 构建与安装

```powershell
go build -o cloudsync.exe ./cmd/cloudsync
# 或安装到 GOPATH/bin
go install ./cmd/cloudsync
```

## 配置

配置文件名按顺序查找：`$CLOUDSYNC_CONFIG` → `./cloudsync.yaml` →
`./cloudsync.yml` → `./cloudsync.json` → `~/.cloudsync.yaml` →
`~/.config/cloudsync/cloudsync.yaml`。也可以给每个子命令传 `-config <path>`。

```yaml
# cloudsync.yaml（完整示例见 cloudsync.yaml.example）
targets:
  cf:
    type: cfkv                      # cfkv | edgeone-kv | edgeone-blob
    base_url: https://sub.example.com
    token: ${CLOUDSYNC_CF_TOKEN}    # 密钥走环境变量，不写进仓库
    filename_prefix: sub/           # 可选：远程 key 前缀
    verify: true                    # 可选：回读校验，默认 true
    # 可选 provider 选项：
    # retries: 3                    # 上传重试次数（默认 3）
    # timeout: 30s                  # HTTP 超时（默认 30s；0 = 关闭）
  edge:
    type: edgeone-blob
    base_url: https://my-project.edgeone.app
    token: ${CLOUDSYNC_EO_TOKEN}
    bucket: subs                    # 可选：目录前缀
```

`config-check` 可校验配置（类型是否注册、URL 是否合法、前缀是否安全）：

```powershell
cloudsync config-check
```

## CLI 用法

```powershell
# 推送单个目标，输出公开 URL
cloudsync push cf .\sub.txt

# 推送并指定远程文件名（支持目录/中文）
cloudsync push cf .\sub.txt --name "sub/订阅.txt"

# 广播到所有配置的目标
cloudsync push --all .\sub.txt

# 回读内容到 stdout / 删除远程对象 / 列出目标
cloudsync read cf "sub/订阅.txt"
cloudsync delete cf "sub/订阅.txt"
cloudsync list
```

常用 flag（push）：

| flag | 说明 |
|---|---|
| `--all` | 推送到所有配置的目标 |
| `--name <name>` | 远程文件名（默认取本地文件 base name） |
| `--verify true\|false` | 覆盖回读校验设置 |
| `--retries <n>` | 上传重试次数（0 = 默认） |
| `--timeout <dur>` | 整体超时，如 `90s` |
| `-config <path>` / `-verbose` | 全局 flag |

## 作为 Go 库使用

```go
import (
    "cloudsync/internal/cloudsync"
    _ "cloudsync/internal/provider/cfkv"   // 注册 provider（init）
    _ "cloudsync/internal/provider/edgeone"
)

cfg, err := cloudsync.LoadConfig("cloudsync.yaml")
targets, err := cfg.Build()                    // map[目标名]Target
url, err := cloudsync.Push(ctx, targets["cf"], "sub/订阅.txt", data)
```

## 服务端协议（§7）

三个服务端交付物实现同一协议：

```text
GET    /<key>                             -> 200 原文（公开读，无鉴权）
PUT    /<key>  x-token: <写token>  Body: 原文 -> 200 {"url": "...", "size": N}
DELETE /<key>  x-token: <写token>           -> 200 {"ok": true}
```

部署步骤见各目录 README：

- `server/cf-worker/`（wrangler + KV namespace + `WRITE_TOKEN` secret）
- `server/edgeone-kv/`（EdgeOne Makers，绑定 KV）
- `server/edgeone-blob/`（EdgeOne Blob，`getStore()`，支持中文/目录/强一致）

部署完成后，把域名填进配置的 `base_url`，即可用 CLI 推送。

## 安全基线

- token 只存在于请求 header（`x-token` 或 `Authorization: Bearer`），禁止进 URL；
- 读公开、写/删鉴权：公开订阅链接没有写权限；
- 密钥通过环境变量注入，本地配置文件权限建议 0600；
- `list` 输出与错误日志对 token 一律脱敏（`***`）；
- 文件名消毒：拒绝路径穿越、控制字符，key 上限 512B（EdgeOne KV 另有字符集转义）。

## 限制与注意事项

- 单对象 ≤ 25 MiB（平台限制）；
- CF KV / EdgeOne KV 最终一致 ≤ 60s，CLI 回读校验会自动重试；EdgeOne Blob 可用强一致模式；
- EdgeOne KV key 只允许 `[A-Za-z0-9_]` 且 ≤512B：中文/点号/斜杠会被转义（`s_<hex>`），
  超长时退化为 `s_<sha256 前 64 位>`；想保留原文件名请用 `edgeone-blob`。

## 测试

```powershell
$env:GOCACHE = "$pwd\.gocache"   # 沙箱环境需要；普通环境可省略
go build ./...
go vet ./...
go test ./...
gofmt -l cmd internal
```

覆盖点：核心库（配置解析/env 展开/重试/sanitize/回读校验容忍最终一致）、
HTTP 协议客户端（§7 往返、403、404、重试、>1000 行大文件）、
edgeone safeKey 转义与哈希、cfkv 往返、CLI 集成（push/read/delete/list/
config-check/version/help、token 脱敏、Unicode 远程名、多目标广播）。

## 与 plan.md 验收标准对照

| 验收项（plan §10） | 现状 |
|---|---|
| 1000 行文件完整上传且读回一致 | 完成：`httpprov` 大 payload 测试 + `testdata/sub_1000.txt` |
| 公开读无 token；无 token 写返回 403 | 完成：服务端按 §7 实现，客户端 401/403 测试 |
| 带 token 可写/可删 | 完成：client/cfkv/edgeone/CLI 测试 |
| token 不出现在 URL/日志 | 完成：token 只走 header；`list`/错误日志脱敏测试 |
| 云端不留测试垃圾 | 完成：CLI `delete` + 测试均删除后退出 |
| iptest-web 前端集成 | 二期（plan §8/M5），不在首期范围 |