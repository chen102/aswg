# 适配器一致性测试规范

版本：`v1`  
日期：2026-03-07

## 1. 目标
1. 保证新增适配器不会破坏统一 API 契约。
2. 保证前端不需要因新增适配器改动核心页面逻辑。
3. 为适配器合并提供明确、可重复的准入门槛。

## 2. 适用范围
1. `Codex Adapter`（首个参考实现）。
2. 后续任意 Agent CLI Adapter。

## 3. 最小接口约束（建议）
```go
type AgentAdapter interface {
    Name() string
    DiscoverSessions(ctx context.Context, req DiscoverRequest) ([]SessionSummary, error)
    GetSession(ctx context.Context, req SessionRequest) (SessionDetail, error)
    GetSessionEvents(ctx context.Context, req EventsRequest) (<-chan SessionEvent, error)
    ContinueSession(ctx context.Context, req ContinueRequest) (<-chan SessionEvent, error)
}
```

说明：
1. `GetSession` 与 REST `GET /sessions/{id}` 对齐。
2. 所有输出事件必须满足统一 `SessionEvent` 模型。

## 4. 测试分层
1. 契约测试：接口字段、分页、错误码、顺序语义。
2. 行为测试：会话发现、历史读取、续接流式输出。
3. 稳定性测试：并发、取消、超时、异常恢复。
4. 安全测试：日志脱敏、路径约束、鉴权失败行为。

## 5. 必测用例

| 用例ID | 场景 | 预期结果 |
| --- | --- | --- |
| ACT-01 | `Name()` 调用 | 返回稳定、唯一且可配置的适配器名 |
| ACT-02 | Discover 默认分页 | 返回合法列表，`updated_at` 可排序 |
| ACT-03 | Discover 过滤 | `query/workspace` 过滤行为正确 |
| ACT-04 | Discover 边界 | `limit` 越界返回参数错误 |
| ACT-05 | GetSession 命中 | 返回 `SessionDetail` 且字段完整 |
| ACT-06 | GetSession 未命中 | 返回 `4003` 语义错误 |
| ACT-07 | Events 历史分页 | 支持 `cursor`，`seq` 单调递增 |
| ACT-08 | Events 续传 | 断点续传不乱序，允许重复可去重 |
| ACT-09 | Continue 成功 | 能产出 `message.delta` 到 `done` 流程 |
| ACT-10 | Continue 取消 | 上下文取消后子任务及时退出 |
| ACT-11 | Continue 幂等键 | 重复请求不会重复启动任务（如实现） |
| ACT-12 | 并发读取 | 多客户端同会话订阅无崩溃 |
| ACT-13 | 子进程异常 | 失败可被捕获并映射为标准错误 |
| ACT-14 | 数据损坏 | 非法事件不导致网关进程崩溃 |
| ACT-15 | 敏感信息 | 日志中无 token、私有路径、密钥 |
| ACT-16 | 鉴权失败 | 返回 `4010` 语义错误 |
| ACT-17 | 限流行为 | 超频请求返回 `4290` |
| ACT-18 | 性能基线 | 中规模会话列表 p95 满足目标阈值 |

## 6. 通过标准
1. `ACT-01` 到 `ACT-17` 全部通过。
2. `ACT-18` 不通过时，需有已批准的性能豁免记录。
3. 失败用例必须绑定 issue，并附复现步骤与日志证据。

## 7. 产物要求
每次一致性测试至少输出：
1. 执行环境信息（OS、Go 版本、适配器版本）。
2. 用例结果汇总（通过/失败/阻塞）。
3. 失败详情（request_id、错误码、最小复现步骤）。
4. 风险评估与是否允许合并的结论。

## 8. CI 门禁建议
1. PR 触发适配器一致性测试任务。
2. Gate 规则：
   - 关键用例失败：禁止合并。
   - 非关键性能豁免：需维护者明确批准。
3. 合并前需保留最新测试报告链接。

## 9. 角色与职责
1. Adapter Owner：实现与修复适配器逻辑。
2. Reviewer：审查契约兼容性与风险。
3. Release Owner：确认门禁通过并签署发布。
