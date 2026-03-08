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

## 快速开始（10 分钟）

### 1) 前置依赖
1. `Go >= 1.22`
2. `codex` CLI（`real` 模式必需）
3. 可选：`Node.js >= 20`（仅用于前端静态检查或脚本）

建议先确认：
```bash
go version
codex --version
```

若你要使用真实流式对话（默认 `CODEX_STREAM_MODE=real`），请先确保 `codex` CLI 可正常使用。

### 2) 一键启动（默认无鉴权）
在仓库根目录执行：
```bash
cd backend
SERVER_HOST=127.0.0.1 \
SERVER_PORT=8080 \
AUTH_TOKEN= \
ENABLED_ADAPTERS=codex \
DEFAULT_ADAPTER=codex \
CODEX_STREAM_MODE=real \
CODEX_CLI_BIN=codex \
CODEX_CLI_ARGS='exec --json --dangerously-bypass-approvals-and-sandbox' \
FRONTEND_DIR=../frontend/src \
go run ./cmd/server
```

启动后访问：
`http://127.0.0.1:8080`

如需同时启用 PicoClaw 适配器（连接外部 Pico channel）：
```bash
cd backend
SERVER_HOST=127.0.0.1 \
SERVER_PORT=8080 \
AUTH_TOKEN= \
ENABLED_ADAPTERS=codex,picoclaw \
DEFAULT_ADAPTER=codex \
CODEX_STREAM_MODE=real \
CODEX_CLI_BIN=codex \
CODEX_CLI_ARGS='exec --json --dangerously-bypass-approvals-and-sandbox' \
PICOCLAW_WS_BASE_URL=ws://127.0.0.1:8081 \
PICOCLAW_TOKEN=replace-with-your-pico-token \
FRONTEND_DIR=../frontend/src \
go run ./cmd/server
```

### 3) 最小联调链路
1. 设置页点击“连接测试”。
2. 会话列表选择 `codex` 会话。
3. 输入 prompt 后执行 `continue`。
4. 观察消息气泡与 WebSocket 实时流式更新。

### 4) API 快速自检
无鉴权模式：
```bash
curl -sS http://127.0.0.1:8080/api/v1/health
curl -sS http://127.0.0.1:8080/api/v1/adapters
curl -sS 'http://127.0.0.1:8080/api/v1/adapters/codex/sessions?limit=20'
```

### 5) 开启鉴权（生产/公网建议）
启动时设置：
```bash
AUTH_TOKEN='replace-with-your-token'
```

调用 REST：
```bash
curl -sS -H 'Authorization: Bearer replace-with-your-token' \
  http://127.0.0.1:8080/api/v1/adapters
```

浏览器 WebSocket（Query 方式）：
```text
ws://127.0.0.1:8080/ws/v1/adapters/codex/sessions/<session_id>?access_token=replace-with-your-token
```

### 6) 局域网/公网访问
1. 局域网访问请将服务监听到所有网卡：
```bash
SERVER_HOST=0.0.0.0
```
2. 前端设置页中：
   - `api_base_url` 填 `http://<可访问IP>:<端口>`
   - `ws_base_url` 填 `ws://<可访问IP>:<端口>`
3. 反向代理或 FRP 场景（例如公网 `8081` 映射到本地 `8080`）：
   - `api_base_url=http://<公网IP或域名>:8081`
   - `ws_base_url=ws://<公网IP或域名>:8081`
4. 若公网是 HTTPS，请对应使用 `https://` 与 `wss://`，避免浏览器 mixed-content 拦截。

### 7) 常见问题
1. `Failed to fetch`
   - 检查服务是否已启动、`api_base_url` 是否可达、`AUTH_TOKEN` 是否匹配。
2. WebSocket 连接失败或 `4010 unauthorized`
   - 检查 `ws_base_url`、`access_token` 或 `Authorization` Header 是否正确。
3. `codex: command not found` 或 real 模式没有 AI 回复
   - 安装/修复 `codex` CLI，或临时切换 `CODEX_STREAM_MODE=mock` 做联调。
4. `git push` 报 `Permission denied (publickey)`
   - 检查 SSH key 是否已加入 GitHub，或改用 HTTPS + PAT。

### 8) MVP 常用环境变量
1. `SERVER_HOST`（默认 `127.0.0.1`）
2. `SERVER_PORT`（默认 `8080`）
3. `AUTH_TOKEN`（为空时不强制鉴权）
4. `ENABLED_ADAPTERS`（默认 `codex`）
5. `DEFAULT_ADAPTER`（默认 `codex`）
6. `CODEX_SEED_FILE`（默认 `docs/resume-smoke.jsonl`）
7. `FRONTEND_DIR`（默认 `frontend/src`）
8. `RATE_LIMIT_SESSIONS_PER_SEC`（默认 `30`，`0` 表示关闭）
9. `CODEX_STREAM_MODE`（`real|mock`，默认 `real`）
10. `CODEX_CLI_BIN`（默认 `codex`）
11. `CODEX_CLI_ARGS`（默认 `exec --json --dangerously-bypass-approvals-and-sandbox`）
12. `CODEX_CLI_TIMEOUT_MS`（默认 `300000`）
13. `CODEX_MOCK_FALLBACK`（默认 `false`）
14. `CODEX_HISTORY_ENABLED`（默认 `true`）
15. `CODEX_HISTORY_DIR`（默认 `~/.codex/sessions`）
16. `CODEX_HISTORY_SCAN_TTL_MS`（默认 `5000`）
17. `PICOCLAW_WS_BASE_URL`（默认 `ws://127.0.0.1:8080`）
18. `PICOCLAW_TOKEN`（默认空，启用 `picoclaw` 适配器时必填）
19. `PICOCLAW_ALLOW_TOKEN_QUERY`（默认 `false`）
20. `PICOCLAW_DIAL_TIMEOUT_MS`（默认 `5000`）
21. `PICOCLAW_CONTINUE_TIMEOUT_MS`（默认 `120000`）
22. `PICOCLAW_READ_IDLE_TIMEOUT_MS`（默认 `45000`）

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
6. Codex continue 默认走 `real` 模式（`codex exec --json --dangerously-bypass-approvals-and-sandbox`），本地可按需启用 mock fallback。

## 下一步
1. 补齐 Go 单元测试与适配器一致性测试自动化。
2. 增加真实流式路径的观测与告警（超时、退出码、重试策略）。
3. 执行 QA 计划并输出发布 Go/No-Go 结论。
