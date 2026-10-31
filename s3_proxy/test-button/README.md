# 测试按钮补丁

`proxy_url` 输入框右侧的「测试」按钮要同时改后端和前端，只安装驱动源码不会出现这个按钮。
补丁基于 OpenList `v4.2.9` / OpenList-Frontend `v4.2.9`。

## 后端

在 OpenList 源码根目录执行：

```bash
git apply /path/to/test-button/backend/server/router.go.patch
cp /path/to/test-button/backend/server/handles/s3proxy.go server/handles/
```

新增接口 `POST /api/admin/s3proxy/test`（管理员权限，沿用 `/api/admin` 的鉴权）：

```json
// 请求
{ "proxy_url": "socks5://127.0.0.1:1080", "endpoint": "https://s3.example.com" }

// 成功
{ "code": 200, "message": "success", "data": { "target": "s3.example.com:443", "latency_ms": 123 } }
```

接口会通过 SOCKS5 实际连接 `endpoint`（默认端口 HTTPS 443 / HTTP 80，10 秒超时），
能连上才返回成功，不是只探测代理端口。

## 前端

在 OpenList-Frontend 源码根目录执行：

```bash
git apply /path/to/test-button/frontend/Item.tsx.patch
git apply /path/to/test-button/frontend/AddOrEdit.tsx.patch
python3 /path/to/test-button/frontend/inject_s3proxy_i18n.py src/lang
pnpm install && pnpm build
```

- `Item.tsx`：给字符串字段加 `rightSlot`，把输入框和按钮放进同一个 `HStack`。
- `AddOrEdit.tsx`：只有 `driver === "S3 Proxy"` 且字段是 `proxy_url` 时才传按钮。
- `inject_s3proxy_i18n.py`：给 `src/lang/` 里已有的语言补上驱动词条和「测试」提示；
  新增语言后重跑一次即可，已有的值不会被覆盖。

最后把 `dist/` 覆盖到 OpenList 的 `public/dist/`，再重新编译后端并替换二进制，
否则前端改动不会嵌进程序。
