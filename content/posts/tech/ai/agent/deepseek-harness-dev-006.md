---
title: "AI Agent-deepseek-harness插件开发006 实战一：本地知识库检索插件kb-lite"
date: 2026-10-19T11:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "知识库", "BM25", "RAG", "实战"]
---

## 概述

从本篇开始进入实战。目标是做出一个**真正能每天用**的插件：`kb-lite`——本地 Markdown/文本知识库检索。对标社区热门插件 `ICCuse/dsh-knowledge`（kb_add/kb_search）、`imkelt/DSH-RAG`、`Breeze136/dsh-kb-rag`，但做到**零外部依赖**：不装向量模型、不起外部服务、不需要 API Key，配好 overlay 就能跑。

应用场景：把团队文档、会议纪要、API 手册、个人笔记丢进一个目录，之后让 Agent「先查知识库再回答」，答案带出处。

## 一、需求与设计

### 1.1 能力清单

| 工具 | 行为 | 对标社区插件 |
|------|------|--------------|
| `kb_add` | 读文件（或直接传文本）→ 切块 → 建倒排索引 | dsh-knowledge 的 kb_add |
| `kb_search` | BM25 检索 → 带出处片段列表 | dsh-knowledge 的 kb_search |
| `kb_list` | 列出已入库文档 | DSH-RAG 的库管理 |
| `kb_stats` | 文档数/块数/词项数 | dsh-kb-sieve 的统计 |

### 1.2 技术选型与取舍

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 检索算法 | BM25（纯内存计算） | 零依赖、可解释、短文档检索质量足够 |
| 分词 | 拉丁词 + 中文二元组（bigram） | 不依赖分词词典，中英混合文本都能覆盖 |
| 持久化 | 单文件 JSON + 原子写 | 无数据库依赖；千级文档规模完全够用 |
| 向量检索 | **不做**（留作扩展） | 引入 embedding 模型会破坏「零依赖可复现」；第九节给出混合方案 |

> 什么时候该换 SQLite FTS5 / 向量库：文档数过万、或需要语义泛化检索时。对个人与小团队知识库，BM25 + JSON 是性价比最高的起点——先跑起来，再按需升级。

### 1.3 架构

```
┌─────────────────────────────────────────────┐
│ kb-lite 插件                                 │
│                                             │
│  tokenize() ──▶ chunkText() ──▶ KnowledgeBase │
│   中英分词        段落切块+重叠     (倒排+BM25) │
│                                     │       │
│                                 loadKB/saveKB │
│                                 (JSON 原子写)  │
│                                     │       │
│  defineTool × 4 ◀── ctx.tools.register      │
└─────────────────────────────────────────────┘
                     │
              ~/.dsh/kb-lite/index.json
```

## 二、配置与存储层

先落配置（沿用第 003 篇的设计），再落存储。存储的关键是**原子写**：先写临时文件再 `rename`，避免崩溃时半个 JSON 把索引打挂。

```ts
// src/index.ts
import { readFile, writeFile, rename, mkdir } from 'node:fs/promises'
import { homedir } from 'node:os'
import { dirname, join, basename } from 'node:path'
import type { Context } from '@deepseek-ai/cordis'
import Schema from '@deepseek-ai/schemastery'
import { defineTool } from '@deepseek-ai/dsh-tools'

export interface Config {
  storageDir: string
  chunkSize: number
  chunkOverlap: number
  topK: number
  maxDocChars: number
}

export const Config: Schema<Config> = Schema.object({
  storageDir: Schema.string().default('~/.dsh/kb-lite').description('索引存储目录'),
  chunkSize: Schema.number().default(500).description('文档块目标字符数'),
  chunkOverlap: Schema.number().default(80).description('相邻块重叠字符数'),
  topK: Schema.number().default(5).description('默认检索条数'),
  maxDocChars: Schema.number().default(200000).description('单文档最大入库字符数'),
})

function expandHome(p: string): string {
  return p === '~' || p.startsWith('~/') ? join(homedir(), p.slice(2)) : p
}

interface DocMeta {
  id: string
  title: string
  source: string
  addedAt: string
  chunkCount: number
}

interface Chunk {
  docId: string
  text: string
  /** term → 出现次数 */
  tf: Record<string, number>
  /** 块内 term 总数 */
  len: number
}

interface KBData {
  version: 1
  docs: DocMeta[]
  chunks: Chunk[]
  /** term → 包含该 term 的块数（文档频率） */
  df: Record<string, number>
  avgLen: number
}

async function loadKBData(file: string): Promise<KBData> {
  try {
    return JSON.parse(await readFile(file, 'utf8')) as KBData
  } catch {
    return { version: 1, docs: [], chunks: [], df: {}, avgLen: 0 }
  }
}

async function saveKBData(file: string, data: KBData): Promise<void> {
  await mkdir(dirname(file), { recursive: true })
  const tmp = `${file}.${process.pid}.tmp`
  await writeFile(tmp, JSON.stringify(data))
  await rename(tmp, file) // 原子替换，避免半截文件
}
```

## 三、分词与切块

### 3.1 分词：拉丁词 + 中文二元组

中文检索没有分词器也能做：把连续中文切成**二元组（bigram）**。查询「学习」含 term `学习`，库中含「深度学习」的块有 `深度`、`度学`、`学习`，精确匹配即可命中。bigram 方案无需词典、无 OOV，是中文零依赖检索的经典做法。

```ts
const CJK = /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/
const LATIN = /[a-z0-9][a-z0-9_.+-]*/g

export function tokenize(text: string): string[] {
  const tokens: string[] = []
  const lower = text.toLowerCase()
  // 1) 拉丁词与数字整词保留
  for (const m of lower.matchAll(LATIN)) tokens.push(m[0])
  // 2) 连续 CJK 段取二元组
  let run = ''
  const flush = () => {
    if (!run) return
    if (run.length === 1) {
      tokens.push(run)
    } else {
      for (let i = 0; i + 1 < run.length; i++) tokens.push(run.slice(i, i + 2))
    }
    run = ''
  }
  for (const ch of lower) {
    if (CJK.test(ch)) run += ch
    else flush()
  }
  flush()
  return tokens
}
```

```ts
tokenize('发布流程 release process')
// → ['release', 'process', '发布', '布流', '流程']

tokenize('BM25 检索')
// → ['bm25', '检索']
```

### 3.2 切块：段落聚合 + 重叠

切块策略直接影响检索质量：按空行分段，向块里贪心聚合，超过 `chunkSize` 就切；相邻块保留 `chunkOverlap` 字符重叠，避免答案正好跨块被切断。超长段落（如一整页日志）硬切。

```ts
export function chunkText(text: string, size: number, overlap: number): string[] {
  const paras = text.replace(/\r\n/g, '\n').split(/\n{2,}/)
  const chunks: string[] = []
  let buf = ''
  for (const p of paras) {
    if (buf && buf.length + p.length + 2 > size) {
      chunks.push(buf)
      buf = buf.slice(Math.max(0, buf.length - overlap)) + '\n\n' + p
    } else {
      buf = buf ? `${buf}\n\n${p}` : p
    }
  }
  if (buf.trim()) chunks.push(buf)
  // 超长块硬切
  const out: string[] = []
  const step = Math.max(50, size - overlap)
  for (const c of chunks) {
    if (c.length <= Math.floor(size * 1.5)) {
      out.push(c)
    } else {
      for (let i = 0; i < c.length; i += step) out.push(c.slice(i, i + size))
    }
  }
  return out
}
```

## 四、BM25 检索器

索引结构：每块存 `tf`（词频）与 `len`（块长）；全局存 `df`（文档频率）与 `avgLen`（平均块长）。BM25 打分：

```
score(q, d) = Σ_{t∈q} IDF(t) · f(t,d)·(k1+1) / ( f(t,d) + k1·(1 − b + b·|d|/avgdl) )
IDF(t)      = ln( 1 + (N − n(t) + 0.5) / (n(t) + 0.5) )
```

`k1=1.5`（词频饱和）、`b=0.75`（长度惩罚）。返回前把分数归一化到 0~1（除以最高分），方便模型判断相关性强弱。

```ts
const K1 = 1.5
const B = 0.75

export class KnowledgeBase {
  constructor(private data: KBData) {}

  static empty(): KnowledgeBase {
    return new KnowledgeBase({ version: 1, docs: [], chunks: [], df: {}, avgLen: 0 })
  }

  toJSON(): KBData {
    return this.data
  }

  add(title: string, source: string, text: string, chunkSize: number, chunkOverlap: number) {
    const pieces = chunkText(text, chunkSize, chunkOverlap)
    const docId = `doc_${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
    this.data.docs.push({
      id: docId,
      title,
      source,
      addedAt: new Date().toISOString(),
      chunkCount: pieces.length,
    })
    for (const piece of pieces) {
      const tf: Record<string, number> = {}
      for (const t of tokenize(piece)) tf[t] = (tf[t] ?? 0) + 1
      for (const t of Object.keys(tf)) this.data.df[t] = (this.data.df[t] ?? 0) + 1
      const len = Object.values(tf).reduce((a, b) => a + b, 0)
      this.data.chunks.push({ docId, text: piece, tf, len })
    }
    const total = this.data.chunks.reduce((a, c) => a + c.len, 0)
    this.data.avgLen = this.data.chunks.length ? total / this.data.chunks.length : 0
    return { docId, chunkCount: pieces.length }
  }

  search(query: string, topK: number, source?: string) {
    const N = this.data.chunks.length
    const qTokens = tokenize(query)
    const scored: { chunk: Chunk; score: number }[] = []
    for (const chunk of this.data.chunks) {
      if (source) {
        const doc = this.data.docs.find((d) => d.id === chunk.docId)
        if (doc && doc.title !== source && doc.source !== source) continue
      }
      let score = 0
      for (const t of qTokens) {
        const f = chunk.tf[t]
        if (!f) continue
        const n = this.data.df[t] ?? 0
        const idf = Math.log(1 + (N - n + 0.5) / (n + 0.5))
        const denom = f + K1 * (1 - B + (B * chunk.len) / (this.data.avgLen || 1))
        score += (idf * (f * (K1 + 1))) / denom
      }
      if (score > 0) scored.push({ chunk, score })
    }
    scored.sort((a, b) => b.score - a.score)
    const top = scored.slice(0, Math.max(1, topK))
    const max = top.length ? top[0].score : 1
    return top.map(({ chunk, score }) => {
      const doc = this.data.docs.find((d) => d.id === chunk.docId)!
      return {
        docId: chunk.docId,
        title: doc.title,
        source: doc.source,
        score: Number((score / max).toFixed(4)),
        snippet: chunk.text.length > 200 ? `${chunk.text.slice(0, 200)}…` : chunk.text,
      }
    })
  }

  list() {
    return this.data.docs.map((d) => ({ ...d }))
  }

  stats() {
    return {
      docs: this.data.docs.length,
      chunks: this.data.chunks.length,
      terms: Object.keys(this.data.df).length,
      avgChunkLen: Math.round(this.data.avgLen),
    }
  }
}
```

## 五、插件组装：四个工具

```ts
export const name = 'kb-lite'
export const inject = ['tools']

export function apply(ctx: Context, config: Config) {
  const indexFile = join(expandHome(config.storageDir), 'index.json')
  let kb: KnowledgeBase | null = null
  let loading: Promise<KnowledgeBase> | null = null

  // 懒加载 + 单飞：并发调用不会重复读盘
  async function ready(): Promise<KnowledgeBase> {
    if (kb) return kb
    if (!loading) {
      loading = loadKBData(indexFile).then((data) => {
        kb = new KnowledgeBase(data)
        return kb
      })
    }
    return loading
  }

  /* ---------- kb_add ---------- */
  ctx.tools.register(
    defineTool({
      name: 'kb_add',
      description:
        '把文档加入本地知识库：按段落切块并建立倒排索引，之后可被 kb_search 检索。'
        + '适用：团队文档、会议纪要、API 手册、笔记等可检索文本。',
      parameters: {
        path: { type: 'string', description: '要入库的本地文件路径（与 content 二选一）' },
        content: { type: 'string', description: '直接入库的文本（与 path 二选一）' },
        title: { type: 'string', description: '文档标题；缺省取文件名' },
      },
      output: {
        schema: {
          type: 'object',
          properties: {
            added: { type: 'boolean' },
            docId: { type: 'string' },
            title: { type: 'string' },
            chunkCount: { type: 'number' },
            truncated: { type: 'boolean' },
            reason: { type: 'string' },
          },
        },
        render: (_args, value) => [
          {
            type: 'text',
            text: value.added
              ? `已入库《${value.title}》：${value.chunkCount} 块${value.truncated ? '（超长已截断）' : ''}，docId=${value.docId}`
              : `未入库：${value.reason}`,
          },
        ],
      },
      async execute(args, exec) {
        let text: string
        let title = args.title
        if (args.path) {
          text = await readFile(args.path, { encoding: 'utf8', signal: exec.signal })
          title = title ?? basename(args.path)
        } else if (args.content) {
          if (!title) throw new Error('使用 content 入库时必须提供 title')
          text = args.content
        } else {
          throw new Error('必须提供 path 或 content 之一')
        }
        const truncated = text.length > config.maxDocChars
        if (truncated) text = text.slice(0, config.maxDocChars)
        const inst = await ready()
        if (!text.trim()) {
          return { added: false, docId: '', title: title ?? '', chunkCount: 0, truncated, reason: '文档内容为空' }
        }
        const { docId, chunkCount } = inst.add(
          title ?? 'untitled',
          args.path ?? '(inline)',
          text,
          config.chunkSize,
          config.chunkOverlap,
        )
        await saveKBData(indexFile, inst.toJSON())
        return { added: true, docId, title: title ?? 'untitled', chunkCount, truncated, reason: '' }
      },
    }),
  )

  /* ---------- kb_search ---------- */
  ctx.tools.register(
    defineTool({
      name: 'kb_search',
      description:
        '在本地知识库中做 BM25 全文检索，返回按相关度排序、带出处的片段。'
        + '适用：查找已入库文档中的段落。不适用：实时网页搜索。',
      parameters: {
        query: { type: 'string', required: true, description: '检索查询，自然语言或关键词' },
        topK: { type: 'number', description: `返回条数，默认 ${config.topK}` },
        source: { type: 'string', description: '限定文档（按标题或路径匹配），可选' },
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
              ? value.hits
                  .map((h: any) => `[${h.score}] 《${h.title}》(${h.source})\n${h.snippet}`)
                  .join('\n\n')
              : '知识库中没有匹配内容。',
          },
        ],
      },
      // Host 本地消费方（桌面壳/CLI）读这个；内置 Web 卡片走 client 侧 renderer，
      // 需额外用 output.presentationMeta 派生 metadata——见第 005 篇 5.1。
      presentResult: (args, value) => ({
        card: 'search',
        query: args.query,
        hits: value.hits,
        truncated: value.hits.some((h: any) => h.snippet.endsWith('…')),
      }),
      async execute(args) {
        const inst = await ready()
        const hits = inst.search(args.query, args.topK ?? config.topK, args.source)
        return { hits, total: hits.length }
      },
    }),
  )

  /* ---------- kb_list ---------- */
  ctx.tools.register(
    defineTool({
      name: 'kb_list',
      description: '列出知识库中已入库的全部文档及其块数。',
      parameters: {},
      output: {
        schema: {
          type: 'object',
          properties: {
            docs: { type: 'array' },
          },
        },
        render: (_args, value) => [
          {
            type: 'text',
            text: value.docs.length
              ? value.docs
                  .map((d: any) => `- 《${d.title}》 ${d.chunkCount} 块 · ${d.source} · ${d.addedAt}`)
                  .join('\n')
              : '知识库为空，先用 kb_add 入库。',
          },
        ],
      },
      async execute() {
        const inst = await ready()
        return { docs: inst.list() }
      },
    }),
  )

  /* ---------- kb_stats ---------- */
  ctx.tools.register(
    defineTool({
      name: 'kb_stats',
      description: '查看知识库统计：文档数、块数、词项数、平均块长。',
      parameters: {},
      output: {
        schema: {
          type: 'object',
          properties: {
            docs: { type: 'number' },
            chunks: { type: 'number' },
            terms: { type: 'number' },
            avgChunkLen: { type: 'number' },
          },
        },
        render: (_args, value) => [
          {
            type: 'text',
            text: `文档 ${value.docs} 篇 · 块 ${value.chunks} · 词项 ${value.terms} · 平均块长 ${value.avgChunkLen}`,
          },
        ],
      },
      async execute() {
        const inst = await ready()
        return inst.stats()
      },
    }),
  )
}
```

### 5.1 设计要点回顾

- **懒加载 + 单飞**：`ready()` 保证首次读盘只发生一次，并发工具调用不打架；
- **写后即持久化**：每次 `kb_add` 落盘，插件卸载不丢数据——所以不需要 `ctx.effect` 做 flush；
- **领域结果不抛异常**：内容为空返回 `{ added: false, reason }`；文件不存在（基础设施故障）才抛；
- **signal 传导**：读文件传 `exec.signal`；
- **卡片意图**：`kb_search` 声明 `search` 卡片并带截断标记。

## 六、挂载与运行验证

`cordis.yml`：

```yaml
- insert:
    - id: kb-lite
      name: '/absolute/path/to/kb-lite/src/index.ts'
      config:
        storageDir: '~/.dsh/kb-lite'
        chunkSize: 500
        topK: 5
```

启动后按下面剧本验证：

**第一幕：入库**

> 把 docs/release-playbook.md 加入知识库

预期：模型调用 `kb_add(path="docs/release-playbook.md")`，返回「已入库《release-playbook.md》：12 块」。

**第二幕：检索**

> 发布前必须完成哪些检查？

预期：模型调用 `kb_search(query="发布前检查")`，返回若干带出处片段；模型基于片段回答，并引用《release-playbook.md》。

**第三幕：管理**

> 知识库里现在有什么？

预期：`kb_list` 输出文档清单。

**验证文件**：`~/.dsh/kb-lite/index.json` 应包含 `docs`/`chunks`/`df`/`avgLen` 四个字段。

### 6.1 人工验证（不依赖模型）

没有 API Key 也能验证核心逻辑——直接跑单测（下节）或写个一次性脚本：

```ts
// scripts/smoke.ts
import { KnowledgeBase, tokenize, chunkText } from '../src/index.js'

const kb = KnowledgeBase.empty()
kb.add('playbook', 'docs/playbook.md', '发布前必须完成安全扫描与回归测试。\n\n回滚流程：先切流量再下线。', 500, 80)
kb.add('oncall', 'docs/oncall.md', '值班响应：先止损后排查。告警分 P0 P1 P2。', 500, 80)
console.log(tokenize('发布流程'))
console.log(chunkText('A\n\nB\n\nC', 3, 1))
console.log(kb.search('发布前检查', 2))
```

```sh
node --experimental-strip-types scripts/smoke.ts   # Node 22+ 可直接跑 TS
```

## 七、测试

核心逻辑（分词、切块、BM25 排序）必须单测——它们决定检索质量。用 `node:test` 零依赖跑：

```ts
// test/kb.test.ts
import { test } from 'node:test'
import assert from 'node:assert'
import { tokenize, chunkText, KnowledgeBase } from '../src/index.js'

test('中文按二元组分词', () => {
  assert.deepStrictEqual(tokenize('深度学习'), ['深度', '度学', '学习'])
})

test('中英混合分词', () => {
  assert.deepStrictEqual(tokenize('BM25 检索'), ['bm25', '检索'])
})

test('段落聚合与重叠', () => {
  const chunks = chunkText('一二三四五\n\n六七八九十', 6, 2)
  assert.ok(chunks.length >= 2)
  assert.ok(chunks[0].includes('一'))
})

test('相关文档排前面', () => {
  const kb = KnowledgeBase.empty()
  kb.add('playbook', 'a.md', '发布前必须完成安全扫描和回归测试，通过后才能上线。', 500, 80)
  kb.add('oncall', 'b.md', '值班先止损后排查，告警分等级处理。', 500, 80)
  const hits = kb.search('发布前检查', 2)
  assert.ok(hits.length > 0)
  assert.strictEqual(hits[0].title, 'playbook')
})

test('空库检索返回空', () => {
  assert.deepStrictEqual(KnowledgeBase.empty().search('anything', 5), [])
})

test('序列化后可恢复', () => {
  const kb = KnowledgeBase.empty()
  kb.add('t', 's.md', '内容', 100, 10)
  const round = new KnowledgeBase(JSON.parse(JSON.stringify(kb.toJSON())))
  assert.strictEqual(round.list().length, 1)
})
```

```sh
node --test --experimental-strip-types test/*.test.ts
```

## 八、与社区插件的对照

| 插件 | 核心能力 | 与 kb-lite 的差异 |
|------|----------|-------------------|
| `ICCuse/dsh-knowledge` | kb_add/kb_search/kb_show/kb_timeline，与 Codex CLI 共享格式 | 格式互通；kb-lite 专注「最小可跑」 |
| `imkelt/DSH-RAG` | 多目录增量索引、BM25+向量混合检索、可定位来源 | 向量混合是下一步扩展（见下节） |
| `Breeze136/dsh-kb-rag` | 10 个工具、本地 bge 嵌入 + SQLite | 重量级方案；千篇文档 86s 入库 |
| `omdsh-dev/dsh-kb-sieve` | SQLite FTS5、可审计知识库包 | 存储层换 SQLite 即可对标 |

**结论**：kb-lite 覆盖了日常检索的大部分需求，全部代码约 400 行，是继续演进的完美基线。

## 九、扩展方向（按优先级）

1. **混合检索**：`kb_search` 增加可选 embedding 召回（本地 bge 或 OpenAI 兼容 API），用 RRF 融合 BM25 与向量分数——对标 `imkelt/DSH-RAG`；
2. **增量索引**：入库前比对文件 mtime/内容 hash，跳过未变化文档，支持「同步整个目录」；
3. **目录监听**：在 `ctx.effect()` 里用 `fs.watch` 监听文档目录，变更自动重建索引，插件卸载时 `close()` watcher（第 004 篇的 effect 用武之地）；
4. **引用回跳**：卡片里带 `locations` 让编辑器跳转到源文件行；
5. **多库隔离**：用服务隔离（group）给不同项目配不同索引目录，见第 008 篇。

## 十、练习

1. 给 `kb_add` 加 `tags` 参数，入库时写入 DocMeta，`kb_search` 支持按 tag 过滤；
2. 实现 `kb_remove(docId)`：从 docs/chunks/df/avgLen 中完整移除一个文档（注意 df 与 avgLen 的重算）；
3. 把 `kb_search` 的 `topK` 默认值改为从 `ctx.get()` 取一个可选服务（模拟「用户偏好服务」），练习可选依赖；
4. （进阶）把存储层换成 `node:sqlite` 的 FTS5，比较查询延迟。

## 小结

- 一个可用插件 = 好配置 + 清晰索引模型 + 守契约的工具；
- 零依赖中文检索三件套：bigram 分词、段落切块+重叠、BM25；
- 懒加载+单飞、写后即持久化、原子写，是本地存储插件的标准套路；
- 领域结果进规范值，基础设施故障才抛异常；
- 下一篇实战二：跨会话记忆插件——把事件系统与被动捕获用起来。

---

**系列导航**：[001 系列导读](/posts/tech/ai/agent/deepseek-harness-dev-001/) · [上一篇：005 工具契约](/posts/tech/ai/agent/deepseek-harness-dev-005/) · [下一篇：007 实战记忆插件](/posts/tech/ai/agent/deepseek-harness-dev-007/) · [原理篇参考：013 工具调用链路](/posts/tech/ai/agent/deepseek-harness-dev-013/)
