# 钱包兑换码购买入口更新

- 功能提交：`b344462fa`。
- 兑换码区域显示“点击前往小铺购买兑换码”，“前往小铺”链接至 `https://catfk.com/shop/GOKNGSCL`，在新标签页打开。
- 类型检查、组件 lint、8 个修改文件的格式检查、现有钱包组件测试（2 项）及生产构建通过。
- 已发布前端资源，后端容器启动时间及版本字段保持不变。
- 本机直连入口及三个线上 API 域名的 `/wallet` 页面、入口脚本、样式和钱包脚本均与本次构建逐一校验一致，`/api/status` 正常。

## 发布与回退

- 服务器备份：`/mnt/data200/new-api/backups/shop-link-20261028`。
- 前端目录：`/opt/new-api/caddy/data/newapi-shop-link-20261028`。
- Caddy 的 `@shop_pages` 负责 `/`、`/index.html`、`/wallet`、`/wallet/` 页面；新资源目录置于原静态资源回退链之前，旧资源仍可访问。
- 钱包脚本：`/static/js/async/53250.38db562b87.js`。
- 首页 SHA256：`6698e6a5ccdf269f1ffd8e3c0b63c4925c69a3518266ffdb4e46451a2e197a79`。
- 回退时恢复备份的 `Caddyfile.before` 并校验、平滑重载 Caddy，不操作数据库。
- 后续部署包含此提交的新 Docker 镜像时，应移除两处 `@shop_pages` 页面覆盖规则，使页面由新镜像返回；静态资源目录可以继续保留，兼容未刷新的页面。
