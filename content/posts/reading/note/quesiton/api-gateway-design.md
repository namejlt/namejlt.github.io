---
title: "API网关如何设计与实现"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["API网关", "微服务", "golang"]
---

## 问题

微服务架构中，API网关是所有外部请求的入口。如何设计一个支持路由转发、负载均衡、限流熔断、认证鉴权的API网关？各中间件的执行顺序如何设计？

## 回答

API网关是微服务架构的"前门"，承担着路由、安全、流量控制等核心职责。一个好的网关设计需要在性能、可扩展性和功能丰富度之间取得平衡。

### 一、API网关的核心职责

```
客户端 → API网关 → 微服务A
              ↓     → 微服务B
         [路由匹配]  → 微服务C
         [认证鉴权]
         [限流熔断]
         [负载均衡]
         [日志监控]
         [协议转换]
```

### 二、中间件链设计

```go
package gateway

import (
	"context"
	"net/http"
)

type Context struct {
	Request  *http.Request
	Writer   http.ResponseWriter
	Params   map[string]string
	Abort    bool
	Status   int
	Metadata map[string]interface{}
}

type Middleware func(*Context) error

type MiddlewareChain struct {
	middlewares []Middleware
}

func NewMiddlewareChain(middlewares ...Middleware) *MiddlewareChain {
	return &MiddlewareChain{middlewares: middlewares}
}

func (c *MiddlewareChain) Then(final Handler) Handler {
	return func(ctx *Context) error {
		return c.execute(ctx, 0, final)
	}
}

func (c *MiddlewareChain) execute(ctx *Context, index int, final Handler) error {
	if ctx.Abort {
		return nil
	}

	if index < len(c.middlewares) {
		mw := c.middlewares[index]
		return mw(ctx)
	}

	return final(ctx)
}

type Handler func(*Context) error
```

### 三、路由引擎

```go
package gateway

import (
	"net/http"
	"strings"
	"sync"
)

type Route struct {
	Method      string
	Path        string
	Handler     Handler
	Middlewares []Middleware
}

type Router struct {
	mu     sync.RWMutex
	routes []*Route
	trees  map[string]*node
}

type node struct {
	path     string
	children map[string]*node
	handler  Handler
	isWild   bool
}

func NewRouter() *Router {
	return &Router{
		trees: make(map[string]*node),
	}
}

func (r *Router) AddRoute(method, path string, handler Handler, middlewares ...Middleware) {
	r.mu.Lock()
	defer r.mu.Unlock()

	route := &Route{
		Method:      method,
		Path:        path,
		Handler:     handler,
		Middlewares: middlewares,
	}
	r.routes = append(r.routes, route)

	if r.trees[method] == nil {
		r.trees[method] = &node{children: make(map[string]*node)}
	}

	segments := splitPath(path)
	current := r.trees[method]

	for _, seg := range segments {
		isWild := strings.HasPrefix(seg, ":")
		child, ok := current.children[seg]
		if !ok {
			child = &node{
				path:     seg,
				children: make(map[string]*node),
				isWild:   isWild,
			}
			current.children[seg] = child
		}
		current = child
	}

	current.handler = handler
}

func (r *Router) Match(method, path string) (Handler, map[string]string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	root, ok := r.trees[method]
	if !ok {
		return nil, nil, false
	}

	segments := splitPath(path)
	params := make(map[string]string)
	current := root

	for _, seg := range segments {
		if child, ok := current.children[seg]; ok {
			current = child
		} else {
			found := false
			for key, child := range current.children {
				if child.isWild {
					params[strings.TrimPrefix(key, ":")] = seg
					current = child
					found = true
					break
				}
			}
			if !found {
				return nil, nil, false
			}
		}
	}

	if current.handler != nil {
		return current.handler, params, true
	}
	return nil, nil, false
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}
```

### 四、核心中间件实现

#### 认证鉴权

```go
func AuthMiddleware(secret string) Middleware {
	return func(ctx *Context) error {
		token := ctx.Request.Header.Get("Authorization")
		if token == "" {
			ctx.Abort = true
			ctx.Status = http.StatusUnauthorized
			ctx.Writer.WriteHeader(http.StatusUnauthorized)
			ctx.Writer.Write([]byte(`{"error":"missing authorization token"}`))
			return nil
		}

		token = strings.TrimPrefix(token, "Bearer ")
		claims, err := validateJWT(token, secret)
		if err != nil {
			ctx.Abort = true
			ctx.Status = http.StatusUnauthorized
			ctx.Writer.WriteHeader(http.StatusUnauthorized)
			ctx.Writer.Write([]byte(`{"error":"invalid token"}`))
			return nil
		}

		ctx.Metadata["user"] = claims
		return nil
	}
}
```

#### 限流

```go
func RateLimitMiddleware(limiter RateLimiter) Middleware {
	return func(ctx *Context) error {
		key := getClientIP(ctx.Request)
		if !limiter.Allow(key) {
			ctx.Abort = true
			ctx.Status = http.StatusTooManyRequests
			ctx.Writer.WriteHeader(http.StatusTooManyRequests)
			ctx.Writer.Write([]byte(`{"error":"rate limit exceeded"}`))
			return nil
		}
		return nil
	}
}
```

#### 请求日志

```go
func LoggingMiddleware(logger Logger) Middleware {
	return func(ctx *Context) error {
		start := time.Now()

		err := ctx.Next()

		duration := time.Since(start)
		logger.Info("request",
			"method", ctx.Request.Method,
			"path", ctx.Request.URL.Path,
			"status", ctx.Status,
			"duration", duration.String(),
			"ip", getClientIP(ctx.Request),
		)

		return err
	}
}
```

#### 熔断

```go
func CircuitBreakerMiddleware(cb *CircuitBreaker) Middleware {
	return func(ctx *Context) error {
		if cb.State() == StateOpen {
			ctx.Abort = true
			ctx.Status = http.StatusServiceUnavailable
			ctx.Writer.WriteHeader(http.StatusServiceUnavailable)
			ctx.Writer.Write([]byte(`{"error":"service unavailable"}`))
			return nil
		}
		return nil
	}
}
```

### 五、反向代理与负载均衡

```go
package gateway

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
)

type LoadBalancer interface {
	Next() *url.URL
}

type RoundRobinLB struct {
	endpoints []*url.URL
	current   uint64
}

func NewRoundRobinLB(endpoints []string) (*RoundRobinLB, error) {
	urls := make([]*url.URL, len(endpoints))
	for i, ep := range endpoints {
		u, err := url.Parse(ep)
		if err != nil {
			return nil, err
		}
		urls[i] = u
	}
	return &RoundRobinLB{endpoints: urls}, nil
}

func (lb *RoundRobinLB) Next() *url.URL {
	n := atomic.AddUint64(&lb.current, 1)
	return lb.endpoints[(n-1)%uint64(len(lb.endpoints))]
}

type ProxyHandler struct {
	lb LoadBalancer
}

func NewProxyHandler(lb LoadBalancer) *ProxyHandler {
	return &ProxyHandler{lb: lb}
}

func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	target := p.lb.Next()

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":"upstream unavailable"}`))
	}

	r.URL.Host = target.Host
	r.URL.Scheme = target.Scheme
	r.Header.Set("X-Forwarded-Host", r.Header.Get("Host"))
	r.Header.Set("X-Forwarded-For", getClientIP(r))

	proxy.ServeHTTP(w, r)
}
```

### 六、网关架构总结

```
请求 → [CORS] → [日志] → [认证] → [限流] → [熔断] → [路由匹配] → [负载均衡] → [反向代理] → 后端服务
```

**中间件执行顺序原则**：
1. 横切关注点在前（日志、CORS）
2. 安全类在前（认证、限流）
3. 保护类在中（熔断、降级）
4. 业务类在后（路由、代理）

### 七、总结

API网关是微服务的统一入口，核心设计要点：

1. **中间件链**：可插拔、可排序的中间件架构
2. **路由引擎**：高性能的路径匹配，支持路径参数
3. **负载均衡**：多种策略（轮询、加权、一致性哈希）
4. **安全防护**：认证、限流、熔断多层保护
5. **可观测性**：请求日志、指标采集、链路追踪

**行业实践**：Kong、Nginx、APISIX、Envoy、Traefik
