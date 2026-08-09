# EdgeOne File Hub（云端文件）

一个部署在 **EdgeOne Makers** 上的轻量文件服务：网页直接查看文件、复制公开链接、按文件夹筛选，管理端支持上传 / 更新本地文件、从远程 URL 拉取内容存为云端文件，并可用通用 `upload.bat` 一键上传。

> **English**: A tiny file hub built on EdgeOne Makers. Public visitors can browse files, filter by folder and copy public links; admins (with a token) can upload/update files, pull remote URLs and generate a generic `upload.bat`.

## 功能

- 公开首页：文件列表、大小、更新时间、公开链接，一行一个「复制」按钮
- 支持文件夹路径（如 `docs/example.txt`），首页顶部按文件夹筛选查看
- 管理员 Token 验证后：上传 / 更新本地文件（可覆盖同名）
- 远程拉取（代理任意 http/https URL）：把远程内容（如 `url.txt`）直接保存为云端文件
- 通用 `upload.bat`：把文件拖到脚本上即可上传，不需要为每个文件生成专属脚本
- 文件内容写入 EdgeOne Makers Blob，持久化在云端对象存储

## 项目结构

```text
edgeone-file-hub/
├── index.html                  # 首页（文件列表 + 管理界面）
├── package.json
├── edge-functions/
│   └── [[default]].js          # Makers Edge Function（Blob 存储 + 接口）
├── README.md
└── LICENSE
```

Makers 会自动识别 `edge-functions/` 并解析依赖（`@edgeone/pages-blob`），不需要在控制台另找「边缘函数」入口。

## 部署到 EdgeOne Makers

推荐使用 **导入 Git 仓库**（不要直接上传 ZIP，Blob SDK 依赖需要在构建时安装）。

1. 把这个仓库推送到 GitHub（或任意 Git 托管平台）。
2. EdgeOne Makers → **创建项目** → **导入 Git 仓库** → 选择这个仓库。
3. 构建设置：
   - 根目录：`/`
   - 构建命令：`npm run build`
   - 输出目录：`/`
4. 点击部署，记下 Makers 提供的访问地址。
5. 进入 **项目设置 → 环境管理 → 生产环境 → 环境变量**，添加：

   ```
   WRITE_TOKEN=你自己设置的一串随机字符
   ```

6. 修改环境变量后，**必须重新部署一次**，新的 Token 才会生效。

> `PUBLIC_BASE_URL` 不需要设置。系统会自动用当前访问域名生成公开链接；如果以后绑定了自定义域名，重新上传一次文件，列表里的链接就会自动更新。

## 使用说明

打开部署后的首页（公开页面，无需登录）：

- 查看文件列表：文件名、大小、更新时间、公开链接
- 点击**文件夹**标签可以按目录筛选查看
- 每个文件都有**复制**按钮，一键复制公开链接发给别人即可读取

管理操作（右上角「管理」→ 输入 `WRITE_TOKEN`）：

| 操作 | 方法 |
| --- | --- |
| 上传 / 更新本地文件 | 填写云端路径（可含文件夹，如 `docs/example.txt`），选择本地文件，点「上传 / 更新」；同名路径会覆盖旧文件 |
| 从远程地址拉取 | 粘贴 http/https 地址，可指定保存路径（留空自动取远程文件名），点「拉取并保存」 |
| 下载通用上传脚本 | 点「下载通用上传脚本 (.bat)」，把文件拖到脚本上即可上传 |
| 更新 / 删除已有文件 | 文件列表「操作」列的按钮（验证 Token 后才显示） |

Token 只保存在当前浏览器标签页的 `sessionStorage` 中，关闭标签页后需要重新输入；网页不会把 Token 写进 URL，也不会把 Token 返回给页面以外的人。

## upload.bat

验证 Token 后即可下载一个**通用** `upload.bat`（脚本内已包含你的 Token 和部署地址）。

- 把文件直接拖到 `upload.bat` 上；或
- 双击运行，再把文件路径粘贴进去（可自定义云端路径，支持文件夹）。

脚本内已经包含 Token，**不要发给任何人**。Windows 一般自带 `curl.exe`；如果提示找不到 curl，请改用网页上传。

## 远程拉取（代理）

### 保存为云端文件

```http
POST /api/proxy
Authorization: Bearer WRITE_TOKEN
Content-Type: application/json

{ "url": "https://example.com/url.txt", "key": "remote/url.txt" }
```

- `key` 可省略，默认取远程地址的文件名
- 拉取成功后保存在 Blob，主页可以看到并复制链接

### 直接代理读取（不保存）

```http
GET /api/proxy?url=https://example.com/url.txt
Authorization: Bearer WRITE_TOKEN
```

直接把远程内容返回，不写入存储。适合临时读取远程配置。

> 代理只接受 `http://` / `https://`，返回内容最大 1MB（平台限制），且需要管理 Token，避免被当裸代理滥用。

## 接口

| Method | Path | 说明 | 鉴权 |
| --- | --- | --- | --- |
| GET | `/api/files` | 列出文件（含大小、时间、链接、来源） | 公开 |
| GET | `/<key>` | 读取文件内容 | 公开 |
| POST | `/api/auth` | 验证管理 Token | 公开（用于登录） |
| POST | `/<key>` | 上传 / 覆盖文件 | `WRITE_TOKEN` |
| PUT | `/<key>` | 兼容旧客户端（新代码不再使用） | `WRITE_TOKEN` |
| DELETE | `/<key>` | 删除文件 | `WRITE_TOKEN` |
| POST | `/api/proxy` | 拉取远程 URL 并保存为云端文件 | `WRITE_TOKEN` |
| GET | `/api/proxy?url=...` | 代理读取远程内容（不保存） | `WRITE_TOKEN` |

上传 / 删除 / 代理请求头二选一（推荐第一个，标准头不会被 EdgeOne 防护误拦截）：

```
Authorization: Bearer WRITE_TOKEN
```

或

```
x-token: WRITE_TOKEN
```

## 防护设计

- 读取和列表公开；写入（上传/覆盖/删除/代理）由后端强制校验 Token
- 前端隐藏管理按钮只简化界面，真正安全由后端保证
- `__meta/` 是系统元数据目录，禁止写入
- 云端路径禁止 `..`、反斜杠、控制字符和危险字符
- 单请求最大 1MB（Makers Edge Functions 平台限制）
- 远程拉取仅允许 http/https 且需要 Token

## Blob 说明

代码使用存储空间 `edgeone-sync`（沿用之前版本命名，方便保留已上传文件）。第一次调用时 Makers 会自动创建该 Blob，**不需要手动绑定 KV 或 Blob**。容量以 EdgeOne Makers Blob 免费版当前额度为准。

## 常见问题

**上传 / 拉取返回 HTTP 545？**
这是防护规则拦截旧版本使用的 PUT 和自定义 `x-token` 请求头。本版本上传改用 POST + 标准 `Authorization: Bearer`；重新部署后即可正常使用。如果你还在用旧代码，请推送本仓库并重新部署。

**超过 1MB 的文件传不上去？**
Makers Edge Functions 的请求体上限是 1MB，这是平台限制。

**上传后怎么更新已有文件？**
再次上传相同的云端路径即可覆盖，更新时间会自动刷新。

**文件夹怎么来的？**
云端路径中的 `/` 就是文件夹，例如 `docs/example.txt` 会出现在 `docs` 文件夹下。

## 同类项目参考

- [TencentEdgeOne/edgeone-makers-mcp](https://github.com/TencentEdgeOne/edgeone-makers-mcp) — 腾讯官方 Makers 工具集
- [sagan/FlareDrive](https://github.com/sagan/FlareDrive) — 基于 Cloudflare Workers 的轻量文件服务（本项目后端逻辑的参考）
- [jonerrr/workers-sharex-host](https://github.com/jonerrr/workers-sharex-host) — Workers 文件上传 + ShareX 集成示例

## License

[MIT](LICENSE)