#!/usr/bin/env python3
"""Add S3 Proxy translation entries to every language in the frontend source."""
import json
import os
import sys

ROOT = sys.argv[1] if len(sys.argv) > 1 else "src/lang"

TEXTS = {
    "en": {
        "proxy_url": "SOCKS5 proxy URL",
        "proxy_url-tips": (
            "All S3 requests are sent through this SOCKS5 proxy. Use "
            "socks5://127.0.0.1:1080 or socks5h://user:pass@127.0.0.1:1080."
        ),
        "test_proxy": "Test",
        "test_proxy_ok": "Proxy works: reached {{target}} in {{ms}} ms",
        "label": "S3 Proxy",
    },
    "zh-CN": {
        "proxy_url": "SOCKS5 代理地址",
        "proxy_url-tips": (
            "所有 S3 请求都通过该 SOCKS5 代理发送，服务器不会用本机 IP 直接连接对象存储。"
            "例如 socks5://127.0.0.1:1080 或 socks5h://user:pass@127.0.0.1:1080。"
        ),
        "test_proxy": "测试",
        "test_proxy_ok": "代理可用：{{target}}，耗时 {{ms}} 毫秒",
        "label": "存储代理",
    },
}

changed = []
for lang in sorted(os.listdir(ROOT)):
    path = os.path.join(ROOT, lang, "drivers.json")
    if not os.path.isfile(path):
        continue
    with open(path, encoding="utf-8") as f:
        data = json.load(f)

    texts = TEXTS.get(lang, TEXTS["en"])
    block = data.get("S3 Proxy")
    if block is None:
        # Reuse the built-in S3 translations so the proxy driver shows real
        # field labels instead of raw key names in this language.
        src = data.get("S3")
        block = dict(src) if isinstance(src, dict) else {}
        data["S3 Proxy"] = block
    for key in ("proxy_url", "proxy_url-tips", "test_proxy", "test_proxy_ok"):
        block[key] = texts[key]

    drivers = data.setdefault("drivers", {})
    drivers["S3 Proxy"] = texts["label"]

    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
        f.write("\n")
    changed.append(lang)

# The backend gained a seed_policy field that the v4.2.9 frontend i18n does not
# know about yet, so add it for every language as well.
SEED = {
    "zh-CN": ("秒传策略", "覆盖此存储自动生成秒传种子的策略"),
    "zh-TW": ("秒傳策略", "覆蓋此存儲自動生成秒傳種子的策略"),
}
SEED_EN = ("Seed policy", "Override the policy for automatically generated seeds for this storage")
SEED_OPTIONS = {
    "zh-CN": {"inherit": "继承", "on": "开启", "off": "关闭"},
    "zh-TW": {"inherit": "繼承", "on": "開啟", "off": "關閉"},
}
SEED_OPTIONS_EN = {"inherit": "Inherit", "on": "On", "off": "Off"}

for lang in sorted(os.listdir(ROOT)):
    path = os.path.join(ROOT, lang, "storages.json")
    if not os.path.isfile(path):
        continue
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
    common = data.setdefault("common", {})
    label, tips = SEED.get(lang, SEED_EN)
    common.setdefault("seed_policy", label)
    common.setdefault("seed_policy-tips", tips)
    common.setdefault("seed_policys", SEED_OPTIONS.get(lang, SEED_OPTIONS_EN))
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
        f.write("\n")

print("updated:", ", ".join(changed))
