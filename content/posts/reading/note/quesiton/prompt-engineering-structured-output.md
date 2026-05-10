---
title: "Prompt工程与结构化输出如何保证可靠性"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["Prompt", "大模型", "golang"]
---

## 问题

大模型输出具有不确定性，如何通过Prompt工程和结构化输出技术保证LLM生成结果的可靠性？如何实现JSON Schema约束、输出校验和自动重试？

## 回答

LLM的本质是概率模型，同样的输入可能产生不同的输出。在生产环境中，不可靠的输出意味着系统不可用。Prompt工程和结构化输出是保证可靠性的两大核心手段。

### 一、Prompt工程的核心原则

#### 1.1 结构化Prompt模板

```go
package prompt

import (
	"bytes"
	"text/template"
)

type PromptTemplate struct {
	tmpl *template.Template
}

func NewPromptTemplate(tmplStr string) (*PromptTemplate, error) {
	tmpl, err := template.New("prompt").Parse(tmplStr)
	if err != nil {
		return nil, err
	}
	return &PromptTemplate{tmpl: tmpl}, nil
}

func (p *PromptTemplate) Execute(data map[string]interface{}) (string, error) {
	var buf bytes.Buffer
	if err := p.tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

const SystemPromptTemplate = `You are a {{.Role}}.

Your responsibilities:
{{range .Responsibilities}}
- {{.}}
{{end}}

Rules you must follow:
1. Always respond in valid JSON format matching the provided schema
2. Never fabricate information not present in the context
3. If you are uncertain, explicitly state your uncertainty
4. Do not include any text outside the JSON structure`
```

#### 1.2 Few-Shot示例

```go
type FewShotExample struct {
	Input  string
	Output string
}

func BuildFewShotPrompt(task string, examples []FewShotExample, query string) string {
	prompt := task + "\n\nExamples:\n"
	for i, ex := range examples {
		prompt += fmt.Sprintf("\nExample %d:\nInput: %s\nOutput: %s", i+1, ex.Input, ex.Output)
	}
	prompt += fmt.Sprintf("\n\nNow process this:\nInput: %s\nOutput:", query)
	return prompt
}
```

### 二、结构化输出

#### 2.1 JSON Schema约束

```go
package structured

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

type JSONSchema struct {
	Type       string                  `json:"type"`
	Properties map[string]SchemaProperty `json:"properties,omitempty"`
	Required   []string                `json:"required,omitempty"`
	Items      *JSONSchema             `json:"items,omitempty"`
}

type SchemaProperty struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
	Items       *JSONSchema `json:"items,omitempty"`
}

func GenerateSchema(v interface{}) *JSONSchema {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return generateSchemaFromType(t)
}

func generateSchemaFromType(t reflect.Type) *JSONSchema {
	switch t.Kind() {
	case reflect.Struct:
		schema := &JSONSchema{
			Type:       "object",
			Properties: make(map[string]SchemaProperty),
		}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			jsonTag := field.Tag.Get("json")
			if jsonTag == "-" || jsonTag == "" {
				continue
			}
			name := strings.Split(jsonTag, ",")[0]
			prop := SchemaProperty{
				Type:        goTypeToJSONType(field.Type),
				Description: field.Tag.Get("desc"),
			}
			if enum := field.Tag.Get("enum"); enum != "" {
				prop.Enum = strings.Split(enum, "|")
			}
			schema.Properties[name] = prop
			schema.Required = append(schema.Required, name)
		}
		return schema
	case reflect.Slice:
		return &JSONSchema{
			Type:  "array",
			Items: generateSchemaFromType(t.Elem()),
		}
	case reflect.String:
		return &JSONSchema{Type: "string"}
	case reflect.Int, reflect.Int64:
		return &JSONSchema{Type: "integer"}
	case reflect.Float64:
		return &JSONSchema{Type: "number"}
	case reflect.Bool:
		return &JSONSchema{Type: "boolean"}
	default:
		return &JSONSchema{Type: "string"}
	}
}

func goTypeToJSONType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int64:
		return "integer"
	case reflect.Float64:
		return "number"
	case reflect.Bool:
		return "boolean"
	case reflect.Slice:
		return "array"
	case reflect.Struct:
		return "object"
	default:
		return "string"
	}
}
```

#### 2.2 输出校验与自动重试

```go
type StructuredOutputClient struct {
	llm       LLMClient
	maxRetries int
}

type LLMClient interface {
	Generate(ctx context.Context, messages []Message) (string, error)
}

func NewStructuredOutputClient(llm LLMClient, maxRetries int) *StructuredOutputClient {
	return &StructuredOutputClient{llm: llm, maxRetries: maxRetries}
}

func (c *StructuredOutputClient) GenerateStructured(ctx context.Context, prompt string, schema *JSONSchema, result interface{}) error {
	schemaJSON, _ := json.MarshalIndent(schema, "", "2")

	systemPrompt := fmt.Sprintf(`You must respond with valid JSON that conforms to the following schema:

%s

Important:
- Output ONLY valid JSON, no markdown code blocks, no extra text
- All required fields must be present
- String values must be properly escaped
- Do not include comments in JSON`, string(schemaJSON))

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			errorMsg := fmt.Sprintf("Your previous output was invalid: %v. Please try again, outputting ONLY valid JSON conforming to the schema.", lastErr)
			messages = append(messages, Message{Role: "assistant", Content: "[invalid JSON]"})
			messages = append(messages, Message{Role: "user", Content: errorMsg})
		}

		resp, err := c.llm.Generate(ctx, messages)
		if err != nil {
			return fmt.Errorf("LLM call failed: %w", err)
		}

		cleaned := cleanJSONResponse(resp)

		if err := json.Unmarshal([]byte(cleaned), result); err != nil {
			lastErr = fmt.Errorf("JSON parse error: %w", err)
			continue
		}

		if err := validateResult(result, schema); err != nil {
			lastErr = fmt.Errorf("schema validation error: %w", err)
			continue
		}

		return nil
	}

	return fmt.Errorf("failed after %d retries: %w", c.maxRetries, lastErr)
}

func cleanJSONResponse(resp string) string {
	resp = strings.TrimSpace(resp)

	if strings.HasPrefix(resp, "```json") {
		resp = strings.TrimPrefix(resp, "```json")
		resp = strings.TrimSuffix(resp, "```")
	} else if strings.HasPrefix(resp, "```") {
		resp = strings.TrimPrefix(resp, "```")
		resp = strings.TrimSuffix(resp, "```")
	}

	resp = strings.TrimSpace(resp)
	return resp
}

func validateResult(result interface{}, schema *JSONSchema) error {
	data, _ := json.Marshal(result)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	for _, req := range schema.Required {
		if _, ok := raw[req]; !ok {
			return fmt.Errorf("missing required field: %s", req)
		}
	}

	for name, prop := range schema.Properties {
		val, ok := raw[name]
		if !ok {
			continue
		}
		if len(prop.Enum) > 0 {
			strVal, _ := val.(string)
			if !contains(prop.Enum, strVal) {
				return fmt.Errorf("field %s value %q not in enum %v", name, strVal, prop.Enum)
			}
		}
	}

	return nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
```

### 三、Chain of Thought与Self-Consistency

```go
type CoTGenerator struct {
	llm      LLMClient
	numPaths int
}

func (g *CoTGenerator) GenerateWithSelfConsistency(ctx context.Context, question string) (string, error) {
	var answers []string

	for i := 0; i < g.numPaths; i++ {
		prompt := fmt.Sprintf(`Think step by step to answer the following question. Show your reasoning process.

Question: %s

Let's think step by step:`, question)

		resp, err := g.llm.Generate(ctx, []Message{{Role: "user", Content: prompt}})
		if err != nil {
			continue
		}

		extractPrompt := fmt.Sprintf(`Based on the following reasoning, provide ONLY the final answer (no explanation):

%s

Final answer:`, resp)

		answer, err := g.llm.Generate(ctx, []Message{{Role: "user", Content: extractPrompt}})
		if err != nil {
			continue
		}
		answers = append(answers, strings.TrimSpace(answer))
	}

	return majorityVote(answers), nil
}

func majorityVote(answers []string) string {
	counts := make(map[string]int)
	for _, a := range answers {
		counts[a]++
	}

	var best string
	maxCount := 0
	for a, c := range counts {
		if c > maxCount {
			maxCount = c
			best = a
		}
	}
	return best
}
```

### 四、输出可靠性保障体系

```
层级1: Prompt约束
  → 明确指定输出格式、Schema、示例
  → 告知"只输出JSON，不要其他文本"

层级2: 模型能力
  → 使用支持Function Calling/JSON Mode的模型
  → GPT-4o、Claude 3.5、Qwen等原生支持

层级3: 后处理校验
  → JSON解析 + Schema校验
  → 字段类型、枚举值、必填项检查

层级4: 自动重试
  → 解析失败时将错误信息反馈给LLM重试
  → 最多重试3次

层级5: 兜底策略
  → 重试失败后使用默认值或降级方案
  → 记录失败case用于后续优化
```

### 五、总结

LLM输出可靠性是AI应用生产化的关键挑战。**核心策略**：

1. **Prompt层面**：结构化模板 + Few-Shot + 明确格式要求
2. **模型层面**：使用支持JSON Mode的模型
3. **校验层面**：JSON解析 + Schema校验 + 业务规则校验
4. **容错层面**：自动重试 + 错误反馈 + 兜底策略
5. **评估层面**：持续监控输出质量，优化Prompt和Schema
