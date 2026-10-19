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

官方文档提供了完整的插件开发路径（第一个插件 → 工具 → 配置 → 打包 → 框架深入 → 实战模式），但社区反馈集中在几个痛点上：**教程之间跳跃大、缺少完整可运行的实战插件、缺少工程化（调试/测试/发布）链路、缺少与社区热门插件的对照**。

本系列共 10 篇，面向有 TypeScript 基础的开发者，目标只有一个：**读完能独立开发出真正可用的 Harness 插件**。系列中会完整实现两个仿制社区热门能力的插件（本地知识库检索、跨会话记忆），全部代码零外部服务依赖、可本地运行。

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

### 1.2 三种使用形态

| 形态 | 命令形态 | 用途 |
|------|----------|------|
| Web UI | `dsh web` / `pnpm dsh web` | 人机交互的日常 Coding Agent |
| headless | CLI/脚本方式驱动 | CI、批处理、自动化 |
| 嵌入运行时 | 作为库/SDK 使用 | 自建 Agent 产品 |

插件对三种形态是同一份代码，这正是插件化架构的红利。

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

加载后，以下服务挂载在 `ctx` 上，是插件最常消费的能力：

| 服务 | 访问方式 | 说明 |
|------|----------|------|
| 工具注册表 | `ctx.tools` | `ctx.tools.register(...)` 注册模型可调用的工具 |
| LLM | `ctx.llm` | 模型请求、适配器注册 |
| Agent | `ctx.agents` | 子代理等 |

服务名、公开方法以仓库为各服务自动生成的子系统页面和 TypeScript 接口为准，不要维护静态清单。

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

## 四、社区生态地图：500+ 插件怎么抄

社区精选列表（awesome-dsh-plugin 等）已收录 500+ 个可通过 `dsh plugin add` 安装的插件，按 20 个大类组织。对插件开发者来说，这份分类表就是**最佳的需求来源**——每个热门分类都代表一类真实场景。

| 分类 | 代表插件 | 能力要点 | 仿制价值 |
|------|----------|----------|----------|
| 🧠 记忆 | `flymysql/dsh-memory`、`ICCuse/dsh-file-memory`、`modusensus/dsh-mneme` | remember/recall/forget 工具 + 跨会话注入 | ⭐⭐⭐ 本系列实战二 |
| 🛠️ 工具与能力 | `ICCuse/dsh-knowledge`、`imkelt/DSH-RAG`、`Breeze136/dsh-kb-rag` | 知识库入库/混合检索/引用溯源 | ⭐⭐⭐ 本系列实战一 |
| 🔔 通知与集成 | 各类 Webhook/IM 推送 | 事件驱动 + HTTP | ⭐⭐ |
| 🔒 安全与权限 | `safety-net`、`secret-guard`、`PerryLink/dsh-defend` | 策略钩子拦截 + 审计日志 | ⭐⭐ 第 009 篇 |
| 🔁 工作流与自动化 | `weibaohui/dsh-continue`、`session-titler` | 事件系统深度使用 | ⭐⭐ |
| 🌐 浏览器与网页 | `browser-automation`、`Lum1104/dsh-browser` | 外部进程/MCP 桥接 | ⭐ |
| 🎨 UI 增强 | `dsh-spotlight`、计费/用量类家族 | 前端桥接层 | ⭐（前端成本高） |
| 🧩 技能包 | `dsh-skillport`、技能迁移类 | SKILL.md 渐进式加载 | ⭐ |
| 🔀 Git 与代码评审 | `dsh-tool-git`、`adversarial-review` | 结构化 Git 工具 | ⭐ |

**为什么选「知识库」和「记忆」做实战**：

1. **高频刚需**：几乎所有重度用户都会装至少一个记忆/知识库插件；
2. **零外部依赖可实现**：不依赖向量模型、不依赖云服务，纯本地可跑，教程可复现；
3. **覆盖全部核心 API**：工具定义、配置、持久化、事件、渲染、后台任务都能练到；
4. **有明确的对照标杆**：可以逐条对比社区插件的行为，验证自己的实现。

## 五、本系列与官方教程的差异

| 维度 | 官方教程 | 本系列 |
|------|----------|--------|
| 定位 | 手册式参考，最短路径 | 工程化教程，强调「为什么」和「坑在哪」 |
| 实战插件 | 无完整插件 | 2 个完整可运行插件（知识库 + 记忆） |
| 代码深度 | 最小示例 | 生产级结构：分层、持久化、错误处理、测试 |
| 工程链路 | 分散 | 调试 → 测试 → 打包 → 发布 → 社区提交全流程 |
| 社区对照 | 无 | 每章标注对标的热门插件与差异点 |
| 排错 | 散落各页 | 集中式「常见坑速查表」 |

原则：**官方文档是唯一权威来源**。本系列所有 API 均以官方文档与仓库源码为准（截至 2026-10）；凡涉及预览版会变化的接口（策略钩子、后台任务等），文中会显式标注「以当前版本参考为准」，并给出保守用法。

## 六、学习路线与文章索引

| # | 标题 | 你将获得 |
|---|------|----------|
| [001](./deepseek-harness-dev-001.md) | 系列导读与生态全景 | 架构心智模型、生态地图、学习路线 |
| [002](./deepseek-harness-dev-002.md) | 环境搭建与第一个插件 | 跑通开发循环，避开 8 个常见坑 |
| [003](./deepseek-harness-dev-003.md) | 插件配置：Schemastery 与层叠机制 | 会设计配置，理解四层覆盖语义 |
| [004](./deepseek-harness-dev-004.md) | 生命周期、effect 与 Fiber | 写出卸载无残留的插件 |
| [005](./deepseek-harness-dev-005.md) | 模型工具开发：五层契约完全解读 | 写出模型爱调用的工具 |
| [006](./deepseek-harness-dev-006.md) | 实战一：本地知识库检索插件 | 完整插件：kb_add / kb_search / kb_list |
| [007](./deepseek-harness-dev-007.md) | 实战二：跨会话记忆插件 | 完整插件：remember / recall / forget + 被动捕获 |
| [008](./deepseek-harness-dev-008.md) | 事件系统、服务与三层能力拆分 | 松耦合架构，可替换 Provider |
| [009](./deepseek-harness-dev-009.md) | 安全策略钩子与后台任务 | 审计日志、敏感信息脱敏、长任务模式 |
| [010](./deepseek-harness-dev-010.md) | 打包、发布与工程实践 | bundle/profile、三条分发路径、测试与 checklist |

建议按序阅读；已有插件开发经验者可跳读 006/007 获取完整代码。

## 七、前置知识

- TypeScript：接口、泛型、声明合并（`declare module`）；
- Node.js：ESM、`node:fs`、`node:path`、`node:os`；
- 包管理：pnpm 基础（link、workspace、peerDependencies）；
- LLM 基础：Function Calling / 工具调用概念（可先读本库 [AI 应用开发-011 MCP协议与A2A智能体协作](../app-dev/011-MCP-A2A.md)）。

不需要：Cordis 源码经验、前端经验（UI 类插件除外）、向量数据库经验。

## 小结

- Harness = Brain（模型）+ Cordis 微内核骨架，一切能力皆插件；
- 配置组装 = Profile → Bundle → Patch 四层覆盖，**整行替换不深合并**；
- 插件三形态（函数/对象/类），能力三角色（Definition/Provider/Consumer）；
- 社区 500+ 插件按 20 类组织，是本系列实战的仿制标杆；
- 下一篇直接动手：搭环境、跑通第一个插件。
