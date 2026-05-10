---
title: "服务注册与发现机制如何设计"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["服务注册", "服务发现", "golang"]
---

## 问题

微服务架构中，服务实例动态变化，调用方如何知道目标服务的地址？服务注册与发现的核心原理是什么？基于etcd和Consul的方案各有什么优劣？如何实现健康检查和故障自动摘除？

## 回答

服务注册与发现是微服务架构的基础设施，解决了"服务实例动态变化时如何找到对方"的问题。没有服务发现，每个服务都需要硬编码其他服务的地址，这在动态扩缩容的环境中是不可行的。

### 一、核心概念

```
服务注册：服务启动时将自己的地址信息注册到注册中心
服务发现：调用方从注册中心获取目标服务的实例列表
健康检查：注册中心定期检查服务实例是否存活
故障摘除：不健康的实例自动从服务列表中移除
```

### 二、两种发现模式

| 模式 | 原理 | 优点 | 缺点 |
|------|------|------|------|
| 客户端发现 | 调用方直接查询注册中心 | 简单直接、无额外网络跳转 | 客户端需集成SDK |
| 服务端发现 | 通过负载均衡器查询注册中心 | 客户端无感知 | 多一跳、LB是单点 |

### 三、基于etcd的实现

```go
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type ServiceInstance struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	Port      int    `json:"port"`
	Metadata  map[string]string `json:"metadata"`
	Healthy   bool   `json:"healthy"`
}

type EtcdRegistry struct {
	client    *clientv3.Client
	prefix    string
	ttl       int64
	leaseID   clientv3.LeaseID
	cancelFn  context.CancelFunc
}

func NewEtcdRegistry(endpoints []string, prefix string, ttl int64) (*EtcdRegistry, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	return &EtcdRegistry{
		client: cli,
		prefix: prefix,
		ttl:    ttl,
	}, nil
}

func (r *EtcdRegistry) Register(ctx context.Context, instance *ServiceInstance) error {
	resp, err := r.client.Grant(ctx, r.ttl)
	if err != nil {
		return fmt.Errorf("failed to create lease: %w", err)
	}
	r.leaseID = resp.ID

	key := fmt.Sprintf("%s/%s/%s", r.prefix, instance.Name, instance.ID)
	value, _ := json.Marshal(instance)

	_, err = r.client.Put(ctx, key, string(value), clientv3.WithLease(r.leaseID))
	if err != nil {
		return fmt.Errorf("failed to register service: %w", err)
	}

	keepAliveCtx, cancel := context.WithCancel(context.Background())
	r.cancelFn = cancel

	ch, err := r.client.KeepAlive(keepAliveCtx, r.leaseID)
	if err != nil {
		return fmt.Errorf("failed to start keepalive: %w", err)
	}

	go func() {
		for range ch {
		}
		log.Printf("Service %s keepalive stopped", instance.ID)
	}()

	log.Printf("Service registered: %s at %s:%d", instance.Name, instance.Address, instance.Port)
	return nil
}

func (r *EtcdRegistry) Deregister(ctx context.Context, instance *ServiceInstance) error {
	if r.cancelFn != nil {
		r.cancelFn()
	}

	key := fmt.Sprintf("%s/%s/%s", r.prefix, instance.Name, instance.ID)
	_, err := r.client.Delete(ctx, key)
	return err
}

func (r *EtcdRegistry) Discover(ctx context.Context, serviceName string) ([]*ServiceInstance, error) {
	prefix := fmt.Sprintf("%s/%s/", r.prefix, serviceName)
	resp, err := r.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}

	var instances []*ServiceInstance
	for _, kv := range resp.Kvs {
		var instance ServiceInstance
		if err := json.Unmarshal(kv.Value, &instance); err != nil {
			continue
		}
		instance.Healthy = true
		instances = append(instances, &instance)
	}

	return instances, nil
}

func (r *EtcdRegistry) Watch(ctx context.Context, serviceName string) (<-chan []*ServiceInstance, error) {
	prefix := fmt.Sprintf("%s/%s/", r.prefix, serviceName)
	updateCh := make(chan []*ServiceInstance, 10)

	instances, err := r.Discover(ctx, serviceName)
	if err != nil {
		return nil, err
	}
	updateCh <- instances

	watchCh := r.client.Watch(ctx, prefix, clientv3.WithPrefix())

	go func() {
		defer close(updateCh)
		for {
			select {
			case <-ctx.Done():
				return
			case resp := <-watchCh:
				for range resp.Events {
					instances, err := r.Discover(ctx, serviceName)
					if err != nil {
						continue
					}
					select {
					case updateCh <- instances:
					default:
					}
				}
			}
		}
	}()

	return updateCh, nil
}
```

### 四、本地缓存与订阅推送

```go
type ServiceDiscovery struct {
	registry   *EtcdRegistry
	cache      map[string][]*ServiceInstance
	mu         sync.RWMutex
	subscribers map[string][]chan []*ServiceInstance
}

func NewServiceDiscovery(registry *EtcdRegistry) *ServiceDiscovery {
	return &ServiceDiscovery{
		registry:    registry,
		cache:       make(map[string][]*ServiceInstance),
		subscribers: make(map[string][]chan []*ServiceInstance),
	}
}

func (d *ServiceDiscovery) GetInstances(ctx context.Context, serviceName string) ([]*ServiceInstance, error) {
	d.mu.RLock()
	if instances, ok := d.cache[serviceName]; ok && len(instances) > 0 {
		d.mu.RUnlock()
		return instances, nil
	}
	d.mu.RUnlock()

	instances, err := d.registry.Discover(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	d.cache[serviceName] = instances
	d.mu.Unlock()

	return instances, nil
}

func (d *ServiceDiscovery) Subscribe(ctx context.Context, serviceName string) error {
	updateCh, err := d.registry.Watch(ctx, serviceName)
	if err != nil {
		return err
	}

	go func() {
		for instances := range updateCh {
			d.mu.Lock()
			d.cache[serviceName] = instances
			subs := d.subscribers[serviceName]
			d.mu.Unlock()

			for _, ch := range subs {
				select {
				case ch <- instances:
				default:
				}
			}
		}
	}()

	return nil
}
```

### 五、健康检查

```go
type HealthChecker struct {
	registry  *EtcdRegistry
	interval  time.Duration
	timeout   time.Duration
}

func NewHealthChecker(registry *EtcdRegistry, interval, timeout time.Duration) *HealthChecker {
	return &HealthChecker{
		registry: registry,
		interval: interval,
		timeout:  timeout,
	}
}

func (h *HealthChecker) Start(ctx context.Context, instances []*ServiceInstance) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, inst := range instances {
				healthy := h.checkHealth(inst)
				inst.Healthy = healthy
			}
		}
	}
}

func (h *HealthChecker) checkHealth(instance *ServiceInstance) bool {
	client := http.Client{Timeout: h.timeout}
	url := fmt.Sprintf("http://%s:%d/health", instance.Address, instance.Port)

	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}
```

### 六、总结

服务注册与发现是微服务通信的基石：

1. **注册**：服务启动时注册，宕机时自动摘除（基于Lease）
2. **发现**：本地缓存 + Watch推送，兼顾性能和实时性
3. **健康检查**：主动探测 + 被动摘除，双重保障
4. **选型**：etcd适合K8s生态，Consul适合多数据中心，Nacos适合阿里生态
