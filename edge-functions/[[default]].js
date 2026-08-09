import { getStore } from "@edgeone/pages-blob";

const STORE_NAME = "edgeone-sync";
const META_PREFIX = "__meta/";
const ID_PREFIX = "__id/";
const MAX_BODY_BYTES = 1024 * 1024;
const MAX_KEY_LENGTH = 512;
const MAX_REDIRECTS = 5;
const PROXY_TIMEOUT_MS = 15000;

export async function onRequestGet(context) {
  return run(async () => {
    const pathname = new URL(context.request.url).pathname;
    if (pathname === "/api/files") return listFiles(context);
    if (pathname === "/api/proxy") return proxyRead(context);
    return readFile(context);
  });
}

export async function onRequestPost(context) {
  return run(async () => {
    const pathname = new URL(context.request.url).pathname;
    if (pathname === "/api/auth") return verifyToken(context);
    if (pathname === "/api/proxy") return proxyPull(context);
    return writeFile(context);
  });
}

export async function onRequestPut(context) {
  return run(async () => {
    const pathname = new URL(context.request.url).pathname;
    if (pathname === "/api/notes") return saveNote(context);
    return writeFile(context);
  });
}

export async function onRequestDelete(context) {
  return run(async () => {
    const pathname = new URL(context.request.url).pathname;
    if (pathname === "/api/notes") return deleteNote(context);
    return deleteFile(context);
  });
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
    const prevCode = meta.code;
    const code = await ensureFileCode(store, meta);
    if (code !== prevCode) await store.setJSON(META_PREFIX + meta.key, meta);
    files.push({
      key: meta.key,
      size: Number(meta.size) || 0,
      contentType: meta.contentType || "application/octet-stream",
      updatedAt: meta.updatedAt || null,
      source: meta.source || null,
      sourceLabel: sourceLabelOf(meta),
      note: typeof meta.note === "string" ? meta.note : "",
      code,
      codeUrl: `${base}/${code}`,
      url: `${base}/${encodeKey(meta.key)}`,
    });
  }
  files.sort((a, b) => String(b.updatedAt || "").localeCompare(String(a.updatedAt || "")));
  return json({ files, count: files.length });
}

function sourceLabelOf(meta) {
  const source = typeof meta?.source === "string" ? meta.source : "";
  if (source === "manual") return "手动输入";
  if (/^https?:\/\//i.test(source)) return "远程拉取";
  return "本地上传";
}

async function readFile({ request }) {
  const rawKey = getKey(request.url);
  if (!rawKey || rawKey.startsWith(META_PREFIX) || rawKey.startsWith(ID_PREFIX)) {
    return json({ error: "not_found" }, 404);
  }

  const store = getStore({ name: STORE_NAME, consistency: "strong" });
  let key = rawKey;
  let viaCode = false;
  if (isCodeFormat(rawKey)) {
    const mapping = await store.get(ID_PREFIX + rawKey, { type: "json", consistency: "strong" });
    if (mapping && typeof mapping.key === "string" && !mapping.key.startsWith(META_PREFIX) && !mapping.key.startsWith(ID_PREFIX)) {
      key = mapping.key;
      viaCode = true;
    }
  }

  const meta = await store.get(META_PREFIX + key, { type: "json", consistency: "strong" });
  const contentType = meta?.contentType || guessContentType(key) || "application/octet-stream";

  const content = await store.get(key, { type: "arrayBuffer", consistency: "strong" });
  if (content === null) return json({ error: "not_found" }, 404);

  return new Response(content, {
    headers: {
      "content-type": contentType,
      "content-disposition": viaCode ? `inline; filename="${rawKey}"` : "inline",
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
  if (key.startsWith(META_PREFIX) || key.startsWith(ID_PREFIX)) return json({ error: "reserved_key" }, 400);

  const declaredLength = Number(request.headers.get("content-length") || 0);
  if (declaredLength > MAX_BODY_BYTES) return json({ error: "file_too_large" }, 413);

  const body = await request.arrayBuffer();
  if (body.byteLength > MAX_BODY_BYTES) return json({ error: "file_too_large" }, 413);

  const sourceHeader = (request.headers.get("x-source") || "").trim();
  let source = null;
  if (sourceHeader === "manual") source = "manual";
  else if (/^https?:\/\//i.test(sourceHeader)) source = sourceHeader;
  const contentType = guessContentType(key);
  const requestUrl = new URL(request.url);
  const base = (env?.PUBLIC_BASE_URL || requestUrl.origin).replace(/\/+$/, "");
  const metadata = { key, size: body.byteLength, contentType, updatedAt: new Date().toISOString() };
  if (source) metadata.source = source;

  const store = getStore({ name: STORE_NAME, consistency: "strong" });
  const oldMeta = await store.get(META_PREFIX + key, { type: "json", consistency: "strong" });
  preserveNote(oldMeta, metadata);
  const code = await resolveCode(store, request, oldMeta, metadata);
  await store.setJSON(ID_PREFIX + code, { key });
  await store.set(key, body);
  await store.setJSON(META_PREFIX + key, metadata);

  return json({ ok: true, file: { ...metadata, code, url: `${base}/${encodeKey(key)}`, codeUrl: `${base}/${code}` } });
}

async function deleteFile({ request, env }) {
  if (!authorized(request, env)) return json({ error: "invalid_token" }, 401);
  const key = getKey(request.url);
  if (!key) return json({ error: "invalid_key" }, 400);
  if (key.startsWith(META_PREFIX) || key.startsWith(ID_PREFIX)) return json({ error: "reserved_key" }, 400);
  const store = getStore({ name: STORE_NAME, consistency: "strong" });
  const meta = await store.get(META_PREFIX + key, { type: "json", consistency: "strong" });
  if (meta && typeof meta.code === "string") {
    const mapping = await store.get(ID_PREFIX + meta.code, { type: "json", consistency: "strong" });
    if (mapping && mapping.key === key) await store.delete(ID_PREFIX + meta.code);
  }
  await store.delete(key);
  await store.delete(META_PREFIX + key);
  return json({ ok: true, key });
}

// POST /api/proxy  { url, key? } —— 拉取远程地址并保存为云端文件（需 Token）
async function proxyPull({ request, env }) {
  if (!authorized(request, env)) return json({ error: "invalid_token" }, 401);
  let url = "", key = "";
  try {
    const body = await request.json();
    url = typeof body?.url === "string" ? body.url.trim() : "";
    key = typeof body?.key === "string" ? body.key.trim() : "";
  } catch {
    return json({ error: "invalid_request" }, 400);
  }

  const target = normalizeRemoteUrl(url);
  if (!target) return json({ error: "invalid_url" }, 400);
  if (isBlockedProxyTarget(target)) return json({ error: "blocked_url" }, 403);

  key = key || remoteBaseName(target);
  const normalized = normalizeKey(key);
  if (!normalized || normalized.startsWith(META_PREFIX) || normalized.startsWith(ID_PREFIX)) return json({ error: "invalid_key" }, 400);

  const remote = await fetchRemote(target);
  if (!remote.ok) {
    if (remote.status === 413) return json({ error: "file_too_large" }, 413);
    if (remote.status === 403) return json({ error: "blocked_url" }, 403);
    return json({ error: remote.error || "remote_fetch_failed", status: remote.status || 502 }, 502);
  }

  const contentType = guessContentType(normalized);
  const requestUrl = new URL(request.url);
  const base = (env?.PUBLIC_BASE_URL || requestUrl.origin).replace(/\/+$/, "");
  const metadata = { key: normalized, size: remote.size, contentType, updatedAt: new Date().toISOString(), source: target };

  const store = getStore({ name: STORE_NAME, consistency: "strong" });
  const oldMeta = await store.get(META_PREFIX + normalized, { type: "json", consistency: "strong" });
  preserveNote(oldMeta, metadata);
  const code = await resolveCode(store, request, oldMeta, metadata);
  await store.setJSON(ID_PREFIX + code, { key: normalized });
  await store.set(normalized, remote.buffer);
  await store.setJSON(META_PREFIX + normalized, metadata);

  return json({ ok: true, file: { ...metadata, code, url: `${base}/${encodeKey(normalized)}`, codeUrl: `${base}/${code}` } });
}

// GET /api/proxy?url=... —— 反代读取远程内容（不落盘）
// 默认公开；环境变量 PUBLIC_PROXY=off 时改为需要 Token
async function proxyRead({ request, env }) {
  if (proxyRequiresToken(env) && !authorized(request, env)) return json({ error: "invalid_token" }, 401);
  const raw = new URL(request.url).searchParams.get("url") || "";
  const target = normalizeRemoteUrl(raw);
  if (!target) return json({ error: "invalid_url" }, 400);
  if (isBlockedProxyTarget(target)) return json({ error: "blocked_url" }, 403);

  const remote = await fetchRemote(target);
  if (!remote.ok) {
    if (remote.status === 413) return json({ error: "file_too_large" }, 413);
    if (remote.status === 403) return json({ error: "blocked_url" }, 403);
    return json({ error: remote.error || "remote_fetch_failed", status: remote.status || 502 }, 502);
  }

  const contentType = remote.contentType || guessContentType(target) || "application/octet-stream";
  return new Response(remote.buffer, {
    headers: {
      "content-type": contentType,
      "cache-control": "no-store",
      "x-content-type-options": "nosniff",
      ...corsHeaders(),
    },
  });
}

// PUT /api/notes?key=... —— 保存 / 清除文件备注（需 Token；备注随元数据持久化，访客只读）
async function saveNote({ request, env }) {
  if (!authorized(request, env)) return json({ error: "invalid_token" }, 401);
  const key = normalizeKey(new URL(request.url).searchParams.get("key") || "");
  if (!key || key.startsWith(META_PREFIX) || key.startsWith(ID_PREFIX)) return json({ error: "invalid_key" }, 400);
  let content = "";
  try {
    const body = await request.json();
    content = typeof body?.content === "string" ? body.content.trim() : "";
  } catch {
    return json({ error: "invalid_request" }, 400);
  }
  if (content.length > 2000) return json({ error: "note_too_long" }, 413);

  const store = getStore({ name: STORE_NAME, consistency: "strong" });
  const meta = await store.get(META_PREFIX + key, { type: "json", consistency: "strong" });
  if (!meta || typeof meta.key !== "string") return json({ error: "file_not_found" }, 404);

  if (!content) {
    delete meta.note;
    delete meta.noteUpdatedAt;
  } else {
    meta.note = content;
    meta.noteUpdatedAt = new Date().toISOString();
  }
  await store.setJSON(META_PREFIX + key, meta);
  return json({ ok: true, note: content ? { key, content, updatedAt: meta.noteUpdatedAt } : null });
}

// DELETE /api/notes?key=... —— 清除文件备注（需 Token）
async function deleteNote({ request, env }) {
  if (!authorized(request, env)) return json({ error: "invalid_token" }, 401);
  const key = normalizeKey(new URL(request.url).searchParams.get("key") || "");
  if (!key || key.startsWith(META_PREFIX) || key.startsWith(ID_PREFIX)) return json({ error: "invalid_key" }, 400);
  const store = getStore({ name: STORE_NAME, consistency: "strong" });
  const meta = await store.get(META_PREFIX + key, { type: "json", consistency: "strong" });
  if (meta && typeof meta.key === "string") {
    delete meta.note;
    delete meta.noteUpdatedAt;
    await store.setJSON(META_PREFIX + key, meta);
  }
  return json({ ok: true });
}

// 覆盖写入 / 远程刷新时保留原备注，避免误删
function preserveNote(oldMeta, metadata) {
  if (!oldMeta) return;
  if (typeof oldMeta.note === "string") metadata.note = oldMeta.note;
  if (typeof oldMeta.noteUpdatedAt === "string") metadata.noteUpdatedAt = oldMeta.noteUpdatedAt;
}

// 固定编码：随机 10 位 base62，随文件元数据持久化；移动 / 覆盖 / 刷新都不会变
function isCodeFormat(value) {
  return typeof value === "string" && /^[A-Za-z0-9]{10}$/.test(value);
}

function generateCode() {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let s = "";
  if (typeof crypto !== "undefined" && crypto.getRandomValues) {
    const bytes = new Uint8Array(10);
    crypto.getRandomValues(bytes);
    for (let i = 0; i < 10; i++) s += alphabet[bytes[i] % alphabet.length];
  } else {
    for (let i = 0; i < 10; i++) s += alphabet[Math.floor(Math.random() * alphabet.length)];
  }
  return s;
}

// 给元数据分配固定编码并写索引；已有编码直接复用（老文件首次列出时自动补齐）
async function ensureFileCode(store, meta) {
  if (typeof meta.code === "string" && isCodeFormat(meta.code)) return meta.code;
  for (let attempt = 0; attempt < 8; attempt++) {
    const code = generateCode();
    const existing = await store.get(ID_PREFIX + code, { type: "json", consistency: "strong" });
    if (!existing) {
      meta.code = code;
      await store.setJSON(ID_PREFIX + code, { key: meta.key });
      return code;
    }
  }
  throw new Error("code_generation_failed");
}

// 移动 / 覆盖 / 刷新时复用编码：x-code 头（管理员移动）> 旧元数据 > 新生成
async function resolveCode(store, request, oldMeta, metadata) {
  const header = (request.headers.get("x-code") || "").trim();
  if (isCodeFormat(header)) {
    metadata.code = header;
    return header;
  }
  if (oldMeta && isCodeFormat(oldMeta.code)) {
    metadata.code = oldMeta.code;
    return oldMeta.code;
  }
  return ensureFileCode(store, metadata);
}

function proxyRequiresToken(env) {
  const value = String(env?.PUBLIC_PROXY || "").trim().toLowerCase();
  if (!value) return false;
  return !(value === "on" || value === "true" || value === "1" || value === "yes" || value === "public");
}

// 抓取远端内容：跟随重定向（每跳都做安全检查），内容最大 1MB
async function fetchRemote(url) {
  let current = url;
  try {
    for (let hop = 0; hop <= MAX_REDIRECTS; hop++) {
      const res = await fetchWithTimeout(current);
      const status = res.status;
      if (status === 301 || status === 302 || status === 303 || status === 307 || status === 308) {
        const location = res.headers.get("location");
        if (!location) return { ok: false, status: 502, error: "remote_fetch_failed" };
        const next = new URL(location, current).toString();
        if (isBlockedProxyTarget(next)) return { ok: false, status: 403, error: "blocked_url" };
        current = next;
        continue;
      }
      if (!res.ok) return { ok: false, status: res.status, error: "remote_fetch_failed" };
      const declared = Number(res.headers.get("content-length") || 0);
      if (declared > MAX_BODY_BYTES) return { ok: false, status: 413, error: "file_too_large" };
      const buffer = await res.arrayBuffer();
      if (buffer.byteLength > MAX_BODY_BYTES) return { ok: false, status: 413, error: "file_too_large" };
      return { ok: true, buffer, size: buffer.byteLength, contentType: res.headers.get("content-type") || "" };
    }
    return { ok: false, status: 502, error: "too_many_redirects" };
  } catch {
    return { ok: false, status: 502, error: "remote_fetch_failed" };
  }
}

async function fetchWithTimeout(url) {
  const init = { redirect: "manual" };
  if (typeof AbortSignal !== "undefined" && typeof AbortSignal.timeout === "function") {
    init.signal = AbortSignal.timeout(PROXY_TIMEOUT_MS);
  }
  return fetch(url, init);
}

function normalizeRemoteUrl(raw) {
  let value = String(raw || "").trim();
  if (!value) return null;
  if (!/^[a-z][a-z0-9+.-]*:\/\//i.test(value)) value = "https://" + value;
  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    return null;
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return null;
  return parsed.toString();
}

// 禁止把本站当跳板访问内网 / 本机地址
function isBlockedProxyTarget(url) {
  try {
    const parsed = new URL(url);
    if (parsed.username || parsed.password) return true;
    const host = parsed.hostname.replace(/^\[|\]$/g, "").toLowerCase();
    if (!host) return true;
    if (host === "localhost" || host.endsWith(".localhost")) return true;
    if (host.endsWith(".local") || host.endsWith(".lan") || host.endsWith(".internal") || host.endsWith(".home.arpa")) return true;
    if (host.includes(":")) return isPrivateIpv6(host);
    if (/^[\d.]+$/.test(host)) return isPrivateIpv4(host);
    return false;
  } catch {
    return true;
  }
}

function isPrivateIpv4(ip) {
  const parts = ip.split(".").map(Number);
  if (parts.length !== 4 || parts.some((n) => Number.isNaN(n) || n < 0 || n > 255)) return true;
  const a = parts[0], b = parts[1];
  if (a === 0 || a === 10 || a === 127) return true;
  if (a === 100 && b >= 64 && b <= 127) return true; // CGNAT
  if (a === 169 && b === 254) return true; // link-local（含云元数据地址）
  if (a === 172 && b >= 16 && b <= 31) return true;
  if (a === 192 && b === 168) return true;
  if (a === 198 && (b === 18 || b === 19)) return true; // benchmarking
  if (a >= 224) return true; // 组播 / 保留
  return false;
}

function isPrivateIpv6(host) {
  const lower = host.toLowerCase();
  if (lower === "::" || lower === "::1" || lower.startsWith("fe80:") || lower.startsWith("fc") || lower.startsWith("fd")) return true;
  const mapped = lower.match(/::ffff:(\d+\.\d+\.\d+\.\d+)$/);
  return mapped ? isPrivateIpv4(mapped[1]) : false;
}

function remoteBaseName(url) {
  try {
    const path = new URL(url).pathname.split("/").filter(Boolean).pop();
    return path ? decodeURIComponent(path) : "remote.txt";
  } catch {
    return "remote.txt";
  }
}

function guessContentType(key) {
  const ext = key.split(".").pop().toLowerCase();
  const map = {
    txt: "text/plain; charset=utf-8", csv: "text/plain; charset=utf-8",
    json: "application/json; charset=utf-8", xml: "application/xml; charset=utf-8",
    html: "text/html; charset=utf-8", htm: "text/html; charset=utf-8",
    css: "text/css; charset=utf-8", js: "application/javascript; charset=utf-8",
    mjs: "application/javascript; charset=utf-8", md: "text/plain; charset=utf-8",
    ini: "text/plain; charset=utf-8", conf: "text/plain; charset=utf-8", log: "text/plain; charset=utf-8",
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
  return normalizeKey(raw);
}

function normalizeKey(raw) {
  if (!raw || raw.length > MAX_KEY_LENGTH) return null;
  let key;
  try {
    key = decodeURIComponent(raw);
  } catch {
    return null;
  }
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
  try {
    return await handler();
  } catch {
    return json({ error: "server_error" }, 500);
  }
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
    "access-control-allow-headers": "content-type, x-token, x-source, x-code, authorization",
  };
}