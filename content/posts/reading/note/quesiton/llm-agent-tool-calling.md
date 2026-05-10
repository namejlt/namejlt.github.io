---
title: "LLM Agent工具调用机制如何设计"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["Agent", "大模型", "golang"]
---

## 问题

大模型Agent如何调用外部工具（API、数据库、代码执行器等）？Function Calling的原理是什么？如何设计一个支持多轮工具调用、错误重试、并行执行的Agent框架？

## 回答

LLM Agent的核心能力在于"工具调用"——让大模型不仅能生成文本，还能与外部世界交互。从ReAct到Function Calling，Agent的工具调用机制已经从实验性方案演进为生产级架构。

### 一、Agent工具调用的演进

```
阶段1: Prompt Engineering（纯文本提示）
  → "请使用计算器计算123*456" → 模型输出文本，无法真正调用

阶段2: ReAct（推理+行动循环）
  → Thought → Action → Observation → Thought → ...
  → 模型生成动作描述，代码解析执行

阶段3: Function Calling（原生函数调用）
  → 模型直接输出结构化的函数调用JSON
  → 原生支持，解析可靠，生产可用
```

### 二、Function Calling原理

Function Calling的核心流程：

```
1. 用户提问 + 工具定义 → 发送给LLM
2. LLM判断是否需要调用工具
   → 不需要：直接生成文本回答
   → 需要：生成function_call（函数名+参数JSON）
3. 代码解析function_call，执行对应函数
4. 将函数返回结果追加到消息列表，再次调用LLM
5. LLM基于函数结果生成最终回答
```

### 三、Go语言Agent框架实现

#### 3.1 工具定义与注册

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
)

type ToolParameter struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Required    bool     `json:"required"`
	Enum        []string `json:"enum,omitempty"`
}

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  []ToolParameter `json:"parameters"`
}

type ToolResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

type Tool interface {
	Definition() ToolDefinition
	Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error)
}

type ToolRegistry struct {
	tools map[string]Tool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

func (r *ToolRegistry) Register(tool Tool) {
	r.tools[tool.Definition().Name] = tool
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *ToolRegistry) Definitions() []ToolDefinition {
	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, tool.Definition())
	}
	return defs
}

func (r *ToolRegistry) Execute(ctx context.Context, name string, params json.RawMessage) (*ToolResult, error) {
	tool, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("tool %q not found", name)
	}
	return tool.Execute(ctx, params)
}
```

#### 3.2 内置工具实现

```go
type CalculatorTool struct{}

func (t *CalculatorTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "calculator",
		Description: "Perform mathematical calculations. Supports basic arithmetic operations.",
		Parameters: []ToolParameter{
			{Name: "expression", Type: "string", Description: "Mathematical expression to evaluate, e.g. '2+3*4'", Required: true},
		},
	}
}

func (t *CalculatorTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	var p struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return &ToolResult{Content: fmt.Sprintf("parameter parsing error: %v", err), IsError: true}, nil
	}

	result, err := evalExpression(p.Expression)
	if err != nil {
		return &ToolResult{Content: fmt.Sprintf("calculation error: %v", err), IsError: true}, nil
	}

	return &ToolResult{Content: fmt.Sprintf("%s = %v", p.Expression, result)}, nil
}

type WebSearchTool struct {
	apiKey string
}

func (t *WebSearchTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "web_search",
		Description: "Search the internet for information. Returns top search results.",
		Parameters: []ToolParameter{
			{Name: "query", Type: "string", Description: "Search query", Required: true},
			{Name: "count", Type: "integer", Description: "Number of results to return (1-10)", Required: false},
		},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	var p struct {
		Query string `json:"query"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return &ToolResult{Content: fmt.Sprintf("parameter parsing error: %v", err), IsError: true}, nil
	}
	if p.Count <= 0 || p.Count > 10 {
		p.Count = 5
	}

	results := fmt.Sprintf("Search results for '%s':\n[Simulated] Found %d results", p.Query, p.Count)
	return &ToolResult{Content: results}, nil
}

type DatabaseQueryTool struct {
	db QueryExecutor
}

func (t *DatabaseQueryTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "database_query",
		Description: "Execute SQL queries against the database. Only SELECT queries are allowed.",
		Parameters: []ToolParameter{
			{Name: "sql", Type: "string", Description: "SQL SELECT query to execute", Required: true},
		},
	}
}

func (t *DatabaseQueryTool) Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error) {
	var p struct {
		SQL string `json:"sql"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return &ToolResult{Content: fmt.Sprintf("parameter parsing error: %v", err), IsError: true}, nil
	}

	if !isSafeQuery(p.SQL) {
		return &ToolResult{Content: "Only SELECT queries are allowed for security reasons", IsError: true}, nil
	}

	rows, err := t.db.Query(ctx, p.SQL)
	if err != nil {
		return &ToolResult{Content: fmt.Sprintf("query execution error: %v", err), IsError: true}, nil
	}

	return &ToolResult{Content: rows}, nil
}

func isSafeQuery(sql string) bool {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	return strings.HasPrefix(upper, "SELECT")
}
```

#### 3.3 Agent核心循环

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type LLMClient interface {
	Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*LLMResponse, error)
}

type LLMResponse struct {
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls"`
	FinishReason string   `json:"finish_reason"`
}

type AgentConfig struct {
	MaxIterations int
	MaxToolCalls  int
	RetryAttempts int
	RetryDelay    time.Duration
	Verbose       bool
}

type Agent struct {
	llm     LLMClient
	registry *ToolRegistry
	config  AgentConfig
}

func New(llm LLMClient, registry *ToolRegistry, config AgentConfig) *Agent {
	if config.MaxIterations <= 0 {
		config.MaxIterations = 10
	}
	if config.MaxToolCalls <= 0 {
		config.MaxToolCalls = 5
	}
	if config.RetryAttempts <= 0 {
		config.RetryAttempts = 2
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = time.Second
	}
	return &Agent{
		llm:      llm,
		registry: registry,
		config:   config,
	}
}

func (a *Agent) Run(ctx context.Context, userMessage string) (*AgentResult, error) {
	messages := []Message{
		{Role: "system", Content: "You are a helpful assistant with access to tools. Use tools when needed to answer questions accurately. Always verify information before responding."},
		{Role: "user", Content: userMessage},
	}

	toolDefs := a.registry.Definitions()
	var toolCallLog []ToolCallRecord

	for i := 0; i < a.config.MaxIterations; i++ {
		if a.config.Verbose {
			log.Printf("Iteration %d: sending %d messages to LLM", i+1, len(messages))
		}

		resp, err := a.llm.Chat(ctx, messages, toolDefs)
		if err != nil {
			return nil, fmt.Errorf("LLM call failed at iteration %d: %w", i+1, err)
		}

		if len(resp.ToolCalls) == 0 {
			return &AgentResult{
				Answer:      resp.Content,
				ToolCalls:   toolCallLog,
				Iterations:  i + 1,
			}, nil
		}

		assistantMsg := Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		messages = append(messages, assistantMsg)

		for _, tc := range resp.ToolCalls {
			result, err := a.executeWithRetry(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = &ToolResult{
					Content: fmt.Sprintf("Tool execution error: %v", err),
					IsError: true,
				}
			}

			toolCallLog = append(toolCallLog, ToolCallRecord{
				ToolName:  tc.Name,
				Arguments: string(tc.Arguments),
				Result:    result.Content,
				IsError:   result.IsError,
			})

			messages = append(messages, Message{
				Role:       "tool",
				Content:    result.Content,
				ToolCallID: tc.ID,
				Name:       tc.Name,
			})
		}

		if len(toolCallLog) >= a.config.MaxToolCalls {
			messages = append(messages, Message{
				Role:    "user",
				Content: "You have reached the maximum number of tool calls. Please provide your final answer based on the information gathered so far.",
			})
		}
	}

	return nil, fmt.Errorf("agent exceeded max iterations (%d)", a.config.MaxIterations)
}

func (a *Agent) executeWithRetry(ctx context.Context, name string, params json.RawMessage) (*ToolResult, error) {
	var lastErr error
	for attempt := 0; attempt <= a.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(a.config.RetryDelay):
			}
		}

		result, err := a.registry.Execute(ctx, name, params)
		if err == nil {
			return result, nil
		}
		lastErr = err

		if a.config.Verbose {
			log.Printf("Tool %q attempt %d failed: %v", name, attempt+1, err)
		}
	}
	return nil, lastErr
}

type AgentResult struct {
	Answer     string
	ToolCalls  []ToolCallRecord
	Iterations int
}

type ToolCallRecord struct {
	ToolName  string
	Arguments string
	Result    string
	IsError   bool
}
```

### 四、并行工具调用

当LLM返回多个ToolCall时，可以并行执行：

```go
func (a *Agent) executeToolCallsParallel(ctx context.Context, toolCalls []ToolCall) []Message {
	type indexedResult struct {
		index int
		msg   Message
	}

	resultCh := make(chan indexedResult, len(toolCalls))

	for i, tc := range toolCalls {
		go func(idx int, call ToolCall) {
			result, err := a.executeWithRetry(ctx, call.Name, call.Arguments)
			content := ""
			if err != nil {
				content = fmt.Sprintf("Tool execution error: %v", err)
			} else {
				content = result.Content
			}

			resultCh <- indexedResult{
				index: idx,
				msg: Message{
					Role:       "tool",
					Content:    content,
					ToolCallID: call.ID,
					Name:       call.Name,
				},
			}
		}(i, tc)
	}

	results := make([]Message, len(toolCalls))
	for i := 0; i < len(toolCalls); i++ {
		r := <-resultCh
		results[r.index] = r.msg
	}
	return results
}
```

### 五、工具安全与权限控制

```go
type SecureToolRegistry struct {
	inner    *ToolRegistry
	policies map[string]*ToolPolicy
}

type ToolPolicy struct {
	AllowedRoles   []string
	MaxCallsPerMin int
	RequireApproval bool
	SanitizeInput  func(json.RawMessage) (json.RawMessage, error)
}

func (r *SecureToolRegistry) Execute(ctx context.Context, name string, params json.RawMessage) (*ToolResult, error) {
	policy, ok := r.policies[name]
	if !ok {
		return r.inner.Execute(ctx, name, params)
	}

	role, _ := ctx.Value("role").(string)
	if !contains(policy.AllowedRoles, role) {
		return &ToolResult{Content: "permission denied", IsError: true}, nil
	}

	if policy.SanitizeInput != nil {
		sanitized, err := policy.SanitizeInput(params)
		if err != nil {
			return &ToolResult{Content: fmt.Sprintf("input validation error: %v", err), IsError: true}, nil
		}
		params = sanitized
	}

	return r.inner.Execute(ctx, name, params)
}
```

### 六、Agent模式对比

| 模式 | 原理 | 优点 | 缺点 |
|------|------|------|------|
| ReAct | Thought→Action→Observation循环 | 透明可解释 | Token消耗大 |
| Function Calling | 原生结构化调用 | 高效可靠 | 依赖模型支持 |
| Plan-and-Execute | 先规划再执行 | 适合复杂任务 | 规划可能出错 |
| Multi-Agent | 多Agent协作 | 分工明确 | 通信开销大 |

### 七、总结

LLM Agent的工具调用机制是连接大模型与外部世界的桥梁。**核心设计要点**：

1. **工具定义要清晰**：Description决定LLM能否正确选择工具
2. **参数校验要严格**：防止注入攻击和非法参数
3. **错误处理要完善**：工具执行失败时提供有用信息给LLM重试
4. **并行执行要安全**：无依赖的工具调用可并行，有依赖的必须串行
5. **权限控制要到位**：敏感工具需要审批和限制
