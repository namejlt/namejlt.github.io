---
title: "短链系统设计"
date: 2025-07-10T11:20:00+08:00
draft: false
toc: true
categories: ["技术/实践/设计"]
tags: ["设计"]
---

## 短链系统设计

本文将详细设计一个高性能、高可用的域名短链系统。我们将遵循标准的软件设计流程，从需求分析到详细设计，并提供关键部分的Go伪代码实现思路。

-----

### 1\. 需求分析 (Requirements Analysis)

首先，明确功能性和非功能性需求。

#### 1.1 功能性需求 (Functional Requirements)

  * **F1: 创建短链** - 用户输入一个长URL，系统为其生成一个全局唯一的短链。
  * **F2: 短链重定向** - 用户访问一个短链，系统能准确、快速地重定向到原始的长URL。
  * **F3: (可选) 自定义短链** - 允许用户指定短链的后缀。
  * **F4: (可选) 短链管理** - 提供API用于删除、修改短链的有效期等。
  * **F5: (可选) 数据分析** - 统计每个短链的访问次数、来源等信息。

#### 1.2 非功能性需求 (Non-functional Requirements)

  * **NF1: 高性能**
      * **生成能力**: 每天支持1亿次短链生成。换算成QPS：`100,000,000 / (24 * 3600) ≈ 1157 QPS/秒` (平均值)。峰值可能达到3-5倍，因此**生成接口需支持至少 5000 QPS**。
      * **访问能力**: 短链的读取（重定向）量通常是生成量的10倍以上。我们假设为20倍，即每天20亿次访问。换算成QPS：`2,000,000,000 / (24 * 3600) ≈ 23,150 QPS/秒` (平均值)。峰值可能非常高，**读取能力需支持至少 100,000 QPS**。
      * **低延迟**: 重定向响应时间（P99）应低于50ms。生成响应时间（P99）应低于200ms。
  * **NF2: 高可用 (HA)**
      * **无单点故障**: 系统的任何组件失效都不能导致整个服务中断。
      * **多副本部署**: 应用服务、缓存、数据库都应采用集群化多副本部署。
      * **跨区域容灾**: 能够抵御单个机房或云区域的故障。
  * **NF3: 可扩展性 (Scalability)**
      * **数据持久化层**: 必须能够轻松扩容以存储海量数据（百亿甚至千亿级别）。
      * **服务层**: 应用服务可以进行无状态水平扩展。
  * **NF4: 数据一致性与持久化**
      * 数据必须被持久化，不能丢失。
      * 在分布式环境下，可接受最终一致性。
  * **NF5: 健壮性**
      * **限流**: 必须有机制防止恶意请求打垮系统。
      * **缓存保护**: 包含防止缓存穿透、缓存击穿的机制。
      * **布隆过滤器**: 用于快速过滤不存在的短链，减轻对缓存和数据库的压力。
  * **NF6: 短链长度**
      * 生成的短链必须足够短，以提高用户体验。

-----

### 2\. 概要设计 (High-Level Design)

基于以上需求，我们设计一个分层的、服务化的系统架构。

#### 2.1 架构图组件说明

  * **用户 (User)**: 通过浏览器或API与系统交互。
  * **CDN (Content Delivery Network)**: (可选但推荐) 可以缓存热门的、非个性化的重定向响应，进一步降低延迟和源站压力。但对于短链系统，其动态性较强，CDN主要起加速静态资源和分担流量的作用。
  * **负载均衡器 (Load Balancer)**: 如 Nginx, HAProxy 或云厂商的ALB/CLB。将流量分发到后端的API网关或Go应用集群。负责SSL卸载和健康检查。
  * **API网关 (API Gateway)**: (可选但推荐) 如 Kong, APISIX。统一处理入口流量，负责认证、全局限流、日志记录、路由等。
  * **短链服务集群 (Shortener Service Cluster)**:
      * 核心业务逻辑层，使用Golang编写。
      * **无状态设计**，所有实例都对等，可以任意水平扩展。
      * 部署在Kubernetes或类似容器编排平台上，实现自动伸缩和故障恢复。
  * **缓存集群 (Caching Cluster)**:
      * 使用Redis集群。
      * **缓存热点数据**：存储 `short_code -> long_URL` 的映射。绝大部分读取请求应由缓存命中。
  * **布隆过滤器 (Bloom Filter)**:
      * 用于快速判断一个 `short_code` **是否可能存在**。如果不存在，则直接返回404，避免查询缓存和数据库。
      * 可以集成在Redis中（如RedisBloom模块），或作为一个独立的服务。
  * **ID生成器 (ID Generator)**:
      * 一个独立的微服务或集成在短链服务中的模块，负责生成全局唯一的64位ID。
  * **持久化数据库集群 (DB Cluster)**:
      * 使用高可用的分布式数据库。
      * 存储所有 `short_code` 和 `long_URL` 的完整映射关系。

#### 2.2 核心流程

  * **生成流程 (Write Path)**:

    1.  用户请求 `POST /api/v1/links` 并附带长URL。
    2.  LB -\> API网关 (限流) -\> Go服务实例。
    3.  Go服务向 **ID生成器** 请求一个唯一ID (e.g., a Snowflake ID)。
    4.  将64位ID通过 **Base62编码** 转换成一个短字符串 `short_code`。
    5.  将 `<short_code, long_URL>` 存入 **数据库**。
    6.  将 `short_code` 添加到 **布隆过滤器**。
    7.  **预热缓存**：将 `<short_code, long_URL>` 写入 **Redis缓存**。
    8.  返回 `https://your.domain/{short_code}` 给用户。

  * **重定向流程 (Read Path)**:

    1.  用户访问 `https://your.domain/{short_code}`。
    2.  LB -\> Go服务实例。
    3.  Go服务从URL中提取 `short_code`。
    4.  **第一关：布隆过滤器**。查询 `short_code` 是否存在于布隆过滤器中。如果不存在，直接返回 `HTTP 404 Not Found`。
    5.  **第二关：缓存**。查询Redis缓存。
          * **缓存命中**: 直接拿到 `long_URL`，返回 `HTTP 301/302` 重定向。**这是最快的路径**。
          * **缓存未命中**: 继续下一步。
    6.  **第三关：数据库**。查询持久化数据库。
          * **数据库命中**: 找到了 `long_URL`。
              * 将 `<short_code, long_URL>` 写回Redis缓存以供下次访问。
              * 返回 `HTTP 301/302` 重定向。
          * **数据库未命中**: (布隆过滤器存在误判的可能) 说明该短链确实不存在，返回 `HTTP 404 Not Found`。

-----

### 3\. 技术选型 (Technology Selection)

| 组件 | 技术 | 选型理由 |
| :--- | :--- | :--- |
| **开发语言** | **Golang** | 天然高并发（Goroutine）、性能卓越、编译型语言、强大的标准库(`net/http`)、成熟的生态。完美契合高性能网络服务场景。 |
| **Web框架** | **Gin** / **net/http** | Gin提供了强大的路由、中间件支持，开发效率高。对于极致性能，直接使用`net/http`也可以，但Gin的性能损失极小。 |
| **ID生成算法** | **雪花算法 (Snowflake)** | 64位趋势递增的ID，天生适合分布式系统，保证全局唯一。ID中包含时间戳，便于按时间排序。 |
| **数据库** | **Cassandra / ScyllaDB** 或 **MySQL/PostgreSQL (分库分表)** | **Cassandra/ScyllaDB**: NoSQL列式存储，为大规模写入和水平扩展设计，无主架构高可用，非常适合此场景。 **MySQL/PostgreSQL + Sharding**: 如果团队对关系型数据库更熟悉，可通过分库分表方案实现扩展，例如按`short_code`的hash值进行分片。 |
| **缓存** | **Redis Cluster** | 内存数据库，读写速度极快。提供丰富的数据结构和高可用集群方案。行业标准，生态成熟。 |
| **布隆过滤器** | **RedisBloom** / 自研 | RedisBloom是Redis官方提供的插件，性能高效且易于集成。也可在Go服务内存中构建，但分布式同步复杂。 |
| **限流** | **Redis + Go Middleware** / **API Gateway** | 使用令牌桶或漏桶算法。在Go中间件中结合Redis实现分布式限流，或直接利用API网关的成熟插件。 |
| **部署与编排** | **Docker + Kubernetes** | 容器化部署标准，提供服务发现、自动伸缩(HPA)、滚动更新和自愈能力，是实现高可用的基石。 |
| **负载均衡** | **Nginx** / 云厂商LB | 成熟、高性能的反向代理和负载均衡器。 |

-----

### 4\. 详细设计 (Detailed Design)

#### 4.1 数据库Schema设计 (以Cassandra为例)

```cql
CREATE KEYSPACE shortener WITH replication = {'class': 'NetworkTopologyStrategy', 'datacenter1': '3'};

USE shortener;

CREATE TABLE links (
    short_code TEXT PRIMARY KEY, // 短码作为主键，查询性能最高
    long_url TEXT,
    created_at TIMESTAMP,
    // 可选字段
    user_id UUID,
    expires_at TIMESTAMP
);
```

  * **分区键**: `short_code`。这使得根据短码查询长链接的操作（系统的核心读操作）极为高效。

#### 4.2 ID生成与编码

**ID生成**: 使用雪花算法（Snowflake）。一个64位的`int64`数字。

  * `1 bit` (符号位) + `41 bits` (时间戳, 毫秒) + `10 bits` (机器ID) + `12 bits` (序列号)。
  * 可以使用成熟的Go库，如 `github.com/bwmarrin/snowflake`。每个Go服务实例在启动时分配一个唯一的机器ID (0-1023)。

**Base62编码**: 为了让URL更短，需要将64位的`int64`转换为更短的字符串。Base62 (`[0-9a-zA-Z]`) 是一个很好的选择，因为它不包含URL中的特殊字符(`+`, `/`)。

  * 一个`int64`最大值约为 `9 x 10^18`。
  * `62^10` 约 `8.3 x 10^17` (不够)
  * `62^11` 约 `5.2 x 10^19` (足够)
  * 这意味着一个64位的雪花ID可以被编码成一个最多11位的Base62字符串，通常情况下会更短（例如7-8位），完全满足“短”的要求。

<!-- end list -->

```go
// Base62编码伪代码
var alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func ToBase62(num int64) string {
    if num == 0 {
        return string(alphabet[0])
    }
    
    var result []byte
    base := int64(len(alphabet))
    
    for num > 0 {
        remainder := num % base
        result = append(result, alphabet[remainder])
        num = num / base
    }
    
    // Reverse result
    for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
        result[i], result[j] = result[j], result[i]
    }
    
    return string(result)
}
```

#### 4.3 核心服务Go代码逻辑 (Gin框架伪代码)

```go
package main

import (
    "context"
    "net/http"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/go-redis/redis/v8"
    "github.com/bwmarrin/snowflake"
    // "your_project/db"
    // "your_project/bloomfilter"
)

var (
    redisClient   *redis.Client
    snowflakeNode *snowflake.Node
    // dbClient      db.Client
    // bloomFilter   bloomfilter.Filter
    ctx           = context.Background()
)

// Gin中间件：分布式限流 (使用Redis)
func RateLimiter(limit int, duration time.Duration) gin.HandlerFunc {
    // ... 使用Redis的INCR和EXPIRE实现令牌桶或漏桶算法
    // 返回一个中间件函数
    return func(c *gin.Context) {
        // ... 限流逻辑
        c.Next()
    }
}

// Handler: 创建短链
func CreateShortLink(c *gin.Context) {
    var req struct {
        URL string `json:"url" binding:"required,url"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid URL"})
        return
    }

    // 1. 生成唯一ID
    id := snowflakeNode.Generate().Int64()
    
    // 2. Base62编码
    shortCode := ToBase62(id)

    // 3. 持久化到数据库
    // if err := dbClient.Save(ctx, shortCode, req.URL); err != nil {
    //     c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save link"})
    //     return
    // }

    // 4. 添加到布隆过滤器
    // bloomFilter.Add(ctx, shortCode)
    
    // 5. 写入缓存 (预热)
    err := redisClient.Set(ctx, shortCode, req.URL, 24*time.Hour).Err() // 设置一个合理的过期时间
    if err != nil {
        // 记录日志，但即使缓存失败也应返回成功，因为数据已持久化
    }
    
    c.JSON(http.StatusOK, gin.H{
        "short_link": "https://your.domain/" + shortCode,
    })
}

// Handler: 重定向
func Redirect(c *gin.Context) {
    shortCode := c.Param("shortCode")

    // 1. 检查布隆过滤器
    // if !bloomFilter.Exists(ctx, shortCode) {
    //     c.String(http.StatusNotFound, "Not Found")
    //     return
    // }

    // 2. 查缓存
    longURL, err := redisClient.Get(ctx, shortCode).Result()
    if err == nil { // 缓存命中
        c.Redirect(http.StatusFound, longURL) // 使用302 Found，便于统计。若用301则浏览器会永久缓存
        return
    }
    
    // 缓存未命中
    if err != redis.Nil {
        // Redis出错了，记录日志
        c.String(http.StatusInternalServerError, "Service Error")
        return
    }

    // 3. 查数据库 (缓存回源)
    // longURL, err = dbClient.Get(ctx, shortCode)
    // if err != nil { // 数据库也出错了或未找到
    //     c.String(http.StatusNotFound, "Not Found")
    //     return
    // }

    // 4. 回填缓存
    // redisClient.Set(ctx, shortCode, longURL, 24*time.Hour)

    // 5. 重定向
    c.Redirect(http.StatusFound, longURL)
}

func main() {
    // 初始化 Snowflake, Redis, DB, Bloom Filter ...

    r := gin.Default()
    
    // 应用限流中间件
    api := r.Group("/api/v1")
    api.Use(RateLimiter(5000, time.Second)) // 示例：每秒5000次请求
    {
        api.POST("/links", CreateShortLink)
    }

    r.GET("/:shortCode", Redirect)

    r.Run(":8080")
}
```

#### 4.4 高可用与容灾策略

  * **服务层 (Go)**: 通过Kubernetes部署，设置`Deployment`的`replicas`大于等于3，并配置`HorizontalPodAutoscaler (HPA)`根据CPU/内存负载自动扩缩容。使用`PodAntiAffinity`确保Pod分散在不同的物理节点上。
  * **缓存层 (Redis)**: 部署Redis Cluster或Sentinel模式，至少三主三从，分布在不同可用区（AZ）。
  * **数据库层 (Cassandra)**: Cassandra原生支持多数据中心复制。在Kubernetes上可以通过StatefulSet部署，配置多个机架（rack），并将这些机架映射到不同的可用区。
  * **跨区域容灾**: 在两个或多个地理区域（Region）部署全套系统（LB, Go服务, Cache, DB）。使用DNS服务商的全局负载均衡（GSLB）功能，根据延迟或健康状况将用户流量导向最近或最健康的区域。数据库层面启用跨区域异步复制。

#### 4.5 缓存策略增强

  * **缓存穿透**: 布隆过滤器已经解决了大部分问题。
  * **缓存击穿 (热点Key失效)**: 当一个热点短链的缓存失效时，大量请求会同时打到数据库。
      * **解决方案**: 使用 `singleflight` 模式。在Go中，可以使用 `golang.org/x/sync/singleflight` 包。当缓存未命中时，只允许一个请求去查询数据库并回填缓存，其他请求等待这个请求的结果。
  * **缓存雪崩**: 大量key在同一时间集体失效。
      * **解决方案**: 在设置缓存TTL时，增加一个小的随机值，例如 `ttl = 24 * time.Hour + time.Duration(rand.Intn(300)) * time.Second`，使得过期时间分散开。

-----

### 总结

这个设计方案通过以下方式满足了所有提出的要求：

  * **高性能**: Golang + Redis + 无状态水平扩展 + 高效ID生成和编码，满足了读写QPS和低延迟的要求。
  * **高可用**: 全链路多副本、多可用区部署、Kubernetes自愈和自动伸缩、数据库和缓存的集群化，避免了单点故障，实现了高SLA。
  * **可扩展性**: 无状态服务层和分布式数据库/缓存层，使得系统可以随着业务增长平滑地水平扩展。
  * **健壮性**: 引入了API网关限流、应用层分布式限流、布隆过滤器和增强的缓存策略，有效保护系统免受异常流量冲击。
  * **短链够短**: 雪花算法 + Base62编码确保了在海量数据下，短链依然保持较短的长度。

其他考虑：短链的随机性保证，这块通常需要在id生成和短码生成上做优化

这套架构是一个经过业界验证的、成熟可靠的设计，能够支撑起一个大规模、高并发的短链服务。在实际实施中，还需要关注监控、告警、日志、安全等运维层面的建设。