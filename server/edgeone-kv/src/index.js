// cloudsync §7 协议 —— EdgeOne 边缘函数（KV 版）
//
//   GET    /<key>                    → 200 原文（公开读，无鉴权）
//   PUT    /<key>  x-token: <token>   → 200 {"url": "...", "size": N}
//   DELETE /<key>  x-token: <token>   → 200 {"ok": true}
//
// 部署要求（EdgeOne 控制台）：
//   1. 创建边缘函数，粘贴本文件；
//   2. 绑定 KV 存储，绑定名必须是 my_kv（本文件按该名字 import）；
//   3. 添加环境变量 WRITE_TOKEN（写 token）；（可选 PUBLIC_BASE_URL）
//   4. 发布并挂到域名路径。
//
// 注意：EdgeOne KV 的 key 只允许 [A-Za-z0-9_] 且 <=512B；cloudsync 客户端
//       会自动把文件名编码为安全 key（如 s_xxxx），因此无需在这里转义。
// KV 是最终一致（<=60s）；客户端回读校验会容忍。
// 需要“目录 + 原文件名 + 强一致”时用 server/edgeone-blob 版。

import { my_kv } from "cloudflare:kv";

const MAX_SIZE = 25 * 1024 * 1024; // 25 MiB

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const method = request.method;

    if (method === "OPTIONS") {
      return new Response(null, { status: 204, headers: corsHeaders() });
    }

    const key = safeKey(url.pathname);
    if (key === null) {
      return json({ error: "invalid key" }, 400);
    }

    switch (method) {
      case "PUT":
      case "POST": {
        if (!authorized(request, env)) {
          return json({ error: "forbidden: missing or invalid write token" }, 403);
        }
        const body = await request.arrayBuffer();
        if (body.byteLength > MAX_SIZE) {
          return json({ error: "payload too large (max 25MB)" }, 413);
        }
        await my_kv.put(key, body);
        const publicUrl = (env.PUBLIC_BASE_URL || url.origin) + url.pathname;
        return json({ url: publicUrl, size: body.byteLength, key }, 200);
      }

      case "GET": {
        const value = await my_kv.get(key);
        if (value === null) {
          return json({ error: "not found" }, 404);
        }
        return new Response(value, {
          headers: {
            "content-type": "text/plain; charset=utf-8",
            "cache-control": "public, max-age=60",
            ...corsHeaders(),
          },
        });
      }

      case "DELETE": {
        if (!authorized(request, env)) {
          return json({ error: "forbidden: missing or invalid write token" }, 403);
        }
        await my_kv.delete(key);
        return json({ ok: true, key }, 200);
      }

      default:
        return json({ error: `method ${method} not allowed` }, 405);
    }
  },
};

function authorized(request, env) {
  const token = env.WRITE_TOKEN;
  if (!token) return false;
  if (request.headers.get("x-token") === token) return true;
  const auth = request.headers.get("authorization") || "";
  return auth === `Bearer ${token}`;
}

function safeKey(pathname) {
  const raw = pathname.replace(/^\/+/, "");
  if (!raw) return null;
  try {
    return decodeURIComponent(raw);
  } catch {
    return null;
  }
}

function json(obj, status) {
  return new Response(JSON.stringify(obj), {
    status,
    headers: { "content-type": "application/json; charset=utf-8", ...corsHeaders() },
  });
}

function corsHeaders() {
  return {
    "access-control-allow-origin": "*",
    "access-control-allow-methods": "GET,PUT,POST,DELETE,OPTIONS",
    "access-control-allow-headers": "content-type,x-token,authorization",
  };
}
