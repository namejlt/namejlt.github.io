---
title: "分布式锁如何设计与实现"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["分布式", "锁", "golang", "Redis"]
---

## 问题

在分布式系统中，多个进程或服务实例可能同时访问共享资源，如何设计一个可靠的分布式锁？基于Redis和etcd的分布式锁各有什么优劣？如何解决锁超时、锁续期、锁误删等关键问题？

## 回答

分布式锁是分布式系统中最常用的协调原语之一。与单机锁不同，分布式锁需要跨越网络和进程边界来保证互斥性，这引入了一系列单机环境下不存在的挑战。

### 一、分布式锁的核心需求

一个可靠的分布式锁必须满足以下条件：

1. **互斥性**：任意时刻，只有一个客户端能持有锁
2. **可重入性**：同一客户端可以多次获取同一把锁
3. **防死锁**：锁必须有超时机制，持有者崩溃后锁能自动释放
4. **高可用**：锁服务不能有单点故障
5. **高性能**：加锁/解锁延迟要低
6. **防误删**：客户端A的锁不能被客户端B释放

### 二、基于Redis的分布式锁

#### 2.1 基础实现

最简单的Redis分布式锁使用`SET key value NX EX timeout`命令：

```go
package distlock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
)

var ErrLockFailed = errors.New("failed to acquire lock")

type RedisLock struct {
	client   *redis.Client
	key      string
	value    string
	ttl      time.Duration
}

func NewRedisLock(client *redis.Client, key string, ttl time.Duration) *RedisLock {
	return &RedisLock{
		client: client,
		key:    key,
		value:  generateLockValue(),
		ttl:    ttl,
	}
}

func generateLockValue() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (l *RedisLock) TryLock(ctx context.Context) error {
	ok, err := l.client.SetNX(ctx, l.key, l.value, l.ttl).Result()
	if err != nil {
		return err
	}
	if !ok {
		return ErrLockFailed
	}
	return nil
}

func (l *RedisLock) Unlock(ctx context.Context) error {
	script := `
	if redis.call("GET", KEYS[1]) == ARGV[1] then
		return redis.call("DEL", KEYS[1])
	else
		return 0
	end
	`
	_, err := l.client.Eval(ctx, script, []string{l.key}, l.value).Result()
	return err
}
```

**为什么value要用随机值？** 这是防误删的关键。考虑以下场景：

```
时间线:
T1: 客户端A获取锁，value="aaa"，TTL=10s
T2: 客户端A执行业务逻辑（耗时超过10s）
T3: 锁自动过期
T4: 客户端B获取锁，value="bbb"
T5: 客户端A执行完毕，尝试删除锁
    → 如果不校验value，A会删掉B的锁！
```

使用Lua脚本保证"检查value + 删除"的原子性，避免了竞态条件。

#### 2.2 锁续期（Watchdog）

业务逻辑执行时间可能超过锁的TTL，需要自动续期机制：

```go
package distlock

import (
	"context"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

type RedisLockWithWatchdog struct {
	client    *redis.Client
	key       string
	value     string
	ttl       time.Duration
	cancelFn  context.CancelFunc
	mu        sync.Mutex
}

func NewRedisLockWithWatchdog(client *redis.Client, key string, ttl time.Duration) *RedisLockWithWatchdog {
	return &RedisLockWithWatchdog{
		client: client,
		key:    key,
		value:  generateLockValue(),
		ttl:    ttl,
	}
}

func (l *RedisLockWithWatchdog) TryLock(ctx context.Context) error {
	ok, err := l.client.SetNX(ctx, l.key, l.value, l.ttl).Result()
	if err != nil {
		return err
	}
	if !ok {
		return ErrLockFailed
	}

	renewCtx, cancel := context.WithCancel(context.Background())
	l.cancelFn = cancel

	go l.startWatchdog(renewCtx)

	return nil
}

func (l *RedisLockWithWatchdog) startWatchdog(ctx context.Context) {
	ticker := time.NewTicker(l.ttl / 3)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.renew(ctx)
		}
	}
}

func (l *RedisLockWithWatchdog) renew(ctx context.Context) {
	script := `
	if redis.call("GET", KEYS[1]) == ARGV[1] then
		return redis.call("PEXPIRE", KEYS[1], ARGV[2])
	else
		return 0
	end
	`
	_, err := l.client.Eval(ctx, script,
		[]string{l.key},
		l.value,
		l.ttl.Milliseconds(),
	).Result()
	if err != nil {
		return
	}
}

func (l *RedisLockWithWatchdog) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.cancelFn != nil {
		l.cancelFn()
	}

	script := `
	if redis.call("GET", KEYS[1]) == ARGV[1] then
		return redis.call("DEL", KEYS[1])
	else
		return 0
	end
	`
	_, err := l.client.Eval(ctx, script, []string{l.key}, l.value).Result()
	return err
}
```

**Watchdog机制**：Redisson（Java）的默认实现是TTL=30s，每10s续期一次（TTL的1/3）。如果持有锁的进程崩溃，Watchdog停止续期，锁会在TTL后自动释放。

#### 2.3 Redlock算法

单节点Redis存在单点故障问题。Redis作者Antirez提出了Redlock算法，使用N个（通常5个）独立的Redis实例：

**算法步骤**：
1. 记录当前时间T1
2. 依次向5个Redis实例请求加锁，使用相同的key和随机value，设置较小的超时时间（远小于锁的TTL）
3. 计算加锁成功所需的实例数（N/2 + 1 = 3）
4. 如果在至少3个实例上加锁成功，且总耗时（T2-T1）小于锁的TTL，则认为加锁成功
5. 加锁成功后，锁的实际有效时间 = TTL - (T2 - T1)
6. 如果加锁失败，向所有实例发送解锁请求

**Redlock的争议**：分布式系统专家Martin Kleppmann指出Redlock存在以下问题：
- 依赖系统时钟的准确性（时钟跳跃可能导致锁提前过期）
- 在网络分区或GC暂停期间可能违反互斥性
- 建议使用fencing token（递增令牌）来保证资源安全

### 三、基于etcd的分布式锁

etcd基于Raft协议，提供强一致性保证，天然适合实现分布式锁。

```go
package distlock

import (
	"context"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

type EtcdLock struct {
	client *clientv3.Client
	prefix string
}

func NewEtcdLock(endpoints []string, prefix string) (*EtcdLock, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return &EtcdLock{client: cli, prefix: prefix}, nil
}

func (l *EtcdLock) Lock(ctx context.Context, lockKey string, timeout time.Duration) (*concurrency.Mutex, func(), error) {
	session, err := concurrency.NewSession(l.client,
		concurrency.WithTTL(int(timeout.Seconds())),
		concurrency.WithContext(ctx),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create session: %w", err)
	}

	mutex := concurrency.NewMutex(session, l.prefix+lockKey)

	if err := mutex.Lock(ctx); err != nil {
		session.Close()
		return nil, nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	unlock := func() {
		mutex.Unlock(context.Background())
		session.Close()
	}

	return mutex, unlock, nil
}
```

**etcd锁的工作原理**：

etcd的`concurrency.Mutex`基于租约（Lease）和前缀目录实现：

1. 创建一个带TTL的Session（底层是Lease）
2. 在指定前缀下创建一个有序的Key（如`/lock/order/xxx-0001`）
3. 查询前缀下所有Key，判断自己的Key是否是最小的
4. 如果是最小的，获取锁成功
5. 如果不是，监听前一个Key的删除事件，等待前一个Key被删除后重新判断
6. Session过期（Lease TTL到期）时，Key自动删除，锁自动释放

**etcd锁的优势**：
- **强一致性**：基于Raft协议，不存在Redlock的时钟问题
- **公平锁**：按请求顺序排队，先到先得
- **自动续期**：Session的Lease会自动续期
- **可监听**：支持Watch机制，实时感知锁状态变化

### 四、Redis vs etcd对比

| 维度 | Redis | etcd |
|------|-------|------|
| 一致性模型 | 最终一致 | 强一致 |
| 性能 | 极高（10万+QPS） | 较高（万级QPS） |
| 锁类型 | 非公平锁 | 公平锁 |
| 时钟依赖 | 依赖系统时钟 | 不依赖 |
| 可重入 | 需自行实现 | 需自行实现 |
| 运维复杂度 | 低 | 中 |
| 适用场景 | 高性能、可容忍极端情况下的互斥失效 | 强一致性要求、金融场景 |

### 五、可重入锁的实现

无论是Redis还是etcd，原生的分布式锁都不支持可重入。需要自行实现：

```go
package distlock

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-redis/redis/v8"
)

type ReentrantRedisLock struct {
	client   *redis.Client
	key      string
	value    string
	ttl      time.Duration
	count    int
	mu       sync.Mutex
	cancelFn context.CancelFunc
}

func NewReentrantRedisLock(client *redis.Client, key string, ttl time.Duration) *ReentrantRedisLock {
	return &ReentrantRedisLock{
		client: client,
		key:    key,
		value:  generateLockValue(),
		ttl:    ttl,
	}
}

func (l *ReentrantRedisLock) Lock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.count > 0 {
		l.count++
		return nil
	}

	script := `
	if redis.call("GET", KEYS[1]) == ARGV[1] then
		redis.call("PEXPIRE", KEYS[1], ARGV[2])
		return 1
	end
	return redis.call("SET", KEYS[1], ARGV[1], "NX", "PX", ARGV[2]) and 1 or 0
	`

	ok, err := l.client.Eval(ctx, script,
		[]string{l.key},
		l.value,
		l.ttl.Milliseconds(),
	).Int64()
	if err != nil {
		return err
	}
	if ok == 0 {
		return ErrLockFailed
	}

	l.count = 1
	renewCtx, cancel := context.WithCancel(context.Background())
	l.cancelFn = cancel
	go l.startWatchdog(renewCtx)

	return nil
}

func (l *ReentrantRedisLock) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.count <= 0 {
		return fmt.Errorf("lock not held")
	}

	l.count--

	if l.count > 0 {
		return nil
	}

	if l.cancelFn != nil {
		l.cancelFn()
	}

	script := `
	if redis.call("GET", KEYS[1]) == ARGV[1] then
		return redis.call("DEL", KEYS[1])
	else
		return 0
	end
	`
	_, err := l.client.Eval(ctx, script, []string{l.key}, l.value).Result()
	return err
}

func (l *ReentrantRedisLock) startWatchdog(ctx context.Context) {
	ticker := l.ttl / 3
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(ticker):
			script := `
			if redis.call("GET", KEYS[1]) == ARGV[1] then
				return redis.call("PEXPIRE", KEYS[1], ARGV[2])
			else
				return 0
			end
			`
			l.client.Eval(ctx, script,
				[]string{l.key},
				l.value,
				l.ttl.Milliseconds(),
			)
		}
	}
}
```

### 六、Fencing Token方案

Martin Kleppmann提出的Fencing Token方案是分布式锁安全性的终极保障：

```go
type FencingTokenProvider struct {
	counter int64
	mu      sync.Mutex
}

func (p *FencingTokenProvider) Next() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.counter++
	return p.counter
}

type ProtectedResource struct {
	lastSeenToken int64
	mu            sync.Mutex
}

func (r *ProtectedResource) Execute(token int64, fn func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if token <= r.lastSeenToken {
		return fmt.Errorf("stale token %d, last seen %d", token, r.lastSeenToken)
	}

	r.lastSeenToken = token
	return fn()
}
```

**原理**：每次获取锁时分配一个递增的Token，资源端记录已处理的最大Token，拒绝Token值更小的请求。即使锁机制失效，也能通过Token保证资源安全。

### 七、总结

分布式锁的选择取决于业务场景：

- **大多数互联网场景**：单节点Redis锁 + Watchdog续期 + Lua脚本防误删，足够可靠
- **强一致性要求**：etcd锁，基于Raft协议，不依赖时钟
- **极端可靠性要求**：Fencing Token + 分布式锁双重保障

**最佳实践**：
1. 永远使用Lua脚本保证Redis操作的原子性
2. 锁的value必须是唯一标识，防止误删
3. 必须实现Watchdog续期机制
4. 业务逻辑要保证幂等，因为分布式锁不能100%保证互斥
5. 锁的粒度要尽可能小，持有时间要尽可能短
