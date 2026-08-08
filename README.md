# edgeone-sync

部署在 **EdgeOne Makers** 上的云端同步服务：`iptest-web`（或其它本地工具）把订阅 / IP 库 / 检测结果上传到云端 **Blob 持久化存储**，其他人通过**公开 URL** 直接读取，无需 token。

- **只读**：`GET /<key>` 公开访问，不带 token
- **写入 / 覆盖 / 删除**：带 `WRITE_TOKEN`（请求头 `x-token` 或 `Authorization: Bearer`）
- **存储**：EdgeOne Makers Blob，持久化在云端对象存储，免费额度 1GB，**无需在控制台创建/绑定 KV 或 Blob**，`getStore()` 首次调用自动创建命名空间 `edgeone-sync`

## 项目文件

```text
edgeone-sync/
├── index.html                    # 首页（也满足直接上传必须的入口文件）
├── package.json                  # 依赖 @edgeone/pages-blob
├── edge-functions/
│   └── [[default]].js            # Makers Edge Function（唯一的输出代码）
└── README.md
```

> 这是 Makers 的规范：**不需要在控制台里单独找“边缘函数/边缘触发规则”入口**。函数代码放在仓库根目录的 `edge-functions/` 文件夹里，随项目一起部署，Makers 会自动按文件路径生成路由（`[[default]].js` = 根路径下任意多级路径）。

## 一、部署（推荐：导入 Git，自动联动）

1. 把这个仓库推到 GitHub（已推）。
2. 打开 [EdgeOne Makers](https://edgeone.ai) 控制台 → **创建项目** → 选择 **导入 Git 仓库** → 授权 GitHub → 选择本仓库。
3. 构建配置：
   - 根目录：`/`
   - 构建命令：`npm run build`（仓库里已有一个空 build，不会报错）
   - 输出目录：`/`
   - Node 版本：默认即可
4. 选择加速区域，点 **开始部署**。部署成功后得到 Makers 默认域名（形如 `https://xxx.edgeone.app` 之类，以控制台显示为准）。
5. **设置环境变量**（必须，否则写入会返回 403）：
   - 进入 **项目设置 → 环境管理 → 编辑（生产）→ 环境变量**，添加：
     - `WRITE_TOKEN`：你自己生成的一串随机字符，例如 `wrt-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx`
   - 注意：环境变量修改只对**新部署**生效，设置完回到构建部署页**再部署一次**。
6. 之后每次 `git push` 会自动重新部署。

## 二、部署（备选：直接上传 ZIP）

1. 在 Makers 控制台 **创建项目 → 直接上传**。
2. 把**整个项目文件夹**（根目录必须含 `index.html` 和 `edge-functions/`）打成 ZIP 后拖入，点击开始部署。
3. 直接上传**不会在平台执行 `npm install`**，因此上传包内要能解析 `@edgeone/pages-blob`；若失败建议改用 Git 导入方式。

## 三、自定义域名（可选）

- Makers 已提供默认 URL，可以先直接用默认 URL 测试。
- 想用自己的域名：控制台 **域名管理 → 添加自定义域名**，按提示完成归属校验 + CNAME 解析 + HTTPS；大陆加速区域需要域名已备案（你的 `sub.glimmer.hidns.vip`、`raw.647674579.xyz` 若已解析可用）。

## 四、接口协议

```text
GET    /<key>                              → 200 原文（公开，无需 token）
PUT    /<key>  x-token: <token>  Body: 原文 → 200 {"ok":true,"url":"...","size":N}
POST   /<key>  x-token: <token>  Body: 原文 → 同上（兼容）
DELETE /<key>  x-token: <token>            → 200 {"ok":true}
```

- `<key>` 就是 URL 路径，比如 `subs/cloudflare-ips.txt`、`subs/订阅.txt`，支持目录与中文。
- 写操作 token 只能在请求头（`x-token` 或 `Authorization: Bearer`），**不放入 URL**。
- CORS 已放开，浏览器端也能直接调用。

## 五、验证

```powershell
# 上传/覆盖
curl.exe -X PUT "https://<你的域名>/subs/cloudflare-ips.txt" -H "x-token: <WRITE_TOKEN>" --data-binary "@sub.txt"

# 公开读取（不带 token）
curl.exe "https://<你的域名>/subs/cloudflare-ips.txt"

# 没有 token 写入 → 403
curl.exe -X PUT "https://<你的域名>/subs/x.txt" --data "hi"

# 删除
curl.exe -X DELETE "https://<你的域名>/subs/cloudflare-ips.txt" -H "x-token: <WRITE_TOKEN>"
```

## 六、填入 iptest-web

| iptest-web 配置项 | 填什么 |
|---|---|
| 云端地址 / base_url | `https://<你的域名>`（部署后 Makers 给的默认域名或自定义域名） |
| Token | 你在环境变量里设置的 `WRITE_TOKEN` |
| 输出文件名 | 自己定，如 `subs/cloudflare-ips.txt`（可含目录/中文） |

iptest-web 每次跑完检测后把输出内容 `PUT` 到 `https://<你的域名>/<输出文件名>`，订阅链接就是对应的公开 URL。

## 七、限制与说明

- Makers Edge Function 限制：请求体 ≤ **1MB**、代码包 ≤ **5MB**、单次 CPU ≤ 200ms（本服务只做纯转发，正常够用；大于 1MB 的文件需要改用 Blob `createUploadUrl` 预签名直传，本版本未内置）。
- 读取默认使用**强一致**，写入后立即可读到最新内容（Blob 也支持最终一致读，需要极致速度/节省主存储读取时可去掉 `consistency:"strong"`）。
- `PUBLIC_BASE_URL` 环境变量**可填可不填**：不填时写接口返回的 `url` 字段会用请求自身的域名；只有当你希望返回的公开链接固定指向另一个域名时才填写。
- 存储数据**免费额度 1GB**，容量超出或超量访问时控制台会有套餐提示。