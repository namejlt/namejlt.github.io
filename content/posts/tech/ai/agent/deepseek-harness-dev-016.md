---
title: "AI Agent-deepseek-harness插件开发016 原理篇⑥：插件开发全流程实战手册"
date: 2026-10-20T10:38:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "调试", "测试", "发布"]
---

## 概述

前面十五篇把原理讲完了，这一篇只讲**动作**：从空目录到能被别人 `dsh plugin add` 的包，中间每一步该敲什么、该看什么、失败时先查什么。它是 002（环境）、010（打包）两篇的合并+升级版，并补上 010 没覆盖的调试与测试链路。

目标：**任何一段代码，你都能说清它失败时会以什么形态出现**。

## 一、先做决策：你要写哪一种插件

```
要影响模型行为？ ──是──▶ 宿主侧工具插件（defineTool + ctx.tools.register）
      │否
      ├─ 要响应/拦截某类事件？ ──▶ 宿主侧事件插件（ctx.on / pre-execute / guard）
      ├─ 要给别的插件提供能力？ ──▶ 宿主侧服务插件（Service 子类 + isolate/intercept）
      ├─ 要在 Web UI 里出现？ ──▶ 双面包（dsh.client + lib/client.js）
      └─ 只是改默认值/开关？ ──▶ 不写插件，写 profile patch
```

最后一行最重要：**能靠 patch 解决的不要写插件**。配置组合是声明式的、可被用户覆盖、不引入代码风险；插件是最后手段。

## 二、脚手架：最小可用包

```
dsh-kb-lite/
├── package.json
├── tsconfig.json
├── cordis.patch.yml        # 作为 bundle 时被 profile 引用（层 1）
├── src/
│   ├── index.ts            # 宿主侧入口
│   └── client.ts           # 仅双面包需要
├── test/
│   └── kb.test.ts
└── lib/                    # 构建产物（不入 src）
```

```json
{
  "name": "dsh-kb-lite",
  "version": "0.1.0",
  "type": "module",
  "main": "lib/index.js",
  "types": "lib/types/index.d.ts",
  "files": ["lib", "cordis.patch.yml"],
  "scripts": {
    "build": "tsdown src/index.ts --format esm --dts",
    "prepublishOnly": "pnpm build"
  },
  "peerDependencies": { "@deepseek-ai/cordis": "*", "@deepseek-ai/dsh-tools": "*" },
  "devDependencies": {
    "@deepseek-ai/cordis": "*",
    "@deepseek-ai/dsh-tools": "*",
    "@deepseek-ai/dsh-agent-loop-testkit": "*",
    "tsdown": "latest",
    "typescript": "latest"
  },
  "engines": { "dsh": ">=0.2.0 <0.3.0" },
  "dsh": { "bundle": { "patch": "./cordis.patch.yml" } }
}
```

四条取舍写进注释里省得忘：

1. **`peerDependencies` + `devDependencies` 双写**（cordis / dsh-tools）：运行时用宿主那份（否则两份 Cordis = 两套 context，服务永远对不上），开发时供类型检查；
2. **`files` 只放 `lib` 与 patch**：把 `src` 发出去会让人误以为可以直接 import；
3. **`engines.dsh` 一票决定兼容性**（有它就不看 peer 范围），选保守区间；
4. **测试相关包放 devDependencies**：`@deepseek-ai/dsh-agent-loop-testkit`（组装测试）、`@deepseek-ai/dsh-client-test-runtime`（客户端测试）。两个包都在 npm 上公开（各 36 个版本，`0.2.1-rc.2` 在列），但 **`latest` 标签停在 `0.0.1-rc.1`，宿主对应的线在 `next`**——写 `npm i @deepseek-ai/dsh-agent-loop-testkit` 会装错版本，必须 `@0.2.1-rc.2` 或 `@next`。

> 本机发行树里**没有** `src/` 与 `.d.ts`，所以 `types` 指向的 `lib/types/**` 只有你自己构建才会存在（npm 上的官方包是带的，见 011 §3）。若只在本地开发，`"types"` 可先省略。

## 三、开发循环：三条路径，各有适用面

| 路径 | 命令 | 适用 | 代价 |
|------|------|------|------|
| A. overlay 直指源码 | `dsh web --patch ./dev.yml`（`name` 指 `./src/index.ts` 或绝对路径） | 迭代最快，不打包、不安装 | 需要运行时能加载 TS（源码 checkout 环境）；不进 profile，别人用不了 |
| B. 本地 link 进 profile | `dsh plugin --profile dev add ./dsh-kb-lite` | 验证"用户装完是什么样" | 要先 build；profile 被改 |
| C. 注册表/git 安装 | `dsh plugin --profile dev add dsh-kb-lite@0.1.0` | 发布前最终验证 | 要走 pnpm、可能触发兼容性门禁 |

推荐节奏：**A 写代码 → B 验安装形态 → C 验发布形态**。三者别混着用，否则"到底是代码问题还是安装问题"永远说不清。

### 3.1 源码热更新的正确开法

默认 `hmr` 行是 `root: []`（只热重载配置）。开发期要在 patch 里覆盖它：

```yaml
# dev.yml
- id: hmr
  disabled: false
  config:
    root: ["."]        # 或写你的插件源码目录
```

然后记住两个时间常数：`debounce: 100ms`、配置文件监听默认 `awaitWriteFinish: true`（约 2 秒稳定窗口）。**保存后等两秒**再判断"有没有生效"。

### 3.2 `--patch` 里路径怎么写才对

- **绝对路径**：永远正确；
- **`./`、`../` 相对路径**：按**该 patch 文件所在目录**解析并转成 file URL（不是按 profile 目录、不是按 cwd）；
- **包名**：从 `<profile>/node_modules` 往上做 Node 解析。

早期版本的 002 篇写的"必须绝对路径"表述不准确——相对路径是支持的，只是**基准容易搞错**。生产配置建议直接用包名（路径解析交给 Node），开发 overlay 用绝对路径。

## 四、调试：六个动作，按顺序用

| 动作 | 命令/写法 | 回答什么 |
|------|-----------|----------|
| ① 看最终配置 | `dsh --profile <p> --dump-config` | 我的层生效了吗？哪一层覆盖了谁？ |
| ② 看配置 schema | `dsh --profile <p> --dump-config-schema` | 这个字段到底叫什么、类型/默认值是什么 |
| ③ 看启动日志 | 终端输出（fail-loud 带标签） | 哪个 entry 失败、失败在 import 还是 apply |
| ④ 先打印后处理 | `ctx.on('session/event', (session, e) => console.dir(e, { depth: 4 }))` | 事件载荷的真实形状（**参数是 `(session, event)`**） |
| ⑤ 单工具验证 | 直接让模型调一次，看 `tools/result` 日志 | 参数校验、规范值、渲染是否符合预期 |
| ⑥ 泄漏自查 | HMR 连改 5 次，看加载/卸载是否严格配对 | effect 是否漏接管资源 |

`--dump-config` 的输出是**可加载的 YAML**，每段带 `# == <来源>` 注释，`!!js` 原样渲染。它的字节稳定性没有承诺（app-boot 开发备注明确说"不承诺跨包版本字节稳定"），所以**别把 dump 结果喂给脚本做自动化断言**。

### 4.1 三种最常见的"没反应"

| 症状 | 先查 | 再查 |
|------|------|------|
| 插件不跑、无报错 | `inject` 的服务有没有提供方 | 该行是否 `disabled: true`（含 `!!js` 求值结果） |
| 改了没变化 | HMR `root` 是否配了源码目录 | 监听窗口（2s）过了吗；是否改的是 `lib/` 而非 `src/` |
| 改了配置但行为没变 | 是否 volatile 字段（默认不是） | 是否整行替换把别的键冲掉了 |

## 五、测试：三层，各有各的假

| 层 | 测什么 | 怎么做 | 不测什么 |
|----|--------|--------|----------|
| 单元 | 纯函数（分词、切块、打分、判定器） | 普通测试框架，零 DSH 依赖 | 不验证插件装配 |
| 组装 | `apply` 里的注册是否正确、依赖是否声明对 | 新建 `Context` → `ctx.plugin(yourPlugin, config)` → 断言服务/工具/事件可见性 | 不验证真实模型与 UI |
| 冒烟 | 装进真 profile 能否启动、能否被模型调用 | 路径 B/C 安装 → `--dump-config` → 启动 → 真实调一次 | 不做穷举 |

组装层的关键写法（Cordis 原生能力，**不依赖测试框架**）：

```ts
import { Context } from '@deepseek-ai/cordis'

const ctx = new Context()
await ctx.plugin(myPlugin, { greeting: 'hi' })   // 返回值是 thenable，失败会 reject
// 断言：ctx.get('myService')、ctx.tools.schemas(...)、ctx.bail('my-event', ...)
await ctx.fiber.dispose()                        // 卸载并结算所有 effect
```

注意 `ctx.plugin()` 返回的 Fiber 是 **PromiseLike**，`await` 会等到激活完成、失败则 reject——这正是"配置非法就响亮失败"的可测形态。别用 `setTimeout` 猜它什么时候好。

## 六、打包与发布 checklist

```yaml
# cordis.patch.yml —— 用包名引用自己（Node 解析才找得到已安装代码）
- insert:
    - id: kb-lite
      name: dsh-kb-lite
      config:
        storageDir: '~/.dsh/kb-lite'
```

发布前逐条打勾：

- [ ] `dsh.bundle.patch` 指向的文件**在 `files` 里**（漏了 → 安装后层是空的，最难查）；
- [ ] `main` / `exports` 指向 `lib/`，且 `pnpm build` 干净通过；
- [ ] `peerDependencies` 与 `devDependencies` 双写 cordis / dsh-tools；
- [ ] `engines.dsh` 是有效 SemVer 范围（空串/`workspace:` 会被判不兼容）；
- [ ] 双面包额外确认：`exports['./client']` → `lib/client.js`，`dsh.client.platform = 'web'`，`external` 只列基座之外的精确请求；
- [ ] 工具 `description` 写了适用/不适用场景（模型只看这个）；
- [ ] 每个工具都有稳定 `output.schema`，不留"返回字符串内部再解析"的洞；
- [ ] 所有 `setInterval` / watcher / 子进程都在 `ctx.effect()` 里；
- [ ] 密钥不进 patch、不进 README 示例、不进日志；
- [ ] README 里有"这是什么/为什么需要/30 秒跑通"三段；
- [ ] git 分发时锁 commit，并知道 `prepare` 构建脚本需要用户授权。

### 6.1 三条分发路径

| 路径 | 用户命令 | 优点 | 代价 |
|------|----------|------|------|
| npm 注册表 | `dsh plugin --profile web add dsh-kb-lite` | 最常见；`saveExact` 精确锁定 | 需要发布流程；本机构建无 |
| 本地目录 / tarball | `dsh plugin --profile web add ./dsh-kb-lite` | 开发验证；保存为 `link:` 依赖 | 只在开发者机器上有意义 |
| git | `dsh plugin --profile web add github:you/dsh-kb-lite#<commit>` | 免发布 | 需要 `prepare` 构建；安装前有 `git ls-remote` 连通性检查（默认 5s 超时） |

`dsh plugin` 与 Web 里的 Plugin Manager 是**同一套操作**（共用 `operations.ts`）：都会走 pnpm、都会做兼容性检查、都由 HMR 串行化重组。差别只在于有 HMR 时它能当场生效，没有时返回 `restart-required`。

## 七、坑表（20 条，按出现频率排）

| # | 坑 | 症状 | 解法 |
|---|----|------|------|
| 1 | 覆盖行只写改动键 | 其他配置"神秘消失" | 重述全部键（整行替换） |
| 2 | 用了 `ctx.tools` 没 `inject` | `Cannot read properties of undefined` / 读服务抛错 | `export const inject = ['tools']` |
| 3 | 监听 `session/event` 参数写成一个 | 拿到 Session 却当 event 用 | 签名是 `(session, event)` |
| 4 | 以为 `presentResult` 能出 Web 卡片 | UI 没变化 | Web 走客户端 renderer（`tool.call.toolview`） |
| 5 | 改源码没生效 | 行为不变 | HMR `root: []` 默认不监听源码；配 `root` 或重启 |
| 6 | 以为改配置一定重载 | 有时生效有时不 | volatile 字段就地提交；你的字段通常不是 |
| 7 | `Config` 导出普通对象 | `Standard Schema` 校验报错 | 导出 Schemastery schema（同名 `Config`） |
| 8 | 空 patch 文件 | 启动失败 | 写 `[]` |
| 9 | 相对路径基准搞错 | 模块找不到 | 相对 patch 文件；生产用包名 |
| 10 | `timeoutMs` 当超时用 | 工具不超时 | 它**只声明**，强制要靠 timeout-policy 包装层 |
| 11 | 领域错误用 `throw` | 模型以为工具坏了 | 业务结果进规范值；基础设施故障才抛 |
| 12 | 注册后改 schema | 行为不确定 | 释放 effect 后重新注册 |
| 13 | `apply` 里做重活/长 await | 启动慢、易 FAILED | 移到 `ctx.effect()` 或 `execute` |
| 14 | 模块顶层存可变状态 | HMR 后幽灵实例 | 状态放 `apply` 闭包或服务里 |
| 15 | 裸 `setInterval` / `fs.watch` | 卸载后仍在跑，双份日志 | `ctx.effect()` 返回清理函数 |
| 16 | 监听器里改 `exec.arguments`（pre-execute） | 日志与实际不符；其实不允许 | 改输入请用 `agent/pre-step` 或工具参数层 |
| 17 | client 包没 build | 激活失败并列出缺失 bundle | `pnpm run build`；dev 时开 `dev:web` |
| 18 | `dsh.client.external` 写了基座内模块 | 组合阶段拒绝 | 只写基座之外的精确请求 |
| 19 | `engines.dsh` 写 `workspace:^` | 判为不兼容 | 写真实 SemVer 范围 |
| 20 | 手动在 profile 里 `pnpm add` | 绕过兼容检查，问题延后爆发 | 用 `dsh plugin add` |

## 八、验收清单（交付前最后一遍）

- [ ] `dsh --profile <p> --dump-config` 能看到你的层，且值正确；
- [ ] 冷启动无警告（`skippedBundles`、required 失败、`invalid config:` 都没有）；
- [ ] 工具能被真实调用一次，`tools/result` 日志里的参数与规范值符合预期；
- [ ] `--dump-config-schema` 里你的 Config 字段名/类型/默认值都是想要的；
- [ ] HMR 连改 5 次，加载/卸载严格配对，无重复副作用；
- [ ] 卸载后（`fiber.dispose()` 或禁用该行）无残留：无定时器、无 watcher、无子进程、无端口占用；
- [ ] 双面包：清空 `lib/client.js` 后启动，错误信息是否符合 README 的预期（能被别人一眼看懂）；
- [ ] 换一个全新 profile 走一遍 `dsh plugin add`，从零可用。

## 小结

- 先决策"该不该写插件"：能靠 patch 解决的不要写代码；
- 三条开发路径各有适用面：overlay（快）、link（真）、registry（最终）；
- 调试六动作：dump-config / dump-config-schema / 启动日志 / 先打印后处理 / 单工具验证 / 泄漏自查；
- 测试三层：纯函数单测 → `ctx.plugin` 组装 → 真 profile 冒烟；
- 打包七要点：`files` 含 patch、`peer`+`dev` 双写、`engines.dsh`、双入口、`external` 精确、密钥不进 patch、README 三段；
- 20 条坑表贴在编辑器旁边，能省掉一半调试时间。

---

**系列导航**：[001 系列导读](/posts/tech/ai/agent/deepseek-harness-dev-001/) · [上一篇：015 双端插件](/posts/tech/ai/agent/deepseek-harness-dev-015/) · 本篇为原理篇完结
