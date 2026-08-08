# edgeone-sync —— EdgeOne 云端同步（部署件）

一个部署在腾讯云 **EdgeOne** 上的边缘函数，作用是为 **iptest-web**（或其他本地工具）
提供「上传到云端 + 公开读取 + 更新覆盖」的能力：

- 把订阅输出 / IP 库 / 检测结果上传到 EdgeOne Blob 存储；
- 返回一个**公开 URL**，订阅者/多端直接 GET 读取，无需 token；
- 写入、覆盖、删除需要带 `WRITE_TOKEN`，读写权限分离。

整个项目只需要一个文件：**`index.js`**（EdgeOne 边缘函数，Blob 版）。

> 为什么选 Blob 而不是 KV：Blob 免绑定存储、支持中文/目录文件名、强一致（写完立读）。
> 单对象上限 25 MiB。

## 接口协议

```text
GET    /<key>                              → 200 原文（公开读，无鉴权）
PUT    /<key>  x-token: <写token>  Body: 原文 → 200 {"url": "...", "size": N}
DELETE /<key>  x-token: <写token>            → 200 {"ok": true}
```

- key 就是 URL 路径，如 `subs/订阅.txt`、`cloudflare-ips-v4.txt`，支持中文与目录；
- 写 token 只走请求头（`x-token` 或 `Authorization: Bearer`），**绝不进 URL**。

## 部署步骤（EdgeOne 控制台，约 5 分钟）

1. 打开 [EdgeOne 控制台](https://edgeone.ai)（腾讯云账号），进入你的站点；
2. 左侧 **边缘函数** → **创建函数**，把 `index.js` 的内容**全部粘贴**进去；
3. **环境变量**中新增：
   - `WRITE_TOKEN`（必填）：自己生成一个长随机串作为写 token，例如 `wrt-` 加 32 位随机字符；
   - `PUBLIC_BASE_URL`（可选）：你的订阅域名，如 `https://sub.example.com`；不填则用函数自带域名；
4. 保存并**发布**，把函数挂到域名路径（如 `https://sub.example.com/*`）；
5. 部署完成后你得到两个值，就是需要填到 iptest-web 的信息：
   - **URL**：函数访问地址（`https://sub.example.com`，不带路径）
   - **Token**：你第 3 步设置的 `WRITE_TOKEN`

## 验证

```powershell
# 上传（key 含目录与中文都没问题）
curl.exe -X PUT "https://<你的域名>/subs/订阅.txt" -H "x-token: <WRITE_TOKEN>" --data-binary "@sub.txt"

# 公开读（不带 token）
curl.exe "https://<你的域名>/subs/订阅.txt"

# 无 token 写 → 403
curl.exe -X PUT "https://<你的域名>/subs/订阅.txt" --data "hi"

# 更新覆盖：再 PUT 一次同名 key 即可；删除：
curl.exe -X DELETE "https://<你的域名>/subs/订阅.txt" -H "x-token: <WRITE_TOKEN>"
```

## 对接 iptest-web

iptest-web 的「云端同步」设置里填入：

| iptest-web 配置项 | 填什么 |
|---|---|
| base_url / 云端地址 | `https://<你的域名>`（部署后函数域名） |
| token | 你设置的 `WRITE_TOKEN` |
| 输出文件名 | 自定义，如 `subs/cloudflare-ips.txt`（可含目录/中文） |

iptest-web 每次跑完检测/维护任务后，把输出内容 `PUT` 到这个地址（带 `x-token`），
订阅链接即指向对应公开 URL。

> 注意：iptest-web 的「云端同步」入口当前仍在开发中（见其 todo.md），
> 部署完本函数后，如需要我可以继续在 iptest-web 中实现该设置与自动上传逻辑。

## 安全说明

- `WRITE_TOKEN` 只存在 EdgeOne 环境变量中，不进代码、不进 git、不进 URL；
- 公开 URL 只读；要撤销写入权限，改掉 `WRITE_TOKEN` 即可；
- 本函数不解析/不记录请求内容，CORS 已放开便于网页端直接调用。

## 文件说明

```text
edgeone-sync/
├── README.md   # 本说明
└── index.js    # EdgeOne 边缘函数（唯一需要部署的文件）
```