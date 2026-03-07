# Drawio 图文流程 SOP

本流程用于统一“画图、导出、内嵌、写文档、发布校验”步骤。

## 1. 产物约定
1. 图源文件：`docs/diagrams/*.drawio`
2. 导出文件：`docs/diagrams/exported/*.svg`
3. 文档内嵌：`docs/**/*.md` 中使用 Markdown 图片语法嵌入 `.svg`
4. 源文件追溯：每张图下保留对应 `.drawio` 链接

## 2. 画图规范（关键）
1. 主方向统一（左到右或上到下）。
2. 连线优先直角路由。
3. 连线不得穿过模块文本区。
4. 节点间距保证标题完整可读。
5. 复杂图拆分成多张聚焦图。

## 3. 导出命令
```bash
cd scripts/drawio-export
npm install --silent
npm run export
```

## 4. 文档内嵌模板
```markdown
![系统架构图](./diagrams/exported/architecture.svg)

源文件：
[architecture.drawio](./diagrams/architecture.drawio)
```

## 5. 文档编写要求
每张图建议补充以下说明：
1. 目的：图要表达什么。
2. 边界：图内/图外包含什么。
3. 主路径：正常业务流。
4. 异常路径：失败、重试、回退。
5. 扩展点：新增 Agent 或模块如何接入。

## 6. 发布前校验
```bash
cd scripts/drawio-export
npm install --silent
npm run verify -- ../..
```

校验目标：
1. 每个 `.drawio` 都有 `.svg`。
2. 每个图都有文档内嵌。
3. 无本机绝对路径泄漏。

## 7. 对应 skill
已沉淀技能：`drawio-doc-embed`

建议路径：
`$CODEX_HOME/skills/drawio-doc-embed`
