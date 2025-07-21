---
title: "支付系统设计"
date: 2025-07-21T22:00:00+08:00
draft: false
toc: true
featured: true
categories: ["技术/实践/后端"]
tags: ["系统设计"]
---


## 摘要

本方案旨在设计一个支持全球多PSP（Payment Service Provider）的统一支付平台。平台核心功能包括**聚合收单**、**批量付款**、**内部记账**和**自动化对账清算**。设计遵循业界领先的实践，强调系统的**高可用**、**高一致性**、**安全**与**可扩展性**，并采用微服务架构实现功能解耦。

-----

## 1\. 功能性需求 (Functional Requirements)

### 1.1 核心支付功能

  * **统一收单网关**:
      * 提供统一的API，接收来自业务方的收款请求。
      * 支持多种支付方式（信用卡、电子钱包、银行转账等）。
      * 根据规则（如费用、成功率、地区）动态路由到最优的PSP通道。
      * 处理来自PSP的同步返回和异步回调通知。
  * **统一付款服务**:
      * 提供API，支持向个人或企业账户进行单笔或批量付款。
      * 支持付款前的审批流（可选）。
      * 管理付款订单的生命周期。
  * **PSP适配层**:
      * 为每个集成的PSP（如Stripe, Adyen, PayPal）提供标准化的适配器。
      * 封装各PSP的API差异，对上层服务提供统一接口。
      * 管理各PSP的API密钥和配置。

### 1.2 账务核心功能

  * **内部账本 (Ledger)**:
      * 采用复式记账法，确保账务平衡。
      * 为每个商户、平台、PSP等实体设立虚拟账户。
      * 记录每一笔资金流动的借贷条目。
      * 提供账户余额查询和交易流水查询功能。
  * **对账与清算**:
      * 自动化下载和解析PSP的交易对账单和资金结算单。
      * 将PSP账单与系统内部支付订单进行逐笔匹配。
      * 自动识别和处理差异（长款、短款、状态不一致），生成差异报告。
      * 根据对账结果，进行内部资金清算，更新商户可提现余额。

### 1.3 支撑功能

  * **状态机管理**:
      * 所有支付、付款订单的状态必须遵循预定义的、单向流转的状态机。例如，支付订单状态：`CREATED` -\> `PROCESSING` -\> `SUCCEEDED` / `FAILED`。一旦进入终态（`SUCCEEDED`/`FAILED`），状态不可逆转。
  * **商户管理**:
      * 商户入网、信息管理、费率配置。
  * **风控与合规**:
      * 基础的交易限额、黑名单校验。
      * 为符合KYC/AML要求预留扩展点。

-----

## 2\. 非功能性需求 (Non-Functional Requirements)

  * **高可用性 (High Availability)**:
      * 系统核心服务可用性 \> 99.99%。
      * 关键服务应采用多活或主备部署，具备跨可用区/跨地域容灾能力。
  * **数据一致性 (Consistency)**:
      * 账务核心数据必须保证强一致性（ACID）。
      * 对于分布式流程，采用最终一致性，并提供补偿和对账机制确保最终结果正确。
  * **可扩展性 (Scalability)**:
      * 系统应能水平扩展，以应对未来交易量的增长。
      * 无状态服务设计，便于动态扩缩容。
  * **安全性 (Security)**:
      * 遵循PCI-DSS安全标准，敏感信息（如银行卡号）不落库，采用Tokenization方案。
      * 所有通信必须使用TLS加密。
      * API访问需经过严格认证和授权。
      * 防止SQL注入、CSRF等常见Web攻击。
  * **低延迟 (Low Latency)**:
      * 收单API响应时间P99 \< 500ms。
  * **幂等性 (Idempotency)**:
      * 所有对外接口（特别是创建和更新操作）必须支持幂等性，防止因网络重试导致重复交易。
  * **可观测性 (Observability)**:
      * 提供全面的日志记录、指标监控（Metrics）和分布式追踪（Tracing）。

-----

## 3\. 顶层设计 (High-Level Design)

我们将采用基于微服务的架构，将系统解耦为专注不同领域的服务。

### 3.1 服务划分

  * **API Gateway**: 统一入口，负责鉴权、路由、限流、协议转换。
  * **Payment Service**: 核心收单服务，处理支付订单创建、状态机流转、路由选择。
  * **Payout Service**: 核心付款服务，处理付款请求和状态管理。
  * **PSP Adapter Service**: PSP适配器层，一个无状态的服务，负责与具体的PSP API交互。
  * **Ledger Service**: 账务核心，提供原子化的记账操作（基于复式记账），是全系统的资金事实来源 (Source of Truth)。
  * **Reconciliation Service**: 对账服务，通常是定时或事件触发的批处理任务，负责拉取PSP账单并进行核对。

### 3.2 核心流程 - 收单示例

1.  **创建订单**: 业务方通过API Gateway调用Payment Service，请求创建支付订单，并提供幂等键 (`Idempotency-Key`)。
2.  **状态初始化**: Payment Service在数据库中创建一条支付订单记录，初始状态为 `CREATED`。
3.  **路由与调用**: Payment Service根据路由规则选择一个PSP，并通过异步消息（如Kafka）将任务发送给PSP Adapter Service。订单状态更新为 `PROCESSING`。
4.  **与PSP交互**: PSP Adapter Service调用相应PSP的API发起支付。
5.  **处理回调**: PSP完成支付后，会通过Webhook异步通知。PSP Adapter Service接收通知，验证签名后，将结果通过消息队列发回。
6.  **更新终态**: Payment Service接收到PSP结果消息，将订单状态更新为 `SUCCEEDED` 或 `FAILED`。
7.  **记账**: Payment Service通知Ledger Service进行记账。例如，一笔成功的交易会产生如下分录：
      * 借：PSP应收账款账户
      * 贷：商户待结算账户
      * 贷：平台手续费收入账户

-----

## 4\. 技术选型 (Technology Stack)

| 类别             | 技术/工具                                                    | 理由                                                                                                        |
| ---------------- | ------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------- |
| **编程语言** | **Go (Golang)** | 高并发性能、静态类型安全、强大的标准库、部署简单，非常适合构建高性能网络服务。                                |
| **Web/API框架** | **Gin** 或 **Echo** | 轻量级、高性能，生态成熟。                                                                                  |
| **RPC框架** | **gRPC** | 用于服务间通信，基于HTTP/2，性能高效，提供强类型的服务定义。                                                |
| **数据库** | **PostgreSQL** | 强大的ACID事务支持，适用于账务等核心数据。JSONB字段可用于存储非结构化元数据。                                 |
| **缓存/幂等性** | **Redis** | 用于缓存热点数据、存储幂等键处理状态、实现分布式锁。                                                          |
| **消息队列** | **Kafka** | 高吞吐量、持久化、可重放的事件流平台，用于服务解耦和异步任务处理，是对账和事件溯源的理想选择。              |
| **容器化与编排** | **Docker & Kubernetes (K8s)** | 实现标准化部署、弹性伸缩和高可用性。                                                                          |
| **可观测性** | **Prometheus** (监控), **Grafana** (可视化), **OpenTelemetry** (追踪), **ELK/Loki** (日志) | 业界标准的组合，提供全方位的系统洞察力。                                                                    |
| **CI/CD** | **GitLab CI/CD** 或 **Jenkins** | 自动化构建、测试和部署流程。                                                                                |

-----

## 5\. 业界最佳实践的表设计

我们采用`DECIMAL`或`BIGINT`（存储分单位）来精确表示金额，避免浮点数精度问题。主键使用`UUID`以避免在分布式环境下ID冲突。

### `payment_orders` - 支付订单表

```sql
CREATE TABLE payment_orders (
    id UUID PRIMARY KEY,
    merchant_id UUID NOT NULL,
    amount DECIMAL(19, 4) NOT NULL, -- 金额
    currency VARCHAR(3) NOT NULL, -- ISO 4217 货币代码
    status VARCHAR(20) NOT NULL, -- CREATED, PROCESSING, SUCCEEDED, FAILED
    psp_id UUID, -- 使用的PSP配置ID
    psp_transaction_id VARCHAR(255), -- PSP返回的交易ID
    idempotency_key VARCHAR(128) UNIQUE NOT NULL, -- 幂等键
    error_code VARCHAR(50),
    error_message TEXT,
    metadata JSONB, -- 其他元数据
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_payment_orders_status ON payment_orders(status);
CREATE INDEX idx_payment_orders_merchant_id ON payment_orders(merchant_id);
CREATE INDEX idx_payment_orders_created_at ON payment_orders(created_at);
```

### `ledger_accounts` - 会计账户表 (复式记账)

```sql
CREATE TABLE ledger_accounts (
    id UUID PRIMARY KEY,
    account_name VARCHAR(255) UNIQUE NOT NULL, -- e.g., 'merchant_A_payable', 'psp_B_receivable'
    account_type VARCHAR(20) NOT NULL, -- ASSET, LIABILITY, EQUITY, REVENUE, EXPENSE
    currency VARCHAR(3) NOT NULL,
    balance DECIMAL(19, 4) NOT NULL DEFAULT 0.00,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### `ledger_transactions` - 账务交易表

```sql
CREATE TABLE ledger_transactions (
    id UUID PRIMARY KEY,
    description VARCHAR(255),
    transaction_date DATE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### `ledger_entries` - 账务分录表

```sql
CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    transaction_id UUID NOT NULL REFERENCES ledger_transactions(id),
    account_id UUID NOT NULL REFERENCES ledger_accounts(id),
    direction VARCHAR(10) NOT NULL, -- DEBIT or CREDIT
    amount DECIMAL(19, 4) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_ledger_entries_transaction_id ON ledger_entries(transaction_id);
CREATE INDEX idx_ledger_entries_account_id ON ledger_entries(account_id);
-- 约束：一个transaction下的所有分录借贷必相加为0 (在应用层或数据库触发器中强制保证)
```

### `reconciliation_batches` - 对账批次表

```sql
CREATE TABLE reconciliation_batches (
    id UUID PRIMARY KEY,
    psp_id UUID NOT NULL,
    statement_date DATE NOT NULL,
    status VARCHAR(20) NOT NULL, -- PENDING, PROCESSING, COMPLETED, FAILED
    raw_file_url VARCHAR(512),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### `reconciliation_items` - 对账明细表

```sql
CREATE TABLE reconciliation_items (
    id UUID PRIMARY KEY,
    batch_id UUID NOT NULL REFERENCES reconciliation_batches(id),
    payment_order_id UUID REFERENCES payment_orders(id), -- 内部订单ID
    psp_transaction_id VARCHAR(255) NOT NULL, -- PSP账单中的交易ID
    status VARCHAR(30) NOT NULL, -- MATCHED, MISMATCH_AMOUNT, MISSING_IN_SYSTEM, MISSING_IN_PSP
    internal_amount DECIMAL(19, 4),
    psp_amount DECIMAL(19, 4),
    discrepancy DECIMAL(19, 4), -- 差异金额
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_reconciliation_items_batch_id ON reconciliation_items(batch_id);
CREATE INDEX idx_reconciliation_items_status ON reconciliation_items(status);
```

-----

## 6\. GoLang 关键核心代码实现

以下是几个核心逻辑的简化版Go代码示例。

### 6.1 PSP 适配器模式 (Adapter Pattern)

定义一个统一的接口，所有PSP的实现都遵循这个接口。

```go
package psp

import "context"

// PaymentRequest contains all info for creating a payment.
type PaymentRequest struct {
	OrderID string
	Amount  int64 // Use minor units (e.g., cents)
	Currency string
}

// PaymentResponse is the result from a PSP.
type PaymentResponse struct {
	PSPTransactionID string
	Status         string // e.g., "SUCCEEDED", "FAILED"
}

// Gateway defines the standard interface for any payment provider.
type Gateway interface {
	CreatePayment(ctx context.Context, req *PaymentRequest) (*PaymentResponse, error)
	GetPaymentStatus(ctx context.Context, pspTransactionID string) (*PaymentResponse, error)
}

// StripeAdapter is an implementation for Stripe.
type StripeAdapter struct {
	// client for stripe sdk
}

func (s *StripeAdapter) CreatePayment(ctx context.Context, req *PaymentRequest) (*PaymentResponse, error) {
	// TODO: Implement Stripe API call logic here.
	// stripe.Charge.New(...)
	return &PaymentResponse{
		PSPTransactionID: "pi_stripe_123",
		Status:         "SUCCEEDED",
	}, nil
}

// AdyenAdapter is an implementation for Adyen.
type AdyenAdapter struct {
	// client for adyen sdk
}

func (a *AdyenAdapter) CreatePayment(ctx context.Context, req *PaymentRequest) (*PaymentResponse, error) {
	// TODO: Implement Adyen API call logic here.
	// adyen.Payment.New(...)
	return &PaymentResponse{
		PSPTransactionID: "ady_adyen_456",
		Status:         "SUCCEEDED",
	}, nil
}
```

### 6.2 支付订单状态机 (State Machine)

确保状态单向流转。

```go
package payment

type OrderStatus string

const (
	StatusCreated   OrderStatus = "CREATED"
	StatusProcessing OrderStatus = "PROCESSING"
	StatusSucceeded OrderStatus = "SUCCEEDED"
	StatusFailed    OrderStatus = "FAILED"
)

// transitionRules defines allowed transitions.
var transitionRules = map[OrderStatus][]OrderStatus{
	StatusCreated:   {StatusProcessing},
	StatusProcessing: {StatusSucceeded, StatusFailed},
	StatusSucceeded: {}, // Terminal state
	StatusFailed:    {}, // Terminal state
}

// CanTransitionTo checks if a state transition is valid.
func (current StatusOrderStatus) CanTransitionTo(next OrderStatus) bool {
	allowedStates, ok := transitionRules[current]
	if !ok {
		return false
	}
	for _, s := range allowedStates {
		if s == next {
			return true
		}
	}
	return false
}

// Example usage in service layer:
//
// func (s *Service) ProcessPayment(order *Order) error {
//     if !order.Status.CanTransitionTo(StatusProcessing) {
//         return fmt.Errorf("invalid state transition from %s to %s", order.Status, StatusProcessing)
//     }
//     order.Status = StatusProcessing
//     // ... save to DB
//     return nil
// }
```

### 6.3 幂等性中间件 (Idempotency Middleware for Gin)

使用Redis来保证API的幂等性。

```go
package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

// IdempotencyMiddleware creates a gin middleware for idempotency.
func IdempotencyMiddleware(redisClient *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		idempotencyKey := c.GetHeader("Idempotency-Key")
		if idempotencyKey == "" {
			c.Next()
			return
		}

		// 1. Check if the key is already being processed or completed
		keyStatus := "idempotency:status:" + idempotencyKey
		keyResponse := "idempotency:response:" + idempotencyKey

		status, err := redisClient.Get(c.Request.Context(), keyStatus).Result()
		if err == nil && status == "completed" {
			// Request was completed, return cached response
			cachedResponse, err := redisClient.Get(c.Request.Context(), keyResponse).Result()
			if err == nil {
				c.String(http.StatusOK, cachedResponse)
				c.Abort()
				return
			}
		}

		// 2. Lock the key to prevent concurrent processing
		lockKey := "idempotency:lock:" + idempotencyKey
		locked, err := redisClient.SetNX(c.Request.Context(), lockKey, "locked", 10*time.Second).Result()
		if err != nil || !locked {
			c.JSON(http.StatusConflict, gin.H{"error": "request with this idempotency key is already being processed"})
			c.Abort()
			return
		}
		defer redisClient.Del(c.Request.Context(), lockKey) // Unlock

		// 3. Mark as in-progress (optional but good practice)
		redisClient.Set(c.Request.Context(), keyStatus, "processing", 24*time.Hour)

		// Create a response writer to capture the response
		// ... (logic to capture response body)

		c.Next() // Process the actual request handler

		// 4. After processing, cache the response and mark as completed
		// responseBody := capturedResponseWriter.Body.String()
		// redisClient.Set(c.Request.Context(), keyResponse, responseBody, 24*time.Hour)
		redisClient.Set(c.Request.Context(), keyStatus, "completed", 24*time.Hour)
	}
}
```

### 6.4 复式记账核心逻辑 (Double-Entry Ledger Logic)

确保记账操作的原子性和平衡性。

```go
package ledger

import (
	"context"
	"database/sql"
	"fmt"
)

type Entry struct {
	AccountID string
	Direction string // "DEBIT" or "CREDIT"
	Amount    int64  // Use minor units
}

type LedgerService struct {
	db *sql.DB
}

// CreateTransaction creates a balanced, double-entry transaction.
func (s *LedgerService) CreateTransaction(ctx context.Context, entries []Entry) error {
	var debitSum, creditSum int64
	for _, entry := range entries {
		if entry.Direction == "DEBIT" {
			debitSum += entry.Amount
		} else if entry.Direction == "CREDIT" {
			creditSum += entry.Amount
		} else {
			return fmt.Errorf("invalid entry direction: %s", entry.Direction)
		}
	}

	if debitSum != creditSum {
		return fmt.Errorf("unbalanced transaction: debits (%d) != credits (%d)", debitSum, creditSum)
	}

	// Use a database transaction to ensure atomicity
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // Rollback on error

	// 1. Create the transaction record
	// ... tx.ExecContext(...) to insert into ledger_transactions

	// 2. Create each entry record
	for _, entry := range entries {
		// ... tx.ExecContext(...) to insert into ledger_entries
		// 3. Update account balances
		// ... tx.ExecContext("UPDATE ledger_accounts SET balance = balance + ? WHERE id = ?", ...)
	}

	// If all good, commit the transaction
	return tx.Commit()
}

```

---


系统运行时的必然会遇到一些异常，我们继续深化设计，解决在实际运行中必然会遇到的几个关键问题。这些异常处理机制是支付系统健壮性的核心体现。

---

## 1. 支付系统调用PSP的重试机制设计

在与外部PSP服务通信时，网络抖动、对方服务临时不可用等问题是常态。一个健壮的重试机制至关重要，但必须智能地执行，避免造成重复支付或加剧系统雪崩。

### 1.1. 错误分类

首先，必须对PSP返回的错误进行分类：

* **可重试的瞬时错误 (Transient Errors)**:
    * **网络问题**: Connection Timeout, Read Timeout, DNS解析失败等。
    * **PSP服务端临时问题**: HTTP `502 Bad Gateway`, `503 Service Unavailable`, `504 Gateway Timeout`。
    * **速率限制**: HTTP `429 Too Many Requests`。
    * **PSP定义的特定可重试错误码**: 例如，PSP返回的业务错误码明确指出“系统繁忙，请稍后重试”。

* **不可重试的确定性错误 (Permanent Errors)**:
    * **客户端请求错误**: HTTP `400 Bad Request` (如参数格式错误), `401 Unauthorized` (密钥错误), `403 Forbidden`。
    * **业务逻辑失败**: HTTP `402 Payment Required` 或 `200 OK` 但返回明确的失败码，如“余额不足”、“卡号无效”、“风险交易被拒”。
    * 任何业务上明确的失败都不应该重试。

### 1.2. 重试策略：带抖动的指数退避 (Exponential Backoff with Jitter)

这是业界标准的重试策略，可以有效避免在下游服务恢复时，大量重试请求瞬间涌入导致的“惊群效应” (Thundering Herd)。

* **指数退避**: 每次重试的间隔时间呈指数级增长。例如：1s, 2s, 4s, 8s...
* **抖动 (Jitter)**: 在指数退避的基础上增加一个随机值。例如，第3次重试的基础等待时间是4s，加入抖动后，实际等待时间可能是 `4s + random(0ms, 1000ms)` 之间的一个值。这可以打散重试请求，使其在时间上分布得更均匀。
* **最大重试次数**: 设定一个封顶的重试次数（如3-5次）。达到上限后，如果仍然失败，则必须将订单置为最终失败状态。

### 1.3. 设计实现

1.  **同步调用场景 (用户等待中)**:
    * 这种场景下，重试窗口非常短（总时长通常不能超过几秒）。
    * 可以进行1-2次快速重试（例如，间隔500ms, 1s）。
    * **关键**: 每次重试必须使用**相同的幂等键**。这是防止重复支付的生命线。
    * 若快速重试后仍然失败，应立即向用户返回“处理中”状态，并将任务转为异步处理。

2.  **异步处理场景 (后台任务)**:
    * 这是重试机制的主要应用场景。
    * **流程**:
        a. 支付服务（Payment Service）将调用PSP的任务放入消息队列（如Kafka）。
        b. PSP适配器服务（PSP Adapter）消费消息，并执行首次调用。
        c. **如果遇到可重试错误**:
            i. 计算下一次重试的时间点（`now() + base_interval * 2^attempt + random_jitter`）。
            ii. 将该任务重新投递到**一个专门的延迟队列**中，或在消息体中附加“下次执行时间”的属性，由消费者判断是否执行。
            iii. 增加消息的“已重试次数”计数器。
        d. **如果遇到不可重试错误**: 直接将结果（失败）通知给支付服务，更新订单为`FAILED`。
        e. **如果达到最大重试次数**: 同样通知支付服务，更新订单为`FAILED`。
        f. **如果成功**: 通知支付服务，更新订单状态。

---

## 2. PSP回调成功，但支付系统订单已Failed的处理

这是一个经典的分布式系统状态冲突问题，通常由系统超时引起。

### 2.1. 问题场景

1.  `T0`: 支付服务发起支付，将订单状态置为`PROCESSING`，并开始等待PSP的回调。
2.  `T0 + 15min`: 支付服务设置的内部超时定时器触发（例如15分钟内未收到任何回调）。为避免商户资金长时间不确定，系统将该订单状态更新为`FAILED`。
3.  `T0 + 16min`: PSP的成功回调通知 (`SUCCESS`) 到达系统。此时，内部订单状态已经是`FAILED`。

### 2.2. 处理原则

* **事实优先**: PSP的账单是资金转移的最终事实。如果PSP确认收款成功，那么钱确实已经被划走了。
* **状态机单向性**: 严格遵守`FAILED`是终态的原则，**绝不能**直接将`FAILED`状态扭转为`SUCCEEDED`。这会破坏状态机的可信度，并可能导致下游业务逻辑混乱（例如，库存已经释放，现在又要重新扣减）。
* **异常记录与告警**: 这是一个必须被记录和跟进的异常情况。

### 2.3. 具体处理方式

1.  **识别冲突**: 当回调处理器接收到PSP的成功通知时，它会加载内部订单。此时发现 `internal_order.status == 'FAILED'`，冲突被识别。

2.  **创建冲正交易 (Reversal Transaction)**:
    * 系统不修改原`FAILED`订单。
    * 在`reconciliation_items`（对账明细表）或一个专门的`payment_anomalies`（支付异常表）中，创建一条记录，标记为`LATE_SUCCESS_ON_FAILED_ORDER`。记录下内部订单ID、PSP交易ID、金额等所有相关信息。

3.  **人工干预队列与告警**:
    * 将这条异常记录推送到一个需要人工审核的“异常处理队列”中。
    * 立即通过监控系统（如PagerDuty, Slack）向财务运营团队和技术团队发送**高优告警**。

4.  **人工或半自动化处理**:
    * **业务决策**: 运营团队需要根据业务场景决定如何处理这笔“意外之财”。
        * **场景A：商品/服务未提供**: 如果因为订单`FAILED`而没有给用户提供服务，最佳选择是**发起退款**。运营人员通过后台系统，针对这笔PSP交易发起一次全额退款操作。
        * **场景B：服务已提供或无法撤销**: 如果业务逻辑复杂，或者需要为商户入账，运营人员需要执行**手动入账**流程。
    * **账务修正**: 无论是退款还是手动入账，都需要在**账务核心 (Ledger Service)** 中创建一笔新的、独立的账务交易来使账目平衡。
        * **手动入账分录**:
            * 借：PSP应收账款
            * 贷：商户待结算账户
            * 贷：平台手续费收入
        * 这笔分录的描述应明确指向原始的`FAILED`订单和本次异常事件，便于审计。

---

## 3. 对账异常的具体处理方式

对账的核心是将我们的记录与PSP的记录进行`JOIN`操作。异常就是`JOIN`失败或`JOIN`后字段不匹配的情况。

### 3.1. Case 1: PSP有，系统无 (长款 - We Owe Money)

* **描述**: PSP的对账单里有一笔成功的交易，但在我们的`payment_orders`表中找不到对应的记录。
* **可能原因**:
    * 用户在支付流程中，请求到达了PSP，但在写入我们自己数据库前，我们的系统崩溃了。
    * 有人绕过系统，直接在PSP的后台（Dashboard）创建了一笔收款。
* **处理流程**:
    1.  **记录差异**: 在`reconciliation_items`表中创建一条记录，`status`为`MISSING_IN_SYSTEM`。
    2.  **高优告警**: 通知财务和技术团队。
    3.  **人工调查**:
        * 财务人员需要根据PSP账单中的信息（如购买者邮箱、备注）来确定这笔资金的归属。
        * 联系对应的商户，确认是否有这笔交易。
    4.  **资金处理**:
        * **若能找到归属**: 通过后台工具**补单**，即手动创建`payment_order`记录和相应的账务分录，然后将这笔对账差异标记为“已解决”。
        * **若无法找到归属**: 这笔资金成为平台的“无人认领资金”。在账务上，记入一个临时的“暂收应付款”科目。在一定期限后（如6个月），如果仍无人认领，根据公司财务制度，可能计入营业外收入，或持续挂账。

### 3.2. Case 2: 系统有，PSP无 (短款 - We are Owed Money)

* **描述**: 我们的系统里有一笔`SUCCEEDED`的订单，但在PSP的对账单（包括交易单和结算单）里都找不到。
* **可能原因**:
    * 我们错误地处理了PSP的回调，将一个失败的请求记为了成功。
    * PSP的结算周期问题，这笔交易可能在下一期的账单中。
* **处理流程**:
    1.  **设置宽限期**: 不要立即判断为异常。将这条记录标记为`PENDING_MATCH`，等待1-2个对账周期。
    2.  **主动查询**: 如果在宽限期后仍然不存在，使用订单的`psp_transaction_id`调用PSP的`GetPaymentStatus` API进行主动查询。
    3.  **确认差异**: 如果API确认该交易**不存在**或**失败**，那么这就是一个严重的内部状态错误。
        * 在`reconciliation_items`中标记为`MISSING_IN_PSP`。
        * **严重告警**: 通知开发团队和财务团队，这通常意味着系统存在Bug。
    4.  **账务冲正**:
        * 运营或财务人员需要执行一个**内部冲正**操作。
        * 在账务核心中，做一笔与原始入账分录完全相反的会计分录，以轧平账目。
        * **状态修正**: 对`payment_orders`表中的这条错误记录，不能直接修改状态。应增加一个`adjustment_status`字段或在关联的修正表里标记其已被冲正，并附上原因。这保证了原始记录的不可篡改性，符合审计要求。

### 3.3. Case 3: 金额/货币不匹配

* **描述**: 系统和PSP都有记录，但金额或货币不一致。
* **可能原因**:
    * **手续费**: 我们记录的是订单金额，PSP账单记录的是扣除手续费后的净额。
    * **退款**: 发生了部分退款。
    * **货币换算**: 涉及跨境支付时，汇率差或换汇手续费导致金额不一致。
* **处理流程**:
    1.  **记录差异**: 在`reconciliation_items`中标记为`AMOUNT_MISMATCH`，并记录下双方的金额和差额。
    2.  **自动化规则匹配**:
        * 系统可以预置PSP的费率规则。如果差额正好等于`订单金额 * 费率 + 固定费用`，系统可以自动将该差异归因于“手续费”，并标记为“已解决”。
        * 同样，如果差额与系统中的退款记录匹配，也可以自动核销。
    3.  **人工审核**: 对于无法被规则自动解释的差额，推送到人工审核队列。
    4.  **账务调整**: 财务人员审核后，手动将差额计入正确的会计科目，如“银行手续费”、“汇兑损益”等。