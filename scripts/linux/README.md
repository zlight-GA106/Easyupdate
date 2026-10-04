# Linux 部署脚本

用于 `/home/zlight106/easyupdate`。复制 `dist/easyupdate-linux-amd64` 到该目录并命名为 `easyupdate`，将本目录三个 `.sh` 文件复制到同一位置。

首次运行时，从 `config.example.yaml` 创建 `config.yaml`，设置监听端口与客户端可访问的 `server.public_url`。配置权限使用 `600`；升级保留配置与 `data/`。

```sh
cd /home/zlight106/easyupdate
chmod 700 easyupdate start.sh stop.sh control-common.sh
./start.sh
./stop.sh
```

`start.sh` 创建本目录下的 `run/` 和 `logs/`，日志为 `logs/server.log`。启动后另行检查 HTTP 地址。`stop.sh` 核对进程路径、工作目录和启动时间后发送 SIGTERM。

服务在 SSH 断开后继续运行；服务器重启后需执行 `start.sh`。脚本依赖 Bash、GNU stat、readlink、flock 和 Linux `/proc`。
