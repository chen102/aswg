# QA 专项验收报告：TASK-P1-04（创建会话）

日期：2026-03-07  
执行人：QA-AGENT  
结论：**Go**

## 1. 验收范围

依据 `A2A.txt` 中 PM 对 `TASK-P1-04` 的要求，验收以下 4 项：

1. 新建后列表可见且默认选中。
2. 新会话可立即 continue 并实时收流。
3. 刷新页面后新会话仍存在。
4. 非法参数返回 `4001`。

## 2. 环境与方法

1. UI 主流程：`http://127.0.0.1:18083`（`AUTH_TOKEN=`）。
2. API 异常/参数校验：`http://127.0.0.1:18080`（`AUTH_TOKEN=qa-token`）与 `18083`。
3. 执行方式：
   - `curl` 校验 REST 返回码与业务码。
   - Playwright（Chrome + `--no-sandbox`）校验前端创建、选中、刷新后存在。
4. 代码核对文件：
   - 前端入口：`frontend/src/index.html:57-64`（创建会话表单）
   - 前端提交逻辑：`frontend/src/app.js:108-145`
   - 后端路由：`backend/internal/server/server.go`（`handleCreateSession`）
   - 契约文档：`docs/api.md`、`docs/api-contract-v1.md`

## 3. 验收结果

| 验收项 | 结果 | 证据 |
| --- | --- | --- |
| 1) 新建后列表可见且默认选中 | PASS | UI 实测：`createStatus=\"创建成功: sess_...\"`；`activeSessionText` 为新建标题 `QA P1 UI Session`。逻辑：创建成功后 `state.selectedSessionID = session.id` 并 `loadSession(session.id)`（`frontend/src/app.js:136-142`）。 |
| 2) 新会话可立即 continue 并实时收流 | PASS | API：`POST /continue` 返回 `202`；事件查询 `has_user=true, has_assistant=true`。UI：提交后气泡角色序列包含新 `user -> assistant`，并出现 `收到 done 帧`。 |
| 3) 刷新页面后新会话仍存在 | PASS | 浏览器实测：刷新前后均可在列表命中新建标题，且刷新后仍为 active。样例：`foundAfter=true`，`activeAfter` 保持新建会话。 |
| 4) 非法参数返回 4001 | PASS | `POST /api/v1/adapters/codex/sessions` 传超长 `seed_prompt(9001)`：HTTP `400`，业务码 `4001`。 |

## 4. 关键执行摘录

### 4.1 创建会话与参数校验（curl 摘录）

```text
http_code=201
{"code":0,"message":"ok","data":{"id":"sess_..."}}

http_code=400
{"code":4001,"message":"invalid parameter",...}
```

### 4.2 刷新后仍存在（浏览器摘录）

```json
{
  "title": "QA-Reload-1772889646222",
  "foundAfter": true,
  "activeAfter": "QA-Reload-1772889646222\n2026-03-07T13:20:47.576450253Z"
}
```

## 5. 额外一致性检查

`TASK-P1-03` 要求的文档同步已满足：

1. `docs/api.md` 已包含 `POST /api/v1/adapters/{adapter}/sessions`。
2. `docs/api-contract-v1.md` 已包含 `CreateSessionRequest` 与 `4.3 POST /sessions` 定义及 `4001` 失败语义。

## 6. 风险与说明

1. 当前仓库适配器为内存实现，跨进程重启后的持久化不在本任务验收范围内。
2. 本任务“刷新后仍存在”按同一服务进程内页面刷新语义验收通过。

## 7. 最终结论

`TASK-P1-04` 四项验收均满足，判定 **Go**。

