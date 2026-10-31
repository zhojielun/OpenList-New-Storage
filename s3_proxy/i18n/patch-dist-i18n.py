#!/usr/bin/env python3
"""Inject Simplified-Chinese (zh-CN) i18n entries for the S3 Proxy driver into
an already-built OpenList frontend dist (public/dist).

Why this is needed
------------------
OpenList's frontend looks driver labels up by key
``drivers.<driver name>.<field>``.  The official zh-CN language pack only knows
the built-in ``S3`` driver, so a driver named ``S3 Proxy`` renders every field
in English (the raw field name) and the driver name itself stays ``S3 Proxy``.
Those translations are compiled into the per-locale JS chunk, so a binary built
from the official dist cannot be translated at runtime.

This script appends the missing entries to the zh-CN chunk(s) of a dist
directory without touching other locales.  It is idempotent.

Usage:
    python3 patch-dist-i18n.py /path/to/OpenList/public/dist
    # then rebuild the binary so the patched dist is embedded
    cd /path/to/OpenList && CGO_ENABLED=0 go build -tags=jsoniter -o bin/openlist .
"""

import os
import re
import sys

DRIVER_KEY = "S3 Proxy"
DRIVER_LABEL = "存储代理"

PROXY_URL_LABEL = "SOCKS5 代理地址"
PROXY_URL_TIPS = (
    "所有 S3 请求都通过该 SOCKS5 代理发送，服务器不会用本机 IP 直接连接对象存储。"
    "例如 socks5://127.0.0.1:1080 或 socks5h://user:pass@127.0.0.1:1080。"
)
EXTRA_COMMON = {
    "seed_policy": "秒传策略",
    "seed_policy-tips": "覆盖此存储自动生成秒传种子的策略",
    "root_folder_path": "根文件夹路径",
}

MARKER = "__s3ProxyI18n"


def js_string(value):
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'


def build_body(drivers_expr, storages_expr):
    extra = ",".join(
        "%s:%s" % (js_string(k), js_string(v)) for k, v in EXTRA_COMMON.items()
    )
    key = js_string(DRIVER_KEY)
    return (
        "try{"
        "var __d=%s;"
        "if(__d&&__d.S3&&!__d[%s]){"
        "var __c={};for(var __k in __d.S3)__c[__k]=__d.S3[__k];"
        "__c.proxy_url=%s;__c[%s]=%s;"
        "__d[%s]=__c;"
        "}"
        "if(__d&&__d.drivers&&!__d.drivers[%s])__d.drivers[%s]=%s;"
        "var __s=%s;"
        "if(__s&&__s.common){var __m={%s};"
        "for(var __n in __m)if(!__s.common[__n])__s.common[__n]=__m[__n];}"
        "}catch(e){}"
        % (
            drivers_expr,
            key,
            js_string(PROXY_URL_LABEL),
            js_string("proxy_url-tips"),
            js_string(PROXY_URL_TIPS),
            key,
            key,
            key,
            js_string(DRIVER_LABEL),
            storages_expr,
            extra,
        )
    )


def patch_modern(text):
    """entry-<hash>.js: `var t={...};export{t as dict};`"""
    if not re.search(r'from"\./zh-CN-[^"]+"', text):
        return None
    export = re.search(r"export\{([A-Za-z_$][\w$]*) as dict\}", text)
    if not export:
        return None
    var = export.group(1)
    inject = "/*%s*/;%s" % (
        MARKER,
        build_body(var + ".drivers", var + ".storages"),
    )
    return text[: export.start()] + inject + text[export.start():]


def patch_legacy(text):
    """entry-legacy-<hash>.js: System.register([`./zh-CN-legacy-...`], ...)
    ending with the comma expression `...,ee(`dict`,{...})`."""
    if not re.search(r"System\.register\(\[`\./zh-CN-legacy-[^`]+`\]", text):
        return None
    call = text.find("ee(`dict`,{")
    if call < 0:
        call = text.find('ee("dict",{')
    if call < 0:
        return None
    head = text[call : call + 700]
    drivers = re.search(r"drivers:([A-Za-z_$][\w$]*)", head)
    storages = re.search(r"storages:([A-Za-z_$][\w$]*)", head)
    if not drivers or not storages:
        return None
    # we are inside an expression list (`a=1,b=2,ee(...)`), so the injection has
    # to be a comma operand, not a statement
    inject = "/*%s*/(function(){%s})()," % (
        MARKER,
        build_body(drivers.group(1), storages.group(1)),
    )
    return text[:call] + inject + text[call:]


def main():
    if len(sys.argv) != 2:
        print(__doc__)
        return 2
    dist = os.path.abspath(sys.argv[1])
    assets = os.path.join(dist, "assets")
    if not os.path.isdir(assets):
        print("not a frontend dist (missing assets/): %s" % dist)
        return 2

    patched = 0
    for name in sorted(os.listdir(assets)):
        if not name.startswith("entry") or not name.endswith(".js"):
            continue
        path = os.path.join(assets, name)
        text = open(path, encoding="utf-8").read()
        if MARKER in text:
            print("already patched:", name)
            continue
        new = patch_modern(text)
        if new is None:
            new = patch_legacy(text)
        if new is None:
            continue
        open(path, "w", encoding="utf-8").write(new)
        print("patched:", name)
        patched += 1

    if not patched:
        print("nothing patched - is this the zh-CN chunk of an OpenList dist?")
        return 1
    print("done. rebuild the binary so the patched dist is embedded.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
