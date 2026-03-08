# QA 专项验收报告：TASK-P1-02（聊天气泡实时会话）

日期：2026-03-07  
执行人：QA-AGENT  
结论：**Go**

## 1. 验收范围

依据 `A2A.txt` 中 PM 对 `TASK-P1-02` 的要求，验收以下 4 项：

1. 用户/助手角色显示正确。
2. assistant 文本流式增长可见。
3. 刷新后历史对话可重建。
4. 异常场景（4010/4003）提示不回退为原始日志。

## 2. 环境与方法

1. 服务实例 A（体验主流程）：`http://127.0.0.1:18083`，`AUTH_TOKEN=`（空）。
2. 服务实例 B（4010 异常）：`http://127.0.0.1:18080`，`AUTH_TOKEN=qa-token`。
3. 浏览器自动化：Node + Playwright（`channel=chrome`，`--no-sandbox`）执行真实页面交互。
4. 代码核对文件：
   - `frontend/src/index.html`
   - `frontend/src/app.js`
   - `frontend/src/styles.css`

## 3. 验收结果

| 验收项 | 结果 | 证据 |
| --- | --- | --- |
| 1) 用户/助手角色显示正确 | PASS | 结构层：`#chat-thread` + `.chat-bubble.user/.assistant` 已落地（`frontend/src/index.html:75`, `frontend/src/styles.css`）。逻辑层：`renderChatThread()` 按 `role` 渲染气泡（`frontend/src/app.js:490-510`）。实测 DOM `roles=["user","assistant","user","assistant"]`。 |
| 2) assistant 文本流式增长可见 | PASS | 实测采样：`lastAssistantLen` 从 `13` 增长到 `40`（同一轮 continue，约 500ms 内增长），`streamStatus` 从 `continue 已提交` 变为 `收到 done 帧`。增量拼接逻辑见 `applyEventToMessages()`（`frontend/src/app.js:447-458`）。 |
| 3) 刷新后历史对话可重建 | PASS | 页面刷新后仍可重建出历史消息：`userCount=2, assistantCount=2`。重建逻辑见 `buildMessagesFromEvents()` + `applyEventToMessages()`（`frontend/src/app.js:412-488`）。 |
| 4) 4010/4003 异常提示且不回退原始日志 | PASS | 4010（同源 18080）页面提示：`加载 adapters 失败: unauthorized (4010)`、`创建失败: unauthorized (4010)`。4003（路由覆盖模拟会话失效）页面提示：`continue 失败: session not found (4003)`。同时 `hasRawTimeline=false`（页面不存在旧 `#event-timeline`，仅气泡线程）。错误展示路径见 `setStreamStatus` 与 `fetchAPI` 错误格式化（`frontend/src/app.js:588-614`, `frontend/src/app.js:631-639`）。 |

## 4. 关键执行摘录

### 4.1 流式增长采样（节选）

```json
{
  "s1": { "lastAssistantLen": 13, "status": "continue 已提交, job=..." },
  "s3": { "lastAssistantLen": 40, "status": "收到 done 帧" }
}
```

### 4.2 异常提示采样（节选）

```json
{
  "4010": {
    "settingsStatus": "加载 adapters 失败: unauthorized (4010)",
    "createStatus": "创建失败: unauthorized (4010)"
  },
  "4003": {
    "streamStatus": "continue 失败: session not found (4003)",
    "hasRawTimeline": false
  }
}
```

## 5. 风险与说明

1. 本轮验证已覆盖真实浏览器交互路径；不依赖纯静态推断。
2. 浏览器执行使用 `--no-sandbox` 仅用于当前容器 QA 环境，非生产运行建议。

## 6. 最终结论

`TASK-P1-02` 四项验收均满足，判定 **Go**。

