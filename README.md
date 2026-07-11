# WOL 精简版

巴法 MQTT + Magic Packet（默认 UDP/9）+ 本机关机客户端。服务端适合 **Linux + macvlan** 部署，在局域网二层直接发唤醒包。

## 组件

- `cmd/server`：Web 管理 / REST / 巴法 / WOL / 客户端 WS / mDNS 浏览
- `cmd/client`：连接服务端、上报网卡、执行关机、mDNS 广播、安装系统服务

## 本地运行

需与 `bemfa-go` 源码并列（`../bemfa-go`，go.mod replace 已配置）：

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
bin\wol-client.exe install -server ws://192.168.1.50:8080/api/ws/client -key my-pc

# Linux（root）
./wol-client install -server ws://192.168.1.50:8080/api/ws/client -key my-pc
```

## Docker Compose

目录结构要求：

```text
GolandProjects/
  bemfa-go/
  wol/                 # 本仓库
    docker-compose.yml
    deploy/
    data/
```

### 1. 桥接示例（简易 / 开发）

映射端口到宿主机，配置写在 `./data`：

```bash
cd wol
cp .env.example .env   # 可选，改 WOL_PORT / TZ
docker compose up -d --build
```

访问 `http://127.0.0.1:8080`。

等价文件：`deploy/docker-compose.yml`。

> 桥接网络下 Magic Packet 广播往往到不了局域网二层。仅 Web/API/巴法调试可用；要可靠唤醒请用 macvlan 或 host 网络。

### 2. macvlan 示例（Linux 生产唤醒）

```bash
cd wol
# 编辑 .env 或 export：
# WOL_PARENT_IFACE=eth0
# WOL_SUBNET=192.168.1.0/24
# WOL_GATEWAY=192.168.1.1
# WOL_IP=192.168.1.50
docker compose -f deploy/docker-compose.macvlan.yml up -d --build
```

访问 `http://192.168.1.50:8080`。配置持久化在 `wol/data`。

> Windows Docker Desktop 对 macvlan 支持有限，生产请用 Linux 宿主机。

### 3. Linux host 网络（可选）

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

参考 `fn-qb-proxy` 配置：

| 文件 | 说明 |
|------|------|
| `.goreleaser.yaml` | 构建 `wol-server` / `wol-client` 多平台产物，并推送 GHCR 镜像 |
| `.github/workflows/release.yml` | `master`/`main` 推送做 snapshot 构建；`v*` 标签正式发布 |
| `.github/workflows/ci.yml` | PR/分支：`go vet` / `test` / `build` |
| `Dockerfile.release` | GoReleaser 打包的运行镜像 |

本地开发仍使用 `replace github.com/leganck/bemfa-go => ../bemfa-go`。  
CI 会 `go get github.com/leganck/bemfa-go@latest` 并去掉 replace。

### 打标签发布

```bash
git tag v0.1.0
git push origin v0.1.0
```

产物：

- GitHub Release：各平台 `wol-server` / `wol-client` 压缩包
- 容器：`ghcr.io/leganck/wol:v0.1.0`（含 `latest` multi-arch manifest）

### 使用发布镜像

```bash
docker run -d --name wol-server \
  -p 8080:8080 \
  -v "$PWD/data:/app/data" \
  ghcr.io/leganck/wol:latest
```
