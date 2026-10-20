---
title: "AI Agent-deepseek-harness插件开发005 模型工具开发：defineTool五层契约"
date: 2026-10-19T10:30:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "defineTool", "工具开发", "Function Calling"]
---

## 概述

工具（Tool）是模型接触世界的唯一通道，也是插件最核心的能力形态。官方教程用 20 行代码演示了最小工具；本篇把工具**当作一份合同**来拆解：模型看到什么、参数怎么校验、执行受什么约束、返回值是什么结构、界面怎么渲染——五层契约逐层讲透，外加 PTC（程序化调用）模式下的设计准则。学完后你写的工具，模型一用就顺。

## 一、工具的五层契约

```ts
import { defineTool } from '@deepseek-ai/dsh-tools'

ctx.tools.register(defineTool({
  // 第 1 层：身份 —— 模型据此决定是否调用
  name: 'greet',
  description: 'Greet someone by name.',

  // 第 2 层：入参契约 —— 模型据此填参，框架据此校验
  parameters: {
    name: { type: 'string', required: true, description: 'The name to greet' },
  },

  // 第 3 层：执行体 —— 领域行为，args 已校验
  async execute(args) {
    return `Hello, ${args.name}!`
  },

  // 第 4 层：输出契约 —— 程序化规范值
  output: {
    schema: { type: 'string' },
    // 第 5 层：渲染 —— 面向模型的内容块
    render: (_args, value) => [{ type: 'text', text: value }],
  },
}))
```

| 层 | 字段 | 消费者 | 设计责任 |
|----|------|--------|----------|
| 1 | `name` / `description` | 模型（决策） | description 是模型唯一能读到的说明书 |
| 2 | `parameters` | 模型（填参）+ 框架（校验） | 类型、必填、说明 |
| 3 | `execute` | 你的代码 | 领域行为、遵守 signal |
| 4 | `output.schema` | 程序（PTC 程序、调用方） | 唯一规范 JSON 值 |
| 5 | `output.render` | 模型（读取结果） | 规范值 → 内容块 |

此外还有三样可选件：`presentCall` / `presentResult`（Host 本地消费方的卡片意图）、`output.presentationMeta`（派生给 Web 卡片的持久 metadata）、`timeoutMs`（仅声明）。分别见第五节与 3.4。

## 二、第 1、2 层：把工具「写说明书」写到极致

### 2.1 description 是模型的眼睛

模型选工具时只看 `name` + `description`。写法准则：

- 说清**做什么**，不说**怎么做**；
- 给出适用场景与不适用场景（模型据此排除误用）；
- 涉及单位/格式时写明（`timeoutMs` 而非 `timeout`）。

```ts
// ❌ 模型不知道该何时用
description: 'Search documents.'

// ✅ 场景、输入、行为、边界都清楚
description: '在本地知识库中做全文检索。适用：查找团队文档、会议纪要、API 手册中的段落。'
  + '输入自然语言查询，返回带出处（文档名+片段）的结果列表。不做实时网页搜索。'
```

### 2.2 parameters：扁平化是最佳实践

官方 DSL 支持标量字段（`type: 'string' | 'number' | 'boolean'`，加 `required` 与 `description`）。**生产建议：优先扁平标量参数**：

```ts
parameters: {
  query:  { type: 'string', required: true, description: '检索查询，自然语言' },
  topK:   { type: 'number', description: '返回条数，默认 5' },
  source: { type: 'string', description: '限定文档名，可选' },
}
```

需要传结构化数据时，**单个 JSON 字符串参数**比深层嵌套 schema 更稳（解析失败可以给出清晰的错误信息，模型也更容易写对）：

```ts
parameters: {
  filters: {
    type: 'string',
    description: '过滤条件 JSON 字符串，如 {"tag":"api","after":"2026-01-01"}',
  },
},
async execute(args) {
  let filters: Record<string, unknown> = {}
  if (args.filters) {
    try { filters = JSON.parse(args.filters) } catch { /* 领域错误，见 3.5 */ }
  }
  // ...
}
```

> **DSL 的完整类型集**（`dsh-tools` README 原文）：`string`、`number`、`integer`、`boolean`、`null`、`array`、`object`、仅供作者使用的 `json`、以及恰好匹配一个分支的 `oneOf`。`InferValue` 在 16 层容器内保留精确类型，之后加宽为 `JsonValue`；原始 JSON Schema（`JsonSchemaNode`）是与 subagent/工作流/MCP 共享的协议级类型。所以嵌套对象、数组、可空字段都能直接表达，**不需要**"扁平标量 + JSON 字符串"这种绕法——本系列示例仍保留一处 JSON 字符串写法，只是为了演示领域错误处理（见 3.5）。

### 2.3 校验边界：框架管格式，你管语义

`defineTool` 在执行前校验：类型、必填、字面量约束。**DSL 表达不了的仍要自己查**：非空字符串、正数、跨字段规则（如 `end > start`）、JSON.parse 的结果。

```ts
async execute(args) {
  if (!args.query.trim()) throw new Error('query 不能为空白')
  if (args.topK !== undefined && args.topK <= 0) throw new Error('topK 必须为正数')
  // ...
}
```

## 三、第 3 层：execute 的八条契约

工具执行函数有八条硬约束（源自官方 cookbook 工具编写参考）：

1. **参数已替你校验**：`args` 类型安全；DSL 之外的语义约束自己补；
2. **注册借用的是只读定义**：注册后不要改 schema、不要替换回调；想热替换工具，正确做法是释放它所属的 effect 再注册新工具；
3. **执行身份受保护**：调用 id、工具名、参数、令牌全程不可变，`args` 按只读输入对待；
4. **声明并返回唯一的规范 JSON 值**：`output.schema` 的根可以是对象、数组、标量或 `null`；注册表负责快照、校验、冻结后交渲染。**不要从函数体直接返回内容块**，更不要逼调用方从自然语言里抠 id；
5. **抛异常或返回非法值 = `isError`**：基础设施故障才抛异常；**业务上「不理想但成功」的结果要写进规范值**（如进程非零退出码），由渲染层解释状态；
6. **遵守 `exec.signal`**：信号触发时取消进行中的工作。所有支持 signal 的 Node API（`fetch`、`fs` 读写）都传下去；
7. **`presentationMeta` 派生可回放的卡片数据**：需要结果期事实的卡片数据，用可选 `presentationMeta` 从同一个规范值派生，核心会持久化到结果事件上，回放时无需保留原始值；
8. **异步通知走 `exec.agent` 注入**：注入的上下文下一次模型请求可见，但它不是唤醒——空闲 agent 不会被吵醒；注入时防备目标已销毁。

### 3.1 signal 传导示例

```ts
async execute(args, exec) {
  const resp = await fetch(args.url, { signal: exec.signal })
  const text = await resp.text()
  // 读大文件同理：readFile(path, { signal: exec.signal })
  return { status: resp.status, body: text.slice(0, 20000) }
}
```

用户点「取消」→ signal 触发 → 你的 fetch 中断 → 工具以取消结束。不接 signal 的后果：调用栈挂着，UI 转圈，会话卡死。

### 3.2 规范值 vs 自然语言（最容易犯的错）

```ts
// ❌ 把 id 藏进人类文案，调用方只能正则解析
async execute() { return '已启动后台任务 bash-1，用 job_kill bash-1 停止' }

// ✅ 结构化句柄，人类文案交给 render
async execute() { return { kind: 'background', jobId: 'bash-1' } }
```

Code Mode（现名 **PTC mode**，`mode: 'ptc'`；程序化调用）下，模型写的是 `const r = await tools.start_job(...)` 这样的代码，拿到的必须是 `r.jobId`。**输出 schema 按程序化接口设计**，人类解释全部留给 render。

### 3.3 错误处理的三层分类

| 类别 | 例子 | 处理 |
|------|------|------|
| 基础设施故障 | 网络断了、文件不存在、DB 连不上 | 抛异常 → `isError` |
| 领域结果（不理想但成功） | 退出码 1、检索无结果、下载 404 | 写进规范值（`{ ok: false, reason: 'no_results' }`） |
| 参数语义错误 | query 空白、topK ≤ 0 | 抛异常（带清晰 message） |

错误信息是写给模型看的：`throw new Error('topK 必须为正整数，收到 -3')` 比 `throw new Error('bad input')` 的自我修复率高一个量级。

### 3.4 三个"声明了但不会自己生效"的字段

这三个字段最容易让人以为"我配了它就会工作"：

| 字段 | 真实语义 | 你要做什么 |
|------|----------|------------|
| `timeoutMs` | **仅作声明**，注册表从不强制截止时间。要强制执行，必须挂 `@deepseek-ai/dsh-tool-call-timeout-policy` 包装层 | 长任务自己实现超时，并接 `exec.signal` |
| `deferLoading: true` | 请求**延迟加载工具定义**；它**不**意味着存在 `tool-addition` 记录（提供方执行语义另有已知限制） | 工具很多、提示词吃不消时才用 |
| `mode: 'ptc'` | 非原生模式要求组合里已有 `ctx.ptcRuntime`，且该语言已注册 SDK 渲染器 | 不确定就别设，默认 `native` |

调用失败时的结构化错误码也值得记住（它们让"调用失败但轮次继续"成为可能）：

| 错误码 | 触发 |
|--------|------|
| `UNKNOWN_TOOL` | 工具名不存在，或工具主体抛异常 |
| `ABORTED_BEFORE_DISPATCH` | 工具主体开始前被取消 |
| `ABORTED` | 主体成功后取消（只能把成功改写成取消） |
| `TOOL_TIMEOUT` | 包装层超时（拒绝/包装层失败/工具失败/后置策略失败也保留各自更具体的结果） |

## 四、第 4、5 层：输出契约与渲染

`execute` 返回规范值 → 注册表校验/冻结 → `render(args, value)` 转成模型可读的内容块：

```ts
output: {
  schema: {
    type: 'object',
    properties: {
      hits: { type: 'array' },
      total: { type: 'number' },
    },
  },
  render: (_args, value) => [{
    type: 'text',
    text: value.hits.map(h => `[${h.score.toFixed(2)}] ${h.title}: ${h.snippet}`).join('\n'),
  }],
}
```

要点：

- **schema 的根**可以是对象/数组/标量/null，注册表统一快照校验；
- render 是**纯函数**：同样的 `(args, value)` 永远产出同样内容块（回放依赖这一点）；
- 人类可读性与机器可读性分离：模型看 render 的文本，程序看 schema 的值。

## 五、UI 呈现：presentCall / presentResult 与 Web 卡片的真相

工具可用两个纯展示方法声明呈现意图：

- `presentCall(args)`：待执行状态（参数预览）；
- `presentResult(args, value)`：完成状态（结果预览）。

返回带 `card` 标签的渲染意图，**给 Host 本地消费方**映射成视图。卡片类型：

| 卡片 | 阶段 | 长相 | 典型使用者 |
|------|------|------|------------|
| `generic` | 待执行 & 已完成 | 默认卡片，可带 kind 图标与 locations（文件跳转） | 大多数工具 |
| `terminal` | 待执行 & 已完成 | shell 命令 + 原始输出、退出码、信号 | bash 类 |
| `diff` | 待执行 & 已完成 | 文件创建/修改的行内 diff | write/edit 类 |
| `read` | 仅已完成 | 带行号、语法高亮的代码窗口 | 读文件类 |
| `search` | 待执行 & 已完成 | 检索结果列表 + 截断标记 | 检索类 |

三条硬规则：

1. **纯函数**：实时流与会话回放都会运行——不做 IO、不读会话状态、不碰时钟随机数；diff 一律从参数派生；
2. **UI 格式隔离**：终端代码块、相对化路径等 UI 专用格式不许混进模型结果；
3. **软校验兜底**：`defineTool` 对展示路径软校验，参数损坏时回退通用卡片，**展示永远不能弄崩回放**。

### 5.1 必须纠正的一个误解：Web UI 不消费 `present*`

`dsh-tools` README 的原话是：

> 工具可以为 **Host 本地消费方**保留纯函数 `presentCall()` 与 `presentResult()` 方法。**内置 Web Client 不消费这些值**，而是通过 `tool.call.toolview` 选择 renderer，并从**原始调用参数、结果内容、失败状态与持久 metadata** 派生 card props。

所以三端各看各的：

| 消费方 | 拿什么渲染 | 你要做什么 |
|--------|-----------|------------|
| 内置 Web 客户端 | 客户端 renderer（`tool.call.toolview`）+ 原始参数 + 内容 + 失败状态 + **持久 metadata** | 写 client 侧 renderer（第 015 篇）；**想让卡片拿到额外字段，用 `output.presentationMeta` 派生 metadata** |
| 桌面壳 / CLI / 第三方宿主 | `presentCall` / `presentResult` 的 `card` 意图 | 写这两个纯函数 |
| 模型 | `render()` 产出的内容块 | 写 `render`（第 4 层） |

也就是说：**`presentResult` 写错不会让 Web UI 变丑，因为它根本不看**。反过来，`output.presentationMeta` 的结果会被快照进结果事件的 metadata，这才是"给 Web 卡片喂数据"的正道。

```ts
// 示意：卡片声明为纯展示意图，具体 card 载荷结构以当前版本参考为准
presentCall: (args) => ({
  card: 'search',
  query: args.query,
}),
presentResult: (args, value) => ({
  card: 'search',
  query: args.query,
  hits: value.hits.map(h => ({ title: h.title, snippet: h.snippet })),
  truncated: value.truncated,
}),

output: {
  schema: { type: 'object', /* ... */ },
  render: (_args, value) => [{ type: 'text', text: /* ... */ }],
  // ← Web 卡片要靠这个：从规范值派生出可持久化的展示数据
  presentationMeta: (_args, value) => ({ total: value.total, truncated: value.truncated }),
}
```

`presentationMeta` 在 `output` 里，签名是 `(args, value)`，返回值会被**快照**（校验后冻结）写进结果事件；它抛错会让本次调用以投影错误失败——所以别在里面做危险计算。

## 六、PTC mode（原 Code Mode）：白送的程序化调用

PTC（`mode: 'ptc'`，Programmatic Tool Calling）下，每个可见的已注册工具都能以 `await tools.<工具名>(参数)` 直接调用，**无需额外集成**：

```ts
// 模型在 PTC 程序里写的代码
const results = await tools.kb_search({ query: '发布流程', topK: 3 })
const titles = results.hits.map(h => h.title)
```

- 参数与返回类型由同一组 schema 派生（`ToolArgsMap` / `ToolOutputMap`）；
- 调用重新进入正常执行流水线：成功 = 策略处理后的最终规范值；失败 = 真实的 `ToolCallError`（程序只看到名称与可读消息，拿不到内部错误码）。

三条设计推论：

1. 输出 schema 按程序化接口设计（字段稳定、类型精确）；
2. 直接返回句柄与字段，别让程序解析文本；
3. 中间值只在执行期存在，工具自己要如实声明采集边界。

## 七、完整示例：生产级 read_document 风格工具

把五层契约组装成一个读文档工具（对标社区 `taxueseek/dsh-files` 的 `read_document` 与 `beancookie/dsh-plugin-anydoc` 的核心行为，此处仅演示本地纯文本读取）：

```ts
import { readFile } from 'node:fs/promises'
import { defineTool } from '@deepseek-ai/dsh-tools'

export const name = 'tool-read-document'
export const inject = ['tools']

const MAX_CHARS = 20000

export function apply(ctx: Context) {
  ctx.tools.register(defineTool({
    name: 'read_document',
    description:
      '读取本地文本文件（.md/.txt/.json/.csv 等），返回带行号的内容。'
      + '适用：查看文档、配置、日志。二进制文件请先用转换工具。',
    parameters: {
      path: { type: 'string', required: true, description: '文件路径（绝对或相对工作区）' },
      offset: { type: 'number', description: '起始行号，从 1 开始，默认 1' },
      limit: { type: 'number', description: '最多读取行数，默认 500' },
    },
    output: {
      schema: {
        type: 'object',
        properties: {
          path: { type: 'string' },
          totalLines: { type: 'number' },
          truncated: { type: 'boolean' },
          content: { type: 'string' },
        },
      },
      render: (_args, value) => [{
        type: 'text',
        text: `${value.path}（共 ${value.totalLines} 行${value.truncated ? '，已截断' : ''}）\n${value.content}`,
      }],
    },
    async execute(args, exec) {
      const raw = await readFile(args.path, { encoding: 'utf8', signal: exec.signal })
      const lines = raw.split('\n')
      const offset = Math.max(1, args.offset ?? 1)
      const limit = args.limit ?? 500
      const slice = lines.slice(offset - 1, offset - 1 + limit)
      let content = slice
        .map((line, i) => `${String(offset + i).padStart(5)}│ ${line}`)
        .join('\n')
      const truncated = content.length > MAX_CHARS
      if (truncated) content = content.slice(0, MAX_CHARS)
      return { path: args.path, totalLines: lines.length, truncated, content }
    },
  }))
}
```

对照检查：description 有场景与边界 ✅；参数扁平 ✅；signal 传导 ✅；规范值是对象（程序友好）✅；render 纯文本可回放 ✅；截断信息进 schema 而不只是文案 ✅。

## 八、常见坑速查

| 坑 | 后果 | 对策 |
|----|------|------|
| execute 直接返回内容块数组 | 破坏 schema 契约，PTC 模式拿不到结构化值 | 返回规范值，render 转内容块 |
| 不接 `exec.signal` | 取消无效、会话卡死 | 所有 IO 传 signal |
| 领域错误也抛异常 | 模型把「正常但空」当成故障 | 领域结果进规范值 |
| description 写实现细节 | 模型误用/不用 | 写场景与边界 |
| 注册后改 schema | 行为不确定 | 释放 effect 后重新注册 |
| render 里读时钟/随机数 | 回放不一致 | 纯函数；会变的数据走 presentationMeta |
| 指望 `timeoutMs` 生效 | 工具永不超时 | 它是声明；挂 timeout-policy 包装层 |
| 以为 `presentResult` 能改 Web 卡片 | UI 没变化 | Web 走 client renderer + `output.presentationMeta` |
| 把 `mode: 'ptc'` 当开关随手开 | 组合里没 `ctx.ptcRuntime` 直接失败 | 默认 `native`，确认运行时存在再开 |

## 九、练习

1. 把 `greet` 改造成生产级：加 `language` 参数（en/zh）、领域错误（空名字走规范值 `{ ok: false }`）、search/generic 卡片；
2. 给第 002 篇的定时器插件注册一个 `heartbeat_status` 工具，返回 `{ alive, lastBeatAt }`——注意 `lastBeatAt` 是「结果期事实」，思考该用 `presentationMeta` 还是直接进 schema；
3. 在 PTC 模式下调用你自己的工具，验证 `ToolArgsMap` 推导是否准确；
4. 故意删掉 `output.render` 看会发生什么（答案：`defineTool` 直接抛 `must declare output { schema, render, presentationMeta? }`）。

## 小结

- 五层契约：身份 → 参数 → 执行 → 规范值 → 渲染，另有 presentCall/presentResult（Host 本地消费方）与客户端 renderer（Web）两条呈现路径；
- description 是模型的说明书；参数优先扁平；语义校验自己补；DSL 支持 `integer/null/array/object/json/oneOf` 等完整集合；
- execute 八契约核心：只读定义、规范值、领域结果不抛异常、遵守 signal；
- `timeoutMs` 只声明不执行，`deferLoading` 不等于 tool-addition，`mode` 默认 `native`；
- **Web UI 不消费 `presentCall`/`presentResult`**；给 Web 卡片喂数据用 `output.presentationMeta`，卡片本身由 client 侧 renderer 决定；
- PTC 模式白送程序化接口：输出 schema 就是调用契约；
- 下一篇进入实战一：用全部契约写一个真正可用的本地知识库插件。

---

**系列导航**：[001 系列导读](/posts/tech/ai/agent/deepseek-harness-dev-001/) · [上一篇：004 生命周期](/posts/tech/ai/agent/deepseek-harness-dev-004/) · [下一篇：006 实战知识库插件](/posts/tech/ai/agent/deepseek-harness-dev-006/) · [原理篇参考：013 工具调用链路](/posts/tech/ai/agent/deepseek-harness-dev-013/)
