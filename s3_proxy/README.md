# S3 Proxy driver

基于 OpenList 自带 `drivers/s3` 复制修改的存储驱动：所有 S3 请求强制走 SOCKS5 代理，
不用服务器本机 IP 直连对象存储。

> 驱动是**编译进二进制**的，没有运行时插件机制。拷源码只是第一步，
> 必须重新编译并替换正在运行的二进制/镜像才会生效；只把 `.go` 文件 `docker cp` 进容器是没用的。

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

3. 重新编译：

   ```bash
   cd "$REPO"
   CGO_ENABLED=0 go build -tags=jsoniter -o openlist .
   ```

   或者用仓库自带脚本（会顺带下载前端并嵌入，产物在 `bin/openlist`）：

   ```bash
   ./build.sh
   ```

4. 用新二进制重启 OpenList；Docker 部署见下面「Docker 部署」。

> 只 `go build` 而没准备前端时，二进制能编出来，但**启动会报 `index.html not exist` 后退出**。
> 用 `build.sh`，或先把官方前端 dist 解压到 `public/dist/` 再编译。

## Docker 部署

数据都在 `/opt/openlist/data` 这个卷里（官方命令是 `-v /etc/openlist:/opt/openlist/data`）。
下面两种方式都**不动这个卷**，换二进制/镜像不会丢配置、账号和存储。

### 方式一：官方镜像 + 替换二进制（最省事）

先按上面第 3 步编出带前端的静态二进制（推荐 `./build.sh`，产物 `bin/openlist`），然后：

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
注意二进制要**静态链接**（`CGO_ENABLED=0`），官方镜像是 Alpine。

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

管理 → 存储 → 添加，驱动选 **`S3 Proxy`**。

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| 挂载路径 | 是 | 例如 `/s3proxy` |
| 桶 / Endpoint / Region | 桶、Endpoint 必填 | 同自带 S3 驱动 |
| Access Key ID / Secret Access Key | 是 | S3 凭据 |
| 强制路径风格 | 否 | MinIO / rclone 这类服务要打开 |
| 用户代理 | 否 | 自定义 `User-Agent` |
| **代理地址 (proxy_url)** | **是** | 在“用户代理”下面，`socks5://user:pass@host:port` 或 `socks5h://...` |

## 行为说明

- `proxy_url` 必填：留空会直接报 `proxy_url is required`，**不会用本机 IP 直连 S3**；
- 只接受 `socks5://` / `socks5h://`，代理连不上也直接报错，不回退直连或 `HTTP_PROXY`/`HTTPS_PROXY`；
- `OnlyProxy` + `NoLinkURL`：下载/上传都经服务端中转（`/p/...`），不生成预签名直链；
- 已去掉 `custom_host`、预签名、直传等会绕过代理的选项；
- 代理只影响 S3 请求，驱动其余行为和自带 `S3` 一致。

## 测试

```bash
CGO_ENABLED=0 go test ./drivers/s3_proxy/
```
