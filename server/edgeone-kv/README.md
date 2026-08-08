# server/edgeone-kv — EdgeOne 边缘函数（KV 版）

与 `server/cf-worker` 相同协议（§7），部署在腾讯 EdgeOne：

```
GET    /<key>                    → 200 原文（公开读，无鉴权）
PUT    /<key>  x-token: <token>   → 200 {"url": "...", "size": N}
DELETE /<key>  x-token: <token>   → 200 {"ok": true}
```

## 与 CF 版的区别

- EdgeOne KV 的 key 只允许 `[A-Za-z0-9_]`（≤512B）：cloudsync 客户端会自动把文件名编码为安全 key（`s_xxxx`），所以公开 URL 是编码后的名字；
- EdgeOne KV 只能在边缘函数中使用；免费账户 1 GB；
- KV 最终一致（写入节点立读，全网 ≤60s），客户端回读校验已容忍；
- 需要「目录 + 原文件名」或强一致时，改用 `server/edgeone-blob`。

## 部署步骤（EdgeOne 控制台）

1. 进入 EdgeOne 站点 → **边缘函数** → 创建函数，把 `src/index.js` 内容粘贴进去；
2. **绑定 KV**：命名空间绑定名必须是 `my_kv`（本文件按该名字 `import { my_kv } from "cloudflare:kv"`；若控制台只允许自定义名，同步修改 import 即可）；
3. **环境变量**：添加 `WRITE_TOKEN`（写 token）；可选 `PUBLIC_BASE_URL`（公开基础 URL，如 `https://sub.example.com`）；
4. 发布，并挂到域名路径（如 `https://your-site.edgeone.app/*` 或自定义域名）。

> 说明：EdgeOne 运行时与 Cloudflare Workers API 兼容；若官方对 KV/Blob 的 import 方式有调整，以 [EdgeOne 官方文档](https://edgeone.ai/document) 为准，改动只涉及文件头部两行。

## 验证

```bash
# 上传
curl -X PUT "https://<function-url>/sub_test" -H "x-token: <WRITE_TOKEN>" --data-binary @sub.txt

# 公开读
curl "https://<function-url>/sub_test"

# 无 token 写 → 403
curl -X PUT "https://<function-url>/sub_test" --data hi

# 删除
curl -X DELETE "https://<function-url>/sub_test" -H "x-token: <WRITE_TOKEN>"
```

## 安全说明

- token 仅存在于环境变量；请求必须用 header 携带，禁止放进 URL；
- 公开 URL 无写权限。
