---
title: "读写分离与分库分表如何设计"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["数据库", "分库分表", "golang"]
---

## 问题

单机数据库的容量和性能有上限，如何通过读写分离和分库分表来扩展？分片键如何选择？跨分片查询如何处理？如何用Go实现一个分库分表中间件？

## 回答

读写分离和分库分表是数据库扩展的两大核心手段。读写分离解决读性能瓶颈，分库分表解决存储和写性能瓶颈。两者通常结合使用，但引入了数据一致性、分布式事务和跨分片查询等新问题。

### 一、读写分离

#### 原理

```
写请求 → 主库（Master）
读请求 → 从库（Slave1, Slave2, Slave3）
主库 → 异步复制 → 从库
```

**核心问题：主从延迟**

写操作在主库完成后，需要时间同步到从库。如果立即读从库，可能读到旧数据。

**解决方案**：

| 方案 | 实现 | 优点 | 缺点 |
|------|------|------|------|
| 强制走主库 | 关键读操作直接查主库 | 简单可靠 | 增加主库压力 |
| 等待同步 | 写后等待从库同步完成 | 保证一致性 | 增加延迟 |
| 中间件路由 | 中间件判断GTID是否已同步 | 透明 | 实现复杂 |

```go
package dbproxy

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
)

type ReadWriteSplitter struct {
	master *sql.DB
	slaves []*sql.DB
	next   uint64
}

func NewReadWriteSplitter(master *sql.DB, slaves ...*sql.DB) *ReadWriteSplitter {
	return &ReadWriteSplitter{
		master: master,
		slaves: slaves,
	}
}

func (r *ReadWriteSplitter) Master() *sql.DB {
	return r.master
}

func (r *ReadWriteSplitter) Slave() *sql.DB {
	if len(r.slaves) == 0 {
		return r.master
	}
	n := atomic.AddUint64(&r.next, 1)
	return r.slaves[(n-1)%uint64(len(r.slaves))]
}

func (r *ReadWriteSplitter) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return r.Slave().QueryContext(ctx, query, args...)
}

func (r *ReadWriteSplitter) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return r.master.ExecContext(ctx, query, args...)
}

func (r *ReadWriteSplitter) QueryWithForceMaster(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return r.master.QueryContext(ctx, query, args...)
}
```

### 二、分库分表

#### 分片策略

| 策略 | 原理 | 优点 | 缺点 |
|------|------|------|------|
| 哈希分片 | hash(key) % N | 数据均匀 | 扩容需迁移 |
| 范围分片 | key在[a,b)范围内 | 扩容方便 | 热点问题 |
| 一致性哈希 | hash(key)映射到环 | 迁移量小 | 实现复杂 |
| 查表法 | 映射表记录分片位置 | 灵活 | 映射表是瓶颈 |

#### Go实现分库分表中间件

```go
package shard

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"sync"
)

type ShardRule struct {
	LogicTable   string
	ShardKey     string
	ShardType    string
	DBCount      int
	TableCount   int
}

type ShardRouter struct {
	rules  map[string]*ShardRule
	dbs    map[string]*sql.DB
	mu     sync.RWMutex
}

func NewShardRouter() *ShardRouter {
	return &ShardRouter{
		rules: make(map[string]*ShardRule),
		dbs:   make(map[string]*sql.DB),
	}
}

func (r *ShardRouter) AddRule(rule *ShardRule) {
	r.rules[rule.LogicTable] = rule
}

func (r *ShardRouter) AddDB(name string, db *sql.DB) {
	r.dbs[name] = db
}

type ShardResult struct {
	DBName    string
	TableName string
	DB        *sql.DB
}

func (r *ShardRouter) Route(logicTable string, shardValue interface{}) (*ShardResult, error) {
	rule, ok := r.rules[logicTable]
	if !ok {
		return nil, fmt.Errorf("no shard rule for table %s", logicTable)
	}

	var shardIdx int
	switch v := shardValue.(type) {
	case int:
		shardIdx = v
	case int64:
		shardIdx = int(v)
	case string:
		h := fnv.New32a()
		h.Write([]byte(v))
		shardIdx = int(h.Sum32())
	default:
		return nil, fmt.Errorf("unsupported shard key type: %T", shardValue)
	}

	dbIdx := shardIdx % rule.DBCount
	tableIdx := shardIdx % rule.TableCount

	dbName := fmt.Sprintf("shard_%d", dbIdx)
	tableName := fmt.Sprintf("%s_%04d", rule.LogicTable, tableIdx)

	db, ok := r.dbs[dbName]
	if !ok {
		return nil, fmt.Errorf("db %s not found", dbName)
	}

	return &ShardResult{
		DBName:    dbName,
		TableName: tableName,
		DB:        db,
	}, nil
}

func (r *ShardRouter) RouteAll(logicTable string) ([]*ShardResult, error) {
	rule, ok := r.rules[logicTable]
	if !ok {
		return nil, fmt.Errorf("no shard rule for table %s", logicTable)
	}

	var results []*ShardResult
	for dbIdx := 0; dbIdx < rule.DBCount; dbIdx++ {
		for tableIdx := 0; tableIdx < rule.TableCount; tableIdx++ {
			dbName := fmt.Sprintf("shard_%d", dbIdx)
			tableName := fmt.Sprintf("%s_%04d", rule.LogicTable, tableIdx)

			if db, ok := r.dbs[dbName]; ok {
				results = append(results, &ShardResult{
					DBName:    dbName,
					TableName: tableName,
					DB:        db,
				})
			}
		}
	}
	return results, nil
}
```

#### 分片SQL执行

```go
type ShardedDB struct {
	router *ShardRouter
}

func NewShardedDB(router *ShardRouter) *ShardedDB {
	return &ShardedDB{router: router}
}

func (s *ShardedDB) Insert(ctx context.Context, logicTable string, shardValue interface{}, query string, args ...interface{}) (sql.Result, error) {
	route, err := s.router.Route(logicTable, shardValue)
	if err != nil {
		return nil, err
	}

	actualQuery := rewriteTable(query, logicTable, route.TableName)
	return route.DB.ExecContext(ctx, actualQuery, args...)
}

func (s *ShardedDB) QueryOne(ctx context.Context, logicTable string, shardValue interface{}, query string, args ...interface{}) (*sql.Rows, error) {
	route, err := s.router.Route(logicTable, shardValue)
	if err != nil {
		return nil, err
	}

	actualQuery := rewriteTable(query, logicTable, route.TableName)
	return route.DB.QueryContext(ctx, actualQuery, args...)
}

func (s *ShardedDB) QueryAll(ctx context.Context, logicTable string, query string, args ...interface{}) ([]*sql.Rows, error) {
	routes, err := s.router.RouteAll(logicTable)
	if err != nil {
		return nil, err
	}

	var allRows []*sql.Rows
	for _, route := range routes {
		actualQuery := rewriteTable(query, logicTable, route.TableName)
		rows, err := route.DB.QueryContext(ctx, actualQuery, args...)
		if err != nil {
			continue
		}
		allRows = append(allRows, rows)
	}

	return allRows, nil
}

func rewriteTable(query, oldTable, newTable string) string {
	return strings.Replace(query, oldTable, newTable, -1)
}
```

### 三、跨分片查询

```go
type MergeSorter struct {
	lessFunc func(a, b interface{}) bool
}

func (m *MergeSorter) Merge(rowsList []interface{}, limit int) []interface{} {
	var all []interface{}
	for _, rows := range rowsList {
		all = append(all, rows)
	}

	sort.Slice(all, func(i, j int) bool {
		return m.lessFunc(all[i], all[j])
	})

	if len(all) > limit {
		all = all[:limit]
	}
	return all
}

type CountMerger struct{}

func (m *CountMerger) Merge(counts []int64) int64 {
	var total int64
	for _, c := range counts {
		total += c
	}
	return total
}
```

### 四、分片键选择原则

| 原则 | 说明 |
|------|------|
| 高离散度 | 分片键值分布均匀，避免热点 |
| 高频查询字段 | 用最常用的查询条件作为分片键 |
| 避免跨分片 | 尽量让查询落在一个分片内 |
| 不可变 | 分片键值不能更新，否则需要迁移 |

**常见分片键**：
- 用户表：user_id
- 订单表：user_id（按买家分）或 merchant_id（按卖家分）
- 日志表：create_time（范围分片）

### 五、总结

读写分离和分库分表是数据库扩展的核心手段：

1. **读写分离**：解决读性能瓶颈，注意主从延迟
2. **哈希分片**：数据均匀，适合等值查询
3. **范围分片**：扩容方便，适合范围查询
4. **分片键**：选择高离散度、高频查询的字段
5. **跨分片**：尽量避免，必要时用MergeSort合并

**行业实践**：ShardingSphere、Vitess、MyCat
