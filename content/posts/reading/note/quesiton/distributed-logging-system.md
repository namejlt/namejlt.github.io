---
title: "分布式日志系统如何设计"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["日志", "分布式", "golang"]
---

## 问题

微服务架构中，日志分散在各个服务实例中，如何实现统一的日志采集、存储和查询？ELK/EFK架构的原理是什么？如何设计一个高性能的异步日志库？

## 回答

日志是系统可观测性的基础。在分布式系统中，日志的采集、传输、存储和查询都面临规模化挑战。一个设计良好的日志系统需要在性能、可靠性和成本之间取得平衡。

### 一、日志系统架构

```
应用服务 → 日志Agent → 消息队列 → 日志处理 → 存储引擎 → 查询界面
           (Filebeat)  (Kafka)   (Logstash)  (Elasticsearch)
```

### 二、高性能异步日志库

```go
package logger

import (
	"io"
	"os"
	"sync"
	"time"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
	FATAL
)

func (l Level) String() string {
	switch l {
	case DEBUG: return "DEBUG"
	case INFO:  return "INFO"
	case WARN:  return "WARN"
	case ERROR: return "ERROR"
	case FATAL: return "FATAL"
	default:    return "UNKNOWN"
	}
}

type LogEntry struct {
	Level   Level
	Time    time.Time
	Message string
	Fields  map[string]interface{}
}

type AsyncLogger struct {
	level     Level
	output    io.Writer
	entryCh   chan *LogEntry
	buffer    []*LogEntry
	bufSize   int
	flushInterval time.Duration
	mu        sync.Mutex
	wg        sync.WaitGroup
	closeCh   chan struct{}
}

func NewAsyncLogger(level Level, output io.Writer, queueSize, bufSize int, flushInterval time.Duration) *AsyncLogger {
	l := &AsyncLogger{
		level:     level,
		output:    output,
		entryCh:   make(chan *LogEntry, queueSize),
		buffer:    make([]*LogEntry, 0, bufSize),
		bufSize:   bufSize,
		flushInterval: flushInterval,
		closeCh:   make(chan struct{}),
	}

	l.wg.Add(1)
	go l.consume()

	return l
}

func (l *AsyncLogger) Debug(msg string, fields map[string]interface{}) {
	l.log(DEBUG, msg, fields)
}

func (l *AsyncLogger) Info(msg string, fields map[string]interface{}) {
	l.log(INFO, msg, fields)
}

func (l *AsyncLogger) Warn(msg string, fields map[string]interface{}) {
	l.log(WARN, msg, fields)
}

func (l *AsyncLogger) Error(msg string, fields map[string]interface{}) {
	l.log(ERROR, msg, fields)
}

func (l *AsyncLogger) Fatal(msg string, fields map[string]interface{}) {
	l.log(FATAL, msg, fields)
	os.Exit(1)
}

func (l *AsyncLogger) log(level Level, msg string, fields map[string]interface{}) {
	if level < l.level {
		return
	}

	entry := &LogEntry{
		Level:   level,
		Time:    time.Now(),
		Message: msg,
		Fields:  fields,
	}

	select {
	case l.entryCh <- entry:
	default:
	}
}

func (l *AsyncLogger) consume() {
	defer l.wg.Done()

	ticker := time.NewTicker(l.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case entry := <-l.entryCh:
			l.mu.Lock()
			l.buffer = append(l.buffer, entry)
			if len(l.buffer) >= l.bufSize {
				l.flush()
			}
			l.mu.Unlock()

		case <-ticker.C:
			l.mu.Lock()
			l.flush()
			l.mu.Unlock()

		case <-l.closeCh:
			l.mu.Lock()
			for {
				select {
				case entry := <-l.entryCh:
					l.buffer = append(l.buffer, entry)
				default:
					l.flush()
					l.mu.Unlock()
					return
				}
			}
		}
	}
}

func (l *AsyncLogger) flush() {
	if len(l.buffer) == 0 {
		return
	}

	for _, entry := range l.buffer {
		line := formatEntry(entry)
		l.output.Write([]byte(line))
		l.output.Write([]byte("\n"))
	}

	l.buffer = l.buffer[:0]
}

func formatEntry(entry *LogEntry) string {
	fields := ""
	for k, v := range entry.Fields {
		fields += fmt.Sprintf(" %s=%v", k, v)
	}
	return fmt.Sprintf("[%s] %s %s%s", entry.Level, entry.Time.Format(time.RFC3339), entry.Message, fields)
}

func (l *AsyncLogger) Close() {
	close(l.closeCh)
	l.wg.Wait()
}
```

### 三、结构化日志

```go
type StructuredLogger struct {
	inner *AsyncLogger
}

func NewStructuredLogger(inner *AsyncLogger) *StructuredLogger {
	return &StructuredLogger{inner: inner}
}

func (l *StructuredLogger) WithFields(fields map[string]interface{}) *LoggerContext {
	return &LoggerContext{
		logger: l.inner,
		fields: fields,
	}
}

type LoggerContext struct {
	logger *AsyncLogger
	fields map[string]interface{}
}

func (c *LoggerContext) Info(msg string, extraFields ...map[string]interface{}) {
	merged := c.mergeFields(extraFields...)
	c.logger.Info(msg, merged)
}

func (c *LoggerContext) Error(msg string, extraFields ...map[string]interface{}) {
	merged := c.mergeFields(extraFields...)
	c.logger.Error(msg, merged)
}

func (c *LoggerContext) mergeFields(extra ...map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{}, len(c.fields))
	for k, v := range c.fields {
		merged[k] = v
	}
	for _, m := range extra {
		for k, v := range m {
			merged[k] = v
		}
	}
	return merged
}
```

### 四、日志采集Agent

```go
package logagent

import (
	"bufio"
	"context"
	"os"
	"time"
)

type FileWatcher struct {
	path     string
	offset   int64
	output   chan string
	position *os.File
}

func NewFileWatcher(path string, output chan string) *FileWatcher {
	return &FileWatcher{
		path:   path,
		output: output,
	}
}

func (w *FileWatcher) Start(ctx context.Context) error {
	file, err := os.Open(w.path)
	if err != nil {
		return err
	}

	file.Seek(w.offset, 0)
	reader := bufio.NewReader(file)

	go func() {
		defer file.Close()

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				w.savePosition()
				return
			case <-ticker.C:
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						break
					}
					if line != "" {
						w.output <- line
						w.offset += int64(len(line))
					}
				}
			}
		}
	}()

	return nil
}

func (w *FileWatcher) savePosition() {
	posFile, err := os.Create(w.path + ".pos")
	if err != nil {
		return
	}
	defer posFile.Close()
	fmt.Fprintf(posFile, "%d", w.offset)
}
```

### 五、日志级别与采样

```go
type SamplingConfig struct {
	Initial    int
	Thereafter int
	Tick       time.Duration
}

type SamplingLogger struct {
	inner    *AsyncLogger
	counters map[string]*samplingCounter
	mu       sync.Mutex
}

type samplingCounter struct {
	count    int
	lastTick time.Time
	initial  int
	thereafter int
	tick     time.Duration
}

func (s *SamplingLogger) log(level Level, msg string, fields map[string]interface{}) {
	key := fmt.Sprintf("%d:%s", level, msg)

	s.mu.Lock()
	counter, ok := s.counters[key]
	if !ok {
		counter = &samplingCounter{
			initial:    10,
			thereafter: 100,
			tick:       time.Second,
			lastTick:   time.Now(),
		}
		s.counters[key] = counter
	}
	s.mu.Unlock()

	now := time.Now()
	if now.Sub(counter.lastTick) > counter.tick {
		counter.count = 0
		counter.lastTick = now
	}

	counter.count++

	if counter.count <= counter.initial {
		s.inner.log(level, msg, fields)
		return
	}

	if (counter.count-counter.initial)%counter.thereafter == 0 {
		fields["sampled_count"] = counter.count
		s.inner.log(level, msg, fields)
	}
}
```

### 六、总结

分布式日志系统的核心设计要点：

1. **异步写入**：日志不能阻塞业务线程，使用缓冲+批量写入
2. **结构化**：JSON格式便于检索和分析
3. **采集**：Agent实时监控日志文件，增量读取
4. **采样**：高频日志采样，避免存储爆炸
5. **级别**：生产环境INFO级别，关键路径DEBUG

**行业实践**：ELK（Elasticsearch+Logstash+Kibana）、EFK（Elasticsearch+Fluentd+Kibana）、Loki（Grafana轻量级日志）
