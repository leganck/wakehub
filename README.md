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
# Windows（管理员）
bin\wakehub-client.exe install -server ws://192.168.1.50:8080/api/ws/client -key my-pc

# Linux（root）
./wakehub-client install -server ws://192.168.1.50:8080/api/ws/client -key my-pc
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

## API

- `GET/PUT /api/settings`
- `GET/POST /api/devices` · `PUT/DELETE /api/devices/{id}`
- `POST /api/devices/{id}/wake` · `POST /api/devices/{id}/shutdown`
- `POST /api/devices/from-client`
- `GET /api/clients` · `GET /api/discover` · `GET /api/status`
- `GET /api/mqtt` · `GET|DELETE /api/mqtt/logs`
- `WS /api/ws/client`

## 发布（GoReleaser / GitHub Actions）

| 文件 | 说明 |
|------|------|
| `.goreleaser.yaml` | 构建 `wakehub-server` / `wakehub-client` 多平台产物，并推送 GHCR 镜像 |
| `.github/workflows/release.yml` | `master`/`main` 推送做 snapshot 构建；`v*` 标签正式发布 |
| `.github/workflows/ci.yml` | PR/分支：`go vet` / `test` / `build` |
| `Dockerfile.release` | GoReleaser 打包的运行镜像 |

模块依赖直接写入 `go.mod`（`github.com/leganck/bemfa-go v1.1.0`），CI 不再改写 `go.mod`，避免 GoReleaser dirty tree。

### 打标签发布

```bash
git tag v0.1.1
git push origin v0.1.1
```

产物：

- GitHub Release：各平台 `wakehub-server` / `wakehub-client` 压缩包
- 容器：`ghcr.io/leganck/wakehub:v0.1.1`（含 `latest` multi-arch manifest）

### 使用发布镜像

```bash
docker run -d --name wakehub-server \
  -p 8080:8080 \
  -v "$PWD/data:/app/data" \
  ghcr.io/leganck/wakehub:latest
```
