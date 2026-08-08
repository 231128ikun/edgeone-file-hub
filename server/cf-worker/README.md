# server/cf-worker — Cloudflare Worker（KV 版）

实现 cloudsync 的统一协议（§7）：

```
GET    /<key>                    → 200 原文（公开读，无鉴权）
PUT    /<key>  x-token: <token>   → 200 {"url": "...", "size": N}
DELETE /<key>  x-token: <token>   → 200 {"ok": true}
```

## 特性

- 读写分离：读无需 token（订阅链接可直接分享）；写/删必须带 `x-token` 或 `Authorization: Bearer`。
- 正文走请求体（不是 URL 参数），不再有 TEXT2KV 的 65 行限制；单值上限 25 MiB。
- key 支持任意 UTF-8（含中文、`/` 前缀目录），客户端回读校验容忍 KV 最终一致。
- CORS 全开，方便 iptest-web 前端直接调用。

## 部署步骤

前置：安装 [wrangler](https://developers.cloudflare.com/workers/wrangler/) 并登录。

1. 创建 KV namespace（Dashboard → Workers KV，或命令行）：

   ```powershell
   wrangler kv:namespace create CLOUDSYNC_KV
   ```

2. 把返回的 `id` 填进 `wrangler.toml` 的 `[[kv_namespaces]]`（binding 保持 `KV`）。

3. 设置写 token（不进代码、不进 git）：

   ```powershell
   wrangler secret put WRITE_TOKEN
   ```

4. 部署：

   ```powershell
   wrangler deploy
   ```

5. （可选）把订阅域名（如 `sub.example.com`）代理到该 Worker，并在 `wrangler.toml` 的 `[vars]` 填 `PUBLIC_BASE_URL = "https://sub.example.com"`，这样 PUT 返回的 URL 是稳定的自定义域名。

## 本地验证

```powershell
# 上传（必须带 token；body 为原文）
curl.exe -X PUT "https://<worker>/sub/test.txt" -H "x-token: <WRITE_TOKEN>" --data-binary "@sub.txt"

# 公开读（不带 token）
curl.exe "https://<worker>/sub/test.txt"

# 无 token 写 → 403
curl.exe -X PUT "https://<worker>/sub/test.txt" --data "hi"

# 删除
curl.exe -X DELETE "https://<worker>/sub/test.txt" -H "x-token: <WRITE_TOKEN>"
```

## 安全说明

- token 只放在 `WRITE_TOKEN` secret 中；请求必须用 header 携带，禁止放进 URL。
- 公开 URL 无写权限；如需撤销写入，直接替换 `WRITE_TOKEN`。
- 免费额度：CF KV 1 GB 存储；Worker 请求量以 Cloudflare 免费套餐为准。
