# DEV 创建会话交付报告（TASK-P1-03）

日期：2026-03-07  
角色：DEV-FULLSTACK  
任务：`TASK-P1-03`  
目标：新增 `POST /api/v1/adapters/{adapter}/sessions` 与前端“新建会话”入口，创建成功后自动进入并连接流

## 1. 变更摘要

1. 新增后端创建会话接口：
   - `POST /api/v1/adapters/{adapter}/sessions`
   - 请求体：`title/workspace/seed_prompt`（均可选）。
   - 成功：HTTP `201` + `SessionDetail`。
2. Codex Adapter 新增 `CreateSession` 能力并注册 capability：
   - 默认值：`title=New Session`、`workspace=/workspace/new`。
   - 参数约束：`title<=200`、`workspace<=2000`、`seed_prompt<=8000`。
   - `seed_prompt` 非空时写入初始用户事件 + assistant 确认事件。
3. 前端新增“新建会话”内联表单：
   - 创建成功后刷新列表。
   - 自动选中新会话并加载详情。
   - 自动建立 WS 连接，支持立即 continue。
4. 同步 API 文档与契约说明：
   - `docs/api.md`
   - `docs/api-contract-v1.md`

## 2. 变更文件

1. `backend/internal/model/model.go`
2. `backend/internal/adapter/adapter.go`
3. `backend/internal/adapter/codex/adapter.go`
4. `backend/internal/adapter/codex/adapter_test.go`
5. `backend/internal/server/server.go`
6. `backend/internal/server/create_session_test.go`
7. `frontend/src/index.html`
8. `frontend/src/styles.css`
9. `frontend/src/app.js`
10. `docs/api.md`
11. `docs/api-contract-v1.md`

## 3. 接口说明（实现与契约一致）

### 3.1 REST

`POST /api/v1/adapters/{adapter}/sessions`

请求示例：

```json
{
  "title": "P1 Smoke Session",
  "workspace": "/workspace/p1",
  "seed_prompt": "hello seed"
}
```

成功响应：HTTP `201`，`code=0`，`data` 为 `SessionDetail`。  
失败响应：参数非法返回 `4001`。

### 3.2 前端行为

1. 用户在 `create-session-form` 提交可选字段。
2. 成功后显示“创建成功: <session_id>”。
3. 刷新会话列表并自动选中创建出的会话。
4. 调用 `loadSession()` 获取详情/历史并连接 WS。

## 4. 回归测试与结果

### 4.1 自动化测试

```bash
cd backend && GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./...
node --check frontend/src/app.js
```

结果摘要：通过。  
覆盖项包含：

1. `TestCreateSessionWithSeedPrompt`
2. `TestCreateSessionEndpointSuccess`
3. `TestCreateSessionEndpointInvalidParam`

### 4.2 curl smoke（证据日志）

证据文件：`docs/evidence/p1-evidence.log`

成功创建（201）示例摘录：

```text
HTTP/1.1 201 Created
...
{"code":0,"message":"ok","data":{"adapter":"codex","id":"sess_1772889296169053789","title":"P1 Smoke Session",...}}
```

非法参数（4001）示例摘录：

```text
HTTP/1.1 400 Bad Request
...
{"code":4001,"message":"invalid parameter",...}
```

创建后列表可见（query 命中）摘录：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "items": [
      {
        "id": "sess_1772889296169053789",
        "title": "P1 Smoke Session"
      }
    ]
  }
}
```

## 5. 已知限制

1. 当前创建会话为内存态实现，服务重启后会话不会持久化。
2. session_id 生成依赖时间戳，满足 MVP 唯一性需求；若进入分布式部署需升级为全局唯一 ID 策略。
3. 当前默认 `source=cli`、`metadata.model=gpt-5-codex`，后续多适配器可扩展元数据来源。

## 6. 当前状态

`REVIEW`

等待 QA-AGENT 执行 `TASK-P1-04` 验收。
