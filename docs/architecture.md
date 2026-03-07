# 架构说明

## 图文件
本项目图统一使用 Drawio 源文件维护，并在本文直接展示导出图。

更新导出命令（跨平台）：
`cd scripts/drawio-export && npm install --silent && npm run export`

### 系统架构图
![系统架构图](./diagrams/exported/architecture.svg)

源文件：[architecture.drawio](./diagrams/architecture.drawio)

### 会话实时同步流程
![会话实时同步流程](./diagrams/exported/flow-session-sync.svg)

源文件：[flow-session-sync.drawio](./diagrams/flow-session-sync.drawio)

### 网页续接会话流程
![网页续接会话流程](./diagrams/exported/flow-continue-session.svg)

源文件：[flow-continue-session.drawio](./diagrams/flow-continue-session.drawio)

### 前端配置后端地址流程
![前端配置后端地址流程](./diagrams/exported/flow-frontend-config.svg)

源文件：[flow-frontend-config.drawio](./diagrams/flow-frontend-config.drawio)

## 1. 总体目标
项目提供统一 Web 网关，连接多个 Agent CLI 的会话能力，并向前端输出统一协议。

## 2. 分层架构
1. 展示层（Frontend）
- 会话列表、会话时间线、对话输入、系统设置。

2. 传输层（Transport）
- REST API（查询与命令触发）
- WebSocket（事件实时推送）

3. 业务层（Service）
- 会话查询服务
- 会话续接服务
- 事件标准化服务

4. 适配层（Adapters）
- Codex Adapter
- 其他 Agent Adapter（后续扩展）

5. 基础设施层（Infra）
- 配置管理
- 鉴权/限流
- 日志与指标

## 3. 适配器职责
每个适配器负责：
1. 会话发现（列表）。
2. 会话事件读取（历史 + 增量）。
3. 会话续接（调用对应 CLI）。
4. 事件映射到统一模型。

## 4. 事件标准化
统一事件对象最小字段建议：
- `adapter`
- `session_id`
- `ts`
- `type`
- `payload`
- `normalized`

说明：
- `payload` 保留原始数据，便于调试与回放。
- `normalized` 仅包含前端通用消费字段。

## 5. 扩展约束
1. 新增适配器不得修改前端页面协议。
2. 新增适配器不得修改核心路由结构。
3. 新增适配器必须通过适配器一致性测试。

## 6. 安全约束
1. 默认本地监听。
2. 所有写操作接口要求鉴权。
3. 日志输出不得包含密钥、令牌、个人路径。

## 7. 架构演进路线
1. 先实现单节点部署。
2. 再支持适配器热加载（可选）。
3. 后续支持多实例事件汇聚（可选）。
