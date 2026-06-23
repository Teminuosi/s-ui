# S-UI(改版)

**基于 [SagerNet/sing-box](https://github.com/SagerNet/sing-box) 的高级 Web 面板**

> 本仓库是 [alireza0/s-ui](https://github.com/alireza0/s-ui) 的二次开发分支,在保留上游全部能力的基础上,
> 把已在 [3x-ui 改版](https://github.com/Teminuosi/3x-ui) 验证过的体验改进搬了过来:
> 全自动一键安装、面板更新指向本仓库、协议模板、行内二维码、批量删除、中转与多节点部署(逐步推进)。
>
> 仅供个人学习与交流,请勿用于非法用途,请勿用于生产环境。上游版权归原作者所有。

[![License](https://img.shields.io/badge/license-GPL%20V3-blue.svg?longCache=true)](https://www.gnu.org/licenses/gpl-3.0.en.html)

---

## 快速开始

### 全自动安装(推荐)

一条命令装好,全程无需交互:全自动会自动生成随机管理员账号密码和随机面板路径,装完直接打印访问信息。

```sh
SUI_AUTO=1 bash <(curl -Ls https://raw.githubusercontent.com/Teminuosi/s-ui/main/install.sh)
```

### 交互式安装

想自己一步步设置端口、路径、账号密码,用普通模式:

```sh
bash <(curl -Ls https://raw.githubusercontent.com/Teminuosi/s-ui/main/install.sh)
```

装好后,在服务器上随时输入 `s-ui` 打开管理菜单(启动/停止/重启、改设置、改账号、SSL 证书、BBR 等)。

### Windows

1. 从 [Releases](https://github.com/Teminuosi/s-ui/releases/latest) 下载最新 Windows 包并解压
2. 以管理员身份运行 `install-windows.bat`,按向导完成

---

## 默认安装信息

| 项目         | 默认值     |
| ------------ | ---------- |
| 面板端口     | `2095`     |
| 面板路径     | `/app/`    |
| 订阅端口     | `2096`     |
| 订阅路径     | `/sub/`    |
| 账号 / 密码  | `admin`    |

> 全自动安装(`SUI_AUTO=1`)会把账号密码和面板路径换成随机值,请留意安装结束时打印的信息。
> 升级(已存在数据库)时全自动模式会**保留原有设置**,不会改动你的账号与路径。

---

## 与上游的差异(本分支在做的事)

- **阶段 0 — Fork 基建**:安装/更新脚本全部指向本仓库;CI 按 tag 出 Release 并把版本号写进面板;全自动安装体验。✅
- **阶段 1 — 一键模板 + 二维码 + 批量删除**:常用协议(VLESS+Reality、Hysteria2 等)一键建好;客户端列表行内出二维码;入站/客户端支持多选批量删除。🚧
- **阶段 2 — 中转**:粘贴落地分享链接→自动转 outbound→一步配好路由的"添加中转"向导。🚧
- **阶段 3 — 多节点部署**:以"另一台 S-UI = 一个节点"的方式,中央面板通过 APIv2 令牌远程下发配置、同步流量与心跳。🚧

> 持续跟进上游修复与 sing-box 内核升级,按需回合(不整体合并)。

---

## 支持的平台

| 平台    | 架构 | 状态 |
|---------|------|------|
| Linux   | amd64 / arm64 / armv7 / armv6 / armv5 / 386 / s390x | ✅ |
| Windows | amd64 / 386 / arm64 | ✅ |
| macOS   | amd64 / arm64 | 🚧 实验性 |

## 功能概览

- 多协议:Mixed、SOCKS、HTTP、Direct、Redirect、TProxy;VLESS、VMess、Trojan、Shadowsocks;ShadowTLS、Hysteria、Hysteria2、Naive、TUIC(支持 XTLS)
- 入站 / 出站高级配置;流量路由界面(PROXY Protocol、外部/透明代理、SSL、端口)
- 客户端流量上限与到期时间;在线客户端、流量统计与系统状态监控
- 订阅服务(link / json / clash,可加外部订阅);面板与订阅 HTTPS
- 多语言(英语、波斯语、越南语、简体中文、繁体中文、俄语);明暗主题;API 接口

---

## Docker 安装

<details>
<summary>展开</summary>

```shell
# 安装 Docker
curl -fsSL https://get.docker.com | sh

# 运行 S-UI(GHCR 镜像)
mkdir s-ui && cd s-ui
docker run -itd \
    -p 2095:2095 -p 2096:2096 -p 443:443 -p 80:80 \
    -v $PWD/db/:/app/db/ \
    -v $PWD/cert/:/root/cert/ \
    --name s-ui --restart=unless-stopped \
    ghcr.io/teminuosi/s-ui:latest
```

自行构建镜像:

```shell
git clone --recurse-submodules https://github.com/Teminuosi/s-ui
cd s-ui
docker build -t s-ui .
```

</details>

## 卸载

```sh
sudo -i
systemctl disable s-ui --now
rm -f /etc/systemd/system/sing-box.service
systemctl daemon-reload
rm -fr /usr/local/s-ui
rm -f /usr/bin/s-ui
```

---

## 本地开发

<details>
<summary>展开</summary>

```shell
# 克隆(含前端子模块)
git clone --recurse-submodules https://github.com/Teminuosi/s-ui
cd s-ui

# 一键构建并运行(前端 + 后端)
./runSUI.sh
```

分步构建:

```shell
# 前端
cd frontend && npm install && npm run build && cd ..
# 后端(需先构建前端)
rm -fr web/html/* && cp -R frontend/dist/* web/html/
go build -o sui main.go
./sui
```

前端代码在独立子模块仓库:[Teminuosi/s-ui-frontend](https://github.com/Teminuosi/s-ui-frontend)

</details>

## 环境变量

| 变量           | 取值                                     | 默认     |
| -------------- | ---------------------------------------- | -------- |
| SUI_LOG_LEVEL  | `debug` / `info` / `warn` / `error`      | `info`   |
| SUI_DEBUG      | `true` / `false`                         | `false`  |
| SUI_BIN_FOLDER | 字符串                                   | `bin`    |
| SUI_DB_FOLDER  | 字符串                                   | `db`     |
| SINGBOX_API    | 字符串                                   | -        |

---

## 致谢

- 上游项目:[alireza0/s-ui](https://github.com/alireza0/s-ui)、[alireza0/s-ui-frontend](https://github.com/alireza0/s-ui-frontend)
- 内核:[SagerNet/sing-box](https://github.com/SagerNet/sing-box)
- [API 文档(上游 Wiki)](https://github.com/alireza0/s-ui/wiki/API-Documentation)
