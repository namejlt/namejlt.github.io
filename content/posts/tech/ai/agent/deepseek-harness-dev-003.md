---
title: "AI Agent-deepseek-harness插件开发003 插件配置：Schemastery与层叠机制"
date: 2026-10-19T09:30:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "Schemastery", "配置"]
---

## 概述

好插件与坏插件的分水岭，往往不在代码而在配置设计。本篇讲透两件事：如何用 Schemastery 定义「加载时校验、带默认值、类型安全」的插件配置；以及 Harness 四层配置的覆盖语义。学完后你会得到一条可执行的检验标准：**任何部署相关的取值，都能在 cordis.yml 里改，而不动一行代码**。

## 一、Config 类型 + 同名 Schema

让插件接受用户在 `cordis.yml` 传入的配置：导出一个 `Config` 接口，再导出**同名**的 Schemastery schema。默认值直接写在 schema 里：

```ts
import type { Context } from '@deepseek-ai/cordis'
import Schema from '@deepseek-ai/schemastery'

export const name = 'my-plugin'

export interface Config {
  greeting: string
  maxRetries: number
  verbose?: boolean
}

export const Config: Schema<Config> = Schema.object({
  greeting: Schema.string().default('Hello'),
  maxRetries: Schema.number().default(3),
  verbose: Schema.boolean().default(false),
})

export function apply(ctx: Context, config: Config) {
  // config 已校验：用户没传的字段用 schema 默认值填充
  console.log(config.greeting, config.maxRetries)
}
```

用户侧 `cordis.yml`：

```yaml
- insert:
    - id: hello
      name: '/abs/path/to/scratch-plugin/src/my-plugin.ts'
      config:
        greeting: 'Hi there'
        maxRetries: 5
        # verbose 省略 → 用默认值 false
```

框架行为：加载时用导出的 schema 校验配置 → 填充默认值 → 把结果作为第二参传给 `apply`。**校验失败 = 插件加载失败并给出明确错误**，这就是「配置错误要响亮」。

> ⚠️ 不要导出普通对象作为 `Config`。它不满足 Cordis 要求的 Standard Schema 接口，插件会加载失败。

### 1.1 校验到底发生在哪一行代码

不是"框架特殊照顾插件"，而是 **Cordis 在创建 Fiber 时调用了 Standard Schema 接口**：

```ts
// 语义等价于 cordis/fiber 的 resolveConfig
const result = plugin.Config['~standard'].validate(rawConfig)
if (result.issues) throw new ValidationError(result.issues)   // 消息形如 "invalid config:\n  - ..."
```

三条可操作的推论：

1. **`Config` 只要求"是个 Standard Schema"**，不强制是 Schemastery。zod 4 实测可用（本机 `cordis 4.0.5` + `zod 4.4.3`：`~standard.validate` 通过、默认值生效）。但有两个 Schemastery 专有物要留意：**`volatile` 元数据**（`Schema.prototype.volatile`，让字段就地更新而不重挂载服务）在别的库里没有；**`Config.merge`** 是 Cordis 给"服务拦截层"准备的可选钩子——存在就调用，否则退化成 `Object.assign` 浅合并，而 Schemastery 并不提供 `schema.merge`，DSH 里也没有包声明它。zod 的 `ZodObject` 恰好自带同名 `merge` 方法但签名不同（接收 ZodObject），一旦你的 Config 挂在 Service 上又用了拦截配置，这个钩子会被误调——**别在自定义 schema 上暴露 `merge`**。
2. **错误是 `ValidationError`，带逐条 issue**。终端里看到 `invalid config:` 开头 + 多行 `- 原因 (at 路径)`，就是它——按行修值即可，不必怀疑别处。
3. **默认值填充发生在校验里**，所以你拿到的 `config` 一定是"补全后"的对象；`apply` 里再写 `config.x ?? fallback` 属于重复兜底。

## 二、Schemastery 速查

DSH 用的不是 npm 上的 `schemastery`，而是 **fork 后 vendor 进仓库的 `@deepseek-ai/schemastery`（3.18.5）**：它除了 Koishi 那套 API，还暴露 Standard Schema 的 `~standard` 属性（Cordis 正是靠这个做校验），并提供 Loader 判定 volatile 字段所需的元数据。常用原子与组合：

| 写法 | 含义 |
|------|------|
| `Schema.string()` | 字符串 |
| `Schema.number()` | 数字 |
| `Schema.boolean()` | 布尔 |
| `Schema.union(['a', 'b'])` | 字面量联合（等价枚举） |
| `Schema.array(Schema.string())` | 字符串数组 |
| `Schema.object({ k: Schema.string() })` | 对象 |
| `Schema.dict(Schema.number())` | 字典（值同类型） |
| `.default(v)` | 默认值 |
| `.required()` | 必填（无默认即必填的场景显式声明） |
| `.description('...')` | 说明（喂给配置界面/文档） |

严格校验示例：

```ts
export interface Config {
  apiKey: string
  timeout: number
  mode: 'fast' | 'accurate'
}

export const Config = Schema.object({
  apiKey: Schema.string().required(),
  timeout: Schema.number().default(30000),
  mode: Schema.union(['fast', 'accurate']).default('fast'),
})
```

设计原则：

1. **无硬编码可调参数**：不同部署可能取不同值的参数，一律进 Config；
2. **约束表达在 schema 自身**：能写进 schema 的约束（范围、枚举、默认值）就不要在 `apply` 里手写 if 判断；
3. **默认值即最佳实践**：用户大概率会保留的值给默认，其余交给 schema 校验。

## 三、层叠机制：四层顺序与整行替换

复习第 001 篇的四层顺序，这里用实验加深理解。

### 3.1 实验：覆盖 base 的一行

真实例子。`dsh-web-app` 的 patch 里有这样两行（原样，删去注释）：

```yaml
- id: webserver
  name: '@deepseek-ai/dsh-host-webserver'
  inject: [webStartup]
  config:
    host: !!js ctx.webStartup.host ?? '127.0.0.1'
    port: !!js ctx.webStartup.port ?? 3080
    tls: !!js ctx.webStartup.tls
    compression: gzip
    compressionLevel: 1
    compressionThresholdBytes: 1024

- id: web-runtime
  name: '@deepseek-ai/dsh-web-app'
  inject: [webStartup]
  config:
    openBrowser: !!js ctx.webStartup.openBrowser
    printUrl: true
    publicUrl: !!js ctx.webStartup.publicUrl
    surfaceContext: true
```

你想把监听端口改成 8080。**错误写法**：

```yaml
- id: webserver
  config:
    port: 8080        # ← host / tls / compression* 全被丢弃，回落到 schema 默认值
```

因为 **patch 替换目标行的整个 config 值，不做深度合并**。注意两种情况不同：

- 被丢弃的键若 schema 里有默认值 → **静默回落**（最阴险，表现为"某个功能莫名其妙不工作"）；
- 被丢弃的键若 `.required()` 且无默认值 → 启动时报 `invalid config:`。

正确写法是重述全部键：

```yaml
- id: webserver
  config:
    host: '127.0.0.1'
    port: 8080
    compression: gzip
    compressionLevel: 1
    compressionThresholdBytes: 1024
```

推论有三条，都是干货：

1. **你的 patch 可以按 id 覆盖前面各层的行**，但必须重述该行每一个键（只想改一个值时，也可以只覆盖更"窄"的那一行，比如改 `openBrowser` 就只碰 `web-runtime`）；
2. **用户可以在自己 profile 的 cordis.patch.yml 里覆盖你的行**，无需改你的包——所以优先给出用户大概率会保留的默认值；
3. **`!!js` 表达式可以出现在 config 里**，它在 Loader 建立 entry 时求值（此时 `webStartup` 已存在），因此"从命令行标志推导配置"不需要你写代码。

### 3.2 层的优先级实验

准备三个 patch 文件验证顺序（`--patch` 按 argv 顺序，后者胜出）：

```sh
dsh web --patch ./a.yml --patch ./b.yml
```

```yaml
# a.yml
- insert:
    - id: demo
      name: '/abs/path/plugin.ts'
      config:
        greeting: 'from-a'

# b.yml
- insert:
    - id: demo
      name: '/abs/path/plugin.ts'
      config:
        greeting: 'from-b'
```

启动后日志打印 `from-b`。`--dump-config` 会按层打印，一眼确认。

### 3.3 验证而不启动

改完配置不想启动？先 dump：

```sh
dsh --profile demo --dump-config          # 分层最终配置（可加载的 YAML）
dsh --profile demo --dump-config-schema   # 每个字段的类型/默认值/描述
```

`--dump-config` 里每个组合包层以 `# == <package>` 分隔，`!!js` 表达式原样渲染（不求值），输出本身是**可再加载的 YAML**。两条使用纪律：

- 它**不承诺字节稳定**（app-boot 的开发备注明确说了），别把 dump 结果喂给脚本做自动断言；
- 它证明"层生效了"，**不证明"运行时好使"**——比如 `!!js` 表达式求值失败只有启动时才看得到。

空的或只有注释的 patch 文件会让启动**直接失败**；想表达"这层没有行"，写 `[]`。

## 四、配置热更新：两种粒度

改配置后发生什么，取决于**改的是哪一层**和**改了什么字段**：

```
改 profile / home / overlay 的 patch（配置层）
   → HMR 重组：受影响 Fiber: ACTIVE → UNLOADING → DISPOSED
              → 新 Fiber: PENDING → LOADING → ACTIVE（带着新 config）

改的只是 schema 标记为 volatile 的字段
   → 就地提交：apply 不重跑，新值写进运行中的引用
   → 发 loader/volatile-update 给宿主的 Fiber

改的是插件源码文件
   → 默认不发生（base 的 hmr 行是 root: []），配了 root 才会模块热替换
```

第一行有个前提：**该 profile 里 HMR 是启用的**。`dsh-base` 用 `disabled: !!js "!ctx.get('profileContext')"` 只在 profile 里启用它，而 `headless`/`sdk`/`acp` 三个 bundle 在 YAML 里**显式禁用**了 HMR——那些表层改任何配置都要重启。

第三种"就地提交"值得单独理解，因为它是"改了配置但行为没变"投诉的最大来源：

- volatile 是**按字段**声明的元数据。只有"其余选项完全未变、且变化字段全是 volatile"时才走就地提交；
- 你的插件若在 `apply` 里缓存了配置值（`const { topK } = config`），**即使字段是 volatile，你也读不到新值**——volatile 的契约是"每次使用时取当前值"；
- 想让自己的配置可热调，正确写法是把 `config` 对象留着，在动作发生时读 `config.topK`，而不是解构到闭包里。

至于"改配置文件为什么不重启"——那是 HMR 的 profile 配置监听（`root: []` 时依然生效），配的是 `awaitWriteFinish: true`，约 2 秒稳定窗口后才触发。**保存后等两秒再判断**。

## 五、配置设计实战：以知识库插件为例

第 006 篇要实现知识库插件，先在这里把配置设计好（**先设计配置，再写代码**）：

```ts
export interface Config {
  /** 索引存储目录 */
  storageDir: string
  /** 每个文档块的目标字符数 */
  chunkSize: number
  /** 相邻块的重叠字符数 */
  chunkOverlap: number
  /** 检索默认返回条数 */
  topK: number
  /** 单文档最大入库字符数（超出截断） */
  maxDocChars: number
}

export const Config = Schema.object({
  storageDir: Schema.string().default('~/.dsh/kb-lite'),
  chunkSize: Schema.number().default(500),
  chunkOverlap: Schema.number().default(80),
  topK: Schema.number().default(5),
  maxDocChars: Schema.number().default(200000),
})
```

对照三条原则自查：

- 路径、体积、条数都是部署相关 → 全部进 Config ✅
- 约束（默认值、类型）全部在 schema 表达 ✅
- 用户想改行为只动 yaml ✅

## 六、常见坑

| 坑 | 症状 | 解法 |
|----|------|------|
| `Config` 导出普通对象 | 加载失败，报 Standard Schema 相关错误 | 导出 Schemastery schema |
| 覆盖行只写改动键 | 其他配置「神秘消失」或**静默回落默认值** | 重述全部键（或只覆盖更窄的那一行） |
| 接口与 schema 不同名 | 配置不生效/类型丢失 | `interface Config` 与 `const Config` 同名导出 |
| 把密钥写进 patch 提交到 git | 泄露 | 密钥走环境变量/凭据库，patch 里只放非敏感默认值 |
| 可选字段不给默认值 | `config.x` 为 undefined 引发运行时报错 | schema 里 `.default()` 兜底 |
| 空 patch 文件 | 启动失败 | 写 `[]` |
| 在 `apply` 里解构 config | 改配置后行为不变 | volatile 契约是"用时取值"；保留 `config` 对象 |
| 以为改配置一定重载 | 有时生效有时不 | 区分 volatile 就地提交 / 普通重载 / 源码热替换 |
| 在 headless/acp/sdk 里等 HMR | 改了不生效 | 这些 bundle 显式禁用了 HMR，必须重启 |

## 七、练习

1. 给 hello 插件加 `greeting`/`excited`（boolean）/`repeat`（number）三个配置，用 `--patch` 传值验证；
2. 故意传一个错误类型（如 `repeat: "abc"`），观察 `invalid config:` 的逐条 issue 格式，体会「响亮失败」；
3. 写两个 patch 文件覆盖同一行，用 `--dump-config` 确认后者胜出；
4. 覆盖 `webserver` 行但**故意漏掉 `compression`**，用 `--dump-config-schema` 看它回落成了什么——体会"整行替换 + schema 默认值"组合起来的隐蔽性。

## 小结

- Config = 同名接口 + 同名 Schemastery schema，默认值写在 schema；校验走 Standard Schema（`~standard.validate`），失败是 `ValidationError` + 逐条 issue；
- 四层加载顺序：bundles → profile patch → home patch → `--patch` overlay，后者胜出；
- **整行替换，不深合并**——覆盖必须重述全部键，漏掉的键会静默回落默认值；
- 配置变更三种粒度：volatile 就地提交 / 普通重载 / 源码热替换（默认关闭）；headless/acp/sdk 没有 HMR；
- 配置错误要响亮：能进 schema 的约束不进代码；
- 下一篇讲生命周期：插件加载、卸载、自动清理的完整语义。

---

**系列导航**：[001 系列导读](/posts/tech/ai/agent/deepseek-harness-dev-001/) · [上一篇：002 环境搭建](/posts/tech/ai/agent/deepseek-harness-dev-002/) · [下一篇：004 生命周期](/posts/tech/ai/agent/deepseek-harness-dev-004/) · [原理篇参考：014 插件加载与热更新](/posts/tech/ai/agent/deepseek-harness-dev-014/)
