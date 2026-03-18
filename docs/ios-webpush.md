# iOS Web Push 使用说明（ASWG）

更新时间：2026-03-18  
适用范围：ASWG 内置 Web Push，iPhone（无 Mac 也可）

## 1. 目标与边界
1. 在 iPhone 上接收 ASWG 会话通知。
2. 通知点击后跳转到对应会话（`adapter + session_id` 深链）。
3. 不依赖 App Store，不需要原生 App。

边界说明：
1. 本文是 ASWG 项目内置推送链路的操作文档，不是通用 iOS 推送开发教程。
2. 推送测试以“主屏 Web App”场景为准（不是普通 Safari 标签页）。

## 2. 推送链路概览
ASWG 当前链路如下：
1. 前端在用户允许通知后，创建浏览器 Push Subscription。
2. 前端把订阅信息上报到后端接口：
   - `POST /api/v1/push/subscriptions`
   - `POST /api/v1/push/subscriptions/remove`
3. 后端把订阅持久化到 `WEBPUSH_SUBSCRIPTION_FILE`。
4. 会话出现 `assistant` 消息后，后端发送 Web Push。
5. Service Worker 收到推送并展示通知，点击后按 URL 参数回到对应会话。

相关实现位置：
1. `frontend/src/app.js`
2. `frontend/src/sw.js`
3. `backend/internal/server/push.go`

## 3. 前置条件
1. 站点必须可通过 HTTPS 访问（iPhone 上推送必需）。
2. 后端已配置完整 VAPID 三元组：
   - `WEBPUSH_VAPID_PUBLIC_KEY`
   - `WEBPUSH_VAPID_PRIVATE_KEY`
   - `WEBPUSH_VAPID_SUBJECT`
3. iPhone 已允许该 Web App 通知。
4. 你从 iPhone 主屏图标打开 ASWG（不是 Safari 标签页）。

## 4. 后端配置（.env.local）
建议在仓库根目录 `./.env.local` 中配置：

```bash
# 必填：内置 Web Push 发送能力
WEBPUSH_VAPID_PUBLIC_KEY=<your-public-key>
WEBPUSH_VAPID_PRIVATE_KEY=<your-private-key>
WEBPUSH_VAPID_SUBJECT=https://<your-public-domain>

# 可选：订阅存储文件位置
WEBPUSH_SUBSCRIPTION_FILE=.run/webpush-subscriptions.json
WEBPUSH_TTL_SECONDS=60
```

重启服务：

```bash
cd /home/chenxi/aswg
./scripts/aswg restart
```

## 5. 前端设置怎么填
进入页面右上角「连接设置」，重点是这三个字段。

### 5.1 `push_subscribe_url`
填写：

```text
https://<你的域名>/api/v1/push/subscriptions
```

### 5.2 `push_unsubscribe_url`
填写：

```text
https://<你的域名>/api/v1/push/subscriptions/remove
```

### 5.3 `push_vapid_public_key`
填写：

```text
<与 WEBPUSH_VAPID_PUBLIC_KEY 完全一致的公钥>
```

保存配置后点击「开启通知」。

说明：
1. 如果 `push_subscribe_url` / `push_unsubscribe_url` 留空，前端会默认用 `api_base_url` 拼接本地接口。
2. 开源仓库建议保持 `frontend/src/runtime-config.json` 中 `push_vapid_public_key` 为空，避免提交个人环境值。

## 6. iPhone 端操作流程（无 Mac）
1. 用 Safari 打开你的 HTTPS 地址。
2. 点击分享，选择「添加到主屏幕」。
3. 从主屏图标打开 ASWG。
4. 进入「连接设置」，保存推送参数并点击「开启通知」。
5. 在 iOS 系统设置中确认该 App 通知权限已开启（锁屏/横幅/声音）。

## 7. 验证步骤
1. 在电脑端触发一条 `assistant` 消息（建议用非手机当前会话触发）。
2. 把 iPhone Web App 切后台或锁屏。
3. 观察是否收到通知。
4. 点击通知，确认是否跳到对应会话。

服务侧检查：

```bash
cd /home/chenxi/aswg
./scripts/aswg logs -f
```

并确认订阅文件非空：

```bash
cat /home/chenxi/aswg/.run/webpush-subscriptions.json
```

## 8. 常见问题
### 8.1 点了“开启通知”但没收到
按顺序检查：
1. 是否 HTTPS。
2. 是否从主屏图标打开。
3. `push_vapid_public_key` 是否与后端公钥一致。
4. iOS 系统通知权限是否打开。
5. 是否是旧 Service Worker 缓存（关闭主屏 Web App 后重开）。

### 8.2 能收到通知，但点开不是目标会话
1. 旧通知可能仍是旧链接，先触发一条新通知再测。
2. 检查通知 payload 是否带 `adapter/session_id`。
3. 确认前端 URL 参数解析逻辑已是最新版本。

### 8.3 只在前台有消息，后台无通知
1. 前台时系统可能不弹横幅，先锁屏复测。
2. 推送仅对 `assistant` 消息生效，`user` 输入不会触发推送。

## 9. 隐私与开源发布建议
1. 不要提交 `.env.local`。
2. 不要提交生产订阅数据文件（`webpush-subscriptions.json`）。
3. `runtime-config.json` 中不要写入个人公钥/域名。
4. 生产环境建议通过反向代理限制推送订阅接口来源。

当前代码现状说明：
1. 推送订阅接口是：
   - `POST /api/v1/push/subscriptions`
   - `POST /api/v1/push/subscriptions/remove`
2. 这两个接口当前未走 `AUTH_TOKEN` 鉴权流程，请务必在公网侧加额外访问控制（WAF、IP 白名单、网关鉴权或反代规则）。

## 10. 快速参数模板
可直接替换下面模板：

```text
api_base_url=https://<你的域名>
ws_base_url=wss://<你的域名>
push_subscribe_url=https://<你的域名>/api/v1/push/subscriptions
push_unsubscribe_url=https://<你的域名>/api/v1/push/subscriptions/remove
push_vapid_public_key=<你的 WEBPUSH_VAPID_PUBLIC_KEY>
```
