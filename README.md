# WakeHub

局域网电源中枢：巴法 MQTT + Magic Packet（默认 UDP/9）+ 本机关机客户端。
服务端适合 **Linux + macvlan** 部署，在局域网二层直接发唤醒包。

## 组件

- `cmd/server`：Web 管理 / REST / 巴法 MQTT / 唤醒关机 / 客户端 WS / mDNS 浏览
- `cmd/client`：连接服务端、上报网卡、执行关机、mDNS 广播、安装系统服务

## 依赖

- 默认使用已发布模块：`github.com/leganck/bemfa-go v1.1.0`
- 本地联调可选：复制 `go.work.example` 为 `go.work`，与 `../bemfa-go` 组成 workspace

## 本地运行

```bash
go run ./cmd/server -config data/config.json -listen :8080
```

浏览器打开 `http://127.0.0.1:8080`。

客户端：

```bash
go run ./cmd/client run -server ws://<server-ip>:8080/api/ws/client -key my-pc
```

安装为系统服务：

```bash
# Windows（管理员）— 参数写入配置文件，服务 binPath 不含 token
bin\wakehub-client.exe service install -server ws://192.168.1.50:8080/api/ws/client -token SECRET -key my-pc

# Linux（root）
./wakehub-client service install -server ws://192.168.1.50:8080/api/ws/client -token SECRET -key my-pc

# 再次 install 可更新配置（幂等，无需先 uninstall）
# Windows 配置: %ProgramData%\wakehub-client\config.json
# Linux 配置:   /etc/wakehub-client/config.json
```

## Docker Compose

### 1. 桥接示例（简易 / 开发）

直接从项目根目录构建（使用已发布 bemfa-go）：

```bash
cp .env.example .env   # 可选，改 WAKEHUB_PORT / TZ
docker compose up -d --build
```

访问 `http://127.0.0.1:8080`。配置写在 `./data`。

> 桥接网络下 Magic Packet 广播往往到不了局域网二层。仅 Web/API/巴法调试可用；要可靠唤醒请用 macvlan 或 host 网络。

### 2. monorepo 父目录构建（可选本地 bemfa-go）

若希望 Docker 构建时使用同级源码 `../bemfa-go`：

```text
GolandProjects/
  bemfa-go/
  wol/  # 或 wakehub/（deploy 文件中 dockerfile 路径写为 wol/...）
```

```bash
docker compose -f deploy/docker-compose.yml up -d --build
```

### 3. macvlan 示例（Linux 生产唤醒）

```bash
# 编辑 .env 或 export：
# WAKEHUB_PARENT_IFACE=eth0
# WAKEHUB_SUBNET=192.168.1.0/24
# WAKEHUB_GATEWAY=192.168.1.1
# WAKEHUB_IP=192.168.1.50
docker compose -f deploy/docker-compose.macvlan.yml up -d --build
```

访问 `http://192.168.1.50:8080`。配置持久化在 `./data`。

> Windows Docker Desktop 对 macvlan 支持有限，生产请用 Linux 宿主机。

### 4. Linux host 网络（可选）

在 `docker-compose.yml` 中启用 `network_mode: host` 并去掉 `ports`，容器与宿主机共享协议栈，适合同机二层广播（端口直接占用宿主机 8080）。

## 功能说明

| 能力 | 说明 |
|------|------|
| 唤醒 | 服务端发 Magic Packet，默认端口 **9**（macvlan 二层可达） |
| 关机 | 绑定客户端在线时经 WebSocket 下发 `shutdown` |
| 巴法 | 全局 UID；设备开启巴法并保存时 `CreateTopic`（插座后缀 `001`）并订阅 on/off |
| 发现 | 客户端上报网卡 + 服务端 mDNS 浏览；支持一键建档 |
| MQTT 面板 | Web「MQTT」页：连接状态、主题绑定、运行日志 |
| Web 认证 | HTTP Basic；默认 **admin / admin**（可关）；客户端 WS 不受影响；API 不回传明文密码 |
| 配置迁移 | 旧配置无 `basicAuthEnable` 时自动启用认证并写 `config.json.bak` |
| 关机确认 | Web 关机需**两次确认** |
| OpenWrt 全局项 | 默认以 LuCI 为准，Web 设置只读；可选 writeback 写回 UCI |
| 在线探测 | 设备级 TCP/ICMP 探测，卡片展示状态，可手动探测 |
| 定时任务 | 按本地时间 + 星期，对设备或分组执行唤醒/关机 |
| 多网卡 | 编辑设备时可从客户端网卡列表选择 MAC/广播/探测 IP |
| 批量/分组 | 勾选批量操作；分组整组唤醒/关机 |
| 通知 | Webhook POST：唤醒/关机/探测变化/定时任务 |

## API

- `GET/PUT /api/settings`
- `GET/POST /api/devices` · `PUT/DELETE /api/devices/{id}`
- `POST /api/devices/{id}/wake` · `POST /api/devices/{id}/shutdown` · `POST /api/devices/{id}/probe`
- `POST /api/batch` · `POST /api/devices/from-client`
- `GET/POST /api/groups` · `PUT/DELETE /api/groups/{id}` · `POST .../wake|shutdown`
- `GET/POST /api/schedules` · `PUT/DELETE /api/schedules/{id}`
- `GET /api/clients` · `GET /api/discover` · `GET /api/status`
- `GET /api/mqtt` · `GET|DELETE /api/mqtt/logs`
- `WS /api/ws/client`

## OpenWrt

预编译 `.ipk`（服务端 + LuCI），无需完整 OpenWrt SDK。详见 [`openwrt/README.md`](openwrt/README.md)。

```bash
# 本地打包（Linux/WSL，需 go + binutils）
./openwrt/scripts/build-ipk.sh
# 产物：dist/openwrt/wakehub_<ver>_<arch>.ipk
#       dist/openwrt/luci-app-wakehub_<ver>_all.ipk
```

路由安装示例：

```sh
opkg install /tmp/wakehub_*_mipsel_24kc.ipk
opkg install /tmp/luci-app-wakehub_*_all.ipk
# LuCI：服务 → WakeHub → 启用并保存
# 设备管理：http://路由器IP:8080/
```

GitHub Actions：`.github/workflows/openwrt.yml`（push/PR 上传 Artifact；`v*` 标签附带到 Release）。

## 发布（GoReleaser / GitHub Actions）

| 文件 | 说明 |
|------|------|
| `.goreleaser.yaml` | 构建 `wakehub-server` / `wakehub-client` 多平台产物，并推送 GHCR 镜像 |
| `.github/workflows/release.yml` | `master`/`main` 推送做 snapshot 构建；`v*` 标签正式发布 |
| `.github/workflows/ci.yml` | PR/分支：`go vet` / `test` / `build` |
| `.github/workflows/openwrt.yml` | 交叉编译并打包 OpenWrt `wakehub` / `luci-app-wakehub` ipk |
| `Dockerfile.release` | GoReleaser 打包的运行镜像 |

模块依赖直接写入 `go.mod`（`github.com/leganck/bemfa-go v1.1.0`），CI 不再改写 `go.mod`，避免 GoReleaser dirty tree。

### 打标签发布

```bash
git tag v0.1.1
git push origin v0.1.1
```

产物：

- GitHub Release：各平台 `wakehub-server` / `wakehub-client` 压缩包
- GitHub Release：OpenWrt `.ipk`（多架构 `wakehub` + `luci-app-wakehub`）
- 容器：`ghcr.io/leganck/wakehub:v0.1.1`（含 `latest` multi-arch manifest）

### 使用发布镜像

```bash
docker run -d --name wakehub-server \
  -p 8080:8080 \
  -v "$PWD/data:/app/data" \
  ghcr.io/leganck/wakehub:latest
```

