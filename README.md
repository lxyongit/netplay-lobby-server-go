# netplay-lobby-server-go

Netplay lobby server written in GO. Needs Go v1.13.

## Deployment

```bash
go build
./netplay-lobby-server-go
```

## Configuration
Rename the ```config/lobby.template.yaml``` to ```config/lobby.yaml``` and place the configuration file in one of the
following directories:

 - /etc/lobby
 - $HOME/.lobby
 - ./config

## LibretroDroid 直连协调

Go 大厅负责房间信息；Python `netplay-tunnel-server` 在 TCP 55435 上负责 relay session 注册和游戏数据转发。大厅新增的协调服务只交换候选地址，成功直连后不经过服务器转发游戏数据。

在实际使用的 `lobby.yaml` 中加入并配置：

```yaml
traversal:
  address: "[::]:55436"
  advertisehost: "direct.example.com"
  publicport: 55436
```

把 `direct.example.com` 替换为直达这台 Go 服务的域名（配置 A，建议同时配置 AAAA）。开放 TCP 55436；容器部署也需要发布此端口。协调端口需要保留原始 TCP 源地址和端口，不能经过普通 HTTP/CDN、会重建连接的 TCP 反向代理或 SNAT；否则观察到的是代理端口，无法进行 NAT 打洞。IPv4 端口映射可使用 DNAT，但必须保留客户端源地址/端口。

`GET /traversal` 返回 `enabled`、`protocol: tcp-v1`、`host`、`port`，供两台手机自动发现。未配置 `address` 时返回 `enabled: false`，维持原有直连和中转行为。实际配置文件需由部署者填写；模板默认关闭。该端口不是 HTTP 健康检查端口。

穿透使用与 relay session 相同的房间标识配对双方。服务交换观察到的 TCP 源端点及手机声明的公网 IPv6 候选，同公网地址的手机还可交换 LAN 候选。每次配对生成临时随机 nonce，手机在候选连接上确认配对并选择唯一连接，然后执行原有游戏握手。此 nonce 用于隔离连接尝试，不代替游戏口令或用户认证。

房主注册最长等待 60 秒、客户端等待 4 秒；注册读取、响应写入均有限时。服务限制 512 个并发连接、每来源 IP 32 个连接，异常断开会释放等待槽位。房间标识不写入日志。可通过 `NETPLAY-DEBUG traversal paired` 查看配对情况，实际直连是否成功以手机的 `traversal direct selected` 和 `route=direct` 为准。

`relay` 配置仍指向 Python tunnel，例如 `nyc: "relay.example.com:55435"`；IPv6 字面量使用 `"[2001:db8::1]:55435"` 格式。协调服务不修改 RATS/RATL/RATA。两台手机需要同时更新应用和 LibretroDroid AAR。移动网络不一定支持 TCP 同时打开，双方也不一定都具备可互通 IPv6，因此中转后备仍然需要保留。

## LICENSE

The server itself is licensed under AGPLv3.

This product includes GeoLite2 data created by MaxMind, available from
[https://www.maxmind.com](https://www.maxmind.com)
