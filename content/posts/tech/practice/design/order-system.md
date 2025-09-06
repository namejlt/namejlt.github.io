---
title: "订单系统设计"
date: 2025-09-06T09:00:00+08:00
draft: false
toc: true
categories: ["技术/实践/设计"]
tags: ["设计"]
---

## 订单系统设计

本文要设计一个订单系统，并提供一套完整、可扩展、高可用的架构方案。

-----

### **1. 概述 (Overview)**

#### **1.1. 设计目标**

订单系统是电商交易流程的核心，其稳定性、可扩展性和性能直接影响用户体验和公司营收。本次设计旨在构建一个具备以下特点的现代化订单系统：

  * **高并发与高性能**: 能够从容应对大促、秒杀等瞬时高流量场景，实现订单创建的低延迟。
  * **高可用与数据一致性**: 保证系统7x24小时稳定运行，即使在部分服务故障时，也能确保订单数据的最终一致性。
  * **高扩展性与可维护性**: 采用微服务架构，业务模块解耦，支持未来业务的快速迭代和横向扩展。
  * **业务精细化**: 支持复杂的订单业务场景，如订单拆分、多种促销优惠、售后服务等。

#### **1.2. 核心挑战 (Key Challenges)**

  * **“写”操作的并发冲突**: 订单创建涉及库存扣减，是典型的“狼多肉少”场景，如何在高并发下保证库存数据的准确性是核心难点。
  * **分布式事务**: 订单创建流程跨越多个服务（商品、库存、优惠券、支付、用户），如何保证这些操作的原子性是一个巨大挑战。
  * **订单状态机管理**: 订单生命周期长且状态复杂，如何清晰、准确、可靠地管理订单状态流转至关重要。
  * **海量数据存储与查询**: 随着业务增长，订单数据将达到百亿甚至千亿级别，如何设计存储方案以保证高效的写入和查询。

### **2. 整体架构 (Overall Architecture)**

我们将采用成熟的**微服务架构**，将订单系统拆分为多个独立的服务，通过轻量级的通信机制（如gRPC或RESTful API）协作。

#### **2.1. 核心服务**

  * **订单网关 (Order Gateway)**: 作为流量入口，负责鉴权、路由、协议转换、限流熔断等。
  * **订单编排服务 (Order Orchestration Service)**: **核心中的核心**，负责协调整个订单创建流程。它不执行具体业务，而是像一个总指挥，调用其他原子服务完成任务。
  * **订单核心服务 (Order Core Service)**: 负责订单数据的持久化（写库）、状态机管理、以及订单的查询。
  * **购物车服务 (Cart Service)**: 管理用户的购物车数据。
  * **商品服务 (Product Service)**: 提供商品信息查询。
  * **库存服务 (Inventory Service)**: 提供库存查询和扣减原子能力。
  * **营销/优惠服务 (Promotion Service)**: 计算订单包含的优惠，如优惠券、满减等。
  * **支付服务 (Payment Service)**: 与支付网关交互，处理支付请求和回调。
  * **履约服务 (Fulfillment Service)**: 负责订单支付成功后的后续流程，如WMS（仓储管理）、TMS（物流管理）对接。

#### **2.2. 基础设施**

  * **消息队列 (Message Queue - 如Kafka/RocketMQ)**: 用于服务间的异步通信和解耦，是应对高并发写入的关键组件。
  * **分布式缓存 (Distributed Cache - 如Redis)**: 用于缓存热点数据（商品信息、库存），加速读取，并用于实现分布式锁等。
  * **数据库 (Database - 如MySQL/PostgreSQL)**: 采用关系型数据库保证核心交易数据的强一致性。
  * **分布式任务调度 (Job Scheduler - 如XXL-Job)**: 用于处理定时任务，如超时未支付订单的关闭。

### **3. 核心业务流程设计 (Core Business Flows)**

#### **3.1. 订单创建流程 (Order Creation Flow)**

这是整个系统最复杂、并发最高的流程。我们将采用\*\*“读写分离 + 异步化”\*\*的设计思想。

**流程拆解:**

1.  **阶段一：交易验证 (前端 -\> 订单网关 -\> 订单编排服务)**

      * 用户在购物车或商品页点击“结算”，前端携带商品SKU、数量、收货地址等信息发起请求。
      * **订单编排服务**开始工作：
        1.  **前置校验**: 并发调用**用户服务**、**商品服务**、**库存服务**、**营销服务**，进行一系列的“只读”验证。
              * 用户身份、收货地址是否合法？
              * 商品是否存在、是否上架、价格是否变动？
              * **库存是否充足？（只读，不下锁）**
              * 优惠券是否可用？
        2.  **价格试算**: 根据商品价格、运费模板、优惠活动，计算出订单的总金额、优惠金额、实付金额等，生成一个**交易快照（Trade Snapshot）**。
      * **返回给前端**: 将包含金额明细和交易令牌（`trade_token`）的交易快照返回给前端，供用户确认。`trade_token`用于防止重复提交。

2.  **阶段二：订单提交 (前端 -\> 订单网关 -\> 订单编排服务)**

      * 用户点击“提交订单”，前端携带`trade_token`和用户选择的支付方式等信息发起最终请求。
      * **订单编排服务**执行写操作：
        1.  **令牌校验**: 验证`trade_token`的有效性，防止重复提交。成功后立即销毁令牌。
        2.  **库存预扣减 (核心)**: 调用**库存服务**，使用 **Redis + Lua脚本** 原子性地扣减库存。这是应对高并发的关键，内存操作远快于DB。
              * *如果Redis库存扣减失败，立即终止流程，向上游返回“库存不足”。*
              * *这里也可以进行优惠劵预占、会员权益预占等类似库存的预占操作，这里是下单高并发的瓶颈所在！！！*
        3.  **发送半消息 (RocketMQ)**: 将包含了完整订单信息的DTO（Data Transfer Object）作为一条**事务半消息**发送到MQ。
              * \*这一步是保证分布式事务的关键，采用阿里的\*\*事务消息（TCC/SAGA的简化落地）\**方案。*
        4.  **执行本地事务**: 在半消息发送成功后，**订单编排服务**调用**订单核心服务**，将订单数据写入数据库。此时订单状态为“待支付”(`PENDING_PAYMENT`)。
        5.  **提交/回滚半消息**:
              * 如果本地事务（订单写库）成功，则**Commit** MQ中的半消息，下游服务（如履约、数据分析等）可以消费。
              * 如果本地事务失败，则**Rollback** MQ中的半消息，并调用库存服务**归还Redis中的预扣库存**。

3.  **阶段三：异步处理**

      * 订单创建成功后，立即响应用户，引导用户去支付。
      * **消费者**监听`ORDER_CREATED`消息，执行后续非核心业务，如：
          * 通知购物车服务清空已购商品。
          * 为数据仓库（Data Warehouse）提供实时数据源。
          * 启动“超时未支付自动关单”的延迟任务。

#### **3.2. 订单状态机 (Order State Machine)**

订单状态的流转必须是严谨且可追溯的。

| 状态 (State)                 | 状态码 (Code) | 描述                                       | 可流转至的状态                               |
| ---------------------------- | ------------- | ------------------------------------------ | -------------------------------------------- |
| **待支付 (Pending Payment)** | 10            | 订单已创建，等待用户支付                   | `PAID`, `CANCELED`, `CLOSED`                 |
| **已支付 (Paid)** | 20            | 用户已完成支付，等待仓库发货               | `FULFILLING`, `REFUNDING`                      |
| **处理中 (Fulfilling)** | 30            | 仓库正在打包、拣货                         | `SHIPPED`, `REFUNDING`                         |
| **已发货 (Shipped)** | 40            | 包裹已出库，正在运输                       | `DELIVERED`, `REFUNDING`                       |
| **已送达 (Delivered)** | 50            | 用户已签收                                 | `COMPLETED`                                  |
| **已完成 (Completed)** | 60            | 交易完成（超过售后期或用户确认）           | (终态)                                       |
| **已取消 (Canceled)** | 90            | 用户在支付前主动取消或系统超时关闭         | (终态)                                       |
| **退款中 (Refunding)** | 100           | 用户发起退款申请，正在处理                 | `REFUNDED`, (回到原状态如`PAID`)               |
| **已退款 (Refunded)** | 110           | 退款已完成                                 | (终态)                                       |

### **4. 数据库设计 (Database Schema)**

我们将采用\*\*“分库分表”\*\*的策略来应对未来的数据增长。初期可以不分，但设计上必须支持。分片键（Sharding Key）通常选择`user_id`或`order_id`。

#### **4.1. 核心表结构 (Core Tables)**

**注意**: 所有金额字段都应使用`BIGINT`类型，单位为“分”，以避免浮点数精度问题。所有表都包含`created_at`和`updated_at`字段。

1.  **订单主表 (order\_master)**: 存储订单的宏观信息。

    ```sql
    CREATE TABLE `order_master` (
      `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键ID',
      `order_id` BIGINT UNSIGNED NOT NULL COMMENT '订单全局唯一ID',
      `user_id` BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
      `order_status` TINYINT NOT NULL DEFAULT 10 COMMENT '订单状态',
      `total_amount` BIGINT NOT NULL COMMENT '订单总金额（分）',
      `discount_amount` BIGINT NOT NULL DEFAULT 0 COMMENT '优惠总金额（分）',
      `shipping_fee` BIGINT NOT NULL DEFAULT 0 COMMENT '运费（分）',
      `payable_amount` BIGINT NOT NULL COMMENT '应付金额（分）',
      `paid_amount` BIGINT NOT NULL DEFAULT 0 COMMENT '实付金额（分）',
      `payment_method` TINYINT COMMENT '支付方式',
      `paid_at` DATETIME COMMENT '支付时间',
      `completed_at` DATETIME COMMENT '订单完成时间',
      `cancel_reason` VARCHAR(255) COMMENT '取消原因',
      `memo` VARCHAR(500) COMMENT '用户备注',
      `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
      PRIMARY KEY (`id`),
      UNIQUE KEY `uk_order_id` (`order_id`),
      KEY `idx_user_id_status` (`user_id`, `order_status`)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='订单主表';
    ```

2.  **订单明细表 (order\_item)**: 存储订单中的具体商品项。

    ```sql
    CREATE TABLE `order_item` (
      `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
      `order_id` BIGINT UNSIGNED NOT NULL COMMENT '订单ID',
      `sku_id` BIGINT UNSIGNED NOT NULL COMMENT '商品SKU ID',
      `spu_id` BIGINT UNSIGNED NOT NULL COMMENT '商品SPU ID',
      `product_name` VARCHAR(255) NOT NULL COMMENT '商品名称（快照）',
      `product_image` VARCHAR(512) COMMENT '商品图片（快照）',
      `quantity` INT UNSIGNED NOT NULL COMMENT '购买数量',
      `original_price` BIGINT NOT NULL COMMENT '商品原价（分）',
      `deal_price` BIGINT NOT NULL COMMENT '商品成交价（分）',
      `total_item_price` BIGINT NOT NULL COMMENT '该项商品总价（分）',
      `attributes` JSON COMMENT '商品销售属性（快照），如颜色、尺寸',
      `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
      PRIMARY KEY (`id`),
      KEY `idx_order_id` (`order_id`)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='订单明细表';
    ```

      * **设计思想**: `order_item`中的商品信息是**交易快照**，不能只存一个`sku_id`。因为商品信息（如名称、价格）可能会在后续发生变化，订单中的信息必须是下单那一刻的视图。

3.  **订单收货信息表 (order\_shipping\_info)**

    ```sql
    CREATE TABLE `order_shipping_info` (
      `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
      `order_id` BIGINT UNSIGNED NOT NULL COMMENT '订单ID',
      `receiver_name` VARCHAR(100) NOT NULL COMMENT '收货人姓名',
      `receiver_phone` VARCHAR(20) NOT NULL COMMENT '收货人电话',
      `province` VARCHAR(50) NOT NULL,
      `city` VARCHAR(50) NOT NULL,
      `district` VARCHAR(50) NOT NULL,
      `detailed_address` VARCHAR(255) NOT NULL COMMENT '详细地址',
      `shipping_method` TINYINT COMMENT '配送方式',
      `tracking_number` VARCHAR(100) COMMENT '物流单号',
      `shipping_company` VARCHAR(100) COMMENT '物流公司',
      `shipped_at` DATETIME COMMENT '发货时间',
      `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
      PRIMARY KEY (`id`),
      UNIQUE KEY `uk_order_id` (`order_id`)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='订单收货信息表';
    ```

4.  **订单状态历史表 (order\_state\_history)**: 记录每一次状态变更，用于追踪和审计。

    ```sql
    CREATE TABLE `order_state_history` (
      `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
      `order_id` BIGINT UNSIGNED NOT NULL COMMENT '订单ID',
      `previous_status` TINYINT COMMENT '前一状态',
      `current_status` TINYINT NOT NULL COMMENT '当前状态',
      `operator` VARCHAR(100) NOT NULL DEFAULT 'system' COMMENT '操作人',
      `remark` VARCHAR(500) COMMENT '备注',
      `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      PRIMARY KEY (`id`),
      KEY `idx_order_id` (`order_id`)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='订单状态流转历史表';
    ```

#### **4.2. 订单拆分设计 (Order Splitting)**

对于平台型电商（如京东、天猫），一个订单可能包含多个商家的商品，需要拆分成多个子订单进行履约。

  * **引入`order_parent`和`order_sub`的概念。**
  * 用户支付是针对`order_parent`的。
  * 支付成功后，系统根据商家、仓库、商品类型等规则，将一个`order_parent`拆分成多个`order_sub`。
  * 后续的发货、退款等流程都在`order_sub`的维度上进行。

在这种设计下，`order_master`表可以演变为`order_sub`子订单表，并增加`parent_order_id`字段。再增加一个`order_parent`表来记录父订单的支付信息。

### **5. 核心代码实现 (Golang)**

下面是订单创建核心流程的伪代码和关键实现，使用`Gin`作为Web框架，`GORM`作为ORM。

#### **5.1. 项目结构 (Project Structure)**

```
/order-system
  /cmd
    /api
      main.go
  /internal
    /handler    // API层，处理HTTP请求
      order_handler.go
    /service    // 业务逻辑层
      order_service.go
    /repository // 数据访问层
      order_repo.go
      model.go    // GORM模型
    /dto        // 数据传输对象
      order_dto.go
  /pkg
    /util       // 工具类，如雪花算法生成ID
    /mq         // 消息队列封装
  /configs      // 配置文件
```

#### **5.2. 数据传输对象 (DTO)**

`internal/dto/order_dto.go`

```golang
package dto

// CreateOrderRequest 代表前端提交订单的请求体
type CreateOrderRequest struct {
	UserID         int64            `json:"user_id" binding:"required"`
	Items          []OrderItemDTO   `json:"items" binding:"required,min=1"`
	AddressID      int64            `json:"address_id" binding:"required"`
	PaymentMethod  int              `json:"payment_method"`
	Memo           string           `json:"memo"`
	TradeToken     string           `json:"trade_token" binding:"required"` // 防重令牌
}

// OrderItemDTO 代表订单中的一个商品项
type OrderItemDTO struct {
	SkuID    int64 `json:"sku_id" binding:"required"`
	Quantity int   `json:"quantity" binding:"required,gt=0"`
}

// CreateOrderResponse 是创建订单后的响应
type CreateOrderResponse struct {
	OrderID      string `json:"order_id"`
	PayableAmount int64  `json:"payable_amount"`
	// ... 其他支付所需信息
}
```

#### **5.3. 核心业务逻辑 (Service)**

`internal/service/order_service.go`

```golang
package service

import (
	"context"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"order-system/internal/dto"
	"order-system/internal/repository"
	"order-system/pkg/mq"
	"order-system/pkg/util"
)

type OrderService interface {
	CreateOrder(ctx context.Context, req *dto.CreateOrderRequest) (*dto.CreateOrderResponse, error)
}

type orderServiceImpl struct {
	repo         repository.OrderRepository
	// 依赖注入其他服务的客户端
	// productClient product.Client
	// inventoryClient inventory.Client
	// promotionClient promotion.Client
	mqProducer   mq.Producer
	idGenerator  util.IDGenerator // 雪花算法
}

func NewOrderService(repo repository.OrderRepository, producer mq.Producer) OrderService {
	return &orderServiceImpl{
		repo:         repo,
		mqProducer:   producer,
		idGenerator:  util.NewSnowflakeGenerator(),
	}
}

// CreateOrder 是订单创建的核心方法
func (s *orderServiceImpl) CreateOrder(ctx context.Context, req *dto.CreateOrderRequest) (*dto.CreateOrderResponse, error) {
	// 伪代码:
	// 1. 校验TradeToken，防重放攻击 (通常在Redis中检查和删除)
	// if !checkAndDelTradeToken(req.TradeToken) {
	//    return nil, errors.New("invalid trade token or duplicate submission")
	// }

	// 2. 编排: 并发调用其他服务进行数据获取和校验
	// address, err := userClient.GetAddress(ctx, req.AddressID)
	// itemsWithDetails, err := productClient.GetProductDetails(ctx, req.Items)
	// promotionResult, err := promotionClient.CalculatePromotions(ctx, itemsWithDetails)

	// 3. 计算最终价格
	// totalAmount, discountAmount, payableAmount := s.calculatePrice(itemsWithDetails, promotionResult)
	var payableAmount int64 = 10000 // 假设为100元

	// 4. 预扣减库存 (Redis) - 这是一个关键步骤，应该是原子操作
	// err := inventoryClient.PreDeductStock(ctx, req.Items)
	// if err != nil {
	//    return nil, err // 库存不足
	// }

	// === 分布式事务核心区域 ===
	orderID := s.idGenerator.Generate()
	orderModel := s.buildOrderModel(req, orderID, payableAmount)

	// 5. 将要写入数据库的订单数据作为消息体
	msgBody, _ := json.Marshal(orderModel)

	// 6. 使用事务消息来保证最终一致性
	err := s.mqProducer.SendMessageInTransaction(ctx, "ORDER_TOPIC", msgBody, func() error {
		// 这个函数是本地事务的执行单元
		// 7. 将订单数据写入数据库
		return s.repo.CreateOrderInTx(ctx, orderModel)
	})

	if err != nil {
		// 事务失败，需要进行补偿
		// inventoryClient.RevertPreDeductedStock(ctx, req.Items) // 归还预扣库存
		fmt.Printf("Create order failed, transaction rolled back. Error: %v\n", err)
		return nil, errors.New("failed to create order")
	}

	// 8. 成功，返回给前端支付所需信息
	return &dto.CreateOrderResponse{
		OrderID:       fmt.Sprintf("%d", orderID),
		PayableAmount: payableAmount,
	}, nil
}

func (s *orderServiceImpl) buildOrderModel(req *dto.CreateOrderRequest, orderID int64, payableAmount int64) *repository.OrderAllInOneModel {
	// ... 根据请求构建完整的订单模型，包括主表、明细表、地址表等
	// 返回一个聚合模型，方便在同一个DB事务中插入
	return &repository.OrderAllInOneModel{
		Master: &repository.OrderMaster{
			OrderID:       orderID,
			UserID:        req.UserID,
			OrderStatus:   10, // Pending Payment
			PayableAmount: payableAmount,
			// ...
		},
		Items: []*repository.OrderItem{
			// ...
		},
		Shipping: &repository.OrderShippingInfo{
			// ...
		},
	}
}
```

#### **5.4. 数据访问层 (Repository)**

`internal/repository/order_repo.go`

```golang
package repository

import (
	"context"
	"gorm.io/gorm"
)

type OrderRepository interface {
	CreateOrderInTx(ctx context.Context, orderData *OrderAllInOneModel) error
}

type orderRepositoryImpl struct {
	db *gorm.DB
}

func NewOrderRepository(db *gorm.DB) OrderRepository {
	return &orderRepositoryImpl{db: db}
}

// OrderAllInOneModel 是一个聚合模型，用于事务性写入
type OrderAllInOneModel struct {
	Master   *OrderMaster
	Items    []*OrderItem
	Shipping *OrderShippingInfo
	History  *OrderStateHistory
}

// CreateOrderInTx 在一个数据库事务中创建所有与订单相关的数据
func (r *orderRepositoryImpl) CreateOrderInTx(ctx context.Context, orderData *OrderAllInOneModel) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 插入主订单
		if err := tx.Create(orderData.Master).Error; err != nil {
			return err
		}

		// 2. 批量插入订单项
		for _, item := range orderData.Items {
			item.OrderID = orderData.Master.OrderID // 关联主订单ID
		}
		if err := tx.Create(&orderData.Items).Error; err != nil {
			return err
		}

		// 3. 插入配送信息
		orderData.Shipping.OrderID = orderData.Master.OrderID
		if err := tx.Create(orderData.Shipping).Error; err != nil {
			return err
		}

		// 4. 插入初始状态历史
		orderData.History = &OrderStateHistory{
			OrderID:       orderData.Master.OrderID,
			CurrentStatus: orderData.Master.OrderStatus,
			Operator:      "system",
			Remark:        "Order created",
		}
		if err := tx.Create(orderData.History).Error; err != nil {
			return err
		}

		// 如果所有操作都成功，事务会自动提交
		return nil
	}) // 如果任何步骤返回错误，事务会自动回滚
}
```

### **6. 总结 (Conclusion)**

这份设计文档提供了一个全面、健壮且可扩展的电商订单系统架构方案。其核心思想在于：

1.  **架构解耦**: 通过微服务划分业务边界，使得系统易于维护和扩展。
2.  **流量削峰**: 通过异步化和消息队列，将瞬时的高并发写请求平滑地分散到后端系统中处理，保证系统的稳定性。
3.  **数据一致性**: 采用成熟的分布式事务方案（如事务消息），确保在分布式环境下核心数据（订单、库存、支付）的最终一致性。
4.  **数据建模**: 采用交易快照和状态历史记录等设计，保证了数据的完整性和可追溯性。

这套架构能够支撑从初创电商到大型平台的业务需求，并通过横向扩展服务和数据库分片来应对未来的业务增长。后续的迭代可以围绕订单履约、逆向流程（退款/退货）、客服工单系统等进行深化设计。