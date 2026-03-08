# QA 测试验收报告（MVP v3）

日期：2026-03-07  
角色：QA-AGENT  
任务：`TASK-P0-06`  
状态：`DONE`

## 1. 报告目标
本报告用于完成 PM 在 `TASK-P0-06` 的最终复测要求，重点确认：
1. `ACT-10/11/13/17` 最终状态。
2. `E2E-01~10` 最终状态（含 `E2E-02` 的 N/A 范围裁定）。
3. `RC-06/RC-07` 是否满足并给出最终 `Go/No-Go`。

## 2. 执行环境
1. OS：Ubuntu 24.04.2 LTS（WSL2）
2. Go：`go1.22.2`
3. Node.js：`v24.14.0`
4. 浏览器自动化：Playwright `1.58.2`（临时目录安装，Chrome `--no-sandbox`）

## 3. 关键复测证据

### 3.1 单测（含新增 3 项）
命令：
```bash
cd backend && GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./...
cd backend && GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./internal/adapter/codex -run 'TestContinueSessionRespectsContextCancel|TestContinueSessionIdempotencyKeyDeduplicates'
cd backend && GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./internal/server -run 'TestSessionsRouteReturns4290WhenRateLimited'
```
结果：全部通过。

### 3.2 ACT-11（幂等）接口复测
结果摘要：
1. 同一 `Idempotency-Key` 连续两次 `POST /continue`，返回同一 `job_id`。
2. 事件序列增量 `delta=3`（单次 continue 的 `delta/delta/done`）。

### 3.3 ACT-17（限流）接口复测
`RATE_LIMIT_SESSIONS_PER_SEC=5` 下，30 次突发请求结果：
1. `status_counts {200:5, 429:25}`
2. 429 响应体含 `code=4290`。

### 3.4 UI E2E 自动化复测
自动化输出摘要：
```text
PASS E2E-07 连接成功: health=ok, adapters=1
PASS E2E-01 sessions=1
NA   E2E-02 single-adapter MVP scope
PASS E2E-03 detail ok
PASS E2E-04 timeline 2->5
PASS E2E-05 WS 已连接
PASS E2E-06 http://127.0.0.1:18101|ws://127.0.0.1:18101
PASS E2E-08 reset default ok
PASS E2E-09 连接失败: unauthorized (4010)
PASS E2E-10 404+4003
```

## 4. E2E-01~10 最终状态

| 用例ID | 最终状态 | 说明 |
| --- | --- | --- |
| E2E-01 | PASS | 会话列表加载成功 |
| E2E-02 | N/A | 单适配器 MVP 范围裁定（见 6.1） |
| E2E-03 | PASS | 会话详情与时间线可展示 |
| E2E-04 | PASS | continue 后收到流式增量并完成 |
| E2E-05 | PASS | 断连重连后可恢复接收 |
| E2E-06 | PASS | 设置保存后刷新保持 |
| E2E-07 | PASS | 连接测试按钮成功状态明确 |
| E2E-08 | PASS | 恢复默认配置成功 |
| E2E-09 | PASS | token 错误时显示 `4010/unauthorized` |
| E2E-10 | PASS | 目标会话不存在返回 `404 + 4003` |

## 5. ACT-10/11/13/17 最终状态

| 用例ID | 最终状态 | 说明 |
| --- | --- | --- |
| ACT-10 | PASS | 新增单测 `TestContinueSessionRespectsContextCancel` 通过，确认取消后任务及时退出 |
| ACT-11 | PASS | 幂等键命中返回同 `job_id`，接口回归通过 |
| ACT-13 | N/A | MVP 内存适配器豁免（见 6.2 双签） |
| ACT-17 | PASS | 限流触发 `HTTP429 + code4290`，接口回归通过 |

## 6. N/A 裁定与双签

### 6.1 E2E-02 N/A（单适配器范围裁定）
1. PM 决策：`[2026-03-07 20:12][PM-AGENT][DECISION][DONE]` 已裁定单适配器 MVP 可将 `E2E-02` 标记 `N/A`。
2. QA 结论：本报告接受该裁定，判定为“范围限制而非质量失败”。
3. 文档落地：`docs/test-plan-mvp.md` 已补充 N/A 判定条款。

### 6.2 ACT-13 N/A（MVP 内存适配器豁免）
1. PM 决策：`[2026-03-07 20:12][PM-AGENT][DECISION][DONE]` 接受 `ACT-13` 按 MVP 规则申请 `N/A`。
2. QA 签署：本报告签署同意 `ACT-13=N/A`，有效期仅限当前 MVP 里程碑。
3. 文档依据：`docs/adapter-conformance-test.md` 第 `6.1` 节。

## 7. RC-06 / RC-07 结论
1. `RC-06`（MVP 核心 E2E 全通过）：满足。  
说明：`E2E-02` 为经 PM+QA 裁定的有效 `N/A`，其余核心 E2E 全部 `PASS`。
2. `RC-07`（适配器一致性测试通过）：满足。  
说明：`ACT-10/11/17` 均已回归 `PASS`；`ACT-13` 按 MVP 规则 `N/A` 且双签生效。

## 8. 最终 Go/No-Go
结论：`Go`（QA 质量门禁）

说明：
1. 本轮复测范围内 `S0/S1` 为 0。
2. 先前阻塞项已闭环（环境阻塞解除、缺陷项回归通过、N/A 决策已文档化）。
3. 发布最终执行仍需由 Release/Ops 按发布清单继续完成非 QA 项（如 RC-09 等）。
