---
title: "AI应用评估与可观测性如何设计"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["AI", "可观测性", "golang"]
---

## 问题

AI应用的输出具有不确定性，如何评估其质量？如何建立可观测性体系来监控LLM调用的延迟、成本、准确率？如何实现自动化的质量评估和告警？

## 回答

AI应用的可观测性比传统软件更重要——传统软件的行为是确定的，而AI应用的输出是概率性的。没有可观测性，AI应用就像一个黑盒，无法发现问题、优化效果和控制系统成本。

### 一、AI应用可观测性的三大支柱

| 维度 | 关注点 | 指标 |
|------|--------|------|
| 质量 | 输出是否正确 | 准确率、幻觉率、相关性 |
| 性能 | 响应是否及时 | TTFT、TPOT、吞吐 |
| 成本 | 花费是否可控 | Token用量、API费用 |

### 二、LLM调用追踪

```go
package observability

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type LLMSpan struct {
	TraceID    string
	SpanID     string
	ParentID   string
	Model      string
	StartTime  time.Time
	EndTime    time.Time
	InputTokens  int
	OutputTokens int
	Prompt     string
	Completion string
	Status     string
	Error      string
	Metadata   map[string]string
}

type TraceCollector struct {
	mu    sync.Mutex
	spans []LLMSpan
}

func NewTraceCollector() *TraceCollector {
	return &TraceCollector{}
}

func (c *TraceCollector) Record(span LLMSpan) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.spans = append(c.spans, span)
}

func (c *TraceCollector) GetSpans(traceID string) []LLMSpan {
	c.mu.Lock()
	defer c.mu.Unlock()

	var result []LLMSpan
	for _, s := range c.spans {
		if s.TraceID == traceID {
			result = append(result, s)
		}
	}
	return result
}

type ObservableLLMClient struct {
	inner     LLMClient
	collector *TraceCollector
}

func NewObservableLLMClient(inner LLMClient, collector *TraceCollector) *ObservableLLMClient {
	return &ObservableLLMClient{inner: inner, collector: collector}
}

func (c *ObservableLLMClient) Generate(ctx context.Context, messages []Message) (string, error) {
	span := LLMSpan{
		TraceID:   getTraceID(ctx),
		SpanID:    generateSpanID(),
		StartTime: time.Now(),
		Model:     "default",
		Prompt:    formatMessages(messages),
		Status:    "started",
	}

	resp, err := c.inner.Generate(ctx, messages)

	span.EndTime = time.Now()
	if err != nil {
		span.Status = "error"
		span.Error = err.Error()
	} else {
		span.Status = "success"
		span.Completion = resp
		span.OutputTokens = estimateTokens(resp)
	}

	span.InputTokens = estimateTokens(span.Prompt)
	c.collector.Record(span)

	return resp, err
}
```

### 三、质量评估框架

```go
package observability

type EvaluationMetric struct {
	Name        string
	Description string
	Score       float32
	Reasoning   string
}

type Evaluator interface {
	Evaluate(ctx context.Context, input, output, expected string) (*EvaluationMetric, error)
}

type LLMAsJudgeEvaluator struct {
	llm LLMClient
}

func NewLLMAsJudgeEvaluator(llm LLMClient) *LLMAsJudgeEvaluator {
	return &LLMAsJudgeEvaluator{llm: llm}
}

func (e *LLMAsJudgeEvaluator) Evaluate(ctx context.Context, input, output, expected string) (*EvaluationMetric, error) {
	prompt := fmt.Sprintf(`Evaluate the following AI response on a scale of 0 to 1.

Input: %s
AI Response: %s
Expected Output: %s

Evaluate on these criteria:
1. Relevance (0-1): Is the response relevant to the input?
2. Accuracy (0-1): Is the information correct?
3. Completeness (0-1): Does it cover all aspects?
4. Hallucination (0-1): Does it contain fabricated information? (1 = no hallucination)

Respond in JSON:
{"relevance": 0.0, "accuracy": 0.0, "completeness": 0.0, "hallucination_free": 0.0, "overall": 0.0, "reasoning": "..."}`, input, output, expected)

	resp, err := e.llm.Generate(ctx, []Message{{Role: "user", Content: prompt}})
	if err != nil {
		return nil, err
	}

	var result struct {
		Relevance       float32 `json:"relevance"`
		Accuracy        float32 `json:"accuracy"`
		Completeness    float32 `json:"completeness"`
		HallucinationFree float32 `json:"hallucination_free"`
		Overall         float32 `json:"overall"`
		Reasoning       string  `json:"reasoning"`
	}

	json.Unmarshal([]byte(cleanJSON(resp)), &result)

	return &EvaluationMetric{
		Name:        "llm_judge",
		Description: "LLM-as-Judge evaluation",
		Score:       result.Overall,
		Reasoning:   result.Reasoning,
	}, nil
}

type RuleBasedEvaluator struct{}

func NewRuleBasedEvaluator() *RuleBasedEvaluator {
	return &RuleBasedEvaluator{}
}

func (e *RuleBasedEvaluator) Evaluate(ctx context.Context, input, output, expected string) (*EvaluationMetric, error) {
	var score float32

	if output != "" {
		score += 0.2
	}

	if len(output) > 10 {
		score += 0.2
	}

	if containsAny(output, []string{"I don't know", "I cannot", "I'm not sure"}) {
		score += 0.3
	}

	if expected != "" && similarity(output, expected) > 0.7 {
		score += 0.3
	}

	return &EvaluationMetric{
		Name:        "rule_based",
		Description: "Rule-based evaluation",
		Score:       score,
	}, nil
}
```

### 四、指标聚合与监控

```go
package observability

import (
	"sync"
	"sync/atomic"
	"time"
)

type Metrics struct {
	TotalRequests    int64
	SuccessRequests  int64
	FailedRequests   int64
	TotalInputTokens int64
	TotalOutputTokens int64
	TotalLatency     int64
	AvgLatency       float64
	AvgQualityScore  float64
}

type MetricsCollector struct {
	mu             sync.Mutex
	metrics        Metrics
	qualityScores  []float64
	latencyBuckets map[string]int64
	costPerToken   float64
}

func NewMetricsCollector(costPerToken float64) *MetricsCollector {
	return &MetricsCollector{
		costPerToken:   costPerToken,
		latencyBuckets: make(map[string]int64),
	}
}

func (m *MetricsCollector) RecordRequest(success bool, inputTokens, outputTokens int, latency time.Duration, qualityScore float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	atomic.AddInt64(&m.metrics.TotalRequests, 1)
	if success {
		atomic.AddInt64(&m.metrics.SuccessRequests, 1)
	} else {
		atomic.AddInt64(&m.metrics.FailedRequests, 1)
	}

	atomic.AddInt64(&m.metrics.TotalInputTokens, int64(inputTokens))
	atomic.AddInt64(&m.metrics.TotalOutputTokens, int64(outputTokens))
	atomic.AddInt64(&m.metrics.TotalLatency, int64(latency))

	if qualityScore > 0 {
		m.qualityScores = append(m.qualityScores, qualityScore)
	}

	bucket := latencyBucket(latency)
	m.latencyBuckets[bucket]++
}

func (m *MetricsCollector) GetMetrics() Metrics {
	m.mu.Lock()
	defer m.mu.Unlock()

	metrics := m.metrics
	if metrics.TotalRequests > 0 {
		metrics.AvgLatency = float64(metrics.TotalLatency) / float64(metrics.TotalRequests) / float64(time.Millisecond)
	}

	if len(m.qualityScores) > 0 {
		var sum float64
		for _, s := range m.qualityScores {
			sum += s
		}
		metrics.AvgQualityScore = sum / float64(len(m.qualityScores))
	}

	return metrics
}

func (m *MetricsCollector) GetCost() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	totalTokens := m.metrics.TotalInputTokens + m.metrics.TotalOutputTokens
	return float64(totalTokens) * m.costPerToken
}

func latencyBucket(d time.Duration) string {
	ms := d.Milliseconds()
	switch {
	case ms < 100:
		return "<100ms"
	case ms < 500:
		return "100-500ms"
	case ms < 1000:
		return "500ms-1s"
	case ms < 3000:
		return "1-3s"
	default:
		return ">3s"
	}
}
```

### 五、告警系统

```go
type AlertRule struct {
	Name      string
	Condition func(metrics Metrics) bool
	Severity  string
	Message   string
}

type AlertManager struct {
	rules   []AlertRule
	channel chan Alert
}

type Alert struct {
	Rule      AlertRule
	Timestamp time.Time
	Metrics   Metrics
}

func NewAlertManager() *AlertManager {
	return &AlertManager{
		channel: make(chan Alert, 100),
	}
}

func (a *AlertManager) AddRule(rule AlertRule) {
	a.rules = append(a.rules, rule)
}

func (a *AlertManager) Check(metrics Metrics) {
	for _, rule := range a.rules {
		if rule.Condition(metrics) {
			a.channel <- Alert{
				Rule:      rule,
				Timestamp: time.Now(),
				Metrics:   metrics,
			}
		}
	}
}

func (a *AlertManager) Alerts() <-chan Alert {
	return a.channel
}

func DefaultAlertRules() []AlertRule {
	return []AlertRule{
		{
			Name:     "high_error_rate",
			Severity: "critical",
			Message:  "Error rate exceeds 10%",
			Condition: func(m Metrics) bool {
				if m.TotalRequests == 0 {
					return false
				}
				return float64(m.FailedRequests)/float64(m.TotalRequests) > 0.1
			},
		},
		{
			Name:     "high_latency",
			Severity: "warning",
			Message:  "Average latency exceeds 3 seconds",
			Condition: func(m Metrics) bool {
				return m.AvgLatency > 3000
			},
		},
		{
			Name:     "low_quality",
			Severity: "warning",
			Message:  "Average quality score below 0.7",
			Condition: func(m Metrics) bool {
				return m.AvgQualityScore > 0 && m.AvgQualityScore < 0.7
			},
		},
		{
			Name:     "cost_spike",
			Severity: "warning",
			Message:  "Token usage spike detected",
			Condition: func(m Metrics) bool {
				return m.TotalInputTokens+m.TotalOutputTokens > 1000000
			},
		},
	}
}
```

### 六、可观测性架构

```
LLM调用 → Span记录 → TraceCollector → 指标聚合 → 告警检测
                ↓                        ↓
            日志存储                   仪表盘
                ↓                        ↓
            质量评估 → Evaluator → 质量分数 → 趋势分析
```

### 七、总结

AI应用可观测性的核心是**"让黑盒变白盒"**：

1. **追踪**：记录每次LLM调用的完整信息
2. **评估**：自动化评估输出质量（LLM-as-Judge + 规则）
3. **监控**：实时聚合延迟、成本、质量指标
4. **告警**：异常时及时通知（错误率、延迟、质量、成本）
5. **迭代**：基于数据持续优化Prompt和系统

**行业实践**：
- **LangSmith**：LangChain的可观测性平台
- **Helicone**：LLM调用监控和缓存
- **Arize AI**：ML可观测性平台
- **Weights & Biases Weave**：LLM实验追踪
