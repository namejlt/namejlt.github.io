---
title: "AI Agent-deepseek-harness插件开发014 原理篇④：插件加载、组合与热更新"
date: 2026-10-20T10:30:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "Cordis", "HMR", "Loader"]
---

## 概述

001/002/003 三篇讲了"四层配置"，010 篇讲了"怎么打包"。但中间那段——**从敲下 `dsh web` 到一个插件真的跑起来**——一直是黑箱。本篇拆开它：环境层、profile 解析、patch 组合、Loader entry、模块导入、Fiber 生成、依赖门控、`apply` 执行。然后讲热更新的**三种粒度**，这是绝大多数"改了没反应 / 莫名其妙重载"问题的答案。

## 一、启动链路：七个阶段

```
① dsh CLI（apps/cli）
     └─ 解析 argv → dsh-cmdline 冻结成不可变交接对象（cmdlineArgs 服务）

② dsh-app-boot .boot()
     ├─ loadLayeredEnv：读一次调用目录与 $DSH_HOME 下的 .env（不搜父目录）
     ├─ 解析 profile：$DSH_HOME/profiles/<name>/package.json 的 dsh.profile.bundles
     ├─ 组合 patch 层（见第二节），在每个组合包层解析 dsh.bundle.patch
     ├─ 版本门禁：engines.dsh 或 peer 范围比对运行时版本
     └─ 挂载 Loader 根 Include

③ Loader（vendor/loader）建立 EntryTree
     └─ 每个 entry：id / name / config / disabled / inject / group
        !!js 表达式在此求值（profileContext 已存在之后）

④ import 模块
     └─ 失败 = 该 entry failed（非 required 则警告继续）

⑤ 生成 Fiber（Cordis registry）
     └─ runtime = { name, callback, Config }，fibers 列表登记

⑥ 依赖门控（inject → epoch）
     └─ 服务齐 → LOADING → apply(ctx, config)
        服务缺 → PENDING（静默等待）

⑦ 应用就绪 → required entry 审计 → 打印 URL / 打开浏览器
```

两个细节值得单独记：

- **`.env` 只读一次、只读两个目录**（调用目录 + `$DSH_HOME`），不向上搜索、不跟随之后的 workspace 切换。
- **required entry 失败 = 整个应用拆除 + 非零退出**。全局 required 列表覆盖：`agent-loop`、`webserver`、`web-runtime`、`modules`、`connection`、`headless-runner`、`acp`、`sdk-jsonrpc-server`；profile 里不存在的 required id、或被禁用的 required 行，不影响启动。

## 二、patch 组合：四层，整行替换

```
层 1  bundle 层      dsh.profile.bundles 顺序；每个 bundle 的 dsh.bundle.patch
                     （一个文件或有序文件列表 → 同一层内按序拼接）
层 2  profile 层     $DSH_HOME/profiles/<name>/cordis.patch.yml
层 3  home 层        $DSH_HOME/cordis.patch.yml
层 4  overlay 层     每个 --patch <path>，按 argv 顺序
```

语义只有一条，但要背到反射级：**按 `id` 定位行，整行替换（含整个 `config`），绝不深合并**。`dsh-base` 自己的注释就是这么写的，`app-boot` 的限制列表也再写了一遍。

由此推出三个工程结论：

1. 覆盖别人的行，**必须重述想保留的每一个键**；
2. 想"删掉某行的某个配置项"，只能重写整块（省略即等于不传，回落到 schema 默认值）；
3. **不是"后写的行生效"，而是"同 id 的最后一次写入生效"**。行序没有加载语义——激活是服务可用性驱动的（`dsh-base/cordis.patch.yml` 顶部注释原文）。

### 2.1 行语法全集

```yaml
- insert:
    - id: kb-lite                      # 必填：定位与覆盖的键
      name: dsh-kb-lite                # 模块说明符：包名 / 绝对路径 / ./ 相对（相对 patch 文件）
      inject: [tools]                  # 该行额外的必需服务（等价于插件 static inject 的补充）
      disabled: false                  # 停下并阻止启动
      isolate: { shell: true }         # true = 该行独享该服务域；字符串 = 同标签共享
      intercept:                       # 给下层插件的该服务注入配置
        shell: { timeoutMs: 5000 }
      config:                          # 整块替换的语义单位
        storageDir: '~/.dsh/kb-lite'
    - id: crew                         # 分组行
      name: '@deepseek-ai/cordis-plugin-group'
      group: true                      # config 是子 entry 列表
      config:
        - id: inner
          name: ./inner.ts             # 嵌套 id 为 crew:inner
```

> `isolate` / `intercept` 是 **Loader entry 的选项**（`isolate?: Dict<true | string>`），在 patch context 阶段生效；插件内部要做同类隔离得用 `ctx.isolate(name, label)` / `ctx.intercept(name, config)`。

`!!js` 表达式在你需要"按部署环境取值"时非常有用，但要注意求值时机：

```yaml
- id: hmr
  name: '@deepseek-ai/dsh-hmr'
  disabled: !!js "!ctx.get('profileContext')"   # profileContext 存在才启用
  config:
    root: []
```

- 表达式在 **Loader 建立 entry 时求值**，此时 `profileContext` 等服务已经存在（`dsh-web-app` 的注释明确说"Loader 在它存在之后才求值这些表达式"）；
- `disabled` 表达式在启动与嵌套 Include 处按同样方式求值，HMR 重组时也会重新求值；
- 别在表达式里做副作用——它可能被求值多次。

### 2.2 空 patch 文件是错误，不是"没配置"

一个只含注释（或完全为空）的 patch 文件会让启动**失败**。要表达"我什么都不加"，写 `[]`。这是 003 篇没提到、但第一次手写 overlay 就会撞上的坑。

## 三、manifest 全集：一个 `package.json` 里的四个域

| 字段 | 谁读 | 语义 | 易错点 |
|------|------|------|--------|
| `dsh.manifestVersion` | 声明式 | 格式版本，当前为 `1` | 与 npm 包版本、Session 格式版本**互不相干** |
| `dsh.bundle.patch` | app-boot | 一个路径或**有序列表**，相对包根 | 列表是"同一层按序应用"，不是多个层 |
| `dsh.profile.bundles` | app-boot | 这套组合由哪些 bundle 按什么顺序组成 | 只属于 profile；bundle 不能声明它 |
| `dsh.client` | client-modules | `{ platform: 'web', inject: [...], external: [...] }` | 有它就必须有构建好的 `lib/client.js` |
| `engines.dsh` | app-boot | SemVer 范围；**声明了它，peer 范围就被忽略** | 空串/非法范围 = 不兼容；`workspace:` 在这里非法 |

版本门禁的完整口径（`app-boot` README）：

1. 有 `engines.dsh` → **只看它**，忽略 `peerDependencies`；
2. 没有 → `@deepseek-ai/dsh` 与所有 `@deepseek-ai/dsh-*` 的 peer 范围都必须匹配运行时版本；源码工作区里的 `workspace:^ | ~ | *` 映射到同一运行时；
3. 预发布版本参与范围匹配；
4. 两者都没声明 → 不施加约束；空/非法范围 → 视为不兼容。

被拒的普通行会变成游离的 `disabled: true` 行（模块永不 import）；被拒的 bundle 整体跳过，列进 `skippedBundles` 并每次启动报告一次。**这不是安全沙箱**——它只防版本错配，不防恶意代码。

需要强行安装不兼容版本时，走显式豁免：

```sh
dsh plugin --profile web version-exemptions
dsh plugin --profile web allow-version pkg@1.2.3 --dsh-version 0.2.1-rc.2 --accept-risk
dsh plugin --profile web revoke-version pkg@1.2.3 --dsh-version 0.2.1-rc.2
```

## 四、Fiber 的三种"重来"：epoch、重载、重建

理解这三者的差别，能省掉大量调试时间。

| 触发 | 发生了什么 | 你的代码感知 |
|------|-----------|--------------|
| **epoch 变化**（服务重新提供/消失、`fiber.update()`、HMR 重组） | 依赖集合重算；从 `PENDING` 重新走 `LOADING → ACTIVE` | `apply` 再次执行，旧 effect 已结算 |
| **volatile 配置变更** | Loader 比对配置，只有 volatile 字段变了 → **就地提交**，不重载 | `apply` **不会**再执行；消费方通过 `loader/volatile-update` 感知 |
| **profile 重组**（patch / bundle 列表变化） | 新一代组合预检 → 停止受影响 entry → 导入新文件 → 启动 | 受影响插件 `apply` 再执行；未变化的 entry 不重复导入 |

volatile 是 DSH 里最容易误解的机制：`Entry.update` 严格比较 entry 选项，若**只有 config 变了且该字段被 schema 标为 volatile**，就走 `_commitVolatile` 把新值写进运行中的引用，并发出 `loader/volatile-update`。典型例子是 agent-loop 的 `maxParallelToolCalls`——它在"下一组工具开始时"读取，所以改它不需要重启。**你自己插件的配置默认不是 volatile**，改了就会重载。这是设计选择：volatile 要求"读的时候取当前值"，而不是在 `apply` 里缓存。

## 五、热更新：HMR 的三层能力

`dsh-hmr` 把**模块替换、Include 刷新、profile 配置变更**放进同一个协调队列（`runExclusive`），包安装则刻意在队列外（pnpm 与 HMR 不同步，plugin-manager 另有 manifest 写锁）。

第一件必须知道的事：**base 组合里 `hmr` 行的配置是 `root: []`**。

```yaml
# dsh-base/cordis.patch.yml（节选）
- id: hmr
  name: '@deepseek-ai/dsh-hmr'
  disabled: !!js "!ctx.get('profileContext')"
  config:
    root: []          # ← 只监听 profile 配置，不监听源码模块
```

含义：**默认只热重载"配置"，不热重载"你的 .ts 源文件"**。想改源码即时生效，得在你的 profile patch 里覆盖这一行：

```yaml
- id: hmr
  disabled: false
  config:
    root: ["."]        # 模块监听目录；相对 base URL
    # ignored 默认 ["**/node_modules", "**/.*", "cache", "data"]
    # debounce 默认 100ms
```

第二件事：**三层能力各自的边界**。

| 层级 | 触发 | 生效范围 | 不生效的场景 |
|------|------|----------|--------------|
| profile 配置重载 | 改 profile manifest（bundle 列表）、profile patch、home patch | 重新组合，替换/启停受影响 entry | 仅 `package.json` 的依赖字段变化不触发 |
| 模块源码替换 | chokidar 监听 `root` 目录 | 沿 ESM 链接与被替换模块的 CommonJS `children` 传播；受影响 entry 停止后由重组启动 | 宿主依赖图内的模块保持 pending（要求重启）；Worker / TSX loader 线程各有自己的缓存 |
| 客户端 bundle | `pnpm run dev:web` 重建 `lib/client.js` | 浏览器侧按 revision 换单资源 combo URL | 宿主没在跑 dev:web 时不会发生 |

第三件事：**失败语义与"重启要求"**。

- 替换版本导入/启动失败 → 插件被停下、报 `failed` + 加载错误；**旧代码不会恢复**；此后每次重组都会再导入一次，文件修好即启动。
- 需要重启的情况会以 `restart-required` + `restartReason` 报告，取值有 `no-hmr`、`package-moved`，以及"只有重启才能加载的已变化模块"的 HMR 待处理原因。
- 配置监听默认 `awaitWriteFinish: true`（约 2 秒稳定窗口）。**快速连改多次文件，可能要等这一窗口过去**；需要立刻生效时用 Plugin Manager 触发的重组（它不等文件事件）。

## 六、`dsh plugin` 与 HMR 的交互

`dsh plugin` 与 Web 里的 Plugin Manager 共用同一套 `operations.ts`，差别只在"是否有 HMR"：

| 操作 | 有 HMR | 无 HMR |
|------|--------|--------|
| 安装新包 | pnpm 成功后选入，交给 HMR 重组 | 发布 runtime resolution，激活需重启 |
| 删除包 | 先取消选入并完成卸载，再跑 pnpm | 删除成功后发布；已运行的启动型 bundle 仍跑着 → `restart-required` |
| 更新版本 | 必须先卸再装（两步） | 同上，且可能 `already-installed`/`restart-required` |

三条实践建议：**（1）** 别手动在 profile 里 `pnpm add`——绕过兼容性检查（虽然 HMR 会照样替换文件）；**（2）** 改完包记得 `dsh --profile <name> --dump-config` 先看层，再启动；**（3）** 无 HMR 的 profile（headless/sdk/acp 在 YAML 里显式禁用了 `hmr`）改任何东西都要重启。

## 七、失败语义速查

| 现象 | 根因 | 排查入口 |
|------|------|----------|
| 启动直接退出，日志有 `invalid config:` + 逐条 issue | Config schema 校验失败（`ValidationError`） | 修 patch 里的 config 值 |
| 插件一直不跑，也没报错 | `inject` 的服务没人提供 → `PENDING` | 查该服务的提供方是否在同一组合里 |
| 加载报"找不到模块" | `name` 说明符解析失败（相对路径基准、包未安装） | `--dump-config` 看最终 `name`；确认解析基准 |
| 改了源码没反应 | HMR `root: []`（默认不监听源码）/ 监听窗口未过 | 配 `root` 或重启；看 HMR 日志 |
| 报 `skippedBundles` | bundle 版本不兼容 / manifest 读取失败 | 看报告中的包名与版本，决定升级或豁免 |
| 报 `restart-required` | 无 HMR 下的安装/删除，或模块只能在重启时加载 | 重启进程；检查 `restartReason` |

## 小结

- 启动七阶段：CLI → app-boot（env/profile/patch/门禁）→ Loader entry → import → Fiber → inject 门控 → apply；
- 四层 patch，**整行替换**；`!!js` 在 Loader 建 entry 时求值；空 patch 文件 = 启动失败，写 `[]`；
- manifest 四个域各归其主；`engines.dsh` 一票决定 peer 是否被忽略；
- 三种"重来"：epoch 重载、volatile 就地提交、profile 重组——**你的配置默认不是 volatile**；
- HMR 默认只重载配置（`root: []`），源码热替换要显式配 `root`；替换失败不恢复旧代码；
- 下一篇讲另一半世界：浏览器侧插件（`dsh.client`）与动态包。

---

**系列导航**：[001 系列导读](/posts/tech/ai/agent/deepseek-harness-dev-001/) · [上一篇：013 运行原理](/posts/tech/ai/agent/deepseek-harness-dev-013/) · [下一篇：015 双端插件](/posts/tech/ai/agent/deepseek-harness-dev-015/)
