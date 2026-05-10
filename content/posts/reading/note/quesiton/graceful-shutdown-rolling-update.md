---
title: "优雅上下线与平滑发布如何设计"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["平滑发布", "优雅关闭", "golang"]
---

## 问题

微服务发布新版本时，如何保证正在处理的请求不被中断？优雅关闭和平滑上线如何实现？K8s环境下如何配置探针和生命周期钩子？

## 回答

服务上下线看似简单，但在生产环境中，一次不当的发布可能导致请求失败、数据丢失。优雅上下线是保证服务可用性的最后一道防线。

### 一、优雅关闭

优雅关闭的核心是"处理完已有请求再退出"。

```go
package graceful

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type Server struct {
	httpServer *http.Server
	wg         sync.WaitGroup
	shutdownCh chan struct{}
}

func NewServer(addr string, handler http.Handler) *Server {
	return &Server{
		httpServer: &http.Server{Addr: addr, Handler: handler},
		shutdownCh: make(chan struct{}),
	}
}

func (s *Server) Start() error {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	log.Printf("Server started on %s", s.httpServer.Addr)
	return nil
}

func (s *Server) WaitForShutdown(timeout time.Duration) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	sig := <-quit
	log.Printf("Received signal %v, starting graceful shutdown...", sig)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("Server shutdown gracefully")
	case <-ctx.Done():
		log.Println("Shutdown timeout, forcing exit")
	}
}
```

### 二、连接排空

```go
type DrainingServer struct {
	inner     http.Handler
	draining  int32
	activeReq int64
}

func NewDrainingServer(inner http.Handler) *DrainingServer {
	return &DrainingServer{inner: inner}
}

func (s *DrainingServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if atomic.LoadInt32(&s.draining) == 1 {
		w.Header().Set("Connection", "close")
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("server is shutting down"))
		return
	}

	atomic.AddInt64(&s.activeReq, 1)
	defer atomic.AddInt64(&s.activeReq, -1)

	s.inner.ServeHTTP(w, r)
}

func (s *DrainingServer) StartDraining() {
	atomic.StoreInt32(&s.draining, 1)
}

func (s *DrainingServer) ActiveRequests() int64 {
	return atomic.LoadInt64(&s.activeReq)
}

func (s *DrainingServer) WaitForDrain(timeout time.Duration) {
	s.StartDraining()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.ActiveRequests() == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
```

### 三、平滑上线

新实例启动后不能立即接收全量流量，需要预热。

```go
type WarmupHandler struct {
	inner       http.Handler
	startTime   time.Time
	warmupDuration time.Duration
}

func NewWarmupHandler(inner http.Handler, warmupDuration time.Duration) *WarmupHandler {
	return &WarmupHandler{
		inner:          inner,
		startTime:      time.Now(),
		warmupDuration: warmupDuration,
	}
}

func (h *WarmupHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	elapsed := time.Since(h.startTime)
	if elapsed < h.warmupDuration {
		if shouldReject(elapsed) {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("service warming up"))
			return
		}
	}

	h.inner.ServeHTTP(w, r)
}

func shouldReject(elapsed time.Duration) bool {
	warmupProgress := float64(elapsed) / float64(warmupDuration)
	return rand.Float64() > warmupProgress
}
```

### 四、K8s探针配置

```go
func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	if !isReady() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("not ready"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ready"))
}

var ready bool

func isReady() bool {
	return ready
}

func SetReady(r bool) {
	ready = r
}
```

**K8s配置**：

```yaml
spec:
  containers:
  - name: app
    lifecycle:
      preStop:
        exec:
          command: ["/bin/sh", "-c", "sleep 10"]
    livenessProbe:
      httpGet:
        path: /healthz
        port: 8080
      initialDelaySeconds: 10
      periodSeconds: 10
    readinessProbe:
      httpGet:
        path: /ready
        port: 8080
      initialDelaySeconds: 5
      periodSeconds: 5
  terminationGracePeriodSeconds: 60
```

**关键配置说明**：
- `preStop sleep 10`：给K8s时间从Service中摘除Pod，避免新请求路由到即将关闭的Pod
- `terminationGracePeriodSeconds: 60`：等待最多60秒让在途请求完成
- `readinessProbe`：就绪后才接收流量

### 五、发布策略

| 策略 | 原理 | 优点 | 缺点 |
|------|------|------|------|
| 滚动发布 | 逐个替换旧实例 | 零停机 | 发布慢 |
| 蓝绿发布 | 新旧两套环境切换 | 回滚快 | 资源翻倍 |
| 金丝雀发布 | 先发布少量实例验证 | 风险小 | 需要流量控制 |

### 六、总结

优雅上下线的核心是"不丢请求"：

1. **优雅关闭**：先停止接收新请求，等待在途请求完成
2. **连接排空**：标记为draining状态，新请求返回503
3. **平滑上线**：预热期间逐步放量
4. **K8s探针**：liveness保活、readiness控制流量
5. **preStop钩子**：给K8s时间摘除Pod

**行业实践**：K8s Rolling Update、Istio流量管理、Argo Rollouts
