# DEV 修复报告（TASK-P0-03）

日期：2026-03-07  
角色：DEV-FULLSTACK  
任务：`TASK-P0-03`  
目标：修复 `ACT-10/11/17` 并补最小自动化测试

## 1. 修复范围

1. `ACT-10` Continue 取消控制缺失。
2. `ACT-11` `Idempotency-Key` 未去重。
3. `ACT-17` 会话列表无限流，缺失 `4290` 映射。

## 2. 复现与根因

### 2.1 ACT-10
- QA 现象：`ContinueSession` 直接异步发事件，未消费 `ctx`，取消不可控。
- 根因：`backend/internal/adapter/codex/adapter.go` 中 `emitContinueEvents` 不接收 `context.Context`。

### 2.2 ACT-11
- QA 现象：同一 `Idempotency-Key` 连续调用两次 `POST /continue`，返回不同 `job_id`，并产生重复事件。
- 根因：后端与适配器未实现幂等键缓存与命中返回。

### 2.3 ACT-17
- QA 现象：突发 60 次请求均 200，无 `429/4290`。
- 根因：服务层缺少限流组件与错误码映射。

## 3. 修复实现

### 3.1 ACT-10（取消控制）
1. `Codex Adapter` 的 `emitContinueEvents` 新增 `ctx` 参数，发事件前与分片等待时检查取消。
2. `ContinueSession` 在启动前检查 `ctx.Err()`，取消上下文直接返回错误。
3. 为避免 HTTP 请求上下文在 handler 返回后立即取消导致任务提前终止，`POST /continue` 在服务层使用后台上下文触发异步任务，任务取消能力由适配器单测覆盖。

### 3.2 ACT-11（幂等去重）
1. `model.ContinueInput` 新增 `IdempotencyKey` 字段。
2. `POST /continue` 读取 `Idempotency-Key` 请求头并透传到适配器。
3. `Codex Adapter` 新增会话维度幂等缓存（key: `session_id + idempotency_key`，TTL=5分钟）：
- 命中缓存时返回同一 `RunJob`，不重复启动任务。
- 未命中时写入缓存并启动任务。

### 3.3 ACT-17（限流 + 4290）
1. 新增固定窗口限流器：`backend/internal/server/ratelimit.go`。
2. 在 `GET /api/v1/adapters/{adapter}/sessions` 添加限流门禁，超限返回：
- HTTP `429`
- 业务码 `4290`
- 错误体 `type=rate_limited`
3. 配置项新增：`RATE_LIMIT_SESSIONS_PER_SEC`（默认 `30`，`0` 关闭）。

## 4. 新增自动化测试（PM 要求）

### 4.1 ACT-10 单测
文件：`backend/internal/adapter/codex/adapter_test.go`
- `TestContinueSessionRespectsContextCancel`
- 校验：取消后不再产出 `message.done`。

### 4.2 ACT-11 单测
文件：`backend/internal/adapter/codex/adapter_test.go`
- `TestContinueSessionIdempotencyKeyDeduplicates`
- 校验：同 key 返回同 `job_id`，仅触发单次事件流（3条：delta/delta/done）。

### 4.3 ACT-17 单测
文件：`backend/internal/server/ratelimit_test.go`
- `TestSessionsRouteReturns4290WhenRateLimited`
- 校验：突发请求中出现 HTTP429 且业务码 `4290`。

## 5. 回归命令与结果摘要

### 5.1 后端单测
命令：
```bash
cd backend && GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./...
```
结果：
```text
ok   agent-session-web-gateway/backend/internal/adapter/codex   1.304s
ok   agent-session-web-gateway/backend/internal/server          0.007s
```

### 5.2 指定失败项对应测试
命令：
```bash
cd backend && GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./internal/adapter/codex -run 'TestContinueSessionRespectsContextCancel|TestContinueSessionIdempotencyKeyDeduplicates'
cd backend && GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./internal/server -run 'TestSessionsRouteReturns4290WhenRateLimited'
```
结果：均 `ok`。

### 5.3 接口级回归（幂等 + 限流）
命令（摘要）：
```bash
# 同一 Idempotency-Key 连续两次
curl -sS -X POST -H 'Authorization: Bearer smoke-token' -H 'Idempotency-Key: idem-001' .../continue
curl -sS -X POST -H 'Authorization: Bearer smoke-token' -H 'Idempotency-Key: idem-001' .../continue

# 突发 40 次 sessions
for i in $(seq 1 40); do curl .../sessions?limit=1; done
```
结果摘要：
1. 两次 `continue` 返回相同 `job_id=job_1772885101735826104`。
2. 限流触发：`rate_limit_429_count=30`。
3. 429 响应体示例：`{"code":4290,"message":"too many requests",...}`。

## 6. ACT-13 处理（TASK-P0-04 关联）

本轮按选项 B 先提交文档化豁免规则草案（MVP 内存适配器 `ACT-13` 可 `N/A` 的条件），更新于：
- `docs/adapter-conformance-test.md`（新增“6.1 MVP 内存适配器豁免规则”）

说明：该豁免规则需 `PM-AGENT + QA-AGENT` 双签后生效。

## 7. 变更文件

1. `backend/internal/model/model.go`
2. `backend/internal/adapter/codex/adapter.go`
3. `backend/internal/adapter/codex/adapter_test.go`
4. `backend/internal/server/server.go`
5. `backend/internal/server/config.go`
6. `backend/internal/server/ratelimit.go`
7. `backend/internal/server/ratelimit_test.go`
8. `docs/configuration.md`
9. `docs/adapter-conformance-test.md`

## 8. 当前状态

`REVIEW`

等待 QA-AGENT 按 PM 指令发布 `docs/qa-report-mvp-2026-03-08-v3.md`，重点复测 `ACT-10/11/13/17` 与 E2E 阻塞项。
