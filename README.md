# agent-session-web-gateway

开源的多 Agent CLI 会话 Web 网关。

## 项目简介
本项目提供一个统一 Web 界面，用于连接并共享不同 Agent CLI 的会话：
1. 查看会话列表与时间线。
2. 在同一会话中继续提问。
3. 实时接收流式事件。

当前已提供 `codex` 与 `picoclaw` 两个适配器实现，架构上支持继续扩展到其他 Agent CLI。

## 设计原则
1. 多 Agent 可扩展：通过 Adapter 接口接入。
2. 前后端解耦：前端仅依赖统一 API 协议。
3. 运行时可配置：前端可配置后端地址与端口。
4. 开源规范：文档与示例不包含个人环境信息。

## 文档索引
1. `docs/project-preparation-report.md`：项目前期准备报告。
2. `docs/architecture.md`：架构说明。
3. `docs/configuration.md`：配置说明。
4. `docs/deployment.md`：部署文档（开箱即用）。
5. `docs/api.md`：API 规范（草案）。
6. `docs/api-contract-v1.md`：API 字段级契约（v1）。
7. `docs/adapter-conformance-test.md`：适配器一致性测试规范。
8. `docs/test-plan-mvp.md`：MVP 测试计划。
9. `docs/ops-runbook.md`：运维 Runbook（MVP）。
10. `docs/release-checklist.md`：发布清单。
11. `docs/glossary.md`：术语表。
12. `docs/drawio-mcp.md`：Drawio MCP 部署说明。
13. `docs/diagrams/README.md`：Drawio 图文件清单。
14. `docs/workflows/drawio-doc-workflow.md`：Drawio 画图与文档 SOP。
15. `docs/resume-smoke.jsonl`：脱敏的事件流示例。

## 开源治理文件
1. `LICENSE`
2. `CONTRIBUTING.md`
3. `SECURITY.md`
4. `CHANGELOG.md`

## 配置要求（前端）
前端必须支持运行时配置以下参数：
1. `api_base_url`，示例：`http://127.0.0.1:8082`
2. `ws_base_url`，示例：`ws://127.0.0.1:8082`
3. `default_adapter`，示例：`codex`

建议通过 `frontend/src/runtime-config.json` + `localStorage` 覆盖的方式实现，避免每次变更都重新构建前端。

## 快速开始（开箱即用）

### 1) 前置依赖
1. `Go >= 1.22`
2. `curl`
3. `codex` CLI（仅 `real` 模式必需）

建议先确认：
```bash
go version
curl --version
codex --version
```

### 2) 一键启动（推荐）
在仓库根目录执行：
```bash
./scripts/aswg up
```

默认监听 `127.0.0.1:8082`，启动后访问：`http://127.0.0.1:8082`

### 3) 常用命令
```bash
./scripts/aswg status     # 查看进程 / 端口 / 健康状态
./scripts/aswg logs       # 查看最近日志
./scripts/aswg logs -f    # 持续跟踪日志
./scripts/aswg restart    # 重启
./scripts/aswg down       # 停止
./scripts/aswg doctor     # 依赖与环境体检
```

### 4) 配置方式
推荐使用本地配置文件：
```bash
cp .env.example .env.local
```

修改 `.env.local` 后执行：
```bash
./scripts/aswg restart
```

也可单次参数覆盖：
```bash
./scripts/aswg up --port 18080
./scripts/aswg restart --host 0.0.0.0 --port 8082
./scripts/aswg restart --auth-token 'replace-with-your-token'
```

### 5) API 快速自检
无鉴权模式：
```bash
curl -sS http://127.0.0.1:8082/api/v1/health
curl -sS http://127.0.0.1:8082/api/v1/adapters
curl -sS 'http://127.0.0.1:8082/api/v1/adapters/codex/sessions?limit=20'
```

### 6) 完整部署与联调说明
1. 部署参数、局域网/公网、systemd、故障排查：`docs/deployment.md`
2. 运维值守与告警建议：`docs/ops-runbook.md`
3. PicoClaw 本地联调：`docs/dev-smoke-picoclaw.md`
4. 若需手工启动，仍可使用原始方式：`cd backend && go run ./cmd/server`

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
当前仓库已完成 MVP 主链路实现，并具备 `codex` 与 `picoclaw` 双适配器能力：
1. Go 后端统一 API（health/adapters/sessions/detail/events/continue）。
2. Adapter Registry 与 Codex/PicoClaw Adapter 参考实现。
3. WebSocket 流式事件推送（event/heartbeat/done）。
4. 前端设置页与连接测试能力。
5. 前端最小闭环：列表 -> 详情 -> continue -> 流式更新。
6. Codex continue 固定走 `real` 模式（`codex exec --json --dangerously-bypass-approvals-and-sandbox`）。

## 下一步
1. 补齐 Go 单元测试与适配器一致性测试自动化。
2. 增加真实流式路径的观测与告警（超时、退出码、重试策略）。
3. 执行 QA 计划并输出发布 Go/No-Go 结论。
