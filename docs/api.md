# API 规范（草案）

版本：`v1`

说明：本文为接口总览草案。字段级契约、分页语义、WebSocket 帧结构请以 `docs/api-contract-v1.md` 为准。

## 1. 统一约定
1. 基础路径：`/api/v1`
2. 鉴权：Header `Authorization: Bearer <token>`
3. 返回格式：
```json
{
  "code": 0,
  "message": "ok",
  "data": {}
}
```

## 2. 适配器接口
1. `GET /api/v1/adapters`
- 说明：获取可用适配器列表。

## 3. 会话接口
1. `GET /api/v1/adapters/{adapter}/sessions`
- 说明：查询会话列表。

2. `GET /api/v1/adapters/{adapter}/sessions/{id}`
- 说明：获取会话详情。

3. `GET /api/v1/adapters/{adapter}/sessions/{id}/events`
- 说明：分页读取历史事件。

4. `POST /api/v1/adapters/{adapter}/sessions/{id}/continue`
- 说明：在指定会话继续提问。
- 请求体：
```json
{
  "prompt": "请继续",
  "cwd": "可选"
}
```

## 4. 实时接口
1. `GET /ws/v1/adapters/{adapter}/sessions/{id}`
- 说明：订阅会话实时事件。

## 5. 健康检查
1. `GET /api/v1/health`
- 说明：服务与适配器健康状态。

## 6. 错误码建议
1. `4001`：参数错误
2. `4002`：适配器不存在
3. `4003`：会话不存在
4. `4004`：续接任务启动失败
5. `4010`：鉴权失败
6. `4290`：请求过于频繁
7. `5000`：内部错误
