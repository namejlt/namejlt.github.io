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

## 二、Schemastery 速查

Schemastery 是 Koishi 生态的 schema 库，DSH 复用它做配置校验。常用原子与组合：

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

假设某组合包（`dsh-web-app`）在它的 patch 里注册了：

```yaml
- insert:
    - id: web
      name: '@deepseek-ai/dsh-web-app'
      config:
        port: 3080
        host: '127.0.0.1'
        openBrowser: true
```

你想只改端口。**错误写法**：

```yaml
- insert:
    - id: web
      name: '@deepseek-ai/dsh-web-app'
      config:
        port: 8080        # ← host 和 openBrowser 被丢弃了！
```

因为 **patch 替换目标行的整个 config 值，不做深度合并**。正确写法是重述全部键：

```yaml
- insert:
    - id: web
      name: '@deepseek-ai/dsh-web-app'
      config:
        port: 8080
        host: '127.0.0.1'
        openBrowser: true
```

推论有两条，都是干货：

1. **你的 patch 可以按 id 覆盖前面各层的行**，但必须重述该行每一个键；
2. **用户可以在自己 profile 的 cordis.patch.yml 里覆盖你的行**，无需改你的包——所以优先给出用户大概率会保留的默认值。

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
dsh --profile demo --dump-config
```

输出里每个组合包层以 `# == <package>` 分隔，确认你的层存在且值正确，再启动。

## 四、HMR：配置热更新

加载 `@deepseek-ai/dsh-hmr` 后，修改 `cordis.yml` 中某插件的 `config`，框架会**卸载旧实例、加载新实例**：

```
改 cordis.yml → Fiber: ACTIVE → UNLOADING → DISPOSED
             → 新 Fiber: PENDING → LOADING → ACTIVE（带着新 config）
```

由于所有注册都是自动清理的 effect，替换后不会残留旧实例的注册——这正是第 004 篇的主题。开发期改配置不用重启，反馈极快。

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
| 覆盖行只写改动键 | 其他配置「神秘消失」 | 重述全部键 |
| 接口与 schema 不同名 | 配置不生效/类型丢失 | `interface Config` 与 `const Config` 同名导出 |
| 把密钥写进 patch 提交到 git | 泄露 | 密钥走环境变量/凭据库，patch 里只放非敏感默认值 |
| 可选字段不给默认值 | `config.x` 为 undefined 引发运行时报错 | schema 里 `.default()` 兜底 |

## 七、练习

1. 给 hello 插件加 `greeting`/`excited`（boolean）/`repeat`（number）三个配置，用 `--patch` 传值验证；
2. 故意传一个错误类型（如 `repeat: "abc"`），观察加载失败信息，体会「响亮失败」；
3. 写两个 patch 文件覆盖同一行，用 `--dump-config` 确认后者胜出。

## 小结

- Config = 同名接口 + 同名 Schemastery schema，默认值写在 schema；
- 四层加载顺序：bundles → profile patch → home patch → `--patch` overlay，后者胜出；
- **整行替换，不深合并**——覆盖必须重述全部键；
- 配置错误要响亮：能进 schema 的约束不进代码；
- 下一篇讲生命周期：插件加载、卸载、自动清理的完整语义。

---

**系列导航**：[001 系列导读](./deepseek-harness-dev-001.md) · [上一篇：002 环境搭建](./deepseek-harness-dev-002.md) · [下一篇：004 生命周期](./deepseek-harness-dev-004.md)
