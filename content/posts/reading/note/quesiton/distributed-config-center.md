---
title: "分布式配置中心如何设计与实现"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["配置中心", "分布式", "golang"]
---

## 问题

微服务架构中，配置分散在各个服务中，如何集中管理、动态更新、版本回滚？基于etcd的配置中心如何实现长轮询推送和配置变更监听？

## 回答

配置中心是微服务的基础设施之一，解决了配置集中管理、动态更新和环境隔离等问题。一个生产级配置中心需要支持实时推送、版本管理、灰度发布和权限控制。

### 一、配置中心的核心需求

1. **集中管理**：所有配置集中存储，统一管理
2. **动态更新**：配置变更实时推送到客户端，无需重启
3. **版本管理**：配置变更历史可追溯，支持回滚
4. **环境隔离**：dev/staging/prod环境配置隔离
5. **灰度发布**：配置变更先在小范围验证，再全量发布

### 二、基于etcd的配置中心实现

```go
package configcenter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type ConfigItem struct {
	Key       string      `json:"key"`
	Value     string      `json:"value"`
	Version   int64       `json:"version"`
	Env       string      `json:"env"`
	App       string      `json:"app"`
	UpdatedAt time.Time   `json:"updated_at"`
	UpdatedBy string      `json:"updated_by"`
}

type ConfigCenter struct {
	client *clientv3.Client
	prefix string
	mu     sync.RWMutex
	cache  map[string]*ConfigItem
}

func NewConfigCenter(endpoints []string, prefix string) (*ConfigCenter, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	cc := &ConfigCenter{
		client: cli,
		prefix: prefix,
		cache:  make(map[string]*ConfigItem),
	}

	return cc, nil
}

func (cc *ConfigCenter) configKey(env, app, key string) string {
	return fmt.Sprintf("%s/%s/%s/%s", cc.prefix, env, app, key)
}

func (cc *ConfigCenter) Set(ctx context.Context, env, app, key, value, operator string) error {
	k := cc.configKey(env, app, key)

	resp, err := cc.client.Get(ctx, k)
	if err != nil {
		return err
	}

	var version int64 = 1
	if resp.Count > 0 {
		var existing ConfigItem
		json.Unmarshal(resp.Kvs[0].Value, &existing)
		version = existing.Version + 1
	}

	item := &ConfigItem{
		Key:       key,
		Value:     value,
		Version:   version,
		Env:       env,
		App:       app,
		UpdatedAt: time.Now(),
		UpdatedBy: operator,
	}

	data, _ := json.Marshal(item)
	_, err = cc.client.Put(ctx, k, string(data))
	if err != nil {
		return err
	}

	cc.mu.Lock()
	cc.cache[k] = item
	cc.mu.Unlock()

	return nil
}

func (cc *ConfigCenter) Get(ctx context.Context, env, app, key string) (*ConfigItem, error) {
	k := cc.configKey(env, app, key)

	cc.mu.RLock()
	if item, ok := cc.cache[k]; ok {
		cc.mu.RUnlock()
		return item, nil
	}
	cc.mu.RUnlock()

	resp, err := cc.client.Get(ctx, k)
	if err != nil {
		return nil, err
	}

	if resp.Count == 0 {
		return nil, fmt.Errorf("config not found: %s", k)
	}

	var item ConfigItem
	json.Unmarshal(resp.Kvs[0].Value, &item)

	cc.mu.Lock()
	cc.cache[k] = &item
	cc.mu.Unlock()

	return &item, nil
}

func (cc *ConfigCenter) GetByApp(ctx context.Context, env, app string) ([]*ConfigItem, error) {
	prefix := fmt.Sprintf("%s/%s/%s/", cc.prefix, env, app)
	resp, err := cc.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}

	var items []*ConfigItem
	for _, kv := range resp.Kvs {
		var item ConfigItem
		json.Unmarshal(kv.Value, &item)
		items = append(items, &item)
	}

	return items, nil
}

func (cc *ConfigCenter) Watch(ctx context.Context, env, app string, onChange func(item *ConfigItem)) error {
	prefix := fmt.Sprintf("%s/%s/%s/", cc.prefix, env, app)

	watchCh := cc.client.Watch(ctx, prefix, clientv3.WithPrefix())

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case resp := <-watchCh:
			for _, event := range resp.Events {
				var item ConfigItem
				json.Unmarshal(event.Kv.Value, &item)

				k := string(event.Kv.Key)
				cc.mu.Lock()
				cc.cache[k] = &item
				cc.mu.Unlock()

				onChange(&item)
			}
		}
	}
}

func (cc *ConfigCenter) Rollback(ctx context.Context, env, app, key string, targetVersion int64) error {
	k := cc.configKey(env, app, key)

	resp, err := cc.client.Get(ctx, k)
	if err != nil {
		return err
	}

	if resp.Count == 0 {
		return fmt.Errorf("config not found")
	}

	var current ConfigItem
	json.Unmarshal(resp.Kvs[0].Value, &current)

	if current.Version <= targetVersion {
		return fmt.Errorf("cannot rollback to version %d, current is %d", targetVersion, current.Version)
	}

	current.Version = current.Version + 1
	current.Value = fmt.Sprintf("ROLLBACK_FROM_v%d", targetVersion)
	current.UpdatedAt = time.Now()
	current.UpdatedBy = "system_rollback"

	data, _ := json.Marshal(current)
	_, err = cc.client.Put(ctx, k, string(data))
	return err
}
```

### 三、客户端SDK

```go
type ConfigClient struct {
	center    *ConfigCenter
	env       string
	app       string
	callbacks map[string][]func(string)
	mu        sync.RWMutex
}

func NewConfigClient(center *ConfigCenter, env, app string) *ConfigClient {
	return &ConfigClient{
		center:    center,
		env:       env,
		app:       app,
		callbacks: make(map[string][]func(string)),
	}
}

func (c *ConfigClient) GetString(ctx context.Context, key string) string {
	item, err := c.center.Get(ctx, c.env, c.app, key)
	if err != nil {
		return ""
	}
	return item.Value
}

func (c *ConfigClient) OnChange(key string, callback func(value string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.callbacks[key] = append(c.callbacks[key], callback)
}

func (c *ConfigClient) StartWatch(ctx context.Context) error {
	return c.center.Watch(ctx, c.env, c.app, func(item *ConfigItem) {
		c.mu.RLock()
		callbacks := c.callbacks[item.Key]
		c.mu.RUnlock()

		for _, cb := range callbacks {
			cb(item.Value)
		}
	})
}
```

### 四、总结

配置中心的核心价值是**配置集中管理 + 动态推送**：

1. **存储**：etcd/Consul/Nacos提供可靠存储和Watch机制
2. **缓存**：客户端本地缓存，避免每次都查询注册中心
3. **推送**：基于Watch的实时推送，配置变更秒级生效
4. **版本**：每次变更记录版本号，支持回滚
5. **隔离**：环境/应用维度的Key前缀实现隔离

**行业实践**：Apollo（携程）、Nacos（阿里）、Spring Cloud Config
