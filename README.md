# OpenList-New-Storage

汇总这个目录里给 OpenList 新增/扩展的存储驱动。

## 目前包含的存储

| 驱动 | 界面显示名 | 说明 | 代码 | 文档 |
| --- | --- | --- | --- | --- |
| **S3 Proxy** | 存储代理 | 完整复制 OpenList 自带 `S3` 驱动的全部字段和行为，只多一个必填的 `proxy_url`：所有 S3 请求强制走 SOCKS5 代理，服务器 IP 不直连对象存储，`proxy_url` 右侧带代理连通性测试按钮 | [`s3_proxy/`](s3_proxy/) | [`s3_proxy/README.md`](s3_proxy/README.md) |

## 目录结构

```text
OpenList-New-Storage/
├── README.md      # 本文件：汇总
└── s3_proxy/      # S3 Proxy 驱动（源码 + 安装/使用/Docker 说明）
    └── test-button/   # 可选：proxy_url 测试按钮补丁
```

安装方式、字段说明、Docker 部署都在各驱动自己的 README 里。
