---
title: "AI Agent-deepseek-harness插件开发004 生命周期、effect与Fiber状态机"
date: 2026-10-19T10:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "生命周期", "Fiber", "HMR"]
---

## 概述

插件化框架的胜负手是**卸载是否干净**：热替换、服务消失、配置变更都会触发卸载，残留一个定时器、一条监听器，就可能在几小时后的深夜变成离奇 bug。本篇讲透 Harness 的生命周期模型：Fiber 状态机、依赖驱动加载、自动清理、手动 `ctx.effect`、子 Fiber 与 dispose 语义。读完你能写出「卸载无残留」的插件，并建立自查清单。

## 一、Fiber 状态机

每个被加载的插件拥有一个 Fiber 作用域：

```
PENDING ──▶ LOADING ──▶ ACTIVE
   │            │
   │            ▼
   │         FAILED          （apply 抛异常）
   ▼
ACTIVE ──▶ UNLOADING ──▶ DISPOSED
```

| 状态 | 含义 | 你要做什么 |
|------|------|------------|
| PENDING | 已声明，依赖未就绪 | 什么都别做，等着 |
| LOADING | 依赖就绪，正在执行 `apply` | 同步初始化放这里 |
| ACTIVE | 运行中 | 正常服务 |
| FAILED | `apply` 抛异常 | 看日志修 bug；框架**不会自动重试**，只在依赖 epoch 变化时再试（见 7.1） |
| UNLOADING | 正在卸载、释放资源 | effect 处置器在这里跑 |
| DISPOSED | 完全卸载 | 终态 |

> 枚举真实取值：`PENDING=0, LOADING=1, ACTIVE=2, FAILED=3, DISPOSED=4, UNLOADING=5`。编号**没有**语义，别拿它做大小比较或"进度"判断。

状态转换的两个驱动源：

1. **依赖**：`inject` 的服务就绪 → 加载；服务消失 → 卸载，恢复后重载；
2. **配置**：`cordis.yml` 改动（HMR）→ 卸载旧实例、加载新实例（volatile 字段例外，见第 003 篇）。

## 二、依赖驱动加载

```ts
export const inject = ['tools', 'llm']

export function apply(ctx: Context) {
  // 框架保证 ctx.tools 与 ctx.llm 在此就绪
}
```

- 依赖未就绪：插件停在 PENDING，**静默等待，不报错**；
- 依赖消失（提供方被替换）：插件自动 dispose，服务恢复后自动重新加载；
- 这条兜底意味着：**你不需要写「服务是否还存在」的防御代码**，但要保证 `apply` 里拿到的引用在插件存活期内有效。

可选依赖用 `ctx.get('name')` 在使用点查询，服务不存在时得到 `undefined`，自行降级。

## 三、自动清理：什么被框架追踪

通过 `ctx` 做的注册，卸载时自动撤销：

| 注册 | 写法 | 卸载时 |
|------|------|--------|
| 事件监听 | `ctx.on('event', handler)` | 监听器移除 |
| 工具 | `ctx.tools.register(tool)` | 工具注销 |
| LLM 适配器 | `ctx.llm.registerAdapter(names, adapter)` | 适配器注销 |
| 自定义资源 | `ctx.effect(() => cleanup)` | cleanup 执行 |

**注册即清理**，不需要手动 `removeListener` / `clearInterval`。这是 Harness 插件可以「随便热替换」的根基。

### 3.1 反模式警示

```ts
// ❌ 裸 setInterval：卸载后仍在跑，HMR 后变成双份
export function apply(ctx: Context) {
  setInterval(() => console.log('heartbeat'), 5000)
}
```

## 四、ctx.effect：手动清理的通道

框架追踪不到的资源（文件句柄、网络连接、子进程），用 `ctx.effect()` 声明清理方式：

```ts
import type { Context } from '@deepseek-ai/cordis'

export function apply(ctx: Context) {
  ctx.effect(() => {
    const timer = setInterval(() => {
      console.log('heartbeat')
    }, 5000)

    // 返回的函数在插件卸载时执行
    return () => clearInterval(timer)
  })
}
```

典型用途：

- 打开的文件/数据库连接；
- `child_process` 启动的子进程（`kill` 掉）；
- 向外部服务注册的 webhook（注销）；
- 写入的临时目录（删除）。

### 4.1 异步清理

effect 处置器可以返回 Promise，框架会等待：

```ts
ctx.effect(async () => {
  const db = await openDatabase()
  return async () => {
    await db.flush()
    await db.close()
  }
})
```

但要注意下面的顺序语义。

## 五、子 Fiber 与 dispose 语义

`ctx.plugin()` 创建子 Fiber：继承父上下文，但生命周期独立，随父卸载：

```ts
export function apply(ctx: Context) {
  ctx.plugin(childPlugin)   // 子插件随本插件一起卸载
  ctx.plugin(otherPlugin, { config: { ... } })
}
```

手动提前终止：

```ts
const fiber = ctx.plugin(myPlugin)
await fiber.dispose()
```

`dispose` 保证：

1. 该插件拥有的所有注册被移除；
2. 子插件被**递归**卸载；
3. 返回的 Promise 在所有异步清理完成后兑现。

### 5.1 顺序语义（重要）

**跨 effect** 的处置器**并发执行**（框架对它们做一次 `Promise.all`），不保证逐个完成；**同一 effect** 里返回的多个处置器则按序 `await`。精确语义见 7.2。

```ts
// ❌ 有顺序依赖的清理拆成两个 effect：并发执行，可能撞车
ctx.effect(() => { db = open(); return () => db.close() })
ctx.effect(() => { db.query('...'); return () => db.rollback() })

// ✅ 有顺序依赖的清理放进同一个 effect，由处置器串行等待
ctx.effect(async () => {
  const conn = await connect()
  return async () => {
    await conn.rollback()   // 先回滚
    await conn.close()      // 后关闭
  }
})
```

规则：**有顺序依赖的清理步骤必须放进同一个 `ctx.effect()` 返回的处置器**。

## 六、实战：带定时任务与缓存的插件骨架

把本篇知识组装成一个「每天刷新远程配置」的骨架，第 006/007 篇的实战插件会用到同样的组织方式：

```ts
import type { Context } from '@deepseek-ai/cordis'

export const name = 'refresher'
export const inject = ['tools']

interface Cache { value: unknown; expiresAt: number }

export function apply(ctx: Context, config: Config) {
  let cache: Cache | null = null

  // 缓存查询：同步、无副作用
  function get(key: string) { /* ... */ }

  // 1) 事件监听：自动清理
  ctx.on('tools/result', (exec, result) => {
    console.log(`[refresher] observed ${exec.name}`)
  })

  // 2) 定时刷新：effect 手动清理
  ctx.effect(() => {
    const timer = setInterval(async () => {
      cache = await fetchRemote()
    }, config.refreshMs)
    return () => clearInterval(timer)
  })

  // 3) 注册工具：自动注销
  ctx.tools.register(defineTool({ /* ... */ }))
}
```

自查这个骨架：卸载后定时器停了吗？（停了，effect）监听器撤了吗？（撤了，ctx.on）工具没了吗？（没了，register）——全部可逆。

## 七、排错：FAILED 与泄漏检查

### 7.1 FAILED 态排查

`apply` 抛异常 → FAILED，插件不服务。常见原因：

| 原因 | 排查 |
|------|------|
| 同步初始化抛错 | 把可能失败的逻辑移到 effect 或工具 execute 内；`apply` 里只做轻量同步初始化 |
| 访问了未 inject 的服务 | 检查 `ctx.xxx` 是否有对应 `inject`（报错原文是 `cannot get property "xxx" without inject`） |
| Config 校验失败 | 见第 003 篇（`ValidationError`，`invalid config:` 开头） |
| 模块加载错误（语法/导入） | 看启动日志堆栈；此时根本没进 `apply` |

**"不会自动重试"要精确到条件**：`apply` 抛错后 Fiber 停在 FAILED 并把 epoch 置为 INACTIVE；**只有当依赖集合再次变化**——某个服务重新提供、`fiber.update()` 改配置、HMR 重组——它才会重新走 `LOADING`。所以"我修好了代码却还在 FAILED"的两种真实原因：

- 改的是源码，但 HMR 没监听源码（`root: []`）；
- 改的是配置，但改动没触发该行更新（比如改的是别的行）。

设计准则：**`apply` 保持「薄」**——同步、轻量、不抛。重活放 effect / execute。

### 7.2 处置器的精确语义

两句容易记混的话，这里说清楚：

- **不同 effect 之间：并发**。卸载时框架对所有 effect 的处置器做一次 `Promise.all`，不保证先后；
- **同一个 effect 返回多个处置器时：串行**，按返回顺序依次 `await`。

因此"有顺序依赖的清理"要么放进**同一个**处置器的函数体里（`await a(); await b()`），要么放进同一个 effect 返回的处置器数组。拆到两个 effect 里就是并发，撞车概率随负载上升。

### 7.3 泄漏检查清单

- [ ] 所有 `setInterval` / `setTimeout` 长任务都在 `ctx.effect` 里；
- [ ] 所有 `process.on`（如 `SIGINT`）都已移除或包在 effect；
- [ ] 所有子进程在卸载时 kill；
- [ ] 没有在模块顶层（`apply` 外）持有可变单例状态——热替换会得到新模块，旧模块的状态成幽灵；
- [ ] HMR 连续改 5 次文件，观察日志中卸载/加载是否严格配对。

## 八、练习

1. 写一个插件：`apply` 里注册一个每秒打印的定时器（用 effect），HMR 触发三次，确认每秒只有一行日志；
2. 同一个插件里再 `ctx.plugin()` 挂一个子插件，`dispose()` 父 Fiber，验证子插件也被卸载；
3. 在 `apply` 里 `throw new Error('x')`，观察 FAILED 与启动日志；把 throw 移进 `ctx.effect` 的回调再观察差异。

## 小结

- Fiber 六态：PENDING → LOADING → ACTIVE → UNLOADING → DISPOSED，异常落 FAILED（编号无语义）；
- `inject` 依赖驱动：未就绪等待（静默），消失自卸载、恢复自重载；
- FAILED **不自动重试**，只在依赖 epoch 变化（服务重提供 / `update()` / HMR）时再试；
- ctx 注册全部自动清理：`ctx.on`、`ctx.tools.register`、`ctx.llm.registerAdapter`、`ctx.effect`；
- 处置器：跨 effect 并发（`Promise.all`）、同 effect 内按序 `await`——有顺序依赖的清理放同一个处置器里；
- `apply` 保持薄；重活外移；用清单防泄漏；
- 下一篇深入工具：模型真正能调用的能力长什么样。

---

**系列导航**：[001 系列导读](/posts/tech/ai/agent/deepseek-harness-dev-001/) · [上一篇：003 插件配置](/posts/tech/ai/agent/deepseek-harness-dev-003/) · [下一篇：005 工具契约](/posts/tech/ai/agent/deepseek-harness-dev-005/) · [原理篇参考：012 设计哲学](/posts/tech/ai/agent/deepseek-harness-dev-012/)
