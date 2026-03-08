# DEV 对话体验交付报告（TASK-P1-01）

日期：2026-03-07  
角色：DEV-FULLSTACK  
任务：`TASK-P1-01`  
目标：实现“聊天气泡 + 实时增量 + 刷新可重建”的会话体验，禁止仅展示原始事件日志

## 1. 变更摘要

1. 前端会话详情区改为聊天气泡视图：按 `user` / `assistant` 角色渲染，替代原事件日志展示。
2. `continue` 提交后先本地注入一条用户气泡（`pending`），再等待服务端事件对齐 `seq` 并转为正式消息。
3. assistant 侧采用增量拼接：`message.delta` 连续追加文本，`message.done` 收尾。
4. 页面刷新或重进会话时，从 `GET /events` 历史事件重建对话顺序，不依赖前端内存状态。
5. 后端 continue 事件语义补齐：在 assistant 增量前写入 `message.user`（`normalized.role=user`）事件。

## 2. 变更文件

1. `frontend/src/index.html`
2. `frontend/src/styles.css`
3. `frontend/src/app.js`
4. `backend/internal/adapter/codex/adapter.go`
5. `backend/internal/adapter/codex/adapter_test.go`
6. `docs/api.md`
7. `docs/api-contract-v1.md`

## 3. 关键实现点

### 3.1 聊天气泡视图替换

1. 页面新增 `#chat-thread` 区域与 `.chat-bubble.user/.assistant` 样式。
2. 不再将“原始事件 JSON 日志”作为主视图，消息由 `state.messages` 统一驱动渲染。

### 3.2 Continue 实时体验

1. 点击 Continue：
   - 立即渲染本地用户气泡（`pending=true`）。
   - 调用 `POST /continue`。
2. 收到 WS `event` 帧：
   - 若是 `role=user`，与本地 pending 用户消息对齐并落 `seq`。
   - 若是 `role=assistant` 且 `message.delta`，持续追加文本。
   - 若是 `message.done`，assistant 当前草稿气泡标记完成。

### 3.3 刷新重建

1. `loadSession()` 会先取 `SessionDetail`，再取 `events?limit=500`。
2. `buildMessagesFromEvents()` 顺序回放事件，恢复完整消息序列与 assistant 草稿状态。

## 4. 运行与验证步骤（可复现）

### 4.1 静态检查与单测

```bash
cd backend && GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./...
node --check frontend/src/app.js
```

结果摘要：均通过。

### 4.2 P1 专项 smoke（日志证据）

执行脚本后输出并归档至：

1. `docs/evidence/p1-evidence.log`
2. `docs/evidence/p1-error-evidence.log`

覆盖点：

1. 创建会话 `201`。
2. `continue` 后事件顺序（`user -> assistant.delta... -> assistant.done`）。
3. 非法参数 `4001`。
4. 鉴权失败 `4010`、会话不存在 `4003`。

## 5. 证据摘录

### 5.1 Continue 事件顺序（来自 `docs/evidence/p1-evidence.log`）

```json
[
  {
    "seq": 3,
    "type": "message.user",
    "role": "user",
    "text": "please continue in stream",
    "done": true
  },
  {
    "seq": 4,
    "type": "message.delta",
    "role": "assistant",
    "text": "收到继续请求，开始执行: ",
    "done": false
  },
  {
    "seq": 5,
    "type": "message.delta",
    "role": "assistant",
    "text": "please continue in stream",
    "done": false
  },
  {
    "seq": 6,
    "type": "message.done",
    "role": "assistant",
    "text": "",
    "done": true
  }
]
```

### 5.2 异常语义（来自 `docs/evidence/p1-error-evidence.log`）

1. 未鉴权访问 `GET /api/v1/adapters`：HTTP `401` + 业务码 `4010`。
2. 不存在会话执行 continue：HTTP `404` + 业务码 `4003`。

前端 `fetchAPI()` 会将错误文案统一展示在状态栏（`stream-status`），保持聊天视图不回退为原始日志。

## 6. 已知限制

1. 当前气泡仅展示纯文本，不做 Markdown 渲染。
2. assistant 侧按“单草稿”策略增量拼接，若未来支持并行多路 assistant 输出，需引入消息级关联 ID。
3. 本报告以日志证据为主，截图可在 QA `TASK-P1-02` 阶段补充归档。

## 7. 当前状态

`REVIEW`

等待 QA-AGENT 执行 `TASK-P1-02` 专项验收。
