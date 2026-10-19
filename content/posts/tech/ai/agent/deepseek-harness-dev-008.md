---
title: "AI Agent-deepseek-harness插件开发008 事件系统、服务与三层能力拆分"
date: 2026-10-19T12:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "事件系统", "服务", "架构"]
---

## 概述

前两篇的实战插件是「自给自足」形态：自己存储、自己检索。但插件生态真正的威力在**协作**：你的插件可以监听别人的事件、消费别人的服务、也可以把自己的能力提供给别人。本篇讲透三件事：事件系统四种调度模式、服务的提供与消费、以及官方推荐的「三层能力拆分」架构。学完后你的插件可以从「孤岛」变成「生态位」。

## 一、事件系统：四种调度模式

事件是 Cordis 插件间通信的核心机制，Harness 大量使用事件实现松耦合扩展点。

### 1.1 基本 API

```ts
ctx.on('event-name', handler)   // 监听（随插件卸载自动移除）
ctx.emit('event-name', payload) // 广播
```

### 1.2 四种模式

| 模式 | 调用 | 执行方式 | 返回值语义 |
|------|------|----------|------------|
| emit | `ctx.emit(name, payload)` | 所有监听器**同步**执行 | 忽略返回值 |
| bail | `ctx.bail(name, input)` | 按序执行 | 第一个非 `null/false/undefined` 的返回值成为最终结果（短路） |
| serial | `await ctx.serial(name, input)` | 按注册序**依次 await** | 同上（异步版 bail） |
| waterfall | `await ctx.waterfall(name, input, initial)` | 流水线 | 每个监听器包装下游返回值，**必须调 `next()`** |

```ts
// bail：拦截式决策
const verdict = ctx.bail('tool-check', { command: 'rm -rf /' })
ctx.on('tool-check', (input) => {
  if (isDangerous(input.command)) return 'blocked'   // 短路，后续监听器不再执行
  return null                                        // 继续下一个
})

// serial：异步顺序执行
await ctx.serial('setup-phase', context)

// waterfall：管道变换
const output = await ctx.waterfall('my-plugin/transform', input, async () => input)
ctx.on('my-plugin/transform', async (_input, next) => {
  const downstream = await next()      // 必须调用！
  return downstream.trim()
})
```

> waterfall 的头号坑：**不调用 `next()` 会短路整条流水线**。这是故意设计（用于拦截/网关），但忘了写就是线上故障。所有 waterfall 监听器先写 `const downstream = await next()` 再写逻辑，能规避 99% 的事故。

### 1.3 类型安全的事件

用声明合并给自定义事件加类型：

```ts
import '@deepseek-ai/cordis'

declare module '@deepseek-ai/cordis' {
  interface Events {
    'my-plugin/ready': (payload: { id: string }) => void
    'my-plugin/check': (input: string) => boolean | undefined
    'my-plugin/transform': (input: string, next: () => Promise<string>) => Promise<string>
  }
}
```

之后 `ctx.on('my-plugin/ready', ...)` 与 `ctx.emit('my-plugin/ready', ...)` 全部获得类型推导。

### 1.4 内置事件与「同名陷阱」

官方文档给出的内置事件名遵循 `namespace/action` 约定：

```
agent/pre-step          agent/request         agent/request-error
tools/result            session/event
```

**关键陷阱**：`turn/*`、`step/*`、`tool/call`、`tool/result`、`compaction/*` 是**持久化的会话事件类型，不是同名 Cordis 事件**。要观察它们，监听 `session/event` 并检查 `event.type`（第 007 篇的被动捕获正是这么做的）。

```ts
// ✅ 正确：观察会话内持久化事件
ctx.on('session/event', (event) => {
  if (event.type === 'tool/result') { /* ... */ }
})

// ❌ 错误：tool/result 不是 Cordis 事件
ctx.on('tool/result', handler)   // 永远不会触发
```

### 1.5 实战：一个日志插件（官方示例拆解）

```ts
import type { Context } from '@deepseek-ai/cordis'
import '@deepseek-ai/dsh-tools'

export const name = 'tool-logger'

export function apply(ctx: Context) {
  ctx.on('tools/result', (exec, result) => {
    const text = result.content
      .map((block) => (block.type === 'text' ? block.text : ''))
      .join('')
    console.log(`[tool] ${exec.name}(${JSON.stringify(exec.arguments)})`)
    console.log(`[tool result] ${text.slice(0, 100)}`)
  })
}
```

这个 15 行插件展示了事件模式的标准用法：监听 → 提取 → 输出。注意监听器随插件卸载自动移除，不需要手动清理。

## 二、服务：能力的命名挂载

### 2.1 什么是服务

`tools`、`llm`、`agents` 都是服务——挂载在 `ctx` 上的命名能力。**任何插件都可以提供服务给其他插件用**。

### 2.2 提供服务（Service 基类）

```ts
import { Service, type Context } from '@deepseek-ai/cordis'

export default class MetricsService extends Service {
  static inject = ['llm']        // 服务也可以依赖其他服务

  constructor(ctx: Context) {
    super(ctx, 'metrics')        // 'metrics' 即服务名
  }

  record(event: string, value: number) { /* ... */ }
}
```

配套类型声明让 `ctx.metrics` 有正确类型：

```ts
declare module '@deepseek-ai/cordis' {
  interface Context {
    metrics: MetricsService
  }
}
```

### 2.3 消费服务

```ts
// 必需依赖：服务缺失时插件不加载
export const inject = ['metrics']
export function apply(ctx: Context) {
  ctx.metrics.record('plugin_loaded', 1)
}

// 可选依赖：使用点查询，缺失时降级
export function apply(ctx: Context) {
  ctx.get('metrics')?.record('plugin_loaded', 1)
}
```

### 2.4 服务消失的行为

提供方被卸载/替换时：依赖它的插件**自动 dispose**，服务恢复后**自动重新加载**。你不需要写「服务是否还存在」的防御代码——但这也意味着：**不要在闭包里长期缓存服务引用后跨「服务消失窗口期」使用**，需要用时从 `ctx` 现取。

## 三、三层能力拆分：Service Definition / Provider / Consumer

### 3.1 什么时候拆

当一项能力**需要支持可替换的提供方**时（Bash 执行、模型接入、存储后端……），官方建议拆成三个角色。判断标准：

- 会有多个实现吗？（本地 bash / 容器 bash / 远程 ssh）
- 实现方与调用方需要独立演进吗？
- 想不想让用户用一行 yaml 换实现？

三个「是」→ 拆；否则**不要预防性拆分**，一个包全包了更简单。

### 3.2 以 Bash 为范本

```
┌─────────────┐     ┌──────────────────┐     ┌──────────────┐
│  Definition │────▶│    Provider      │     │   Consumer   │
│  dsh-shell  │     │  dsh-bash-local  │     │ dsh-tool-bash│
└─────────────┘     └──────────────────┘     └──────────────┘
```

- Definition 拥有 Request/Result 类型；
- Provider 依赖 Definition（不依赖 Consumer）；
- Consumer 依赖 Definition（不依赖 Provider）；
- 用户在 `cordis.yml` 里选择 Provider，Definition 与工具不变。

### 3.3 完整实战：网页抓取 seam

我们来拆一个「网页抓取」能力。对标社区 `liustack/modsearch`（联网搜索桥）与 `browser-automation` 的接缝设计。

**第一步：Service Definition（`dsh-fetch`）**

```ts
// packages/fetch/dsh-fetch/src/index.ts
import { Service, type Context } from '@deepseek-ai/cordis'

declare module '@deepseek-ai/cordis' {
  interface Context {
    fetch: FetchService
  }
}

export abstract class FetchService extends Service {
  constructor(ctx: Context) {
    super(ctx, 'fetch')
  }

  /** 抓取网页并抽取正文 */
  abstract fetch(request: FetchRequest): Promise<FetchResult>
}

export interface FetchRequest {
  url: string
  maxChars?: number
  /** 取消信号，由调用方（工具的 exec.signal）透传 */
  signal?: AbortSignal
}

export interface FetchResult {
  url: string
  status: number
  title?: string
  text: string
}
```

**第二步：Service Provider（`dsh-fetch-local`，node 原生实现）**

```ts
// packages/fetch/dsh-fetch-local/src/index.ts
import type { Context } from '@deepseek-ai/cordis'
import { FetchService, type FetchRequest, type FetchResult } from 'dsh-fetch'

class FetchLocal extends FetchService {
  async fetch(request: FetchRequest): Promise<FetchResult> {
    const resp = await fetch(request.url, { signal: request.signal })
    const html = await resp.text()
    return {
      url: request.url,
      status: resp.status,
      title: /<title[^>]*>([^<]*)<\/title>/i.exec(html)?.[1]?.trim(),
      text: htmlToText(html).slice(0, request.maxChars ?? 20000),
    }
  }
}

export const name = 'dsh-fetch-local'

export function apply(ctx: Context) {
  ctx.plugin(FetchLocal)
}

function htmlToText(html: string): string {
  return html
    .replace(/<script[\s\S]*?<\/script>/gi, '')
    .replace(/<style[\s\S]*?<\/style>/gi, '')
    .replace(/<[^>]+>/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}
```

**第三步：Consumer（`dsh-tool-fetch`，暴露为模型工具）**

```ts
// packages/fetch/dsh-tool-fetch/src/index.ts
import type { Context } from '@deepseek-ai/cordis'
import { defineTool } from '@deepseek-ai/dsh-tools'

export const name = 'dsh-tool-fetch'
export const inject = ['tools', 'fetch']

export function apply(ctx: Context) {
  ctx.tools.register(
    defineTool({
      name: 'web_fetch',
      description: '抓取一个网页的正文文本。适用：阅读文章、文档、公告。不适用：需要交互的页面。',
      parameters: {
        url: { type: 'string', required: true, description: '目标网址' },
        maxChars: { type: 'number', description: '最多返回字符数，默认 20000' },
      },
      output: {
        schema: {
          type: 'object',
          properties: {
            url: { type: 'string' },
            status: { type: 'number' },
            title: { type: 'string' },
            text: { type: 'string' },
          },
        },
        render: (_args, value) => [
          { type: 'text', text: `${value.title ?? value.url} (${value.status})\n${value.text}` },
        ],
      },
      async execute(args, exec) {
        return ctx.fetch.fetch({ url: args.url, maxChars: args.maxChars, signal: exec.signal })
      },
    }),
  )
}
```

**组合（cordis.yml）**

```yaml
- name: 'dsh-fetch-local'   # 换成 dsh-fetch-remote 即可切换实现
- name: 'dsh-tool-fetch'
```

### 3.4 设计要点

- **显式优于隐式**：默认值通过显式的 `resolve(request): Spec` 步骤处理，不要藏在 `run()` 里的 `?? default`；
- Definition 一旦被消费方依赖就**很少改动**（它是契约）；
- 换 Provider = 改一行 yaml，Definition 和工具零改动——这就是拆分红利。

## 四、服务隔离：同一个服务，多个实例

不同插件组可能需要同一服务的不同配置。用 group 隔离：

```yaml
- id: group-a
  name: '@deepseek-ai/cordis-plugin-group'
  group: true
  isolate:
    shell: true
  config:
    - name: '@deepseek-ai/dsh-bash-local'
      config:
        timeoutMs: 5000
    - name: './src/plugin-a.ts'

- id: group-b
  name: '@deepseek-ai/cordis-plugin-group'
  group: true
  isolate:
    shell: true
  config:
    - name: '@deepseek-ai/dsh-bash-local'
      config:
        timeoutMs: 60000
    - name: './src/plugin-b.ts'
```

`plugin-a` 与 `plugin-b` 各自看到自己组内的 Bash 实例，互不影响。**典型用途**：给「快速检查」和「长时间构建」配不同的 shell 超时。

## 五、把实战插件接进事件/服务生态

前两篇的插件可以这样融入生态：

| 插件 | 融入方式 |
|------|----------|
| kb-lite | 提供 `kb` 服务（检索 API），让其他插件复用；消费 `llm` 服务做查询扩展 |
| memory-lite | 监听 `session/event` 已做；再把「记忆写入」暴露为服务，供工作流插件调用 |
| 两者都可 | 监听 `tools/result` 做审计（第 009 篇） |

## 六、常见坑

| 坑 | 后果 | 对策 |
|----|------|------|
| waterfall 不调 `next()` | 流水线静默中断 | 监听器第一行先 `await next()` |
| 监听 `tool/result` 期待触发 | 永远不触发 | 那是会话事件类型，监听 `session/event` 查 `event.type` |
| 缓存服务引用跨窗口期使用 | 调到已卸载服务 | 用时从 `ctx` 现取；依赖框架的自动重载 |
| 预防性拆分 | 三个包啃不动，契约还老变 | 先单包，出现第二个实现再拆 |
| 事件名不用命名空间 | 与内置事件撞车 | `my-plugin/action` 约定 |

## 七、练习

1. 给 memory-lite 的「记忆写入」抽一个 `MemoryWriter` 服务，并写一个消费方插件在会话结束时自动写入摘要；
2. 用 waterfall 实现一个「提示词美化」管道：三个插件依次给用户消息加前后缀，其中一个故意不调 `next()`，观察短路效果；
3. 把 kb-lite 改造成 Definition + Provider + Consumer 三层，并写第二个 Provider（从 SQLite FTS5 读取）；
4. 用 group 隔离给 kb-lite 配两套 storageDir，验证两个实例互不干扰。

## 小结

- 事件四模式：emit / bail / serial / waterfall；waterfall 必须 `next()`；
- 自定义事件用 `declare module Events` 加类型；注意会话事件（`session/event`）与 Cordis 事件的区别；
- 服务 = ctx 上的命名能力：`Service` 基类提供，`inject` 必需消费，`ctx.get()` 可选消费；
- 三层拆分（Definition/Provider/Consumer）用于可替换能力，**不预防性拆分**；
- 下一篇讲安全：策略钩子、审计与后台任务。

---

**系列导航**：[001 系列导读](./deepseek-harness-dev-001.md) · [上一篇：007 实战记忆插件](./deepseek-harness-dev-007.md) · [下一篇：009 安全与后台任务](./deepseek-harness-dev-009.md)
