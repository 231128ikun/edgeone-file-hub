// cloudsync §7 协议 —— EdgeOne 边缘函数（Blob 版）
//
//   GET    /<key>                    → 200 原文（公开读，无鉴权）
//   PUT    /<key>  x-token: <token>   → 200 {"url": "...", "size": N}
//   DELETE /<key>  x-token: <token>   → 200 {"ok": true}
//
// 与 KV 版的区别：
//   - Blob 免绑定（无需在控制台绑定存储），getStore() 直接可用；
//   - 支持目录/中文/带点文件名（key 形如 subs/filename.txt）；
//   - 强一致，写完立读即可看到新内容；
//   - 单对象上限 25 MiB。
//
// 部署要求（EdgeOne 控制台）：
//   1. 创建边缘函数，粘贴本文件；
//   2. 添加环境变量 WRITE_TOKEN（写 token）；（可选 PUBLIC_BASE_URL）
//   3. 发布并挂到域名路径。
//
// 若运行时未提供 `import { getStore } from "cloudflare:blob"`，
// 可改用全局 getStore()（两种写法选一种，勿同时使用）。

import { getStore } from "cloudflare:blob";

const MAX_SIZE = 25 * 1024 * 1024; // 25 MiB

const store = getStore();

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
        await store.set(key, body);
        const publicUrl = (env.PUBLIC_BASE_URL || url.origin) + url.pathname;
        return json({ url: publicUrl, size: body.byteLength, key }, 200);
      }

      case "GET": {
        const file = await store.get(key);
        if (!file) {
          return json({ error: "not found" }, 404);
        }
        return new Response(file.body, {
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
        await store.delete(key);
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
