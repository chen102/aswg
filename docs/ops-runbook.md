# 运维 Runbook（MVP）

版本：`v1`  
日期：2026-03-07

## 1. 目标
1. 提供统一的部署、巡检、故障处理与回滚流程。
2. 降低单点人员依赖，确保问题可被快速定位和恢复。

## 2. 运行组件
1. `backend`：统一 API 与 WebSocket 网关。
2. `frontend`：静态页面（会话展示 + 设置页）。
3. `adapter`：以 Codex 为首个参考实现。

## 3. 部署前检查
1. 配置文件齐全，关键环境变量已设置：
   - `SERVER_HOST`
   - `SERVER_PORT`
   - `AUTH_TOKEN`
   - `ENABLED_ADAPTERS`
   - `DEFAULT_ADAPTER`
2. 端口可用，无冲突。
3. 日志目录可写，磁盘空间充足。
4. 对外暴露场景下反向代理与鉴权已配置。

## 4. 启动与停止（模板）
以下命令为模板，待可执行文件或启动脚本确定后替换。

启动后端（示例）：
```bash
./agent-session-web-gateway-server --config ./config/config.yaml
```

停止后端（示例）：
```bash
pkill -f agent-session-web-gateway-server
```

启动前端静态服务（示例）：
```bash
./agent-session-web-gateway-frontend --config ./config/frontend.yaml
```

## 5. 常规巡检
1. 健康检查：
```bash
curl -s http://127.0.0.1:8080/api/v1/health
```
2. 适配器可用性：
```bash
curl -s -H "Authorization: Bearer <token>" http://127.0.0.1:8080/api/v1/adapters
```
3. WebSocket 订阅连通性：
   - 至少验证一条会话可收到 `heartbeat` 帧。
4. 日志检查：
   - 错误率是否异常升高。
   - 是否出现敏感信息。

## 6. 告警建议
1. `health.status != ok` 持续 1 分钟告警。
2. `5xx` 比例超过阈值告警。
3. `4010`、`4290` 突增告警。
4. WebSocket 连接失败率超过阈值告警。

## 7. 故障处理手册

### 7.1 服务不可用
排查步骤：
1. 检查进程是否存活。
2. 检查端口监听与冲突。
3. 查看启动日志中的配置解析错误。
4. 执行 `health` 接口确认恢复。

### 7.2 鉴权失败激增（4010）
排查步骤：
1. 检查 token 是否轮换或过期。
2. 检查反向代理是否透传 `Authorization`。
3. 核对前端设置页 token/地址是否配置错误。

### 7.3 续接任务启动失败（4004）
排查步骤：
1. 检查适配器进程与依赖 CLI 可执行性。
2. 检查工作目录权限与路径存在性。
3. 检查子进程退出码与 stderr。
4. 根据 request_id 检索同链路日志。

### 7.4 WebSocket 频繁断开
排查步骤：
1. 观察是否只有移动网络场景触发。
2. 检查心跳发送是否正常。
3. 核对代理层的空闲超时配置。
4. 客户端按 `last_seq` 续传验证是否可恢复。

## 8. 回滚流程
1. 确认触发条件：
   - 核心功能不可用。
   - 出现 `S0/S1` 级缺陷。
2. 执行回滚：
   - 切回上一稳定版本二进制或镜像。
   - 回退对应配置变更。
3. 验证回滚：
   - `health`、`adapters`、`sessions`、`continue` 全部可用。
4. 记录复盘：
   - 事件时间线、根因、修复计划、预防项。

## 9. 日志与审计
1. 日志至少包含：
   - `request_id`
   - `adapter`
   - `session_id`
   - `latency_ms`
   - `error_code`
2. 敏感字段脱敏后再输出。
3. 审计日志建议保留周期：不少于 30 天（按团队策略调整）。

## 10. Runbook 维护规则
1. 每次发布后检查 Runbook 是否与真实操作一致。
2. 发生故障复盘后，必须补充到对应章节。
