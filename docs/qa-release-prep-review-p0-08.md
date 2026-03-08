# QA 发布准备复核（TASK-P0-08）

日期：2026-03-07  
角色：QA-AGENT  
状态：`DONE`

## 1. 复核范围
1. `RC-08`：配置样例与实际部署参数一致。
2. `RC-09`：回滚演练完成并可复现。

## 2. 输入依据
1. `docs/configuration.md`
2. `docs/ops-runbook.md`
3. `backend/internal/server/config.go`
4. `frontend/src/runtime-config.json`
5. `docs/dev-release-prep-p0-08.md`（DEV 证据草案）
6. QA 独立执行的回滚演练命令与输出

## 3. RC-08 复核结果
结论：`通过（QA 侧）`

核对点：
1. 后端默认参数与文档一致：
   - `SERVER_HOST=127.0.0.1`
   - `SERVER_PORT=8080`
   - `DEFAULT_ADAPTER=codex`
   - `ENABLED_ADAPTERS` 默认 `codex`
   - `CODEX_SEED_FILE=docs/resume-smoke.jsonl`
   - `FRONTEND_DIR=frontend/src`
   - `RATE_LIMIT_SESSIONS_PER_SEC=30`
2. 前端运行时配置样例一致：
   - `api_base_url=http://127.0.0.1:8080`
   - `ws_base_url=ws://127.0.0.1:8080`
   - `default_adapter=codex`
   - `request_timeout_ms=30000`

命令证据（摘要）：
```bash
rg -n "...defaults..." backend/internal/server/config.go
cat frontend/src/runtime-config.json
rg -n "...配置项..." docs/configuration.md
```

## 4. RC-09 复核结果
结论：`通过（QA 侧）`

### 4.1 演练步骤
1. 启动稳定配置实例（端口 `18130`，`ENABLED_ADAPTERS=codex`）。
2. 验证 `health/adapters` 正常。
3. 停止稳定实例。
4. 注入错误配置（`ENABLED_ADAPTERS=none`）并启动，验证启动失败（`no adapters enabled`）。
5. 回退到稳定配置重新启动。
6. 验证回退后 `health/adapters/sessions/continue` 全部恢复可用。

### 4.2 结果摘录
1. 故障注入期：进程退出码 `1`，日志包含 `no adapters enabled`。
2. 回退后：`health` 返回 `code=0`，`adapters` 返回 `codex`，`continue` 返回 `202` + `job_id`。

命令证据（摘要）：
```bash
# good config start -> verify
# bad config start (expect fail)
# rollback to good config -> verify health/adapters/sessions/continue
```

## 5. 风险结论
总体风险：`中低`

说明：
1. `RC-08/RC-09` 从 QA 独立复核角度可判定通过。
2. DEV 已提交 `docs/dev-release-prep-p0-08.md`，其证据与 QA 独立复核结果一致。
3. RC 条目最终勾选仍需 Ops Owner 在发布流程中完成签署确认。
3. 发布阶段仍受 `RC-10~RC-13` 约束，当前不在本次复核范围内。
