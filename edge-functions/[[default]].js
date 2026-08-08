// edgeone-sync — EdgeOne Makers Edge Function（Blob 持久化存储）
//
// 路由由 Makers 平台根据 edge-functions/ 目录自动生成：
//   edge-functions/[[default]].js  → 匹配根路径下任意多级路径
//
// 公开接口：
//   GET    /<key>                   公开读取（无需 token）
//
// 管理接口（需要 x-token: <WRITE_TOKEN> 或 Authorization: Bearer）：
//   GET    /api/files               列出文件（key/size/updatedAt/url）
//   PUT    /<key>                   上传/覆盖（正文 < 1MB）
//   POST   /<key>                   同上（兼容 POST 客户端）
//   DELETE /<key>                   删除
//
// 环境变量（项目设置 → 环境管理）：
//   WRITE_TOKEN      必填：写操作 token（值 ≤500 字节）
//   PUBLIC_BASE_URL  可选：返回 url 字段的公开前缀；不填则取请求自身域名
//
// 存储说明：
//   getStore() 首次调用会自动创建命名空间，无需在控制台绑定 KV/Blob；
//   数据持久化在云端对象存储（免费额度 1GB），控制台 Blob 页可只读浏览。
//   内容存 <key>，元数据（大小/更新时间/url）存 __meta/<key>，
//   读取默认使用强一致，上传后立即可读到最新内容。
import { getStore } from "@edgeone/pages-blob";

const STORE_NAME = "edgeone-sync";
const META_PREFIX = "__meta/";
const MAX_BODY_BYTES = 1024 * 1024; // Makers 平台请求体上限 1MB

export async function onRequestGet(context) {
  const url = new URL(context.request.url);
  if (url.pathname === "/api/files" || url.pathname === "/api") {
    return handleList(context);
  }
  return handleGet(context);
}

export async function onRequestPut(context) {
  return handleWrite(context);
}

export async function onRequestPost(context) {
  return handleWrite(context);
}

export async function onRequestDelete(context) {
  return handleDelete(context);
}

export async function onRequestOptions() {
  return new Response(null, { status: 204, headers: corsHeaders() });
}

async function handleList({ request, env }) {
  if (!authorized(request, env)) {
    return json({ error: "forbidden: missing or invalid write token" }, 403);
  }

  const store = getStore({ name: STORE_NAME, consistency: "strong" });
  const { blobs } = await store.list({ prefix: META_PREFIX });

  const files = [];
  for (const b of blobs) {
    const meta = await store.get(b.key, { type: "json" });
    if (meta && typeof meta.key === "string") {
      files.push(meta);
    }
  }
  files.sort((a, b) =>
    String(b.updatedAt || "").localeCompare(String(a.updatedAt || ""))
  );

  return json({ files, count: files.length }, 200);
}

async function handleGet({ request }) {
  const key = safeKey(request.url);
  if (key === null || key.startsWith(META_PREFIX)) {
    return json({ error: "not found" }, 404);
  }

  const store = getStore({ name: STORE_NAME, consistency: "strong" });
  const content = await store.get(key);
  if (content === null) {
    return json({ error: "not found" }, 404);
  }

  return new Response(content, {
    headers: {
      "content-type": "text/plain; charset=utf-8",
      "cache-control": "public, max-age=60",
      ...corsHeaders(),
    },
  });
}

async function handleWrite({ request, env }) {
  if (!authorized(request, env)) {
    return json({ error: "forbidden: missing or invalid write token" }, 403);
  }

  const key = safeKey(request.url);
  if (key === null) {
    return json({ error: "invalid key" }, 400);
  }
  if (key.startsWith(META_PREFIX)) {
    return json({ error: "invalid key: reserved prefix" }, 400);
  }

  const declared = Number(request.headers.get("content-length") || 0);
  if (declared > MAX_BODY_BYTES) {
    return json({ error: "payload too large (max 1MB)" }, 413);
  }

  const buf = await request.arrayBuffer();
  if (buf.byteLength > MAX_BODY_BYTES) {
    return json({ error: "payload too large (max 1MB)" }, 413);
  }

  const text = new TextDecoder().decode(buf);
  const base = (env.PUBLIC_BASE_URL || new URL(request.url).origin).replace(/\/+$/, "");
  const publicUrl = `${base}/${encodeURI(key)}`;

  const store = getStore({ name: STORE_NAME, consistency: "strong" });
  await store.set(key, text);
  await store.setJSON(META_PREFIX + key, {
    key,
    url: publicUrl,
    size: buf.byteLength,
    updatedAt: new Date().toISOString(),
  });

  return json({ ok: true, key, url: publicUrl, size: buf.byteLength }, 200);
}

async function handleDelete({ request, env }) {
  if (!authorized(request, env)) {
    return json({ error: "forbidden: missing or invalid write token" }, 403);
  }

  const key = safeKey(request.url);
  if (key === null || key.startsWith(META_PREFIX)) {
    return json({ error: "invalid key" }, 400);
  }

  const store = getStore({ name: STORE_NAME, consistency: "strong" });
  await store.delete(key);
  await store.delete(META_PREFIX + key);
  return json({ ok: true, key }, 200);
}

function authorized(request, env) {
  const token = env.WRITE_TOKEN;
  if (!token) return false;
  if (request.headers.get("x-token") === token) return true;
  const auth = request.headers.get("authorization") || "";
  return auth === `Bearer ${token}`;
}

function safeKey(requestUrl) {
  const raw = new URL(requestUrl).pathname.replace(/^\/+/, "");
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
    "access-control-allow-methods": "GET, PUT, POST, DELETE, OPTIONS",
    "access-control-allow-headers": "content-type, x-token, authorization",
  };
}