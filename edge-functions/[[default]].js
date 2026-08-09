import { getStore } from "@edgeone/pages-blob";

const STORE_NAME = "edgeone-sync";
const META_PREFIX = "__meta/";
const MAX_BODY_BYTES = 1024 * 1024;
const MAX_KEY_LENGTH = 512;

export async function onRequestGet(context) {
  return run(async () => {
    const pathname = new URL(context.request.url).pathname;
    return pathname === "/api/files" ? listFiles(context) : readFile(context);
  });
}

export async function onRequestPost(context) {
  return run(async () => {
    const pathname = new URL(context.request.url).pathname;
    return pathname === "/api/auth" ? verifyToken(context) : writeFile(context);
  });
}

export async function onRequestPut(context) {
  return run(() => writeFile(context));
}

export async function onRequestDelete(context) {
  return run(() => deleteFile(context));
}

export async function onRequestOptions() {
  return new Response(null, { status: 204, headers: corsHeaders() });
}

async function verifyToken({ request, env }) {
  try {
    const body = await request.json();
    const token = typeof body?.token === "string" ? body.token : "";
    const configured = env?.WRITE_TOKEN || "";
    if (!configured) return json({ ok: false, error: "server_token_not_configured" }, 503);
    if (!sameSecret(token, configured)) return json({ ok: false, error: "invalid_token" }, 401);
    return json({ ok: true }, 200);
  } catch {
    return json({ ok: false, error: "invalid_request" }, 400);
  }
}

async function listFiles({ request, env }) {
  const store = getStore({ name: STORE_NAME, consistency: "strong" });
  const { blobs } = await store.list({ prefix: META_PREFIX, consistency: "strong" });
  const base = (env?.PUBLIC_BASE_URL || new URL(request.url).origin).replace(/\/+$/, "");
  const files = [];
  for (const blob of blobs) {
    const meta = await store.get(blob.key, { type: "json", consistency: "strong" });
    if (!meta || typeof meta.key !== "string") continue;
    files.push({
      key: meta.key,
      size: Number(meta.size) || 0,
      contentType: meta.contentType || "application/octet-stream",
      updatedAt: meta.updatedAt || null,
      url: `${base}/${encodeKey(meta.key)}`,
    });
  }
  files.sort((a, b) => String(b.updatedAt || "").localeCompare(String(a.updatedAt || "")));
  return json({ files, count: files.length });
}

async function readFile({ request }) {
  const key = getKey(request.url);
  if (!key || key.startsWith(META_PREFIX)) return json({ error: "not_found" }, 404);

  const store = getStore({ name: STORE_NAME, consistency: "strong" });
  const meta = await store.get(META_PREFIX + key, { type: "json", consistency: "strong" });
  const contentType = meta?.contentType || guessContentType(key) || "application/octet-stream";

  const content = await store.get(key, { type: "arrayBuffer", consistency: "strong" });
  if (content === null) return json({ error: "not_found" }, 404);

  return new Response(content, {
    headers: {
      "content-type": contentType,
      "content-disposition": "inline",
      "cache-control": "no-store",
      "x-content-type-options": "nosniff",
      ...corsHeaders(),
    },
  });
}

async function writeFile({ request, env }) {
  if (!authorized(request, env)) return json({ error: "invalid_token" }, 401);
  const key = getKey(request.url);
  if (!key) return json({ error: "invalid_key" }, 400);
  if (key.startsWith(META_PREFIX)) return json({ error: "reserved_key" }, 400);

  const declaredLength = Number(request.headers.get("content-length") || 0);
  if (declaredLength > MAX_BODY_BYTES) return json({ error: "file_too_large" }, 413);

  const body = await request.arrayBuffer();
  if (body.byteLength > MAX_BODY_BYTES) return json({ error: "file_too_large" }, 413);

  const contentType = guessContentType(key);
  const requestUrl = new URL(request.url);
  const base = (env?.PUBLIC_BASE_URL || requestUrl.origin).replace(/\/+$/, "");
  const metadata = { key, size: body.byteLength, contentType, updatedAt: new Date().toISOString() };

  const store = getStore({ name: STORE_NAME, consistency: "strong" });
  await store.set(key, body);
  await store.setJSON(META_PREFIX + key, metadata);

  return json({ ok: true, file: { ...metadata, url: `${base}/${encodeKey(key)}` } });
}

async function deleteFile({ request, env }) {
  if (!authorized(request, env)) return json({ error: "invalid_token" }, 401);
  const key = getKey(request.url);
  if (!key) return json({ error: "invalid_key" }, 400);
  if (key.startsWith(META_PREFIX)) return json({ error: "reserved_key" }, 400);
  const store = getStore({ name: STORE_NAME, consistency: "strong" });
  await store.delete(key);
  await store.delete(META_PREFIX + key);
  return json({ ok: true, key });
}

function guessContentType(key) {
  const ext = key.split(".").pop().toLowerCase();
  const map = {
    txt: "text/plain; charset=utf-8", csv: "text/plain; charset=utf-8",
    json: "application/json; charset=utf-8", xml: "application/xml; charset=utf-8",
    html: "text/html; charset=utf-8", htm: "text/html; charset=utf-8",
    css: "text/css; charset=utf-8", js: "application/javascript; charset=utf-8",
    mjs: "application/javascript; charset=utf-8", md: "text/plain; charset=utf-8",
    yaml: "text/plain; charset=utf-8", yml: "text/plain; charset=utf-8",
    png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif",
    svg: "image/svg+xml", webp: "image/webp", ico: "image/x-icon",
    pdf: "application/pdf", zip: "application/zip", gz: "application/gzip",
    mp3: "audio/mpeg", mp4: "video/mp4", avi: "video/x-msvideo",
    woff: "font/woff", woff2: "font/woff2", ttf: "font/ttf", otf: "font/otf",
  };
  return map[ext] || "application/octet-stream";
}

function getKey(requestUrl) {
  const raw = new URL(requestUrl).pathname.replace(/^\/+/, "");
  if (!raw || raw.length > MAX_KEY_LENGTH) return null;
  let key;
  try { key = decodeURIComponent(raw); } catch { return null; }
  if (!key || key.includes("\\") || key.includes("\0")) return null;
  if (/(^|\/)\.\.?($|\/)/.test(key)) return null;
  if ([...key].some((char) => char.charCodeAt(0) < 32)) return null;
  return key;
}

function encodeKey(key) {
  return key.split("/").map((part) => encodeURIComponent(part)).join("/");
}

function authorized(request, env) {
  const configured = env?.WRITE_TOKEN || "";
  if (!configured) return false;
  const xToken = request.headers.get("x-token") || "";
  const authorization = request.headers.get("authorization") || "";
  return sameSecret(xToken, configured) || sameSecret(authorization, `Bearer ${configured}`);
}

function sameSecret(left, right) {
  if (typeof left !== "string" || typeof right !== "string") return false;
  const a = new TextEncoder().encode(left);
  const b = new TextEncoder().encode(right);
  if (a.length !== b.length) return false;
  let result = 0;
  for (let i = 0; i < a.length; i++) result |= a[i] ^ b[i];
  return result === 0;
}

async function run(handler) {
  try { return await handler(); } catch { return json({ error: "server_error" }, 500); }
}

function json(value, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: {
      "content-type": "application/json; charset=utf-8",
      "cache-control": "no-store",
      "x-content-type-options": "nosniff",
      ...corsHeaders(),
    },
  });
}

function corsHeaders() {
  return {
    "access-control-allow-origin": "*",
    "access-control-allow-methods": "GET, POST, PUT, DELETE, OPTIONS",
    "access-control-allow-headers": "content-type, x-token, authorization",
  };
}