# Windows 杀软误报说明（CobaltStrike 等）

## 现象

`wakehub-client.exe` 可能被 Windows Defender 或其它杀软标为：

- `Backdoor/W64.CobaltStrike.eo`
- 或其它 Trojan / Heur / Gen 类名

这是 **Go 语言客户端 + 网络长连接 + 系统服务 + 关机命令** 组合下的**常见误报**，不是项目被植入木马。

## 为何容易误报

| 行为 | 客户端用途 | 杀软启发式 |
|------|------------|------------|
| 出站 WebSocket | 连 `wakehub-server` | 像 C2 回连 |
| `sc.exe` 安装服务 | `service install` | 持久化 |
| `shutdown /s` + `cmd` | 远程关机 | 像远控 |
| 自更新下载 exe | `update apply` | 像投递载荷 |
| 未代码签名 | 本地/CI 构建 | 信誉为 0 |
| `-ldflags "-s -w"` | 减小体积 | 像加壳/抹符号 |

> 客户端**不再执行远程重启**（不嵌入/不调用 `shutdown /r`）。实测含 `shutdown /r` 的构建易被标为 CobaltStrike 并秒删；去掉后可正常落地。

Cobalt Strike 信标常被杀软用「Go 网络 + 服务 + 命令执行」一类启发式覆盖，**大量合法 Go 工具会中招**。

## 本地处理（开发机）

### 1. 先核对文件来源

只运行本仓库 `go build` 或 GitHub Release 产物，不要混用来路不明的拷贝。

```powershell
# 在项目根目录重新构建
go build -o bin/wakehub-client.exe ./cmd/client
Get-FileHash bin\wakehub-client.exe -Algorithm SHA256
```

### 2. Windows Defender 排除（仅限可信目录）

以管理员 PowerShell：

```powershell
Add-MpPreference -ExclusionPath "D:\Projects\GolandProjects\wol\bin"
# 或排除你的安装目录，例如：
# Add-MpPreference -ExclusionPath "D:\DevelopTools"
```

### 3. 提交误报

- [Microsoft Security Intelligence 提交](https://www.microsoft.com/en-us/wdsi/filesubmission)  
  选择 **Software developer** / false positive，附上源码仓库与 SHA256。

## 发布侧建议（彻底缓解）

1. **Authenticode 代码签名**（最有效）  
   用购买的代码签名证书对 `wakehub-client.exe` / `wakehub-server.exe` 签名后再分发。
2. 仅从 **GitHub Releases** 下载，并校验 checksum。
3. 企业环境：用自己的证书签名，或通过应用控制（WDAC）放行。

本仓库已为 Windows 客户端加入 **VERSIONINFO** 资源（文件说明 / 产品名 / 开源声明），可降低部分启发式误报，**不能替代代码签名**。

## 构建带版本信息的客户端

```powershell
# 生成 Windows 资源（只需在修改 versioninfo.json 后执行）
go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.1 `
  -64 -o cmd/client/rsrc_windows_amd64.syso cmd/client/versioninfo.json

go build -ldflags "-s -w -X main.version=dev" -o bin/wakehub-client.exe ./cmd/client
```

`rsrc_windows_amd64.syso` 仅在 `windows/amd64` 链接，不影响 Linux/macOS 构建。
