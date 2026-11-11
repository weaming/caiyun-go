# caiyun-go

彩云天气 h5 匿名接口的 Go 实现：终端打印对齐表格报告，也作为 HTTP 服务给浏览器渲染同一份文本。

## 用法

```sh
go run . -once [-days 3]                  # 打印报告后退出
go run . -addr :1314 [-days 7]            # HTTP 服务
go run . -addr :3443 -tls-crt c.pem -tls-key k.pem [-http-addr :3080]   # HTTPS，HTTP 口 308 跳转
```

`-days` 范围 1~16（越界夹紧）。坐标缺失时一律用深圳 `114.0579,22.5431`。

接口：

- `GET /`：页面（`page.html` 内嵌），直出深圳报告，随后按浏览器定位/localStorage 更新
- `GET /text?lng=&lat=&days=`：`text/plain` 报告，带 `X-Cells`、`ETag`，支持 304
- `GET /geo?lng=&lat=`：`{"name":"..."}` 地名反查
- `GET /favicon.svg`（`/favicon.ico` 同）：内嵌 SVG 图标

## 缓存

统一 10 分钟，三层：

| 层 | key | 说明 |
|---|---|---|
| 浏览器 localStorage | `snap(lng),snap(lat),days` | 报告正文 + 位置（含地名、`at`） |
| 浏览器 HTTP | URL | `/text` `max-age=600`、`/geo` `86400`，均带 `ETag` |
| 服务端内存 | `"%.2f,%.2f,days"` | + singleflight 合并并发同坐标请求 |

坐标先吸附到 `0.01°`（≈1.1km）网格再参与一切计算（含展示），同格内小幅移动复用缓存，跨格才重新请求上游；
`gridStep`（Go）与 `snap()`（`page.html`）需保持一致。ticket 单独缓存 30 分钟（`~/.cache/caiyun/ticket.json`）。

定位：只用浏览器 `navigator.geolocation`，服务端不存用户位置；位置/报告都 10 分钟过期，刷新或切回前台时
若权限已 `granted` 则静默重定位（位移 <500m 只更新时间戳，0 请求）。Chrome 只在 HTTPS 或 localhost 下允许
定位，其余情况页面提示「定位需 HTTPS」且不请求权限。

## 构建

```sh
make        # dist/caiyun-linux-amd64（zig cc musl 静态）+ dist/caiyun-darwin-arm64
make once ARGS='-days 3'; make serve ADDR=:1314
```

`printable/go-column` 是 cgo + 预编译静态库，**不能** `CGO_ENABLED=0`。

## 上游链路

1. `GET https://h5.caiyunapp.com/h5` 取 `Set-Cookie: ticket`（与 UA 绑定，缓存在 `~/.cache/caiyun/`）。
2. `POST https://h5.caiyunapp.com/api/`，body `{"url","method"}`，服务端替换 `<t2.5>` token 后转发。
3. 只调 `/v2.5/<t2.5>/{lng},{lat}/weather`，一次拿 realtime/hourly/daily/alert。

正文用 `font-size: clamp(9px, (100vw - 2*pad) / 半格数, 12px)`：手机上按最长行正好塞进屏宽（常见 9~11px，偏保守），
桌面封顶 12px。页面不做自己的缩放逻辑，`user-scalable=yes` + `maximum-scale=4` 放开浏览器原生双指缩放，
放大后的拖动由浏览器负责；`overflow-x: hidden` 兜底，任何情况都不出现横向滚动条。

注意：iOS 微信（WKWebView）会按自己的字体档位重排 `font-size`，动态改字号（JS 逐帧设 `font-size`）会被它改回去，
这类需求只能走 `transform` 缩放。
