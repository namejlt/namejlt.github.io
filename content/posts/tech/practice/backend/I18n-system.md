---
title: "通用多语言（i18n）服务设计"
date: 2025-07-16T10:00:00+08:00
draft: false
toc: true
featured: true
categories: ["技术/实践/后端"]
tags: ["系统设计"]
---

## 需求

面向国际市场，IT系统需提供多语言服务，一个通用的多语言服务设计比较重要。

这套设计旨在实现**集中化管理**、**高性能**、**高可用**和**易于接入**的目标，并提供一个生产可用的 Golang SDK 详细示例。

-----

### **摘要 (Executive Summary)**

本设计方案提出一个独立部署的**中央多语言服务 (I18n Center)**。它统一负责所有业务系统、所有模块的文案（Key-Value）存储和翻译管理。业务系统通过集成本方案提供的 **SDK**，在运行时从 I18n Center 拉取最新的多语言文案，并缓存在本地。SDK 会自动处理请求上下文中的语言标识，无感地为业务逻辑提供正确的翻译文本，从而实现业务代码与多语言文案的彻底解耦。

-----

### **核心设计原则**

1.  **集中化管理 (Centralization)**: 所有多语言文案在一个地方维护，避免散落在各个项目的配置文件中，确保一致性和可维护性。
2.  **高性能 (High Performance)**: 客户端（业务系统）通过 SDK 内置的**多级缓存**（内存 + 可能的持久化缓存）直接获取文案，网络请求仅用于定期同步，对正常业务请求的性能影响降至最低。
3.  **高可用 (High Availability)**: 即使中央服务宕机，由于客户端有缓存，业务系统依然可以正常提供多语言功能，具备强大的容错能力。
4.  **语言与业务解耦 (Decoupling)**: 业务代码中只使用抽象的 `Key`，无需关心具体语言的翻译。翻译人员或产品经理可以直接在管理后台修改文案，无需开发人员介入和重新部署。
5.  **易于集成与使用 (Ease of Use)**: 提供轻量级、无依赖或少依赖的 SDK，几行代码即可完成接入。
6.  **可观测性 (Observability)**: 服务和 SDK 都应提供必要的日志和监控指标（如：缓存命中率、同步成功/失败次数）。

-----

### **系统架构**

系统主要由以下四个部分组成：

1.  **数据存储 (Data Store)**

      * **职责**: 持久化存储所有多语言 KV 数据。
      * **技术选型**:
          * **首选: PostgreSQL**。利用其强大的 `JSONB` 类型，可以非常高效地存储一个 Key 对应的所有语言翻译，查询性能优秀。
          * **备选: MySQL** (使用 `JSON` 类型) 或 **MongoDB** (文档型数据库天然契合)。

2.  **多语言中心服务 (I18n Center)**

      * **职责**: 提供 API 接口，供 SDK 拉取数据和供管理后台进行数据管理。
      * **技术选型**: **Golang**。编译后为单一二进制文件，部署简单，性能高，并发能力强，非常适合做中间件服务。

3.  **管理后台 (Admin Panel)**

      * **职责**: 提供一个 Web UI，供非技术人员（如 PM、运营、翻译）管理多语言文案。功能包括：按系统/模块（命名空间）增删改查 Key、为 Key 添加/修改不同语言的翻译、发布/版本控制、导入/导出等。
      * **技术选型**: **React/Vue** + **Go** (提供后端 API)。

4.  **客户端 SDK (Client SDK)**

      * **职责**: 集成在各个业务系统中，负责：
          * 初始化时从 I18n Center 拉取全量或增量数据。
          * 将数据缓存在业务系统本地内存中。
          * 启动后台 Goroutine 定期与 I18n Center 同步数据。
          * 提供简洁的 API (`Get`, `T`, `Translate`) 供业务代码调用。
          * 自动从 HTTP 请求头 (`Accept-Language`) 或 RPC Context 中解析语言标识。

-----

### **详细设计**

#### **1. 数据模型 (Data Model)**

我们使用 PostgreSQL 的 `JSONB` 类型来设计核心表。

**表名**: `translations`

| 字段名 | 类型 | 描述 | 示例 |
| :--- | :--- | :--- | :--- |
| `id` | `BIGSERIAL` | 主键，自增 ID | `1001` |
| `namespace` | `VARCHAR(128)` | **命名空间**。用于隔离不同系统或模块，防止 Key 冲突。复合主键之一。| `user_service` |
| `key` | `VARCHAR(256)` | **文案键**。在同一 `namespace` 下唯一。复合主键之一。| `profile.greeting` |
| `translations` | `JSONB` | **翻译内容**。一个 JSON 对象，Key 是语言代码 (IETF BCP 47)，Value 是翻译。| `{"en-US": "Hello, {name}!", "zh-CN": "你好, {name}!"}` |
| `description`| `TEXT` | **描述**。方便翻译人员理解 Key 的使用场景。| `用户个人页面的欢迎语，{name}是占位符` |
| `created_at` | `TIMESTAMPTZ` | 创建时间 | `2025-07-16 00:00:00Z` |
| `updated_at` | `TIMESTAMPTZ` | 最后更新时间 | `2025-07-16 00:00:00Z` |

**索引**:

  * `PRIMARY KEY (namespace, key)`: 确保 Key 的唯一性并加速查找。
  * `GIN index on translations`: 如果需要根据翻译内容反向搜索，可以创建 GIN 索引。

#### **2. API 接口设计 (I18n Center)**

使用 RESTful API 风格。所有响应都应是 JSON 格式。

**核心接口：获取指定命名空间的所有翻译**

这是 SDK 拉取数据的主要接口。一次性拉取一个命名空间的所有数据可以显著减少网络请求次数。

  * **Endpoint**: `GET /api/v1/translations/{namespace}`

  * **描述**: 获取一个命名空间下的所有 Key 及其所有语言的翻译。

  * **URL 参数**:

      * `namespace`: `string`, 必填。

  * **成功响应 (200 OK)**:

    ```json
    {
      "code": 0,
      "message": "success",
      "data": {
        "namespace": "user_service",
        "translations": {
          "profile.greeting": {
            "en-US": "Hello, {name}!",
            "zh-CN": "你好, {name}!",
            "ja-JP": "こんにちは、{name}さん！"
          },
          "button.submit": {
            "en-US": "Submit",
            "zh-CN": "提交"
          }
        },
        "version": "e8a3f5c7" // 数据版本号，可以是内容的 hash 或时间戳
      }
    }
    ```

      * **为什么要返回 `version`?** SDK 可以缓存这个版本号。在下一次同步时，通过 `If-None-Match` 请求头带上这个版本号。如果服务端数据没有变化，可以直接返回 `304 Not Modified`，节省带宽。

  * **失败响应 (404 Not Found)**:

    ```json
    {
      "code": 40401,
      "message": "namespace not found"
    }
    ```

#### **3. 缓存策略 (Caching Strategy)**

SDK 的缓存是高性能的关键。

1.  **内存缓存**: SDK 将从 API 获取的数据构建成一个 `map[string]map[string]string` 结构（`map[key]map[lang]value`）并存储在内存中。使用 `sync.RWMutex` 保证并发安全。
2.  **后台同步**: SDK 初始化后，会启动一个独立的 Goroutine，使用 `time.Ticker` 定期（例如，每 5 分钟）调用 I18n Center 的 API。
3.  **原子替换**: 当后台 Goroutine 获取到新数据后，它会构建一个新的 map。为了避免在更新缓存时出现长时间的写锁定，它会直接用新的 map **原子地替换**掉旧的 map 的指针 (`atomic.Value` 或 指针赋值配合锁)。这保证了读操作（`Get` 方法）几乎不受更新过程的影响。
4.  **启动时缓存**: 为了应对业务系统重启后 I18n Center 恰好不可用的情况，SDK 可以在第一次成功拉取数据后，将数据以文件形式（如 gob/json）缓存到本地磁盘。应用重启时，如果无法从远端服务拉取，则优先加载本地磁盘缓存，进一步提升可用性。

-----

### **Golang SDK 详细实现**

下面是一个生产可用的 Go SDK 包的详细代码。

#### **项目结构**

```
i18n-sdk-go/
├── client.go       # SDK 核心客户端
├── cache.go        # 缓存实现
├── updater.go      # 后台数据同步器
├── options.go      # 配置选项
└── go.mod
```

#### **`options.go`**

```go
package i18nsdk

import "time"

// Option 是用于配置 Client 的函数类型
type Option func(*Options)

// Options 存储了 Client 的所有配置
type Options struct {
	Endpoint        string        // I18n Center 的 API 地址
	Namespace       string        // 当前服务所需的命名空间
	UpdateInterval  time.Duration // 后台同步数据的周期
	DefaultLanguage string        // 默认语言
	EnableFileCache bool          // 是否启用文件缓存
	FileCachePath   string        // 文件缓存路径
}

// 默认配置
func defaultOptions() Options {
	return Options{
		UpdateInterval:  5 * time.Minute,
		DefaultLanguage: "en-US",
		EnableFileCache: false,
		FileCachePath:   "./i18n_cache.gob",
	}
}

func WithEndpoint(endpoint string) Option {
	return func(o *Options) {
		o.Endpoint = endpoint
	}
}

func WithNamespace(ns string) Option {
	return func(o *Options) {
		o.Namespace = ns
	}
}

func WithUpdateInterval(d time.Duration) Option {
	return func(o *Options) {
		o.UpdateInterval = d
	}
}

func WithDefaultLanguage(lang string) Option {
	return func(o *Options) {
		o.DefaultLanguage = lang
	}
}

func WithFileCache(path string) Option {
	return func(o *Options) {
		o.EnableFileCache = true
		o.FileCachePath = path
	}
}
```

#### **`cache.go`**

```go
package i18nsdk

import (
	"encoding/gob"
	"os"
	"sync"
)

// translationMap 定义了缓存的数据结构: map[key]map[lang]value
type translationMap map[string]map[string]string

// cache 是线程安全的内存缓存
type cache struct {
	mu   sync.RWMutex
	data translationMap
}

func newCache() *cache {
	return &cache{
		data: make(translationMap),
	}
}

// get 从缓存中获取一个翻译
func (c *cache) get(key, lang string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if langMap, ok := c.data[key]; ok {
		if value, ok := langMap[lang]; ok {
			return value, true
		}
	}
	return "", false
}

// update 使用新数据原子地替换整个缓存
func (c *cache) update(newData translationMap) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = newData
}

// saveToFile 将缓存数据序列化到文件
func (c *cache) saveToFile(path string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := gob.NewEncoder(file)
	return encoder.Encode(c.data)
}

// loadFromFile 从文件加载缓存
func (c *cache) loadFromFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err // 文件不存在是正常情况
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	var data translationMap
	if err := decoder.Decode(&data); err != nil {
		return err
	}

	c.update(data) // 加载数据到内存
	return nil
}
```

#### **`updater.go`**

```go
package i18nsdk

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// apiResponse 是从 I18n Center 获取的数据结构
type apiResponse struct {
	Data struct {
		Translations translationMap `json:"translations"`
		Version      string         `json:"version"`
	} `json:"data"`
}

// updater 负责定期从远端同步数据
type updater struct {
	opts    Options
	cache   *cache
	version string
	client  *http.Client
	ticker  *time.Ticker
	closeCh chan struct{}
}

func newUpdater(opts Options, cache *cache) *updater {
	return &updater{
		opts:    opts,
		cache:   cache,
		version: "",
		client: &http.Client{Timeout: 10 * time.Second},
		closeCh: make(chan struct{}),
	}
}

// start 启动后台同步任务
func (u *updater) start() {
	// 立即执行一次，以尽快获得初始数据
	if err := u.fetch(); err != nil {
		log.Printf("[i18n-sdk] initial fetch failed: %v. Trying to load from file cache.", err)
		if u.opts.EnableFileCache {
			if err := u.cache.loadFromFile(u.opts.FileCachePath); err != nil {
				log.Printf("[i18n-sdk] failed to load file cache: %v", err)
			} else {
				log.Printf("[i18n-sdk] successfully loaded from file cache: %s", u.opts.FileCachePath)
			}
		}
	}

	u.ticker = time.NewTicker(u.opts.UpdateInterval)
	go func() {
		for {
			select {
			case <-u.ticker.C:
				if err := u.fetch(); err != nil {
					log.Printf("[i18n-sdk] background update failed: %v", err)
				}
			case <-u.closeCh:
				u.ticker.Stop()
				return
			}
		}
	}()
}

// stop 停止同步任务
func (u *updater) stop() {
	close(u.closeCh)
}

// fetch 执行一次数据拉取和更新
func (u *updater) fetch() error {
	url := fmt.Sprintf("%s/api/v1/translations/%s", u.opts.Endpoint, u.opts.Namespace)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 使用 ETag/If-None-Match 机制进行缓存控制
	if u.version != "" {
		req.Header.Set("If-None-Match", u.version)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		log.Println("[i18n-sdk] data not modified, skipping update.")
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("received non-200 status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return fmt.Errorf("failed to unmarshal json response: %w", err)
	}

	// 更新内存缓存
	u.cache.update(apiResp.Data.Translations)
	// 更新版本号
	u.version = apiResp.Data.Version
	if u.version == "" { // 如果 API 未提供 version，则使用 ETag
		u.version = resp.Header.Get("ETag")
	}

	log.Printf("[i18n-sdk] successfully updated translations for namespace '%s', new version: %s", u.opts.Namespace, u.version)

	// 如果启用了文件缓存，则写入文件
	if u.opts.EnableFileCache {
		if err := u.cache.saveToFile(u.opts.FileCachePath); err != nil {
			log.Printf("[i18n-sdk] failed to save cache to file: %v", err)
		}
	}

	return nil
}

```

#### **`client.go`**

```go
package i18nsdk

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// langCtxKey 是用于在 context.Context 中传递语言标识的 key
type langCtxKey struct{}

// Client 是 SDK 的主客户端
type Client struct {
	opts    Options
	cache   *cache
	updater *updater
}

// NewClient 创建并初始化一个新的 I18n 客户端
// 这是 SDK 的入口点
func NewClient(opts ...Option) (*Client, error) {
	options := defaultOptions()
	for _, o := range opts {
		o(&options)
	}

	if options.Endpoint == "" || options.Namespace == "" {
		return nil, fmt.Errorf("endpoint and namespace are required")
	}

	c := &Client{
		opts:  options,
		cache: newCache(),
	}
	c.updater = newUpdater(c.opts, c.cache)
	c.updater.start()

	log.Println("[i18n-sdk] client initialized successfully.")
	return c, nil
}

// T 是 Translate 的简写形式，用于获取翻译
// 它会自动从 context 中解析语言，如果失败则使用默认语言
func (c *Client) T(ctx context.Context, key string) string {
	return c.Translate(ctx, key, nil)
}

// Translate 是获取翻译的核心方法
// key: 要翻译的文案键
// data: 用于格式化字符串的占位符数据，例如：map[string]interface{}{"name": "Bob"}
func (c *Client) Translate(ctx context.Context, key string, data map[string]interface{}) string {
	lang := c.getLangFromContext(ctx)

	// 从缓存中查找翻译
	format, ok := c.cache.get(key, lang)
	if !ok {
		// 如果指定语言找不到，尝试回退到默认语言
		if lang != c.opts.DefaultLanguage {
			format, ok = c.cache.get(key, c.opts.DefaultLanguage)
		}
		// 如果仍然找不到，返回原始的 key，便于问题排查
		if !ok {
			return key
		}
	}

	// 替换占位符
	return replacePlaceholders(format, data)
}

// Close 平滑地关闭 SDK 客户端，停止后台任务
func (c *Client) Close() {
	c.updater.stop()
	log.Println("[i18n-sdk] client closed.")
}

// getLangFromContext 从 context 中提取语言信息
func (c *Client) getLangFromContext(ctx context.Context) string {
	if lang, ok := ctx.Value(langCtxKey{}).(string); ok && lang != "" {
		return lang
	}
	return c.opts.DefaultLanguage
}

// WithLang 将语言标识存入 context，用于下游的 Translate 调用
// 通常在 HTTP Middleware 或 RPC Interceptor 中调用
func WithLang(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, langCtxKey{}, lang)
}

// replacePlaceholders 替换形如 {key} 的占位符
func replacePlaceholders(format string, data map[string]interface{}) string {
	if len(data) == 0 {
		return format
	}
	for k, v := range data {
		placeholder := fmt.Sprintf("{%s}", k)
		format = strings.ReplaceAll(format, placeholder, fmt.Sprintf("%v", v))
	}
	return format
}
```

### **如何在业务系统中使用 SDK**

1.  **初始化 (在 `main.go` 或服务启动时)**

    ```go
    import "your-repo/i18n-sdk-go"

    var i18nClient *i18nsdk.Client

    func main() {
        var err error
        i18nClient, err = i18nsdk.NewClient(
            i18nsdk.WithEndpoint("http://your-i18n-center.com"),
            i18nsdk.WithNamespace("my_awesome_app"),
            i18nsdk.WithUpdateInterval(10*time.Minute),
            i18nsdk.WithDefaultLanguage("zh-CN"),
            i18nsdk.WithFileCache("/var/data/i18n_cache.gob"), // 推荐在生产环境启用
        )
        if err != nil {
            log.Fatalf("failed to init i18n client: %v", err)
        }
        defer i18nClient.Close()
        
        // ... 启动你的 web 服务 ...
    }
    ```

2.  **创建中间件 (Middleware) 来注入语言**

    对于 HTTP 服务（如 Gin），创建一个中间件来解析 `Accept-Language` 头，并将语言注入到 `context` 中。

    ```go
    import "github.com/gin-gonic/gin"

    func I18nMiddleware(client *i18nsdk.Client) gin.HandlerFunc {
        return func(c *gin.Context) {
            lang := c.GetHeader("Accept-Language")
            // 这里可以添加更复杂的逻辑来解析 Accept-Language，例如：
            // zh-CN,zh;q=0.9,en;q=0.8
            // 并选择最匹配的一个
            if lang == "" {
                lang = "zh-CN" // fallback to a default
            } else {
                // 简化处理，只取第一个
                parts := strings.Split(lang, ",")
                lang = strings.TrimSpace(parts[0])
            }
            
            // 将语言注入 context
            ctxWithLang := i18nsdk.WithLang(c.Request.Context(), lang)
            c.Request = c.Request.WithContext(ctxWithLang)
            
            c.Next()
        }
    }

    // 注册中间件
    // r := gin.Default()
    // r.Use(I18nMiddleware(i18nClient))
    ```

3.  **在业务逻辑中使用**

    ```go
    func GetUserProfile(c *gin.Context) {
        // ...
        
        // 直接使用，无需关心当前是什么语言
        greeting := i18nClient.T(c.Request.Context(), "profile.greeting")
        // -> 如果语言是 zh-CN, greeting = "你好"
        // -> 如果语言是 en-US, greeting = "Hello"

        // 使用占位符
        welcomeMsg := i18nClient.Translate(c.Request.Context(), "welcome.message", map[string]interface{}{
            "username": "张三",
        })
        
        c.JSON(http.StatusOK, gin.H{
            "greeting":    greeting,
            "welcome_msg": welcomeMsg,
        })
    }
    ```

### **部署与运维**

1.  **I18n Center 部署**: 使用 Docker + Kubernetes 进行部署，轻松实现水平扩展和高可用。
2.  **数据库**: 使用云厂商提供的托管数据库服务（如 AWS RDS, Google Cloud SQL），简化运维。
3.  **CI/CD (文案更新流程)**:
      * **推荐流程**: 将文案的源文件（如 YAML 或 JSON）存储在 Git 仓库中。
      * 当有文案变更时，翻译人员或 PM 提交 Merge Request。
      * MR 合并后，触发 CI/CD 流水线，执行一个脚本，该脚本解析 Git 中的文案文件，并通过调用 I18n Center 的内部管理 API 将变更同步到数据库中。
      * 这个流程为所有文案变更提供了**版本控制**和**审计追踪**。

这个设计方案兼顾了性能、可用性和可维护性，能够作为一个稳定可靠的基础设施，支撑公司内所有业务的多语言需求。