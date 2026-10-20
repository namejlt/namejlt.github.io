---
title: "AI Agent-deepseek-harness插件开发013 原理篇③：主循环与工具调用链路"
date: 2026-10-20T10:20:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "Agent Loop", "工具调用"]
---

## 概述

你在 005 篇学会了写工具，但不知道工具是被谁、在什么上下文里调起来的；你在 008 篇学会了监听事件，但不知道某个时刻"下一句该发什么"是谁决定的。这一篇把时间轴补上：**从一次用户输入，到工具结果落进会话日志**，中间每一步落在哪个包、哪个事件、哪个插件可介入的点。

只讲一条链路，但讲到能用来定位线上问题。所有结论来自：`dsh-agent-loop/README.zh.md`、`dsh-tools/README.zh.md`、`dsh-session/README.zh.md`、`dsh-client-connection`（传输）、`dsh-base/cordis.patch.yml`（谁被挂载）。

## 一、全景：一条消息的九站

```
浏览器 (client-connection)                                  宿主进程
──────────────────────────────────────────────────────────────────────────
你按回车
  │  RPC: 提交用户消息
  ├──────────────────────────▶ dsh-api-gateway（typert RPC 面）
  │                                   │
  │                                   ├─▶ agent.inbox 追加（Session.append）
  │                                   │       └─ 提交 agent/inbox/spliced 事件
  │                                   │
  │                                   ▼
  │                          ┌──────────────────┐
  │                          │  turn 开始        │  agent-loop 领取 inbox
  │                          └────────┬─────────┘
  │                                   ▼
  │                          ┌──────────────────┐
  │                          │  step 开始        │
  │                          └────────┬─────────┘
  │                                   │ ① agent/pre-step 事件（可改写本步输入）
  │                                   ▼
  │                          ② 派生历史 + 可见工具 schema
  │                              （system/message 取最新非空节点）
  │                                   │
  │                                   ▼
  │                          ③ agent/request 事件（可补 provider/model）
  │                                   │
  │                                   ▼
  │                          ④ ctx.llm.prepareCall() 校验适配器字段、
  │                              解析 reasoningEffort / maxTokens 默认值
  │                                   │
  │                                   ▼
  │                          ⑤ 流式请求 + 记录 request/header
  │                                   │  （assistant/message 落库前的流）
  │                                   ▼
  │                          ⑥ 工具调用分发（受护栏流水线）
  │                                   │  并行安全调用 ≤ maxParallelToolCalls
  │                                   │  独占调用单独跑 + 排序屏障
  │                                   ▼
  │                          ⑦ 每条被接纳的事实先落会话日志
  │                                   │
  │                                   └─▶ 回到 step 开始（有下一批调用）
  │                                   ▼
  │                          ⑧ turn/end（含 turn 结束原因）
  │
  ◀──────────────────────────  SSE 推送投影更新（流式文本、卡片、任务条）
```

九站里，**插件能介入的是 ①③⑥⑦**：`agent/pre-step`、`agent/request`、`tools/*` 流水线、`session/event`。其余是框架内部时序，别试图插进去。

## 二、第 0 站：消息是怎么"进来"的

Web 侧的真实路径是 RPC → API 网关 → agent inbox，不是"直接调模型"。要点：

- 每条用户消息经 `Session.append()` 写入**事件溯源日志**，同时发出 `agent/inbox/spliced`；`Session.append()` 返回时，实时投影已经反映这次追加（读己之写）。
- inbox 有三条语义不同的事件：`agent/inbox/inserted`（新消息）、`agent/inbox/claimed`（被 agent 领取）、`agent/inbox/discarded`（被丢弃，携带 `outcome: 'canceled'`）。
- **需要"消息被移除"的消费方，应该监听 claimed/discarded，而不是盯着 splice 前的投影视图**——这是官方明确的坑。

对插件作者的意义：想在"模型看到用户消息之前"动手，正确位置是 `agent/pre-step`（改写本步输入）或会话 surface 投影层，**不是**在 gateway 或传输层劫持。

## 三、第 1–2 站：step 的输入从哪来

每个 step 发出两样东西：**派生历史** 和 **可见工具 schema**。

- 派生历史 = 会话日志经 surface 投影后的消息序列。规则：**最新一个非空 `system/message` 节点就是有效提示词**；它的渲染提示词为空时，请求里就没有系统消息。
- 可见工具 = 注册表按作用域投影出来的 `ToolSchema` 集合（**不含** `execute`、`output`、`render`、`timeoutMs`——这些永不泄漏到协议）。
- 工具可见性是可裁剪的：`ctx.tools.restrict(filter)` 按 agent 作用域做允许/拒绝掩码（掩码取交集，dispose 时解除），`ctx.tools.schemas(scope)` 拿这份投影。

插件视角的三个推论：

1. 你 register 一个工具，**不需要**手动去改提示词——注册表会把 schema 送进提示词组装。
2. 动态增删工具会改变**请求前缀**，进而影响 provider 的 KV cache 命中（`dsh-tools` 的 README 专门有 "KV Cache 影响" 段）。
3. 想给某类会话"少几个工具"，用 `restrict` 或分层组合，别在 `description` 里写"请不要用我"。

## 四、第 3–5 站：请求的准备与失败

- `agent/request` 是**分发前最后一次机会**：声明式 agent 缺少 `provider`/`model` 时，监听器可以补齐这对值；也可以覆盖 `reasoningEffort`。
- 之后 `ctx.llm.prepareCall()` 才真正校验适配器字段（在活跃 turn 的 signal 下），解析推理强度与输出 token 默认值；**同一个适配器实例要贯穿解析、`request/header` 记录与分发**——换适配器会让 header 与实际请求不一致。
- 请求头（`request/header`）记录的是 envelope：provider/model、工具集合、显式消息序列起点、surface 的 `contentGeneration`。**提示词本身不算 header**。
- 失败分两类：
  - **可恢复**：`agent/request-error` 的监听器返回 `{ kind: 'retry' }` 且不调 `next()` → 重试；
  - **终态**：没人处理 → 该 turn 结束（**插件失败结束的是轮次，不是整个循环**）。

## 五、第 6 站：工具分发，五段流水线

这是插件作者最需要精确掌握的一段。每次调用都跑固定五段：

```
tools/pre-execute   ── 可扩展的允许/拒绝/询问门禁（waterfall，可重排）
      ↓
已注册 guardians     ── ctx.tools.guard()：单调同步守卫，一旦拒绝不可被后续翻案
      ↓
tools/execute        ── 环绕分发的包装层（超时、重试、指标）；唯一可以替换必填 signal 的视图
      ↓
tools/post-execute   ── 检查/替换内容或值、阻止、附加有序上下文
      ↓
finalizeContent      ── 定义自身持有的内容收尾
      ↓
tools/result         ── 只读观测（emit，冻结后的最终结果）
```

配套的硬约束：

| 约束 | 说明 |
|------|------|
| **`pre-execute` 不能改写 `exec.arguments`** | 否则日志与实际运行内容脱节；这是有意设计（改写方案仍在 proposed 阶段） |
| **guard 是"单调"的** | 同步、只能收紧不能放宽；顺序在 `pre-execute` 之后 |
| **`timeoutMs` 只是声明** | 注册表**从不**强制执行截止时间；强制要靠 `@deepseek-ai/dsh-tool-call-timeout-policy` 包装层 |
| **取消是协作式** | 每次调用都给 `exec.signal`，工具必须观测它；主体执行前取消 → `ABORTED_BEFORE_DISPATCH`，执行后取消 → 只能把成功改写成 `ABORTED` |
| **未知工具与抛错都变结构化错误** | `UNKNOWN_TOOL` 等错误码让调用失败但**不结束 turn** |

### 5.1 并行与独占

- 每个 step 里，**并行安全**的工具调用最多重叠 `maxParallelToolCalls` 个（默认 `10`；`1` 表示严格串行）。
- **声明字段是 `isConcurrencySafe(args) → boolean`**，写在 `defineTool` 的选项里（`ToolDefinition` 上是可选方法。缺省 = 独占；传了函数则先跑参数校验，**参数不合法一律返回 `false`**，不会让脏参数拿到并行许可）。分类器在 `ctx.tools.executionMode(exec)` 里，返回 `{ kind: 'parallel' | 'exclusive' }`，调度器按它决定能否并进当前池。
- **独占调用单独执行，并构成排序屏障**：它前后的并行组不能越过它。所以"先写文件，再并行读三个文件"不需要你手动加锁——但顺序依赖必须靠这个声明表达：**只有你声明了并行安全，它才会被并行**；默认为独占，这也是最安全的默认。
- PTC 模式另有一层并发上限：`maxParallelSubCalls`（默认 `10`，`1` 恢复严格串行分发）。

## 六、第 7 站：为什么"模型可见 = 已落库"

`dsh-session` 的规则：**每个被接纳的事实都在下一步据此派生之前追加到会话日志**。这带来两个直接可观察的后果：

1. **turn 被取消后不会"凭空少一条"**：未分发的模型工具调用会收到一对合成的 `tool/call` + `ABORTED_BEFORE_DISPATCH` 结果，让历史保持配对完整。
2. **崩溃恢复靠对账，不靠猜**：
   - 恢复时发现 assistant 请求了工具，但日志里没有 `tool/call` → 合成 `TOOL_NOT_STARTED`（"记录为开始前就被中断，需要的话请重试"）；
   - 有 `tool/call` 但没有结果 → 合成 `TOOL_OUTCOME_UNKNOWN`（"结果未知，只对只读或幂等操作直接重试，有副作用先核实外部状态或问用户"）。

> 这条对插件作者的现实要求：**你的工具不该假设"没被记录就等于没执行"**。有副作用的操作要么幂等、要么把幂等键写进规范值，让恢复后的模型能自己判断。

## 七、第 8 站：turn 的收尾与取消

- `turn/end` 携带结束原因（`TurnEndCancelCause`）；取消时在里面记一份新的 `AgentCancelCause`，**保留调用方的 `kind` 与 hook 的 `reason` 文本**，但不把调用栈写进日志。
- `agent.cancel()` 中止当前活动，并在未设置 `keepInbox` 时清掉待处理工作；**已经流式交付给用户的文本会保留**（半句话不会凭空消失）。
- 取消后的收尾顺序：先结算已启动的分发，再恢复原 turn 的失败原因；已提交的结果不动。

## 八、压缩与"前缀稳定"：为什么改工具会影响成本

- 上下文压缩（`packages/compaction/*`）会把长历史折叠成摘要；**折叠点是 surface 替换**，不是删除。
- 请求归并策略：只有"已准备路由未声明 `toolUpdate`"时，可见工具 schema 变化才**强制合并提示词**；既能更新工具又能更新提示词的路由，可以追加变更后的提示词并激活新工具，而不改写此前消息。
- 实践含义：**插件在每次 `agent/pre-step` 里都改系统提示词，会让前缀每轮都变**，等于自愿放弃 provider 侧的缓存复用。要动态上下文，优先用"独立 `user/message` 注入"这类不破坏稳定前缀的形态。

## 九、插件介入点速查

| 你想做的事 | 介入点 | 能不能改数据 |
|-----------|--------|--------------|
| 本步开始前改写输入/注入上下文 | `agent/pre-step` | 能（本步生效） |
| 补/改 provider、model、推理强度 | `agent/request` | 能（本请求生效） |
| 工具调用前允许/拒绝/询问 | `tools/pre-execute` | 能（否决或问） |
| 红线兜底、不可翻案 | `ctx.tools.guard()` | 只能拒绝 |
| 给工具加分发层（超时/重试） | `tools/execute` | 能（可替换 signal） |
| 脱敏、改写结果、附加上下文 | `tools/post-execute` | 能（内容与值均可，但**内容替换不影响程序侧取值**） |
| 审计每次调用 | `tools/result` | 只读 |
| 观察轮次/步/消息/工具事实 | `session/event`（`(session, event)`） | 只读（要改请走投影） |
| 会话落盘时机 | `session/flush` | 观测 |

## 小结

- 一条消息九站：inbox → turn → step → `agent/pre-step` → 派生历史 + 工具 schema → `agent/request` → `prepareCall` → 流式 + 工具分发 → 落库 → 下一 step；
- 工具分发是固定五段流水线：`pre-execute` → guards → `execute` → `post-execute` → `finalizeContent` → `result`；
- `timeoutMs` 只声明不执行；取消靠 `exec.signal` 与 `ABORTED*`；抛错变结构化错误、不结束 turn；
- 并行安全受 `maxParallelToolCalls` 限制，独占调用是排序屏障；
- 会话日志是唯一真相：取消/崩溃靠 `TOOL_NOT_STARTED`、`TOOL_OUTCOME_UNKNOWN` 对账；
- 频繁改提示词 = 主动放弃 KV cache，动态上下文优先用独立消息注入；
- 下一篇回到宿主侧：这套循环是怎么被"装配"起来的——加载、组合、热更新。

---

**系列导航**：[001 系列导读](/posts/tech/ai/agent/deepseek-harness-dev-001/) · [上一篇：012 设计哲学](/posts/tech/ai/agent/deepseek-harness-dev-012/) · [下一篇：014 插件加载与热更新](/posts/tech/ai/agent/deepseek-harness-dev-014/)
