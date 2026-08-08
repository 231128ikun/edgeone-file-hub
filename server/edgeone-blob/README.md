# server/edgeone-blob — EdgeOne 边缘函数（Blob 版）

与 CF/KV 版相同协议（§7），但底层用 EdgeOne **Blob 对象存储**：

```
GET    /<key>                    → 200 原文（公开读，无鉴权）
PUT    /<key>  x-token: <token>   → 200 {"url": "...", "size": N}
DELETE /<key>  x-token: <token>   → 200 {"ok": true}
```

## 与 KV 版的区别

| 项目 | EdgeOne KV | EdgeOne Blob（本目录） |
|---|---|---|
| key 字符 | 仅 `[A-Za-z0-9_]` | 任意（目录 `/`、中文、点号都可以） |
| 绑定 | 需在控制台绑定 KV | `getStore()` 免绑定 |
| 一致性 | 最终一致 ≤60s | 强一致（写完立读） |
| 使用位置 | 仅边缘函数 | 边缘函数 / 云函数 |

所以想要「`subs/ip.txt`、中文文件名、写完立刻回读一致」时优先 Blob。

## 部署步骤（EdgeOne 控制台）

1. 进入 EdgeOne 站点 → **边缘函数** → 创建函数，粘贴 `src/index.js`；
2. 无需绑定存储（Blob 免绑定）；
3. **环境变量**：添加 `WRITE_TOKEN`；可选 `PUBLIC_BASE_URL`；
4. 发布并挂到域名路径。

> 若运行时不识别 `import { getStore } from "cloudflare:blob"`，把该行换成全局 `getStore()`（两种写法选一种）。API 细节以 [EdgeOne 官方文档](https://edgeone.ai/document) 为准。

## 验证

```bash
# 上传（key 含目录与中文都没问题）
curl -X PUT "https://<function-url>/subs/订阅.txt" -H "x-token: <WRITE_TOKEN>" --data-binary @sub.txt

# 公开读（URL 与客户端返回一致）
curl "https://<function-url>/subs/订阅.txt"

# 无 token 写 → 403
curl -X PUT "https://<function-url>/subs/订阅.txt" --data hi

# 删除
curl -X DELETE "https://<function-url>/subs/订阅.txt" -H "x-token: <WRITE_TOKEN>"
```

## 安全说明

- token 仅存在于环境变量；请求必须用 header 携带，禁止放进 URL；
- 公开 URL 无写权限。
