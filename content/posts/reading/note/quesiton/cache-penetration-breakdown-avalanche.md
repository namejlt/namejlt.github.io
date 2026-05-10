---
title: "缓存穿透击穿雪崩如何解决"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["缓存", "Redis", "高并发"]
---

## 问题

在高并发系统中，缓存是保护数据库的核心手段。但缓存穿透、缓存击穿、缓存雪崩这三大问题可能导致缓存失效、数据库瞬间被打垮。这三种问题的本质区别是什么？各自的解决方案和Go语言实现是怎样的？

## 回答

缓存是高并发系统的第一道防线，但缓存并非万能。当缓存失效时，大量请求会直接打到数据库，可能导致数据库崩溃、系统雪崩。理解三种缓存问题的本质区别和解决方案，是设计高可用系统的必备能力。

### 一、三种问题的本质区别

```
缓存穿透: 请求的数据在缓存和数据库中都不存在
          → 每次请求都穿透缓存，直达数据库

缓存击穿: 热点Key过期瞬间，大量并发请求同时到达
          → 缓存未命中，大量请求同时查询数据库

缓存雪崩: 大量Key同时过期，或缓存服务宕机
          → 大面积缓存失效，数据库压力骤增
```

| 维度 | 穿透 | 击穿 | 雪崩 |
|------|------|------|------|
| 根因 | 数据不存在 | 热点Key过期 | 大量Key同时过期/宕机 |
| 请求特征 | 查询不存在的数据 | 查询同一热点数据 | 查询多种数据 |
| 影响范围 | 特定不存在的Key | 单个热点Key | 大量Key |
| 发生频率 | 持续性（恶意攻击） | 瞬时性（过期时刻） | 突发性 |

### 二、缓存穿透的解决方案

#### 方案1：布隆过滤器

布隆过滤器是一种空间效率极高的概率型数据结构，可以判断一个元素"一定不存在"或"可能存在"。

**原理**：使用位数组和多个哈希函数。插入元素时，将多个哈希函数计算的位置设为1。查询时，如果所有位置都为1，则"可能存在"；如果任一位置为0，则"一定不存在"。

```go
package bloom

import (
	"hash"
	"hash/fnv"
	"math"
)

type BloomFilter struct {
	bits     []uint64
	size     uint
	hashFns  []hash.Hash64
	k        uint
}

func NewBloomFilter(expectedItems uint, falsePositiveRate float64) *BloomFilter {
	size := optimalSize(expectedItems, falsePositiveRate)
	k := optimalHashCount(size, expectedItems)

	hashFns := make([]hash.Hash64, k)
	for i := range hashFns {
		hashFns[i] = fnv.New64a()
	}

	return &BloomFilter{
		bits:    make([]uint64, (size+63)/64),
		size:    size,
		hashFns: hashFns,
		k:       k,
	}
}

func optimalSize(n uint, p float64) uint {
	return uint(-float64(n) * math.Log(p) / (math.Ln2 * math.Ln2))
}

func optimalHashCount(m, n uint) uint {
	return uint(float64(m) / float64(n) * math.Ln2)
}

func (bf *BloomFilter) getHashes(data []byte) []uint {
	hashes := make([]uint, bf.k)
	for i, h := range bf.hashFns {
		h.Reset()
		h.Write(data)
		hashVal := h.Sum64()
		hashes[i] = uint(hashVal % uint64(bf.size))
	}
	return hashes
}

func (bf *BloomFilter) Add(data []byte) {
	for _, pos := range bf.getHashes(data) {
		wordIndex := pos / 64
		bitIndex := pos % 64
		bf.bits[wordIndex] |= 1 << bitIndex
	}
}

func (bf *BloomFilter) MightContain(data []byte) bool {
	for _, pos := range bf.getHashes(data) {
		wordIndex := pos / 64
		bitIndex := pos % 64
		if bf.bits[wordIndex]&(1<<bitIndex) == 0 {
			return false
		}
	}
	return true
}
```

**在缓存架构中的位置**：

```
请求 → 布隆过滤器 → 不存在 → 直接返回
              ↓ 存在
           查询缓存 → 命中 → 返回
              ↓ 未命中
           查询数据库 → 写入缓存 → 返回
```

**布隆过滤器的局限**：
- 存在误判率（可配置，通常1%以下）
- 不支持删除（可用Counting Bloom Filter解决）
- 需要预先加载数据，数据变更时需要重建

#### 方案2：缓存空值

当查询数据库发现数据不存在时，将空值写入缓存，设置较短的TTL：

```go
package cache

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8"
)

const emptyValue = "NULL"

type CacheWithNullValue struct {
	client      *redis.Client
	nullTTL     time.Duration
	defaultTTL  time.Duration
}

func NewCacheWithNullValue(client *redis.Client, defaultTTL, nullTTL time.Duration) *CacheWithNullValue {
	return &CacheWithNullValue{
		client:     client,
		nullTTL:    nullTTL,
		defaultTTL: defaultTTL,
	}
}

func (c *CacheWithNullValue) GetOrLoad(ctx context.Context, key string,
	loadFn func() (string, error)) (string, error) {

	val, err := c.client.Get(ctx, key).Result()
	if err == nil {
		if val == emptyValue {
			return "", nil
		}
		return val, nil
	}

	if err != redis.Nil {
		return "", err
	}

	result, err := loadFn()
	if err != nil {
		return "", err
	}

	if result == "" {
		c.client.Set(ctx, key, emptyValue, c.nullTTL)
		return "", nil
	}

	c.client.Set(ctx, key, result, c.defaultTTL)
	return result, nil
}
```

**注意事项**：
- 空值TTL要短（通常2~5分钟），避免占用过多缓存空间
- 需要区分"数据不存在"和"查询出错"
- 如果不存在的Key很多，会浪费大量缓存空间

### 三、缓存击穿的解决方案

#### 方案1：互斥锁（Mutex Lock）

只允许一个线程查询数据库并重建缓存，其他线程等待：

```go
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
)

var ErrLockFailed = errors.New("failed to acquire lock")

type CacheWithMutex struct {
	client     *redis.Client
	defaultTTL time.Duration
	lockTTL    time.Duration
}

func NewCacheWithMutex(client *redis.Client, defaultTTL, lockTTL time.Duration) *CacheWithMutex {
	return &CacheWithMutex{
		client:     client,
		defaultTTL: defaultTTL,
		lockTTL:    lockTTL,
	}
}

func (c *CacheWithMutex) GetOrLoad(ctx context.Context, key string,
	loadFn func() (string, error)) (string, error) {

	val, err := c.client.Get(ctx, key).Result()
	if err == nil {
		return val, nil
	}

	if err != redis.Nil {
		return "", err
	}

	lockKey := "lock:" + key
	lockValue := time.Now().UnixNano()

	ok, err := c.client.SetNX(ctx, lockKey, lockValue, c.lockTTL).Result()
	if err != nil {
		return "", err
	}

	if ok {
		result, err := loadFn()
		if err != nil {
			c.client.Del(ctx, lockKey)
			return "", err
		}

		c.client.Set(ctx, key, result, c.defaultTTL)
		c.client.Del(ctx, lockKey)
		return result, nil
	}

	for i := 0; i < 50; i++ {
		time.Sleep(50 * time.Millisecond)

		val, err = c.client.Get(ctx, key).Result()
		if err == nil {
			return val, nil
		}

		if err != redis.Nil {
			return "", err
		}
	}

	return "", errors.New("timeout waiting for cache rebuild")
}
```

**互斥锁的优缺点**：
- 优点：保证只有一个请求查询数据库，数据库压力最小
- 缺点：其他请求需要等待，吞吐量下降；存在死锁风险（锁TTL必须大于数据库查询时间）

#### 方案2：逻辑过期

不设置物理TTL，而是在数据中嵌入逻辑过期时间。发现逻辑过期后，异步更新缓存：

```go
package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-redis/redis/v8"
)

type CacheData struct {
	Data      json.RawMessage `json:"data"`
	ExpireAt  time.Time       `json:"expire_at"`
}

type CacheWithLogicalExpiry struct {
	client       *redis.Client
	logicalTTL   time.Duration
	refreshChan  chan string
}

func NewCacheWithLogicalExpiry(client *redis.Client, logicalTTL time.Duration) *CacheWithLogicalExpiry {
	c := &CacheWithLogicalExpiry{
		client:      client,
		logicalTTL:  logicalTTL,
		refreshChan: make(chan string, 1000),
	}
	go c.asyncRefresh()
	return c
}

func (c *CacheWithLogicalExpiry) GetOrLoad(ctx context.Context, key string,
	loadFn func() (interface{}, error)) (interface{}, error) {

	raw, err := c.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return c.loadAndSet(ctx, key, loadFn)
	}
	if err != nil {
		return nil, err
	}

	var cacheData CacheData
	if err := json.Unmarshal([]byte(raw), &cacheData); err != nil {
		return c.loadAndSet(ctx, key, loadFn)
	}

	if time.Now().After(cacheData.ExpireAt) {
		select {
		case c.refreshChan <- key:
		default:
		}
		return cacheData.Data, nil
	}

	return cacheData.Data, nil
}

func (c *CacheWithLogicalExpiry) loadAndSet(ctx context.Context, key string,
	loadFn func() (interface{}, error)) (interface{}, error) {

	data, err := loadFn()
	if err != nil {
		return nil, err
	}

	cacheData := CacheData{
		Data:     mustMarshal(data),
		ExpireAt: time.Now().Add(c.logicalTTL),
	}

	encoded, _ := json.Marshal(cacheData)
	c.client.Set(ctx, key, string(encoded), 0)

	return data, nil
}

func (c *CacheWithLogicalExpiry) asyncRefresh() {
	for key := range c.refreshChan {
		ctx := context.Background()
		raw, err := c.client.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var cacheData CacheData
		if err := json.Unmarshal([]byte(raw), &cacheData); err != nil {
			continue
		}

		if time.Now().Before(cacheData.ExpireAt) {
			continue
		}

		lockKey := "lock:" + key
		ok, _ := c.client.SetNX(ctx, lockKey, 1, 10*time.Second).Result()
		if !ok {
			continue
		}

		go func(k string) {
			defer c.client.Del(ctx, "lock:"+k)
			c.loadAndSet(ctx, k, nil)
		}(key)
	}
}

func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
```

**逻辑过期的优势**：用户永远不需要等待缓存重建，始终能获取到数据（虽然可能是旧数据）。适合对一致性要求不高但对可用性要求极高的场景，如商品详情页。

### 四、缓存雪崩的解决方案

#### 方案1：TTL随机化

给缓存的TTL添加随机偏移量，避免大量Key同时过期：

```go
package cache

import (
	"math/rand"
	"time"
)

func RandomizedTTL(baseTTL time.Duration) time.Duration {
	offset := time.Duration(rand.Int63n(int64(baseTTL) / 5))
	return baseTTL + offset
}
```

#### 方案2：多级缓存

```
请求 → 本地缓存(L1) → Redis缓存(L2) → 数据库
        (进程内)        (分布式)        (持久化)
        TTL短            TTL中          -
        容量小            容量大
```

```go
package cache

import (
	"context"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

type MultiLevelCache struct {
	local  *LocalCache
	remote *redis.Client
}

type LocalCache struct {
	mu    sync.RWMutex
	items map[string]*localItem
}

type localItem struct {
	value    string
	expireAt time.Time
}

func NewLocalCache() *LocalCache {
	lc := &LocalCache{
		items: make(map[string]*localItem),
	}
	go lc.cleanup()
	return lc
}

func (lc *LocalCache) Get(key string) (string, bool) {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	item, ok := lc.items[key]
	if !ok || time.Now().After(item.expireAt) {
		return "", false
	}
	return item.value, true
}

func (lc *LocalCache) Set(key, value string, ttl time.Duration) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	lc.items[key] = &localItem{
		value:    value,
		expireAt: time.Now().Add(ttl),
	}
}

func (lc *LocalCache) cleanup() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		lc.mu.Lock()
		now := time.Now()
		for k, v := range lc.items {
			if now.After(v.expireAt) {
				delete(lc.items, k)
			}
		}
		lc.mu.Unlock()
	}
}

func NewMultiLevelCache(redisClient *redis.Client) *MultiLevelCache {
	return &MultiLevelCache{
		local:  NewLocalCache(),
		remote: redisClient,
	}
}

func (mc *MultiLevelCache) GetOrLoad(ctx context.Context, key string,
	loadFn func() (string, error)) (string, error) {

	if val, ok := mc.local.Get(key); ok {
		return val, nil
	}

	val, err := mc.remote.Get(ctx, key).Result()
	if err == nil {
		mc.local.Set(key, val, 30*time.Second)
		return val, nil
	}

	result, err := loadFn()
	if err != nil {
		return "", err
	}

	mc.remote.Set(ctx, key, result, 30*time.Minute)
	mc.local.Set(key, result, 30*time.Second)

	return result, nil
}
```

#### 方案3：熔断降级

当数据库压力过大时，直接返回降级数据，保护数据库：

```go
func (mc *MultiLevelCache) GetWithCircuitBreaker(ctx context.Context, key string,
	loadFn func() (string, error), fallbackFn func() string) string {

	if val, ok := mc.local.Get(key); ok {
		return val
	}

	val, err := mc.remote.Get(ctx, key).Result()
	if err == nil {
		mc.local.Set(key, val, 30*time.Second)
		return val
	}

	if isCircuitBreakerOpen() {
		return fallbackFn()
	}

	result, err := loadFn()
	if err != nil {
		recordFailure()
		return fallbackFn()
	}

	recordSuccess()
	mc.remote.Set(ctx, key, result, 30*time.Minute)
	mc.local.Set(key, result, 30*time.Second)
	return result
}
```

### 五、方案选型总结

| 问题 | 推荐方案 | 适用场景 |
|------|----------|----------|
| 穿透 | 布隆过滤器 | 数据相对固定，可预加载 |
| 穿透 | 缓存空值 | 数据动态变化，不存在Key较少 |
| 击穿 | 互斥锁 | 对一致性要求高，可接受短暂等待 |
| 击穿 | 逻辑过期 | 对可用性要求高，可接受短暂不一致 |
| 雪崩 | TTL随机化 | 最简单，预防性措施 |
| 雪崩 | 多级缓存 | 高可用要求，可接受一定复杂度 |
| 雪崩 | 熔断降级 | 最后防线，保护数据库 |

**生产环境最佳实践**：TTL随机化（预防） + 多级缓存（核心） + 互斥锁（击穿保护） + 布隆过滤器（穿透保护） + 熔断降级（兜底），形成完整的缓存防护体系。
