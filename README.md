<div align="center">

![new-api](/web/public/logo.png)

# New API

**面向团队与个人的统一 AI 服务入口**

<p align="center">
  简体中文 |
  <a href="./README.zh_TW.md">繁體中文</a> |
  <a href="./README.en.md">English</a> |
  <a href="./README.fr.md">Français</a> |
  <a href="./README.ja.md">日本語</a>
</p>

<p align="center">
  <a href="https://raw.githubusercontent.com/Calcium-Ion/new-api/main/LICENSE">
    <img src="https://img.shields.io/github/license/Calcium-Ion/new-api?color=brightgreen" alt="license">
  </a>
  <a href="https://github.com/QuantumNous/new-api/releases/latest">
    <img src="https://img.shields.io/github/v/release/Calcium-Ion/new-api?color=brightgreen&include_prereleases" alt="release">
  </a>
  <a href="https://hub.docker.com/r/CalciumIon/new-api">
    <img src="https://img.shields.io/badge/docker-dockerHub-blue" alt="docker">
  </a>
  <a href="https://atomgit.com/QuantumNous/new-api" target="_blank">
    <img alt="AtomGit G-Star" src="https://atomgit.com/QuantumNous/new-api/star/badge.svg"/>
  </a>
</p>

</div>

## 简介

New API 是一个面向团队和个人的 AI 服务管理平台。它把不同服务整合到一个简洁的使用入口中，帮助你统一管理模型、账号、访问凭证、用量和费用。

平台支持自定义品牌信息、首页内容、登录页背景、全局背景和毛玻璃卡片效果，适合个人工作台、团队内部服务以及面向用户的 AI 服务门户。

## 如何使用

### 方式一：使用 Docker Compose

```bash
git clone https://github.com/QuantumNous/new-api.git
cd new-api
docker-compose up -d
```

启动完成后，在浏览器打开：

```text
http://localhost:3000
```

首次进入时按照页面提示完成初始化，然后即可登录使用。

### 方式二：使用 Docker 镜像

```bash
docker pull calciumion/new-api:latest
docker run --name new-api -d --restart always \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  calciumion/new-api:latest
```

数据会保存在当前目录的 `data` 文件夹中。更多安装方式请查看[官方部署指南](https://docs.newapi.pro/zh/docs/installation)。

## 功能介绍

- **统一服务入口**：在一个页面中使用和管理多种 AI 模型服务。
- **模型与渠道管理**：为不同用途配置模型、分组和优先级。
- **访问凭证管理**：创建、分组、停用和查看访问凭证的使用情况。
- **用量与费用查看**：查看请求数量、额度消耗、趋势和明细。
- **用户与权限管理**：支持注册、登录、邀请、分组和管理员权限。
- **多种登录方式**：支持密码登录，以及 GitHub、Discord、Telegram、LinuxDO、OIDC 等登录方式。
- **首页工作台**：使用单屏首页快速进入常用操作，减少冗余介绍内容。
- **外观自定义**：设置主题、主页背景、登录背景、全局背景和背景遮罩。
- **毛玻璃卡片调节**：调整卡片透明度、模糊、边框和阴影，让页面更贴合品牌风格。
- **多语言界面**：支持简体中文、繁体中文、英文、法语、日语、俄语和越南语。
- **消息与通知**：集中查看公告、系统消息和重要提醒。

## 文档与帮助

- [官方文档](https://docs.newapi.pro/zh/docs)
- [安装与部署](https://docs.newapi.pro/zh/docs/installation)
- [常见问题](https://docs.newapi.pro/zh/docs/support/faq)
- [问题反馈](https://github.com/QuantumNous/new-api/issues)
- [最新版本](https://github.com/QuantumNous/new-api/releases)
- [new-api-key-tool](https://github.com/Calcium-Ion/new-api-key-tool)

## 许可证与署名

本项目采用 [GNU Affero General Public License v3.0（AGPLv3）](./LICENSE) 发布。

修改版本必须保留以下署名：

> Frontend design and development by New API contributors.

带有用户界面的修改版本必须保留指向原项目的可见链接：

<https://github.com/QuantumNous/new-api>

本项目基于 [One API](https://github.com/songquanpeng/one-api) 开发。项目由 QuantumNous 与贡献者共同维护。

项目最初来源于 [Calcium-Ion/new-api](https://github.com/Calcium-Ion/new-api)，相关项目身份、作者信息和许可证说明请完整保留。

感谢 [JetBrains](https://www.jetbrains.com/?from=new-api) 为本项目提供免费的开源开发许可证，也感谢所有合作伙伴与贡献者的支持。

## 相关链接

- [项目主页](https://github.com/QuantumNous/new-api)
- [Docker 镜像](https://hub.docker.com/r/CalciumIon/new-api)
- [AtomGit 镜像](https://atomgit.com/QuantumNous/new-api)
- [Star History](https://star-history.com/#Calcium-Ion/new-api&Date)

<div align="center">

感谢使用 New API，欢迎在 [GitHub](https://github.com/QuantumNous/new-api) 提交反馈与改进建议。

<sub>Built with ❤️ by QuantumNous</sub>

</div>
