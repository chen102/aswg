# Drawio 图目录

本目录存放项目图的 Drawio 源文件（`.drawio`）：

1. `architecture.drawio`：系统架构图
2. `flow-session-sync.drawio`：会话实时同步流程
3. `flow-continue-session.drawio`：网页续接会话流程
4. `flow-frontend-config.drawio`：前端配置后端地址流程

导出后的 SVG 位于：
1. `exported/architecture.svg`
2. `exported/flow-session-sync.svg`
3. `exported/flow-continue-session.svg`
4. `exported/flow-frontend-config.svg`

使用方式：
1. 浏览器打开 `https://app.diagrams.net/`，选择打开本地文件。
2. 或通过 Drawio MCP 编辑。
3. 编辑后运行 `cd scripts/drawio-export && npm install --silent && npm run export` 更新 `exported/`。
4. 建议再执行 `cd scripts/drawio-export && npm install --silent && npm run verify -- ../..`，检查文档内嵌覆盖和绝对路径泄漏。
