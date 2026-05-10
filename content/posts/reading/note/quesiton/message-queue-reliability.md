---
title: "消息队列如何保证消息不丢失"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["消息队列", "可靠性", "golang"]
---

## 问题

在使用消息队列（如Kafka、RabbitMQ）时，消息可能在生产端、MQ自身、消费端三个环节丢失。如何从架构层面和代码层面保证消息的可靠传递，实现"至少一次"（at-least-once）甚至"精确一次"（exactly-once）的语义？

## 回答

消息丢失是消息队列使用中最严重的问题之一。一条消息从生产到消费，要经过三个阶段，每个阶段都可能导致消息丢失。必须对每个环节都进行可靠性设计，才能保证端到端的消息不丢失。

### 一、消息丢失的三个环节

```
生产者 ──发送──→ 消息队列 ──投递──→ 消费者
  ↑                ↑                ↑
环节1:           环节2:           环节3:
发送失败         存储丢失         消费失败
```

#### 环节1：生产端丢失

**场景**：生产者发送消息后，网络异常导致消息未到达MQ；或MQ返回失败但生产者未处理。

**解决方案**：确认机制（ACK）+ 重试

#### 环节2：MQ端丢失

**场景**：MQ收到消息后暂存在内存中，还未写入磁盘就宕机了。

**解决方案**：持久化 + 同步刷盘

#### 环节3：消费端丢失

**场景**：消费者拿到消息后立即返回ACK，但处理过程中发生异常，消息已从MQ中删除。

**解决方案**：手动ACK（处理完成后再确认）

### 二、生产端可靠性保证

#### 2.1 确认机制与重试

```go
package mq

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/segmentio/kafka-go"
)

type ReliableProducer struct {
	writer *kafka.Writer
}

func NewReliableProducer(brokers []string, topic string) *ReliableProducer {
	return &ReliableProducer{
		writer: &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Topic:        topic,
			Balancer:     &kafka.LeastBytes{},
			RequiredAcks: kafka.RequireAll,
			MaxAttempts:  3,
			BatchTimeout: 10 * time.Millisecond,
		},
	}
}

func (p *ReliableProducer) Send(ctx context.Context, key, value []byte) error {
	return p.SendWithRetry(ctx, key, value, 3)
}

func (p *ReliableProducer) SendWithRetry(ctx context.Context, key, value []byte, maxRetries int) error {
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		err := p.writer.WriteMessages(ctx, kafka.Message{
			Key:   key,
			Value: value,
		})
		if err == nil {
			return nil
		}

		lastErr = err
		log.Printf("Send attempt %d failed: %v", i+1, err)

		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return err
		}

		backoff := time.Duration(1<<uint(i)) * 100 * time.Millisecond
		time.Sleep(backoff)
	}

	return lastErr
}

func (p *ReliableProducer) Close() error {
	return p.writer.Close()
}
```

**关键配置**：
- `RequiredAcks: kafka.RequireAll`：等待所有ISR副本确认，保证消息已持久化到多数节点
- `MaxAttempts: 3`：发送失败自动重试3次
- 指数退避重试：避免在MQ故障时雪崩

#### 2.2 本地消息表方案

当MQ不可用时，如何保证消息最终一定能发出？本地消息表是业界最常用的方案：

```go
package mq

import (
	"database/sql"
	"encoding/json"
	"time"
)

const (
	statusPending   = "PENDING"
	statusSent      = "SENT"
	statusFailed    = "FAILED"
)

type OutboxMessage struct {
	ID        int64
	Topic     string
	Key       string
	Payload   string
	Status    string
	RetryCount int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type OutboxRepository struct {
	db *sql.DB
}

func NewOutboxRepository(db *sql.DB) *OutboxRepository {
	return &OutboxRepository{db: db}
}

func (r *OutboxRepository) SaveWithTx(tx *sql.Tx, topic, key string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO outbox_messages (topic, key, payload, status, retry_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, ?, ?)
	`, topic, key, string(data), statusPending, time.Now(), time.Now())

	return err
}

func (r *OutboxRepository) GetPendingMessages(limit int) ([]OutboxMessage, error) {
	rows, err := r.db.Query(`
		SELECT id, topic, key, payload, status, retry_count, created_at, updated_at
		FROM outbox_messages
		WHERE status = ? AND retry_count < 5
		ORDER BY created_at ASC
		LIMIT ?
	`, statusPending, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []OutboxMessage
	for rows.Next() {
		var m OutboxMessage
		if err := rows.Scan(&m.ID, &m.Topic, &m.Key, &m.Payload, &m.Status, &m.RetryCount, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, nil
}

func (r *OutboxRepository) MarkSent(id int64) error {
	_, err := r.db.Exec(`UPDATE outbox_messages SET status = ?, updated_at = ? WHERE id = ?`, statusSent, time.Now(), id)
	return err
}

func (r *OutboxRepository) MarkFailed(id int64) error {
	_, err := r.db.Exec(`
		UPDATE outbox_messages SET status = ?, retry_count = retry_count + 1, updated_at = ? WHERE id = ?
	`, statusFailed, time.Now(), id)
	return err
}
```

**核心思想**：将业务操作和消息写入放在同一个数据库事务中，利用数据库事务的原子性保证"业务操作成功则消息一定存在"。后台定时任务扫描未发送的消息并投递到MQ。

```go
func (r *OutboxRepository) OutboxRelay(producer *ReliableProducer) {
	messages, err := r.GetPendingMessages(100)
	if err != nil {
		log.Printf("Failed to get pending messages: %v", err)
		return
	}

	for _, msg := range messages {
		err := producer.Send(context.Background(), []byte(msg.Key), []byte(msg.Payload))
		if err != nil {
			log.Printf("Failed to send message %d: %v", msg.ID, err)
			r.MarkFailed(msg.ID)
			continue
		}
		r.MarkSent(msg.ID)
	}
}
```

#### 2.3 事务消息

RocketMQ原生支持事务消息，Kafka也支持事务。以下是Kafka事务消息的Go实现：

```go
func SendTransactionalMessage(brokers []string, topic string, key, value []byte, businessFn func() error) error {
	conn, err := kafka.DialLeader(context.Background(), "tcp", brokers[0], topic, 0)
	if err != nil {
		return err
	}
	defer conn.Close()

	txID := "tx-" + time.Now().Format("20060102150405")

	producer := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireAll,
		TransactionalID: &txID,
	}
	defer producer.Close()

	err = businessFn()
	if err != nil {
		return err
	}

	err = producer.WriteMessages(context.Background(), kafka.Message{
		Key:   key,
		Value: value,
	})

	return err
}
```

### 三、MQ端可靠性保证

#### 3.1 Kafka的可靠性配置

| 配置项 | 推荐值 | 说明 |
|--------|--------|------|
| `replication.factor` | ≥ 3 | 每个Partition至少3个副本 |
| `min.insync.replicas` | 2 | 最少2个ISR副本确认写入成功 |
| `acks=all` | - | 等待所有ISR副本确认 |
| `unclean.leader.election.enable` | false | 禁止非ISR副本成为Leader |

**为什么`min.insync.replicas=2`？** 假设3个副本，1个宕机，剩余2个ISR副本仍可正常写入。如果设为1，当1个副本宕机后，只有1个副本确认即可写入成功，此时再宕机1个就会丢数据。

#### 3.2 RabbitMQ的可靠性配置

```go
func SetupReliableRabbitMQ(ch *amqp.Channel, queueName string) error {
	_, err := ch.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	err = ch.Confirm(false)
	if err != nil {
		return fmt.Errorf("failed to enable confirm mode: %w", err)
	}

	return nil
}
```

**关键配置**：
- Queue的`durable=true`：队列持久化
- Message的`deliveryMode=2`：消息持久化
- 开启Confirm模式：生产者可以收到MQ的确认

### 四、消费端可靠性保证

#### 4.1 手动ACK

```go
package mq

import (
	"context"
	"log"

	"github.com/segmentio/kafka-go"
)

type ReliableConsumer struct {
	reader *kafka.Reader
	handler func(key, value []byte) error
}

func NewReliableConsumer(brokers []string, topic, groupID string, handler func(key, value []byte) error) *ReliableConsumer {
	return &ReliableConsumer{
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers:  brokers,
			Topic:    topic,
			GroupID:  groupID,
			MinBytes: 10e3,
			MaxBytes: 10e6,
		}),
		handler: handler,
	}
}

func (c *ReliableConsumer) Start(ctx context.Context) error {
	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("Failed to read message: %v", err)
			continue
		}

		err = c.handler(msg.Key, msg.Value)
		if err != nil {
			log.Printf("Failed to process message (offset %d): %v", msg.Offset, err)
			continue
		}

		err = c.reader.CommitMessages(ctx, msg)
		if err != nil {
			log.Printf("Failed to commit message (offset %d): %v", msg.Offset, err)
		}
	}
}

func (c *ReliableConsumer) Close() error {
	return c.reader.Close()
}
```

**核心原则**：**先处理业务逻辑，再提交Offset**。如果处理失败，不提交Offset，消息会被重新投递。

#### 4.2 死信队列

消费失败的消息不能无限重试，需要进入死信队列（DLQ）供人工处理：

```go
type DLQConsumer struct {
	primaryReader *kafka.Reader
	dlqWriter     *kafka.Writer
	maxRetries    int
}

func NewDLQConsumer(brokers []string, topic, dlqTopic, groupID string, maxRetries int) *DLQConsumer {
	return &DLQConsumer{
		primaryReader: kafka.NewReader(kafka.ReaderConfig{
			Brokers: brokers,
			Topic:   topic,
			GroupID: groupID,
		}),
		dlqWriter: &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Topic:        dlqTopic,
			RequiredAcks: kafka.RequireAll,
		},
		maxRetries: maxRetries,
	}
}

func (c *DLQConsumer) ProcessMessage(ctx context.Context, msg kafka.Message, handler func([]byte, []byte) error) error {
	retryCount := getRetryCount(msg.Headers)

	err := handler(msg.Key, msg.Value)
	if err == nil {
		return nil
	}

	if retryCount >= c.maxRetries {
		return c.sendToDLQ(ctx, msg, err)
	}

	return nil
}

func (c *DLQConsumer) sendToDLQ(ctx context.Context, msg kafka.Message, processErr error) error {
	headers := append(msg.Headers,
		kafka.Header{Key: "x-dlq-reason", Value: []byte(processErr.Error())},
		kafka.Header{Key: "x-dlq-original-topic", Value: []byte(msg.Topic)},
		kafka.Header{Key: "x-dlq-original-partition", Value: []byte(fmt.Sprintf("%d", msg.Partition))},
		kafka.Header{Key: "x-dlq-original-offset", Value: []byte(fmt.Sprintf("%d", msg.Offset))},
	)

	return c.dlqWriter.WriteMessages(ctx, kafka.Message{
		Key:     msg.Key,
		Value:   msg.Value,
		Headers: headers,
	})
}

func getRetryCount(headers []kafka.Header) int {
	for _, h := range headers {
		if h.Key == "x-retry-count" {
			count, err := strconv.Atoi(string(h.Value))
			if err == nil {
				return count
			}
		}
	}
	return 0
}
```

### 五、Exactly-Once语义

At-least-once + 幂等消费 = Exactly-once，这是业界最常用的实现方式。

#### 幂等消费的实现

```go
type IdempotentConsumer struct {
	db      *sql.DB
	handler func([]byte, []byte) error
}

func NewIdempotentConsumer(db *sql.DB, handler func([]byte, []byte) error) *IdempotentConsumer {
	return &IdempotentConsumer{
		db:      db,
		handler: handler,
	}
}

func (c *IdempotentConsumer) Process(msgKey, msgValue []byte, msgID string) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRow(`SELECT 1 FROM consumed_messages WHERE msg_id = ?`, msgID).Scan(&exists)
	if err == nil {
		return nil
	}

	err = c.handler(msgKey, msgValue)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`INSERT INTO consumed_messages (msg_id, consumed_at) VALUES (?, ?)`, msgID, time.Now())
	if err != nil {
		return err
	}

	return tx.Commit()
}
```

### 六、端到端可靠性架构

```
生产者                    消息队列                    消费者
┌──────────────┐      ┌──────────────┐      ┌──────────────┐
│ 1.本地消息表  │      │ 1.多副本复制  │      │ 1.手动ACK    │
│ 2.确认+重试   │ ──→  │ 2.同步刷盘    │ ──→  │ 2.幂等消费   │
│ 3.事务消息    │      │ 3.ISR确认    │      │ 3.死信队列   │
└──────────────┘      └──────────────┘      └──────────────┘
```

### 七、总结

保证消息不丢失需要在三个环节都做好可靠性设计：

| 环节 | 核心方案 | 关键配置 |
|------|----------|----------|
| 生产端 | 本地消息表 + 确认重试 | acks=all, 重试+退避 |
| MQ端 | 多副本 + 持久化 | replication.factor≥3, min.insync.replicas=2 |
| 消费端 | 手动ACK + 幂等消费 | enable.auto.commit=false |

**行业实践**：
- **金融场景**：本地消息表 + 事务消息 + 幂等消费，保证Exactly-once
- **电商场景**：确认机制 + 手动ACK + 死信队列，保证At-least-once
- **日志场景**：At-most-once即可，优先保证吞吐量

消息不丢失的本质是用性能换可靠性。每个环节的可靠性保障都会增加延迟和开销，需要根据业务场景在可靠性和性能之间找到平衡点。
