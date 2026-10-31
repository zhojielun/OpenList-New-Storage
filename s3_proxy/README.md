# S3 Proxy driver

OpenList 自带 `drivers/s3` 的完整副本，只多一个必填字段 `proxy_url`：
所有 S3 API 请求（列目录、上传、下载、复制、删除……）都强制走 SOCKS5 代理，
服务器不会用本机 IP 直接连接对象存储。

> 驱动是**编译进二进制**的，没有运行时插件机制。拷源码只是第一步，
> 必须重新编译并替换正在运行的二进制/镜像才会生效；只把 `.go` 文件 `docker cp` 进容器是没用的。

## 与自带 S3 驱动的差异

只有一处字段差异，其余全部保留，行为与自带 `S3` 驱动一致：

| | 自带 `S3` | 本驱动 (`S3 Proxy`) |
| --- | --- | --- |
| 字段 | 18 个 | 同样 18 个 + **`proxy_url`（必填）** |
| 网络出口 | 本机直连 | 所有 S3 请求经 SOCKS5 |
| 预签名 / 直链 / 直传 | 支持 | 同样支持（这些链接由浏览器直接访问，不占服务器出口 IP） |

`meta.go` 与 `drivers/s3/meta.go` 的差异只有 `ProxyURL` 一行；`driver.go` / `util.go`
只是把 `S3` 改成 `S3Proxy` 并把 HTTP Transport 换成 SOCKS5 Dialer。

## 中文名称与界面翻译

**这不是驱动写错了。** OpenList 前端的字段标题是按
`drivers.<驱动名>.<字段名>` 查语言包的，驱动名是后端 `Config().Name`（这里叫
`S3 Proxy`）。官方 zh-CN 语言包里只有 `S3` 的词条，没有 `S3 Proxy`，所以：

- 驱动下拉框显示 `S3 Proxy`
- 该驱动下面所有字段回退成英文原始字段名（`Bucket`、`Endpoint`……）
- 字段提示回退成 `Proxy url-tips` 这种占位文本

汉化有两条路，任选一条即可。

### 方式一：补语言包后重新构建前端（推荐，长期）

1. 把 [`i18n/zh-CN.drivers.json`](i18n/zh-CN.drivers.json) 合并进
   `OpenList-Frontend/src/lang/zh-CN/drivers.json`：

   ```bash
   python3 - <<'PY'
   import json
   base = "OpenList-Frontend/src/lang/zh-CN/drivers.json"
   snippet = json.load(open("s3_proxy/i18n/zh-CN.drivers.json", encoding="utf-8"))
   data = json.load(open(base, encoding="utf-8"))

   data["S3 Proxy"] = snippet["S3 Proxy"]              # 字段标题/提示
   data.setdefault("drivers", {}).update(snippet["drivers"])  # 驱动显示名：存储代理

   json.dump(data, open(base, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
   PY
   ```

2. 构建前端 dist，然后把它放进 `OpenList/public/dist/` 再编后端（或直接用
   自定义 dist 替换官方 release 里的 dist）。显示名想改就改
   `snippet["drivers"]["S3 Proxy"]` 的值。

### 方式二：直接给官方前端 dist 打补丁（不用装 Node）

官方 release 的 zh-CN 词条是编译进 `assets/entry-*.js` 的，脚本会往里追加
`S3 Proxy` 词条（字段直接复用 `S3` 的翻译），幂等、不影响其他语言：

```bash
python3 s3_proxy/i18n/patch-dist-i18n.py /path/to/OpenList/public/dist
cd /path/to/OpenList && CGO_ENABLED=0 go build -tags=jsoniter -o bin/openlist .
```

补丁同时会补上官方前端 v4.2.9 里缺失的两个通用字段翻译
（`seed_policy` → 秒传策略、`root_folder_path` → 根文件夹路径）。

不汉化也能正常用，只是名称显示 `S3 Proxy`、字段显示英文。

## 从源码安装

1. 拷驱动包：

   ```bash
   REPO=/path/to/OpenList
   mkdir -p "$REPO/drivers/s3_proxy"
   cp *.go "$REPO/drivers/s3_proxy/"
   ```

2. 在 `$REPO/drivers/all.go` 加一行引用（和其他驱动放一起）：

   ```go
   _ "github.com/OpenListTeam/OpenList/v4/drivers/s3_proxy"
   ```

3. 编译（`build.sh` 会顺带下载官方前端并嵌入，产物 `bin/openlist`）：

   ```bash
   cd "$REPO"
   CGO_ENABLED=0 ./build.sh release docker
   ```

   也可以手动编：

   ```bash
   # 先把官方前端 dist 解压到 public/dist/，否则启动会报 index.html not exist
   CGO_ENABLED=0 go build -tags=jsoniter -ldflags "-w -s" -o bin/openlist .
   ```

   需要汉化就在编译前执行上面「方式二」的补丁脚本（或替换成自己构建的 dist）。

4. 用新二进制重启 OpenList；Docker 部署见下面「Docker 部署」。

## Docker 部署

数据都在 `/opt/openlist/data` 这个卷里（官方命令是 `-v /etc/openlist:/opt/openlist/data`）。
下面两种方式都**不动这个卷**，换二进制/镜像不会丢配置、账号和存储。

### 方式一：官方镜像 + 替换二进制（最省事）

先按上面第 3 步编出**静态链接**的二进制（`CGO_ENABLED=0`，官方镜像是 Alpine），
然后：

```bash
docker cp bin/openlist openlist:/opt/openlist/openlist
docker restart openlist
docker logs --tail 20 openlist
```

容器**重建后要重新塞一次**。不想每次重塞就改用 bind mount，启动时把宿主机文件挂进去：

```bash
docker run --user $(id -u):$(id -g) -d --restart=unless-stopped \
  --name openlist \
  -v /etc/openlist:/opt/openlist/data \
  -v /opt/openlist-bin/openlist:/opt/openlist/openlist:ro \
  -p 5244:5244 -e UMASK=022 \
  openlistteam/openlist:latest
```

以后升级只要替换 `/opt/openlist-bin/openlist` 再 `docker restart openlist`。

### 方式二：自己构建镜像

在 OpenList 源码根目录（已合并 `drivers/s3_proxy`）新建 `Dockerfile.s3proxy`：

```dockerfile
FROM golang:1.27-alpine AS builder
RUN apk add --no-cache git curl tar
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# 源码里的 public/dist 只有占位文件，这里拉官方前端嵌进二进制
ARG FRONTEND_VERSION=v4.2.9
RUN mkdir -p public/dist \
    && curl -fsSL "https://github.com/OpenListTeam/OpenList-Frontend/releases/download/${FRONTEND_VERSION}/openlist-frontend-dist-${FRONTEND_VERSION}.tar.gz" -o /tmp/dist.tar.gz \
    && tar -xzf /tmp/dist.tar.gz -C public/dist \
    && test -f public/dist/index.html

# 需要中文界面时在这里打补丁（把 i18n/ 目录一起 COPY 进来）
# RUN apk add --no-cache python3 && python3 i18n/patch-dist-i18n.py public/dist

RUN CGO_ENABLED=0 go build -buildvcs=false -tags=jsoniter -ldflags "-w -s" -o /out/openlist .

FROM openlistteam/openlist-base-image:base
ARG USER=openlist
ARG UID=1001
ARG GID=1001
WORKDIR /opt/openlist/
RUN addgroup -g ${GID} ${USER} && \
    adduser -D -u ${UID} -G ${USER} ${USER} && \
    mkdir -p /opt/openlist/data
COPY --from=builder --chown=${UID}:${GID} /out/openlist ./
COPY --chown=${UID}:${GID} entrypoint.sh /entrypoint.sh
RUN chmod 755 /opt/openlist/openlist /entrypoint.sh
USER ${USER}
RUN /entrypoint.sh version
ENV UMASK=022
VOLUME /opt/openlist/data/
EXPOSE 5244 5245
CMD [ "/entrypoint.sh" ]
```

构建并替换容器（卷、端口、数据都不变）：

```bash
cd "$REPO"
docker build -f Dockerfile.s3proxy -t openlist-s3proxy:latest .

docker stop openlist && docker rename openlist openlist-backup
docker run --user $(id -u):$(id -g) -d --restart=unless-stopped \
  --name openlist \
  -v /etc/openlist:/opt/openlist/data \
  -p 5244:5244 -e UMASK=022 \
  openlist-s3proxy:latest
```

`openlist-backup` 留着回滚：起回官方镜像挂同一个卷即可。

### compose

```yaml
services:
  openlist:
    image: openlist-s3proxy:latest      # 方式一可继续用 openlistteam/openlist:latest
    container_name: openlist
    restart: unless-stopped
    ports:
      - "5244:5244"
    environment:
      - UMASK=022
    volumes:
      - /etc/openlist:/opt/openlist/data
      # 方式一的二进制挂载（需要时取消注释）
      # - /opt/openlist-bin/openlist:/opt/openlist/openlist:ro
    # 代理跑在宿主机上时加上，代理地址才能写 host.docker.internal
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

```bash
docker compose up -d
```

### 代理地址在容器里怎么写

`proxy_url` 是**从 OpenList 容器内部**发起的连接，所以：

| 代理位置 | 写法 |
| --- | --- |
| 宿主机 | `socks5://172.17.0.1:1080`，或加 `--add-host=host.docker.internal:host-gateway` 后写 `socks5://host.docker.internal:1080` |
| 另一个容器 | 同一自定义网络用容器名；默认 bridge 网络用 `docker inspect` 查到的容器 IP |
| 容器自己 | 不能写 `127.0.0.1`（那是 OpenList 容器自身） |

需要域名交给代理解析时用 `socks5h://`。

## 使用

管理 → 存储 → 添加，驱动选 **`存储代理`**（英文界面是 `S3 Proxy`）。

字段和自带 `S3` 驱动完全一样（桶、Endpoint、Region、AK/SK、Session Token、
Custom Host、预签名、直传、User-Agent……），只是最后多一个：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| **SOCKS5 代理地址 (`proxy_url`)** | **是** | `socks5://host:port` / `socks5://user:pass@host:port` / `socks5h://...` |

## 行为说明

- `proxy_url` 必填：留空会直接报 `proxy_url is required`，**不会用本机 IP 直连 S3**；
- 只接受 `socks5://` / `socks5h://`，代理连不上也直接报错，不回退直连或 `HTTP_PROXY`/`HTTPS_PROXY`；
- `http.Transport.Proxy` 显式设为 `nil`，环境变量代理也影响不到它；
- 预签名 / 自定义域名 / 直传这些选项**保留**：它们生成的是给浏览器用的直链，不经过服务器，
  因此不影响「服务器 IP 不直连对象存储」这一目标；
- 除网络传输外，其余行为与自带 `S3` 驱动一致（同名方法仅替换类型名）。

## 测试

```bash
CGO_ENABLED=0 go test ./drivers/s3_proxy/
```

包含一个端到端测试：起一个本地 SOCKS5 服务 + 假 S3 服务，验证 List / Link / Put
全部走代理，并且代理为空时直接失败。

## 目录结构

```text
s3_proxy/
├── driver.go            # 与 drivers/s3/driver.go 同构，类型改名为 S3Proxy
├── meta.go              # S3 的 18 个字段 + ProxyURL
├── util.go              # 与 s3 相同，initSession 换成 SOCKS5 HTTPClient
├── types.go
├── proxy.go             # newSOCKS5HTTPClient：fail-closed 的 SOCKS5 Dialer
├── proxy_test.go        # SOCKS5 端到端测试
├── util_test.go         # 从 drivers/s3 带过来的测试
├── i18n/
│   ├── zh-CN.drivers.json   # 合并进前端 drivers.json 的中文词条
│   └── patch-dist-i18n.py   # 给官方前端 dist 打中文补丁（可选）
└── README.md
```
