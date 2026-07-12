# WakeHub OpenWrt 打包

本目录提供 **预编译二进制 → `.ipk`** 方案（CI 使用自带 `mkipk.sh`，无需完整 OpenWrt SDK）。

> **包格式**：OpenWrt 24+/25、Kwrt 等新版 `opkg` 使用 **gzip 压缩的 tar**（魔数 `1f 8b`），**不是** 旧式 Debian `ar`（`!<arch>`）。`mkipk.sh` 已按官方包布局打包。

## 包内容

| 包名 | 说明 |
|------|------|
| `wakehub` | `wakehub-server`、procd 脚本、UCI、UCI→JSON 同步 |
| `luci-app-wakehub` | LuCI：启用服务、端口、巴法 UID、客户端 Token 等 |

设备增删 / 唤醒 / 关机仍使用内置 Web UI（默认 `http://路由器IP:8080/`）。

## 架构映射

| OpenWrt `Architecture` | Go 构建参数 |
|------------------------|-------------|
| `mipsel_24kc` | `GOARCH=mipsle GOMIPS=softfloat` |
| `mips_24kc` | `GOARCH=mips GOMIPS=softfloat` |
| `aarch64_generic` | `GOARCH=arm64` |
| `arm_cortex-a7` | `GOARCH=arm GOARM=7` |
| `x86_64` | `GOARCH=amd64` |
| `riscv64_generic` | `GOARCH=riscv64` |

在路由上确认架构：

```sh
opkg print-architecture
# 或
. /etc/openwrt_release; echo $DISTRIB_ARCH
```

若系统 arch 名称与上表不完全一致（例如 `aarch64_cortex-a53`），通常仍可安装 **兼容指令集** 的包（同为 aarch64 / mipsel 等）；以 `opkg install` 是否接受为准。

## 本地构建

在 Linux / WSL / macOS（需 `bash`、`go`、`ar`/`binutils`）：

```bash
# 全部架构
./openwrt/scripts/build-ipk.sh

# 指定版本与架构
VERSION=0.1.1 ARCHES="mipsel_24kc x86_64" ./openwrt/scripts/build-ipk.sh

# 输出目录
ls dist/openwrt/*.ipk
```

产物示例：

```text
dist/openwrt/wakehub_0.1.1_mipsel_24kc.ipk
dist/openwrt/luci-app-wakehub_0.1.1_all.ipk
dist/openwrt/SHA256SUMS
```

## 路由上安装

```sh
# 将 ipk 拷到 /tmp
opkg update
opkg install /tmp/wakehub_*_mipsel_24kc.ipk
opkg install /tmp/luci-app-wakehub_*_all.ipk

# 或仅服务端（无 LuCI）
uci set wakehub.main.enabled='1'
uci set wakehub.main.listen_port='8080'
uci commit wakehub
/etc/init.d/wakehub enable
/etc/init.d/wakehub start
```

LuCI：**服务 → WakeHub**（启用并保存应用）。

## UCI 配置

`/etc/config/wakehub`：

| 选项 | 说明 |
|------|------|
| `enabled` | `1` 启动服务 |
| `listen_port` | **运行端口**（HTTP/WebSocket，默认 8080；LuCI 可改） |
| `config_path` | JSON 配置路径 |
| `bemfa_uid` | 巴法私钥 |
| `client_token` | 客户端 WS Token |
| `ws_path` | WS 路径 |

启动时 `/etc/init.d/wakehub` 使用 `-listen :$listen_port`，与 LuCI「运行端口」一致。

启动时 `/usr/libexec/wakehub-uci-sync` 将上述全局项写入 `config.json` 的 `settings`，并尽量保留已有 `devices[]`。

> 全局参数以 **UCI / LuCI 为准**。若在内置 Web「设置」页修改同一字段，下次服务重启可能被 UCI 覆盖。设备列表请用内置 Web 管理。

## 目录结构

```text
openwrt/
├── README.md
├── scripts/
│   ├── build-ipk.sh    # 交叉编译 + 打 ipk
│   └── mkipk.sh        # 纯 shell 生成 .ipk（ar）
├── wakehub/
│   ├── CONTROL/        # control 模板、conffiles、postinst/prerm
│   └── files/          # 安装根文件系统
└── luci-app-wakehub/
    ├── CONTROL/
    └── files/
```

## GitHub Actions

工作流：`.github/workflows/openwrt.yml`

- `pull_request` / `push`：构建 snapshot ipk 并上传 Artifact  
- `v*` tag：构建正式版本，并附加到 GitHub Release  
- `workflow_dispatch`：手动触发  

## 与完整 OpenWrt feed 的关系

当前方案适合 **预编译分发**。若要接入官方式 `package/Makefile` + `golang-package.mk` 源码编译，可在本目录基础上再增加 feed 包定义；ipk 文件布局已与 `opkg` 兼容。
