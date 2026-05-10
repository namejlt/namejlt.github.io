---
title: "分布式链路追踪如何设计与实现"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["链路追踪", "可观测性", "golang"]
---

## 问题

微服务调用链路复杂，一个请求可能经过数十个服务。如何追踪请求的完整调用链？OpenTelemetry的Trace/Span模型如何设计？如何实现跨服务传播和采样策略？

## 回答

分布式链路追踪是微服务可观测性的三大支柱之一。它通过在请求中传递TraceID和SpanID，将分散在各服务中的日志串联成完整的调用链，是排查性能瓶颈和故障根因的关键工具。

### 一、核心概念

```
Trace: 一次完整的请求链路
  └── Span: 一个服务的一次操作
        ├── SpanID: 当前Span的唯一标识
        ├── ParentSpanID: 父Span的ID
        ├── TraceID: 整条链路的唯一标识
        ├── StartTime: 开始时间
        ├── Duration: 持续时间
        └── Attributes: 附加属性
```

### 二、Go实现

```go
package tracing

import (
	"context"
	"sync"
	"time"
)

type TraceID string
type SpanID string

type Span struct {
	TraceID      TraceID
	SpanID       SpanID
	ParentSpanID SpanID
	Operation    string
	StartTime    time.Time
	EndTime      time.Time
	Attributes   map[string]string
	Events       []SpanEvent
	Status       SpanStatus
	mu           sync.Mutex
}

type SpanEvent struct {
	Name       string
	Timestamp  time.Time
	Attributes map[string]string
}

type SpanStatus int

const (
	StatusOK SpanStatus = iota
	StatusError
)

type Tracer struct {
	serviceName string
	exporter    SpanExporter
	sampler     Sampler
}

func NewTracer(serviceName string, exporter SpanExporter, sampler Sampler) *Tracer {
	return &Tracer{
		serviceName: serviceName,
		exporter:    exporter,
		sampler:     sampler,
	}
}

func (t *Tracer) Start(ctx context.Context, operation string) (context.Context, *Span) {
	parentSpan := SpanFromContext(ctx)

	span := &Span{
		TraceID:    generateTraceID(),
		SpanID:     generateSpanID(),
		Operation:  operation,
		StartTime:  time.Now(),
		Attributes: map[string]string{"service": t.serviceName},
		Events:     make([]SpanEvent, 0),
	}

	if parentSpan != nil {
		span.TraceID = parentSpan.TraceID
		span.ParentSpanID = parentSpan.SpanID
	}

	if !t.sampler.ShouldSample(span) {
		return ctx, span
	}

	return ContextWithSpan(ctx, span), span
}

func (t *Tracer) End(span *Span) {
	span.EndTime = time.Now()
	t.exporter.Export(span)
}

func (s *Span) SetAttribute(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Attributes[key] = value
}

func (s *Span) AddEvent(name string, attrs map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Events = append(s.Events, SpanEvent{
		Name:       name,
		Timestamp:  time.Now(),
		Attributes: attrs,
	})
}

func (s *Span) SetError(err error) {
	s.Status = StatusError
	s.SetAttribute("error", err.Error())
}

func (s *Span) Duration() time.Duration {
	return s.EndTime.Sub(s.StartTime)
}

type contextKey struct{}

func SpanFromContext(ctx context.Context) *Span {
	span, _ := ctx.Value(contextKey{}).(*Span)
	return span
}

func ContextWithSpan(ctx context.Context, span *Span) context.Context {
	return context.WithValue(ctx, contextKey{}, span)
}
```

### 三、跨服务传播

```go
type Propagator struct{}

const (
	TraceIDHeader    = "x-trace-id"
	SpanIDHeader     = "x-span-id"
	ParentSpanIDHeader = "x-parent-span-id"
)

func (p *Propagator) Inject(ctx context.Context, headers map[string]string) {
	span := SpanFromContext(ctx)
	if span == nil {
		return
	}

	headers[TraceIDHeader] = string(span.TraceID)
	headers[SpanIDHeader] = string(span.SpanID)
	headers[ParentSpanIDHeader] = string(span.ParentSpanID)
}

func (p *Propagator) Extract(ctx context.Context, headers map[string]string) context.Context {
	traceID := headers[TraceIDHeader]
	spanID := headers[SpanIDHeader]
	parentSpanID := headers[ParentSpanIDHeader]

	if traceID == "" {
		return ctx
	}

	span := &Span{
		TraceID:      TraceID(traceID),
		SpanID:       SpanID(spanID),
		ParentSpanID: SpanID(parentSpanID),
	}

	return ContextWithSpan(ctx, span)
}
```

### 四、采样策略

```go
type Sampler interface {
	ShouldSample(span *Span) bool
}

type AlwaysSampler struct{}

func (s *AlwaysSampler) ShouldSample(span *Span) bool { return true }

type NeverSampler struct{}

func (s *NeverSampler) ShouldSample(span *Span) bool { return false }

type ProbabilitySampler struct {
	rate float64
}

func NewProbabilitySampler(rate float64) *ProbabilitySampler {
	return &ProbabilitySampler{rate: rate}
}

func (s *ProbabilitySampler) ShouldSample(span *Span) bool {
	return float64(hash(string(span.TraceID))) / float64(1<<32) < s.rate
}

type RateLimitingSampler struct {
	tokens    int
	maxTokens int
	rate      float64
	lastTime  time.Time
	mu        sync.Mutex
}

func NewRateLimitingSampler(rate float64) *RateLimitingSampler {
	return &RateLimitingSampler{
		tokens:    int(rate),
		maxTokens: int(rate),
		rate:      rate,
		lastTime:  time.Now(),
	}
}

func (s *RateLimitingSampler) ShouldSample(span *Span) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(s.lastTime).Seconds()
	s.tokens += int(elapsed * s.rate)
	if s.tokens > s.maxTokens {
		s.tokens = s.maxTokens
	}
	s.lastTime = now

	if s.tokens > 0 {
		s.tokens--
		return true
	}
	return false
}
```

### 五、Span导出

```go
type SpanExporter interface {
	Export(span *Span)
}

type ConsoleExporter struct{}

func (e *ConsoleExporter) Export(span *Span) {
	fmt.Printf("[%s] %s trace=%s span=%s parent=%s duration=%v attrs=%v\n",
		span.Attributes["service"],
		span.Operation,
		span.TraceID,
		span.SpanID,
		span.ParentSpanID,
		span.Duration(),
		span.Attributes,
	)
}

type BatchExporter struct {
	batchSize int
	batch     []*Span
	exporter  SpanExporter
	mu        sync.Mutex
}

func NewBatchExporter(exporter SpanExporter, batchSize int) *BatchExporter {
	be := &BatchExporter{
		batchSize: batchSize,
		batch:     make([]*Span, 0, batchSize),
		exporter:  exporter,
	}
	go be.flushLoop()
	return be
}

func (e *BatchExporter) Export(span *Span) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.batch = append(e.batch, span)
	if len(e.batch) >= e.batchSize {
		e.flush()
	}
}

func (e *BatchExporter) flush() {
	for _, span := range e.batch {
		e.exporter.Export(span)
	}
	e.batch = e.batch[:0]
}

func (e *BatchExporter) flushLoop() {
	ticker := time.NewTicker(5 * time.Second)
	for range ticker.C {
		e.mu.Lock()
		e.flush()
		e.mu.Unlock()
	}
}
```

### 六、HTTP中间件集成

```go
func TracingMiddleware(tracer *Tracer, propagator *Propagator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headers := make(map[string]string)
			for k, v := range r.Header {
				if len(v) > 0 {
					headers[k] = v[0]
				}
			}

			ctx := propagator.Extract(r.Context(), headers)

			operation := fmt.Sprintf("%s %s", r.Method, r.URL.Path)
			ctx, span := tracer.Start(ctx, operation)
			defer tracer.End(span)

			span.SetAttribute("http.method", r.Method)
			span.SetAttribute("http.url", r.URL.String())
			span.SetAttribute("http.remote_addr", r.RemoteAddr)

			rw := &responseWriter{ResponseWriter: w, statusCode: 200}
			next.ServeHTTP(rw, r.WithContext(ctx))

			span.SetAttribute("http.status_code", fmt.Sprintf("%d", rw.statusCode))
			if rw.statusCode >= 400 {
				span.Status = StatusError
			}
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
```

### 七、总结

分布式链路追踪的核心设计：

1. **Trace/Span模型**：一个Trace包含多个Span，形成树状调用链
2. **上下文传播**：通过HTTP Header传递TraceID/SpanID
3. **采样策略**：概率采样 + 限流采样，控制数据量
4. **批量导出**：减少网络开销，提高吞吐
5. **HTTP集成**：中间件自动创建Span，对业务透明

**行业实践**：Jaeger、Zipkin、SkyWalking、OpenTelemetry
