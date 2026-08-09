# 外部项目对接：上传文件到 File Hub

任何项目只要拿到 **站点地址** 和 **写入 Token**，就可以通过 HTTP 上传文件，
并获得一个公开可访问的远程 URL（例如 `https://你的站点/iptest/result.txt`）。

## 你需要准备的两样东西

| 参数 | 说明 | 怎么拿 |
| --- | --- | --- |
| `BASE_URL` | 这个站点部署后的根地址，如 `https://files.example.com` | 部署完成后的访问地址 / 自定义域名 |
| `WRITE_TOKEN` | 写入 Token，也就是项目环境变量里配的 `WRITE_TOKEN` 的值 | EdgeOne Makers → 项目设置 → 环境变量 |

## 核心接口（一条就够）

```
POST {BASE_URL}/{云端路径}
Authorization: Bearer {WRITE_TOKEN}

body = 文件原始字节（任意文本 / JSON / 二进制，≤ 1MB）
```

- `{云端路径}` 支持文件夹：如 `iptest/2026-08-09.txt`
- 同名路径再次上传 = 覆盖更新，不会产生重复文件
- 成功响应：

```json
{ "ok": true, "file": { "key": "iptest/result.txt", "size": 123, "contentType": "text/plain; charset=utf-8", "url": "https://站点/iptest/result.txt" } }
```

- 公开读取（无需 Token）：`GET {BASE_URL}/{key}`，直接命中返回文件内容

## 各语言示例

### curl（Windows / Linux 都可用）

```bash
BASE=https://files.example.com
TOKEN=你的WRITE_TOKEN

# 本地上传
curl -X POST "$BASE/iptest/result.txt" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: text/plain; charset=utf-8" \
  --data-binary "@result.txt"
```

### PowerShell

```powershell
$body = [System.IO.File]::ReadAllBytes("result.txt")
$resp = Invoke-RestMethod -Method Post -Uri "$BASE/iptest/result.txt" `
  -Headers @{ Authorization = "Bearer $TOKEN" } `
  -ContentType "text/plain; charset=utf-8" `
  -Body $body
$resp.file.url   # 公开访问 URL
```

### Python（标准库，无第三方依赖）

```python
import json
import urllib.request

BASE = "https://files.example.com"
TOKEN = "你的WRITE_TOKEN"
KEY = "iptest/result.txt"

with open("result.txt", "rb") as f:
    content = f.read()

req = urllib.request.Request(
    f"{BASE}/{KEY}",
    data=content,
    headers={
        "Authorization": f"Bearer {TOKEN}",
        "Content-Type": "text/plain; charset=utf-8",
    },
    method="POST",
)
with urllib.request.urlopen(req) as resp:
    data = json.loads(resp.read())
print(data["file"]["url"])
```

### Node.js（Node 18+ 内置 fetch）

```js
const BASE = "https://files.example.com";
const TOKEN = "你的-write-token";
const KEY = "iptest/result.txt";

const content = await fs.readFile("result.txt", "utf8"); // 或直接传 Buffer
const resp = await fetch(`${BASE}/${KEY}`, {
  method: "POST",
  headers: {
    "Authorization": `Bearer ${TOKEN}`,
    "Content-Type": "text/plain; charset=utf-8",
  },
  body: content,
});
const data = await resp.json();
console.log(data.file.url); // 公开访问 URL
```

### Go（与 iptest-web 完全一致的写法）

```go
func Upload(ctx context.Context, base, token, key string, content []byte) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimRight(base, "/")+"/"+key,
		bytes.NewReader(content),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("upload failed: HTTP %d", resp.StatusCode)
	}
	// 生成公开读取 URL（每段路径各自 URL 编码）
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.TrimRight(base, "/") + "/" + strings.Join(parts, "/"), nil
}
```

## 其他有用的接口

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/files` | 文件列表（公开），可用来确认上传是否成功 |
| `GET` | `/{key}` | 公开读取文件内容 |
| `POST` | `/api/auth` | 校验 Token（body: `{"token":"..."}`），无副作用 |
| `POST` | `/api/proxy` | 拉取远程 URL 并保存为云端文件（body: `{"url":"...","key":"..."}`） |
| `DELETE` | `/{key}` | 删除文件 |
| `PUT` | `/api/notes?key=...` | 给文件加备注（body: `{"content":"..."}`） |

鉴权除 `Authorization: Bearer <token>` 外，旧客户端也可用 `x-token: <token>`（推荐 Bearer）。

## 注意

- 单文件上限 **1 MB**（EdgeOne Makers 平台限制），超限返回 HTTP 413。
- 云端路径规则：不能含 `..`、`\`、控制字符；`__meta/` 为系统保留目录。
- 上传成功即覆盖同名文件，首页/文件列表会自动同步显示。
- 已接入参考实现：`iptest-web` 项目的 `internal/cloud/edgeone.go`
  （`EdgeOneChannel.Upload`），以及其对外的 `POST /api/cloud/upload`。
- 浏览器跨域已放开（`Access-Control-Allow-Origin: *`），前端也可直接用 fetch 上传。