---
title: "AI Agent-deepseek-harness插件开发009 安全策略钩子与后台任务"
date: 2026-10-19T13:30:00+08:00
draft: false
toc: true
categories: ["技术/AI/Agent"]
tags: ["AI", "Agent", "DeepSeek Harness", "插件开发", "dsh", "安全", "策略钩子", "后台任务"]
---

## 概述

插件拿到的是与 Harness 同级的运行时能力：能注册工具、读文件、发请求。安全因此不是可选项，而是插件设计的默认组成。本篇分两部分：**策略钩子**（把权限与脱敏做成策略而不是焊死在工具里）和**后台任务**（长工作的正确生命周期）。对标社区 `secret-guard`、`safety-net`、`PerryLink/dsh-defend` 等安全插件的核心思路。

## 一、安全模型：策略与工具分离

社区安全插件的共识做法：**权限和沙箱做成策略，不写进工具本体**。框架在工具执行流水线上提供一排扩展点：

| 扩展点 | 职责 | 典型用途 |
|--------|------|----------|
| `pre-execute` | 可扩展的允许/拒绝/询问策略 | 命令白名单、路径保护 |
| `guard` | 最终的**单调拒绝**，后续监听器无法撤销 | 红线兜底（如禁止 `rm -rf /`） |
| `execute` 包装器 | 给分发加截止时间、重试或指标采集 | 超时治理 |
| `post-execute` | 替换展示内容或返回值、拦截结果、附加模型可见上下文 | 脱敏、结果改写 |
| `result` | 只读观测归一化后的不可变结果 | 审计日志 |

注意两个语义细节：

- **内容替换不影响程序侧取值**（Code Mode 拿到的还是原值）；
- **保密策略要屏蔽或替换值本身**，而不是只改展示。

> 这些扩展点的精确注册签名以 `@deepseek-ai/dsh-tools` 当前版本的扩展点参考为准。本篇把重点放在**两个 100% 稳定的落点**：基于 `tools/result` 事件的审计，以及工具化的敏感信息扫描。策略钩子部分给出设计范式与接线思路，接入时以当版参考校准。

## 二、稳定落点一：审计日志（tools/result）

官方事件文档确认 `tools/result` 是 Cordis 事件，载荷为 `(exec, result)`。审计插件只用这一个事件就能覆盖全部工具调用：

```ts
// src/index.ts
import { appendFile, mkdir, readFile } from 'node:fs/promises'
import { homedir } from 'node:os'
import { dirname, join } from 'node:path'
import type { Context } from '@deepseek-ai/cordis'
import Schema from '@deepseek-ai/schemastery'
import { defineTool } from '@deepseek-ai/dsh-tools'

export interface Config {
  auditLog: string
  redact: boolean
  patterns: string[]
}

export const Config: Schema<Config> = Schema.object({
  auditLog: Schema.string().default('~/.dsh/audit-lite/audit.jsonl').description('审计日志文件'),
  redact: Schema.boolean().default(true).description('记录前脱敏'),
  patterns: Schema.array(Schema.string()).default([
    'sk-[A-Za-z0-9]{20,}',              // API Key 形态
    'AKIA[0-9A-Z]{16}',                 // AWS AccessKey
    '-----BEGIN [A-Z ]*PRIVATE KEY-----',// PEM 私钥
  ]).description('脱敏正则列表'),
})

export const name = 'audit-lite'
export const inject = ['tools']

export function apply(ctx: Context, config: Config) {
  const logFile = expandHome(config.auditLog)
  const regexes = config.patterns.map((p) => new RegExp(p, 'g'))

  // 日志目录可能不存在：启动时确保创建（失败不阻塞插件加载，appendFile 处再兜底）
  void mkdir(dirname(logFile), { recursive: true }).catch(() => {})

  function redact(text: string): string {
    return regexes.reduce((acc, re) => acc.replace(re, '[REDACTED]'), text)
  }

  // 1) 审计：每次工具结果落一条 JSONL
  ctx.on('tools/result', (exec, result) => {
    const text = result.content
      .map((b: any) => (b.type === 'text' ? b.text : `[${b.type}]`))
      .join('')
    const line = JSON.stringify({
      ts: new Date().toISOString(),
      tool: exec.name,
      args: config.redact ? redact(JSON.stringify(exec.arguments)) : JSON.stringify(exec.arguments),
      result: config.redact ? redact(text.slice(0, 2000)) : text.slice(0, 2000),
    })
    void appendFile(logFile, `${line}\n`).catch(() => {})
  })

  // 2) 工具化：让模型自己也能扫敏感信息
  ctx.tools.register(
    defineTool({
      name: 'secret_scan',
      description:
        '扫描文本中的敏感信息（API Key、私钥、令牌等），返回命中位置与类型，不返回明文。'
        + '适用：提交前自查、日志检查。',
      parameters: {
        path: { type: 'string', required: true, description: '要扫描的本地文件路径' },
      },
      output: {
        schema: {
          type: 'object',
          properties: {
            scanned: { type: 'string' },
            findings: { type: 'array' },
          },
        },
        render: (_args, value) => [
          {
            type: 'text',
            text: value.findings.length
              ? value.findings.map((f: any) => `第 ${f.line} 行命中「${f.kind}」`).join('\n')
              : '未发现敏感信息。',
          },
        ],
      },
      async execute(args, exec) {
        const text = await readFile(args.path, { encoding: 'utf8', signal: exec.signal })
        const findings: { line: number; kind: string }[] = []
        const kinds = ['api-key', 'aws-access-key', 'private-key']
        text.split('\n').forEach((line, i) => {
          config.patterns.forEach((p, pi) => {
            if (new RegExp(p).test(line)) findings.push({ line: i + 1, kind: kinds[pi] ?? `pattern-${pi}` })
          })
        })
        return { scanned: args.path, findings }
      },
    }),
  )
}

function expandHome(p: string): string {
  return p === '~' || p.startsWith('~/') ? join(homedir(), p.slice(2)) : p
}
```

### 2.1 设计要点

- **审计与工具解耦**：审计走事件（观测一切调用），扫描走工具（模型主动用）；
- **脱敏在记录侧**：日志文件不落明文——审计日志本身就是高价值目标；
- **findings 不含明文**：只报行号与类型，模型不需要看到密钥内容；
- **patterns 进 Config**：不同团队有不同红线，yaml 可改。

## 三、稳定落点二：把「红线」做成可执行策略

上表的 `pre-execute` / `guard` 是执行前拦截；在签名未确认的版本，有一个等价的稳定模式——**包装工具**：注册一个「闸门工具」让模型先调，或直接用 `execute` 包装器思路。这里给出社区 `safety-net`（破坏性命令拦截）的范式代码骨架，接线处以当版参考为准：

```ts
// 范式：破坏性命令判定（纯函数，便于单测）
export interface Verdict { action: 'allow' | 'ask' | 'block'; reason?: string }

const DESTRUCTIVE = [
  { re: /\brm\s+(-\w+\s+)*-\w*[rf]/, why: '递归/强制删除' },
  { re: /\bgit\s+push\s+.*--force/, why: '强制推送' },
  { re: /\bgit\s+reset\s+--hard/, why: '硬重置丢弃改动' },
  { re: /\bdd\s+if=/, why: '原始磁盘写入' },
  { re: />\s*\/dev\/sd[a-z]/, why: '直接写块设备' },
]

export function judge(command: string, allowlist: string[] = []): Verdict {
  if (allowlist.some((a) => command.startsWith(a))) return { action: 'allow' }
  for (const d of DESTRUCTIVE) {
    if (d.re.test(command)) return { action: 'block', reason: d.why }
  }
  return { action: 'allow' }
}
```

接线方式（三选一，按版本能力选择）：

1. `pre-execute` 监听器调用 `judge()`，返回拒绝/询问（推荐，框架统一走审批 UI）；
2. 包装 bash 工具的 `execute`，在调用真实实现前判定（Provider 层拦截）；
3. 注册 `command_check` 工具让模型主动预检（最保守，零集成风险）。

模式二示例（Provider 层包装，只依赖已确认的 API）：

```ts
// 思路示意（伪代码）：ShellService 为示意基类，实际应替换为你要包住的那个 Provider
class GuardedShell extends ShellService {
  async exec(request: ShellRequest) {
    const verdict = judge(request.command, this.allowlist)
    if (verdict.action === 'block') {
      return { exitCode: -1, stdout: '', stderr: `blocked: ${verdict.reason}` }
    }
    return super.exec(request)
  }
}
```

## 四、后台任务：长工作的正确生命周期

会跑很久的工具（全量重建索引、长视频转写）不该阻塞一次工具调用。官方参考给出的模式：

- 需要 `run_in_background` 且由部署配置开关的工具，通过 `ctx.jobs.start()` 注册为后台任务；
- 运行时先校验所有者和任务控制器可用，再提供任务 id、会话围栏、通用控制工具和清理钩子；
- 注册成功返回**类型化句柄**，如 `{ kind: 'background', jobId: 'bash-1' }`；
- 模型侧用任务终止工具、所有者销毁、服务卸载来结束任务；
- 收集输出用任务输出工具。

### 4.1 取消信号的语义（高频出错点）

```
任务发布后：
  exec.signal  →  只代表「不再等待这次调用」，不终止已发布的工作
  任务自有信号  →  真正终止任务（经任务终止工具触发）
```

> 官方警告原样照抄：**绝不能通过解析「started background job bash-1」这种文本去取得 id**。要拿 id，拿结构化字段 `jobId`。

### 4.2 设计稿：给 kb-lite 加一个「目录全量入库」后台任务

kb-lite 目前只有单文档入库。假设要新增「把整个目录递归入库」的能力，用后台任务模式实现（签名以当前版本 `ctx.jobs` 参考为准）：

```ts
// 设计稿：签名以当前版本 ctx.jobs 参考为准
output: {
  schema: {
    type: 'object',
    properties: {
      kind: { type: 'string' },     // 'background'
      jobId: { type: 'string' },
    },
  },
  render: (_args, value) => [
    { type: 'text', text: `已启动后台重建任务 ${value.jobId}，可用任务工具查看进度` },
  ],
},
async execute(args, exec) {
  if (!config.backgroundRebuild) throw new Error('后台重建未开启（backgroundRebuild=false）')
  const job = await ctx.jobs.start({
    name: 'kb-rebuild',
    async run(signal) {
      const files = await walk(args.dir)
      for (const f of files) {
        if (signal.aborted) return { cancelled: true }
        await rebuildOne(f)
      }
      return { rebuilt: files.length }
    },
  })
  return { kind: 'background', jobId: job.id }
}
```

要点：**是否允许后台由配置决定**（不能写死）；`run` 接收任务自有 `signal`；返回句柄而不是文案；进度查询走任务输出工具。

### 4.3 没有 jobs 时的降级方案

当前版本若未暴露 `ctx.jobs`，用「分段 + 游标」模拟：`kb_rebuild(dir, cursor?)` 每次处理 N 个文件，返回 `nextCursor`，模型多轮调用推进。零新 API，且天然可取消（不调下一轮就停）。

## 五、审计与合规注意事项

1. **日志脱敏**：审计日志本身可能含密钥，记录侧脱敏（见第二节）；
2. **最小采集**：工具自己如实声明采集边界，别把整个文件系统状态倒进日志；
3. **可回放性**：卡片与展示数据用 `presentationMeta` 派生，不要存易变明文；
4. **审批门**：高危动作（写保护目录、外发数据）转人工，fail-safe 默认拒绝（对标 `PerryLink/dsh-defend` 的 allow/ask/block 三态）；
5. **密钥来源**：从环境变量或凭据库读，绝不写进 patch 文件提交 git。

## 六、与社区安全插件对照

| 插件 | 核心思路 | 本篇对应 |
|------|----------|----------|
| `safety-net` | 破坏性命令解析 + 执行前确认 | 第三节 `judge()` 判定器 |
| `secret-guard` | 阻止读写敏感文件 + 掩码泄露 + 审计日志 | 第二节 audit-lite |
| `PerryLink/dsh-defend` | 提示注入/越狱/密钥泄露检测，三道缝拦截 | pre-execute 策略模式 |
| `PerryLink/dsh-mask` | PII 脱敏中间件，展示层还原 | 替换级脱敏（非展示级） |
| `moon09300731/dsh-approval-gate` | Flash 预判不可回补性，自动/人工分流 | 判定器 + 审批门 |

## 七、练习

1. 给 audit-lite 加 `session/event` 监听，记录用户消息中的「记住：」捕获次数，输出日签到审计日志；
2. 扩展 `judge()`：支持 allowlist 配置项，并为每条规则补单测（表驱动测试）；
3. 把 kb-lite 的 `kb_add` 目录批量入库改造成 4.3 节的「分段 + 游标」模式，并处理 `maxDocChars` 边界；
4. （进阶）读完 `@deepseek-ai/dsh-tools` 的扩展点参考后，把 `judge()` 以 `pre-execute` 接线，验证审批 UI 行为。

## 小结

- 策略与工具分离：判定器是纯函数，接线用扩展点；
- 两个稳定落点：`tools/result` 审计 + 工具化扫描，记录侧脱敏；
- 后台任务三原则：配置开关、任务自有 signal、结构化 jobId 句柄；
- 取消语义：`exec.signal` 止等待，任务信号止工作；
- 安全默认值：fail-safe 拒绝、最小采集、密钥不进 patch；
- 下一篇收官：打包发布、测试策略与最佳实践清单。

---

**系列导航**：[001 系列导读](./deepseek-harness-dev-001.md) · [上一篇：008 事件与服务](./deepseek-harness-dev-008.md) · [下一篇：010 打包发布](./deepseek-harness-dev-010.md)
