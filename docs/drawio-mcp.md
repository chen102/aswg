# Drawio MCP 部署说明

## 1. 选型结论
项目采用 `drawio-mcp-server` 作为 Drawio MCP 实现。

选型依据：
1. 支持 MCP 标准传输。
2. 支持 `--editor`，便于通过浏览器编辑 Drawio 图。
3. 与 Codex 的 MCP 配置方式兼容。

参考：
1. GitHub：`https://github.com/lgazo/drawio-mcp-server`
2. npm：`https://www.npmjs.com/package/drawio-mcp-server`

## 2. 推荐配置
建议使用如下 MCP 服务配置：
1. server name：`drawio`
2. command：`npx -y drawio-mcp-server`
3. transport：`stdio`
4. `startup_timeout_sec`：建议 `60`

> 说明：`drawio-mcp-server@1.8.0` 在 `--editor` + `stdio` 下会在握手前向 `stdout` 打初始化日志，可能导致部分 MCP 客户端（包括 Codex）握手失败。Codex 下优先使用不带 `--editor` 的 `stdio` 配置。

### 2.1 Codex + WSL 稳定配置（推荐）
如果在 Codex 中反复出现 `initialize response` 握手失败，建议使用以下 `~/.codex/config.toml` 配置：

```toml
[mcp_servers.drawio]
command = "bash"
args = ["-lc", "export PATH=$HOME/.nvm/versions/node/v24.14.0/bin:$PATH; EXT_PORT=$((20000 + RANDOM % 20000)); HTTP_PORT=$((40000 + RANDOM % 20000)); exec drawio-mcp-server --extension-port \"$EXT_PORT\" --http-port \"$HTTP_PORT\""]
startup_timeout_sec = 60
```

并确保已安装全局命令：
```bash
npm install -g drawio-mcp-server
```

`PATH` 中的 Node 版本目录请按本机实际版本调整（例如 `v24.14.0`）。

原因说明：
1. WSL 下常见 `node` 路径不可见问题（`/usr/bin/env: node: No such file or directory`）。
2. `drawio-mcp-server` 会额外启动 HTTP 端口（默认 `3000`），旧进程残留时会端口冲突。
3. 随机化 `extension-port` 和 `http-port` 可避免残留进程导致的新实例启动失败。

## 3. 注册命令
```bash
codex mcp add drawio -- npx -y drawio-mcp-server
```

## 4. 验证命令
```bash
codex mcp get drawio
codex mcp list --json
npx -y drawio-mcp-server --help
```

## 5. 可选 HTTP 模式
如需以 HTTP 方式运行（便于独立调试）：
```bash
npx -y drawio-mcp-server --editor --transport http --http-port 3000
```

在 Codex 中接入该 HTTP 端点：
```bash
codex mcp add drawio-http --url http://127.0.0.1:3000/mcp
```

## 6. 项目图文件约定
项目图以 Drawio 源文件为准，目录：`docs/diagrams/`。

1. `architecture.drawio`
2. `flow-session-sync.drawio`
3. `flow-continue-session.drawio`
4. `flow-frontend-config.drawio`

导出图目录：`docs/diagrams/exported/`（文档内直接嵌入 SVG）。

批量导出命令：
```bash
cd scripts/drawio-export
npm install --silent
npm run export
```

## 7. 编辑方式
1. 使用 draw.io 打开 `docs/diagrams/*.drawio`。
2. 或通过已注册的 Drawio MCP 进行编辑。
3. 每次编辑后执行导出命令，确保文档内嵌图与源文件一致。

## 8. Skill 建议（美化 + 导出 + 内嵌）
建议在 Codex skills 目录中维护 `drawio-doc-embed` skill。

建议路径：`$CODEX_HOME/skills/drawio-doc-embed`

用途：
1. Drawio 可读性优化检查（连线、间距、覆盖问题）。
2. Drawio 批量导出 SVG。
3. Markdown 内嵌图与源文件链接规范化。
4. 校验文档是否存在本机绝对路径泄漏。
5. 统一“画图 + 文档编写 + 发布门禁”流程。

## 9. 流程沉淀文档
项目内已沉淀 SOP：
`docs/workflows/drawio-doc-workflow.md`
