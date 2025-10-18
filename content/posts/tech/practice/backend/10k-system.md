---
title: "高并发设计"
date: 2025-10-18T14:00:00+08:00
draft: false
toc: true
featured: false
categories: ["技术/实践/后端"]
tags: ["系统设计"]
---

## 缓存异步设计

面对“每秒1w请求”这样的高并发场景，我们的核心设计思想必须是：**异步、解耦、削峰**。

直接让1w个请求同时冲击MySQL是绝对不可行的，这会瞬间打垮数据库。MySQL的行锁、事务、连接数都无法承受这个量级。我们必须将“前端的瞬时高并发”转换为“后端的平稳低并发”。

-----

### 1\. 核心设计思想

1.  **异步解耦 (Asynchronous Decoupling):** 用户的“秒杀”请求（写操作）不应直接操作数据库。我们将使用**消息队列（Kafka）** 作为缓冲。API层只负责快速接收请求、在缓存中预扣库存，然后立即将“创建订单”任务丢入Kafka，并马上响应用户“处理中”。
2.  **内存预扣减 (In-Memory Pre-Deduction):** 真正的瓶颈在于库存（Stock）这个“热点数据”。我们将使用 **Redis** 来存储和操作库存。利用Redis的原子操作（如Lua脚本）来处理1w/s的库存扣减，这是Redis的强项（单机可达10w QPS）。
3.  **削峰填谷 (Peak Shaving):** 1w/s的请求被Kafka缓冲后，后端的消费者（Consumer）服务可以按照自己的节奏（例如 1k/s）平稳地从Kafka拉取消息，并**批量**写入MySQL。这样，数据库的压力就被极大降低了。
4.  **数据最终一致性 (Eventual Consistency):** 这种架构下，用户下单“成功”（Redis扣减成功）和数据库真正创建订单“成功”之间有几秒到几十秒的延迟。这是C端高并发场景下必须做出的取舍，我们保证的是**最终一致性**。

### 2\. 技术栈选型

  * **API服务 (Go):**
      * `Gin`: 高性能Web框架。
      * `go-redis/redis`: Redis客户端。
      * `segmentio/kafka-go`: Kafka客户端（纯Go实现，性能优秀）。
  * **消费服务 (Go):**
      * `segmentio/kafka-go`: Kafka消费者。
      * `database/sql` + `go-sql-driver/mysql`: 标准库MySQL驱动。
      * `sqlx` (可选): 方便数据库操作。
  * **中间件:**
      * **Redis:** 核心。用于原子扣库存、防刷（如Set记录用户ID）。
      * **Kafka:** 核心。用于请求缓冲和解耦。
      * **MySQL:** 最终数据存储（订单、库存、日志）。
  * **部署:**
      * **Kubernetes (K8s):** 用于API服务和消费服务的无状态横向扩缩容。
      * **Nginx/LB:** 接入层，负载均衡。

### 3\. 架构设计

**流程详解:**

1.  **数据预热 (Warm-up):** 秒杀活动开始前，运营人员配置好MySQL中的商品库存（如1000件）。一个**预热脚本**将库存数据从MySQL加载到Redis中。

      * `Redis Key: stock:product_id:123`
      * `Value: 1000`

2.  **API请求层 (Fast Path):**

      * a. 用户请求 (10k/s) 到达Go API集群。
      * b. **前置过滤:**
          * **活动校验:** 活动是否开始/结束？（可存Redis）
          * **用户防刷:** 同一用户是否重复请求？（用 `Redis SADD "user:activity:123" user_id`，返回0表示重复）。
      * c. **核心：原子扣库存 (Redis Lua):**
          * 执行一段Lua脚本，原子性地“查询并扣减”。为什么用Lua？因为 `DECR` + `GET` 是两次操作，非原子。我们需要一次原子操作完成“判断库存是否\>0，如果是则-1，否则返回失败”。
      * d. **投递消息 (Kafka):**
          * 如果Lua脚本返回成功（库存扣减OK），则立即构建一个订单消息（如 `{"user_id": 456, "product_id": 123, "request_id": "uuid-..."}`）。
          * 将消息**同步**（或配置为高可靠的异步）发送到Kafka的 `orders` Topic。
      * e. **响应用户:**
          * 只要Kafka接收成功，就立即返回HTTP 200/202给用户，内容为：“下单成功，正在处理中”。（此时MySQL还未有任何数据）。

3.  **消费处理层 (Slow Path):**

      * a. 另一个Go服务（Consumer集群）订阅 Kafka 的 `orders` Topic。
      * b. **批量拉取:** Consumer一次从Kafka拉取一批消息（例如100条）。
      * c. **批量处理:**
          * **聚合:** 将100条消息按 `product_id` 聚合。例如：`{"product_123": 50, "product_456": 30, ...}`。
          * **DB事务:** 启动一个MySQL事务。
          * **批量INSERT:** 使用 `INSERT INTO orders (...) VALUES (...), (...), ...` 一次性插入100条订单记录。
          * **批量UPDATE:** 循环聚合后的map，执行 `UPDATE products SET stock = stock - 50 WHERE product_id = 123 AND stock >= 50`。
          * **写入日志:** 批量 `INSERT` 到 `order_logs`。
      * d. **提交Offset:**
          * 如果DB事务**成功**，则向Kafka提交Offset，表示这批消息处理完毕。
          * 如果DB事务**失败**（例如数据库抖动），**不提交Offset**。Kafka会在SLA后自动重发这批消息，消费者必须保证**幂等性**。

### 4\. 幂等性设计

消费端必须幂等。如果Kafka重发消息，不能重复创建订单和扣减库存。

  * **方案:**
    1.  API层生成一个唯一的 `request_id`（如UUID）并放入Kafka消息。
    2.  在 `orders` 表中为 `request_id` 建立**唯一索引 (UNIQUE INDEX)**。
    3.  当Consumer批量 `INSERT` 时，如果遇到 `Duplicate entry` 错误，说明这是重复消息，**忽略该错误**（或仅记录日志），并视为成功，继续处理下一条。

### 5\. 核心代码逻辑 (Golang)

> **注意:** 以下是生产级的核心逻辑。省略了完整的`main`函数、配置加载（`viper`）、结构化日志（`zap`）和完整的HTTP错误处理，重点展示架构实现。

#### 目录结构 (示例)

```
/high-concurrency-service
  /api              // API 服务 (接收1w QPS)
    main.go
    handler.go
    redis.go
    kafka_producer.go
  /consumer         // 消费服务 (处理Kafka消息)
    main.go
    processor.go
  go.mod
```

-----

#### `api/redis.go` (Redis原子扣库存 - 核心中的核心)

```go
package main

import (
	"context"
	"fmt"
	"github.com/go-redis/redis/v8"
	"time"
)

var rdb *redis.Client

// 初始化Redis客户端 (在 main.go 中调用)
func InitRedis() {
	rdb = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // 应从配置读取
		PoolSize: 100,              // 根据压测调整
	})
}

// Lua脚本：原子地检查库存并扣减
// KEYS[1]: 库存Key (e.g., "stock:product:123")
// ARGV[1]: 扣减数量 (e.g., 1)
// 返回值：
// 1: 扣减成功
// 0: 库存不足
// -1: Key不存在 (活动未预热)
const luaDeductStock = `
local stock_key = KEYS[1]
local deduct_qty = tonumber(ARGV[1])

local current_stock = redis.call('GET', stock_key)
if not current_stock then
    return -1
end

if tonumber(current_stock) >= deduct_qty then
    redis.call('DECRBY', stock_key, deduct_qty)
    return 1
else
    return 0
end
`

var deductScript = redis.NewScript(luaDeductStock)

// AtomicDeductStock 调用Lua脚本原子扣库存
func AtomicDeductStock(ctx context.Context, productID string) (int64, error) {
	stockKey := fmt.Sprintf("stock:product:%s", productID)
	
	// SCRIPT LOAD 会缓存脚本并返回SHA，生产中应使用 EvalSha 提高性能
	// 为简化示例，这里使用 Eval
	result, err := deductScript.Run(ctx, rdb, []string{stockKey}, 1).Result()
	if err != nil {
		// 如果是 redis.Nil，也返回错误，因为脚本总应有返回值
		return -1, fmt.Errorf("failed to run lua script: %w", err)
	}

	resCode, ok := result.(int64)
	if !ok {
		return -1, fmt.Errorf("lua script returned unexpected type: %T", result)
	}
	
	return resCode, nil
}

// CheckAndSetDuplicateRequest 检查用户是否重复请求（防刷）
// activityID: 活动ID
// userID: 用户ID
// ttl: 过期时间
func CheckAndSetDuplicateRequest(ctx context.Context, activityID string, userID string, ttl time.Duration) (bool, error) {
	key := fmt.Sprintf("user:activity:%s", activityID)
	// SAdd 返回1表示新元素，0表示已存在
	added, err := rdb.SAdd(ctx, key, userID).Result()
	if err != nil {
		return false, err
	}
	
	// 如果是新用户，设置key的过期时间（例如活动结束后）
	if added == 1 {
		rdb.Expire(ctx, key, ttl)
	}

	return added == 0, nil // 0表示重复 (isDuplicate)
}

```

-----

#### `api/kafka_producer.go`

```go
package main

import (
	"context"
	"encoding/json"
	"github.com/segmentio/kafka-go"
	"time"
)

var kafkaWriter *kafka.Writer

// InitKafkaProducer 初始化Kafka生产者 (在 main.go 中调用)
func InitKafkaProducer() {
	// segmentio/kafka-go 推荐使用 Balancer=Hash 来确保相同 key (如 product_id) 进入同一分区
	kafkaWriter = &kafka.Writer{
		Addr:     kafka.TCP("localhost:9092"), // 应从配置读取
		Topic:    "orders_topic",
		Balancer: &kafka.Hash{},
		// 生产环境必须配置高可靠性
		RequiredAcks: kafka.RequireAll, // 保证Leader和Follower都写入
		MaxAttempts:  10,               // 重试
		WriteTimeout: 10 * time.Second,
	}
}

// OrderMessage 定义了写入Kafka的消息结构
type OrderMessage struct {
	RequestID string `json:"request_id"` // 用于幂等性
	UserID    string `json:"user_id"`
	ProductID string `json:"product_id"`
	Timestamp int64  `json:"timestamp"`
}


// SendOrderMessage 发送订单消息到Kafka
// 使用同步发送，确保消息进入Kafka后再响应用户
func SendOrderMessage(ctx context.Context, msg OrderMessage) error {
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal order message: %w", err)
	}

	// 使用 ProductID 作为Key，可以保证同一商品的所有订单消息
	// 进入同一个Kafka分区，这有助于消费者按顺序处理同一商品的库存
	err = kafkaWriter.WriteMessages(ctx,
		kafka.Message{
			Key:   []byte(msg.ProductID),
			Value: msgBytes,
		},
	)
	
	if err != nil {
		// 这里的错误非常严重，代表Redis已扣库存，但Kafka写入失败
		// 必须告警，并考虑后续补偿（例如将失败消息存入Redis list，由job重试）
		// log.Error("Critical: failed to write message to kafka", ...)
		return fmt.Errorf("failed to write message to kafka: %w", err)
	}
	return nil
}
```

-----

#### `api/handler.go` (Gin路由和处理器)

```go
package main

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"log" // 生产环境应替换为 zap/logrus
	"net/http"
	"time"
)

// (省略 main.go 中的 r.POST("/purchase", HandlePurchase) )

// HandlePurchase 是处理秒杀请求的核心处理器
func HandlePurchase(c *gin.Context) {
	// 1. 获取参数 (UserID应从JWT Token获取，这里简化)
	userID := c.Query("user_id")
	productID := c.Query("product_id")
	activityID := "activity_123" // 假设为某个活动

	if userID == "" || productID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid parameters"})
		return
	}
	
	ctx := c.Request.Context()

	// 2. 防刷：检查是否重复请求
	// 活动结束后自动过期
	isDuplicate, err := CheckAndSetDuplicateRequest(ctx, activityID, userID, 24*time.Hour)
	if err != nil {
		log.Printf("Error checking duplicate: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Service busy"})
		return
	}
	if isDuplicate {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Don't request repeatedly"})
		return
	}

	// 3. 核心：原子扣库存
	deductCode, err := AtomicDeductStock(ctx, productID)
	if err != nil {
		log.Printf("Error deducting stock: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Service busy"})
		// 【注意】这里失败了，需要把SADD的防刷记录回滚吗？
		// 业务决定。通常不回滚，防止用户利用系统错误刷单。
		return
	}

	switch deductCode {
	case 0:
		c.JSON(http.StatusOK, gin.H{"code": "SOLD_OUT", "message": "Sold out"})
		return
	case -1:
		c.JSON(http.StatusOK, gin.H{"code": "NOT_READY", "message": "Activity not started"})
		return
	case 1:
		// 扣减成功，继续
	}

	// 4. 扣减成功，生成唯一请求ID
	requestID := uuid.New().String()

	// 5. 构造消息并发送到Kafka
	msg := OrderMessage{
		RequestID: requestID,
		UserID:    userID,
		ProductID: productID,
		Timestamp: time.Now().Unix(),
	}

	if err := SendOrderMessage(ctx, msg); err != nil {
		// ！！！【高危风险点】！！！
		// Redis已扣库存，但Kafka写入失败！
		log.Printf("CRITICAL: Stock deducted but Kafka failed: %v", err)
		
		// 简单的补偿：尝试把Redis库存加回去
		// rdb.Incr(context.Background(), fmt.Sprintf("stock:product:%s", productID))
		// 但高并发下 Incr 也可能失败。
		
		// 最佳实践：将失败的msg存入Redis的 "dead-letter-list"，由后台job补偿。
		// 并返回用户失败。
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Order failed, please retry"})
		return
	}

	// 6. 响应用户
	c.JSON(http.StatusOK, gin.H{
		"code":      "PROCESSING",
		"message":   "Order processing, please wait for final result",
		"request_id": requestID,
	})
}
```

-----

#### `consumer/processor.go` (消费端批量处理 - 核心)

```go
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/segmentio/kafka-go"
	"log"
	"strings"
	"time"
)

// (省略 main.go 中的 DB和Kafka Reader初始化 )

// OrderMessage 必须和生产者侧的结构一致
type OrderMessage struct {
	RequestID string `json:"request_id"`
	UserID    string `json:"user_id"`
	ProductID string `json:"product_id"`
	Timestamp int64  `json:"timestamp"`
}

// ProcessBatchMessages 批量处理Kafka消息
func ProcessBatchMessages(ctx context.Context, db *sql.DB, messages []kafka.Message) error {
	
	// 1. 聚合：按ProductID统计要扣减的数量，并准备批量INSERT的参数
	productCounts := make(map[string]int)
	orderPlaceholders := make([]string, 0, len(messages))
	orderValues := make([]interface{}, 0, len(messages)*5)
	
	processedMessages := make([]OrderMessage, 0, len(messages))

	for _, msg := range messages {
		var orderMsg OrderMessage
		if err := json.Unmarshal(msg.Value, &orderMsg); err != nil {
			log.Printf("Failed to unmarshal msg: %v. Skipping.", err)
			continue // 跳过坏消息
		}
		
		processedMessages = append(processedMessages, orderMsg)
		productCounts[orderMsg.ProductID]++
		
		// 准备SQL批量INSERT
		orderPlaceholders = append(orderPlaceholders, "(?, ?, ?, ?, ?)")
		orderValues = append(orderValues, orderMsg.RequestID, orderMsg.UserID, orderMsg.ProductID, "CREATED", time.Unix(orderMsg.Timestamp, 0))
	}
	
	if len(processedMessages) == 0 {
		log.Println("No valid messages in batch.")
		return nil
	}
	
	// 2. 开启DB事务
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	// 确保事务回滚
	defer tx.Rollback() // 如果Commit成功，Rollback是空操作

	// 3. 批量 INSERT 订单
	// 必须使用 IGNORE 或 ON DUPLICATE KEY UPDATE 来处理幂等性
	// 这里使用 IGNORE，如果 request_id (唯一键) 重复，则忽略
	orderQuery := fmt.Sprintf(
		"INSERT IGNORE INTO orders (request_id, user_id, product_id, status, created_at) VALUES %s",
		strings.Join(orderPlaceholders, ","),
	)

	_, err = tx.ExecContext(ctx, orderQuery, orderValues...)
	if err != nil {
		return fmt.Errorf("failed to batch insert orders: %w", err)
	}

	// 4. (可选) 批量 INSERT 日志 (类似操作)
	// ...

	// 5. 批量 UPDATE 库存
	// 这是将Redis的预扣减 "落实" 到数据库
	updateStockQuery := "UPDATE products SET stock = stock - ? WHERE product_id = ? AND stock >= ?"
	
	for productID, count := range productCounts {
		res, err := tx.ExecContext(ctx, updateStockQuery, count, productID, count)
		if err != nil {
			return fmt.Errorf("failed to update stock for product %s: %w", productID, err)
		}
		
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected for product %s: %w", productID, err)
		}
		
		// 【重要】如果 affected == 0，说明数据库的真实库存不足！
		// 这种情况是 "超卖" (Redis预扣成功，但DB库存不足)。
		// 意味着预热的Redis库存 > DB实际库存。
		if affected == 0 {
			// 必须记录日志，并（业务上）标记这些订单为失败，后续退款或通知。
			log.Printf("CRITICAL OVERSOLD: Product %s, tried to deduct %d, but DB stock insufficient.", productID, count)
			// 此处可以选择回滚事务，让Kafka重试（但可能会卡住），
			// 或者（更好）继续Commit，但把这些超卖的订单状态更新为 "FAILED_OVERSOLD"
		}
	}

	// 6. 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit tx: %w", err)
	}
	
	log.Printf("Successfully processed %d messages.", len(messages))
	return nil
}
```

-----

#### `consumer/main.go` (消费端主循环)

```go
package main

import (
	"context"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/segmentio/kafka-go"
	"log"
	"time"
)

// (DB 和 Kafka Reader 的初始化... )

func main() {
	// 1. 初始化DB
	db, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:3306)/dbname") // 从配置读取
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(10)

	// 2. 初始化Kafka Reader (消费者组)
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  []string{"localhost:9092"},
		GroupID:  "order-consumer-group",
		Topic:    "orders_topic",
		MinBytes: 10e3, // 10KB
		MaxBytes: 10e6, // 10MB
		MaxWait:  1 * time.Second, // 最多等1秒，用于攒批
	})
	defer r.Close()

	log.Println("Consumer started...")
	
	// 3. 消费循环
	for {
		ctx := context.Background()
		var batch []kafka.Message

		// 攒批：在 MaxWait 时间内，最多拉取100条 (或 Min/MaxBytes)
		const batchSize = 100
		
		// 先读第一条 (会阻塞直到有消息或超时)
		msg, err := r.FetchMessage(ctx)
		if err != nil {
			log.Printf("Error fetching message: %v", err)
			continue
		}
		batch = append(batch, msg)

		// 尝试读取更多消息，凑齐一个批次
		for len(batch) < batchSize {
			// ReadMessage 不会阻塞，如果没消息会立即返回 EOF
			// 我们使用 FetchMessage + MaxWait=0 (或很小) 的方式来“探查”
			// 但更简单的方式是配置ReaderConfig的 MinBytes 和 MaxWait
			// 这里使用 ReaderConfig 默认的批处理行为
			
			// 上面的 MaxWait: 1 * time.Second 已经帮我们实现了批处理
			// FetchMessage 会阻塞直到 MinBytes / MaxWait 满足
			// 但 segmentio/kafka-go 的批处理API (ReadBatch) 更好用
			
			// 我们改用更简单的循环
			break // 依赖于 ReaderConfig 的 MaxWait 自动攒批
		}
		
		// FetchMessage 已经帮我们实现了基于 MaxWait 的批处理，
		// 但它一次只返回一条消息。
		// 为了实现真正的批量拉取和批量提交，我们重构循环：

		// 重构：
		// for {
		//   ctx := context.Background()
		//   msg, err := r.FetchMessage(ctx) // Fetch会阻塞，直到 MinBytes 或 MaxWait
		//   if err != nil { break }
		//   
		//   batch = append(batch, msg)
		//   
		//   if len(batch) >= batchSize { // 假设 batchSize=100
		//      err := ProcessBatchMessages(ctx, db, batch)
		//      if err == nil {
		// 			r.CommitMessages(ctx, batch...) // 批量提交
		//          batch = batch[:0] // 清空
		//      } else {
		//          // 错误处理，不Commit，Kafka会重发
		//      }
		//   }
		// }
		
		// 简化的逻辑（逐条获取，批量提交）：
		// (以上注释的攒批逻辑比较复杂，我们使用逐条Fetch+批量Commit的简化模式)
		
		// 我们采用官方推荐的 `ReadMessage` 循环
		// ReadMessage 会自动处理 Rebalance，比 FetchMessage 更安全
		
		/* --- 推荐的 Consumer 循环 --- */
		// for {
		//   ctx := context.Background()
		//	 msg, err := r.ReadMessage(ctx)
		//   if err != nil {
		//      log.Printf("Error reading message: %v", err)
		//      continue // 或 break
		//   }
		//   
		//   // 这里为了演示批量，强行攒批
		//   // 生产环境不推荐这么做，而应依赖 ReaderConfig 的 MinBytes/MaxWait
		//   // 或者使用 ReadBatch API (如果v0.5+支持)
		// }
		
		// 妥协：我们假设 `FetchMessage` + `MaxWait` 返回了一批数据
		// (实际上 segmentio/kafka-go 的 FetchMessage 只返回1条)
		
		// 最终：我们使用 ReadMessage，但模拟批量处理
		
		// ---- 生产可用的 Consumer 循环 (推荐) ----
		// 我们将在一个goroutine中读取，并发送到channel，
		// 另一个goroutine从channel批量读取。
		
		// (为保持示例简单，我们放弃复杂的攒批，改为逐条处理)
		// (但逐条处理DB压力大，不符合设计)
		
		// ---- 最终方案：使用定时 + 定量攒批 ----
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		
		messageBatch := make([]kafka.Message, 0, batchSize)
		
	ConsumerLoop:
		for {
			select {
			case <-ticker.C:
				// 时间到了，处理当前批次
				if len(messageBatch) > 0 {
					log.Printf("Ticker processing %d messages", len(messageBatch))
					if err := ProcessBatchMessages(ctx, db, messageBatch); err != nil {
						log.Printf("Failed to process batch (on ticker): %v", err)
						// 错误，不提交Offset，等待下次重试
					} else {
						// 成功，提交Offset
						if err := r.CommitMessages(ctx, messageBatch...); err != nil {
							log.Printf("Failed to commit messages: %v", err)
						}
						messageBatch = messageBatch[:0] // 清空
					}
				}
			default:
				// 时间未到，继续拉消息
				msg, err := r.FetchMessage(ctx) // FetchMessage会阻塞
				if err != nil {
					log.Printf("Error fetching message: %v", err)
					break ConsumerLoop
				}
				
				messageBatch = append(messageBatch, msg)
				
				if len(messageBatch) >= batchSize {
					// 数量到了，处理当前批次
					log.Printf("Batch size processing %d messages", len(messageBatch))
					if err := ProcessBatchMessages(ctx, db, messageBatch); err != nil {
						log.Printf("Failed to process batch (on size): %v", err)
						// 错误，不提交Offset
					} else {
						// 成功，提交Offset
						if err := r.CommitMessages(ctx, messageBatch...); err != nil {
							log.Printf("Failed to commit messages: %v", err)
						}
						messageBatch = messageBatch[:0] // 清空
					}
					ticker.Reset(500 * time.Millisecond) // 重置计时器
				}
			}
		}
	}
}
```

## 数据一致性问题

以上库存保存在缓存中，如果异常导致缓存丢失，缓存需要重建

缓存重建公式：`库存数据 = 原始库存 - 数据库中记录 - mq中记录`

这个公式在**理论上是完全正确的**，但在**工程实践上是绝对不可行的**。

  * **为什么不可行？**
    1.  **无法原子查询:** 你无法在一个原子操作中同时“查询DB”和“查询Kafka”。
    2.  **无法精确统计Kafka:** Kafka是一个流，你无法在O(1)时间内“查询”到“Topic里还有多少未消费的product\_id=123的消息”。你必须消费它们，但消费它们就会改变DB。
    3.  **高速竞态:** 在你查询DB和Kafka的毫秒间，1000个新请求又进入了Kafka，同时500个旧请求刚被Consumer写入DB。你拿到的DB和MQ的快照永远是“脏”的。

**结论：** 任何试图在“活动进行中”通过 `DB + MQ` 来重建Redis库存的尝试，都将导致数据严重错乱（大概率是超卖）。

-----

### 真正的问题：缓存失效的“惊群效应” (Thundering Herd)

我们真正要解决的不是“如何计算”，而是“当Redis Key丢失时，如何处理”。

假设 `stock:product:123` 意外丢失（Redis节点宕机、手滑`FLUSHALL`），此时10k/s的请求涌入：

1.  10,000个请求同时调用Lua脚本，都返回-1 (Key不存在)。
2.  这10,000个请求会全部涌向“缓存重建”逻辑。
3.  如果“缓存重建”逻辑是去查DB，这10,000个请求会瞬间打垮MySQL。

### 架构师的抉择：两种“灾难恢复”方案

在秒杀架构中，Redis中的库存**不是数据库的缓存（Cache），它就是“事实上的库存”（Ephemeral State）**。它是在活动期间的**唯一热点数据源**。

一旦这个数据源丢失，你必须在“**A. 容忍超卖（数据不一致）**”和“**B. 暂停活动（服务不可用）**”之间做出选择。

-----

### 方案一：高可用（HA）集群（99.99%的预防）

这是首选方案，我们应该尽一切努力**防止**Key丢失，而不是考虑它丢失后怎么办。

1.  **永不“主动”过期:** 秒杀库存Key**绝对不能设置TTL**（或设置一个远超活动时间的TTL，如24小时）。它应该在活动结束后，由清理脚本（在确认MQ全部消费完毕后）手动删除。
2.  **Redis高可用:** 必须使用 **Redis Sentinel (哨兵模式)** 或 **Redis Cluster (集群模式)**。
      * 在哨兵模式下，如果Master节点宕机，Sentinel会立即将一个Slave节点提升为Master。因为数据是实时复制的，`stock:product:123` 这个Key会**立即在新Master上可用**。
      * 对于API服务来说，这个切换过程是（几乎）透明的（`go-redis`客户端库支持哨兵模式），最多只会导致几百毫秒的请求失败，而不会导致Key的“丢失”。

**在99.99%的情况下，采用高可用Redis集群，这个Key永远不会丢失。**

-----

### 方案二：分布式锁 + DB回源（容忍超卖风险）

这是你问题中提到的“从DB获取”的实现。但如前所述，它**有超卖风险**。

假设发生了0.01%的灾难（比如整个Redis集群数据被误删），我们必须重建。

**思路：** 10,000个请求中，只允许“一个”请求去DB重建缓存，其他请求原地等待。

**实现：** 使用**分布式锁**（例如 `SETNX`）。

修改 `api/handler.go` 中的 `HandlePurchase` 逻辑：

```go
// ... (接之前的 HandlePurchase)

	// 3. 核心：原子扣库存
	deductCode, err := AtomicDeductStock(ctx, productID)
	if err != nil && err != redis.Nil { // 忽略 redis.Nil，下面单独处理
		log.Printf("Error deducting stock: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Service busy"})
		return
	}

	// ------------------------------------------------------------------
	// 【关键】处理缓存失效 (Thundering Herd)
	// ------------------------------------------------------------------
	if deductCode == -1 || err == redis.Nil { 
		// Key 不存在！启动缓存重建和防惊群逻辑
		
		lockKey := fmt.Sprintf("lock:product:%s", productID)
		// 尝试获取一个短期的锁，NX=Not Exists, EX=10s
		// 只有一个请求能成功
		isAcquired, lockErr := rdb.SetNX(ctx, lockKey, "reloading", 10*time.Second).Result()
		
		if lockErr != nil {
			log.Printf("Error acquiring reload lock: %v", lockErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Service busy, try again"})
			return
		}

		if isAcquired {
			// (1) 我是“天选之子”，我去DB重建缓存
			log.Printf("Cache miss. Acquired lock. Reloading stock from DB for %s", productID)
			
			// 【警告】这里的DB Stock是“不准确”的，它未包含MQ中的数据
			// dbStock := getStockFromDB(productID) // 伪代码：SELECT stock FROM products ...
			// rdb.Set(ctx, fmt.Sprintf("stock:product:%s", productID), dbStock, 24*time.Hour)
			
			// 【严重警告】加载DB库存会导致超卖！
			// 因为 DB stock (500) > 真实 stock (DB 500 - MQ 100 = 400)
			// rdb.Set(ctx, stockKey, 500, ...)
			
			// *** (见方案三：更安全的做法) ***
			// 简单的演示：我们先假设加载了DB库存
			// 在释放锁之前，我们必须完成加载
			
			// 释放锁
			rdb.Del(ctx, lockKey)
			
			// 告知用户重试，因为本次请求未处理
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Stock reloading, please retry in 5 seconds"})
			return

		} else {
			// (2) 别人正在重建，我“自旋”等待或立即失败
			log.Printf("Cache miss. Lock busy. Waiting for reload for %s", productID)
			
			// 策略a: 自旋等待 (不推荐，占用API连接)
			// time.Sleep(100 * time.Millisecond)
			// (goto 3. 重新尝试 AtomicDeductStock)

			// 策略b: 立即失败，让客户端重试
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Service busy, please retry"})
			return
		}
	}
	// ------------------------------------------------------------------
	// 缓存重建逻辑结束
	// ------------------------------------------------------------------


	switch deductCode {
	case 0:
		c.JSON(http.StatusOK, gin.H{"code": "SOLD_OUT", "message": "Sold out"})
		return
	case 1:
		// 扣减成功，继续
	}
    
    // ... (后续逻辑不变：发送Kafka等)
```

**方案二的致命缺陷：**
如代码注释所示，`dbStock := getStockFromDB(productID)` 这一步是**灾难的开始**。

  * 假设DB库存（Consumer刚处理完）是500。
  * 但Kafka里还有100条消息在途。
  * 真实可用库存是 400。
  * 你从DB加载了500到Redis。
  * 这导致系统**多卖了100件商品**（超卖）。

**结论：** 方案二（分布式锁+DB回源）在高并发场景下**不可取**，除非业务方能接受“在极端灾难下发生超卖”的后果。

-----

### 方案三：熔断 + 人工介入（100%准确，牺牲可用性）

这是面对金融、高价值商品（如茅台、iPhone）时唯一正确的方案。**牺牲可用性，保证数据绝对一致。**

**思路：** 如果Redis库存Key丢失（这本不该发生），立即“熔断”该商品的购买API，不允许任何新请求进入，直到人工修复数据。

**实现：** 修改 `api/handler.go`

```go
// ... (接之前的 HandlePurchase)

	// 3. 核心：原子扣库存
	deductCode, err := AtomicDeductStock(ctx, productID)
	if err != nil && err != redis.Nil { 
		log.Printf("Error deducting stock: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Service busy"})
		return
	}

	// ------------------------------------------------------------------
	// 【关键】处理缓存失效 (熔断方案)
	// ------------------------------------------------------------------
	if deductCode == -1 || err == redis.Nil { 
		// Key 不存在！这是灾难性故障！
		
		// 1. 立即熔断
		// (生产环境应使用更复杂的熔断器，如 Hystrix-go)
		// 简单实现：设置一个“熔断”标志
		circuitBreakerKey := fmt.Sprintf("circuit:product:%s", productID)
		rdb.Set(ctx, circuitBreakerKey, "BROKEN", 10*time.Minute) // 熔断10分钟

		// 2. 立即告警
		log.Printf("CRITICAL: Stock key for %s is MISSING. Circuit breaker triggered.", productID)
		// (此处应有 PagerDuty / 钉钉 / 企业微信告警)

		// 3. 拒绝请求
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code": "ACTIVITY_SUSPENDED", 
			"message": "Activity is temporarily suspended due to system error.",
		})
		return
	}
	
	// 如果其他请求进来时，发现已熔断，也立即拒绝
	circuitBreakerKey := fmt.Sprintf("circuit:product:%s", productID)
	if rdb.Exists(ctx, circuitBreakerKey).Val() > 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code": "ACTIVITY_SUSPENDED", 
			"message": "Activity is temporarily suspended.",
		})
		return
	}
	// ------------------------------------------------------------------
	// 熔断逻辑结束
	// ------------------------------------------------------------------


	switch deductCode {
	case 0:
		c.JSON(http.StatusOK, gin.H{"code": "SOLD_OUT", "message": "Sold out"})
		return
	case 1:
		// 扣减成功，继续
	}
    
    // ... (后续逻辑不变：发送Kafka等)
```

**SRE/运维的“人工修复”Playbook：**

1.  收到 `CRITICAL: Stock key for ... MISSING` 告警。
2.  **立即停止 API 服务**（或通过网关层切断该 `productID` 的所有写流量）。
3.  **监控 Kafka Consumer**，等待 `orders_topic` 中关于 `productID` 的消息**全部消费完毕**（Consumer Lag = 0）。
4.  此时，Kafka中积压的消息为0，API入口已关闭。
5.  **DB中的 `products.stock` 字段现在是 100% 准确的最终状态。**
6.  SRE/DBA 执行：`SELECT stock FROM products WHERE product_id = 123` (假设得到 400)。
7.  SRE/DBA 手动登录Redis-cli：`SET stock:product:123 400`
8.  SRE/DBA 手动删除熔断Key：`DEL circuit:product:123`
9.  **重新开启 API 服务**（或打开网关流量）。
10. 活动恢复。

-----

### 最终总结

1.  **方案一（高可用预防）：** 是99.99%的解决方案。通过Redis Sentinel/Cluster保证Key的连续性，防止它丢失。**Key永不主动过期**。
2.  **方案三（熔断+人工）：** 是0.01%的灾难恢复方案。如果Key真的丢了，立即熔断服务，等待Kafka消费完毕，从DB（唯一的真相源）中手动恢复数据到Redis，然后重启服务。这是唯一能保证100%数据准确性的方法。

## 其他高并发方案

我们刚刚讨论的 “**Redis（缓存）+ Kafka（消息队列）**” 方案，是业界公认的、专门针对C端 **“高并发写-强资源竞争（High Contention）”** 场景（如秒杀、抢购）的S级（S-Tier）解决方案。它本质上是一种 **“异步削峰”** 架构。

然而，“高并发”是一个非常宽泛的命题。**10k QPS的读** 和 **10k QPS的写** 所用的架构截然不同

除了“异步削峰”，业界的最佳实践中还有其他几种核心的高并发处理模式，它们分别应对不同的问题。

以下是除“缓存+MQ”方案之外，最常用、最重要的几种高并发架构实践。

-----

### 一、 读写分离 (Read/Write Splitting)

这是最经典、最基础的数据库高并发扩展方案，主要解决 **“读多写少”** 场景下的高并发读压力。

#### 1\. 详细介绍

现代C端业务（如新闻、电商、社交）通常是“读多写少”的，读QPS可能是写QPS的10倍甚至100倍。例如，1w人同时在浏览商品详情页，但只有10个人在下单。

MySQL的写入（INSERT/UPDATE/DELETE）会产生行锁、表锁，并且需要刷脏页，成本很高。如果大量的读（SELECT）请求和写请求竞争同一批资源，性能会急剧下降。

**读写分离** 的核心思想是：

1.  **主库 (Master):** 只负责处理**写**操作（事务）。
2.  **从库 (Slaves):** 负责处理所有**读**操作。
3.  **数据同步:** 主库通过 Binlog 实时（或异步）将数据变更同步给一个或多个从库。
4.  **应用层:** 在应用的数据访问层（DAO或DB中间件），根据SQL是 `SELECT` 还是 `UPDATE/INSERT`，将其路由到不同的数据库实例。

#### 2\. 优点

  * **架构简单:** 易于理解和实现，风险可控。
  * **成本低:** 增加几个从库实例的成本远低于升级主库硬件。
  * **读能力水平扩展:** 当读压力增大时，只需要增加从库（Slave）的数量即可。
  * **资源隔离:** 写操作的锁不会阻塞读操作，读操作的慢查询也不会拖垮主库。

#### 3\. 缺点

  * **数据延迟（核心缺点）:** 主从同步存在延迟（毫秒级到秒级）。
  * **数据不一致:** 刚在主库写入，立即去从库读，可能读不到（“最终一致性”）。
  * **主库单点:** 写的瓶颈依然在主库。如果写QPS非常高（比如你的1w/s修改库存），读写分离**无法解决**问题。

#### 4\. 适用业务场景

  * **读远大于写的业务:** 如新闻门户、博客、电商的商品浏览、论坛。
  * **对数据一致性要求不高的读场景:**
      * 场景A（适用）：用户刚发了评论（写主库），刷新页面（读从库）晚1秒看到自己的评论，可以接受。
      * 场景B（不适用）：用户刚充值（写主库），立即查询余额（读从库），发现钱没到账，不可接受。
  * **解决“充值后读不到”的办法：** 在某些关键流程中，强制所有读写都走主库（“强制主库读”）。

-----

### 二、 数据库分库分表 (Sharding)

当“读写分离”也无法解决问题时（例如，写QPS太高，或单一表数据量过亿），就需要“分库分表”。这是解决 **“数据存储与写入”** 瓶颈的“核武器”。

#### 1\. 详细介绍

分库分表的核心思想是 **“分而治之 (Divide and Conquer)”**。

  * **垂直拆分 (Vertical Sharding):**

      * **分库:** 将一个大数据库按业务拆分。例如，`user_db`（用户库）、`order_db`（订单库）、`product_db`（商品库）。
      * **分表:** 将一个大表按字段拆分。例如，`user_main`（存储登录信息）和 `user_profile`（存储用户详情）。
      * *目的: 解决业务耦合和表字段过多导致的IO问题。*

  * **水平拆分 (Horizontal Sharding):**

      * **分表:** 将一个大表（如 `orders` 表有50亿条）按照某个规则（如 `user_id`取模）拆分成1024个小表（`orders_0000` ... `orders_1023`）。
      * **分库:** 将1024个表均匀散落在16个数据库实例中。
      * *目的: 解决单表数据量过大和高并发写的瓶颈。*

如果有“1w/s修改库存”问题，如果使用Sharding，可能会这样做：
将 `stock` 表按照 `product_id` 进行Hash，分散到16个库、1024个表中。`product_id=123` 的请求永远落在 `stock_db_05.stock_0512` 表上。这样，1w/s的QPS就被分散到了16个数据库实例上，每个实例只承担约 600 QPS，就**有可能**抗住。

#### 2\. 优点

  * **真正意义上的水平扩展:** 理论上可以无限扩展数据库的存储和写入能力。
  * **极致的性能:** 将热点数据打散，极大降低了单库单表的锁竞争。

#### 3\. 缺点

  * **架构复杂度核弹级:**
      * **路由:** 应用层需要知道 `product_id=123` 到底该去哪个库的哪张表。通常需要引入 `Sharding-JDBC`、`MyCAT` 等中间件。
      * **全局ID:** 无法再使用数据库自增ID，必须使用雪花算法(Snowflake)等全局ID生成器。
      * **分布式事务:** 跨库的事务（如“下单”既要改 `orders` 库又要改 `stock` 库）成为噩梦。
      * **查询:** 无法简单 `JOIN` 和 `GROUP BY`。跨库查询需要依赖Elasticsearch或HBase等数据总线。
      * **扩容:** 增加机器（比如从16个库扩到32个库）意味着数据重平衡（Data Rebalancing），非常危险和复杂。

#### 4\. 适用业务场景

  * **数据量和QPS极高的业务:**
      * **订单/支付:** 支付宝、微信支付、淘宝订单，每天几亿笔交易，必须分库分表。
      * **社交Feed:** 微博、Twitter的发帖和时间线，数据量和读写量都是天文数字。
  * **不适用于秒杀:** 秒杀是 **“单一热点”** 问题。1w个请求都是在抢 *同一个* `product_id`。你无论怎么Sharding，这1w个请求最终还是会打到 *同一个* 库的 *同一张* 表的 *同一行* 上。因此，**Sharding解决不了秒杀的“热点行”问题**，而“缓存+MQ”方案能解决。

-----

### 三、 乐观锁 (Optimistic Locking)

这是另一种处理“写竞争”的思路，它与“悲观锁”（如 `SELECT ... FOR UPDATE`）相对。它假设“冲突是小概率事件”。

#### 1\. 详细介绍

乐观锁不使用数据库的锁机制，而是在业务层实现。

1.  在数据表中增加一个 `version`（版本号）字段，默认为0。
2.  **读取数据:** `SELECT stock, version FROM products WHERE product_id = 123`。
      * 此时读到 `stock = 10`, `version = 5`。
3.  **业务处理:** 在内存中计算 `stock = 10 - 1 = 9`。
4.  **写入数据:**
      * `UPDATE products`
      * `SET stock = 9, version = version + 1`
      * `WHERE product_id = 123 AND version = 5` (CAS, Compare-And-Swap)
5.  **检查结果:**
      * 如果 `RowsAffected == 1`：说明更新成功（因为只有我拿到了`version=5`）。
      * 如果 `RowsAffected == 0`：说明在我执行第4步之前，其他人已经把 `version` 改成6了（竞争失败）。
      * 竞争失败的线程需要**重试**（重新执行第2步）。

#### 2\. 优点

  * **无数据库锁:** 完全不依赖数据库的锁，没有死锁风险，在高并发“读”时性能很好。
  * **实现简单:** 只需要增加一个字段和修改UPDATE语句。

#### 3\. 缺点

  * **CPU空转:** 竞争失败的线程需要“自旋”重试，在高并发冲突下（如秒杀）会大量消耗CPU。
  * **ABA问题:** 如果一个值从A-\>B-\>A，version会变化，但乐观锁无法感知“值”的变化。
  * **不适用高竞争场景:** 这是乐观锁的**致命弱点**。
      * 在1w/s抢100件库存的场景下，99%的请求都会更新失败（`RowsAffected == 0`）。
      * 这99%的失败请求会立即重试，导致API服务器、连接池、CPU全部被打满，引发\*\*“重试风暴”\*\*，最终系统雪崩。

#### 4\. 适用业务场景

  * **写竞争概率低的场景:**
      * 例如，编辑Wiki词条。1000个人同时在看，但只有2个人“碰巧”在同一秒点击了“保存”。第一个人保存成功（version+1），第二个人保存失败，系统提示“内容已更新，请刷新重试”。
  * **更新用户个人资料:** 多个设备同时修改头像，只有最后一次（或第一次）CAS成功。
  * **绝对不适用于秒杀。**

-----

### 四、 CDN 与边缘计算 (CDN & Edge Computing)

这是解决 **“超高并发读”** 和 **“静态内容”** 问题的银弹。它试图在流量到达你的服务器之前就解决问题。

#### 1\. 详细介绍

  * **CDN (Content Delivery Network):**

      * 将你的静态资源（图片、JS、CSS、视频）甚至“半动态”内容（如商品详情页HTML）缓存到离用户最近的边缘节点上。
      * 100w人同时看商品详情页，这些请求全部由CDN节点（如Cloudflare/Akamai）响应，你的源站服务器QPS可能为0。

  * **边缘计算 (Edge Computing, e.g., Cloudflare Workers, Lambda@Edge):**

      * 更进一步，可以在CDN边缘节点上运行简单的逻辑（代码）。
      * 例如，在秒杀开始前，边缘节点可以直接返回“活动未开始”。秒杀结束后，边缘节点可以直接返回“活动已结束”。
      * 只有在活动进行中的那几分钟，流量才被允许“回源”到你的API服务器。

#### 2\. 优点

  * **极大缓解源站压力:** 将99%的读请求拦截在“国门”之外。
  * **极致的用户体验:** 用户从最近的节点获取数据，延迟极低。
  * **防御DDoS:** CDN是天然的DDoS防御层。

#### 3\. 缺点

  * **成本:** 流量费用很高。
  * **缓存一致性:** 和Redis一样，存在缓存刷新和数据一致性问题。
  * **功能受限:** 边缘计算只能处理简单、无状态的逻辑。

#### 4\. 适用业务场景

  * **所有C端业务:** 只要有静态资源（图片、JS），就必须上CDN。
  * **高并发读:** 新闻、视频、电商详情页。
  * **全局业务:** 为全球用户提供低延迟访问。

-----

### 总结对比

| 方案 | 核心思想 | 解决的主要问题 | 优点 | 缺点 |
| :--- | :--- | :--- | :--- | :--- |
| **缓存+MQ (异步削峰)** | 异步、解耦、削峰 | **高并发“写”竞争** (单一热点) | 吞吐量极高、用户响应快 | 最终一致性、架构复杂、依赖中间件 |
| **读写分离** | 主写从读 | **高并发“读”** (读多写少) | 简单、低成本、读能力易扩展 | 主从延迟、写瓶颈仍在主库 |
| **分库分表 (Sharding)** | 分而治之 | **高并发“写”** (分散热点)、**海量数据存储** | 真正水平扩展、性能极致 | 复杂度极高、分布式事务、查询困难 |
| **乐观锁 (CAS)** | 无锁+版本号重试 | **低并发“写”竞争** | 简单、无数据库锁 | 高竞争下引发“重试风暴”，CPU空转 |
| **CDN与边缘计算** | 流量卸载 | **超高并发“读”** (静态/半动态) | 极大缓解源站压力、低延迟 | 成本高、缓存一致性、功能受限 |

针对秒杀类需求：

  * **唯一解:** “缓存(Redis)+MQ(Kafka) 异步削峰”。
  * **无效解:** “读写分离”（不解决写瓶颈）、“分库分表”（热点在同一行，分片无效）、“乐观锁”（引发重试风暴）。