---
title: "AI Agent-deepseek-harness插件开发015 原理篇⑤：双端插件——server、client 与动态包"
date: 2026-10-20T10:35:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "客户端插件", "HMR", "架构"]
---

## 概述

前十篇讲的都是**宿主侧（server）插件**：跑在 Node 进程里，能注册工具、服务、事件。但只要你想让 Web UI 里有东西——一个面板、一张卡片、一条侧栏——就进入另一个世界：**浏览器侧（client）插件**。它有独立的 manifest、独立的加载器、独立的 HMR、独立的版本身份，而且**宿主侧 import 不到它**。

本篇讲清三件事：两端插件的关系、client 插件从声明到渲染的完整链路、以及"动态包"（模型/用户运行时装载的插件）这一 DSH 特有的第四形态。

## 一、为什么必须分成两端

```
┌─────────────────────────── 宿主进程（Node） ───────────────────────────┐
│  dsh-base 层：llm / session / tools / agent-loop / jobs / shell / fs    │
│  dsh-web-app 层：                                                       │
│    webserver ── web-runtime ── modules(宿主半) ── connection ── …      │
│                                     │                                   │
│                          扫描 Loader 树 → 组合启动图                     │
│                          提供 /plugins/<id>/client.js                   │
└─────────────────────────────────────┼───────────────────────────────────┘
                                      │  启动图（window.__DSH_BOOT__）
                                      ▼
┌─────────────────────────── 浏览器（页面） ──────────────────────────────┐
│  壳 kernel：构造冻结的共享模块表（PLATFORM_MODULES）                    │
│  modules(浏览器半)：按需惰性加载插件 bundle                              │
│  ui-layout / ui-session / ui-chat / ui-sidebar / ui-tool / …            │
└─────────────────────────────────────────────────────────────────────────┘
```

分端的**物理原因**：宿主是 Node（有 `fs`、子进程、PTY），浏览器只有 DOM 与 fetch。**协议原因**：浏览器侧的模块图必须由宿主侧组合，否则前端不知道该加载什么、顺序如何、共享依赖怎么对齐。**安全原因**：`window.__DSH_BOOT__` 是注入到 HTML 的启动图，插件提供的字符串必须被转义（`<` 已转义），否则就是 XSS。

一条硬约束先记住：**宿主侧的 bundle 不能 import client 包**（`dsh-web-app/cordis.patch.yml` 里的注释原话：client-hmr 之所以是一行独立 row 而不是 web-runtime 的子节点，就因为它的 node 半是客户端包，宿主侧 bundle import 不到）。所以双面包必须是**两个入口文件**，用 `exports` 分流。

## 二、client 插件的声明：`dsh.client`

```json
{
  "name": "dsh-my-panel",
  "version": "0.1.0",
  "type": "module",
  "exports": {
    ".":        { "types": "./lib/types/index.d.ts", "default": "./lib/index.js" },
    "./client": { "types": "./lib/types/client/index.d.ts", "default": "./lib/client.js" }
  },
  "dsh": {
    "client": {
      "platform": "web",
      "inject": ["@deepseek-ai/dsh-client-ui-layout"],
      "external": ["@deepseek-ai/dsh-client-ui-primitives"]
    }
  },
  "peerDependencies": { "@deepseek-ai/cordis": "~4.0.5" }
}
```

四个字段的准确含义：

| 字段 | 含义 | 易错点 |
|------|------|--------|
| `platform: 'web'` | 显式声明这是浏览器侧插件 | 缺失时宿主侧不会把它编进启动图 |
| `exports['./client']` | 浏览器 bundle 的入口，必须是**构建产物** | 指向 `.ts` 会在激活阶段失败 |
| `dsh.client.inject` | 依赖哪些插件的**模块 row**（不是服务名） | 与宿主侧的 `inject`（服务名）语义不同，别混 |
| `dsh.client.external` | 基座之外需要解析的模块请求，逐条精确匹配 | 只写"基座里没有的"；写基座已有的会被组合阶段拒绝 |

`external` 的解析规则值得单独记：**外壳初始化一张冻结的模块表**（`PLATFORM_MODULES`），每个动态 bundle 的 external 都精确针对这张表解析；`external` 只添加基座之外的**精确请求**，纯类型 import 会被擦除、不产生请求。组合阶段会拒绝：畸形请求、缺失提供方、自请求、同步请求环。

## 三、从声明到页面上的一个组件

```
① 宿主侧 dsh-client-modules（node 半）
     扫描已启用的 Loader entry → 读每个包的 dsh.client → 归一化成启动图
② 启动图注入 HTML：window.__DSH_BOOT__（WebBootEntry 行，字符串已转义）
③ 壳 kernel 先于 cordis 存在：建立共享模块表 + 注册 factory
④ 浏览器侧按需加载：combo URL（可批量）→ 每个插件 bundle 只"注册 factory"
⑤ 首次被 import / 物化时才执行模块体（副作用、CSS 注入都在这一步）
⑥ 插件在浏览器侧拿到自己的 ctx：注册槽位/渲染器/命令，渲染进布局
```

第 ④⑤ 步是"惰性 CJS 模型"，它带来两个可观察行为：

- **未使用的插件不会执行任何模块体副作用**（包括 CSS 注入）——所以别在模块顶层做"注册即生效"的事；
- **factory 之间不能有 require 环**：factory 形式的 CJS 无法提供部分导出，环会直接抛错。

第 ⑥ 步落在三个具体的面上（读 `dsh-client-ui-renderer/lib/client.js` 与 `dsh-client-ui-tool/README.zh.md` 可逐条对上）：

- **`ctx.uiRenderer` 只有一个方法**：`mount(container)` —— 挂载组装好的应用、返回卸载 disposer。它由 Web 外壳在启动内核稳定后调用，业务插件**不直接调用**。
- **`ctx.slots.inject(key, callback)`**：等槽位被声明后再执行注册；返回幂等 disposer（插件卸载自动撤销）。槽位未声明时回调不执行，声明被撤回时回调的 effect 也被回收。
- **`ctx.slots.register({ name, key, store? }, Component)`**：注册一个 entry，返回 disposer。`name` 是槽位键，`key` 是该槽位内的分发键（工具卡片就是 wire 工具名）。
- `ctx.slots.renderSlot(key, ownerProps)` 是渲染器内部出口，业务插件一般用不到。

工具卡片的注册长这样（`dsh-client-ui-tool` 的 README 原文）：

```text
ctx.slots.inject('tool.call.toolview', () =>
  ctx.slots.register({ name: 'tool.call.toolview', key: '<wire tool name>' }, BusinessToolRow))
```

owner props 是 `ToolCallOwnerProps`：`callId`、`toolName`、`phase`（preparing / start / result 判别）、对应阶段的 `block`、可选 `cwd` / `home`、会话授权的 `loadImage`，以及 `openFile` / `inspect` 回调。注册项拿不到 React 节点或运行时服务。

### 3.1 工具卡片的真相

这一条必须单独说，因为它推翻了本系列早期的一个错误印象：

> **Web UI 里的工具卡片不是由 `presentCall` / `presentResult` 渲染的。** 内置 Web 客户端**不消费**这两个值；它通过 `tool.call.toolview` 选择 renderer，并从**原始调用参数、结果内容、失败状态、持久 metadata** 派生 card props。`presentCall`/`presentResult` 是给 **Host 本地消费方**（桌面壳、CLI 呈现、第三方宿主）用的。

所以：想让工具在 Web 里有好看的卡片 → 写 client 侧 renderer（`dsh-client-ui-tool` 的 keyed per-tool presentation）；想让宿主侧/其他消费方有结构化呈现 → 用 `presentCall`/`presentResult`。二者不能互相替代。

## 四、构建要求：没有 `lib/client.js` 就没有插件

`dsh-client-modules` 的限制写得很硬：宿主提供的是**已构建**的客户端 bundle，启动前 `pnpm run build` 必须产出每个 `lib/client.js`；缺失 bundle 会**明确导致激活失败**，并列出包名与路径。

由此产生三个日常事实：

1. **改 client 代码必须重新构建**——它不是"改了就生效"的 TS 源码；
2. **源码启动（checkout 里跑）只映射宿主侧 import 到 TS 源码，客户端仍消费构建产物**；
3. **client 侧的热更新是另一条链**：`dsh-client-hmr` 监听重建，浏览器侧把变化的 row 切到带 revision 的单资源 combo URL。没有 `pnpm run dev:web` 在跑，就不会有自动更新。

### 4.1 release 身份：为什么不许跨版本混跑

每个启动图携带 `release = { version, commit }`（本包版本 + 客户端构建固化进宿主产物的源码 commit）。页面只应用**与自身启动 release 相同**的图与 rebuilt 帧。宿主换了 release 时：

- 页面**不应用**它的任何内容，当前插件继续跑；
- 通过 `ctx.modules.entries.releaseChange` 发布三方信息（页面启动的 release、宿主上一个 release、当前 release）；
- **只有刷新页面**才会加载另一个 release；在插件页"重试失败的插件"也不会跨 release 加载（会以下载失败结束）。

这条设计的动机很实际：混跑两个 release 的模块 = 类型/协议错配的幽灵 bug；宁可让用户刷新。

## 五、第四形态：动态包（运行时装载）

除了静态双面包，DSH 还有一个更激进的机制：`dsh-cordis-client-runner` 能在**页面里装载运行时生成的包**（请求经批准或用户显式触发）。

它的边界条件非常特殊，写"能生成代码"的插件时必须知道：

| 维度 | 规则 |
|------|------|
| 语言 | 浏览器半是**纯 JavaScript**：无 JSX、无 TypeScript、**不能 import 模块** |
| 形态 | 作为 `async` 函数体求值，返回值是插件对象 |
| 符号面 | 只有固定的几个名字：`React`、`console`、`styles`、`host`；**`fetch`、`setTimeout` 等浏览器全局不可用** |
| 能力 | 只能用生命周期动词 + 自己在 `inject` 里声明的服务 |
| 跨端 | `host.call(method, args)` 到达它自己的宿主半（经 Remote namespace 路由；省略入参以 `null` 过线） |
| 安全性 | guard 是白名单（生命周期动词 + 已声明服务），与宿主侧沙箱门面对称 |
| 生命周期 | 装载幂等：同一 revision 重装无变化，新 revision 顶替，retract 后同 revision 可重装；**刷新页面回到干净状态**（宿主仍持有定义） |

它与静态 client 插件的关键差别：**动态包与静态包共享同一套激活门控、fiber effect 清理与状态投影**——不是"在沙箱里跑一段脚本"，而是"真的成为一个插件"。因此它的崩溃会顺着同一套 slot registry 的 entry-error seam 报上去：一路给模型，一路给面板（`renderFailures`）。

## 六、决策表：这个功能该放哪一端

| 需求 | 放哪 | 理由 |
|------|------|------|
| 给模型加能力 | 宿主侧工具插件 | 模型只看得见宿主侧工具 |
| 读文件/起进程/连数据库 | 宿主侧 | 浏览器没有这些能力 |
| 改系统提示词、加服务 | 宿主侧 | 浏览器侧没有 `ctx.tools`、`ctx.llm` |
| 加面板/卡片/侧栏/命令 | client 侧 | 只有浏览器有 DOM 与槽位 |
| 工具调用的可视化 | **client 侧 renderer** | Web 不消费 `presentCall` |
| 桌面壳/CLI 呈现 | 宿主侧 `presentCall`/`presentResult` | 那些消费方在宿主侧 |
| 让模型自己写个 UI 小工具 | 动态包 | 唯一支持运行时装载的通道 |
| 宿主+浏览器都要 | 双面包（两入口） | 一个包，两个 `exports` |

## 小结

- 两端分离是物理+协议+安全三重原因；宿主侧 bundle **不能** import client 包，双面包必须两个入口；
- `dsh.client` 四字段各有语义，`inject` 是模块 row 依赖、`external` 只写基座之外的精确请求；
- client 插件是**构建产物**：没有 `lib/client.js` 即激活失败；改代码要重建，HMR 需要 `dev:web`；
- Web 工具卡片来自客户端 renderer（`tool.call.toolview`），**不是** `presentCall`/`presentResult`；
- release 身份（version+commit）隔离跨版本混跑，切 release 只能刷新；
- 动态包是第四形态：纯 JS、固定符号面、白名单 guard、与静态插件共享同一套生命周期；
- 下一篇把这些知识压成一条可执行流程：从零写一个插件到发布。

---

**系列导航**：[001 系列导读](/posts/tech/ai/agent/deepseek-harness-dev-001/) · [上一篇：014 插件加载与热更新](/posts/tech/ai/agent/deepseek-harness-dev-014/) · [下一篇：016 开发全流程](/posts/tech/ai/agent/deepseek-harness-dev-016/)
