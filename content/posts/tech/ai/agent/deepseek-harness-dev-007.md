---
title: "AI Agent-deepseek-harness插件开发007 实战二：跨会话记忆插件memory-lite"
date: 2026-10-19T11:30:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "记忆", "事件系统", "实战"]
---

## 概述

第二个实战：`memory-lite`——跨会话记忆插件。对标社区热门插件 `flymysql/dsh-memory`（remember/recall/forget + 每轮提示注入）、`ICCuse/dsh-file-memory`（文件型工作记忆）、`Phant0Meow/dsh-memory-meow`（项目级记忆）。核心能力：

- **三个工具**：`memory_remember` / `memory_recall` / `memory_forget`，模型可主动存取；
- **被动捕获**：监听会话事件，用户消息里出现「记住：xxx」时自动存档——即使模型忘了调用工具，记忆也不丢；
- **人类可读镜像**：记忆同时写入 `MEMORY.md`，可直接手工编辑。

应用场景：跨会话记住「部署必须走 canary 流程」「这个项目的测试命令是 pnpm test」这类事实，不用每次重述。

## 一、需求与设计

### 1.1 与 kb-lite 的差异

| 维度 | kb-lite（006） | memory-lite（本篇） |
|------|----------------|---------------------|
| 数据来源 | 用户主动入库文档 | 对话中沉淀的事实 |
| 写入方 | 模型调 kb_add | 模型调工具 **+ 事件被动捕获** |
| 条目粒度 | 文档块（几百字） | 单条事实（一两句） |
| 读取方 | 模型检索 | 模型检索（+ 可选注入，第六节） |
| 用到的 API | 工具、配置、存储 | 工具、配置、存储、**事件系统** |

### 1.2 架构

```
┌──────────────────────────────────────────────────┐
│ memory-lite                                       │
│                                                  │
│  ctx.on('session/event') ──▶ 被动捕获（记住：…）    │
│         │                            │           │
│         ▼                            ▼           │
│  defineTool × 3 ◀────────── MemoryStore           │
│  remember / recall / forget      │               │
│                          ┌───────┴────────┐      │
│                          ▼                ▼      │
│                    memories.json      MEMORY.md   │
│                    （权威）          （人类镜像）   │
└──────────────────────────────────────────────────┘
```

设计决策：

- **JSON 为权威存储**，`MEMORY.md` 每次写入时全量重建——避免双写不一致；
- 召回打分用**词重叠 + 新近度加权**，不引入 BM25（记忆库条目少，简单可解释更重要）；
- 被动捕获用**防御式解析**（详见第五节），因为事件载荷结构以当前版本为准。

## 二、存储层

```ts
// src/index.ts
import { readFile, writeFile, rename, mkdir } from 'node:fs/promises'
import { homedir } from 'node:os'
import { dirname, join } from 'node:path'
import type { Context } from '@deepseek-ai/cordis'
import Schema from '@deepseek-ai/schemastery'
import { defineTool } from '@deepseek-ai/dsh-tools'

export interface Config {
  dataDir: string
  maxItems: number
  captureEnabled: boolean
  topK: number
}

export const Config: Schema<Config> = Schema.object({
  dataDir: Schema.string().default('~/.dsh/memory-lite').description('存储目录'),
  maxItems: Schema.number().default(500).description('记忆条目上限，超出淘汰最旧'),
  captureEnabled: Schema.boolean().default(true).description('是否启用「记住：」被动捕获'),
  topK: Schema.number().default(5).description('recall 默认返回条数'),
})

interface Memory {
  id: string
  content: string
  category: string
  tags: string[]
  createdAt: string
  origin: 'agent' | 'passive'
}

interface MemData {
  version: 1
  items: Memory[]
}

function expandHome(p: string): string {
  return p === '~' || p.startsWith('~/') ? join(homedir(), p.slice(2)) : p
}

const STOPWORDS = new Set(['的', '了', '是', '我', '你', '他', '她', '它', '们', '在', '和', '有', '就', '不', '都', '一下', '什么', '怎么', 'the', 'a', 'an', 'is', 'are', 'to', 'of', 'and', 'it'])

export class MemoryStore {
  constructor(private data: MemData, private file: string) {}

  static async open(file: string): Promise<MemoryStore> {
    let data: MemData = { version: 1, items: [] }
    try {
      data = JSON.parse(await readFile(file, 'utf8')) as MemData
    } catch {
      /* 首次使用 */
    }
    return new MemoryStore(data, file)
  }

  add(content: string, category: string, tags: string[], origin: Memory['origin'], maxItems: number): Memory {
    const item: Memory = {
      id: `mem_${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`,
      content: content.trim(),
      category: category || 'general',
      tags,
      createdAt: new Date().toISOString(),
      origin,
    }
    // 去重：内容完全相同则刷新而非新增
    const dup = this.data.items.find((m) => m.content === item.content)
    if (dup) {
      dup.createdAt = item.createdAt
    } else {
      this.data.items.push(item)
    }
    // 超上限淘汰最旧
    if (this.data.items.length > maxItems) {
      this.data.items = this.data.items.slice(-maxItems)
    }
    return dup ?? item
  }

  remove(id: string): boolean {
    const before = this.data.items.length
    this.data.items = this.data.items.filter((m) => m.id !== id)
    return this.data.items.length < before
  }

  search(query: string, topK: number) {
    const q = terms(query)
    const now = Date.now()
    const scored = this.data.items
      .map((m) => {
        const t = terms(m.content)
        const overlap = q.filter((x) => t.includes(x)).length
        if (overlap === 0) return null
        const coverage = overlap / Math.max(1, q.length)
        // 新近度加权：30 天半衰期
        const ageDays = (now - new Date(m.createdAt).getTime()) / 86400000
        const recency = Math.pow(0.5, ageDays / 30)
        return { memory: m, score: Number((coverage * 0.7 + recency * 0.3).toFixed(4)) }
      })
      .filter((x): x is { memory: Memory; score: number } => x !== null)
      .sort((a, b) => b.score - a.score)
      .slice(0, Math.max(1, topK))
    return scored
  }

  list() {
    return this.data.items.map((m) => ({ ...m }))
  }

  stats() {
    return { items: this.data.items.length, categories: new Set(this.data.items.map((m) => m.category)).size }
  }

  /** 序列化为人类可读的 Markdown 镜像 */
  toMarkdown(): string {
    const lines = ['# MEMORY', '']
    const byCat = new Map<string, Memory[]>()
    for (const m of this.data.items) {
      if (!byCat.has(m.category)) byCat.set(m.category, [])
      byCat.get(m.category)!.push(m)
    }
    for (const [cat, items] of byCat) {
      lines.push(`## ${cat}`, '')
      for (const m of items) {
        const tags = m.tags.length ? ` \`${m.tags.join('` `')}\`` : ''
        lines.push(`- [${m.id}]${tags} ${m.content}`)
      }
      lines.push('')
    }
    return lines.join('\n')
  }

  async flush(): Promise<void> {
    await mkdir(dirname(this.file), { recursive: true })
    const md = join(dirname(this.file), 'MEMORY.md')
    const tmpJ = `${this.file}.${process.pid}.tmp`
    await writeFile(tmpJ, JSON.stringify(this.data))
    await rename(tmpJ, this.file)
    const tmpM = `${md}.${process.pid}.tmp`
    await writeFile(tmpM, this.toMarkdown())
    await rename(tmpM, md)
  }
}

/** 轻量词项提取：拉丁词 + 中文二元组，去停用词 */
function terms(text: string): string[] {
  const out: string[] = []
  const lower = text.toLowerCase()
  for (const m of lower.matchAll(/[a-z0-9][a-z0-9_.+-]*/g)) out.push(m[0])
  let run = ''
  const flush = () => {
    if (!run) return
    if (run.length === 1) out.push(run)
    else for (let i = 0; i + 1 < run.length; i++) out.push(run.slice(i, i + 2))
    run = ''
  }
  for (const ch of lower) {
    if (/[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/.test(ch)) run += ch
    else flush()
  }
  flush()
  return out.filter((t) => !STOPWORDS.has(t) && t.length > 1)
}
```

## 三、三个工具

```ts
export const name = 'memory-lite'
export const inject = ['tools']

export function apply(ctx: Context, config: Config) {
  const jsonFile = join(expandHome(config.dataDir), 'memories.json')
  let store: MemoryStore | null = null
  let loading: Promise<MemoryStore> | null = null

  async function ready(): Promise<MemoryStore> {
    if (store) return store
    if (!loading) {
      loading = MemoryStore.open(jsonFile).then((s) => {
        store = s
        return s
      })
    }
    return loading
  }

  /* ---------- memory_remember ---------- */
  ctx.tools.register(
    defineTool({
      name: 'memory_remember',
      description:
        '把一条事实存入跨会话记忆。适用：用户明确要求记住的偏好、约定、项目事实'
        + '（如「记住：发布必须走 canary」）。一条记忆只说一件事。',
      parameters: {
        content: { type: 'string', required: true, description: '要记住的内容，一句话' },
        category: { type: 'string', description: '分类，如 project / preference / ops，默认 general' },
        tags: { type: 'string', description: '标签，逗号分隔，可选' },
      },
      output: {
        schema: {
          type: 'object',
          properties: {
            saved: { type: 'boolean' },
            id: { type: 'string' },
            deduped: { type: 'boolean' },
          },
        },
        render: (_args, value) => [
          { type: 'text', text: value.saved ? `已记住（${value.id}${value.deduped ? '，刷新了重复项' : ''}）` : '未保存' },
        ],
      },
      async execute(args) {
        const s = await ready()
        // 先查重再入库：若靠长度比较，淘汰旧条目时长度不变会误判为去重
        const deduped = s.list().some((m) => m.content === args.content.trim())
        const item = s.add(args.content, args.category ?? 'general', splitTags(args.tags), 'agent', config.maxItems)
        await s.flush()
        return { saved: true, id: item.id, deduped }
      },
    }),
  )

  /* ---------- memory_recall ---------- */
  ctx.tools.register(
    defineTool({
      name: 'memory_recall',
      description:
        '按查询召回相关记忆，按相关度（词重叠 + 新近度）排序返回。'
        + '适用：回答涉及项目约定、用户偏好、历史决策前先查一遍。',
      parameters: {
        query: { type: 'string', required: true, description: '检索查询，自然语言或关键词' },
        topK: { type: 'number', description: `返回条数，默认 ${config.topK}` },
      },
      output: {
        schema: {
          type: 'object',
          properties: {
            hits: { type: 'array' },
            total: { type: 'number' },
          },
        },
        render: (_args, value) => [
          {
            type: 'text',
            text: value.hits.length
              ? value.hits.map((h: any) => `[${h.score}] (${h.memory.category}) ${h.memory.content}`).join('\n')
              : '没有相关记忆。',
          },
        ],
      },
      presentResult: (args, value) => ({
        card: 'search',
        query: args.query,
        hits: value.hits.map((h: any) => ({ title: h.memory.category, snippet: h.memory.content })),
      }),
      async execute(args) {
        const s = await ready()
        const hits = s.search(args.query, args.topK ?? config.topK)
        return { hits, total: hits.length }
      },
    }),
  )

  /* ---------- memory_forget ---------- */
  ctx.tools.register(
    defineTool({
      name: 'memory_forget',
      description: '按 id 删除一条记忆。id 来自 memory_recall 的结果（字段 id）。',
      parameters: {
        id: { type: 'string', required: true, description: '记忆 id，如 mem_abc123' },
      },
      output: {
        schema: {
          type: 'object',
          properties: {
            removed: { type: 'boolean' },
          },
        },
        render: (_args, value) => [
          { type: 'text', text: value.removed ? `已删除 ${_args.id}` : `没有找到 ${_args.id}` },
        ],
      },
      async execute(args) {
        const s = await ready()
        const removed = s.remove(args.id)
        if (removed) await s.flush()
        return { removed }
      },
    }),
  )
}

function splitTags(tags?: string): string[] {
  if (!tags) return []
  return tags
    .split(/[,，]/)
    .map((t) => t.trim())
    .filter(Boolean)
}
```

要点：

- `memory_forget` 的入参 id **来自 recall 的结构化字段**，不是从自然语言里解析——这正是第 005 篇「规范值」契约的落地；
- 去重逻辑：相同内容刷新时间而非重复入库；
- 每次写操作后 `flush()` 双写 JSON + MEMORY.md。

## 四、被动捕获：监听 session/event

被动捕获是记忆插件区别于普通存储工具的关键：**用户在对话里说「记住：xxx」，即使模型没有调用工具，也自动存档**。

官方事件文档给出的事件名包括 `session/event`（持久化会话事件）等，并提醒：`turn/*`、`step/*`、`tool/call` 等是**持久化的会话事件类型，不是同名 Cordis 事件**，需要监听 `session/event` 并检查 `event.type`。

由于事件载荷的精确形状以当前版本为准，捕获代码采用**防御式解析**：尝试多种常见形状，全部落空就静默跳过（绝不因解析失败污染会话）。

```ts
// 在 apply() 内追加
if (config.captureEnabled) {
  ctx.on('session/event', (event: any) => {
    const text = extractUserText(event)
    if (!text) return
    const m = text.match(/(?:记住|remember)[:：]\s*(.{2,200})/i)
    if (!m) return
    void (async () => {
      const s = await ready()
      s.add(m[1].trim(), 'captured', [], 'passive', config.maxItems)
      await s.flush()
      console.log(`[memory-lite] captured: ${m[1].slice(0, 40)}`)
    })()
  })
}

/** 防御式提取用户消息文本：覆盖多种可能的载荷形状 */
function extractUserText(event: any): string | null {
  if (!event || typeof event !== 'object') return null
  const type = event.type ?? event.event ?? event.name
  const role = event.role ?? event.message?.role
  // 宽松判定：type 命中用户消息类，或 role 为 user；两者都不满足就不关心
  const isUserType = type ? /^(user|message|message_added|user_message)$/i.test(String(type)) : false
  if (!isUserType && role !== 'user') return null
  const content = event.content ?? event.text ?? event.message?.content
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content
      .map((b: any) => (typeof b === 'string' ? b : b?.text ?? ''))
      .join('')
  }
  return null
}
```

> 工程建议：第一次接入某个事件时，**先只打印不处理**——`ctx.on('session/event', (e) => console.dir(e, { depth: 4 }))`，跑一轮真实会话看清结构，再把 `extractUserText` 收窄成精确判断。宽松匹配只适合启动期。

## 五、注入扩展点：先打印，后处理

社区插件有两种让记忆「主动可见」的思路：

| 思路 | 代表插件 | 机制 |
|------|----------|------|
| 每轮注入系统提示 | `futongxu9-maker/dsh-shared-memory` | MEMORY.md + USER.md 注入每个会话的 system prompt |
| 工具驱动 + 面板浏览 | `flymysql/dsh-memory` | recall 工具 + 设置页条目浏览，模型按需查 |

注入需要挂到提示组装的扩展点上，具体载荷随版本演进。推荐的学习路径：

```ts
// 第一步：只观察，确认事件存在与载荷形状
ctx.on('agent/pre-step', (payload: any) => {
  console.dir(payload, { depth: 3 })
})

// 第二步：确认形状后，再改成受控注入（示意）
ctx.on('agent/pre-step', async (payload: any, next: any) => {
  const s = await ready()
  const brief = s.list().slice(-20).map((m) => `- (${m.category}) ${m.content}`).join('\n')
  // 注入方式（改写 systemPrompt / 注入 message / waterfall 包装）以当前版本事件契约为准
  return next()
})
```

memory-lite 默认**不注入**：三个工具 + 被动捕获已经可用；注入作为可选项，等你看清自己版本的事件契约后再开。这比照着一个不确定的签名写死注入要工程化得多。

## 六、运行验证剧本

```yaml
- insert:
    - id: memory-lite
      name: '/absolute/path/to/memory-lite/src/index.ts'
      config:
        dataDir: '~/.dsh/memory-lite'
        maxItems: 500
        captureEnabled: true
```

**第一幕：主动记忆**

> 记住：这个项目的测试命令是 pnpm test，发布前必须全绿

预期：`memory_remember(content="...", category="project")`，返回 id；`~/.dsh/memory-lite/MEMORY.md` 出现对应行。

**第二幕：新会话召回**（刷新页面开新会话）

> 这个项目跑测试用什么命令？

预期：模型先 `memory_recall(query="测试命令")`，基于召回结果回答「pnpm test」。

**第三幕：被动捕获**

新会话里直接输入：

> 记住：值班表每周一更新

预期：即使模型没有调用工具，终端出现 `[memory-lite] captured: 值班表每周一更新`，且 MEMORY.md 中多出 `(captured)` 条目。

**第四幕：遗忘**

> 把刚才那条值班表的记忆删掉

预期：`memory_recall` 找到 id → `memory_forget(id)` → MEMORY.md 同步移除。

## 七、测试

```ts
// test/memory.test.ts
import { test } from 'node:test'
import assert from 'node:assert'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { MemoryStore } from '../src/index.js'

async function makeStore() {
  return MemoryStore.open(join(tmpdir(), `mem-test-${Date.now()}.json`))
}

test('remember 后能 recall', async () => {
  const s = await makeStore()
  s.add('测试命令是 pnpm test', 'project', [], 'agent', 100)
  const hits = s.search('测试命令', 5)
  assert.ok(hits.length > 0)
  assert.match(hits[0].memory.content, /pnpm test/)
})

test('重复内容去重', async () => {
  const s = await makeStore()
  s.add('同一条内容', 'general', [], 'agent', 100)
  s.add('同一条内容', 'general', [], 'agent', 100)
  assert.strictEqual(s.list().length, 1)
})

test('超出上限淘汰最旧', async () => {
  const s = await makeStore()
  for (let i = 0; i < 10; i++) s.add(`内容${i}`, 'general', [], 'agent', 5)
  assert.strictEqual(s.list().length, 5)
  assert.ok(!s.list().some((m) => m.content === '内容0'))
})

test('forget 按 id 删除', async () => {
  const s = await makeStore()
  const item = s.add('待删除', 'general', [], 'agent', 100)
  assert.strictEqual(s.remove(item.id), true)
  assert.strictEqual(s.remove('不存在的id'), false)
})

test('flush 双写且 Markdown 可读', async () => {
  const s = await makeStore()
  s.add('发布走 canary', 'ops', ['deploy'], 'agent', 100)
  await s.flush()
  const md = s.toMarkdown()
  assert.match(md, /# MEMORY/)
  assert.match(md, /发布走 canary/)
  assert.match(md, /`deploy`/)
})
```

## 八、与社区插件的对照

| 插件 | 机制 | memory-lite 的取舍 |
|------|------|--------------------|
| `flymysql/dsh-memory` | 三工具 + 每轮注入 + 设置页 | 工具一致；注入留作扩展点（默认关） |
| `ICCuse/dsh-file-memory` | 会话笔记文件，抗上下文压缩 | 可扩展：recall 结果写入会话笔记 |
| `futongxu9-maker/dsh-shared-memory` | MEMORY.md 注入系统提示 | MEMORY.md 已生成；注入待事件契约确认后开启 |
| `modusensus/dsh-mneme` | SQLite + Markdown 镜像 + 后台巩固 | 存储可换 SQLite；巩固可作后台任务（第 009 篇） |
| `Phant0Meow/dsh-memory-meow` | PROJECT.md 快照注入首条消息 | 按项目分文件即可：dataDir 指向项目目录 |

## 九、扩展方向

1. **项目隔离**：`dataDir` 设为 `<workspace>/.memory/`，每个项目独立记忆（对标 dsh-memory-meow）；
2. **后台巩固**：把「去重/合并/冲突裁决」做成后台任务（第 009 篇的 `ctx.jobs` 模式）；
3. **引用式记忆**：召回结果带 `(sessionId, eventRange)` 引用，可展开回原始日志（对标 `Jesse-njx/dsh-memory`）；
4. **压缩抗丢**：把关键记忆同步进会话笔记文件，无损挺过上下文压缩（对标 ICCuse/dsh-file-memory）；
5. **安全加固**：记忆可能含敏感信息，读写过策略钩子脱敏（第 009 篇）。

## 十、练习

1. 给 `memory_recall` 增加 `category` 过滤参数，并更新测试；
2. 实现 `memory_list(category?)` 工具，返回全部条目（带分页参数 `offset/limit`）；
3. 把 `extractUserText` 收窄：先按第四节的「只打印」方案跑一轮，根据真实载荷重写为精确判断；
4. （进阶）实现启动时若 `memories.json` 缺失但 `MEMORY.md` 存在，则从 Markdown 重建索引（`- [id] \`tag\` content` 行的解析）。

## 小结

- 记忆插件 = 三工具（结构化契约）+ 被动捕获（事件系统）+ 双写存储（JSON 权威 + MD 镜像）；
- 事件集成第一课：**先打印后处理**，防御式解析，失败静默；
- 不确定的扩展点（注入）宁可留作开关，不照抄不确定的签名；
- 召回打分保持简单可解释：词重叠 + 新近度半衰期；
- 下一篇从单个插件走向架构：事件系统深水区、服务提供与三层能力拆分。

---

**系列导航**：[001 系列导读](./deepseek-harness-dev-001.md) · [上一篇：006 实战知识库插件](./deepseek-harness-dev-006.md) · [下一篇：008 事件与服务](./deepseek-harness-dev-008.md)
