# DEV 真实流式桥接报告（TASK-P2-01）

日期：2026-03-07  
角色：DEV-FULLSTACK  
任务：`TASK-P2-01`  
目标：Codex Adapter 从内存 mock 升级为真实 CLI 子进程流式桥接，默认走真实路径，mock 仅作为本地可选 fallback

## 1. 变更摘要

1. `continue` 默认改为真实 CLI 执行链路：
   - 启动 `codex exec --json <prompt>` 子进程。
   - 解析 JSONL 事件并转为统一 `message.delta` / `message.done`。
2. 保留本地 fallback 能力（非默认）：
   - `CODEX_MOCK_FALLBACK=true` 时，真实流失败且未产出 delta 才回退 mock。
3. 错误语义补齐：
   - 子进程不可执行（如 binary 不存在）在 `POST /continue` 直接失败，映射 `4004`。
   - 运行期失败（如超时）输出 `message.done`，`payload.raw_type=assistant_error`，保留错误文本。
4. 过滤非对话内容：
   - 对 `item.completed` 事件中的 `reasoning` 类型不入对话流。
5. 补齐运行时配置项并更新文档。

## 2. 变更文件

1. `backend/internal/adapter/codex/adapter.go`
2. `backend/internal/adapter/codex/adapter_test.go`
3. `backend/internal/server/create_session_test.go`
4. `backend/internal/server/ratelimit_test.go`
5. `docs/configuration.md`
6. `docs/api.md`
7. `docs/api-contract-v1.md`
8. `README.md`

## 3. 启动参数（真实流）

建议参数：

```bash
cd backend
SERVER_HOST=127.0.0.1 \
SERVER_PORT=18132 \
AUTH_TOKEN=qa-token \
CODEX_STREAM_MODE=real \
CODEX_CLI_BIN=codex \
CODEX_CLI_ARGS='exec --json' \
CODEX_CLI_TIMEOUT_MS=180000 \
CODEX_MOCK_FALLBACK=0 \
GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod \
go run ./cmd/server
```

## 4. 最小复现命令

### 4.1 成功流式

```bash
# 1) 创建会话
curl -sS -H 'Authorization: Bearer qa-token' -H 'Content-Type: application/json' \
  -d '{"title":"P2 Real Stream Evidence","workspace":"/workspace/p2"}' \
  http://127.0.0.1:18132/api/v1/adapters/codex/sessions

# 2) 发起 continue
curl -sS -H 'Authorization: Bearer qa-token' -H 'Content-Type: application/json' \
  -d '{"prompt":"请用中文回答：给我6条关于提升工程交付效率的可执行建议，每条15到25字，换行分隔。"}' \
  http://127.0.0.1:18132/api/v1/adapters/codex/sessions/<sid>/continue

# 3) 拉取事件
curl -sS -H 'Authorization: Bearer qa-token' \
  'http://127.0.0.1:18132/api/v1/adapters/codex/sessions/<sid>/events?limit=400'
```

成功证据见：`docs/evidence/p2-realstream-success.log`

### 4.2 超时样例

服务参数：`CODEX_CLI_TIMEOUT_MS=1`  
证据见：`docs/evidence/p2-realstream-timeout.log`

### 4.3 子进程失败样例

服务参数：`CODEX_CLI_BIN=/not-found/codex`  
证据见：`docs/evidence/p2-realstream-subprocess.log`

### 4.4 鉴权样例

无 token 访问受保护接口，证据见：`docs/evidence/p2-realstream-auth.log`

## 5. 关键证据摘录

### 5.1 完整流式（至少 3 个 delta）

来自 `docs/evidence/p2-realstream-success.log`：

1. `assistant_delta_count=3`
2. 事件顺序：
   - `seq=1 message.user`
   - `seq=2 message.delta`
   - `seq=3 message.delta`
   - `seq=4 message.delta`
   - `seq=5 message.done`

### 5.2 超时语义

来自 `docs/evidence/p2-realstream-timeout.log`：

1. `POST /continue` 返回 `202`（任务已受理）。
2. 事件流输出：
   - `message.user`
   - `message.done`，`payload.raw_type=assistant_error`
   - 文本：`continue 执行失败: codex cli timeout after 1ms`

### 5.3 子进程失败映射

来自 `docs/evidence/p2-realstream-subprocess.log`：

1. `POST /continue` 返回 `HTTP 500`。
2. 业务码：`4004`。
3. 错误详情：`codex cli not found ...`。
4. 响应中包含 `request_id`，可用于链路追踪。

### 5.4 鉴权语义

来自 `docs/evidence/p2-realstream-auth.log`：

1. 未鉴权请求返回 `HTTP 401` + `code=4010`。
2. 响应包含 `request_id`。

## 6. 回归验证

```bash
cd backend && GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./...
node --check frontend/src/app.js
```

结果：通过。

## 7. 已知限制

1. 真实流解析依赖 `codex exec --json` 事件格式；若 CLI 事件结构变更需同步解析器。
2. 当前对话增量主要来源于 `item.completed(agent_message)` 的文本切片；未来可扩展到更细粒度事件。
3. 运行期异常目前以会话事件反馈，不会改变已返回的 `202` 结果。

## 8. 当前状态

`REVIEW`

等待 QA-AGENT 执行 `TASK-P2-02` 实时对话专项验收。
