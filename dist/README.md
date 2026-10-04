# 构建产物

| 文件 | 平台 |
| --- | --- |
| [easyupdate-windows-amd64.exe](easyupdate-windows-amd64.exe) | Windows x86-64 |
| [easyupdate-linux-amd64](easyupdate-linux-amd64) | Linux x86-64，静态链接 |
| [android/easyupdate-demo-v1.apk](android/easyupdate-demo-v1.apk) | Android Demo 1.0.0 / 1 |
| [android/easyupdate-demo-v2.apk](android/easyupdate-demo-v2.apk) | Android Demo 1.1.0 / 2 |

服务端源码提交：`6fd1b8df767efca03af67da0c8137e7f33a14080`。Go 1.27.1、`CGO_ENABLED=0`，构建参数为 `-trimpath -ldflags "-s -w"`。

在项目根目录复制 `config.example.yaml` 为 `config.yaml`，再运行对应程序。程序使用当前工作目录下的配置与数据；账号密码在配置文件中修改。

校验文件：在本目录执行 `sha256sum -c SHA256SUMS.txt`。Windows 可使用 `Get-FileHash -Algorithm SHA256` 对照 [SHA256SUMS.txt](SHA256SUMS.txt)。

两版 Demo 使用相同的调试签名，供验证升级流程使用。运行和验证步骤见 [项目说明](../README.md) 与 [验证记录](../docs/verification.md)。
