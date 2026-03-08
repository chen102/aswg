# QA UI E2E 可执行方案（TASK-P0-05）

日期：2026-03-07  
角色：QA-AGENT  
状态：`REVIEW`  
结论：采用方案 A（修复 Playwright 环境并执行 E2E）

## 1. 背景
`qa-report-mvp-2026-03-07-v2` 中 UI E2E 受环境限制（Playwright 在 root+sandbox 组合环境不可用）。  
本次已完成执行链路修复并跑通可自动化的 UI 用例。

## 2. 执行环境修复

### 2.1 启动测试后端
1. 无鉴权实例（主 UI 流程）：
```bash
cd backend && SERVER_HOST=127.0.0.1 SERVER_PORT=18083 AUTH_TOKEN= GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go run ./cmd/server
```

2. 鉴权实例（E2E-09）：
```bash
cd backend && SERVER_HOST=127.0.0.1 SERVER_PORT=18084 AUTH_TOKEN=qa-token GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go run ./cmd/server
```

### 2.2 Playwright 运行时
在临时目录安装并使用 Playwright，避免污染仓库依赖：
```bash
mkdir -p /tmp/qa-ui-playwright
cd /tmp/qa-ui-playwright
npm init -y
npm install playwright@1.58.2
```

浏览器启动参数使用：
`--no-sandbox --disable-setuid-sandbox`

## 3. 用例结果（E2E-01 ~ E2E-10）

| 用例ID | 结果 | 证据摘要 |
| --- | --- | --- |
| E2E-01 | PASS | 页面加载后会话列表可见，`sessions=1` |
| E2E-02 | BLOCKED | 仅单适配器 `codex`，无“切换适配器”可执行对象 |
| E2E-03 | PASS | 点击会话后详情区渲染 `sess_demo_001` 信息 |
| E2E-04 | PASS | `continue` 后时间线行数增长（示例 `5->8`） |
| E2E-05 | PASS | 点击“重连流”后状态恢复 `WS 已连接` |
| E2E-06 | PASS | 修改设置后刷新仍保持 `18083` 地址 |
| E2E-07 | PASS | “连接测试”返回 `连接成功: health=ok, adapters=1` |
| E2E-08 | PASS | “恢复默认”后字段恢复 `8080/codex` 默认值 |
| E2E-09 | PASS | 在 `18084` 同源下未携带 token，页面显示 `unauthorized (4010)` |
| E2E-10 | PASS | 页面上下文发起缺失会话请求，返回 `404 + code 4003` |

## 4. 关键输出摘录

主 UI 脚本输出：
```text
PASS E2E-07 连接成功: health=ok, adapters=1
PASS E2E-01 sessions=1
BLOCKED E2E-02 only one adapter option (codex)
PASS E2E-03 session detail rendered
PASS E2E-04 timeline 5->8
PASS E2E-05 WS 已连接
PASS E2E-06 http://127.0.0.1:18083 | ws://127.0.0.1:18083
PASS E2E-08 reset to runtime defaults
PASS E2E-10 404 + code 4003
```

E2E-09 独立脚本输出：
```text
PASS E2E-09 加载 adapters 失败: unauthorized (4010) || 连接失败: unauthorized (4010)
```

## 5. 对 RC-06 的影响
1. 之前“UI E2E 无法执行”的环境阻塞已解除。
2. 当前剩余阻塞是功能/范围性质：`E2E-02`（单适配器场景）。
3. 若要满足“核心 E2E 全通过”，需二选一：
   - 补充至少第二个可切换适配器（可为测试桩）。
   - 或由 PM+QA 确认在 MVP 单适配器模式下将 E2E-02 标记为 N/A（并回写测试计划）。
