---
title: "AI Agent-deepseek-harness插件开发002 环境搭建与第一个插件"
date: 2026-10-19T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "Cordis", "入门"]
---

## 概述

本篇把开发环境跑起来，写出第一个插件，并建立「改代码 → 看到效果」的开发循环。目标：30 分钟内让终端打印出你自己插件的加载日志，并理解 overlay 机制。文末附 8 个常见坑速查。

## 一、两种开发起点

| 起点 | 适合谁 | 命令形态 |
|------|--------|----------|
| A. 源码 checkout | 想读框架源码、给 Harness 本身做贡献 | `pnpm dsh web --patch ...` |
| B. 已安装 dsh CLI | 只开发插件，不碰框架源码 | `dsh web --patch ...` / `dsh --profile ...` |

两条路径的插件代码完全一致，差别只在启动命令。本教程以源码 checkout 为主（官方第一个插件教程的路径），CLI 差异处会标注。

前置条件：

- Node.js（版本要求以仓库 README 为准，用 LTS）；
- pnpm（Harness 仓库使用 pnpm workspace）；
- 一个 DeepSeek API Key（用于后续让模型调用你的工具，[platform.deepseek.com](https://platform.deepseek.com) 申请）。

## 二、创建 scratch-plugin 项目

在 Harness 仓库根目录执行：

```sh
mkdir -p scratch-plugin/src
```

创建 `scratch-plugin/src/my-plugin.ts`——这是整个系列的第一个插件：

```ts
import type { Context } from '@deepseek-ai/cordis'

export const name = 'hello-plugin'

export function apply(ctx: Context) {
  // 依赖就绪后才会执行到这里
  console.log('[hello-plugin] plugin loaded!')
}
```

三个要点：

1. **`name` 是插件的唯一标识**，日志、排错、卸载都按它索引；
2. **`apply` 是框架的加载入口**，框架传入 `ctx`，你在 `ctx` 上注册一切能力；
3. **`import type` 只引入类型**，编译后不留运行时依赖。

### 2.1 插件的三种形态对比

同一个插件可以写成三种形态，语义等价：

```ts
// 形态一：函数式（推荐默认）
export const name = 'my-plugin'
export function apply(ctx: Context) {}

// 形态二：对象式
export default {
  name: 'my-plugin',
  apply(ctx: Context) {},
}

// 形态三：类式（需要向其他插件提供服务时使用）
import { Service, type Context } from '@deepseek-ai/cordis'
export default class MyService extends Service {
  constructor(ctx: Context) {
    super(ctx, 'myService')
  }
}
```

判断标准：**除非你的插件要向别人提供服务，否则一律函数式**。类式的正确用法在第 008 篇展开。

## 三、用 overlay 把插件挂进 Web UI

在仓库根目录执行 `pwd` 拿到绝对路径，创建 `scratch-plugin/cordis.yml`：

```yaml
- insert:
    - id: hello
      name: '/absolute/path/to/deepseek-harness/scratch-plugin/src/my-plugin.ts'
```

启动 Web UI：

```sh
pnpm dsh web --patch ./scratch-plugin/cordis.yml
```

打开 http://127.0.0.1:3080 ，启动日志里应出现：

```
[hello-plugin] plugin loaded!
```

看到这行，说明：模块解析成功 → Fiber 创建成功 → `apply` 执行成功。整条链路通了。

### 3.1 overlay 机制详解

```
--patch ./scratch-plugin/cordis.yml
        │
        ▼
┌─────────────────────────────────────────┐
│ 层 4：--patch overlay（argv 顺序）        │  ← 你的开发层，最高优先级
│ 层 3：$DSH_HOME/cordis.patch.yml         │  ← 机器本地偏好
│ 层 2：profile 的 cordis.patch.yml        │  ← 用户配置
│ 层 1：bundles 的 patch（base 先行）        │  ← 官方默认
└─────────────────────────────────────────┘
```

- **插件路径必须是绝对路径**：overlay 文件不改变 loader 解析模块路径时使用的 profile 目录，相对路径会按错误的基准解析；
- overlay 只贡献配置，**不改动磁盘上的 profile**，随时可弃，是开发期的安全沙盒；
- 多个 `--patch` 按 argv 顺序应用，后面的胜出。

## 四、声明依赖：inject

如果插件要使用 `tools`、`llm` 等服务，必须声明 `inject`：

```ts
import type { Context } from '@deepseek-ai/cordis'

export const name = 'my-tool-plugin'
export const inject = ['tools']

export function apply(ctx: Context) {
  // 框架保证：ctx.tools 在这里一定就绪
  ctx.tools.register(/* ... */)
}
```

- 未声明的服务**不会**出现在 `ctx` 上，访问即 `undefined`；
- 服务未就绪时插件停在 PENDING，不会执行 `apply`，也不会报错；
- 服务消失（提供方被卸载/替换）时，依赖它的插件会自动卸载，服务恢复后自动重载——**这是框架的兜底，你不需要写防御代码**。

可选依赖用 `ctx.get()` 按需查询：

```ts
export function apply(ctx: Context) {
  const metrics = ctx.get('metrics')
  metrics?.record('plugin_loaded', 1)  // 没有 metrics 服务就静默跳过
}
```

## 五、开发循环与调试

### 5.1 标准开发循环

```
改 src/my-plugin.ts → 保存 →（HMR 自动重载 或 重启）→ 终端看日志 → Web UI 验证
```

开启 HMR（热模块替换）：在 `cordis.yml` 加载 `@deepseek-ai/dsh-hmr` 后，修改插件源文件会触发「卸载旧插件（清理所有注册）→ 重新加载 → 执行新 `apply`」。因为注册都是自动清理的 effect，热替换不会残留旧实例。

```yaml
# cordis.yml 追加一行即可开启（开发期推荐）
- name: '@deepseek-ai/dsh-hmr'
```

### 5.2 三件排错武器

| 武器 | 用法 | 能回答什么 |
|------|------|------------|
| `--dump-config` | `dsh --profile demo --dump-config` | 我的 patch 层到底生效了吗？按层打印最终配置 |
| 启动日志 | 看终端输出 | 插件加载/卸载/异常 |
| Web UI 会话 | 直接和模型对话 | 工具是否可被调用、返回是否符合预期 |

`--dump-config` 会以 `# == <package>` 注释标出每一层，是排查「配置没生效」的第一工具。典型输出：

```
# == @deepseek-ai/dsh-base
- insert:
    - id: hello
      ...
# == dsh-hello-plugin
- insert:
    - id: hello
      config:
        greeting: 'Hi there'
```

### 5.3 常见坑速查表（8 个）

| # | 坑 | 症状 | 解法 |
|---|----|------|------|
| 1 | overlay 里写相对路径 | 模块解析失败 | 改绝对路径 |
| 2 | 用了 `ctx.tools` 但没 `inject` | `Cannot read properties of undefined` | 加 `export const inject = ['tools']` |
| 3 | 覆盖别人的行只写改动的键 | 配置「丢了」其他字段 | 重述该行全部键（整行替换语义） |
| 4 | 以为改文件即时生效 | 行为没变 | 确认加载了 dsh-hmr，或重启 |
| 5 | `Config` 导出普通对象 | 加载失败 | 必须导出 Schemastery schema（第 003 篇） |
| 6 | 端口 3080 被占用 | Web UI 打不开 | 换端口或释放进程 |
| 7 | 多个 overlay 顺序搞反 | 覆盖关系颠倒 | `--patch` 按 argv 顺序，后面的胜出 |
| 8 | 从 git 安装的 TS 包加载失败 | 找不到入口 | 包需要 `prepare` 构建脚本 + 用户授权（第 010 篇） |

## 六、从 overlay 到本地 npm 包

overlay 适合开发期，但迟早要变成可安装的包。先搭好骨架，后续每章都在它上面长：

```
scratch-plugin/
├── package.json          # 私有开发包，后续加 dsh.bundle
├── tsconfig.json
├── src/
│   └── my-plugin.ts
└── cordis.yml            # overlay 保持不变，继续指向 src
```

`package.json` 初始形态：

```json
{
  "name": "dsh-hello-plugin",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "main": "src/my-plugin.ts",
  "scripts": {
    "dev": "pnpm dsh web --patch ./cordis.yml"
  }
}
```

在 Harness 仓库根目录（或任意已装 dsh CLI 的环境）：

```sh
cd scratch-plugin
pnpm add -D @deepseek-ai/cordis @deepseek-ai/dsh-tools typescript
```

> `@deepseek-ai/cordis` 提供 `Context` 等类型；`@deepseek-ai/dsh-tools` 提供 `defineTool`。需要与宿主共享实例的 dsh 包，要同时声明在 `peerDependencies` 与 `devDependencies`（第 010 篇详述）。

这样编辑器补全、类型检查全部就位，开发体验与正式包一致。

## 七、验证清单

完成本章后，逐条确认：

- [ ] `pnpm dsh web --patch ./scratch-plugin/cordis.yml` 能启动，浏览器打开 127.0.0.1:3080
- [ ] 终端出现 `[hello-plugin] plugin loaded!`
- [ ] 改 `name` 重启后日志随之变化（证明加载的是你的文件）
- [ ] `dsh --profile <你的profile> --dump-config` 能看到你的 patch 层
- [ ] 故意写一个不存在的服务名进 `inject`，观察插件停在哪（答案：静默 PENDING，不报错）

## 八、练习

1. 把 hello 插件改成对象式和类式各一份，用 overlay 同时加载三个，观察日志顺序与 Fiber 行为；
2. 给插件加一个 5 秒定时器（先不用 `ctx.effect`，直接 `setInterval`），然后卸载插件，观察定时器是否还在跑——体会第 004 篇「自动清理」要解决的问题；
3. 在 `cordis.yml` 再加一行 `--patch`，验证层覆盖顺序。

## 小结

- 插件 = 导出 `name` + `apply(ctx)` 的 TypeScript 模块，三种形态语义等价；
- overlay（`--patch`）是开发期最高优先级的配置层，路径必须绝对；
- `inject` 声明依赖，框架保证就绪、消失自卸载；
- 排错三件套：`--dump-config`、启动日志、Web UI 会话；
- 下一篇给插件加配置，深入 Schemastery 与四层覆盖语义。

---

**系列导航**：[001 系列导读](./deepseek-harness-dev-001.md) · [下一篇：003 插件配置](./deepseek-harness-dev-003.md)
