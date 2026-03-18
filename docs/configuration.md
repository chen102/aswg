# 配置说明

## 1. 设计原则
1. 所有可变参数外置。
2. 前端支持运行时覆盖，不强制重新构建。
3. 配置项使用清晰默认值。

## 2. 后端配置
推荐使用环境变量 + 配置文件覆盖。

关键配置项：
1. `SERVER_HOST`：服务监听地址，默认 `127.0.0.1`
2. `SERVER_PORT`：服务端口，默认 `8080`
3. `AUTH_TOKEN`：API 鉴权令牌
4. `ENABLED_ADAPTERS`：启用的适配器列表
5. `DEFAULT_ADAPTER`：默认适配器
6. `LOG_LEVEL`：日志级别
7. `RATE_LIMIT_SESSIONS_PER_SEC`：会话列表接口每秒限流阈值（默认 `30`，`0` 表示关闭）
8. `CODEX_STREAM_MODE`：Codex 续接模式，`real|mock`（默认 `real`）
9. `CODEX_CLI_BIN`：真实模式下调用的 CLI 可执行文件（默认 `codex`）
10. `CODEX_CLI_ARGS`：真实模式 CLI 参数（默认 `exec --json`）
11. `CODEX_CLI_TIMEOUT_MS`：真实模式单次 continue 子进程超时（默认 `300000`）
12. `CODEX_MOCK_FALLBACK`：真实模式失败时是否回退 mock（默认 `false`）
13. `CODEX_HISTORY_ENABLED`：是否默认读取本机历史会话（默认 `true`）
14. `CODEX_HISTORY_DIR`：历史会话目录（默认 `~/.codex/sessions`）
15. `CODEX_HISTORY_SCAN_TTL_MS`：历史索引缓存时间（默认 `5000`）
16. `PICOCLAW_WS_BASE_URL`：Pico WebSocket 基地址（默认 `ws://127.0.0.1:8080`）
17. `PICOCLAW_TOKEN`：Pico 鉴权 Token（启用 `picoclaw` 适配器时必填）
18. `PICOCLAW_ALLOW_TOKEN_QUERY`：是否允许 token 走 Query（默认 `false`）
19. `PICOCLAW_DIAL_TIMEOUT_MS`：Pico 连接握手超时（默认 `5000`）
20. `PICOCLAW_CONTINUE_TIMEOUT_MS`：单次 continue 总超时（默认 `120000`）
21. `PICOCLAW_READ_IDLE_TIMEOUT_MS`：流式读取空闲超时（默认 `45000`）
22. `PUSH_WEBHOOK_URL`：消息推送 Webhook 地址（留空则关闭推送）
23. `PUSH_WEBHOOK_AUTH_BEARER`：推送请求 Bearer 鉴权（可选）
24. `PUSH_WEBHOOK_HMAC_SECRET`：推送签名密钥（可选，生成 `X-ASWG-Signature`）
25. `PUSH_WEBHOOK_TIMEOUT_MS`：推送 HTTP 超时（默认 `3000`）
26. `PUSH_QUEUE_SIZE`：推送队列长度（默认 `512`）
27. `PUSH_DEDUPE_TTL_MS`：重复推送去重窗口（默认 `1800000`）
28. `WEBPUSH_VAPID_PUBLIC_KEY`：内置 Web Push VAPID 公钥（可选）
29. `WEBPUSH_VAPID_PRIVATE_KEY`：内置 Web Push VAPID 私钥（可选）
30. `WEBPUSH_VAPID_SUBJECT`：内置 Web Push VAPID Subject（如 `mailto:ops@example.com`）
31. `WEBPUSH_SUBSCRIPTION_FILE`：内置 Web Push 订阅持久化文件（默认 `.run/webpush-subscriptions.json`）
32. `WEBPUSH_TTL_SECONDS`：内置 Web Push 通知 TTL（默认 `60`）

## 3. 前端配置
前端采用两层配置：
1. 默认配置文件：`frontend/src/runtime-config.json`
2. 本地覆盖配置：`localStorage`（来自设置页）

`runtime-config.json` 示例：
```json
{
  "api_base_url": "http://127.0.0.1:8080",
  "ws_base_url": "ws://127.0.0.1:8080",
  "default_adapter": "codex",
  "request_timeout_ms": 30000
}
```

## 4. 前端设置页要求
1. 可编辑 `api_base_url`。
2. 可编辑 `ws_base_url`。
3. 可编辑 `default_adapter`。
4. 提供“连接测试”按钮。
5. 提供“恢复默认配置”按钮。

## 5. 配置校验建议
1. 地址格式校验（http/https、ws/wss）。
2. 超时阈值校验（最小值/最大值）。
3. 适配器名称白名单校验。

## 6. 发布注意事项
1. 不提交真实 Token。
2. 提交 `config.example.*`，不提交私有 `config.*`。
3. CI 中增加配置文件结构校验。

## 7. MVP 环境变量（后端）
1. `SERVER_HOST`：默认 `127.0.0.1`
2. `SERVER_PORT`：默认 `8080`
3. `AUTH_TOKEN`：为空时不强制鉴权
4. `ENABLED_ADAPTERS`：默认 `codex`
5. `DEFAULT_ADAPTER`：默认 `codex`
6. `CODEX_SEED_FILE`：默认 `docs/resume-smoke.jsonl`
7. `FRONTEND_DIR`：默认 `frontend/src`
8. `RATE_LIMIT_SESSIONS_PER_SEC`：默认 `30`
9. `CODEX_STREAM_MODE`：默认 `real`
10. `CODEX_CLI_BIN`：默认 `codex`
11. `CODEX_CLI_ARGS`：默认 `exec --json`
12. `CODEX_CLI_TIMEOUT_MS`：默认 `300000`
13. `CODEX_MOCK_FALLBACK`：默认 `false`
14. `CODEX_HISTORY_ENABLED`：默认 `true`
15. `CODEX_HISTORY_DIR`：默认 `~/.codex/sessions`
16. `CODEX_HISTORY_SCAN_TTL_MS`：默认 `5000`
17. `PICOCLAW_WS_BASE_URL`：默认 `ws://127.0.0.1:8080`
18. `PICOCLAW_TOKEN`：默认空（启用 `picoclaw` 时必填）
19. `PICOCLAW_ALLOW_TOKEN_QUERY`：默认 `false`
20. `PICOCLAW_DIAL_TIMEOUT_MS`：默认 `5000`
21. `PICOCLAW_CONTINUE_TIMEOUT_MS`：默认 `120000`
22. `PICOCLAW_READ_IDLE_TIMEOUT_MS`：默认 `45000`
23. `PUSH_WEBHOOK_URL`：默认空（关闭）
24. `PUSH_WEBHOOK_AUTH_BEARER`：默认空
25. `PUSH_WEBHOOK_HMAC_SECRET`：默认空
26. `PUSH_WEBHOOK_TIMEOUT_MS`：默认 `3000`
27. `PUSH_QUEUE_SIZE`：默认 `512`
28. `PUSH_DEDUPE_TTL_MS`：默认 `1800000`
29. `WEBPUSH_VAPID_PUBLIC_KEY`：默认空（不启用内置 Web Push 发送）
30. `WEBPUSH_VAPID_PRIVATE_KEY`：默认空
31. `WEBPUSH_VAPID_SUBJECT`：默认空
32. `WEBPUSH_SUBSCRIPTION_FILE`：默认 `.run/webpush-subscriptions.json`
33. `WEBPUSH_TTL_SECONDS`：默认 `60`
