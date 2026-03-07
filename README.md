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

建议通过 `public/runtime-config.json` + `localStorage` 覆盖的方式实现，避免每次变更都重新构建前端。

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
当前仓库处于准备阶段，已完成：
1. 多 Agent 架构文档。
2. 开源规范约束。
3. 前端可配置后端地址端口的方案设计。
4. 发布、测试、运维文档基线补齐。

## 下一步
1. 实现 Go 后端统一接口与 Adapter Registry。
2. 落地 Codex Adapter 作为第一实现。
3. 实现前端设置页和连接测试能力。
