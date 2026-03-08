# DEV 发布准备证据草案（TASK-P0-08）

日期：2026-03-07  
角色：DEV-FULLSTACK  
任务：`TASK-P0-08`  
范围：`RC-08`（配置一致性）+ `RC-09`（回滚演练）

## 1. RC-08 配置一致性（草案）

### 1.1 对齐基准

1. 后端配置定义：`backend/internal/server/config.go`
2. 配置文档：`docs/configuration.md`
3. 前端运行时配置：`frontend/src/runtime-config.json`

### 1.2 验证命令（可复现）

```bash
# 启动（使用文档约定的关键环境变量）
cd backend && \
SERVER_HOST=127.0.0.1 \
SERVER_PORT=18090 \
AUTH_TOKEN=release-token \
DEFAULT_ADAPTER=codex \
ENABLED_ADAPTERS=codex \
CODEX_SEED_FILE=../docs/resume-smoke.jsonl \
FRONTEND_DIR=../frontend/src \
RATE_LIMIT_SESSIONS_PER_SEC=30 \
GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod \
go run ./cmd/server

# 运行态验证
curl -sS http://127.0.0.1:18090/api/v1/health
curl -sS -H 'Authorization: Bearer release-token' http://127.0.0.1:18090/api/v1/adapters
curl -sS -H 'Authorization: Bearer release-token' 'http://127.0.0.1:18090/api/v1/adapters/codex/sessions?limit=1'
cat frontend/src/runtime-config.json
```

### 1.3 输出摘要

1. `health` 返回 `code=0`、`status=ok`。
2. `adapters` 返回 `codex` 且 `default=true`。
3. `sessions` 可返回 `sess_demo_001`。
4. 前端 `runtime-config.json` 包含 `api_base_url/ws_base_url/default_adapter/request_timeout_ms`。

结论（草案）：`RC-08` 具备“配置样例与运行参数一致”的可复现证据，待 QA/Ops 复核签署。

---

## 2. RC-09 回滚演练（草案）

### 2.1 演练目标

验证在错误发布配置下，能够按 Runbook 执行回滚并恢复核心链路：
`health`、`adapters`、`sessions`、`continue`。

### 2.2 演练步骤（可复现）

1. **故障注入（坏配置）**：`ENABLED_ADAPTERS=none` 启动服务。  
2. **观察失败**：服务启动日志出现 `no adapters enabled` 并退出。  
3. **回滚执行**：恢复稳定配置 `ENABLED_ADAPTERS=codex` 并重启。  
4. **回滚验证**：依次调用：
   - `GET /api/v1/health`
   - `GET /api/v1/adapters`
   - `GET /api/v1/adapters/codex/sessions?limit=1`
   - `POST /api/v1/adapters/codex/sessions/sess_demo_001/continue`

### 2.3 关键命令与结果摘录

```text
bad start log:
2026/03/07 20:29:19 no adapters enabled
exit status 1

rollback health: code=0, status=ok
rollback adapters: code=0, items=[codex]
rollback sessions: code=0, has_more=false
rollback continue: code=0, status=accepted, job_id=job_1772886560897014332
```

结论（草案）：`RC-09` 回滚流程可执行且可复现，回滚后核心 API 链路恢复成功，待 QA/Ops 独立复核签署。

---

## 3. 证据文件

1. 原始执行日志：`/tmp/p0-08-evidence.log`
2. 坏配置启动日志：`/tmp/rc09-bad.log`
3. 回滚后启动日志：`/tmp/rc09-good.log`

## 4. 当前状态

`REVIEW`

说明：本报告为 DEV 侧“证据草案”；最终 `RC-08/RC-09` 通过状态以 QA/Ops 复核结果为准。
