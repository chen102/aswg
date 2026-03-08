# PicoClaw `pico` Channel 接入 ASWG 方案（系统版）

更新时间：2026-03-08  
适用范围：`agent-session-web-gateway`（ASWG）对接 `PicoClaw` 的 `pico` channel

---

## 1. 调研结论（先说结论）
1. `PicoClaw` 主仓库源码中，`pico` channel 已存在，且是一个**内置 WebSocket 服务端**实现（不是外部桥接）。
2. `pico` channel 的接入入口是共享 Gateway HTTP 服务上的 `GET /pico/ws`。
3. 鉴权为 Bearer Token，支持可选 query token（仅在配置显式开启时）。
4. `pico` channel 本身不提供“会话列表/历史查询 API”，仅提供 WS 实时消息通道，因此 ASWG 需要补一层“会话索引与事件持久化”。
5. 官方站点有 `2.0.0` 迁移语义（配置迁移文档），但 GitHub release/tag 目前仍是 `v0.x` 系列；因此建议按“源码能力”而非“版本号字面”做接入。

---

## 2. 证据与来源
### 2.1 官方文档来源
1. 官网文档首页：<https://docs.picoclaw.io/docs/>
2. 安装与迁移语义（提到从 `v2.0.0` 起移除 legacy providers 配置）：<https://docs.picoclaw.io/docs/installation>
3. 模型配置迁移文档：<https://docs.picoclaw.io/docs/migration/model-list-migration>
4. GitHub Releases（当前公开 release 仍以 `v0.x` 为主）：<https://github.com/sipeed/picoclaw/releases>

### 2.2 源码来源（本地调研快照）
仓库：`https://github.com/sipeed/picoclaw`  
调研本地路径：`/tmp/picoclaw-src-lite`  
快照 commit：`81dfdf5`

关键代码点：
1. `pico` channel 实现：[/tmp/picoclaw-src-lite/pkg/channels/pico/pico.go](/tmp/picoclaw-src-lite/pkg/channels/pico/pico.go)
2. `pico` 协议消息类型：[/tmp/picoclaw-src-lite/pkg/channels/pico/protocol.go](/tmp/picoclaw-src-lite/pkg/channels/pico/protocol.go)
3. channel 工厂注册：[/tmp/picoclaw-src-lite/pkg/channels/pico/init.go](/tmp/picoclaw-src-lite/pkg/channels/pico/init.go)
4. 管理器中启用条件（`channels.pico.enabled && token!=empty`）：[/tmp/picoclaw-src-lite/pkg/channels/manager.go:267](/tmp/picoclaw-src-lite/pkg/channels/manager.go:267)
5. 共享 HTTP 服务器挂载 webhook/channel 路径：[/tmp/picoclaw-src-lite/pkg/channels/manager.go:285](/tmp/picoclaw-src-lite/pkg/channels/manager.go:285)
6. 网关启动逻辑（共享 HTTP + channels）：[/tmp/picoclaw-src-lite/cmd/picoclaw/internal/gateway/helpers.go:183](/tmp/picoclaw-src-lite/cmd/picoclaw/internal/gateway/helpers.go:183)
7. `PicoConfig` 字段定义：[/tmp/picoclaw-src-lite/pkg/config/config.go:405](/tmp/picoclaw-src-lite/pkg/config/config.go:405)

---

## 3. `pico` channel 能力模型（和 ASWG 关注点）
### 3.1 连接与鉴权
1. WS 路径：`/pico/ws`（通过共享 Gateway 暴露）
2. 鉴权：
   - Header：`Authorization: Bearer <token>`
   - Query：`token=<token>`（仅当 `allow_token_query=true`）
3. 可配置项（`channels.pico`）：
   - `enabled`
   - `token`
   - `allow_token_query`
   - `allow_origins`
   - `ping_interval`
   - `read_timeout`
   - `write_timeout`
   - `max_connections`
   - `allow_from`
   - `placeholder`

### 3.2 消息协议（双向）
1. 客户端 -> 服务端：
   - `message.send`
   - `ping`
2. 服务端 -> 客户端：
   - `message.create`
   - `message.update`
   - `typing.start`
   - `typing.stop`
   - `error`
   - `pong`

说明：协议里定义了 `media.send/media.create` 常量，但当前 `handleMessage` 仅显式处理 `message.send/ping`，媒体路径在当前实现中不是主流程能力。

### 3.3 会话语义
1. `session_id` 来自 query（可传），不传则服务端生成 UUID。
2. chatID 内部约定：`pico:<session_id>`。
3. channel 不提供 session 列表和历史查询 API。

---

## 4. ASWG 接入目标定义
在不破坏现有 Codex Adapter 的前提下，新增 `picoclaw` 适配器，使 ASWG 支持：
1. `GET /adapters` 能看到 `picoclaw`。
2. 会话创建与管理（ASWG 侧维护会话索引）。
3. Web 对话实时流式可见（通过 Pico WS 桥接）。
4. 断线重连、错误映射、鉴权语义统一到 ASWG 现有契约。
5. 历史可重建（ASWG 侧持久化事件，不依赖 Pico 提供历史接口）。

---

## 5. 总体架构（推荐）
### 5.1 组件关系
1. ASWG Frontend（现有）
2. ASWG Backend（新增 `picoclaw` adapter）
3. PicoClaw Gateway（外部服务，`/pico/ws`）

### 5.2 核心思路
1. ASWG 作为 **Pico WS 客户端**（不是服务端代理）。
2. 每个 ASWG session 对应一个 `pico session_id`。
3. ASWG 侧维护：
   - session 索引（用于 discover/list）
   - event append-only 日志（用于 detail/events/rebuild）
4. Continue 时：
   - ASWG 发送 `message.send` 到 Pico WS
   - 将 Pico 返回的 `message.create/update/typing/error` 映射为 ASWG 规范事件（`message.delta/done/action/error`）

---

## 6. 接口映射设计
### 6.1 ASWG -> Pico 消息映射
1. `POST /continue`  
   输入 `prompt` -> Pico `message.send.payload.content`

### 6.2 Pico -> ASWG 事件映射（建议）
1. `message.create` -> `message.delta`（assistant，初始化块）
2. `message.update` -> `message.delta`（assistant，增量/覆盖块）
3. `typing.start` -> `message.action`（`assistant_typing_start`）
4. `typing.stop` -> `message.done`（assistant 回合边界）
5. `error` -> `message.done + assistant_error`（并带错误信息）

### 6.3 done 边界策略
1. 优先用 `typing.stop` 判定本轮 assistant 结束。
2. 若无 typing 事件，则以“空闲超时（如 1200ms）”补 done。
3. 新一轮 `message.send` 前，强制收敛上一轮未 done 草稿。

---

## 7. ASWG 代码改造点（拟定）
### 7.1 后端
1. 新增目录：`backend/internal/adapter/picoclaw/`
2. 主要文件建议：
   - `adapter.go`（实现 Adapter 接口）
   - `ws_client.go`（连接管理、重连、心跳）
   - `mapper.go`（Pico/ASWG 事件映射）
   - `store.go`（会话与事件持久化）
   - `adapter_test.go`（单测）
3. Registry 注册：在 `adapter registry` 增加 `picoclaw`。
4. 配置项新增建议：
   - `PICOCLAW_WS_BASE_URL`（例：`ws://127.0.0.1:18790`）
   - `PICOCLAW_TOKEN`
   - `PICOCLAW_ALLOW_INSECURE_QUERY_TOKEN`（默认 false）
   - `PICOCLAW_CONNECT_TIMEOUT_MS`
   - `PICOCLAW_IDLE_DONE_MS`
   - `PICOCLAW_RECONNECT_BACKOFF_MS`
   - `PICOCLAW_MAX_RECONNECT_MS`

### 7.2 前端
1. 设置页新增可选参数：
   - `picoclaw_ws_base_url`
   - `picoclaw_token`（可为空，由后端统一管理时不暴露前端）
2. 适配器选择支持 `codex/picoclaw`。
3. UI 无需大改，复用现有消息气泡与流式事件渲染。

### 7.3 数据持久化
1. 推荐沿用 ASWG 现有事件存储风格（按 session append）。
2. 至少落地字段：
   - `session_id`
   - `adapter = picoclaw`
   - `seq`
   - `ts`
   - `raw_type`
   - `normalized.role/text/done/action`
   - `metadata`（原始 pico payload、pico_msg_id）

---

## 8. 安全与运维要求
1. 默认仅允许 Bearer Token 鉴权，不启用 query token。
2. ASWG 到 Pico 的 token 存后端环境变量，不落前端。
3. Pico 侧 `allow_origins` 如需开放浏览器直连，必须最小化白名单。
4. 接入公网必须走 HTTPS/WSS + 反代鉴权与限流。
5. 日志脱敏：token、Authorization、query token 统一掩码。

---

## 9. 分阶段里程碑（可直接执行）
### Phase A（1-2 天）：最小可用桥接
1. 接入 `picoclaw` adapter + `continue` 单轮通路。
2. WS 收到 `message.create/update` 可在前端实时显示。
3. 基础错误映射（4010/4004/5000）。

验收：
1. 新建 session -> continue -> 实时收到 assistant 回复。
2. 两轮连续对话均可成功。

### Phase B（1-2 天）：会话与历史
1. 完成 session 索引与 events 持久化。
2. 刷新后历史可重建。
3. `GET /sessions` 支持稳定分页排序。

验收：
1. 历史会话可见、可打开、可继续。
2. 长对话不丢事件顺序。

### Phase C（1 天）：稳定性与发布门禁
1. 重连策略、心跳、idle-done 补偿完善。
2. 压测与异常注入（Pico 重启、网络抖动、token 错误）。
3. 完成 QA 报告与 release checklist 回填。

---

## 10. 风险与规避
1. 风险：Pico 协议无显式 `message.done`。  
规避：`typing.stop + idle timeout` 双保险。
2. 风险：Pico 不提供历史查询。  
规避：ASWG 本地事件持久化为真源。
3. 风险：文档站点尚无独立 pico channel 页。  
规避：以源码行为为准，先实现灰度接入，再补兼容层。
4. 风险：版本语义（2.0.0）与 release tag（0.x）不一致。  
规避：锁定 commit + 协议契约测试，不依赖版本名字符串。

---

## 11. 测试清单（接入专项）
1. 鉴权：
   - Bearer token 正常
   - token 错误 -> 4010
2. 实时：
   - 单轮 >= 3 段增量可见
   - typing.start/stop 行为正确
3. 历史：
   - 刷新重建
   - 分页稳定
4. 稳定性：
   - Pico 重启后自动恢复
   - 网络抖动不丢会话
5. 安全：
   - 日志不泄露 token
   - query token 默认禁用

---

## 12. 立即可执行的下一步
1. 在 ASWG 建分支：`feat/picoclaw-pico-adapter`
2. 先落 Phase A（最小桥接），并提供一份 `dev-smoke-picoclaw.md`
3. QA 同步准备 `qa-picoclaw-checklist.md`，按上面 11 项执行
