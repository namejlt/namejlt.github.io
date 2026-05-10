---
title: "多Agent协作系统如何设计"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["Agent", "多Agent", "golang"]
---

## 问题

单个Agent的能力有限，复杂任务需要多个Agent协作完成。多Agent系统的编排模式有哪些？如何设计Agent间的通信、任务分配和冲突解决机制？如何用Go实现一个多Agent协作框架？

## 回答

多Agent系统（Multi-Agent System, MAS）通过将复杂任务分解为子任务，由不同角色的Agent协作完成。从AutoGPT到MetaGPT，多Agent架构已经证明了其在复杂任务上的优势。

### 一、多Agent协作模式

#### 1.1 顺序模式（Pipeline）

```
用户 → Agent1(分析) → Agent2(规划) → Agent3(执行) → Agent4(审查) → 结果
```

适合流程明确的任务，如代码开发（需求分析→设计→编码→测试）。

#### 1.2 并行模式（MapReduce）

```
用户 → Agent1(子任务1) ─┐
     → Agent2(子任务2) ─┤→ 汇总Agent → 结果
     → Agent3(子任务3) ─┘
```

适合可独立执行的子任务，如多文档摘要、多角度分析。

#### 1.3 辩论模式（Debate）

```
用户 → AgentA(正方) → AgentB(反方) → AgentC(裁判) → 结果
           ↑__________________________|
              (多轮辩论)
```

适合需要多角度思考的决策任务。

#### 1.4 层级模式（Hierarchical）

```
         Supervisor Agent
        /       |        \
   Worker1  Worker2   Worker3
```

Supervisor负责任务分解和分配，Worker负责执行。

### 二、Go语言多Agent框架实现

```go
package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

type AgentMessage struct {
	From      string          `json:"from"`
	To        string          `json:"to"`
	Content   string          `json:"content"`
	Type      string          `json:"type"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

type Agent interface {
	Name() string
	Role() string
	Process(ctx context.Context, msg AgentMessage) (*AgentMessage, error)
}

type MessageBus struct {
	mu       sync.RWMutex
	handlers map[string]chan AgentMessage
	history  []AgentMessage
}

func NewMessageBus() *MessageBus {
	return &MessageBus{
		handlers: make(map[string]chan AgentMessage),
	}
}

func (b *MessageBus) Register(agentName string) <-chan AgentMessage {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan AgentMessage, 100)
	b.handlers[agentName] = ch
	return ch
}

func (b *MessageBus) Send(ctx context.Context, msg AgentMessage) error {
	b.mu.RLock()
	ch, ok := b.handlers[msg.To]
	b.mu.RUnlock()

	if !ok {
		return fmt.Errorf("agent %q not found", msg.To)
	}

	b.history = append(b.history, msg)

	select {
	case ch <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *MessageBus) Broadcast(ctx context.Context, from string, msg AgentMessage) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for name, ch := range b.handlers {
		if name == from {
			continue
		}
		m := msg
		m.From = from
		m.To = name
		select {
		case ch <- m:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

type SupervisorAgent struct {
	name     string
	llm      LLMClient
	bus      *MessageBus
	workers  map[string]Agent
}

func NewSupervisorAgent(name string, llm LLMClient, bus *MessageBus) *SupervisorAgent {
	return &SupervisorAgent{
		name:    name,
		llm:     llm,
		bus:     bus,
		workers: make(map[string]Agent),
	}
}

func (s *SupervisorAgent) Name() string { return s.name }
func (s *SupervisorAgent) Role() string { return "supervisor" }

func (s *SupervisorAgent) AddWorker(agent Agent) {
	s.workers[agent.Name()] = agent
}

func (s *SupervisorAgent) Process(ctx context.Context, msg AgentMessage) (*AgentMessage, error) {
	workerList := make([]string, 0, len(s.workers))
	for name, agent := range s.workers {
		workerList = append(workerList, fmt.Sprintf("- %s: %s", name, agent.Role()))
	}

	prompt := fmt.Sprintf(`You are a supervisor agent. Given the following task, decide which worker should handle it.

Available workers:
%s

Task: %s

Respond in JSON format:
{"worker": "worker_name", "instruction": "specific instruction for the worker"}`, 
		fmt.Sprintf("\n%s", workerList), msg.Content)

	resp, err := s.llm.Generate(ctx, []Message{{Role: "user", Content: prompt}})
	if err != nil {
		return nil, err
	}

	var decision struct {
		Worker      string `json:"worker"`
		Instruction string `json:"instruction"`
	}
	if err := json.Unmarshal([]byte(cleanJSON(resp)), &decision); err != nil {
		return nil, fmt.Errorf("failed to parse supervisor decision: %w", err)
	}

	workerMsg := AgentMessage{
		From:    s.name,
		To:      decision.Worker,
		Content: decision.Instruction,
		Type:    "task",
	}

	if err := s.bus.Send(ctx, workerMsg); err != nil {
		return nil, err
	}

	return &AgentMessage{
		From:    s.name,
		To:      msg.From,
		Content: fmt.Sprintf("Task assigned to %s", decision.Worker),
		Type:    "status",
	}, nil
}

type PipelineOrchestrator struct {
	agents []Agent
	bus    *MessageBus
	llm    LLMClient
}

func NewPipelineOrchestrator(bus *MessageBus, llm LLMClient, agents ...Agent) *PipelineOrchestrator {
	return &PipelineOrchestrator{
		agents: agents,
		bus:    bus,
		llm:    llm,
	}
}

func (o *PipelineOrchestrator) Execute(ctx context.Context, input string) (string, error) {
	currentInput := input

	for i, agent := range o.agents {
		msg := AgentMessage{
			From:    "orchestrator",
			To:      agent.Name(),
			Content: currentInput,
			Type:    "task",
		}

		result, err := agent.Process(ctx, msg)
		if err != nil {
			return "", fmt.Errorf("agent %q failed at step %d: %w", agent.Name(), i+1, err)
		}

		currentInput = result.Content
	}

	return currentInput, nil
}

type ParallelOrchestrator struct {
	agents     []Agent
	bus        *MessageBus
	llm        LLMClient
	aggregator func(results []string) (string, error)
}

func NewParallelOrchestrator(bus *MessageBus, llm LLMClient, aggregator func([]string) (string, error), agents ...Agent) *ParallelOrchestrator {
	return &ParallelOrchestrator{
		agents:     agents,
		bus:        bus,
		llm:        llm,
		aggregator: aggregator,
	}
}

func (o *ParallelOrchestrator) Execute(ctx context.Context, input string) (string, error) {
	type agentResult struct {
		index  int
		result string
		err    error
	}

	resultCh := make(chan agentResult, len(o.agents))

	for i, agent := range o.agents {
		go func(idx int, a Agent) {
			msg := AgentMessage{
				From:    "orchestrator",
				To:      a.Name(),
				Content: input,
				Type:    "task",
			}

			result, err := a.Process(ctx, msg)
			if err != nil {
				resultCh <- agentResult{index: idx, err: err}
				return
			}
			resultCh <- agentResult{index: idx, result: result.Content}
		}(i, agent)
	}

	results := make([]string, len(o.agents))
	for i := 0; i < len(o.agents); i++ {
		r := <-resultCh
		if r.err != nil {
			return "", r.err
		}
		results[r.index] = r.result
	}

	return o.aggregator(results)
}

type DebateOrchestrator struct {
	proponent Agent
	opponent  Agent
	judge     Agent
	rounds    int
}

func NewDebateOrchestrator(proponent, opponent, judge Agent, rounds int) *DebateOrchestrator {
	return &DebateOrchestrator{
		proponent: proponent,
		opponent:  opponent,
		judge:     judge,
		rounds:    rounds,
	}
}

func (o *DebateOrchestrator) Execute(ctx context.Context, topic string) (string, error) {
	proponentMsg := AgentMessage{From: "system", To: o.proponent.Name(), Content: topic, Type: "debate_opening"}

	for round := 0; round < o.rounds; round++ {
		proResult, err := o.proponent.Process(ctx, proponentMsg)
		if err != nil {
			return "", err
		}

		oppMsg := AgentMessage{From: o.proponent.Name(), To: o.opponent.Name(), Content: proResult.Content, Type: "debate_rebuttal"}
		oppResult, err := o.opponent.Process(ctx, oppMsg)
		if err != nil {
			return "", err
		}

		proponentMsg = AgentMessage{From: o.opponent.Name(), To: o.proponent.Name(), Content: oppResult.Content, Type: "debate_rebuttal"}
	}

	judgeMsg := AgentMessage{
		From:    "system",
		To:      o.judge.Name(),
		Content: fmt.Sprintf("Please evaluate the debate on topic: %s", topic),
		Type:    "debate_judgment",
	}

	judgeResult, err := o.judge.Process(ctx, judgeMsg)
	if err != nil {
		return "", err
	}

	return judgeResult.Content, nil
}

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = s[3:]
		if strings.HasPrefix(s, "json") {
			s = s[4:]
		}
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}
```

### 三、Agent间通信协议

```go
const (
	MsgTypeTask       = "task"
	MsgTypeResult     = "result"
	MsgTypeError      = "error"
	MsgTypeStatus     = "status"
	MsgTypeQuery      = "query"
	MsgTypeBroadcast  = "broadcast"
)

type AgentProtocol struct {
	bus *MessageBus
}

func (p *AgentProtocol) RequestResponse(ctx context.Context, from, to, content string) (*AgentMessage, error) {
	msg := AgentMessage{From: from, To: to, Content: content, Type: MsgTypeTask}
	if err := p.bus.Send(ctx, msg); err != nil {
		return nil, err
	}

	ch := p.bus.Register(from + "_response")
	select {
	case resp := <-ch:
		return &resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
```

### 四、冲突解决机制

当多个Agent给出矛盾结果时，需要冲突解决策略：

| 策略 | 实现 | 适用场景 |
|------|------|----------|
| 投票 | 多数同意 | 事实性问题 |
| 权威 | 指定Agent拥有最终决定权 | 有明确专家的场景 |
| 证据 | 谁的论据更充分 | 分析类任务 |
| LLM裁判 | 第三方LLM评判 | 开放性问题 |

### 五、总结

多Agent系统的核心挑战在于**编排和协调**：

1. **选择合适的协作模式**：顺序/并行/辩论/层级
2. **设计清晰的通信协议**：消息格式、类型、流向
3. **实现冲突解决机制**：投票、权威、证据、裁判
4. **控制Token消耗**：多Agent交互容易指数级增长
5. **监控和调试**：记录所有Agent间的消息历史

**行业实践**：
- **AutoGen**：微软开源，支持多Agent对话
- **CrewAI**：角色驱动的多Agent框架
- **MetaGPT**：模拟软件公司的多Agent协作
- **LangGraph**：基于图的多Agent工作流
