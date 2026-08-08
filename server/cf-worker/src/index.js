// cloudsync §7 协议 —— Cloudflare Worker（KV 版）
//
//   GET    /<key>                    → 200 原文（公开读，无鉴权）
//   PUT    /<key>  x-token: <token>   → 200 {"url": "...", "size": N}
//   DELETE /<key>  x-token: <token>   → 200 {"ok": true}
//
// 写 token 用 wrangler secret put WRITE_TOKEN 设置（不进入代码/git）。
// KV 绑定名固定为 KV（见 wrangler.toml）。密钥上限 25 MiB。

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
        await env.KV.put(key, body);
        const publicUrl = (env.PUBLIC_BASE_URL || url.origin) + url.pathname;
        return json({ url: publicUrl, size: body.byteLength, key }, 200);
      }

      case "GET": {
        const value = await env.KV.get(key, "arrayBuffer");
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
        await env.KV.delete(key);
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
