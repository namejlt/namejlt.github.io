---
title: "WebSocket长连接系统如何设计"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["WebSocket", "长连接", "golang"]
---

## 问题

即时通讯、实时推送等场景需要WebSocket长连接。如何管理百万级长连接？连接心跳、重连、消息推送、集群广播如何设计？

## 回答

WebSocket长连接系统的核心挑战是连接管理和消息路由。单机可支撑数万连接，百万级连接需要集群化方案，且需要解决连接迁移、消息广播和状态同步问题。

### 一、架构设计

```
客户端 → 负载均衡 → WebSocket网关集群 → 消息队列 → 业务服务
                    (维护连接)          (消息路由)
```

### 二、连接管理

```go
package ws

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type Connection struct {
	ID        string
	UserID    string
	Conn      *websocket.Conn
	mu        sync.Mutex
	closed    bool
	lastPing  time.Time
	sendCh    chan []byte
	closeCh   chan struct{}
}

type ConnectionManager struct {
	mu          sync.RWMutex
	connections map[string]*Connection
	userConns   map[string]map[string]*Connection
	maxConns    int
	connCount   int64
}

func NewConnectionManager(maxConns int) *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]*Connection),
		userConns:   make(map[string]map[string]*Connection),
		maxConns:    maxConns,
	}
}

func (m *ConnectionManager) Add(conn *Connection) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.maxConns > 0 && int(atomic.LoadInt64(&m.connCount)) >= m.maxConns {
		return fmt.Errorf("max connections reached")
	}

	m.connections[conn.ID] = conn

	if _, ok := m.userConns[conn.UserID]; !ok {
		m.userConns[conn.UserID] = make(map[string]*Connection)
	}
	m.userConns[conn.UserID][conn.ID] = conn

	atomic.AddInt64(&m.connCount, 1)
	return nil
}

func (m *ConnectionManager) Remove(connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, ok := m.connections[connID]
	if !ok {
		return
	}

	delete(m.connections, connID)

	if conns, ok := m.userConns[conn.UserID]; ok {
		delete(conns, connID)
		if len(conns) == 0 {
			delete(m.userConns, conn.UserID)
		}
	}

	atomic.AddInt64(&m.connCount, -1)
}

func (m *ConnectionManager) GetByUser(userID string) []*Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conns, ok := m.userConns[userID]
	if !ok {
		return nil
	}

	result := make([]*Connection, 0, len(conns))
	for _, conn := range conns {
		result = append(result, conn)
	}
	return result
}

func (m *ConnectionManager) Count() int64 {
	return atomic.LoadInt64(&m.connCount)
}
```

### 三、WebSocket服务端

```go
type WSServer struct {
	manager   *ConnectionManager
	upgrader  websocket.Upgrader
	handlers  map[string]MessageHandler
}

type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type MessageHandler func(conn *Connection, msg *Message) error

func NewWSServer(manager *ConnectionManager) *WSServer {
	return &WSServer{
		manager:  manager,
		upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		handlers: make(map[string]MessageHandler),
	}
}

func (s *WSServer) Handle(messageType string, handler MessageHandler) {
	s.handlers[messageType] = handler
}

func (s *WSServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	connID := generateConnID()
	userID := r.URL.Query().Get("uid")

	wsConn := &Connection{
		ID:       connID,
		UserID:   userID,
		Conn:     conn,
		sendCh:   make(chan []byte, 256),
		closeCh:  make(chan struct{}),
		lastPing: time.Now(),
	}

	if err := s.manager.Add(wsConn); err != nil {
		conn.Close()
		return
	}

	go s.readPump(wsConn)
	go s.writePump(wsConn)
}

func (s *WSServer) readPump(conn *Connection) {
	defer func() {
		s.manager.Remove(conn.ID)
		conn.Conn.Close()
	}()

	conn.Conn.SetReadLimit(65536)
	conn.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.Conn.SetPongHandler(func(string) error {
		conn.lastPing = time.Now()
		conn.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := conn.Conn.ReadMessage()
		if err != nil {
			return
		}

		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		handler, ok := s.handlers[msg.Type]
		if !ok {
			continue
		}

		handler(conn, &msg)
	}
}

func (s *WSServer) writePump(conn *Connection) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		conn.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-conn.sendCh:
			conn.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				conn.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			conn.mu.Lock()
			err := conn.Conn.WriteMessage(websocket.TextMessage, message)
			conn.mu.Unlock()
			if err != nil {
				return
			}

		case <-ticker.C:
			conn.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (s *WSServer) SendToUser(userID string, message []byte) error {
	conns := s.manager.GetByUser(userID)
	if len(conns) == 0 {
		return fmt.Errorf("user %s not online", userID)
	}

	for _, conn := range conns {
		select {
		case conn.sendCh <- message:
		default:
		}
	}
	return nil
}
```

### 四、集群广播

```go
type ClusterBroadcaster struct {
	localServer *WSServer
	redis       *redis.Client
	channel     string
	nodeID      string
}

func NewClusterBroadcaster(server *WSServer, redisClient *redis.Client, nodeID, channel string) *ClusterBroadcaster {
	b := &ClusterBroadcaster{
		localServer: server,
		redis:       redisClient,
		channel:     channel,
		nodeID:      nodeID,
	}
	go b.subscribe()
	return b
}

type BroadcastMessage struct {
	TargetNode string `json:"target_node,omitempty"`
	UserID     string `json:"user_id"`
	Payload    []byte `json:"payload"`
}

func (b *ClusterBroadcaster) BroadcastToUser(userID string, payload []byte) error {
	msg := BroadcastMessage{
		UserID:  userID,
		Payload: payload,
	}

	data, _ := json.Marshal(msg)
	return b.redis.Publish(context.Background(), b.channel, data).Err()
}

func (b *ClusterBroadcaster) subscribe() {
	sub := b.redis.Subscribe(context.Background(), b.channel)
	ch := sub.Channel()

	for msg := range ch {
		var broadcast BroadcastMessage
		if err := json.Unmarshal([]byte(msg.Payload), &broadcast); err != nil {
			continue
		}

		if broadcast.TargetNode != "" && broadcast.TargetNode != b.nodeID {
			continue
		}

		b.localServer.SendToUser(broadcast.UserID, broadcast.Payload)
	}
}
```

### 五、心跳与重连

**服务端心跳**：30秒发一次Ping，60秒无Pong则断开

**客户端重连**：

```go
type ReconnectingClient struct {
	url       string
	conn      *websocket.Conn
	reconnect chan struct{}
	done      chan struct{}
}

func NewReconnectingClient(url string) *ReconnectingClient {
	return &ReconnectingClient{
		url:       url,
		reconnect: make(chan struct{}, 1),
		done:      make(chan struct{}),
	}
}

func (c *ReconnectingClient) Start() {
	for {
		select {
		case <-c.done:
			return
		default:
			c.connect()
			time.Sleep(c.backoff())
		}
	}
}

func (c *ReconnectingClient) connect() {
	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		return
	}
	c.conn = conn

	defer conn.Close()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			return
		}
	}
}

func (c *ReconnectingClient) backoff() time.Duration {
	return 3 * time.Second
}
```

### 六、总结

WebSocket长连接系统的核心设计：

1. **连接管理**：按用户ID索引连接，支持多端登录
2. **心跳机制**：Ping/Pong保活，超时自动断开
3. **集群广播**：Redis Pub/Sub跨节点消息路由
4. **重连策略**：指数退避重连，避免雪崩
5. **背压控制**：发送缓冲区满时丢弃或降级

**行业实践**：GoEasy、Socket.IO、Centrifugo
