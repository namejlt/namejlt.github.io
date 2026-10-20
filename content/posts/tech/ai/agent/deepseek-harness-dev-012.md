---
title: "AI Agent-deepseek-harness插件开发012 原理篇②：设计哲学与六个核心抽象"
date: 2026-10-20T09:40:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "Cordis", "架构", "设计哲学"]
---

## 概述

读完 001–010 你能写出能跑的插件，但写不出**该这么写**的插件。差别在抽象：知道 `ctx.tools.register` 是"调用一个方法"，和知道"你正在往一个可撤销的作用域里登记一条契约"，会导出完全不同的代码。

本篇只讲设计与边界，不讲语法。六个抽象：**Context、Fiber、Service、Event、Session、ToolDefinition**。每个都回答三个问题：它解决什么、它的边界在哪、越界的典型症状是什么。

## 一、总纲：无特权内核

Cordis 内核只做四件事，全部以服务形式挂在 `ctx` 上：

```ts
export interface Context {
  events: EventsService     // 事件总线（ctx.on / ctx.emit / ctx.bail / ...）
  logger: LoggerService     // ctx.logger(name)
  reflect: ReflectService   // 服务读写与代理拦截（ctx.get / ctx.provide）
  registry: RegistryService // 插件注册（ctx.plugin / ctx.inject）
  root: this                // 根上下文
  baseUrl?: string          // 解析相对模块说明符的基准
}
```

除此之外**没有任何内置能力**：模型接入是 `dsh-llm-deepseek-api-key` 插件，文件系统是 `dsh-fs-sandbox` 插件，shell 是 `dsh-shell-*` 插件，会话是 `dsh-session` 插件，**连 Agent 主循环本身都是 `dsh-agent-loop` 插件**。

这条设计带来三个硬后果，写插件时必须认账：

1. **你不能假设任何服务存在**。连 `ctx.tools` 都可能不在——没挂 `dsh-tools` 的组合里它就不存在。所以一切跨包依赖都要 `inject` 声明（002 篇）。
2. **框架没有"特权 API"给你开后门**。想影响工具调用，只能走 `dsh-tools` 公开的流水线事件；想影响提示词，只能走 `dsh-system-prompt` 的注册接口。
3. **同一份代码能跑在不同表层**。`web`/`headless`/`acp`/`sdk`/`sdk-minimal` 是不同 bundle 组合，插件不感知差别——这是插件化最实际的收益。

## 二、Context：作用域，而不是全局对象

`Context` 是一个 **Proxy**（`ReflectService.handler`），属性读取走服务解析，写入走服务注册检查。它的核心不是"容器"，而是**作用域**：

```ts
ctx.extend(meta)              // 派生上下文：原型继承 + 覆盖若干自有属性
ctx.isolate(name, label?)     // 为某个服务名派生独立解析作用域
ctx.intercept(name, config)   // 为下层插件注入该服务的拦截配置
```

边界条件（都是实际会咬人的）：

- **`isolate` 是按服务名隔离，不是按插件隔离**。`ctx.isolate('shell')` 之后，这个子树里 `ctx.shell` 解析到另一份实现；子树外的插件完全不受影响。这是"同一进程跑两套 shell"的唯一正路。
- **`intercept` 影响的是配置解析，不是服务实例**。服务实现若声明了 `Config.merge`，拦截配置按"离根越近越先"合并；没有 `merge` 就是 `Object.assign` 浅合并（`Service[symbols.resolveConfig]`）。
- **代理读取未提供/未注入的服务会报错，不是 `undefined`**。`ctx.get('name')` 才是"可能没有"的查询姿势；`ctx.name` 是"我声明过，必须有"的姿势。二者混用是 `Cannot read properties of undefined` 之外另一类高频事故——详见 002/004 的修订段。
- **`ctx.root` 是共享根**：跨插件通信不要靠模块级单例，靠 `root` 上的服务或事件。

> 心智模型：`ctx` 不是"全局 app 对象"，而是"**我这个插件在这个作用域里能看见的世界**"。你在 `apply(ctx)` 里拿到的那份 `ctx`，是框架为你的 Fiber 特化的视图。

## 三、Fiber：一次装载的生命周期实例

一个插件被加载一次，就产生一个 Fiber。它持有：配置、依赖声明、状态机、**以及一棵 effect 树**。

```
PENDING ──(依赖就绪)──▶ LOADING ──(apply 返回)──▶ ACTIVE
   ▲                        │
   │                        └──(抛错)──▶ FAILED
   └──(依赖变化，epoch 重算)────────────────┘
ACTIVE ──(卸载/更新/HMR)──▶ UNLOADING ──(effect 全部结算)──▶ DISPOSED
```

（`FiberState` 枚举的真实顺序是 `PENDING=0, LOADING=1, ACTIVE=2, FAILED=3, DISPOSED=4, UNLOADING=5`，编号本身没有语义，别拿它做比较。）

三条容易误解的语义：

1. **"可逆注册"不是靠插件自觉，是靠 effect 所有权**。`ctx.on()`、`ctx.tools.register()`、`ctx.provide()`、`ctx.plugin()` 内部都登记成当前 Fiber 的 effect；卸载 = 逆序结算 effect 树。你自己 `new` 出来的东西不在树里，所以必须 `ctx.effect()` 手动接管。
2. **失败不是终点，但也不是自动重试**。`apply` 抛错→`FAILED`，Fiber 记下 `_error` 并把 epoch 置为 `INACTIVE`；**只有当依赖集合再次变化（服务重新提供、`update()` 改配置、HMR 重载）时才会重新尝试**。所以"我的插件报错了，改完源码却没反应"通常是你既没触发 epoch 变化、也没配 HMR `root`。
3. **子插件是父的 effect**。`ctx.plugin(child)` 返回的 Fiber 把"自己的 dispose"登记在父 Fiber 上，撤回是递归的、且父的 `dispose()` 会等所有异步处置器结算（`inertia` 循环）。

## 四、Service：能力的命名挂载

服务 = "在某个作用域里，用一个名字暴露一个对象"。它同时是三件东西：**依赖注入的键**、**能力替换的接缝（seam）**、**配置拦截的锚点**。

```ts
export default class Metrics extends Service {
  static inject = ['tools']            // 类插件也可以声明依赖
  constructor(ctx: Context) {
    super(ctx, 'metrics')              // 内部调用 ctx.reflect.provide(name, this)
  }
  record(name: string, v: number) { /* ... */ }
}
```

设计要点：

- **服务名就是 API 版本**。`ctx.tools` 上有什么方法、语义如何，是跨包契约。加方法比改语义安全得多。
- **一个作用域里同名服务只能有一个提供者**。`dsh-base` 的已知限制写得很直白：在沙箱化文件系统提供方（`fs-sandbox`）之上再挂 `fs-local`，两者抢注 `ctx.fs`，**profile 直接加载失败**。这是"服务是单例"的代价，也是可预测性的来源。
- **替换实现要用 isolate 或分层，不是覆盖注册**。想给某个子树上另一份实现，`ctx.isolate('fs')`；想给所有人换实现，换掉那一行 patch。
- **可选能力用"可空服务 + `ctx.get()`"**，不要用"定义了但可能抛错"的接口。

## 五、Event：五种调度模式，五种契约

事件不是"通知"，而是**扩展点的形状**。Cordis 给了五种分发语义，选错就是架构错：

| 模式 | 调用 | 语义 | 典型用途 |
|------|------|------|----------|
| `emit` | `ctx.emit(name, ...args)` | 同步广播，忽略返回值；**监听器抛错被逐个包裹并记日志**，不影响其他监听器 | 观测、日志、审计 |
| `parallel` | `await ctx.parallel(name, ...args)` | 并发等待全部监听器 | 并行副作用（清理、上报） |
| `bail` | `ctx.bail(name, ...args)` | 顺序执行，首个非 `null/false/undefined` 短路 | 同步否决（策略门） |
| `serial` | `await ctx.serial(name, ...args)` | 顺序 `await`，同样短路 | 异步否决、串行审批 |
| `waterfall` | `await ctx.waterfall(name, ...args, next)` | 洋葱模型，监听器包裹下游 | 可改写/可拦截的管道 |

边界条件：

- **`waterfall` 不调用 `next()` 就是"否决"，不是 bug**。这是官方定义的语义（`internal/update` 就用它 veto 配置更新）。代价是：忘了写 `await next()` 会静默掐断整条链。
- **`emit` 的容错是"每个监听器独立包裹"**，所以审计插件抛错不会拖垮工具执行——但反过来，**你的错误也可能被静默吞掉**，只能靠日志。
- **监听器也属于注册**：`ctx.on()` 的注销是 Fiber 的 effect；`ctx.once()` 一次性；`prepend`/`global` 控制顺序与作用域穿透。
- **内置事件分两类**：`tools/*`、`agent/*`、`session/event`、`session/flush` 是 Cordis 事件；`turn/*`、`step/*`、`tool/call`、`assistant/message` 是**持久化会话事件**，只能通过 `session/event` 观察。二者混淆是本系列早期最大的坑（007/008/009 已修订）。

## 六、Session：唯一事实来源

`dsh-session` 的设计一句话：**会话是仅追加（append-only）的类型化事件日志，其他一切都是投影**。

```
SessionEvent 日志（唯一真相）
   ├─ surface 投影  → 派生给模型的消息（system/developer/user/assistant/tool）
   ├─ turnOutline   → UI 的轮次大纲
   ├─ stats/title   → 统计与标题
   ├─ 持久化（JSONL）→ 崩溃恢复
   └─ 遥测（OTel）  → 反馈授权后才上报
```

由此推出几条铁律，插件作者必须遵守：

1. **模型可见 = 已落库**。任何进入模型请求的内容都必须能从日志重建；你"临时拼一段提示词直接塞给模型"的做法，在 replay、fork、压缩之后就会失真。
2. **surface 事件必须带 `surfaceOp`**（`append` 或 `replace{startSeq,endSeq}`）。改写历史不是 `splice`，是**带序号的替换语义**。
3. **顺序即身份**：`SessionSeq` 是包含边界的序号；`replace`/`fork` 都按它定位。
4. **不许"补记录"**：如果进程在结算前硬崩，就不存在半条 assistant stream——这是设计，不是缺陷。

## 七、ToolDefinition：模型面与程序面的分离

一个工具同时面对两类消费者，这是 DSH 工具契约的核心洞察：

| 面 | 谁看 | 看什么 |
|----|------|--------|
| 模型面 | 模型 | `name`、`description`、`parameters` schema（注册表自动送进提示词组装） |
| 程序面 | 调用方（PTC 程序、subagent、工作流） | `output.schema` 声明的**唯一规范 JSON 值** |
| 渲染面 | UI / 宿主展示 | `render()` 内容块；Web 侧另由客户端渲染器（`tool.call.toolview`）决定卡片 |
| 策略面 | 插件 | `tools/pre-execute` → guards → `tools/execute` → `tools/post-execute` → `finalizeContent` → `tools/result` |

关键判断：**"给模型看的文本"和"给程序读的值"必须分开**。所以 `execute` 返回规范值、`render` 负责转成内容块；所以替换/脱敏内容**不会**改变程序侧拿到的值（想真禁止，只能阻止执行或替换值本身）。这条在 005 篇展开，这里只记住一句：**任何"从自然语言里抠 id"的设计都是错的**。

## 八、失败哲学：三档音量

| 场景 | 音量 | 机制 |
|------|------|------|
| 配置非法、required entry 加载失败 | **最大声**：整个应用拆掉、非零退出 | `ValidationError`（`invalid config:` + 逐条 issue）、`installFailLoud` |
| 单个插件激活失败（非 required） | 中：带标签警告，其余继续 | 启动保留所有能激活的插件 |
| 事件监听器抛错 | 最小：记日志，继续分发 | `emit` 逐监听器包裹；`tools/result` 观察者失败只 warn |
| 工具业务上"不理想但成功" | 零噪音：写进规范值 | `{ ok:false }` 而非 `throw` |
| 取消 | 协作式 | `exec.signal` + `ABORTED*` 结果码 |

判断口径只有一句：**"这是程序错误，还是业务结果？"** 前者抛、后者进值。搞反的后果很具体：抛异常会让整轮失败（并给模型一条 `isError`），而业务结果进值让模型能继续推理。

## 九、越界的四种典型症状

1. **模块顶层存状态** → HMR 后出现"幽灵实例"（旧模块的状态还活着，新模块看不到）。状态必须挂在 Fiber 上（闭包在 `apply` 内）或服务里。
2. **`apply` 里做重活**（网络、大文件、`await` 长任务）→ 启动变慢、失败面变大，且 `apply` 抛错就是 `FAILED`。重活放 `ctx.effect()` 或工具 `execute`。
3. **自己 `new` 资源但不 `ctx.effect()`** → 卸载残留（定时器、watcher、子进程）。
4. **绕过 `ctx.fs` 直接 `node:fs` 写文件** → 沙箱策略被绕过；`dsh-base` 明确要求"保持沙箱化文件系统提供方是唯一写入路径"。

## 小结

- 内核只有四个服务，**一切能力皆插件**（含 Agent 循环）——所以依赖必须 `inject`，且不假设服务存在；
- `Context` 是作用域代理：`extend`/`isolate`/`intercept` 三个动词，隔离按服务名；
- `Fiber` 是生命周期 + effect 树：可逆注册靠所有权，失败需 epoch 变化才重试；
- `Service` 是命名能力：同名唯一、按作用域隔离、可 intercept 配置；
- `Event` 五种模式各有契约，`waterfall` 不 `next()` = 否决；
- `Session` 是仅追加日志，模型可见即已记录；
- `ToolDefinition` 强制分离模型面/程序面/渲染面；
- 失败分三档音量，判断口径是"程序错误 vs 业务结果"；
- 下一篇把这些抽象装进时间轴：一次用户输入到底走了哪些步。

---

**系列导航**：[001 系列导读](/posts/tech/ai/agent/deepseek-harness-dev-001/) · [上一篇：011 项目结构](/posts/tech/ai/agent/deepseek-harness-dev-011/) · [下一篇：013 运行原理](/posts/tech/ai/agent/deepseek-harness-dev-013/)
