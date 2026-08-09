# EdgeOne File Hub（云端文件 + URL 反代）

一个部署在 **EdgeOne Makers** 上的轻量文件服务：

- **公开首页**：直观看到已上传的文件列表、上传时间、文件大小、来源和公开链接，一键复制；
- **URL 反代**：主页输入任意 http/https 网址，自动生成 `/api/proxy?url=...` 链接，可复制分享或直接跳转访问远端内容，**只转发不保存**；
- **文件备注**：管理员可给文件添加备注（云端持久化），首页公开显示备注内容，访客只读；
- **多来源上传**：本地文件（支持拖拽）/ 远程 URL 拉取 / 手动粘贴文本，三种来源统一在管理面板里操作；
- **通用 upload.bat**：验证 Token 后下载，把文件拖到脚本上即可上传，不用为每个文件做专属脚本。

> **English**: A tiny file hub on EdgeOne Makers. Public visitors can browse files, copy links and use a public URL proxy; admins can upload files from local / remote URL / manual text with one token-protected panel.

## 功能

- 公开首页：文件列表（名称、大小、更新时间、来源徽章：本地上传 / 远程拉取 / 手动输入、备注内容）
- 支持文件夹路径（如 `docs/example.txt`），首页按文件夹筛选
- URL 反代：`GET /api/proxy?url=https://...`，不保存、公开可用（默认）
- 管理面板（Token 验证后）：
  - **本地文件**：拖拽或点击选择，自动填云端路径，单击「上传 / 更新」，同名覆盖
  - **远程 URL**：粘贴地址，可选保存路径（默认取远程文件名），拉取后存为云端文件
  - **手动输入**：输入文件名 + 粘贴文本内容，适合手工维护 url.txt 之类的小文件
- 通用 `upload.bat`：一个脚本管所有文件
- 文件存在 EdgeOne Makers **Blob**（`edgeone-sync`），持久化在云端对象存储

## 项目结构

```text
edgeone-file-hub/
├── index.html                  # 首页（文件列表 + 管理面板 + URL 反代）
├── package.json
├── edge-functions/
│   └── [[default]].js          # Makers Edge Function（Blob 存储 + 接口）
├── README.md
└── LICENSE
```

## 部署到 EdgeOne Makers

推荐用 **导入 Git 仓库** 部署（不要上传 ZIP，Blob SDK 依赖需要在云端构建时安装）。

1. 把这个仓库推送到 GitHub（公开或私有均可）。
2. EdgeOne Makers → **创建项目** → **导入 Git 仓库** → 选择这个仓库。
3. 构建设置：
   - 根目录：`/`
   - 构建命令：`npm run build`
   - 输出目录：`/`
4. 点部署，记下 Makers 给的访问地址。
5. **项目设置 → 环境管理 → 生产环境 → 环境变量**，添加：

   ```
   WRITE_TOKEN=glimmer
   ```

   `glimmer` 换成你自己好记的 Token（管理面板登录、上传、删除都靠它）。

6. 改完环境变量后**重新部署一次**才生效。以后每次推送到默认分支（`main`），EdgeOne 会自动重新部署。

### 环境变量

| 变量 | 必填 | 说明 |
| --- | --- | --- |
| `WRITE_TOKEN` | 是 | 管理 Token，登录 / 上传 / 删除 / 拉取保存都校验它 |
| `PUBLIC_BASE_URL` | 否 | 留空即可，自动用当前域名生成公开链接 |
| `PUBLIC_PROXY` | 否 | 留空 = 公开 URL 反代；填 `off` = 关闭公开反代（此时反代需要 Token） |

## 使用说明

### 首页（公开，无需登录）

- 上方是 **URL 反代**：输入网址（不带 `https://` 会自动补）→ 自动生成反代链接：点「复制」发给别人，或点「跳转」直接访问。只转发不保存，不会出现在文件列表里。
- 下方是**文件列表**：点「复制」复制公开链接；点文件夹标签按目录看文件；有备注的文件会直接显示备注内容（备注仅管理员可编辑）。

### 管理面板

点右上角「管理」→ 输入 `WRITE_TOKEN`（输入一次，刷新标签页后要重新输入；关闭标签页后要重登）。登录后才显示上传 / 下载脚本等按钮。

| 来源 | 操作 |
| --- | --- |
| 本地文件 | 拖文件进选择区，或点击选择；自动填好云端路径，点「上传 / 更新」 |
| 远程 URL | 粘贴 http/https 地址 → 可改保存路径（留空取远程文件名）→「拉取并保存」 |
| 手动输入 | 文件名 + 粘贴正文 →「保存」 |

文件列表「操作」列（验证 Token 后显示）：打开 / 复制 / 备注；管理操作（覆盖更新 / 从来源更新 / 移动 / 删除）收在「⋯」菜单里。顶部可下载**通用 upload.bat**。

## upload.bat

验证 Token 后下载一个**通用**脚本（脚本里已写好你的部署地址和 Token，**不要发给别人**）。

- 把文件直接拖到 `upload.bat` 上；或
- 双击运行，粘贴文件路径（可填云端路径，支持文件夹）。

Windows 自带 `curl.exe` 即可，无需额外安装。

## URL 反代接口

```
GET /api/proxy?url=https://example.com/url.txt
```

- 默认**公开**、**不保存**，直接返回远端内容（支持文本 / 图片 / 二进制等）
- 自动补协议：填 `example.com/url.txt` 也可以
- 防护：拒绝 localhost、内网、云元数据等地址；最大 1MB；15 秒超时；最多跟随 5 次重定向（每跳重新检查目标是否安全）
- 想完全关闭公开反代：环境变量 `PUBLIC_PROXY=off`（此后必须带 Token 才能用）

### 远程拉取并保存（需 Token）

```
POST /api/proxy
Authorization: Bearer WRITE_TOKEN
Content-Type: application/json

{ "url": "https://example.com/url.txt", "key": "remote/url.txt" }
```

- `key` 可省略，默认取远程文件名
- 保存到 Blob，文件列表会出现（来源：远程拉取），可直接复制链接读取

## 接口一览

| 方法 | 路径 | 说明 | 鉴权 |
| --- | --- | --- | --- |
| GET | `/api/files` | 文件列表（名称 / 大小 / 时间 / 来源 / 备注 / 链接） | 公开 |
| GET | `/<key>` | 读取文件内容 | 公开 |
| GET | `/api/proxy?url=...` | URL 反代（不保存） | 默认公开；`PUBLIC_PROXY=off` 后需 Token |
| POST | `/<key>` | 上传 / 覆盖文件 | `WRITE_TOKEN` |
| PUT | `/<key>` | 兼容旧客户端 | `WRITE_TOKEN` |
| DELETE | `/<key>` | 删除文件 | `WRITE_TOKEN` |
| POST | `/api/proxy` | 拉取远程 URL 并保存为云端文件 | `WRITE_TOKEN` |
| PUT | `/api/notes?key=...` | 保存 / 清除文件备注（body: `{ "content": "..." }`，空内容 = 清除） | `WRITE_TOKEN` |
| DELETE | `/api/notes?key=...` | 清除文件备注 | `WRITE_TOKEN` |
| POST | `/api/auth` | 验证 Token（前端登录用） | 公开 |

头二选一（推荐第一个，标准头不会被 EdgeOne 防护误拦截）：

```
Authorization: Bearer WRITE_TOKEN
```

或

```
x-token: WRITE_TOKEN
```

## 防护设计

- 普通用户只能读文件和偶尔用 URL 反代；写入 / 删除 / 拉取保存 / 备注编辑全部由后端校验 Token 管住，前端隐藏按钮只是简化界面
- 前端不把 Token 写进 URL，只存在当前标签页会话里
- `__meta/` 为系统保留目录，禁止写入；云端路径禁止 `..`、反斜杠和危险字符
- 单请求最大 1MB（Makers Edge Functions 平台限制）
- URL 反代带 SSRF 防护（内网 / 本机 / 元数据地址一律拒绝）+ 1MB + 超时 + 重定向防护；不想开就设 `PUBLIC_PROXY=off`

## Blob 说明

代码使用存储空间 `edgeone-sync`（沿用旧命名，方便保留已上传文件）。第一次调用时 Makers 会自动创建 Blob，**不需要手动绑定 KV / Blob 空间**。容量以 EdgeOne Makers Blob 当前免费额度为准。

## 常见问题

**HTTP 545？**
老版本用 PUT / 自定义 `x-token` 被防火墙拦截的提示。本版已改 POST + 标准 `Authorization: Bearer`，推送到默认分支后 EdgeOne 会自动重新部署。

**超过 1MB 传不上？**
Makers Edge Functions 请求体上限 1MB，平台限制。

**怎么更新已有文件？**
再次上传相同云端路径即可覆盖；远程拉取 / 手动输入用同一个路径也会覆盖。

**文件夹怎么来的？**
路径里的 `/` 就是文件夹，如 `docs/example.txt` 属于 `docs`。

**改环境变量后没生效？**
改完必须触发一次重新部署（推代码会自动部署，改环境变量要手点一次部署）。

## 同类项目参考

- [TencentEdgeOne/edgeone-makers-mcp](https://github.com/TencentEdgeOne/edgeone-makers-mcp) — 腾讯官方 Makers 工具集
- [sagan/FlareDrive](https://github.com/sagan/FlareDrive) — 基于 Cloudflare Workers 的轻量文件服务（本项目后端逻辑参考）
- [jonerrr/workers-sharex-host](https://github.com/jonerrr/workers-sharex-host) — Workers 文件上传 + ShareX 集成示例

## License

[MIT](LICENSE)