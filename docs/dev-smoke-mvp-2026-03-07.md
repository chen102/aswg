# DEV Smoke Report (MVP)

日期：2026-03-07  
角色：DEV-FULLSTACK  
任务：`TASK-P0-01`

## 1. 运行前置条件

1. OS: `Linux <host> 6.6.x-microsoft-standard-WSL2 x86_64`
2. Go: `go version go1.22.2 linux/amd64`
3. Node.js: `v24.14.0`
4. npm: `11.9.0`

> 说明：本次在当前执行环境安装了 `golang-go` 后完成后端烟测。

## 2. 环境变量与启动步骤

### 2.1 后端启动环境变量

```bash
SERVER_HOST=127.0.0.1
SERVER_PORT=8080
AUTH_TOKEN=smoke-token
DEFAULT_ADAPTER=codex
ENABLED_ADAPTERS=codex
CODEX_SEED_FILE=../docs/resume-smoke.jsonl
FRONTEND_DIR=../frontend/src
GOCACHE=/tmp/go-build
GOMODCACHE=/tmp/go-mod
```

### 2.2 启动命令

```bash
cd backend && SERVER_HOST=127.0.0.1 SERVER_PORT=8080 AUTH_TOKEN=smoke-token DEFAULT_ADAPTER=codex ENABLED_ADAPTERS=codex CODEX_SEED_FILE=../docs/resume-smoke.jsonl FRONTEND_DIR=../frontend/src GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go run ./cmd/server
```

启动日志摘要：
- `server started on 127.0.0.1:8080`

## 3. 自测命令与结果

### 3.1 Go 测试

命令：
```bash
cd backend && GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./...
```

结果摘要：
```text
? agent-session-web-gateway/backend/cmd/server [no test files]
? agent-session-web-gateway/backend/internal/adapter [no test files]
? agent-session-web-gateway/backend/internal/adapter/codex [no test files]
? agent-session-web-gateway/backend/internal/model [no test files]
? agent-session-web-gateway/backend/internal/server [no test files]
```

结论：命令执行成功（当前尚未补充 Go 单元测试文件）。

### 3.2 REST API 烟测

#### (1) health
命令：
```bash
curl -sS http://127.0.0.1:8080/api/v1/health
```
结果摘要：`code=0`, `status=ok`, `adapters[0].name=codex`

#### (2) adapters
命令：
```bash
curl -sS -H 'Authorization: Bearer smoke-token' http://127.0.0.1:8080/api/v1/adapters
```
结果摘要：`items[0].name=codex`, `capabilities=[discover_sessions,events,continue]`

#### (3) sessions
命令：
```bash
curl -sS -H 'Authorization: Bearer smoke-token' 'http://127.0.0.1:8080/api/v1/adapters/codex/sessions?limit=20'
```
结果摘要：返回 `sess_demo_001`, `has_more=false`

#### (4) session detail
命令：
```bash
curl -sS -H 'Authorization: Bearer smoke-token' http://127.0.0.1:8080/api/v1/adapters/codex/sessions/sess_demo_001
```
结果摘要：返回 `id=sess_demo_001`, `metadata.model=gpt-5-codex`

#### (5) events
命令：
```bash
curl -sS -H 'Authorization: Bearer smoke-token' 'http://127.0.0.1:8080/api/v1/adapters/codex/sessions/sess_demo_001/events?limit=5'
```
结果摘要：返回 `seq=1..5`，含 `message.delta` 与 `message.done`，`next_cursor=c2VxOjU=`

#### (6) continue
命令：
```bash
curl -sS -X POST -H 'Authorization: Bearer smoke-token' -H 'Content-Type: application/json' http://127.0.0.1:8080/api/v1/adapters/codex/sessions/sess_demo_001/continue -d '{"prompt":"请输出 smoke 测试完成确认"}'
```
结果摘要：HTTP 202，返回 `job_id=job_1772881571411462626`, `status=accepted`

### 3.3 WebSocket 烟测（event/heartbeat/done）

命令：
```bash
node /tmp/ws-smoke.mjs
```

输出摘要：
```text
CONTINUE_HTTP 202 ...
WS_SUMMARY {"got_event":true,"got_done":true,"got_heartbeat":true,"frame_count":15}
```

关键帧样例：
```text
WS_FRAME {"frame_type":"event","seq":9,"event_type":"message.delta","text":"收到继续请求，开始执行: "}
WS_FRAME {"frame_type":"done","seq":11,...}
WS_FRAME {"frame_type":"heartbeat","seq":11,...}
```

结论：WS 路径可稳定收到 `event`、`done`、`heartbeat` 三类帧。

补充回归（同日二次执行，含订阅顺序加固后）：
- `WS_SUMMARY {"got_event":true,"got_done":true,"got_heartbeat":true,"frame_count":15}`
- 关键序列样例：`seq=1,2,3,...,11` 单调递增，无乱序样例。

### 3.4 负向场景补充校验

#### (1) 未鉴权访问 adapters
命令：
```bash
curl -sS http://127.0.0.1:8080/api/v1/adapters
```
结果摘要：`code=4010`, `message=unauthorized`

#### (2) sessions limit 越界
命令：
```bash
curl -sS -H 'Authorization: Bearer smoke-token' 'http://127.0.0.1:8080/api/v1/adapters/codex/sessions?limit=999'
```
结果摘要：`code=4001`, `field=limit`, `reason=must be between 1 and 100`

## 4. 服务访问日志摘要

服务日志中记录到本次自测请求：
- `GET /api/v1/health`
- `GET /api/v1/adapters`
- `GET /api/v1/adapters/codex/sessions`
- `GET /api/v1/adapters/codex/sessions/sess_demo_001`
- `GET /api/v1/adapters/codex/sessions/sess_demo_001/events`
- `POST /api/v1/adapters/codex/sessions/sess_demo_001/continue`
- `GET /ws/v1/adapters/codex/sessions/sess_demo_001`

## 5. 已知限制

1. Codex Adapter 当前为 MVP 参考实现（内存态 + seed 数据），尚未对接真实 Codex CLI 子进程。
2. `go test ./...` 已通过，但当前暂无后端单元测试用例文件（均显示 `[no test files]`）。
3. 目前 `go test ./...` 仅验证可编译和基础执行路径，尚未覆盖自动化断言（建议补充单元/集成测试后再作为发布门禁）。

## 6. 当前状态

`REVIEW`

可交付给 QA 的证据已齐备，可据此执行 `docs/adapter-conformance-test.md` 与 `docs/test-plan-mvp.md` 的复测。
