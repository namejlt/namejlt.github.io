---
title: "延迟队列如何设计与实现"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["延迟队列", "golang", "Redis"]
---

## 问题

在业务系统中经常需要处理延迟任务，如订单30分钟未支付自动取消、消息延迟投递、定时提醒等。如何设计一个高可用、高性能的延迟队列？基于时间轮、Redis、消息队列的方案各有什么优劣？

## 回答

延迟队列是分布式系统中的基础组件，广泛应用于订单超时、延迟通知、定时任务等场景。不同的实现方案在性能、可靠性、精确度上有显著差异，需要根据业务特点选择。

### 一、延迟队列的核心需求

1. **精确延迟**：消息在指定的延迟时间后被消费，误差在可接受范围内
2. **可靠性**：消息不能丢失，即使服务重启也能恢复
3. **高性能**：支持大量延迟消息的插入和消费
4. **可扩展**：支持不同粒度的延迟时间（秒级、分钟级、小时级）

### 二、方案1：时间轮（Time Wheel）

时间轮是一种高效的定时器实现，适合单机、内存级别的延迟任务管理。

**原理**：一个环形数组，每个槽（Slot）代表一个时间间隔，指针以固定速率转动。任务放入对应的槽中，指针转到该槽时执行任务。

```
单层时间轮（12个槽，每槽1小时）：

     0
  11/ \1
  10   2
   9   3
  8\  /4
    5-6-7

当前指针指向3，一个5小时后执行的任务放入槽8
```

**Go实现**：

```go
package delayqueue

import (
	"container/list"
	"sync"
	"time"
)

type TimeWheel struct {
	tickDuration time.Duration
	ticks        int
	currentPos   int
	slots        []*list.List
	mu           sync.Mutex
	quit         chan struct{}
}

type task struct {
	delay    time.Duration
	circle   int
	callback func()
}

func NewTimeWheel(tickDuration time.Duration, ticks int) *TimeWheel {
	tw := &TimeWheel{
		tickDuration: tickDuration,
		ticks:        ticks,
		currentPos:   0,
		slots:        make([]*list.List, ticks),
		quit:         make(chan struct{}),
	}

	for i := range tw.slots {
		tw.slots[i] = list.New()
	}

	return tw
}

func (tw *TimeWheel) Start() {
	ticker := time.NewTicker(tw.tickDuration)
	go func() {
		for {
			select {
			case <-tw.quit:
				ticker.Stop()
				return
			case <-ticker.C:
				tw.tickHandler()
			}
		}
	}()
}

func (tw *TimeWheel) Stop() {
	close(tw.quit)
}

func (tw *TimeWheel) AddTask(delay time.Duration, callback func()) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	ticks := int(delay / tw.tickDuration)
	circle := ticks / tw.ticks
	slotPos := (tw.currentPos + ticks) % tw.ticks

	t := &task{
		delay:    delay,
		circle:   circle,
		callback: callback,
	}

	tw.slots[slotPos].PushBack(t)
}

func (tw *TimeWheel) tickHandler() {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	currentSlot := tw.slots[tw.currentPos]
	next := currentSlot.Front()

	for next != nil {
		t := next.Value.(*task)
		if t.circle > 0 {
			t.circle--
			next = next.Next()
			continue
		}

		go t.callback()

		removeNext := next.Next()
		currentSlot.Remove(next)
		next = removeNext
	}

	tw.currentPos = (tw.currentPos + 1) % tw.ticks
}
```

**时间轮的局限**：
- 纯内存实现，服务重启后任务丢失
- 单机方案，无法在分布式环境中使用
- 延迟精度受限于tickDuration

### 三、方案2：基于Redis的延迟队列

#### 2.1 ZSET方案

利用Redis的有序集合（ZSET），将延迟时间作为Score，定时轮询到期消息：

```go
package delayqueue

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
)

type RedisDelayQueue struct {
	client   *redis.Client
	queueKey string
}

func NewRedisDelayQueue(client *redis.Client, queueKey string) *RedisDelayQueue {
	return &RedisDelayQueue{
		client:   client,
		queueKey: queueKey,
	}
}

func (q *RedisDelayQueue) Push(ctx context.Context, id string, payload string, delay time.Duration) error {
	execAt := time.Now().Add(delay).UnixMilli()
	return q.client.ZAdd(ctx, q.queueKey, &redis.Z{
		Score:  float64(execAt),
		Member: id,
	}).Err()
}

func (q *RedisDelayQueue) PushWithPayload(ctx context.Context, id string, payload string, delay time.Duration) error {
	pipe := q.client.Pipeline()

	execAt := time.Now().Add(delay).UnixMilli()
	pipe.ZAdd(ctx, q.queueKey, &redis.Z{
		Score:  float64(execAt),
		Member: id,
	})

	payloadKey := fmt.Sprintf("%s:payload:%s", q.queueKey, id)
	pipe.Set(ctx, payloadKey, payload, delay+time.Hour)

	_, err := pipe.Exec(ctx)
	return err
}

func (q *RedisDelayQueue) Pop(ctx context.Context) (string, error) {
	now := time.Now().UnixMilli()

	members, err := q.client.ZRangeByScore(ctx, q.queueKey, &redis.ZRangeBy{
		Min: "0",
		Max: fmt.Sprintf("%d", now),
	}).Result()
	if err != nil {
		return "", err
	}

	if len(members) == 0 {
		return "", nil
	}

	for _, member := range members {
		removed, err := q.client.ZRem(ctx, q.queueKey, member).Result()
		if err != nil {
			continue
		}
		if removed > 0 {
			return member, nil
		}
	}

	return "", nil
}

func (q *RedisDelayQueue) Consume(ctx context.Context, handler func(id string) error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			id, err := q.Pop(ctx)
			if err != nil {
				log.Printf("Failed to pop message: %v", err)
				continue
			}
			if id == "" {
				continue
			}

			if err := handler(id); err != nil {
				log.Printf("Failed to handle message %s: %v", id, err)
			}
		}
	}
}
```

**ZSET方案的问题**：
- 轮询间隔影响延迟精度和Redis压力
- 多消费者竞争时`ZRangeByScore + ZRem`不是原子操作，需要Lua脚本

**Lua脚本保证原子性**：

```go
const popScript = `
local messages = redis.call('ZRANGEBYSCORE', KEYS[1], '0', ARGV[1], 'LIMIT', 0, 1)
if #messages > 0 then
    redis.call('ZREM', KEYS[1], messages[1])
    return messages[1]
end
return nil
`

func (q *RedisDelayQueue) PopAtomic(ctx context.Context) (string, error) {
	now := fmt.Sprintf("%d", time.Now().UnixMilli())
	result, err := q.client.Eval(ctx, popScript, []string{q.queueKey}, now).Result()
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	return result.(string), nil
}
```

#### 2.2 Redis Keyspace Notification方案

利用Redis的键空间通知，在Key过期时触发回调：

```go
package delayqueue

import (
	"context"
	"fmt"

	"github.com/go-redis/redis/v8"
)

type RedisExpiredDelayQueue struct {
	client  *redis.Client
	channel string
}

func NewRedisExpiredDelayQueue(client *redis.Client) *RedisExpiredDelayQueue {
	return &RedisExpiredDelayQueue{
		client:  client,
		channel: "__keyevent@0__:expired",
	}
}

func (q *RedisExpiredDelayQueue) Push(ctx context.Context, id string, payload string, delay string) error {
	key := fmt.Sprintf("delay:%s:%s", id, payload)
	return q.client.Set(ctx, key, payload, 0).Err()
}

func (q *RedisExpiredDelayQueue) Consume(ctx context.Context, handler func(id, payload string) error) {
	sub := q.client.Subscribe(ctx, q.channel)
	defer sub.Close()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			handler(msg.Channel, msg.Payload)
		}
	}
}
```

**Keyspace Notification的局限**：
- Redis的过期事件不保证可靠投递（如果Redis重启，未触发的过期事件会丢失）
- 不适合对可靠性要求高的场景
- 需要开启Redis的`notify-keyspace-events`配置

### 四、方案3：基于消息队列的延迟队列

#### Kafka延迟队列

Kafka本身不支持延迟消息，但可以通过多级Topic实现：

```go
package delayqueue

import (
	"context"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
)

type KafkaDelayQueue struct {
	brokers    []string
	delayTopic string
	targetTopic string
	groupID    string
}

func NewKafkaDelayQueue(brokers []string, delayTopic, targetTopic, groupID string) *KafkaDelayQueue {
	return &KafkaDelayQueue{
		brokers:     brokers,
		delayTopic:  delayTopic,
		targetTopic: targetTopic,
		groupID:     groupID,
	}
}

func (q *KafkaDelayQueue) SendDelayMessage(ctx context.Context, key, value []byte, delay time.Duration) error {
	writer := &kafka.Writer{
		Addr:         kafka.TCP(q.brokers...),
		Topic:        q.delayTopic,
		RequiredAcks: kafka.RequireAll,
	}
	defer writer.Close()

	return writer.WriteMessages(ctx, kafka.Message{
		Key:   key,
		Value: value,
		Headers: []kafka.Header{
			{Key: "target-topic", Value: []byte(q.targetTopic)},
			{Key: "deliver-at", Value: []byte(fmt.Sprintf("%d", time.Now().Add(delay).UnixMilli()))},
		},
	})
}

func (q *KafkaDelayQueue) StartDelayWorker(ctx context.Context) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  q.brokers,
		Topic:    q.delayTopic,
		GroupID:  q.groupID + "-delay",
		MinBytes: 10e3,
		MaxBytes: 10e6,
	})
	defer reader.Close()

	writer := &kafka.Writer{
		Addr:         kafka.TCP(q.brokers...),
		Topic:        q.targetTopic,
		RequiredAcks: kafka.RequireAll,
	}
	defer writer.Close()

	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			continue
		}

		var deliverAt int64
		for _, h := range msg.Headers {
			if h.Key == "deliver-at" {
				fmt.Sscanf(string(h.Value), "%d", &deliverAt)
			}
		}

		now := time.Now().UnixMilli()
		if deliverAt > now {
			time.Sleep(time.Duration(deliverAt-now) * time.Millisecond)
		}

		writer.WriteMessages(ctx, kafka.Message{
			Key:   msg.Key,
			Value: msg.Value,
		})

		reader.CommitMessages(ctx, msg)
	}
}
```

### 五、方案4：基于数据库的延迟队列

最简单但可靠的方案，适合对性能要求不高的场景：

```go
package delayqueue

import (
	"database/sql"
	"fmt"
	"time"
)

type DBDelayQueue struct {
	db *sql.DB
}

func NewDBDelayQueue(db *sql.DB) *DBDelayQueue {
	return &DBDelayQueue{db: db}
}

func (q *DBDelayQueue) Push(tx *sql.Tx, topic, payload string, delay time.Duration) error {
	deliverAt := time.Now().Add(delay)
	_, err := tx.Exec(`
		INSERT INTO delay_messages (topic, payload, deliver_at, status, created_at)
		VALUES (?, ?, ?, 'PENDING', ?)
	`, topic, payload, deliverAt, time.Now())
	return err
}

func (q *DBDelayQueue) Pop(topic string, limit int) ([]DelayMessage, error) {
	rows, err := q.db.Query(`
		SELECT id, topic, payload, deliver_at
		FROM delay_messages
		WHERE topic = ? AND status = 'PENDING' AND deliver_at <= ?
		ORDER BY deliver_at ASC
		LIMIT ?
		FOR UPDATE SKIP LOCKED
	`, topic, time.Now(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []DelayMessage
	for rows.Next() {
		var m DelayMessage
		if err := rows.Scan(&m.ID, &m.Topic, &m.Payload, &m.DeliverAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}

	if len(messages) == 0 {
		return nil, nil
	}

	tx, err := q.db.Begin()
	if err != nil {
		return nil, err
	}

	for _, m := range messages {
		_, err := tx.Exec(`UPDATE delay_messages SET status = 'PROCESSING', updated_at = ? WHERE id = ? AND status = 'PENDING'`, time.Now(), m.ID)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return messages, nil
}

func (q *DBDelayQueue) Ack(id int64) error {
	_, err := q.db.Exec(`UPDATE delay_messages SET status = 'COMPLETED', updated_at = ? WHERE id = ?`, time.Now(), id)
	return err
}

func (q *DBDelayQueue) Nack(id int64) error {
	_, err := q.db.Exec(`UPDATE delay_messages SET status = 'PENDING', updated_at = ? WHERE id = ?`, time.Now(), id)
	return err
}

type DelayMessage struct {
	ID        int64
	Topic     string
	Payload   string
	DeliverAt time.Time
}
```

**`FOR UPDATE SKIP LOCKED`**：这是MySQL 8.0+的特性，允许多个消费者并发消费，已锁定的行自动跳过，避免竞争等待。

### 六、方案对比

| 方案 | 延迟精度 | 可靠性 | 性能 | 实现复杂度 | 适用场景 |
|------|---------|--------|------|-----------|---------|
| 时间轮 | 毫秒级 | 低（内存） | 极高 | 低 | 单机定时任务 |
| Redis ZSET | 百毫秒级 | 中 | 高 | 中 | 中小规模延迟任务 |
| Redis过期通知 | 秒级 | 低 | 高 | 低 | 非关键通知 |
| Kafka | 秒级 | 高 | 极高 | 高 | 大规模消息系统 |
| 数据库 | 秒级 | 极高 | 低 | 低 | 低频关键任务 |

### 七、生产环境最佳实践

**推荐架构**：Redis ZSET + 数据库兜底

```
写入: Redis ZSET（快速写入）
      ↓
消费: 定时轮询ZSET，到期消息移入就绪队列
      ↓
处理: 消费者从就绪队列获取消息处理
      ↓
兜底: 数据库记录所有延迟消息，用于故障恢复
```

**关键设计要点**：
1. 使用Lua脚本保证ZSET操作的原子性
2. 消息处理完成后才从ZSET中删除，避免消息丢失
3. 设置消息重试次数，超过次数进入死信队列
4. 数据库记录消息状态，用于故障恢复和监控
5. 多消费者通过分布式锁或Lua脚本保证消息不被重复消费

### 八、总结

延迟队列的选择取决于业务场景的可靠性要求和性能需求：

- **单机场景**：时间轮，简单高效
- **中小规模**：Redis ZSET，兼顾性能和可靠性
- **大规模消息**：Kafka + 多级Topic，或RocketMQ原生延迟消息
- **关键业务**：数据库 + Redis双写，数据库作为持久化保障

**行业实践**：
- **RocketMQ**：原生支持18个延迟等级（1s~2h），开源版不支持任意延迟
- **RabbitMQ**：通过DLX（Dead Letter Exchange）+ TTL实现延迟队列
- **阿里云MQ**：支持任意延迟时间，底层基于时间轮 + RocksDB
