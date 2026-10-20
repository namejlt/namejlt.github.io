---
title: "AI Agent-deepseek-harness插件开发001 系列导读与生态全景"
date: 2026-10-18T17:50:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "Cordis"]
---

## 概述

DeepSeek Harness（命令行工具名 `dsh`）是 DeepSeek 开源的 Agent Harness：它既是一个可以直接运行的 Coding Agent（Web 与 headless 两种形态），底层又是一套「一切皆插件」的框架——模型、工具、沙箱、会话存储、UI、乃至 Agent Loop 本身，都是插件。

官方仓库里有一套按子系统组织的文档与 cookbook（`docs/subsystems/*.zh.md`、`docs/cookbook/adding-a-tool.zh.md` 等，**发行树不含，本次未逐篇核对**），但它们的形态是"参考手册"，社区反馈集中在几个痛点上：**章节之间跳跃大、缺少完整可运行的实战插件、缺少工程化（调试/测试/发布）链路、缺少与社区热门插件的对照**。

本系列共 16 篇，面向有 TypeScript 基础的开发者，目标只有一个：**读完能独立开发出真正可用的 Harness 插件**。系列中会完整实现两个仿制社区热门能力的插件（本地知识库检索、跨会话记忆），全部代码零外部服务依赖、可本地运行。

系列分两条轨：**主线 002–010**（动手：环境、配置、生命周期、工具契约、两个实战、事件与服务、安全与后台任务、打包）；**原理篇 011–016**（架构：项目结构、设计哲学、主循环与工具链路、插件加载与热更新、双端插件、开发全流程）。原理篇不是附录——**011/012 建议在 002 之前读一遍**，013–016 在写完第一个插件后回读收益最大。

> **事实基线**：本系列所有 API 以 `@deepseek-ai/dsh` `0.2.1-rc.2`（本机桌面版发行树，296 个 `@deepseek-ai/*` 包）、Cordis `4.0.5`、Schemastery `3.18.5` 为准。发行树不含 `src/` 与 `.d.ts`，故签名以各包 `README.zh.md` 与 `lib/*.js` 内 JSDoc 为依据，关键处再用 npm 包里的 `.d.ts`（同版本 `0.2.1-rc.2`）交叉核对；仍无法核实的内容会明确标注为“未核实”，不写猜测。

## 一、DeepSeek Harness 是什么

### 1.1 Brain + Harness 范式

在 DeepSeek 的工程范式里，一个自主 Agent 不是单体黑盒，而是「大脑（模型）+ 神经系统与骨架（Harness）」的协同：

```
┌────────────────────────────────────────────────────────┐
│                      Harness（骨架）                    │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────┐ │
│  │ 模型接入  │  │ 工具执行  │  │ 会话存储  │  │  UI   │ │
│  │ (llm)    │  │ (tools)  │  │ (session)│  │ (web) │ │
│  └──────────┘  └──────────┘  └──────────┘  └────────┘ │
│                         │                              │
│                  Cordis 微内核                          │
│         （一切皆插件 · 可逆注册 · 自动清理）              │
└────────────────────────────────────────────────────────┘
                          │
                    ┌──────────┐
                    │  DeepSeek │  ← Brain（模型）
                    │   Model   │
                    └──────────┘
```

「一切皆插件」不是营销话术，它有明确的工程含义：

- **无特权内核**：内核（Cordis）不内置任何具体能力，模型接入、bash 执行、文件读写全部是插件；
- **可逆注册**：任何能力注册进来都可以被完整撤销，卸载插件不残留状态；
- **配置层组合**：最终生效的行为由多层 patch 按序组合，用户可以在自己 profile 里覆盖任何插件的默认配置。

### 1.2 五种表层（profile），同一份插件代码

"表层"由 bundle 组合决定，每个表层就是一个随发行版交付的 profile 模板：

| 表层 | 典型命令 | 用途 | 组合特征 |
|------|----------|------|----------|
| `web` | `dsh --profile web` | 人机交互的日常 Coding Agent | `dsh-base` + `dsh-web-app`（HTTP/SSE/客户端模块） |
| `headless` | `dsh --profile headless "<task>"` | 一次性执行、脚本、CI | `dsh-base` + `dsh-headless`（无 UI）；YAML 里**显式禁用 HMR** |
| `acp` | `dsh --profile acp` | 编辑器/宿主通过 ACP 协议驱动 | automation-only、JSON-RPC stdio |
| `sdk` / `sdk-minimal` | 进程内嵌入 | 自建 Agent 产品 | `sdk` 保留 base 栈；`sdk-minimal` 是独立配置树（不依赖 base） |

插件对五种表层是同一份代码——这是插件化架构的红利；但**组合不同意味着可见服务不同**：headless 里没有 `dsh-client-*`，`acp` 里没有浏览器侧。所以"我的插件在 web 里好好的、headless 里没跑"，第一反应应该是查 `inject` 的服务在该组合里有没有提供方。

## 二、核心架构：Profile、Bundle、Patch 三层配置

插件开发之前，必须先理解 Harness 的**配置组装机制**，这是它区别于普通插件体系（如 VS Code 扩展）的核心。

### 2.1 三层概念

```
Profile（$DSH_HOME/profiles/<name>/）
  ├── package.json         ← 声明 dsh.profile：由哪些 bundle 按什么顺序组成
  ├── cordis.patch.yml     ← 用户自己的 patch 层
  └── node_modules/
        └── dsh-hello-plugin/   ← Bundle（npm 包）
              ├── package.json  ← 声明 dsh.bundle：贡献哪个 patch 文件
              ├── cordis.patch.yml
              └── index.js      ← 插件模块本体
```

- **Bundle（组合包）**：附带一个配置层的 npm 包，manifest 声明 `dsh.bundle`，回答「这个包贡献什么」；
- **Profile**：位于 `$DSH_HOME/profiles/<name>` 的目录，manifest 声明 `dsh.profile`，回答「这套可启动组合由哪些 bundle 按什么顺序组成」；
- **Patch 文件**：YAML 数组，每一行（row）声明加载哪个插件、传什么配置。

### 2.2 四层加载顺序（必须背下来）

生效配置在空根之上按以下顺序逐层组合，**后应用的层按行胜出**：

1. profile 的 `dsh.profile.bundles` 列表所列各组合包的 patch（先是 `@deepseek-ai/dsh-base`，然后按加入顺序）；
2. profile 自己的 `cordis.patch.yml`；
3. home 级 `$DSH_HOME/cordis.patch.yml`（机器本地偏好，各 profile 共享）；
4. 每个 `--patch <path>` overlay，按 argv 顺序。

> 关键语义：**patch 替换目标行的整个 config 值，不做深度合并**。所以你要覆盖别人某一行时，必须重述该行需要的每一个键，而不是只写改动的那一个。这是新手最常踩的坑，第 003 篇会专门演练。

### 2.3 内置服务

服务不是"内核自带"，而是**由具体插件行挂上来的**。下表是本系列会用到的几个（提供方写成包名，是为了让你在 `--dump-config` 里能对上号）：

| 服务 | 提供方 | 说明 |
|------|--------|------|
| `ctx.tools` | `@deepseek-ai/dsh-tools` | `register` / `get` / `schemas` / `restrict` / `guard`；工具 schema 自动流向提示词组装 |
| `ctx.llm` | `@deepseek-ai/dsh-llm` | 模型请求与适配器注册、`prepareCall()` |
| `ctx.agents` | `@deepseek-ai/dsh-agent-loop` | `create()` / `resume()`，返回 `AgentHandle` |
| `ctx.sessions` | `@deepseek-ai/dsh-session` | 事件溯源会话；`list()`、`append()`、`session/event` 流 |
| `ctx.systemPrompt` | `@deepseek-ai/dsh-system-prompt` | 系统提示词组装注册表 |
| `ctx.jobs` | `@deepseek-ai/dsh-jobs`（抽象）+ `dsh-jobs-local` | 后台任务；`start()` 需要已附加的控制器（`dsh-tool-jobs`） |
| `ctx.fs` | `@deepseek-ai/dsh-fs-sandbox` | 受沙箱围栏的文件读写；**同类服务只能有一个提供者** |

两个必记的边界：**同名服务在一个作用域里只能有一个提供者**（多挂一个 `fs-local` 会让 profile 加载失败）；**没挂对应行就没有这个服务**（`ctx.tools` 也会不存在）。精确方法集以各包 `README.zh.md` 为准，不要维护静态清单。

## 三、插件的三种形态与能力的三种角色

### 3.1 插件三种形态

| 形态 | 写法 | 适用场景 |
|------|------|----------|
| 函数式 | `export const name = 'x'; export function apply(ctx) {}` | 90% 的场景，官方推荐默认 |
| 对象式 | `export default { name, inject, apply(ctx) {} }` | 与函数式等价的组织方式 |
| 类式 | `export default class X extends Service {}` | 需要向其他插件**提供服务**时 |

### 3.2 能力的三种角色（三层拆分）

当一项能力需要支持可替换的实现时（典型如 Bash 执行），官方建议拆成三个角色、三个包：

```
┌─────────────┐     ┌──────────────────┐     ┌──────────────┐
│  Definition │────▶│    Provider      │     │   Consumer   │
│  dsh-shell  │     │  dsh-bash-local  │     │ dsh-tool-bash│
│ (定义契约)   │     │  (本地执行实现)    │     │ (暴露为工具)  │
└─────────────┘     └──────────────────┘     └──────────────┘
       ▲                                            │
       └────────────────────────────────────────────┘
                    inject: ['shell']
```

- **Service Definition**：定义 Cordis 服务与请求/结果类型；
- **Service Provider**：具体实现（本地执行、远程执行、容器内执行……）；
- **Consumer**：把能力包装成模型可调用的工具。

好处：换 Provider 不动 Definition 和工具；三者依赖解耦（Provider 与 Consumer 互不依赖）。**但不要预防性拆分**——简单工具插件一个包就够了，第 008 篇会给判断标准。

## 四、社区生态地图：怎么找仿制标杆

社区精选列表（awesome-dsh-plugin 等）收录了大量可通过 `dsh plugin add` 安装的插件，并按能力分类。对插件开发者来说，这份分类表就是**最佳的需求来源**——每个热门分类都代表一类真实场景。

> **数字不要抄，机制要懂**：`dsh-plugin-market` 默认 `indexes: []`——"市场"不是内核内置的一张表，而是**你配置的索引**；分类 id 与标签也由索引文档自己声明（`category` 是"插件的分类 id"）。社区最全的一份清单是 [awesome-dsh-plugin](https://github.com/awesome-dsh-plugin/awesome-dsh-plugin)，它把插件总数做成了实时徽章（`https://awesome-dsh-plugin.com/count.json`，2026-10-20 读到 6084 条），并同时以 format-1 索引发布：在 **Plugins → Discover → Index sources** 里加 `https://cdn.jsdelivr.net/npm/dsh-plugin-catalog/index.json`，客户端（以及 `plugin_market` 工具）就能搜到同一批条目。**任何写死的"X 个插件、Y 个分类"都会在一个月内失效——引用数量前读 count.json。**

下表只保留"分类 → 你该学到的能力"这一层稳定的对应关系。**具体插件名不写进正文**（社区条目每天在变，写死即过时），需要时去上面那个索引里搜；分类名采用清单的 23 类中的九类：

| 清单分类 | 典型做法 | 你会用到的 API | 仿制价值 |
|----------|----------|----------------|----------|
| 🧠 记忆 | remember/recall/forget 工具 + 跨会话注入 | `defineTool`、`session/event`、持久化存储 | ⭐⭐⭐ 本系列实战二 |
| 🛠️ 工具与能力 | 知识库入库、混合检索、引用溯源 | `defineTool`、`ctx.fs`、`presentationMeta` | ⭐⭐⭐ 本系列实战一 |
| 🔔 通知与集成 | Webhook / IM 推送、外部进程桥接 | `ctx.on`、`fetch`、后台任务 | ⭐⭐ |
| 🔒 安全与权限 | 策略钩子拦截、审计日志、脱敏 | `tools/pre-execute`、`ctx.effect` | ⭐⭐ 第 009 篇 |
| 🔁 工作流与自动化 | 定时唤醒、轮次续跑、条件门禁 | 事件系统 + 定时器 + `ctx.jobs` | ⭐⭐ |
| 🌐 浏览器与 Web | 外部浏览器、页面抓取 | MCP / 子进程 / `fetch` | ⭐ |
| 🎨 UI 增强 / 主题 | 状态栏、主题包、卡片视图 | `dsh.client`、`ctx.slots`、`tool.call.toolview` | ⭐（前端成本高） |
| 🧩 技能 | SKILL.md 渐进式加载、技能迁移 | skill 注册表 + `tool-skill` | ⭐ |
| 🔀 Git 与代码评审 | 结构化 Git 工具、评审门禁 | 独占工具声明 + `tools/post-execute` | ⭐ |

**为什么选「知识库」和「记忆」做实战**：

1. **高频刚需**：几乎所有重度用户都会装至少一个记忆/知识库插件；
2. **零外部依赖可实现**：不依赖向量模型、不依赖云服务，纯本地可跑，教程可复现；
3. **覆盖全部核心 API**：工具定义、配置、持久化、事件、渲染、后台任务都能练到；
4. **有明确的对照标杆**：可以逐条对比社区插件的行为，验证自己的实现。

## 五、本系列与官方文档的差异

| 维度 | 官方文档（仓库 docs/） | 本系列 |
|------|------------------------|--------|
| 定位 | 按包子系统组织的参考手册 | 任务导向教程，强调「为什么」和「坑在哪」 |
| 组织单位 | 包 / 子系统 | 读者的下一步动作 |
| 实战插件 | 最小示例为主 | 2 个完整可运行插件（知识库 + 记忆）+ 3 个骨架插件 |
| 代码深度 | 最小片段 | 生产级结构：分层、持久化、错误处理、测试 |
| 工程链路 | 分散在各包 README | 集中成 010 + 016：调试 → 测试 → 打包 → 发布 |
| 社区对照 | 无 | 每章标注对标插件与差异点 |
| 排错 | 各包"已知限制"段 | 集中式「常见坑速查表」+ 失败语义速查 |

原则：**包级 README 与源码是唯一权威**。本系列所有 API 均以 `0.2.1-rc.2` 发行树核对过（截至 2026-10-20）；凡无法核实的内容一律标注为“未核实”，并给出不依赖未核实签名的保守用法。

## 六、学习路线与文章索引

编号是**写作顺序**，不是阅读顺序。两条轨可以穿插：

**主线（动手）002 → 010**

| # | 标题 | 你将获得 |
|---|------|----------|
| [002](/posts/tech/ai/agent/deepseek-harness-dev-002/) | 环境搭建与第一个插件 | 跑通开发循环，避开常见坑 |
| [003](/posts/tech/ai/agent/deepseek-harness-dev-003/) | 插件配置：Schemastery 与层叠机制 | 会设计配置，理解四层覆盖语义 |
| [004](/posts/tech/ai/agent/deepseek-harness-dev-004/) | 生命周期、effect 与 Fiber | 写出卸载无残留的插件 |
| [005](/posts/tech/ai/agent/deepseek-harness-dev-005/) | 模型工具开发：五层契约完全解读 | 写出模型爱调用的工具 |
| [006](/posts/tech/ai/agent/deepseek-harness-dev-006/) | 实战一：本地知识库检索插件 | 完整插件：kb_add / kb_search / kb_list |
| [007](/posts/tech/ai/agent/deepseek-harness-dev-007/) | 实战二：跨会话记忆插件 | 完整插件：remember / recall / forget + 被动捕获 |
| [008](/posts/tech/ai/agent/deepseek-harness-dev-008/) | 事件系统、服务与三层能力拆分 | 松耦合架构，可替换 Provider |
| [009](/posts/tech/ai/agent/deepseek-harness-dev-009/) | 安全策略钩子与后台任务 | 审计日志、敏感信息脱敏、长任务模式 |
| [010](/posts/tech/ai/agent/deepseek-harness-dev-010/) | 打包、发布与工程实践 | bundle/profile、三条分发路径、发布 checklist |

**原理篇（架构）011 → 016**

| # | 标题 | 你将获得 | 建议时机 |
|---|------|----------|----------|
| [011](/posts/tech/ai/agent/deepseek-harness-dev-011/) | 原理篇①：项目结构与目录职责地图 | 三套目录的分界、49 个 packages 领域的职责 | 读完本篇立刻读 |
| [012](/posts/tech/ai/agent/deepseek-harness-dev-012/) | 原理篇②：设计哲学与六个核心抽象 | Context/Fiber/Service/Event/Session/Tool 的边界 | 002 之前 |
| [013](/posts/tech/ai/agent/deepseek-harness-dev-013/) | 原理篇③：主循环与工具调用链路 | 一次输入的九站、五段工具流水线 | 005 之后 |
| [014](/posts/tech/ai/agent/deepseek-harness-dev-014/) | 原理篇④：插件加载、组合与热更新 | 启动七阶段、patch 行语法、三种"重来" | 003/004 之后 |
| [015](/posts/tech/ai/agent/deepseek-harness-dev-015/) | 原理篇⑤：双端插件 | `dsh.client`、启动图、release 身份、动态包 | 想做 UI 时 |
| [016](/posts/tech/ai/agent/deepseek-harness-dev-016/) | 原理篇⑥：开发全流程实战手册 | 三条开发路径、调试六动作、测试三层、20 条坑 | 交付前 |

推荐路径：**001 → 011 → 012 → 002 → 003 → 004 → 005 → 013 → 006 → 007 → 008 → 009 → 014 → 010 → 015 → 016**。已有插件开发经验者可直接读 006/007 拿完整代码，再用 013/014 校准对运行时的理解。

## 七、前置知识

- TypeScript：接口、泛型、声明合并（`declare module`）；
- Node.js：ESM、`node:fs`、`node:path`、`node:os`；
- 包管理：pnpm 基础（link、workspace、peerDependencies）；
- LLM 基础：Function Calling / 工具调用概念（可先读本库 [AI 应用开发-011 MCP协议与A2A智能体协作](../app-dev/011-MCP-A2A.md)）。

不需要：Cordis 源码经验、前端经验（UI 类插件除外）、向量数据库经验。

## 小结

- Harness = Brain（模型）+ Cordis 微内核骨架，**一切能力皆插件**（含 Agent 主循环本身）；
- 配置组装 = Profile → Bundle → Patch 四层覆盖，**整行替换不深合并**，行序无加载语义；
- 插件三形态（函数/对象/类），能力三角色（Definition/Provider/Consumer）；
- 五种表层（web/headless/acp/sdk/sdk-minimal）是不同 bundle 组合，同一份插件代码，但**可见服务不同**；
- 本系列共 16 篇：主线 002–010 动手，原理篇 011–016 补架构；编号是写作顺序，推荐路径见 §六；
- 下一站建议先读 [011 项目结构](/posts/tech/ai/agent/deepseek-harness-dev-011/)，再动手搭环境。

---

**系列导航**：[下一篇：002 环境搭建与第一个插件](/posts/tech/ai/agent/deepseek-harness-dev-002/) · [原理篇入口：011 项目结构与目录职责地图](/posts/tech/ai/agent/deepseek-harness-dev-011/)
