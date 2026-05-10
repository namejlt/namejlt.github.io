---
title: "大模型推理服务如何优化延迟与吞吐"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["大模型", "推理优化", "golang"]
---

## 问题

大模型推理的延迟和吞吐是AI应用落地的关键瓶颈。KV Cache、Continuous Batching、Speculative Decoding等优化技术的原理是什么？如何设计一个高吞吐低延迟的推理服务架构？

## 回答

大模型推理是典型的"内存带宽受限"计算。模型参数和KV Cache的读取速度远慢于计算速度，因此优化的核心不是"算得更快"，而是"减少等待和浪费"。

### 一、推理性能瓶颈分析

**自回归生成的本质**：每生成一个Token，都需要读取全部模型参数和KV Cache，但只做一次矩阵乘法。计算利用率极低。

```
生成1个Token的过程：
1. 读取模型参数（数十GB） → 内存带宽瓶颈
2. 读取KV Cache（数GB）   → 内存带宽瓶颈
3. 计算一次前向传播       → 计算量小
4. 生成1个Token           → 输出
```

**关键指标**：
- **TTFT（Time To First Token）**：首Token延迟，影响用户感知
- **TPOT（Time Per Output Token）**：每Token生成时间，影响吞吐
- **吞吐量**：每秒生成的Token总数

### 二、KV Cache优化

KV Cache是推理优化的基础，缓存Attention的Key和Value矩阵，避免重复计算。

```go
package inference

import (
	"sync"
)

type KVCache struct {
	mu      sync.Mutex
	slots   map[int64]*CacheSlot
	maxSize int
	current int
	lru     *LRUQueue
}

type CacheSlot struct {
	ID       int64
	SeqLen   int
	KeyCache []float32
	ValCache []float32
	LastUsed int64
}

func NewKVCache(maxSize int) *KVCache {
	return &KVCache{
		slots:   make(map[int64]*CacheSlot),
		maxSize: maxSize,
		lru:     NewLRUQueue(),
	}
}

func (c *KVCache) Get(id int64) (*CacheSlot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	slot, ok := c.slots[id]
	if ok {
		slot.LastUsed = currentTime()
		c.lru.Touch(id)
	}
	return slot, ok
}

func (c *KVCache) Put(id int64, slot *CacheSlot) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.current >= c.maxSize {
		evicted := c.lru.Evict()
		if evicted >= 0 {
			delete(c.slots, evicted)
			c.current--
		}
	}

	c.slots[id] = slot
	c.lru.Add(id)
	c.current++
	return nil
}

func (c *KVCache) Delete(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.slots, id)
	c.lru.Remove(id)
	c.current--
}

type LRUQueue struct {
	head *lruNode
	tail *lruNode
}

type lruNode struct {
	id   int64
	prev *lruNode
	next *lruNode
}

func NewLRUQueue() *LRUQueue {
	return &LRUQueue{}
}

func (q *LRUQueue) Add(id int64) {
	node := &lruNode{id: id}
	if q.head == nil {
		q.head = node
		q.tail = node
	} else {
		node.next = q.head
		q.head.prev = node
		q.head = node
	}
}

func (q *LRUQueue) Touch(id int64) {}

func (q *LRUQueue) Remove(id int64) {}

func (q *LRUQueue) Evict() int64 {
	if q.tail == nil {
		return -1
	}
	id := q.tail.id
	if q.tail.prev != nil {
		q.tail.prev.next = nil
		q.tail = q.tail.prev
	} else {
		q.head = nil
		q.tail = nil
	}
	return id
}

func currentTime() int64 { return 0 }
```

**KV Cache的内存计算**：

```
KV Cache大小 = 2 × num_layers × seq_len × hidden_dim × batch_size × sizeof(float16)

示例（Llama-7B, seq_len=2048, batch=32）:
= 2 × 32 × 2048 × 4096 × 32 × 2 bytes
= 32 GB
```

**PagedAttention**：vLLM的核心创新，将KV Cache按固定大小的Page管理，类似操作系统的虚拟内存，消除内存碎片。

### 三、Continuous Batching

传统Static Batching等待所有序列完成后才处理下一批，造成大量计算浪费。Continuous Batching在每个迭代步动态调度，完成的序列立即替换为新序列。

```go
package inference

import (
	"container/heap"
	"context"
	"sync"
	"time"
)

type Request struct {
	ID       int64
	Prompt   []int
	MaxTokens int
	Stream   chan Token
	Done     chan struct{}
}

type Token struct {
	ID    int
	Text  string
	Final bool
}

type Scheduler struct {
	mu          sync.Mutex
	waiting     []*Request
	running     []*Request
	maxBatchSize int
	maxTokens   int
}

func NewScheduler(maxBatchSize, maxTokens int) *Scheduler {
	return &Scheduler{
		maxBatchSize: maxBatchSize,
		maxTokens:    maxTokens,
	}
}

func (s *Scheduler) Submit(req *Request) {
	s.mu.Lock()
	s.waiting = append(s.waiting, req)
	s.mu.Unlock()
}

func (s *Scheduler) Run(ctx context.Context, engine InferenceEngine) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.schedule()
			s.step(engine)
		}
	}
}

func (s *Scheduler) schedule() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for len(s.running) < s.maxBatchSize && len(s.waiting) > 0 {
		req := s.waiting[0]
		s.waiting = s.waiting[1:]
		s.running = append(s.running, req)
	}
}

func (s *Scheduler) step(engine InferenceEngine) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.running) == 0 {
		return
	}

	tokens := engine.BatchDecode(s.running)

	var stillRunning []*Request
	for i, req := range s.running {
		token := tokens[i]
		req.Stream <- token

		if token.Final {
			close(req.Stream)
			close(req.Done)
		} else {
			stillRunning = append(stillRunning, req)
		}
	}

	s.running = stillRunning
}

type InferenceEngine interface {
	BatchDecode(requests []*Request) []Token
}
```

**Continuous Batching vs Static Batching**：

```
Static Batching:
  Seq1: [prefill][decode][decode][decode][idle][idle]
  Seq2: [prefill][decode][decode][decode][decode][decode]
  Seq3: [prefill][decode][decode][idle][idle][idle]
  → 短序列完成后空等长序列

Continuous Batching:
  Seq1: [prefill][decode][decode][decode]✓
  Seq2: [prefill][decode][decode][decode][decode][decode]
  Seq3: [prefill][decode][decode]✓
  Seq4:                [prefill][decode][decode]✓
  → 完成的序列立即被新序列替换
```

### 四、Speculative Decoding

用小模型"猜测"多个Token，大模型并行验证，正确则一次接受多个Token。

```go
type SpeculativeDecoder struct {
	draftModel  InferenceEngine
	targetModel InferenceEngine
	specLength  int
}

func NewSpeculativeDecoder(draft, target InferenceEngine, specLength int) *SpeculativeDecoder {
	return &SpeculativeDecoder{
		draftModel:  draft,
		targetModel: target,
		specLength:  specLength,
	}
}

func (d *SpeculativeDecoder) Generate(ctx context.Context, prompt []int, maxTokens int) []Token {
	var result []Token
	currentTokens := prompt

	for len(result) < maxTokens {
		draftTokens := d.draftModel.GenerateDraft(ctx, currentTokens, d.specLength)

		allTokens := append(currentTokens, tokensToIDs(draftTokens)...)

		verified := d.targetModel.Verify(ctx, allTokens)

		accepted := 0
		for i, vToken := range verified {
			if i < len(draftTokens) {
				if vToken.ID == draftTokens[i].ID {
					accepted++
					result = append(result, vToken)
				} else {
					result = append(result, vToken)
					accepted++
					break
				}
			} else {
				result = append(result, vToken)
				accepted++
				break
			}
		}

		if accepted == 0 {
			break
		}

		newTokens := make([]int, 0, accepted)
		for i := len(result) - accepted; i < len(result); i++ {
			newTokens = append(newTokens, result[i].ID)
		}
		currentTokens = append(currentTokens, newTokens...)
	}

	return result
}
```

**加速原理**：如果小模型猜测5个Token，大模型验证后4个正确，则一次迭代生成了5个Token（4个猜测+1个修正），速度提升约5倍。

### 五、推理服务架构

```
客户端 → 负载均衡 → 推理网关 → GPU Worker Pool
                      ↓
                   请求队列
                   优先级调度
                   批处理引擎
```

```go
type InferenceService struct {
	scheduler  *Scheduler
	engine     InferenceEngine
	gpuWorkers []*GPUWorker
}

type GPUWorker struct {
	id     int
	engine InferenceEngine
	busy   bool
}

func (s *InferenceService) Generate(ctx context.Context, prompt string, maxTokens int) (<-chan Token, error) {
	req := &Request{
		ID:        generateID(),
		MaxTokens: maxTokens,
		Stream:    make(chan Token, 100),
		Done:      make(chan struct{}),
	}

	s.scheduler.Submit(req)

	return req.Stream, nil
}
```

### 六、优化技术总结

| 技术 | 优化目标 | 原理 | 加速比 |
|------|---------|------|--------|
| KV Cache | 减少重复计算 | 缓存Attention的K/V | 2-3x TTFT |
| Continuous Batching | 提升吞吐 | 动态调度，消除空闲 | 2-4x 吞吐 |
| PagedAttention | 减少内存浪费 | 按Page管理KV Cache | 2-4x 吞吐 |
| Speculative Decoding | 降低延迟 | 小模型猜测+大模型验证 | 2-3x 延迟 |
| 量化（INT8/INT4） | 减少内存和带宽 | 降低参数精度 | 2-4x 吞吐 |
| Tensor Parallelism | 加速计算 | 多GPU并行矩阵运算 | 线性扩展 |

### 七、总结

大模型推理优化的核心思路是"减少等待和浪费"：

1. **KV Cache**：避免重复计算已处理的Token
2. **Continuous Batching**：消除序列间的空闲等待
3. **PagedAttention**：消除KV Cache的内存碎片
4. **Speculative Decoding**：用小模型加速大模型
5. **量化**：用精度换速度和内存

**行业实践**：
- **vLLM**：PagedAttention + Continuous Batching，当前最流行的推理框架
- **TensorRT-LLM**：NVIDIA官方，深度优化CUDA内核
- **TGI**：HuggingFace的推理服务，支持Continuous Batching
- **llama.cpp**：CPU/Apple Silicon优化推理，支持量化
