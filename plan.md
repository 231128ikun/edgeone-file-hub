# cloudsync —— 云端文本/文件推送同步工具（计划）

> 状态：计划阶段（仅 plan.md，尚未写代码）。
> 本文件是后续新开对话的实现蓝图；新对话请先读完本文件再动工。
> 命名说明：cloudsync = cloud + sync，中文可叫「云端推送/云端同步」。如果后面觉得名字不合适，只是一个目录名 + module 名，随时可改。

---

## 1. 项目定位

一句话：**「把文本/文件推送到可公开访问的云端存储，并拿到一个稳定 URL 供分发/订阅拉取」的通用工具。**

形态：**Go 库 + CLI**，并预留 HTTP 服务形态，方便被本地 Web 应用（首要是 `iptest-web`）集成。

“通用”体现在：

- 后端不绑定单一平台：Cloudflare、腾讯 EdgeOne（edgeone.ai Makers）、WebDAV、GitHub 等可插拔；
- 输入不限“IP 列表”：任何文本/JSON/CSV/小文件都行；
- 既可单次命令行推送，也可一键配置多个目标同时同步；
- 现阶段的“当前需求”特指 iptest-web 的：订阅输出、IP 库导出、检测结果上传。

## 2. 背景与缘起

- iptest-web 的 `todo.md` 里已有「云端同步」待办：检测结果 / IP 库 / 订阅输出上传云端、多端访问、鉴权与数据安全。
- 现在用的是 `CF-Workers-TEXT2KV`（Cloudflare Pages + KV + update.bat），存在已知硬伤：
  - URL 传参 → 只传前 65 行；
  - 读/写共用一个 token，公开订阅链接等于公开写入权；
  - token 暴露在浏览器历史 / URL 中。
- 腾讯 `edgeone.ai`（EdgeOne Makers）经核实能力相当，国内访问更好，可作为第二/首选后端。
- 结论：做一个**独立的通用上传/同步项目**，iptest-web 消费它，而不是把上传逻辑写死在 iptest-web 里。

## 3. 目标 / 非目标

### 3.1 目标（首期）

- [ ] 提供统一 Go Provider 接口：`Push / Read / Delete`，返回公开 URL。
- [ ] 至少实现两个后端：
  - **Cloudflare KV**（改造版 cf-worker：POST body + header token，见 §7）
  - **EdgeOne KV / Blob**（提供可部署的边缘函数）
- [ ] CLI：`cloudsync push <目标名> <文件路径>`，输出公开 URL。
- [ ] 支持配置化目标（YAML/JSON，密钥走环境变量或本地权限文件）。
- [ ] 上传成功后可**回读校验**，确认内容一致。
- [ ] 简单重试（网络抖动、5xx 限流）。
- [ ] 仓库内附带服务端交付物：`server/` 里的 CF Worker 与 EdgeOne Function 模板（部署即用）。

### 3.2 目标（二期/预留）

- [ ] WebDAV Provider（通用，写文件 + 可选前缀）
- [ ] GitHub 仓库 raw Provider（仓库内可放参考代码、Github API 上传）
- [ ] AWS S3 / Cloudflare R2 Provider（Presigned URL 或直传）
- [ ] 多个目标同时推送（广播）
- [ ] 定时/自动同步（cron 或由宿主应用触发）
- [ ] HTTP 服务模式（供 iptest-web / 其他本地工具调用）

### 3.3 非目标（首期明确不做）

- 不做多用户/账号体系；
- 不做双向同步、冲突合并（只做“上传/覆盖”）；
- 不做 GUI（CLI + 库即可）。

## 4. 关键事实与约束（调研结论，开发时勿绕过）

| 平台 | 事实 | 影响 |
|---|---|---|
| CF KV | 单 value ≤ 25MB；写入走 URL 有限制（TEXT2KV 65 行）；最终一致 ≤ 60s | 必须用 POST/PUT body 传输；写入后回读可能不明显，校验要考虑缓存 |
| EdgeOne KV | 免费账户 1GB；key 只允许 `[A-Za-z0-9_]` 且 ≤512B；value ≤25MB；**只能在 Edge Functions 里用**；最终一致 ≤60s（写入节点立读） | 中文/带点文件名需转义成 safe key（如 `f_<hash>`），或改用 Blob |
| EdgeOne Blob | 对象存储支持 `/` 目录、任意名字（中文 OK）、25MB/对象；`getStore()` 免绑定；有强一致模式；Edge Functions 和 Cloud Functions 都可用 | 想要「目录 + 原文件名」语义时优先 Blob |
| GitHub raw | 公开读有历史；带 token 的 API 5000 次/时；国内直连不稳、更新有 CDN 延迟 | 只做备选，不适合高频 |
| WebDAV | 多数服务商不给匿名公开读 URL | 需配分享链接/反向代理，首期不做 |

**安全基线（本项目强约束）**

- token/密钥禁止出现在 URL、代码、git 历史中；
- 上传接口 must 用 `Authorization: Bearer <token>` 或 `x-token` header；
- **读与写权限分离**：订阅用的公开 URL 不带写 token；写入必须带 token（服务端函数实现）；
- 本地密钥文件权限 0600 或环境变量注入。

## 5. 仓库结构（建议，后续可调）

```text
cloudsync/
├── go.mod                       # module cloudsync, go 1.24
├── cmd/cloudsync/main.go        # CLI 入口
├── internal/
│   ├── cloudsync/               # 核心库
│   │   ├── target.go            # Provider / Target 接口 + 注册表
│   │   ├── push.go              # 推送流程：校验 → encode → 上传 → 回读校验 → 返回 URL
│   │   ├── retry.go             # 重试策略
│   │   └── config.go            # 配置结构/加载/校验
│   └── provider/
│       ├── cfkv/                # CF KV（TEXT2KV 改进协议）
│       ├── edgeone/            # EdgeOne KV / Blob
│       ├── webdav/             # 二期
│       └── github/             # 二期
├── server/                      # 服务端部署材料（不是本项目业务，是配套）
│   ├── cf-worker/              # Cloudflare Worker：公开读 + header token 写（KV）
│   ├── edgeone-kv/            # EdgeOne 函数：KV 版
│   └── edgeone-blob/           # EdgeOne 函数：Blob 版
├── testdata/                    # 上传测试样例（含 >65 行文件）
└── plan.md                     # 本文件
```

（真正的目录以实际实现时的心智模型为准，不必死板照抄。）

## 6. 核心接口草案（Go）

```go
// Target 描述一个可推送的云端目标
type Target interface {
    Name() string
    // Push 上传文件内容，返回公开可访问的 URL
    Push(ctx context.Context, filename string, data []byte) (string, error)
    // Read 回读校验用（可选），未实现时推送时跳过回读
    Read(ctx context.Context, filename string) ([]byte, error)
    // Delete 删除云端对象（可选）
    Delete(ctx context.Context, filename string) error
}

// Registry / 配置加载
type Config struct {
    Targets map[string]TargetConfig // YAML 目标名 → 配置
}
```

```yaml
# cloudsync.yaml 示意
targets:
  cf:
    type: cfkv
    base_url: https://cf-workers-text2kv-aec.pages.dev
    token: ${CLOUDSYNC_CF_TOKEN}      # 不落盘，读环境变量
    filename_prefix: sub/          # 可选，所有文件名加前缀
  edge:
    type: edgeone-kv               # 或 edgeone-blob
    base_url: https://my-project.edgeone.app
    token: ${CLOUDSYNC_EO_TOKEN}
  daily:
    type: edgeone-blob
    base_url: https://my-project.edgeone.app
    bucket: subs
```

## 7. 服务端函数协议（配套交付）

统一协议（与平台无关）：

```
GET    /<key>                            → 公开读（无 token）
PUT    /<key>   Header: x-token: <写token>   Body: 原文    → 写入，返回 {url, size}
DELETE /<key>   Header: x-token: <写token>                   → 删除
```

- CF Worker 版：在 `CF-WORKERS-TEXT2KV` 基础上改造：加 POST/PUT 分支，token 优先读 header，**读时不校验 token（公开读）**；删除 KV。
- EdgeOne 版：同协议，函数用 `my_kv.put/get/delete`；若要原文件名保留（中文、点号）→ 用 Blob `getStore().set()`。
- 部署交付物放在 `server/`，每个版本附 README（含控制台绑定步骤）。
- 注意：CF KV / EdgeOne KV 的最终一致（≤60s）会导致“写完立刻回读旧值”，回读校验逻辑要容忍（重试几次或等 1s；或 EdgeOne Blob 用强一致模式）。

## 8. 与 iptest-web 的集成（二期）

推荐链路（不做嵌入式二开，而是消费 cloud sync 库）：

1. iptest-web `go.mod` 通过 `replace` 引入 `cloudsync` 模块（或把 cloudsync 发布后用 require）。
2. iptest-web 新增 `POST /api/cloud/upload`：入参 `{target, filename, content}` → 调用 `cloudsync.Push` → 返回公开 URL。
3. 前端「导出 / IP 库」面板加「上传云端」按钮，展示 URL 可复制；
4. 自动化维护任务在输出完成后支持「上传步骤」：目标可配置；
5. 密钥配置在 `data/config.json`（存储在本机），不上传云端。

（iptest-web 的改动属于那个项目的 range，不在 cloudsync 首期范围；但 cloudsync 的 API 要为它提供便利。）

## 9. 里程碑

- M1（骨架 + 设计定稿）
  - [ ] 仓库初始化（go.mod / 目录 / 约定）
  - [ ] Target 接口 + 注册表 + 配置加载
  - [ ] CLI `push` 子命令（本地文件 → 指定目标）
- M2（服务端交付）
  - [ ] `CF Worker` 改造版
  - [ ] EdgeOne KV 函数版
  - [ ] EdgeOne Blob 函数版
  - [ ] 各部署 README（含绑定 KV / 设置 TOKEN / 域名）
- M3（Provider 落地）
  - [ ] cfkv provider（读/写/回读/重试）
  - [ ] edgeone-kv provider
  - [ ] edgeone-blob provider
  - [ ] 集成测试：上传 >65 行文件 → 拉回一致
- M4（通用+安全加固）
  - [ ] 配置热校验、文件名 sanitize（KV 字符限制）
  - [ ] 密钥脱敏
  - [ ] 多目标广播
- [ ] M5（iptest-web 集成 + 发布）
  - [ ] iptest-web replace 引入
  - [ ] `/api/cloud/upload` + 前端按钮
  - [ ] 自动化任务追加上传步骤
  - [ ] 版本号/构建/回归（按 iptest-web AGENTS.md）

## 10. 验收标准（首期完成时）

- [ ] `cloudsync push mysub sub.txt` 一次性把 ≥1000 行文件完整上上去，公开 URL GET 回来与本地完全一致。
- [ ] 公开 URL 无 token 可读；无 token 写请求返回 403。
- [ ] 带 token 可写/可删。
- [ ] token 不出现在 URL/日志中。
- [ ] 云端目录不留测试垃圾（Delete 可用）。
- [ ] iptest-web 前端出现「上传云端」且可复制 URL（如在 M5 范围内）。

## 11. 风险与待决策

- **命名**：cloudsync 暂时用，不满意可改。
- **默认后端选哪个**：国内访问 EdgeOne 更优；已部署的 CF TEXT2KV 可先平滑替换协议。建议首期俩都做，默认 edgeone（若用户实际以国内订阅为主）。
- **EdgeOne Blob 强一致模式**：需要二次确认文档是否已上线（KV 文档提过 Blob 有强一致）；不要假设，实现时按官方 SDK 确认。
- **免费额度**：CF KV 1GB；EdgeOne KV 1GB；Blob 免费额度实现前到控制台/定价页盖章。
- **国内网络环境**：GitHub API 直连不稳可能影响开发拉包；尽量用官方 CDN/镜像。

## 12. 开发前必须确认（新对话第一件事）

1. 上面命名 `cloudsync` 是否按用户确认。
2. 是否注册为新 git 仓库（推荐：创建，分支 `codex/init`）。
3. 首期 Provider 优先级：`cfkv` + `edgeone` 两遍都做，还是一遍先跑通。
4. 服务端默认协议按 §7（读写分离 + header token）执行，无异议。

> 计划结束。后续新对话请以「实现 cloudsync plan.md」为目标，先读本文件，再开始。

