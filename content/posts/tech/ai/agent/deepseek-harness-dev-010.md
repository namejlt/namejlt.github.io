---
title: "AI Agent-deepseek-harness插件开发010 打包发布与工程实践"
date: 2026-10-19T14:30:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "打包", "发布", "npm", "最佳实践"]
---

## 概述

收官篇。前九篇解决了「怎么写」，本篇解决「怎么交付」：把 overlay 里的源码打包成用户一行命令就能装的组合包（bundle），经三条分发路径发布，并用测试与 checklist 保证质量。最后给出全系列最佳实践清单。

## 一、两个概念，两种 Manifest

安装机制建立在两个概念上，都由一份 `package.json` 描述，但 `dsh` 键下的 manifest 种类不同：

| 概念 | manifest | 回答的问题 |
|------|----------|------------|
| 组合包（bundle） | `dsh.bundle` | 这个包贡献什么配置层？ |
| profile | `dsh.profile` | 这套可启动组合由哪些 bundle 按什么顺序组成？ |

**bundle 是你编写并分发的东西；profile 是用户启动的东西。没有东西同时是两者。**

## 二、打包 Bundle

### 2.1 目录结构

```
kb-lite/
├── package.json       # 声明 dsh.bundle
├── cordis.patch.yml   # 安装后被 profile 列表引用时应用的层
├── lib/               # 构建产物（TS → JS）
│   └── index.js
└── src/
    └── index.ts       # 源码
```

### 2.2 package.json

```json
{
  "name": "dsh-kb-lite",
  "version": "0.1.0",
  "type": "module",
  "main": "lib/index.js",
  "types": "lib/index.d.ts",
  "files": ["lib", "cordis.patch.yml"],
  "scripts": {
    "build": "tsdown src/index.ts --format esm --dts",
    "prepublishOnly": "pnpm build"
  },
  "dependencies": {},
  "peerDependencies": {
    "@deepseek-ai/cordis": "*",
    "@deepseek-ai/dsh-tools": "*"
  },
  "devDependencies": {
    "@deepseek-ai/cordis": "*",
    "@deepseek-ai/dsh-tools": "*",
    "tsdown": "latest",
    "typescript": "latest"
  },
  "engines": {
    "dsh": ">=0.2.0 <0.3.0"
  },
  "dsh": {
    "bundle": {
      "patch": "./cordis.patch.yml"
    }
  }
}
```

三个关键点：

1. **`dsh.bundle.patch`** 指向本包贡献的 patch 文件；
2. **`engines.dsh`** 用 SemVer 范围声明支持的 DSH 版本——安装与启动会拒绝不满足范围的包（没有该字段时退而检查 `@deepseek-ai/dsh*` peer 依赖范围）；
3. **共享实例的 dsh 包**（cordis/dsh-tools）同时放 `peerDependencies` 与 `devDependencies`：peer 保证运行时用宿主副本，dev 供类型检查与独立测试。

### 2.3 cordis.patch.yml

patch 条目里，插件行**按包名**引用（而非源码路径），Node 模块解析才能找到已安装的代码：

```yaml
- insert:
    - id: kb-lite
      name: dsh-kb-lite
      config:
        storageDir: '~/.dsh/kb-lite'
        chunkSize: 500
```

patch 也接受有序文件列表（同一层按序应用）：

```json
"dsh": { "bundle": { "patch": ["./base.patch.yml", "./web.patch.yml"] } }
```

### 2.4 没有 dsh.bundle 的包

仍然可以安装，但只作为普通依赖：`dsh plugin` 会打印警告，不激活任何层。**供其他插件 import 的库**（如 Service Definition 包）就用这种格式。

## 三、安装进 Profile 与四层加载顺序复盘

```sh
dsh plugin --profile demo add ./kb-lite      # 安装本地 checkout
dsh --profile demo --dump-config             # 先验证层，不启动
dsh --profile demo                           # 启动
dsh plugin --profile demo remove dsh-kb-lite # 移除依赖与层
```

首次安装会初始化 profile（`@deepseek-ai/dsh-base` 作为第一个 bundle），并把你的包追加进 `dsh.profile.bundles`：

```json
{
  "name": "dsh-profile-demo",
  "private": true,
  "dependencies": {
    "dsh-kb-lite": "link:/path/to/kb-lite"
  },
  "dsh": {
    "profile": {
      "bundles": ["@deepseek-ai/dsh-base", "dsh-kb-lite"]
    }
  }
}
```

四层顺序复盘（后胜出，整行替换）：

```
1. bundles 的 patch（dsh-base 先行，其余按加入顺序）
2. profile 的 cordis.patch.yml
3. $DSH_HOME/cordis.patch.yml
4. --patch overlay（argv 顺序）
```

推论再强调一次：**你的 patch 可以按 id 覆盖前面层的行，但必须重述全部键**；用户也能在你之上覆盖——所以默认值要选「用户大概率保留」的值。

### 3.1 让 bundle 持有自己的命令行（进阶）

定义了可运行应用的 bundle 可挂一个提供方插件，经 commander 解析自有 flag，无需改启动器：

```yaml
- id: kb-startup
  name: 'dsh-kb-lite/startup'
```

```ts
export const inject = ['cmdlineArgs']
// 用自己的 commander program 调 parseCmdline，在 action 里提供服务
```

受这些参数配置的行可用 `!!js` 读服务并带回退：

```yaml
- id: kb-app
  name: '@example/kb-app'
  inject: [kbStartup]
  config:
    port: !!js ctx.kbStartup.port ?? 8080
```

日常插件用不到，知道有这条扩展路径即可。

## 四、三条分发路径

| 路径 | 命令 | 构建要求 | 适用 |
|------|------|----------|------|
| npm | `pnpm publish` 后 `dsh plugin add dsh-kb-lite` | 发布时构建好 `lib/` | 正式分发（推荐） |
| tarball | `pnpm pack` 后 `dsh plugin add ./dsh-kb-lite-0.1.0.tgz` | 同左 | 离线/内网分发 |
| git | `dsh plugin add github:you/kb-lite` | **需要 prepare 脚本 + 用户授权** | 快速分享源码 |

### 4.1 git 分发的两道坎

git 安装拉取的是源码，**没有任何环节运行你的 build 脚本**，TS 包到手没有 `lib/` 会直接加载失败。两边各做一件事：

**作者侧**：提供自包含的 `prepare` 脚本（pnpm 在 git 安装后运行），不能假设存在 monorepo checkout：

```json
"scripts": {
  "prepare": "tsdown src/index.ts --format esm --no-dts"
}
```

**用户侧**：pnpm ≥10 在显式允许前拒绝运行 git 依赖的 prepare 脚本。首次 add 会失败，dsh 会提示修法——把 pnpm 打印的确切包键复制进该 profile 的 `pnpm-workspace.yaml`：

```yaml
allowBuilds:
  dsh-kb-lite: true
```

然后重新 add。**安全认知**：这项授权 = 允许该包代码在安装时于你的机器执行，且不在 agent 沙箱之内。只授权源码可信的包，并锁定 commit：

```sh
dsh plugin add github:you/kb-lite#<sha>
```

让后续推送无法悄悄改变实际运行的内容。不想让用户做授权，就分发构建产物（npm 或 tarball）。

## 五、测试策略

社区高质量插件（如 `Nicholaskin/vision-exp-tile` 111 项单测）的共识是三层：

### 5.1 单元测试（纯逻辑）

分词、切块、BM25、判定器、配置解析——全部纯函数化后表驱动测试。前两篇实战已给出范例（`node:test` 零依赖）：

```sh
node --test --experimental-strip-types test/*.test.ts
```

### 5.2 组装覆盖（插件装配）

验证「apply + inject + 注册」在真实 ctx 上跑通：工具数量、schema 形状、事件监听数。官方对面向模型/界面的改动要求按仓库测试策略补齐组装覆盖。

### 5.3 冒烟安装（交付前最后一道）

仿 `PerryLink/dsh-test-drive` 的思路：一次性 `DSH_HOME` profile 内安装 + 校验 + 启动，产出结构化通过/失败矩阵：

```sh
# 冒烟脚本骨架：安装 → 校验配置层 → 启动后用一次真实会话确认工具可见
export DSH_HOME=$(mktemp -d)
dsh plugin --profile smoke add ./dsh-kb-lite-0.1.0.tgz
dsh --profile smoke --dump-config | grep -q 'dsh-kb-lite' && echo 'layer OK'
dsh --profile smoke &   # 启动后让模型调一次 kb_search，确认注册与返回正常
```

## 六、提交社区清单

想进 awesome 列表（`awesome-dsh-plugin` 等）？逐条自查：

- [ ] `package.json` 声明 `dsh.bundle`，`dsh plugin add <your-package>` 可装；
- [ ] `engines.dsh` 范围与当前版本兼容；
- [ ] 提供 README：功能、安装命令、配置项、截图/GIF；
- [ ] 单测通过，冒烟安装通过；
- [ ] 无硬编码密钥；配置项全部进 Config；
- [ ] 语义化版本，CHANGELOG 维护；
- [ ] LICENSE（MIT 为主流）；
- [ ] 仓库打 `dsh-plugin` 标签，按列表模板发 PR。

## 七、全系列最佳实践清单

**配置（003）**

1. 凡是部署可能改的参数，一律进 Config，检验标准：不改代码能改值；
2. 约束写进 schema，让无效配置在加载时响亮失败；
3. 默认值选用户大概率保留的。

**生命周期（004）**

4. `apply` 保持薄：同步、轻量、不抛；
5. 注册即清理；定时器/连接/子进程一律 `ctx.effect`；
6. 有顺序依赖的清理放同一个 effect；
7. 不持模块级可变单例。

**工具（005）**

8. description 写场景与边界，不写实现；
9. 参数扁平优先，结构化数据用 JSON 字符串参数；
10. 返回规范值，人类文案归 render；
11. 领域结果进规范值，基础设施故障才抛异常；
12. IO 必传 `exec.signal`；
13. render/present 纯函数，会变的数据走 `presentationMeta`。

**实战（006/007）**

14. 懒加载 + 单飞初始化；写后即持久化；
15. 事件集成先打印后处理，防御式解析，失败静默；
16. 本地存储用原子写（tmp + rename）。

**架构（008）**

17. waterfall 监听器先 `next()` 后逻辑；
18. 会话事件走 `session/event` 查 `type`，不监听同名 Cordis 事件；
19. 不预防性拆分；出现第二个实现再拆三层。

**安全与交付（009/010）**

20. 策略与工具分离，判定器纯函数化；日志记录侧脱敏；
21. 后台任务：配置开关、任务自有 signal、结构化 jobId；
22. 分发构建产物优先；git 分发锁定 commit 并理解 prepare 授权含义；
23. 三层测试：单测 → 组装 → 冒烟安装。

## 八、全系列代码索引

| 篇 | 产物 | 位置建议 |
|----|------|----------|
| 006 | `kb-lite`（知识库检索） | 独立仓库 `dsh-kb-lite`，src/index.ts + test/kb.test.ts |
| 007 | `memory-lite`（跨会话记忆） | 独立仓库 `dsh-memory-lite`，src/index.ts + test/memory.test.ts |
| 008 | `dsh-fetch` seam 三包 | monorepo：dsh-fetch / dsh-fetch-local / dsh-tool-fetch |
| 009 | `audit-lite`（审计 + 扫描） | 独立仓库 `dsh-audit-lite` |
| 010 | bundle 工程化配置 | 各仓库 package.json / cordis.patch.yml |

## 九、结语

回看学习路线：001 建立心智模型 → 002-004 跑通开发循环与生命周期 → 005 掌握工具契约 → 006/007 两个完整插件 → 008-009 架构与安全 → 010 交付上线。官方文档是唯一权威来源，本系列所有 API 均以其与仓库源码为准；遇到预览版接口变化时，用「先 dump / 先打印 / 读生成签名」三板斧校准，而不是照抄旧文。

接下来最好的练习：把 `kb-lite` 按第十篇打包发布，提 PR 进社区列表——那一刻，你就是插件生态的贡献者了。

---

**系列导航**：[001 系列导读](./deepseek-harness-dev-001.md) · [上一篇：009 安全与后台任务](./deepseek-harness-dev-009.md) · 本篇为系列完结篇
