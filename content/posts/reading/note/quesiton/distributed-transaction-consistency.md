---
title: "分布式事务如何保证一致性"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["分布式事务", "一致性", "golang"]
---

## 问题

在微服务架构中，一个业务操作往往涉及多个服务的数据变更，如何保证这些变更要么全部成功、要么全部回滚？2PC、TCC、Saga、本地消息表这些分布式事务方案各自的原理、优劣和适用场景是什么？

## 回答

分布式事务是微服务架构中最复杂的问题之一。CAP定理告诉我们，在网络分区发生时，一致性和可用性不可兼得。分布式事务方案本质上是在一致性、可用性和性能之间做权衡。

### 一、分布式事务的理论基础

#### CAP定理

- **C（Consistency）**：一致性，所有节点看到相同的数据
- **A（Availability）**：可用性，每个请求都能得到响应
- **P（Partition tolerance）**：分区容忍性，网络分区时系统仍能运行

在分布式系统中，网络分区不可避免（P必须保证），因此只能在C和A之间选择。

#### BASE理论

BASE是对CAP中AP方向的延伸，是大多数互联网系统的选择：

- **BA（Basically Available）**：基本可用，允许响应时间增加或功能降级
- **S（Soft State）**：软状态，允许中间状态存在
- **E（Eventually Consistent）**：最终一致性，经过一段时间后达到一致

### 二、2PC（两阶段提交）

#### 原理

2PC是最经典的分布式事务协议，引入协调者（Coordinator）角色：

```
阶段1（Prepare）：
  协调者 → 参与者1: "准备好提交了吗？"
  协调者 → 参与者2: "准备好提交了吗？"
  参与者1 → 协调者: "准备好了" / "不行"
  参与者2 → 协调者: "准备好了" / "不行"

阶段2（Commit/Rollback）：
  如果所有参与者都准备好了 → 协调者发送Commit
  如果任一参与者没准备好   → 协调者发送Rollback
```

**Go实现**：

```go
package dtx

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrPrepareFailed = errors.New("prepare failed")
	ErrCommitFailed  = errors.New("commit failed")
)

type Participant interface {
	Prepare(ctx context.Context) error
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type Coordinator struct {
	participants []Participant
}

func NewCoordinator(participants ...Participant) *Coordinator {
	return &Coordinator{participants: participants}
}

func (c *Coordinator) Execute(ctx context.Context) error {
	prepared := make([]bool, len(c.participants))

	for i, p := range c.participants {
		if err := p.Prepare(ctx); err != nil {
			c.rollback(ctx, prepared)
			return fmt.Errorf("%w: participant %d: %v", ErrPrepareFailed, i, err)
		}
		prepared[i] = true
	}

	var commitErr error
	for i, p := range c.participants {
		if err := p.Commit(ctx); err != nil {
			commitErr = fmt.Errorf("%w: participant %d: %v", ErrCommitFailed, i, err)
			break
		}
		prepared[i] = false
	}

	return commitErr
}

func (c *Coordinator) rollback(ctx context.Context, prepared []bool) {
	var wg sync.WaitGroup
	for i, p := range c.participants {
		if prepared[i] {
			wg.Add(1)
			go func(idx int, participant Participant) {
				defer wg.Done()
				if err := participant.Rollback(ctx); err != nil {
					fmt.Printf("rollback failed for participant %d: %v\n", idx, err)
				}
			}(i, p)
		}
	}
	wg.Wait()
}
```

**2PC的致命缺陷**：

1. **同步阻塞**：Prepare阶段参与者会锁定资源，直到Commit/Rollback，期间其他事务无法访问
2. **单点故障**：协调者宕机后，参与者将永远阻塞
3. **数据不一致**：Commit阶段如果部分参与者收到Commit、部分没收到，数据将不一致
4. **太保守**：任一参与者Prepare失败就回滚，没有超时机制

### 三、TCC（Try-Confirm-Cancel）

#### 原理

TCC是业务层面的2PC，将每个操作分为三个阶段：

- **Try**：预留资源（冻结库存、冻结余额）
- **Confirm**：确认操作（扣减冻结的库存、扣减冻结的余额）
- **Cancel**：取消操作（释放冻结的库存、释放冻结的余额）

```
场景：电商下单（扣库存 + 扣余额）

Try阶段：
  库存服务: 冻结1件商品（可用库存-1，冻结库存+1）
  账户服务: 冻结100元（可用余额-100，冻结金额+100）

Confirm阶段：
  库存服务: 扣减冻结库存（冻结库存-1）
  账户服务: 扣减冻结金额（冻结金额-100）

Cancel阶段：
  库存服务: 释放冻结库存（冻结库存-1，可用库存+1）
  账户服务: 释放冻结金额（冻结金额-100，可用余额+100）
```

**Go实现**：

```go
package dtx

import (
	"context"
	"fmt"
	"sync"
)

type TCCParticipant interface {
	Try(ctx context.Context) error
	Confirm(ctx context.Context) error
	Cancel(ctx context.Context) error
}

type TCCCoordinator struct {
	participants []TCCParticipant
}

func NewTCCCoordinator(participants ...TCCParticipant) *TCCCoordinator {
	return &TCCCoordinator{participants: participants}
}

func (c *TCCCoordinator) Execute(ctx context.Context) error {
	tried := make([]bool, len(c.participants))

	for i, p := range c.participants {
		if err := p.Try(ctx); err != nil {
			c.cancel(ctx, tried)
			return fmt.Errorf("TCC Try failed at participant %d: %w", i, err)
		}
		tried[i] = true
	}

	var confirmErr error
	confirmed := make([]bool, len(c.participants))

	for i, p := range c.participants {
		if err := p.Confirm(ctx); err != nil {
			confirmErr = fmt.Errorf("TCC Confirm failed at participant %d: %w", i, err)
			break
		}
		confirmed[i] = true
	}

	if confirmErr != nil {
		for i, p := range c.participants {
			if tried[i] && !confirmed[i] {
				for retry := 0; retry < 3; retry++ {
					if err := p.Confirm(ctx); err == nil {
						break
					}
				}
			}
		}
	}

	return confirmErr
}

func (c *TCCCoordinator) cancel(ctx context.Context, tried []bool) {
	var wg sync.WaitGroup
	for i, p := range c.participants {
		if tried[i] {
			wg.Add(1)
			go func(idx int, participant TCCParticipant) {
				defer wg.Done()
				for retry := 0; retry < 3; retry++ {
					if err := participant.Cancel(ctx); err == nil {
						return
					}
				}
			}(i, p)
		}
	}
	wg.Wait()
}
```

**TCC的优缺点**：

- **优点**：锁粒度由业务控制（只冻结需要的资源），性能优于2PC；Confirm/Cancel幂等重试保证最终一致
- **缺点**：业务侵入性极强，每个服务都要实现Try/Confirm/Cancel三个接口；开发成本高

### 四、Saga模式

#### 原理

Saga将长事务拆分为多个本地短事务，每个本地事务有对应的补偿操作。如果某个步骤失败，则反向执行之前所有步骤的补偿操作。

**正向执行**：T1 → T2 → T3 → ... → Tn

**补偿回滚**：... → C3 → C2 → C1

```
场景：旅行预订（订机票 + 订酒店 + 租车）

正向: 订机票 → 订酒店 → 租车
反向: 取消机票 ← 取消酒店 ← 取消租车

如果租车失败:
  订机票(成功) → 订酒店(成功) → 租车(失败)
  → 取消酒店 → 取消机票
```

**Go实现**：

```go
package dtx

import (
	"context"
	"fmt"
	"log"
)

type SagaStep struct {
	Name       string
	Execute    func(ctx context.Context) error
	Compensate func(ctx context.Context) error
}

type Saga struct {
	steps []SagaStep
}

func NewSaga() *Saga {
	return &Saga{}
}

func (s *Saga) AddStep(name string, execute, compensate func(ctx context.Context) error) *Saga {
	s.steps = append(s.steps, SagaStep{
		Name:       name,
		Execute:    execute,
		Compensate: compensate,
	})
	return s
}

func (s *Saga) Execute(ctx context.Context) error {
	executedSteps := make([]int, 0, len(s.steps))

	for i, step := range s.steps {
		if err := step.Execute(ctx); err != nil {
			log.Printf("Saga step %q failed: %v, starting compensation", step.Name, err)
			s.compensate(ctx, executedSteps)
			return fmt.Errorf("saga failed at step %q: %w", step.Name, err)
		}
		executedSteps = append(executedSteps, i)
		log.Printf("Saga step %q completed successfully", step.Name)
	}

	return nil
}

func (s *Saga) compensate(ctx context.Context, executedSteps []int) {
	for i := len(executedSteps) - 1; i >= 0; i-- {
		stepIdx := executedSteps[i]
		step := s.steps[stepIdx]

		for retry := 0; retry < 3; retry++ {
			if err := step.Compensate(ctx); err != nil {
				log.Printf("Compensation for step %q failed (attempt %d): %v", step.Name, retry+1, err)
				continue
			}
			log.Printf("Compensation for step %q succeeded", step.Name)
			break
		}
	}
}
```

**使用示例**：

```go
func bookTrip(ctx context.Context) error {
	saga := NewSaga()

	saga.AddStep("book_flight",
		func(ctx context.Context) error {
			return bookFlight(ctx, "flight-123")
		},
		func(ctx context.Context) error {
			return cancelFlight(ctx, "flight-123")
		},
	)

	saga.AddStep("book_hotel",
		func(ctx context.Context) error {
			return bookHotel(ctx, "hotel-456")
		},
		func(ctx context.Context) error {
			return cancelHotel(ctx, "hotel-456")
		},
	)

	saga.AddStep("rent_car",
		func(ctx context.Context) error {
			return rentCar(ctx, "car-789")
		},
		func(ctx context.Context) error {
			return cancelCar(ctx, "car-789")
		},
	)

	return saga.Execute(ctx)
}
```

**Saga的两种编排模式**：

| 模式 | 实现 | 优点 | 缺点 |
|------|------|------|------|
| 编排式（Choreography） | 事件驱动，无中心协调 | 松耦合、简单 | 难以追踪、循环依赖 |
| 协调式（Orchestration） | 中心协调器控制流程 | 流程清晰、易追踪 | 协调器可能成为瓶颈 |

### 五、本地消息表 + 最终一致性

这是互联网公司最常用的方案，本质是"可靠消息 + 最终一致性"：

```go
package dtx

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type TransactionalOutbox struct {
	db *sql.DB
}

func NewTransactionalOutbox(db *sql.DB) *TransactionalOutbox {
	return &TransactionalOutbox{db: db}
}

func (t *TransactionalOutbox) ExecuteWithMessage(
	businessFn func(tx *sql.Tx) error,
	topic string,
	messageKey string,
	payload interface{},
) error {
	tx, err := t.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := businessFn(tx); err != nil {
		return fmt.Errorf("business operation failed: %w", err)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	_, err = tx.Exec(`
		INSERT INTO outbox (topic, msg_key, payload, status, created_at)
		VALUES (?, ?, ?, 'PENDING', ?)
	`, topic, messageKey, string(data), time.Now())
	if err != nil {
		return fmt.Errorf("failed to insert outbox message: %w", err)
	}

	return tx.Commit()
}
```

**核心优势**：业务操作和消息写入在同一个数据库事务中，利用数据库事务的原子性保证"业务成功则消息一定存在"。后台任务异步发送消息，实现最终一致性。

### 六、方案对比与选型

| 维度 | 2PC | TCC | Saga | 本地消息表 |
|------|-----|-----|------|-----------|
| 一致性 | 强一致 | 最终一致 | 最终一致 | 最终一致 |
| 性能 | 低（同步阻塞） | 中（资源冻结） | 高（无锁） | 高（异步） |
| 业务侵入 | 低 | 极高 | 中 | 低 |
| 实现复杂度 | 中 | 高 | 中 | 低 |
| 适用场景 | 传统数据库 | 金融核心 | 长流程业务 | 互联网业务 |

**选型决策**：

```
是否需要强一致性？
├── 是 → 2PC（仅适用于同构数据库）
└── 否 → 是否能接受高业务侵入？
    ├── 是 → TCC（金融核心场景）
    └── 否 → 是否涉及多个服务的长流程？
        ├── 是 → Saga（编排式或协调式）
        └── 否 → 本地消息表（推荐，最通用）
```

### 七、总结

分布式事务没有银弹。2PC虽然保证强一致，但性能和可用性太差；TCC性能好但开发成本极高；Saga适合长流程但补偿逻辑复杂；本地消息表最实用但只保证最终一致性。

**行业实践**：
- **支付宝**：TCC（蚂蚁金服DTCC框架）
- **阿里**：Seata（支持AT、TCC、Saga、XA四种模式）
- **美团**：本地消息表 + 消息队列
- **大多数互联网公司**：本地消息表 + 最终一致性

**核心原则**：尽量用最终一致性替代强一致性，用异步消息替代同步调用，用幂等消费替代事务回滚。只有在金融等对一致性有绝对要求的场景，才考虑TCC或2PC。
