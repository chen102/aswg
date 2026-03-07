# 配置说明

## 1. 设计原则
1. 所有可变参数外置。
2. 前端支持运行时覆盖，不强制重新构建。
3. 配置项使用清晰默认值。

## 2. 后端配置
推荐使用环境变量 + 配置文件覆盖。

关键配置项：
1. `SERVER_HOST`：服务监听地址，默认 `127.0.0.1`
2. `SERVER_PORT`：服务端口，默认 `8080`
3. `AUTH_TOKEN`：API 鉴权令牌
4. `ENABLED_ADAPTERS`：启用的适配器列表
5. `DEFAULT_ADAPTER`：默认适配器
6. `LOG_LEVEL`：日志级别

## 3. 前端配置
前端采用两层配置：
1. 默认配置文件：`public/runtime-config.json`
2. 本地覆盖配置：`localStorage`（来自设置页）

`runtime-config.json` 示例：
```json
{
  "api_base_url": "http://127.0.0.1:8080",
  "ws_base_url": "ws://127.0.0.1:8080",
  "default_adapter": "codex",
  "request_timeout_ms": 30000
}
```

## 4. 前端设置页要求
1. 可编辑 `api_base_url`。
2. 可编辑 `ws_base_url`。
3. 可编辑 `default_adapter`。
4. 提供“连接测试”按钮。
5. 提供“恢复默认配置”按钮。

## 5. 配置校验建议
1. 地址格式校验（http/https、ws/wss）。
2. 超时阈值校验（最小值/最大值）。
3. 适配器名称白名单校验。

## 6. 发布注意事项
1. 不提交真实 Token。
2. 提交 `config.example.*`，不提交私有 `config.*`。
3. CI 中增加配置文件结构校验。
