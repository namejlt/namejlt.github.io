---
title: "高并发下的限流算法如何设计与实现"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["限流", "高并发", "golang"]
---

## 问题

在高并发系统中，如何设计限流策略来保护后端服务不被过载？固定窗口、滑动窗口、令牌桶、漏桶这四种限流算法各自的原理、优劣和适用场景是什么？如何用Go语言实现生产级的限流器？

## 回答

限流是保护分布式系统的第一道防线。当流量超过系统承载能力时，限流通过拒绝部分请求来保证核心功能的可用性。选择错误的限流算法可能导致限流效果不佳，甚至反而加剧系统问题。

### 一、限流的核心概念

**限流（Rate Limiting）**：在给定时间窗口内，限制某个资源被访问的次数或速率。超过限制的请求将被拒绝、排队或降级。

**关键指标**：
- **QPS（Queries Per Second）**：每秒请求数，衡量系统吞吐量
- **阈值（Threshold）**：允许通过的最大请求数
- **拒绝策略**：超限请求的处理方式（直接拒绝、排队等待、降级处理）

### 二、四种限流算法深度剖析

#### 1. 固定窗口计数器（Fixed Window）

**原理**：将时间划分为固定大小的窗口（如每秒、每分钟），在每个窗口内维护一个计数器，请求到达时计数器加1，超过阈值则拒绝。窗口结束时计数器归零。

```
时间轴: |--- 窗口1 ---|--- 窗口2 ---|
计数:   0→1→2→...→100  0→1→2→...
        ↑ 阈值100     ↑ 重置
```

**临界突变问题**：

这是固定窗口最致命的缺陷。假设阈值100/秒，在窗口1的最后100ms涌入100个请求，窗口2的前100ms又涌入100个请求。从业务角度看，200ms内通过了200个请求，瞬时QPS达到了1000，是阈值的10倍。

```
窗口1:                    窗口2:
[0...100ms...200ms...900ms|100ms][100ms|200ms...]
                         100请求↑    ↑100请求
                    ← 200ms内200请求，QPS=1000 →
```

**Go实现**：

```go
package ratelimit

import (
	"sync"
	"time"
)

type FixedWindowLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	count    int
	windowStart time.Time
}

func NewFixedWindow(limit int, window time.Duration) *FixedWindowLimiter {
	return &FixedWindowLimiter{
		limit:  limit,
		window: window,
	}
}

func (l *FixedWindowLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if now.Sub(l.windowStart) >= l.window {
		l.count = 0
		l.windowStart = now
	}

	if l.count >= l.limit {
		return false
	}

	l.count++
	return true
}
```

**适用场景**：对限流精度要求不高、资源限制严格的场景，如API调用次数限制（每日100次）。

#### 2. 滑动窗口计数器（Sliding Window）

**原理**：将固定窗口细分为更小的格子，窗口随时间滑动。每到一个新的小格子时间，窗口向前滑动一格，丢弃最老格子的计数，加入新格子。

```
固定窗口:  |-------- 窗口 --------|
滑动窗口:  [格1][格2][格3][格4][格5]
                [格2][格3][格4][格5][格6]  ← 滑动后
```

**为什么能解决临界问题**：滑动窗口的统计范围始终覆盖最近一个完整窗口期，不会出现"两个半窗口叠加"的问题。格子的粒度越细，限流越平滑。

**Go实现**：

```go
package ratelimit

import (
	"sync"
	"time"
)

type SlidingWindowLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	slots    int
	counters []int
	slotTime time.Duration
	lastSlot int64
}

func NewSlidingWindow(limit int, window time.Duration, slots int) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		limit:    limit,
		window:   window,
		slots:    slots,
		counters: make([]int, slots),
		slotTime: window / time.Duration(slots),
	}
}

func (l *SlidingWindowLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	currentSlot := now.UnixMilli() / l.slotTime.Milliseconds()

	l.advanceSlots(currentSlot)

	total := 0
	for _, c := range l.counters {
		total += c
	}

	if total >= l.limit {
		return false
	}

	idx := currentSlot % int64(l.slots)
	l.counters[idx]++
	return true
}

func (l *SlidingWindowLimiter) advanceSlots(currentSlot int64) {
	if l.lastSlot == 0 {
		l.lastSlot = currentSlot
		return
	}

	diff := currentSlot - l.lastSlot
	if diff <= 0 {
		return
	}

	if diff >= int64(l.slots) {
		for i := range l.counters {
			l.counters[i] = 0
		}
	} else {
		for i := int64(0); i < diff; i++ {
			idx := (currentSlot - int64(l.slots) + i) % int64(l.slots)
			if idx >= 0 {
				l.counters[idx] = 0
			}
		}
	}

	l.lastSlot = currentSlot
}
```

**适用场景**：需要平滑限流的场景，如消息队列消费限速、API网关限流。Sentinel默认采用滑动窗口。

#### 3. 漏桶算法（Leaky Bucket）

**原理**：请求如同水倒入漏桶，桶以固定速率漏水（处理请求）。桶满时新请求被拒绝。无论请求多么突发，处理速率始终恒定。

```
请求流入 →  ┌─────────┐  → 固定速率流出（处理）
            │ 漏桶     │
            │ ████     │  ← 桶满则拒绝
            └─────────┘
```

**核心特征**：**强行平滑流量**。不管输入流量多突发，输出永远是匀速的。这意味着即使系统有空闲处理能力，也不能加速处理积压的请求。

**Go实现**：

```go
package ratelimit

import (
	"sync"
	"time"
)

type LeakyBucketLimiter struct {
	mu       sync.Mutex
	rate     float64
	capacity int
	water    float64
	lastLeak time.Time
}

func NewLeakyBucket(rate float64, capacity int) *LeakyBucketLimiter {
	return &LeakyBucketLimiter{
		rate:     rate,
		capacity: capacity,
		lastLeak: time.Now(),
	}
}

func (l *LeakyBucketLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(l.lastLeak).Seconds()
	l.water -= elapsed * l.rate
	if l.water < 0 {
		l.water = 0
	}
	l.lastLeak = now

	if l.water >= float64(l.capacity) {
		return false
	}

	l.water++
	return true
}
```

**适用场景**：需要严格匀速处理请求的场景，如秒杀系统的订单处理、日志写入、与第三方API的交互（对方有严格的QPS限制）。

#### 4. 令牌桶算法（Token Bucket）

**原理**：系统以固定速率向桶中放入令牌，请求到达时需要从桶中取走一个令牌，桶空则拒绝请求。桶有最大容量，允许积累一定量的令牌以应对突发流量。

```
          令牌以固定速率放入
               ↓ ↓ ↓
            ┌─────────┐
请求 → 取令牌 │ ○○○○○   │
            │ ○○○     │  ← 桶满则停止放入
            └─────────┘
```

**与漏桶的本质区别**：令牌桶**允许突发流量**。如果桶中积累了N个令牌，那么瞬间可以处理N个请求，之后恢复到平均速率。这使得令牌桶在保证平均速率的同时，不会浪费系统的瞬时处理能力。

**Go实现**：

```go
package ratelimit

import (
	"sync"
	"time"
)

type TokenBucketLimiter struct {
	mu         sync.Mutex
	rate       float64
	burst      int
	tokens     float64
	lastRefill time.Time
}

func NewTokenBucket(rate float64, burst int) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		rate:       rate,
		burst:      burst,
		tokens:     float64(burst),
		lastRefill: time.Now(),
	}
}

func (l *TokenBucketLimiter) Allow() bool {
	return l.AllowN(1)
}

func (l *TokenBucketLimiter) AllowN(n int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(l.lastRefill).Seconds()
	l.tokens += elapsed * l.rate
	if l.tokens > float64(l.burst) {
		l.tokens = float64(l.burst)
	}
	l.lastRefill = now

	if l.tokens < float64(n) {
		return false
	}

	l.tokens -= float64(n)
	return true
}

func (l *TokenBucketLimiter) Wait(n int) {
	for {
		if l.AllowN(n) {
			return
		}
		time.Sleep(time.Millisecond * 10)
	}
}
```

**适用场景**：大多数互联网场景的限流，如API网关、微服务间调用限流。Guava RateLimiter、Nginx limit_req均采用令牌桶。

### 三、四种算法对比

| 维度 | 固定窗口 | 滑动窗口 | 漏桶 | 令牌桶 |
|------|---------|---------|------|--------|
| 平滑性 | 差（临界突变） | 较好 | 极好（绝对匀速） | 好（允许突发） |
| 突发流量处理 | 不支持 | 部分支持 | 不支持 | 支持 |
| 实现复杂度 | 低 | 中 | 中 | 中 |
| 内存消耗 | 极低 | 中（需存格子） | 极低 | 极低 |
| 精确度 | 低 | 高 | 高 | 高 |
| 典型应用 | 简单配额限制 | Sentinel | 第三方API调用 | API网关、Guava |

### 四、分布式限流

单机限流只能保护单个实例，在集群部署时需要分布式限流来保证全局阈值。

#### 基于Redis的滑动窗口限流

```go
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

type RedisSlidingWindowLimiter struct {
	client *redis.Client
	key    string
	limit  int
	window time.Duration
}

func NewRedisSlidingWindow(client *redis.Client, key string, limit int, window time.Duration) *RedisSlidingWindowLimiter {
	return &RedisSlidingWindowLimiter{
		client: client,
		key:    key,
		limit:  limit,
		window: window,
	}
}

func (l *RedisSlidingWindowLimiter) Allow(ctx context.Context) (bool, error) {
	now := time.Now().UnixMilli()
	windowStart := now - l.window.Milliseconds()

	pipe := l.client.Pipeline()

	pipe.ZRemRangeByScore(ctx, l.key, "0", fmt.Sprintf("%d", windowStart))

	pipe.ZCard(ctx, l.key)

	member := fmt.Sprintf("%d:%d", now, now%1000)
	pipe.ZAdd(ctx, l.key, &redis.Z{
		Score:  float64(now),
		Member: member,
	})

	pipe.Expire(ctx, l.key, l.window+time.Second)

	results, err := pipe.Exec(ctx)
	if err != nil {
		return false, err
	}

	count := results[1].(*redis.IntCmd).Val()

	if count >= int64(l.limit) {
		return false, nil
	}

	return true, nil
}
```

**Lua脚本保证原子性**（生产级推荐）：

```go
const slidingWindowScript = `
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local member = ARGV[4]

local windowStart = now - window
redis.call('ZREMRANGEBYSCORE', key, '-inf', windowStart)

local count = redis.call('ZCARD', key)
if count >= limit then
    return 0
end

redis.call('ZADD', key, now, member)
redis.call('PEXPIRE', key, window + 1000)
return 1
`

func (l *RedisSlidingWindowLimiter) AllowWithLua(ctx context.Context) (bool, error) {
	now := time.Now().UnixMilli()
	member := fmt.Sprintf("%d:%d", now, now%10000)

	result, err := l.client.Eval(ctx, slidingWindowScript,
		[]string{l.key},
		l.limit,
		l.window.Milliseconds(),
		now,
		member,
	).Int()

	if err != nil {
		return false, err
	}

	return result == 1, nil
}
```

### 五、限流在微服务架构中的位置

```
客户端 → CDN/WAF限流 → API网关限流 → 微服务限流 → 数据库连接池限流
         (IP级)        (租户/API级)   (实例级)     (资源级)
```

**多级限流策略**：
1. **入口层**（Nginx/网关）：基于IP、租户、API路径的粗粒度限流，使用令牌桶
2. **服务层**（应用内）：基于实例的细粒度限流，使用滑动窗口或令牌桶
3. **资源层**（数据库/缓存）：基于连接数的资源保护限流，使用固定窗口或信号量

### 六、拒绝策略

限流触发后如何处理被拒绝的请求，直接影响用户体验：

| 策略 | 实现 | 适用场景 |
|------|------|----------|
| 直接拒绝 | 返回429/503 | API网关、非核心功能 |
| 排队等待 | 请求进入队列，超时则拒绝 | 秒杀、下单 |
| 降级处理 | 返回缓存数据或默认值 | 推荐系统、搜索 |
| 匀速排队 | 控制请求匀速通过 | 消息消费 |

### 七、总结

限流算法的选择取决于业务特征：

- **需要允许突发流量**（大多数互联网场景）→ **令牌桶**
- **需要严格匀速**（第三方API调用、日志写入）→ **漏桶**
- **需要精确统计**（监控、计费）→ **滑动窗口**
- **简单配额限制**（每日调用次数）→ **固定窗口**

在分布式环境中，基于Redis + Lua的滑动窗口限流是最常用的方案，兼顾了精确性和原子性。对于性能要求极高的场景，可以采用本地限流 + 全局限流的两级架构，本地限流作为快速过滤，全局限流作为精确控制。
