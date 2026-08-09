# 云端文件

一个部署在 **EdgeOne Makers** 上的轻量文件服务：

- 任何人可以在主页查看文件列表、上传时间和公开地址
- 只有管理员 Token 可以上传、更新和删除文件
- 管理员验证 Token 后，可以生成一个带 Token 的 `update.bat`
- 文件内容保存在 EdgeOne Makers Blob，持久化在云端

## 项目结构

```text
cloudsync/
├── index.html
├── package.json
├── edge-functions/
│   └── [[default]].js
└── README.md
```

`index.html` 是简洁的文件列表和管理页面；`edge-functions/[[default]].js` 是 Makers Edge Function。Makers 会自动识别 `edge-functions/`，不需要在控制台另找“边缘函数”入口。

## 部署

推荐使用 **导入 Git 仓库**，不要使用直接上传 ZIP。因为 Blob SDK 需要在部署时安装依赖。

1. 将此项目推送到 GitHub。
2. EdgeOne Makers → **创建项目** → **导入 Git 仓库** → 选择此仓库。
3. 构建设置填写：
   - 根目录：`/`
   - 构建命令：`npm run build`
   - 输出目录：`/`
4. 开始部署，记下 Makers 提供的访问地址。
5. 进入 **项目设置 → 环境管理 → 生产环境 → 环境变量**，添加：

   ```text
   WRITE_TOKEN=你自己设置的一串随机字符
   ```

6. 修改环境变量后，必须重新部署一次，新的 Token 才会生效。

> `PUBLIC_BASE_URL` 不需要设置。系统会自动使用当前访问域名生成公开地址。如果以后绑定了自定义域名，重新上传一次文件即可生成新的地址。

## 使用网页

打开部署后的首页：

```text
https://你的域名/
```

### 普通访问

首页直接显示：

- 文件名
- 文件大小
- 上传/更新时间
- 外部公开地址

点击公开地址即可读取文件内容。普通访问者不会看到上传、删除和 BAT 按钮。

### 管理文件

1. 点击右上角 **管理**。
2. 输入 EdgeOne 环境变量中的 `WRITE_TOKEN`。
3. Token 验证通过后才会显示管理区域和文件操作按钮。
4. 选择本地文件，填写云端文件名，点击 **上传 / 更新**。
5. 如果云端文件名已经存在，上传会覆盖旧文件，并更新上传时间。
6. 文件列表中的：
   - **更新**：选择新文件覆盖当前文件
   - **BAT**：下载当前文件专用的一键更新脚本
   - **删除**：删除文件和它的公开地址

Token 只保存在当前浏览器标签页的 `sessionStorage` 中，关闭标签页后需要重新输入。网页不会把 Token 写入 URL，也不会显示或返回 Token。

## update.bat

验证 Token 后，在对应文件的操作栏点击 **BAT**，会下载一个专用脚本。

使用时可以：

- 将新的文件直接拖到这个 BAT 文件上；或
- 双击 BAT，再输入文件路径。

脚本会自动把文件上传到对应的云端地址并覆盖更新。脚本中包含 Token，请不要发给别人。

电脑上的 `curl.exe` 通常已经随 Windows 提供；如果提示找不到 curl，需要安装 curl 或改用网页上传。

## 外部读取

文件公开读取，不需要 Token：

```text
https://你的域名/文件名
```

例如云端文件名为 `docs/example.txt`：

```text
https://你的域名/docs/example.txt
```

网页文件列表中的公开地址可以直接复制给别人使用。

## 接口

```text
GET    /api/files       公开列出文件
GET    /<key>           公开读取文件
POST   /api/auth        验证管理 Token
PUT    /<key>           上传或覆盖文件
POST   /<key>           上传或覆盖文件
DELETE /<key>           删除文件
```

上传和删除请求必须带以下任意一种请求头：

```text
x-token: WRITE_TOKEN
```

或：

```text
Authorization: Bearer WRITE_TOKEN
```

## 防护设计

为了保持操作简单，采用单一管理员 Token：

- 文件读取和文件列表公开，方便外部使用
- 上传、更新、删除由后端强制验证 Token
- 前端隐藏按钮只负责简化界面，不承担真正安全职责
- Token 只存在 EdgeOne 环境变量和管理员当前会话中
- Token 不放在 URL
- `__meta/` 为系统元数据目录，不能作为普通文件写入
- 禁止 `..`、反斜杠、控制字符和危险文件名
- 上传内容统一按普通文本读取，并返回 `nosniff` 安全头
- 单个请求最大 1MB，这是 Makers Edge Functions 的平台限制

任何人即使自己构造 HTTP 请求，也无法在没有 `WRITE_TOKEN` 的情况下上传、覆盖或删除文件。

## Blob 说明

代码中使用的存储空间名称是 `edgeone-sync`，这是为了保留之前版本已经上传的文件。第一次调用 Blob 时会自动创建该命名空间，不需要手动绑定 KV 或 Blob。

Blob 数据持久化在云端对象存储。EdgeOne Makers Blob 免费版存储额度以控制台当前显示为准。