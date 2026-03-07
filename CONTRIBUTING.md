# Contributing Guide

感谢你对 `agent-session-web-gateway` 的贡献。

## 1. 贡献范围
欢迎以下类型贡献：
1. 后端统一接口与适配器实现。
2. 前端会话页面与设置页能力。
3. 文档、流程、测试与发布工具链。
4. 缺陷修复、性能优化与安全加固。

## 2. 开发约束
1. 新增 Agent 能力必须通过 Adapter 抽象接入，不得把特定 Agent 逻辑写入核心路由层。
2. 前端仅依赖统一 API 协议，不得直接耦合某个适配器的私有字段。
3. 不提交真实 token、个人路径、私有会话数据。
4. 变更 API 行为时，必须同步更新文档：
   - `docs/api.md`
   - `docs/api-contract-v1.md`
5. 变更架构或流程图时，必须同步更新 Drawio 与导出 SVG。

## 3. 分支与提交
建议分支命名：
1. `feat/<topic>`
2. `fix/<topic>`
3. `docs/<topic>`
4. `chore/<topic>`

建议提交信息采用 Conventional Commits：
1. `feat: ...`
2. `fix: ...`
3. `docs: ...`
4. `refactor: ...`
5. `test: ...`
6. `chore: ...`

## 4. Pull Request 流程
1. 先拉取最新主分支并解决冲突。
2. 自检通过后提交 PR。
3. 在 PR 描述中说明：
   - 变更目的
   - 主要改动点
   - 风险与回滚方式
   - 测试或验证结果
4. 至少一名维护者评审通过后合并。

## 5. 本地检查清单
提交前请确认：
1. 文档与代码变更一致，术语不冲突。
2. 不存在敏感信息泄漏（token、个人路径、私有域名/IP）。
3. Drawio 相关变更已执行导出：
   - `cd scripts/drawio-export && npm install --silent && npm run export`
4. Drawio 发布校验通过：
   - `cd scripts/drawio-export && npm install --silent && npm run verify -- ../..`
5. 新增适配器时已补充一致性测试用例或结果。

## 6. Issue 建议模板
建议在 issue 中包含：
1. 背景与目标。
2. 当前行为与期望行为。
3. 复现步骤（若为 bug）。
4. 风险评估与影响范围。
5. 可选实现思路。

## 7. 行为准则
请保持专业、尊重、可协作的沟通方式，聚焦问题与证据。
