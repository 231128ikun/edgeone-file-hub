# edgeone-sync

部署在 **EdgeOne Makers** 上的云端同步服务：`iptest-web`（或其它本地工具）把订阅 / IP 库 / 检测结果上传到云端 **Blob 持久化存储**，其他人通过**公开 URL** 直接读取，无需 token。

自带一个**网页管理面板**（仓库根目录 `index.html`）：
- 汇总已上传文件：路径、大小、更新时间、外部访问 URL
- 手动选择文件上传 / 覆盖更新、删除
- 复制每个文件的公开链接
- 下载 `update.bat`：把本地文件拖到 bat 上即可一键上传更新
- 内置用法说明（curl、iptest-web 配置）

## 项目文件

```text
edgeone-sync/
├── index.html                    # 网页管理面板（也满足直接上传必须的入口文件）
├── package.json                  # 依赖 @edgeone/pages-blob
├── edge-functions/
│   └── [[default]].js            # Makers Edge Function（唯一的输出代码）
└── README.md
```

> Makers 规范：**不需要在控制台里单独找“边缘函数”入口**。函数代码放在仓库根目录 `edge-functions/` 里，随项目一起部署，平台自动按文件路径生成路由（`[[default]].js` = 根路径下任意多级路径）。存储用 `@edgeone/pages-blob`，`getStore()` 自动创建命名空间，**不用在控制台绑定 KV/Blob**。

## 一、部署（推荐：导入 Git，自动联动）

1. 把仓库推到 GitHub。
2. 打开 [EdgeOne Makers](https://edgeone.ai) 控制台 → **创建项目** → **导入 Git 仓库** → 授权 GitHub → 选择本仓库。
3. 构建配置：
   - 根目录：`/`；构建命令：`npm run build`（已内置空 build）；输出目录：`/`；Node 版本默认即可。
4. 选择加速区域，点 **开始部署**，得到默认域名 URL（形如 `https://xxx.edgeone.app`，以控制台显示为准）。
5. **设置环境变量**（必须，否则写入返回 403）：
   - **项目设置 → 环境管理 → 编辑（生产）→ 环境变量** → 添加 `WRITE_TOKEN`（自己生成随机串，如 `wrt-xxxxxxxx...`）。
   - 环境变量只对**新部署**生效，设置完到构建部署页**再部署一次**。
6. 之后每次 `git push` 自动重新部署。

## 二、部署（备选：直接上传 ZIP）

1. Makers 控制台 **创建项目 → 直接上传**。
2. 把**整个项目文件夹**（根目录必须含 `index.html` 和 `edge-functions/`）打成 ZIP 拖入。
3. 直接上传**不会执行 npm install**，依赖需能解析；失败就改用 Git 导入。

## 三、自定义域名（可选）

Makers 自带默认 URL 可直接用。想用自己的域名：控制台 **域名管理 → 添加自定义域名** → 归属校验 + CNAME + HTTPS（大陆加速区域需备案）。

## 四、接口协议

```text
公开：
  GET    /<key>                          → 200 原文（无需 token）

管理（请求头 x-token: <WRITE_TOKEN> 或 Authorization: Bearer）：
  GET    /api/files                     → 200 {"files":[{"key","url","size","updatedAt"}],"count":N}
  PUT    /<key>  Body: 原文              → 200 {"ok":true,"url":"...","size":N}
  POST   /<key>  Body: 原文              → 同上（兼容）
  DELETE /<key>                          → 200 {"ok":true}
```

- `<key>` 即 URL 路径，如 `subs/cloudflare-ips.txt`、`subs/订阅.txt`（支持目录/中文）。
- 内容存在 Blob `<key>`，元数据（大小/更新时间/url）存在 `__meta/<key>`；列表由 `/api/files` 汇总。
- token 只能在请求头（`x-token` 或 `Authorization: Bearer`），**不进 URL**。CORS 已放开。

## 五、管理面板使用

部署后直接打开首页（`https://<你的域名>/`）：
1. **写入 Token**：填 `WRITE_TOKEN` 并保存（只存本浏览器）。
2. **上传/更新**：填云端路径（如 `subs/cloudflare-ips.txt`）→ 选本地文件 → 上传。
3. **已上传文件**：表格里看路径/大小/更新时间/外部 URL，可一键复制外链、删除。
4. **下载 update.bat**：点击后生成脚本（内含你的域名、路径和 token），本地把文件拖到 bat 上即自动 PUT 覆盖更新。
   - bat 内含 token，**别外传**；改路径/域名直接右键编辑 bat 顶部的 `BASE_URL / KEY / TOKEN`。

## 六、验证

```powershell
# 列出文件（管理）
curl.exe "https://<你的域名>/api/files" -H "x-token: <WRITE_TOKEN>"

# 上传/覆盖
curl.exe -X PUT "https://<你的域名>/subs/cloudflare-ips.txt" -H "x-token: <WRITE_TOKEN>" --data-binary "@sub.txt"

# 公开读取（不带 token）
curl.exe "https://<你的域名>/subs/cloudflare-ips.txt"

# 删除
curl.exe -X DELETE "https://<你的域名>/subs/cloudflare-ips.txt" -H "x-token: <WRITE_TOKEN>"
```

## 七、填入 iptest-web

| iptest-web 配置项 | 填什么 |
|---|---|
| 云端地址 / base_url | `https://<你的域名>`（默认 URL 或自定义域名） |
| Token | `WRITE_TOKEN` |
| 输出文件名 | 如 `subs/cloudflare-ips.txt` |

## 八、限制与说明

- 单请求正文 ≤ **1MB**（Makers 平台限制），代码包 ≤ 5MB，CPU ≤ 200ms。超大文件需另用 Blob `createUploadUrl` 预签名直传（当前版本未内置）。
- 读取默认弹**强一致**，上传后立即可读最新内容。
- `PUBLIC_BASE_URL` 环境变量**可填可不填**：不填时返回的 `url` 用请求自身域名；要固定外链域名才填。
- Blob 数据**持久化在云端对象存储**，免费额度 1GB。