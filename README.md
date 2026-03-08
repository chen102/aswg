# agent-session-web-gateway

开源的多 Agent CLI 会话 Web 网关。

## 项目简介
本项目提供一个统一 Web 界面，用于连接并共享不同 Agent CLI 的会话：
1. 查看会话列表与时间线。
2. 在同一会话中继续提问。
3. 实时接收流式事件。

首个参考适配器为 Codex，架构上支持后续扩展到其他 Agent CLI。

## 设计原则
1. 多 Agent 可扩展：通过 Adapter 接口接入。
2. 前后端解耦：前端仅依赖统一 API 协议。
3. 运行时可配置：前端可配置后端地址与端口。
4. 开源规范：文档与示例不包含个人环境信息。

## 文档索引
1. `docs/project-preparation-report.md`：项目前期准备报告。
2. `docs/architecture.md`：架构说明。
3. `docs/configuration.md`：配置说明。
4. `docs/api.md`：API 规范（草案）。
5. `docs/api-contract-v1.md`：API 字段级契约（v1）。
6. `docs/adapter-conformance-test.md`：适配器一致性测试规范。
7. `docs/test-plan-mvp.md`：MVP 测试计划。
8. `docs/ops-runbook.md`：运维 Runbook（MVP）。
9. `docs/release-checklist.md`：发布清单。
10. `docs/glossary.md`：术语表。
11. `docs/drawio-mcp.md`：Drawio MCP 部署说明。
12. `docs/diagrams/README.md`：Drawio 图文件清单。
13. `docs/workflows/drawio-doc-workflow.md`：Drawio 画图与文档 SOP。
14. `docs/resume-smoke.jsonl`：脱敏的事件流示例。

## 开源治理文件
1. `LICENSE`
2. `CONTRIBUTING.md`
3. `SECURITY.md`
4. `CHANGELOG.md`

## 配置要求（前端）
前端必须支持运行时配置以下参数：
1. `api_base_url`，示例：`http://127.0.0.1:8080`
2. `ws_base_url`，示例：`ws://127.0.0.1:8080`
3. `default_adapter`，示例：`codex`

建议通过 `frontend/src/runtime-config.json` + `localStorage` 覆盖的方式实现，避免每次变更都重新构建前端。

## 快速启动（MVP）
1. 启动服务：
```bash
cd backend
go run ./cmd/server
```
2. 打开浏览器访问：
`http://127.0.0.1:8080`
3. 最小联调链路：
- 设置页点击“连接测试”
- 会话列表选择 `codex` 会话
- 输入 prompt 执行 `continue`
- 观察时间线和 WebSocket 实时更新

可选环境变量：
1. `SERVER_HOST`（默认 `127.0.0.1`）
2. `SERVER_PORT`（默认 `8080`）
3. `AUTH_TOKEN`（为空时不强制鉴权）
4. `ENABLED_ADAPTERS`（默认 `codex`）
5. `DEFAULT_ADAPTER`（默认 `codex`）
6. `CODEX_SEED_FILE`（默认 `docs/resume-smoke.jsonl`）
7. `FRONTEND_DIR`（默认 `frontend/src`）
8. `CODEX_STREAM_MODE`（`real|mock`，默认 `real`）
9. `CODEX_CLI_BIN`（默认 `codex`）
10. `CODEX_CLI_ARGS`（默认 `exec --json --dangerously-bypass-approvals-and-sandbox`）
11. `CODEX_CLI_TIMEOUT_MS`（默认 `90000`）
12. `CODEX_MOCK_FALLBACK`（默认 `false`）

## 图文档约定（Drawio）
1. `docs/diagrams/*.drawio` 保存源文件。
2. `docs/diagrams/exported/*.svg` 保存导出图，用于文档内联显示。
3. 每次修改 Drawio 后执行（跨平台）：
`cd scripts/drawio-export && npm install --silent && npm run export`
Windows 也可使用：
`powershell -ExecutionPolicy Bypass -File .\scripts\export-drawio.ps1`
4. 发布前建议执行导出覆盖检查：
`cd scripts/drawio-export && npm install --silent && npm run verify -- ../..`
5. 推荐按 `docs/workflows/drawio-doc-workflow.md` 执行完整图文发布流程。

## 状态
当前仓库已完成 MVP 主链路实现，并具备真实 Codex CLI 流式桥接：
1. Go 后端统一 API（health/adapters/sessions/detail/events/continue）。
2. Adapter Registry 与 Codex Adapter 参考实现。
3. WebSocket 流式事件推送（event/heartbeat/done）。
4. 前端设置页与连接测试能力。
5. 前端最小闭环：列表 -> 详情 -> continue -> 流式更新。
6. Codex continue 默认走 `real` 模式（`codex exec --json`），本地可按需启用 mock fallback。

## 下一步
1. 补齐 Go 单元测试与适配器一致性测试自动化。
2. 增加真实流式路径的观测与告警（超时、退出码、重试策略）。
3. 执行 QA 计划并输出发布 Go/No-Go 结论。
