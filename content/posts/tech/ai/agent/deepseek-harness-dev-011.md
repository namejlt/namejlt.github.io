---
title: "AI Agent-deepseek-harness插件开发011 原理篇①：项目结构与目录职责地图"
date: 2026-10-20T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "Cordis", "架构"]
---

## 概述

写插件写到第二周，大部分人卡的不是 `defineTool` 怎么写，而是**不知道自己在哪一层动手**：想加一个能力，应该放 `packages/core`、`packages/client`，还是自己建一个包？改了 `node_modules` 里的 `lib/index.js` 为什么下次启动就没了？`$DSH_HOME/profiles/web/cordis.patch.yml` 和仓库里的 `cordis.patch.yml` 到底谁说话算数？

本篇把 DSH 的**三套目录**掰开：源码仓库树、安装发行树、用户态 profile 树。三者名字相似、职责完全不同，混淆是插件开发第一类系统性错误。

> **本篇的事实基线**：`@deepseek-ai/dsh` 与绝大多数同级包版本 `0.2.1-rc.2`（本机桌面版发行树，共 296 个 `@deepseek-ai/*` 包），Cordis `4.0.5`，Schemastery `3.18.5`，cosmokit `1.8.6`。发行树里没有 `src/`，只有 `lib/` + `README.zh.md` + `locale/`，所以后文凡是"签名"都以包内 `README.zh.md` 与 `lib/types/*.js` 的 JSDoc 为准。仓库里的 `docs/subsystems/*.zh.md`、`docs/config-catalog.zh.md` 等**不在发行树中**，但在 GitHub 仓库的 `docs/` 下（同目录还有 `cookbook/`、`user/`、`upgrade-guide/`），并由仓库根的 `website/`（VitePress）组装成文档站——本地预览 `pnpm docs:dev`，CI 走 `.github/workflows/docs-pages.yml`。

## 一、别混淆：三套目录，三种真相

```
┌─────────────────────────────────────────────────────────────────┐
│ ① 源码仓库树（github.com/deepseek-ai/deepseek-harness）           │
│    packages/*  apps/*  vendor/*  docs/*  .agents/notes/*         │
│    → 类型：TypeScript 源码；你能读能改，改完要构建               │
├─────────────────────────────────────────────────────────────────┤
│ ② 安装发行树（…/resources/app.asar/dsh/node_modules/@deepseek-ai）│
│    <package>/{package.json, lib/*.js, lib/types/*.js,            │
│               README.zh.md, locale/*.json}                        │
│    → 类型：已构建产物；你**只读**，改它等于给自己埋雷            │
├─────────────────────────────────────────────────────────────────┤
│ ③ 用户态 profile 树（$DSH_HOME/profiles/<name>/）                 │
│    package.json（dsh.profile）  cordis.patch.yml  node_modules/   │
│    → 类型：你的组合与配置；插件最终"装"在这一层                  │
└─────────────────────────────────────────────────────────────────┘
```

三条判据，背下来就够用：

| 你的动作 | 落在哪一层 | 是否持久 |
|----------|-----------|----------|
| 写插件源码 | 你自己的仓库（不在三者内） | 是 |
| 让插件生效（开发期） | ③ 的 `cordis.patch.yml` 或 `--patch` overlay | 是（overlay 随 argv 走） |
| 让插件生效（分发期） | 打成 bundle 包，`dsh plugin add` 进 ③ | 是 |
| 改 `node_modules/@deepseek-ai/*/lib/*.js` | ② 只读区 | **否**，升级即丢，且 HMR 会把它当"文件被改"处理 |

## 二、源码仓库树：49 个 packages 领域、295 个包在管什么

仓库是 pnpm monorepo，包目录**按领域**平铺在 `packages/` 下（49 个领域目录、295 个包），`vendor/` 放 fork 过的第三方框架（7 个），`apps/` 放进程入口（2 个）。下面这张表按 0.2.1-rc.2 的 `package.json#repository.directory` 实测统计，**49 个领域全列**（合计 295 包；包数 / 代表包）：

| 目录 | 包数 | 代表包与职责 |
|------|------|--------------|
| `packages/core` | 8 | `dsh-tools`（工具注册表与执行流水线）、`dsh-agent`（Agent 接口与事件词汇）、`dsh-agent-loop`（唯一的具体循环实现）、`dsh-session`（事件溯源会话）、`dsh-system-prompt`（系统提示词组装）、`dsh-scope`（作用域注册原语） |
| `packages/boot` | 6 | `dsh-app-boot`（profile 解析 + patch 组合 + 启动）、`dsh-cmdline`（命令行交接）、`dsh-hmr`、`dsh-plugin-manager`、`dsh-config-editor`、`dsh-plugin-market` |
| `packages/bundle` | 6 | 每种"表层"一个组合包：`dsh-base`、`dsh-web-app`、`dsh-headless`、`dsh-acp-app`、`dsh-sdk-app`、`dsh-sdk-minimal` |
| `packages/client` | 61 | 浏览器侧一切：`dsh-client-modules`（模块系统双面）、`dsh-client-connection`（RPC/SSE 传输）、`dsh-client-hmr`、`dsh-client-ui-*`（布局、会话、设置、侧栏、工具渲染器……） |
| `packages/session` | 21 | 会话持久化与投影：`dsh-session-persistence-jsonl`、`dsh-session-projection*`、`dsh-session-format-v0→v4`（格式迁移）、`dsh-session-title*` |
| `packages/llm` | 9 | 模型接入与适配器、重试、token 统计 |
| `packages/api` | 10 | Host → 浏览器的 Remote 层：`dsh-api-gateway`（网关与鉴权）、`dsh-api-session-controller`、`dsh-api-remote-access`、`dsh-api-remotes` |
| `packages/host` | 9 | 进程级宿主服务：`dsh-host-webserver`（HTTP 传输）、`dsh-host-frontend-static`、`dsh-host-directory-picker*`、`dsh-host-plugin-inventory`、`dsh-host-open-in-app` |
| `packages/context` | 7 | 提示词上下文注入：`dsh-agent-instructions`、`dsh-file-reference*`、`dsh-time-context`、`dsh-harness-dev-context` |
| `packages/subagent` | 5 | 子代理、fork、委派工具 |
| `packages/shell` / `subprocess` | 10 / 3 | bash / pwsh 执行器、PTY、进程树 |
| `packages/sandbox` / `fs` | 4 / 7 | 沙箱策略、`ctx.fs`（`fs-sandbox` 围栏）、原生文件工具 |
| `packages/mcp` / `skill` / `workflow` / `goal` / `plan` / `schedule` / `todo` | 2 / 5 / 3 / 4 / 1 / 2 / 1 | 各自一个完整能力域 |
| `packages/ptc-runtime` | 2 | PTC（`run_code`）运行时与 SDK 渲染器 |
| `packages/jobs` | 3 | `dsh-jobs`（抽象 `ctx.jobs`）、`dsh-jobs-local`、`dsh-tool-jobs`（三个后台任务工具 + 提供方控制器） |
| `packages/util` | 18 | `dsh-package-manifest`（manifest 类型）、`dsh-atomic-write`、`dsh-chunked-list`、`dsh-brand` 等底座 |
| `packages/experimental` | 29 | 未定型能力（含 WebWorker 打包运行时等） |
| 其余 24 个小域 | 59 | `credentials`(5)、`compaction`(5)、`interaction`(5)、`web`(5)、`extensions`(4)、`preset`/`session-query`/`spill`/`storage`/`typert`(各 3)、`attachment`/`feedback`/`guard`/`terminal`/`deliverables`/`sdk`(各 2)，以及 `acp`/`identity`/`document`/`telemetry`/`settings`/`workspace`/`entry`/`win32-x64`(各 1) |
| `apps/cli` / `apps/web` | 2 | 进程入口：CLI launcher 与浏览器壳 |
| `vendor/*` | 7 | `cordis` 4.0.5、`cosmokit` 1.8.6、`loader`、`group`、`include`、`schemastery` 3.18.5、`timer` 1.1.7 |

> 读法建议：**`packages/core` 8 个包 + `packages/boot` 6 个包**是插件作者必须熟的两层；`client`/`session`/`llm` 按需读；`experimental` 先忽略。

### 2.1 为什么 `vendor/` 要 fork

七个 vendor 包都是"上游同名库 + Harness 补丁"。它们进 `vendor/` 而不是当普通依赖，原因很具体：

- **Cordis 被改了**：注册表、Fiber、reflect 都带 Harness 需要的语义（`scope`、`intercept`、`scopeTarget`）。插件框架的 ABI 是这些改动定义的，不能跟上游同版本号混用。
- **Loader 的 `EntryTree` 是 HMR 的支点**：`dsh-hmr` 需要 `moduleNamespace`、`loader.create/update/remove`、`loader/volatile-update` 这些内部面。上游 loader 没有，就得 fork。
- **Schemastery 提供 `~standard`**：Cordis 的配置校验走 Standard Schema（`runtime.Config['~standard'].validate(config)`），不是 Koishi 时代的老路径。

结论：`@deepseek-ai/cordis` 与上游 `cordis` **不是同一个东西**，文档、示例、类型都不要跨用。

## 三、安装发行树：一个包里到底有什么

```
node_modules/@deepseek-ai/dsh-tools/
├── package.json          # exports 指向 lib/index.js 与 lib/types/*
├── lib/
│   ├── index.js          # 构建产物（可读，带 JSDoc）
│   └── types/*.js        # 类型模块的运行时镜像（.d.ts 已被剥掉）
├── locale/{en,zh}.json   # 模型可见文案 / UI 文案
├── README.zh.md          # ★ 权威文档：行为、配置表、已知限制
└── README.i18n.yaml      # 英文版翻译哈希
```

核对 API 的**唯一可靠姿势**：

1. 先读那个包的 `README.zh.md`——它有 `使用本包 / 理解实现 / 模型体验 / 已知限制` 五段，其中"使用本包"就是契约；
2. 再看 `lib/index.js` 里的 JSDoc：发行产物不剥注释，`/** @param */` 都在，函数签名和参数名可信；
3. `.d.ts` **不在**发行树里（`lib/types/*.js` 是运行时镜像，不是声明文件）。但**npm 包是带的**：`package.json` 的 `types` 指向 `lib/types/index.d.ts`，`files` 里也列了 `lib/types/**/*.d.ts`；实测 `dsh-jobs@0.2.1-rc.2` 的 tarball 里 6 个 `.d.ts` 齐全。所以想要类型定义：`npm pack @deepseek-ai/dsh-jobs@0.2.1-rc.2`（或直接 `npm i`）拿到完整声明；桌面发行树是被剥离过的，别在 `app.asar` 里找。

> 版本选择注意：这些包的 npm `latest` 是 `0.0.1-rc.1`（历史遗留标签），与桌面版对应的线在 **`next`** 标签上（`0.2.1-rc.2`）。装测试辅助包时尤其容易踩：`npm i @deepseek-ai/dsh-agent-loop-testkit` 会给你 `0.0.1-rc.1`，必须写 `@0.2.1-rc.2` 或 `@next`。

> 一个反直觉的事实：**文档以包为单位，不以子系统为单位**。想知道"工具怎么注册"，答案在 `dsh-tools/README.zh.md`；想知道"它什么时候跑"，答案在 `dsh-agent-loop/README.zh.md`。仓库里那种 `docs/subsystems/tools.zh.md` 是给贡献者的地图，发行树没有。

## 四、用户态 profile 树：插件真正的落地处

```
$DSH_HOME/
├── cordis.patch.yml              # 机器本地偏好，所有 profile 共享（层 3）
└── profiles/
    └── web/                      # dsh --profile web
        ├── package.json          # {"dsh":{"profile":{"bundles":[...]}}}
        ├── cordis.patch.yml      # 本 profile 的覆盖层（层 2）
        ├── node_modules/         # 插件真正被解析的地方
        └── .plugin-manager/      # dsh plugin 的运行记录与日志
```

四条要点：

1. **profile 名即目录名**，`dsh --profile demo` 找 `$DSH_HOME/profiles/demo`；不存在时由 `dsh plugin` 初始化（以 `@deepseek-ai/dsh-base` 为第一个 bundle）。
2. **模块解析从 profile 目录出发**：patch 行里的 `name` 是包名时，Node 从 `<profile>/node_modules` 往上找；是路径时按 **patch 文件所在目录** 解析 `./`、`../`（绝对路径与 file URL 直接使用）。这条同时解释了"为什么相对路径不是错的、但很容易错"——见 002 的修订与 016 的坑表。
3. **patch 是"行"的集合，不是 JSON 深合并**：按 `id` 定位，整行替换（含整块 `config`）。
4. **升级/重装不会动你的 profile**：`dsh` 本体升级后，profile 里的 `node_modules` 仍是你的选择，这正是"插件生态"的物理基础。

## 五、一个能力该写在哪：目录决策表

| 你想做的事 | 该放哪 | 不该放哪 |
|-----------|--------|----------|
| 给模型加一个工具 | 独立插件包（`defineTool` + `ctx.tools.register`） | `packages/core/tools`（那是框架） |
| 换掉某个能力的实现（如检索） | 同服务的第二个 Provider 包 | 改 `dsh-kb-lite` 内部 |
| 在 Web UI 加一个面板 | 双面包（`dsh.client` + `lib/client.js`） | 往宿主端塞 DOM 字符串 |
| 改默认模型 / 权限 | profile 的 `cordis.patch.yml` | 改 `dsh-base/cordis.patch.yml`（会被升级覆盖） |
| 加一条启动参数 | 提供 `cmdlineArgs` 消费方的 bundle 行 | 改 `dsh-cmdline` |
| 给团队做一套预置 | 自己的 profile 模板（`--from-default-profile` 起手） | 硬编码在插件里 |

## 六、常见误解（真实踩坑现场）

- **"我在 `node_modules` 里改一行就行了"**：HMR 会把这个包当"磁盘文件变了"处理并在下次重组时替换它；重启后改动全丢。要试错请用 overlay 指向源码，或 `link:` 安装。
- **"`$DSH_HOME` 就是仓库根"**：不是。仓库根是构建环境，`$DSH_HOME` 是运行时状态（profile、会话、凭据）。两者可以毫无交集。
- **"`packages/bundle/dsh-base` 是个库"**：它是**一份静态 patch 文档**（只含 `cordis.patch.yml`，`lib/index.js` 只有 11 字节），不挂服务、不发事件。你要改默认值，改自己的 profile patch，而不是改它。
- **"`dsh-client-ui-*` 是给宿主用的 UI 组件库"**：它们全是浏览器侧插件，只在 Web 客户端里运行，宿主进程 import 不到。

## 小结

- 三套目录三种真相：源码仓库（可改，需构建）、发行树（只读，产物 + README）、profile 树（持久，插件落地处）；
- 仓库按领域平铺：`packages/core`（8）+ `packages/boot`（6）是插件作者的必读层，`vendor/*` 是 ABI，`packages/bundle/*` 是组合层；
- 发行树只有 `lib/` + `README.zh.md`，没有 `src/` 也没有 `.d.ts`——**核对 API 靠 README 与 JSDoc**；
- profile 目录 = 模块解析起点 = patch 层 2 = `dsh plugin` 的工作目录；
- 下一 篇讲这套结构背后的设计哲学：为什么内核什么都不会，却能长出一切。

---

**系列导航**：[001 系列导读](/posts/tech/ai/agent/deepseek-harness-dev-001/) · [下一篇：012 设计哲学与核心抽象](/posts/tech/ai/agent/deepseek-harness-dev-012/)
