---
title: "高可用架构中的熔断器如何设计"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["熔断器", "高可用", "golang"]
---

## 问题

在微服务架构中，下游服务故障可能导致上游服务线程池耗尽、级联失败，最终造成整个系统雪崩。熔断器如何防止故障扩散？三种熔断状态（Closed/Open/Half-Open）如何转换？如何用Go实现一个生产级的熔断器？

## 回答

熔断器是微服务容错的核心组件，灵感来源于电路中的保险丝——当电流过载时自动断开，防止设备烧毁。在软件系统中，当下游服务异常率超过阈值时，熔断器自动"断开"，快速失败而非等待超时，从而保护上游服务不被拖垮。

### 一、为什么需要熔断器

**没有熔断器的级联失败**：

```
服务A → 服务B → 服务C（故障，响应时间从50ms变为30s）
         ↑
    线程池被C的慢请求占满
    新请求排队等待
    A的线程池也被占满
    整个调用链崩溃
```

**有熔断器的保护**：

```
服务A → 服务B → [熔断器] → 服务C（故障）
                      ↓
                   快速失败（5ms）
                   不占用线程资源
                   A和B正常运行
```

### 二、熔断器的三种状态

```
         成功率正常                错误率超阈值
  ┌──────────────────┐      ┌──────────────────┐
  │                  │      │                  │
  │    Closed        │─────→│    Open          │
  │  (正常通过)       │      │  (快速失败)       │
  │                  │      │                  │
  └──────────────────┘      └───────┬──────────┘
         ↑                          │
         │     探测成功              │ 超时后
         │                          ↓
         │              ┌──────────────────┐
         └──────────────│  Half-Open       │
                        │  (放行少量请求)    │
                        └──────────────────┘
                               │ 探测失败
                               ↓
                          回到Open状态
```

- **Closed（关闭）**：正常状态，请求正常通过，统计成功/失败率
- **Open（打开）**：熔断状态，所有请求快速失败，不调用下游服务
- **Half-Open（半开）**：探测状态，放行少量请求测试下游是否恢复

### 三、熔断器核心指标

1. **错误率阈值**：触发熔断的错误百分比（如50%）
2. **最小请求数**：统计窗口内的最小请求数，避免样本不足误判（如20个）
3. **统计窗口**：统计错误率的时间窗口（如10秒）
4. **熔断持续时间**：Open状态持续多久后进入Half-Open（如30秒）
5. **半开探测数**：Half-Open状态放行的请求数（如5个）

### 四、Go语言实现

#### 4.1 基于错误率的熔断器

```go
package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("circuit breaker is open")

type State int

const (
	StateClosed    State = iota
	StateOpen
	StateHalfOpen
)

type Counts struct {
	Requests       int64
	TotalFailures  int64
	TotalSuccesses int64
	ConsecutiveFailures  int64
	ConsecutiveSuccesses int64
}

func (c *Counts) clear() {
	*c = Counts{}
}

func (c *Counts) onFailure() {
	c.Requests++
	c.TotalFailures++
	c.ConsecutiveFailures++
	c.ConsecutiveSuccesses = 0
}

func (c *Counts) onSuccess() {
	c.Requests++
	c.TotalSuccesses++
	c.ConsecutiveSuccesses++
	c.ConsecutiveFailures = 0
}

type Settings struct {
	Name          string
	MaxRequests   uint32
	Interval      time.Duration
	Timeout       time.Duration
	FailureRatio  float64
	ReadyToTrip   func(counts Counts) bool
	OnStateChange func(name string, from State, to State)
}

type CircuitBreaker struct {
	name          string
	maxRequests   uint32
	interval      time.Duration
	timeout       time.Duration
	readyToTrip   func(counts Counts) bool
	onStateChange func(name string, from State, to State)

	mu      sync.Mutex
	state   State
	counts  Counts
	expiry  time.Time
}

func New(st Settings) *CircuitBreaker {
	cb := &CircuitBreaker{
		name:          st.Name,
		maxRequests:   st.MaxRequests,
		interval:      st.Interval,
		timeout:       st.Timeout,
		readyToTrip:   st.ReadyToTrip,
		onStateChange: st.OnStateChange,
	}

	if cb.maxRequests == 0 {
		cb.maxRequests = 5
	}
	if cb.interval == 0 {
		cb.interval = time.Duration(0)
	}
	if cb.timeout == 0 {
		cb.timeout = 30 * time.Second
	}
	if cb.readyToTrip == nil {
		cb.readyToTrip = defaultReadyToTrip(st.FailureRatio)
	}

	cb.setState(StateClosed, time.Now())
	return cb
}

func defaultReadyToTrip(ratio float64) func(counts Counts) bool {
	if ratio <= 0 {
		ratio = 0.6
	}
	return func(counts Counts) bool {
		if counts.Requests < 20 {
			return false
		}
		failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
		return failureRatio >= ratio
	}
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
	generation, err := cb.beforeRequest()
	if err != nil {
		return err
	}

	defer func() {
		if r := recover(); r != nil {
			cb.afterRequest(generation, false)
			panic(r)
		}
	}()

	err = fn()
	cb.afterRequest(generation, err == nil)
	return err
}

func (cb *CircuitBreaker) beforeRequest() (uint64, error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	state, generation := cb.currentState(now)

	if state == StateOpen {
		return generation, ErrCircuitOpen
	}

	if state == StateHalfOpen && cb.counts.Requests >= int64(cb.maxRequests) {
		return generation, ErrCircuitOpen
	}

	cb.counts.Requests++
	return generation, nil
}

func (cb *CircuitBreaker) afterRequest(beforeGeneration uint64, success bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	state, generation := cb.currentState(now)

	if generation != beforeGeneration {
		return
	}

	if success {
		cb.onSuccess(state)
	} else {
		cb.onFailure(state)
	}
}

func (cb *CircuitBreaker) onSuccess(state State) {
	cb.counts.onSuccess()

	switch state {
	case StateClosed:
	case StateHalfOpen:
		if cb.counts.ConsecutiveSuccesses >= int64(cb.maxRequests) {
			cb.setState(StateClosed, time.Now())
		}
	}
}

func (cb *CircuitBreaker) onFailure(state State) {
	cb.counts.onFailure()

	switch state {
	case StateClosed:
		if cb.readyToTrip(cb.counts) {
			cb.setState(StateOpen, time.Now())
		}
	case StateHalfOpen:
		cb.setState(StateOpen, time.Now())
	}
}

func (cb *CircuitBreaker) currentState(now time.Time) (State, uint64) {
	switch cb.state {
	case StateClosed:
		if !cb.expiry.IsZero() && cb.expiry.Before(now) {
			cb.counts.clear()
			cb.expiry = now.Add(cb.interval)
		}
	case StateOpen:
		if cb.expiry.Before(now) {
			cb.setState(StateHalfOpen, now)
		}
	}
	return cb.state, 0
}

func (cb *CircuitBreaker) setState(state State, now time.Time) {
	if cb.state == state {
		return
	}

	prev := cb.state
	cb.state = state
	cb.counts.clear()

	switch state {
	case StateClosed:
		cb.expiry = now.Add(cb.interval)
	case StateOpen:
		cb.expiry = now.Add(cb.timeout)
	case StateHalfOpen:
		cb.expiry = time.Time{}
	}

	if cb.onStateChange != nil {
		cb.onStateChange(cb.name, prev, state)
	}
}

func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	state, _ := cb.currentState(now)
	return state
}
```

#### 4.2 使用示例

```go
package main

import (
	"fmt"
	"log"
	"time"

	"your_project/circuitbreaker"
)

func main() {
	cb := circuitbreaker.New(circuitbreaker.Settings{
		Name:         "user-service",
		MaxRequests:  3,
		Interval:     10 * time.Second,
		Timeout:      30 * time.Second,
		FailureRatio: 0.6,
		OnStateChange: func(name string, from circuitbreaker.State, to circuitbreaker.State) {
			log.Printf("CircuitBreaker %s: %v → %v", name, from, to)
		},
	})

	for i := 0; i < 100; i++ {
		err := cb.Execute(func() error {
			return callUserService()
		})

		if err != nil {
			if errors.Is(err, circuitbreaker.ErrCircuitOpen) {
				fmt.Printf("Request %d: Circuit open, fast fail\n", i)
				time.Sleep(1 * time.Second)
				continue
			}
			fmt.Printf("Request %d: Service error: %v\n", i, err)
		} else {
			fmt.Printf("Request %d: Success\n", i)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func callUserService() error {
	return fmt.Errorf("service unavailable")
}
```

### 五、熔断器与降级策略的配合

熔断器打开后，需要有降级策略来处理被拒绝的请求：

```go
type ResilientService struct {
	cb       *CircuitBreaker
	fallback func() (interface{}, error)
}

func NewResilientService(cb *CircuitBreaker, fallback func() (interface{}, error)) *ResilientService {
	return &ResilientService{
		cb:       cb,
		fallback: fallback,
	}
}

func (s *ResilientService) Call(fn func() (interface{}, error)) (interface{}, error) {
	err := s.cb.Execute(func() error {
		result, err := fn()
		if err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		if errors.Is(err, ErrCircuitOpen) && s.fallback != nil {
			return s.fallback()
		}
		return nil, err
	}

	return fn()
}
```

**常见降级策略**：

| 策略 | 实现 | 适用场景 |
|------|------|----------|
| 返回默认值 | 返回静态数据 | 推荐系统、配置服务 |
| 返回缓存 | 从本地缓存读取 | 商品详情、用户信息 |
| 返回精简数据 | 返回核心字段 | 列表页、搜索结果 |
| 排队重试 | 放入延迟队列 | 订单创建、支付 |
| 直接报错 | 返回错误信息 | 核心链路、写操作 |

### 六、熔断器在微服务架构中的位置

```
客户端 → API网关 → [熔断器A] → 服务A → [熔断器B] → 服务B → [熔断器C] → 服务C
                   (网关级)          (服务级)          (服务级)
```

**多级熔断**：
1. **网关级熔断**：基于IP、租户、API的粗粒度熔断
2. **服务级熔断**：基于下游服务实例的细粒度熔断
3. **实例级熔断**：基于单个实例的健康状态熔断

### 七、熔断器与限流器的区别

| 维度 | 熔断器 | 限流器 |
|------|--------|--------|
| 目的 | 防止故障扩散 | 防止过载 |
| 触发条件 | 下游错误率超阈值 | 请求量超阈值 |
| 状态 | 有状态（Closed/Open/HalfOpen） | 无状态 |
| 作用方向 | 保护调用方 | 保护被调用方 |
| 恢复方式 | 自动探测恢复 | 持续限流 |

**最佳实践**：熔断器 + 限流器配合使用。限流器在入口处控制流量，熔断器在调用链中防止故障扩散。

### 八、行业实践

- **Netflix Hystrix**：Java生态最著名的熔断器，已停止维护
- **Resilience4j**：Hystrix的替代品，轻量级函数式设计
- **Sentinel**：阿里开源，支持熔断+限流+系统保护
- **GoBreaker**：Go生态最流行的熔断器库，Sony开源
- **Istio**：Service Mesh层面支持熔断，无需修改代码

### 九、总结

熔断器是微服务高可用架构的必备组件，核心价值在于：

1. **快速失败**：避免线程资源被慢请求耗尽
2. **自动恢复**：Half-Open机制自动探测下游恢复
3. **防止雪崩**：故障隔离在单个服务内，不向上传播

**关键设计原则**：
- 熔断阈值要合理，避免误触发（最小请求数 + 错误率双条件）
- 熔断恢复要渐进，Half-Open逐步放行
- 降级策略要业务相关，不同接口不同降级方案
- 监控告警要完善，熔断触发时及时通知
