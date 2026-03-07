# 术语表

版本：`v1`  
日期：2026-03-07

## A
`Adapter`：对接某个 Agent CLI 的实现层，负责会话发现、事件读取和续接命令执行。  
`Adapter Registry`：适配器注册中心，负责加载、启用、查找适配器实例。

## C
`Continue`：在同一会话 ID 上继续提问并接收新增事件的动作。  
`Conformance Test`：适配器一致性测试，用于验证不同适配器对统一契约的兼容性。

## E
`Event`：会话中产生的最小消息单元，包含原始数据与标准化字段。

## P
`Payload`：适配器保留的原始事件数据，主要用于调试、追溯与回放。  
`Pagination Cursor`：分页游标，不透明字符串，客户端仅传递不解析。

## R
`RunJob`：一次 continue 请求在后端启动的执行任务，包含任务状态与时间信息。

## S
`Session`：某个 Agent CLI 中的一次对话上下文。  
`SessionSummary`：会话摘要信息，用于列表展示。  
`SessionDetail`：会话详情信息，不含全量事件流。  
`SessionEvent`：统一事件对象，至少包含 `adapter`、`session_id`、`seq`、`ts`、`type`、`payload`、`normalized`。

## T
`Transport`：前后端通信层，当前包含 REST 与 WebSocket。

## U
`Unified API`：对前端暴露的统一接口层，不受具体 Agent 差异影响。

## W
`WebSocket Stream`：用于会话事件实时推送的长连接通道。
