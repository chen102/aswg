# QA 测试验收报告（MVP v2）

日期：2026-03-07  
角色：QA-AGENT  
任务：`TASK-P0-02`  
状态：`REVIEW`

## 1. 执行基准
1. `docs/test-plan-mvp.md`
2. `docs/adapter-conformance-test.md`
3. `docs/api-contract-v1.md`
4. `docs/dev-smoke-mvp-2026-03-07.md`

## 2. 执行环境
1. OS：Ubuntu 24.04.2 LTS（WSL2）
2. Go：`go version go1.22.2 linux/amd64`
3. Node.js：`v24.14.0`
4. 说明：端口监听与 WebSocket 验证在授权的非沙箱上下文完成。

## 3. 执行摘要
1. 已完成后端编译/测试基线：`cd backend && GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./...`（通过，无测试文件）。
2. 已完成 REST 主链路验证：`health/adapters/sessions/detail/events/continue`。
3. 已完成 WebSocket `event/heartbeat/done` 与断线续传验证。
4. 前端页面级 E2E 自动化仍受执行环境限制（Playwright 在 root + sandbox 组合环境下无法稳定执行 UI 脚本）。

## 4. E2E 用例结果（E2E-01 ~ E2E-10）

| 用例ID | 结果 | 证据/说明 |
| --- | --- | --- |
| E2E-01 | BLOCKED | UI 自动化受阻；API 侧已验证列表链路：`GET /api/v1/adapters`、`GET /api/v1/adapters/codex/sessions` 返回 200。 |
| E2E-02 | BLOCKED | 当前仅 `codex` 单适配器，且 UI 自动化受阻，无法完成页面层“切换适配器刷新”验收。 |
| E2E-03 | BLOCKED | UI 自动化受阻；API 侧 `GET /sessions/{id}` + `GET /events` 已通过。 |
| E2E-04 | PASS | 命令：`POST /continue` + WS 客户端脚本；结果：收到 `event(seq=6,7,8)` + `done(seq=8)`。 |
| E2E-05 | PASS | 命令：WS 重连脚本（先断开后携带 `last_seq` 重连）；结果：续传 `seq=21,22,23`，有序且可续接。 |
| E2E-06 | BLOCKED | UI 自动化受阻，无法在浏览器层验证“设置保存后刷新保持”。 |
| E2E-07 | BLOCKED | UI 自动化受阻，无法在浏览器层验证“连接测试按钮状态展示”。 |
| E2E-08 | BLOCKED | UI 自动化受阻，无法在浏览器层验证“恢复默认配置”。 |
| E2E-09 | BLOCKED | API 侧已验证 `4010`（未鉴权访问 `/adapters` 返回 401）；UI 层“修复提示文案”未完成自动化验收。 |
| E2E-10 | PASS | 命令：`GET /api/v1/adapters/codex/sessions/not_exist`；结果：HTTP 404，业务码 `4003`。 |

## 5. 适配器一致性结果（ACT-01 ~ ACT-17）

| 用例ID | 结果 | 证据/说明 |
| --- | --- | --- |
| ACT-01 | PASS | `GET /api/v1/adapters` 返回 `name=codex`，稳定唯一。 |
| ACT-02 | PASS | `GET /sessions?limit=20` 返回合法分页结构，`updated_at` 可排序。 |
| ACT-03 | PASS | `query/workspace` 过滤命中与未命中均符合预期。 |
| ACT-04 | PASS | `limit=101` 返回 HTTP 400 + `code=4001`。 |
| ACT-05 | PASS | `GET /sessions/sess_demo_001` 返回完整 `SessionDetail` 字段。 |
| ACT-06 | PASS | `GET /sessions/not_exist` 返回 HTTP 404 + `code=4003`。 |
| ACT-07 | PASS | `events?limit=2` + `next_cursor` 续页后 `seq` 单调（`1,2` -> `3,4`）。 |
| ACT-08 | PASS | WS 断线续传验证通过，`last_seq` 续接后顺序正确。 |
| ACT-09 | PASS | `continue` 后可观察 `message.delta` 到 `done` 完整流程。 |
| ACT-10 | FAIL | 代码证据：`backend/internal/adapter/codex/adapter.go` 中 `ContinueSession` 直接 `go a.emitContinueEvents(...)`，未接入 `ctx` 取消控制。 |
| ACT-11 | FAIL | 同一 `Idempotency-Key` 连续两次 `POST /continue` 均返回 202 且 `job_id` 不同，`delta_seq=6`（重复启动两轮事件）。 |
| ACT-12 | PASS | 并发订阅 fanout 验证：3 个 WS 客户端均收到 `event=3` 与 `done=1`。 |
| ACT-13 | BLOCKED | MVP 适配器为内存实现，无子进程执行路径，无法执行“子进程异常映射”场景。 |
| ACT-14 | PASS | 启动时注入损坏 seed 数据后服务不崩溃，`health/events` 仍可用。 |
| ACT-15 | PASS | token 日志脱敏验证：请求携带 `Bearer s3cr3t-token` 后日志文件未检出 token 字面量。 |
| ACT-16 | PASS | 未鉴权调用返回 HTTP 401 + `code=4010`。 |
| ACT-17 | FAIL | 60 次突发请求全部 HTTP 200，未出现 `429`/`4290`。 |

## 6. 关键命令与结果摘录

1. 后端测试：
```bash
cd backend && GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./...
```
结果：命令执行成功（当前无测试文件）。

2. 典型错误码验证：
```bash
curl -i http://127.0.0.1:18080/api/v1/adapters
```
结果：HTTP 401，`code=4010`。

3. WS 流式验证：
```bash
node <ws-client-script>
```
结果：`event` + `done` + `heartbeat` 均可观察。

4. 幂等键验证（失败）：
```bash
# 相同 Idempotency-Key 连续两次 continue
```
结果：两次均返回 202，不同 `job_id`，事件序列增长 6。

5. 限流验证（失败）：
```bash
for i in $(seq 1 60); do curl .../sessions?limit=1; done
```
结果：`60 x HTTP 200`，无 `429/4290`。

## 7. 缺陷分级

1. `DEF-S2-002`（ACT-10）：Continue 取消控制缺失。  
   - 文件：`backend/internal/adapter/codex/adapter.go`  
   - 接口：`POST /api/v1/adapters/{adapter}/sessions/{id}/continue`  
   - 复现：代码路径可见 `ContinueSession` 未消费 `ctx`，任务不可取消。

2. `DEF-S2-003`（ACT-11）：`Idempotency-Key` 未生效。  
   - 接口：`POST /api/v1/adapters/codex/sessions/sess_demo_001/continue`  
   - 复现：同一 key 请求两次均返回新 `job_id`，并产生重复事件。

3. `DEF-S2-004`（ACT-17）：限流行为未实现。  
   - 接口：`GET /api/v1/adapters/codex/sessions`  
   - 复现：60 次突发请求均 200，无 4290。

4. `ENV-BLOCK-001`（E2E-01/02/03/06/07/08/09）：页面级自动化受环境限制。  
   - 现象：Playwright 在当前 root + sandbox 环境下出现浏览器沙箱冲突/包加载失败。  
   - 影响：上述 UI 用例仅完成 API 侧替代验证，未完成浏览器交互闭环验收。

5. `ENV-BLOCK-002`（ACT-13）：MVP 适配器无子进程路径。  
   - 影响：无法验证“子进程异常映射”场景。

## 8. S0/S1 结论
1. 本轮未发现 `S0`。
2. 本轮未发现新增 `S1`。
3. 但存在多个 `S2` 失败 + 阻塞项，发布门禁仍不满足。

## 9. Go/No-Go 结论
结论：`No-Go`

原因：
1. `RC-06`（核心 E2E 全通过）未满足：多项 UI E2E 处于 `BLOCKED`。
2. `RC-07`（适配器一致性）未满足：`ACT-10/11/17` 失败，`ACT-13` 阻塞。

## 10. 对 DEV-FULLSTACK 的修复建议
1. 在 Continue 任务链路引入可取消上下文并提供可观测退出。
2. 实现 `Idempotency-Key` 去重（至少在会话+key 窗口内单次执行）。
3. 增加请求限流策略与 `4290` 映射。
4. 为子进程异常场景补齐可测实现或提供可替代一致性测试策略说明。
