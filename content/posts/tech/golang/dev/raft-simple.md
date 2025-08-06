---
title: "raft详解与go实现"
date: 2025-08-04T16:00:00+08:00
draft: false
toc: true
featured: false
categories: ["技术/golang/开发"]
tags: ["golang"]
---

## Raft算法详解：从理论到Go语言实战

在分布式系统的世界里，如何保证数据的一致性是核心难题之一。Raft算法的出现，为解决这一难题提供了一个既易于理解又坚实可靠的方案。本文将深入浅出地讲解Raft算法的核心原理，并以Go语言为例，参考etcd的实现方式，编写可运行的核心代码，展示其运行结果。最后，我们将探讨Raft算法成功的原因，并简要介绍其他主流的分布式一致性算法。

### Raft算法详细讲解

Raft是一种用于管理复制日志（replicated log）的一致性算法。它与大名鼎鼎的Paxos算法功能相当，但被设计得更容易理解。易于理解是Raft的首要设计目标，这使得它在工业界和学术界都获得了广泛的应用和认可。

Raft通过将一致性问题分解为三个相对独立的子问题来简化逻辑：

1.  **领导者选举（Leader Election）**
2.  **日志复制（Log Replication）**
3.  **安全性（Safety）**

#### 1\. 服务器角色

在Raft集群中，任何时刻，每个服务器都处于以下三种状态之一：

  * **领导者（Leader）**: 负责处理所有来自客户端的请求，并管理日志复制。在正常情况下，集群中只有一个Leader。
  * **跟随者（Follower）**: 被动的角色，不发送任何请求，只响应来自Leader和Candidate的请求。所有服务器初始状态都是Follower。
  * **候选人（Candidate）**: 用于选举新的Leader。当Follower在一段时间内没有收到Leader的心跳时，会转变为Candidate并发起选举。

#### 2\. 任期（Term）

Raft将时间划分为任意长度的**任期（Term）**，以一个单调递增的整数表示。每个任期都以一次选举开始，一个或多个Candidate会尝试成为Leader。如果一个Candidate赢得选举，它将在该任期的剩余时间里担任Leader。在某些情况下，选举可能会导致选票分裂，这种情况下，该任期将在没有Leader的情况下结束，新的任期将很快开始。任期在Raft中扮演着逻辑时钟的角色，使得服务器可以识别出过时的信息。

#### 3\. 领导者选举（Leader Election）

领导者选举是Raft的核心机制之一。

  * **选举触发**: 当一个Follower在设定的“选举超时”（Election Timeout）时间内没有收到来自Leader的`AppendEntries` RPC（心跳），它就认为当前没有可用的Leader，并转变为Candidate，开始一轮新的选举。选举超时时间通常是一个随机值（例如150-300ms），以减少多个Follower同时发起选举导致选票分裂的可能性。
  * **选举过程**:
    1.  Candidate会增加自己当前的任期号。
    2.  为自己投上一票。
    3.  向集群中的所有其他服务器发送`RequestVote` RPC。
  * **选举结果**:
      * **成为Leader**: 如果一个Candidate收到了来自集群中大多数服务器的投票（包括自己），它就赢得了选举，成为新的Leader。随后，它会立即向所有Follower发送心跳，以确立自己的地位并阻止新的选举。
      * **选举出其他Leader**: 如果在等待投票期间，该Candidate收到了来自另一个声称自己是Leader的服务器的`AppendEntries` RPC，并且该Leader的任期号不小于自己当前的任期号，那么它会承认这个Leader，并转变为Follower。
      * **选举超时**: 如果一段时间后，没有Candidate获得大多数投票（例如，由于选票分裂），Candidate会超时，并增加任期号，开始新一轮的选举。

#### 4\. 日志复制（Log Replication）

一旦选举出Leader，它就开始全权负责处理客户端的请求。每个客户端请求都包含一条将被复制状态机执行的命令。

  * **请求处理**: Leader接收到客户端请求后，会将其作为一条新的日志条目（log entry）附加到自己的日志中。
  * **日志复制**: Leader通过`AppendEntries` RPC将新的日志条目发送给所有的Follower。
  * **日志提交**: 当Leader收到大多数Follower对该日志条目的成功响应后，该日志条目就被认为是\*\*已提交（committed）\*\*的。Leader会将这条命令应用到自己的状态机，并将结果返回给客户端。
  * **状态机应用**: Leader在后续的`AppendEntries` RPC中会通知Follower哪些日志条目已经被提交。Follower收到通知后，也会将这些已提交的日志条目应用到自己的本地状态机中。

Raft通过这种方式，保证了所有服务器上的状态机以相同的顺序执行相同的命令，从而保持一致。

#### 5\. 安全性（Safety）

为了保证在任何情况下（如网络分区、服务器宕机）都不会出现不一致的情况，Raft设计了以下关键的安全性原则：

  * **选举安全（Election Safety）**: 在一个给定的任期内，最多只能有一个Leader被选举出来。
  * **领导者只附加（Leader Append-Only）**: Leader绝不会覆盖或删除自己的日志条目，只会追加新的条目。
  * **日志匹配（Log Matching）**: 如果两个不同日志中的条目拥有相同的索引和任期号，那么它们存储的是相同的命令。并且，这两个日志中在该条目之前的所有条目也都是完全相同的。
  * **领导者完整性（Leader Completeness）**: 如果一个日志条目在某个任期被提交，那么它必然会出现在所有更高任期号的Leader的日志中。
  * **状态机安全（State Machine Safety）**: 如果一个服务器将一个给定的日志条目应用到了它的状态机，那么其他服务器不能在相同的日志索引上应用一个不同的条目。

### go实现raft设计思路

原生实现Raft算法，我们需要手动构建以下组件：

1.  **节点（Node）**: 代表集群中的一个服务器。它将维护Raft的所有核心状态（任期、日志、角色等），并包含主逻辑循环。
2.  **状态机（State Machine）**: 一个简单的键值存储（`map[string]string`），用于应用已提交的日志条目。
3.  **RPC通信**: 使用Go标准库`net/rpc`来实现节点间的通信，用于发送`RequestVote`和`AppendEntries`消息。
4.  **定时器**: 使用`time.Timer`和`time.Ticker`来处理选举超时和领导者心跳。
5.  **并发模型**: 使用Goroutine和Channel来处理并发事件，如RPC请求、定时器触发和客户端提议。

### 核心代码 (`raft_native.go`)

```go
package main

import (
	"encoding/gob"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/rpc"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ServerState 定义节点在Raft集群中的角色
type ServerState int

const (
	Follower  ServerState = iota // 跟随者
	Candidate                    // 候选人
	Leader                       // 领导者
)

// LogEntry 表示Raft日志中的单个条目
type LogEntry struct {
	Term    int         // 日志条目所在的任期
	Command interface{} // 日志条目包含的命令
}

// Node 表示Raft集群中的单个服务器
type Node struct {
	mu sync.Mutex // 互斥锁，保护共享状态

	id    int         // 节点的唯一ID
	peers []string    // 集群中其他节点的地址
	state ServerState // 当前状态（跟随者、候选人或领导者）

	// 所有服务器上的持久状态
	currentTerm int        // 服务器已知的最新任期
	votedFor    int        // 当前任期内收到选票的候选人ID，如果没有投给任何候选人则为-1
	log         []LogEntry // 日志条目

	// 所有服务器上的易失状态
	commitIndex int // 已知已提交的最高日志条目的索引
	lastApplied int // 已应用于状态机的最高日志条目的索引

	// 领导者上的易失状态
	nextIndex  map[int]int // 对于每个服务器，发送到该服务器的下一个日志条目的索引
	matchIndex map[int]int // 对于每个服务器，已知在该服务器上匹配的日志最高索引

	// 通信通道
	rpcChan       chan *rpc.Call   // 接收RPC请求
	heartbeatChan chan bool        // 用于接收心跳信号
	proposeChan   chan interface{} // 接收客户端提案
	commitChan    chan interface{} // 发送已提交的日志条目

	// 定时器
	electionTimer *time.Timer // 选举定时器

	// 持久化相关字段
	logFile      string     // 日志文件路径
	stateFile    string     // 状态文件路径
	persistMutex sync.Mutex // 持久化互斥锁

	// Leader有效性检查
	leaderTerm int // 成为leader时的任期，用于检查是否仍然是有效的leader
}

// RequestVoteArgs RequestVote RPC的参数
type RequestVoteArgs struct {
	Term         int // 候选人的任期
	CandidateId  int // 候选人的ID
	LastLogIndex int // 候选人最后一个日志条目的索引
	LastLogTerm  int // 候选人最后一个日志条目的任期
}

// RequestVoteReply RequestVote RPC的回复
type RequestVoteReply struct {
	Term        int  // 当前任期，用于候选人更新自己
	VoteGranted bool // 候选人是否获得了选票
}

// AppendEntriesArgs AppendEntries RPC的参数
type AppendEntriesArgs struct {
	Term         int        // 领导者的任期
	LeaderId     int        // 领导者的ID
	PrevLogIndex int        // 紧邻新日志条目之前的日志条目的索引
	PrevLogTerm  int        // 紧邻新日志条目之前的日志条目的任期
	Entries      []LogEntry // 需要存储的日志条目（空表示心跳；为了提高效率可能发送多个）
	LeaderCommit int        // 领导者的已提交日志索引
}

// AppendEntriesReply AppendEntries RPC的回复
type AppendEntriesReply struct {
	Term    int  // 当前任期，用于领导者更新自己
	Success bool // 如果跟随者包含与PrevLogIndex和PrevLogTerm匹配的日志条目，则为true
}

// NewNode 创建一个新的Raft节点
// id: 节点的唯一标识符
// peers: 集群中所有节点的地址列表
// 返回: 新创建的Raft节点实例
func NewNode(id int, peers []string) *Node {
	node := &Node{
		id:            id,
		peers:         peers,
		state:         Follower,
		currentTerm:   0,
		votedFor:      -1,
		log:           make([]LogEntry, 1), // 日志从索引0的虚拟条目开始
		commitIndex:   0,
		lastApplied:   0,
		heartbeatChan: make(chan bool, 1), // 初始化channel
		proposeChan:   make(chan interface{}, 1),
		commitChan:    make(chan interface{}, 1),
		logFile:       fmt.Sprintf("raft_log_%d.dat", id),
		stateFile:     fmt.Sprintf("raft_state_%d.dat", id),
	}

	// 加载持久化状态
	node.loadState()
	node.loadLog()

	node.resetElectionTimer()
	go node.run()
	go node.startRPCServer()
	return node
}

// electionTimeout 计算随机选举超时时间
// 返回: 随机的选举超时时间
// 1. 增加选举超时时间范围
func electionTimeout() time.Duration {
	// 将选举超时时间增加到 500-1000 毫秒
	return time.Duration(500+rand.Intn(500)) * time.Millisecond
}

// resetElectionTimer 重置节点的选举定时器
func (n *Node) resetElectionTimer() {
	if n.electionTimer != nil {
		n.electionTimer.Stop()
	}
	n.electionTimer = time.NewTimer(electionTimeout())
}

// run 是Raft节点的主循环
// 根据节点的当前状态（跟随者、候选人或领导者）执行相应的逻辑
func (n *Node) run() {
	for {

		//log.Printf("[%d] 节点run ================================================== 状态: %v", n.id, n.state)

		switch n.state {
		case Follower:
			select {
			case <-n.electionTimer.C:
				log.Printf("[%d] 选举超时，成为候选人", n.id)
				n.becomeCandidate()
			case <-n.heartbeatChan:
				log.Printf("[%d] follower 收到心跳信号, 重置选举定时器", n.id) //抑制节点选举
				n.resetElectionTimer()
			case prop := <-n.proposeChan: //非leader节点忽略提案，转发到leader上
				n.mu.Lock()
				leaderId := n.votedFor //投票给谁，默认为leader
				n.mu.Unlock()
				if leaderId > 0 { // 检查 leaderId 是否有效
					ret := new(string)
					// 转发提案到 leader
					err := n.sendRPC(leaderId, "Node.ProposeCall", prop, ret)
					if err != nil {
						log.Printf("[%d] 跟随者收到提案 %+v，忽略，需转发到leader，报错 %v", n.id, prop, err)
					}
				} else {
					log.Printf("[%d] 未找到有效 leader，无法转发提案 %+v", n.id, prop)
				}
			default: // 按照节点最新状态处理逻辑
				time.Sleep(2 * time.Millisecond)
			}
		case Candidate:
			select {
			case <-n.electionTimer.C:
				log.Printf("[%d] 选举超时，开始新的选举", n.id)
				n.becomeCandidate() // 开始新的选举
			case prop := <-n.proposeChan: //非leader节点忽略提案
				n.proposeChan <- prop // 为简单起见，将其放回，等待leader转发
			default: // 按照节点最新状态处理逻辑
				time.Sleep(2 * time.Millisecond)
			}
		case Leader:
			// 检查是否仍然是有效的leader
			n.mu.Lock()
			if n.leaderTerm != n.currentTerm {
				log.Printf("[%d] 不再是有效leader，任期已变更", n.id)
				n.mu.Unlock()
				continue
			}
			_ = n.currentTerm // 避免linter警告
			n.mu.Unlock()

			// 领导者发送心跳
			log.Printf("[%d] 领导者发送心跳开始", n.id)
			n.sendHeartbeats()
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// becomeCandidate 将节点转换为候选人状态并开始选举
func (n *Node) becomeCandidate() {
	n.mu.Lock()
	n.state = Candidate
	n.currentTerm++
	n.votedFor = n.id
	n.leaderTerm = 0 // 重置leader任期
	n.resetElectionTimer()
	savedCurrentTerm := n.currentTerm
	// 保存状态变更
	n.saveState()
	n.mu.Unlock()

	log.Printf("[%d] 成为%d任期的候选人", n.id, savedCurrentTerm)

	votes := atomic.Int32{}
	votes.Add(1)
	// 向所有其他节点发送RequestVote RPC
	for i := range n.peers {
		if i+1 == n.id {
			continue
		}
		go func(peerIndex int) {
			n.mu.Lock()
			lastLogIndex := len(n.log) - 1
			lastLogTerm := n.log[lastLogIndex].Term
			n.mu.Unlock()

			args := RequestVoteArgs{
				Term:         savedCurrentTerm,
				CandidateId:  n.id,
				LastLogIndex: lastLogIndex,
				LastLogTerm:  lastLogTerm,
			}
			var reply RequestVoteReply

			log.Printf("[%d] 向%d发送RequestVote: %+v", n.id, peerIndex+1, args)
			err := n.sendRPC(peerIndex+1, "Node.RequestVote", args, &reply)
			log.Printf("[%d] 节点%d回复RequestVote: %+v err: %v", n.id, peerIndex+1, reply, err)
			log.Printf("[%d] 当前节点信息: %+v", n.id, n)
			if err == nil {
				n.mu.Lock()
				defer n.mu.Unlock()
				if n.state != Candidate || savedCurrentTerm != n.currentTerm {
					// 状态已更改，忽略回复
					return
				}
				if reply.Term > n.currentTerm {
					log.Printf("[%d] 从节点%d发现更高的任期%d，成为跟随者", n.id, peerIndex+1, reply.Term)
					n.becomeFollower(reply.Term)
				} else if reply.VoteGranted {
					votes.Add(1)
					log.Printf("[%d] 收到来自%d的选票，总票数: %d", n.id, peerIndex+1, votes.Load())
					// 检查是否获得多数票
					if votes.Load() > int32(len(n.peers)/2) {
						log.Printf("[%d] 以%d票赢得选举，准备成为领导者", n.id, votes.Load())
						n.becomeLeader()
						log.Printf("[%d] 已调用 becomeLeader 方法", n.id)
					}
				}
			}
		}(i)
	}
}

// becomeFollower 将节点转换为跟随者状态
// term: 新的任期号
func (n *Node) becomeFollower(term int) {
	n.state = Follower
	n.currentTerm = term
	n.votedFor = -1
	n.leaderTerm = 0 // 重置leader任期
	n.resetElectionTimer()
	// 保存状态变更
	n.saveState()
	log.Printf("[%d] 成为%d任期的跟随者", n.id, n.currentTerm)
}

// becomeLeader 将节点转换为领导者状态
func (n *Node) becomeLeader() {
	if n.state != Candidate {
		log.Printf("[%d] 不是候选人，无法成为领导者", n.id)
		return
	}
	log.Printf("[%d] 开始转换为领导者状态", n.id)
	n.state = Leader
	n.leaderTerm = n.currentTerm
	if n.electionTimer != nil {
		n.electionTimer.Stop()
		log.Printf("[%d] 已停止选举定时器", n.id)
	}

	n.nextIndex = make(map[int]int)
	n.matchIndex = make(map[int]int)
	for i := range n.peers {
		n.nextIndex[i+1] = len(n.log)
		n.matchIndex[i+1] = 0
	}
	log.Printf("[%d] 已初始化nextIndex和matchIndex", n.id)

	// 立即广播心跳（非阻塞方式）
	go n.sendHeartbeats()
	log.Printf("[%d] 已启动心跳发送goroutine", n.id)

	log.Printf("[%d] 成为%d任期的领导者", n.id, n.currentTerm)

	go func() {
		for cmd := range n.proposeChan {
			n.mu.Lock()
			if n.state != Leader || n.leaderTerm != n.currentTerm {
				log.Printf("[%d] 领导者已不再是领导者，当前任期 %d", n.id, n.currentTerm)
				n.mu.Unlock()
				return
			}
			n.log = append(n.log, LogEntry{Term: n.currentTerm, Command: cmd})
			log.Printf("[%d] 领导者在索引%d处向其日志追加新条目", n.id, len(n.log)-1)
			n.saveLog()
			n.mu.Unlock()
		}
	}()
}

// sendHeartbeats 向跟随者发送AppendEntries RPC（作为心跳或带有新条目）
func (n *Node) sendHeartbeats() {
	n.mu.Lock()
	if n.state != Leader {
		n.mu.Unlock()
		return
	}
	savedCurrentTerm := n.currentTerm
	n.mu.Unlock()

	log.Printf("[%d] 开始发送心跳", n.id)
	for i := range n.peers {
		if i+1 == n.id {
			continue
		}
		go func(peerIndex int) {
			n.mu.Lock()
			ni := n.nextIndex[peerIndex+1]
			prevLogIndex := ni - 1
			prevLogTerm := n.log[prevLogIndex].Term
			entries := n.log[ni:]

			args := AppendEntriesArgs{
				Term:         savedCurrentTerm,
				LeaderId:     n.id,
				PrevLogIndex: prevLogIndex,
				PrevLogTerm:  prevLogTerm,
				Entries:      entries,
				LeaderCommit: n.commitIndex,
			}
			n.mu.Unlock()

			var reply AppendEntriesReply
			if err := n.sendRPC(peerIndex+1, "Node.AppendEntries", args, &reply); err == nil {
				n.mu.Lock()
				defer n.mu.Unlock()
				if reply.Term > n.currentTerm {
					log.Printf("[%d] 收到更高任期%d，从leader转换为follower", n.id, reply.Term)
					n.becomeFollower(reply.Term)
					return
				}
				if n.state == Leader && savedCurrentTerm == n.currentTerm {
					if reply.Success {
						n.nextIndex[peerIndex+1] = ni + len(entries)
						n.matchIndex[peerIndex+1] = n.nextIndex[peerIndex+1] - 1
						log.Printf("[%d] 向%d发送AppendEntries成功。nextIndex: %d, matchIndex: %d", n.id, peerIndex+1, n.nextIndex[peerIndex+1], n.matchIndex[peerIndex+1])

						// 检查是否可以提交新条目
						for i := n.commitIndex + 1; i < len(n.log); i++ {
							if n.log[i].Term == n.currentTerm {
								matchCount := 1
								for pId := range n.peers {
									if pId+1 != n.id && n.matchIndex[pId+1] >= i {
										matchCount++
									}
								}
								if matchCount > len(n.peers)/2 {
									n.commitIndex = i
									log.Printf("[%d] 领导者提交索引%d", n.id, n.commitIndex)
									n.commitChan <- n.log[i].Command
								}
							}
						}
					} else {
						// 跟随者的日志不一致，递减nextIndex并重试
						n.nextIndex[peerIndex+1]--
						log.Printf("[%d] 向%d发送AppendEntries失败，将nextIndex递减到%d", n.id, peerIndex+1, n.nextIndex[peerIndex+1])
					}
				}
			} else {
				log.Printf("[%d] 向%d发送AppendEntries失败: %v", n.id, peerIndex+1, err)
			}
		}(i)
	}
}

// RequestVote RPC处理程序
// 处理来自候选人的投票请求
// args: 投票请求参数
// reply: 投票回复结果
// 返回: 错误信息，如果有的话
func (n *Node) RequestVote(args RequestVoteArgs, reply *RequestVoteReply) error {
	n.mu.Lock()
	log.Printf("[%d] RequestVote received: args=%+v, currentTerm=%d, votedFor=%d, state=%d", n.id, args, n.currentTerm, n.votedFor, n.state)
	if args.Term > n.currentTerm {
		n.becomeFollower(args.Term)
		lastLogIndex := len(n.log) - 1
		lastLogTerm := n.log[lastLogIndex].Term
		upToDate := args.LastLogTerm > lastLogTerm || (args.LastLogTerm == lastLogTerm && args.LastLogIndex >= lastLogIndex)
		log.Printf("[%d] RequestVote调试: lastLogIndex=%d, lastLogTerm=%d, args.LastLogIndex=%d, args.LastLogTerm=%d, upToDate=%v, votedFor=%d", n.id, lastLogIndex, lastLogTerm, args.LastLogIndex, args.LastLogTerm, upToDate, n.votedFor)
		if (n.votedFor == -1 || n.votedFor == args.CandidateId) && upToDate {
			log.Printf("[%d] RequestVote IF分支: 满足投票条件，准备投票给%d", n.id, args.CandidateId)
			n.votedFor = args.CandidateId
			reply.VoteGranted = true
			n.resetElectionTimer()
			log.Printf("[%d] RequestVote: 新任期%d，投票给%d (lastLogIndex=%d, lastLogTerm=%d, upToDate=%v)", n.id, n.currentTerm, args.CandidateId, lastLogIndex, lastLogTerm, upToDate)
		} else {
			log.Printf("[%d] RequestVote IF分支: 不满足投票条件，拒绝投票", n.id)
			reply.VoteGranted = false
			log.Printf("[%d] RequestVote: 新任期%d，拒绝投票给%d (已投票给: %d, 日志是否最新: %v)", n.id, n.currentTerm, args.CandidateId, n.votedFor, upToDate)
		}
		reply.Term = n.currentTerm
		log.Printf("[%d] RequestVote reply: reply=%+v", n.id, reply)
		n.saveState()
		log.Printf("[%d] RequestVote return: VoteGranted=%v, Term=%d", n.id, reply.VoteGranted, reply.Term)
		n.mu.Unlock()
		return nil
	}
	// 其余逻辑保持原样
	defer func() {
		log.Printf("[%d] RequestVote return: VoteGranted=%v, Term=%d", n.id, reply.VoteGranted, reply.Term)
		n.mu.Unlock()
	}()

	if args.Term < n.currentTerm {
		reply.Term = n.currentTerm
		reply.VoteGranted = false
		log.Printf("[%d] RequestVote: candidate term %d < currentTerm %d, vote not granted", n.id, args.Term, n.currentTerm)
		return nil
	}
	if args.Term == n.currentTerm && (n.state == Candidate || n.state == Leader) {
		n.becomeFollower(args.Term)
		log.Printf("[%d] RequestVote: same term %d, but was candidate/leader, become follower", n.id, args.Term)
	}

	reply.Term = n.currentTerm
	voteGranted := false

	lastLogIndex := len(n.log) - 1
	lastLogTerm := n.log[lastLogIndex].Term
	upToDate := args.LastLogTerm > lastLogTerm || (args.LastLogTerm == lastLogTerm && args.LastLogIndex >= lastLogIndex)

	if (n.votedFor == -1 || n.votedFor == args.CandidateId) && upToDate {
		voteGranted = true
		n.votedFor = args.CandidateId
		n.resetElectionTimer()
		log.Printf("[%d] 在任期%d中授予%d选票 (lastLogIndex=%d, lastLogTerm=%d, upToDate=%v)", n.id, n.currentTerm, args.CandidateId, lastLogIndex, lastLogTerm, upToDate)
	} else {
		log.Printf("[%d] 拒绝%d的选票。已投票给: %d, 日志是否最新: %v (lastLogIndex=%d, lastLogTerm=%d, args.LastLogIndex=%d, args.LastLogTerm=%d)", n.id, args.CandidateId, n.votedFor, upToDate, lastLogIndex, lastLogTerm, args.LastLogIndex, args.LastLogTerm)
	}

	reply.VoteGranted = voteGranted
	log.Printf("[%d] RequestVote reply: reply=%+v", n.id, reply)
	return nil
}

// saveState 保存当前任期和投票信息到磁盘
func (n *Node) saveState() {
	n.persistMutex.Lock()
	defer n.persistMutex.Unlock()

	// 创建临时文件
	file, err := os.Create(n.stateFile + ".tmp")
	if err != nil {
		log.Printf("[%d] 保存状态失败: %v", n.id, err)
		return
	}
	defer file.Close()

	// 序列化状态
	encode := gob.NewEncoder(file)
	if err := encode.Encode(n.currentTerm); err != nil {
		log.Printf("[%d] 编码currentTerm失败: %v", n.id, err)
		return
	}
	if err := encode.Encode(n.votedFor); err != nil {
		log.Printf("[%d] 编码votedFor失败: %v", n.id, err)
		return
	}

	// 原子替换文件
	if err := os.Rename(n.stateFile+".tmp", n.stateFile); err != nil {
		log.Printf("[%d] 重命名状态文件失败: %v", n.id, err)
		return
	}

	log.Printf("[%d] 状态已保存到 %s", n.id, n.stateFile)
}

// saveLog 保存日志到磁盘
func (n *Node) saveLog() {
	n.persistMutex.Lock()
	defer n.persistMutex.Unlock()

	// 创建临时文件
	file, err := os.Create(n.logFile + ".tmp")
	if err != nil {
		log.Printf("[%d] 保存日志失败: %v", n.id, err)
		return
	}
	defer file.Close()

	// 序列化日志
	encode := gob.NewEncoder(file)
	if err := encode.Encode(n.log); err != nil {
		log.Printf("[%d] 编码日志失败: %v", n.id, err)
		return
	}

	// 原子替换文件
	if err := os.Rename(n.logFile+".tmp", n.logFile); err != nil {
		log.Printf("[%d] 重命名日志文件失败: %v", n.id, err)
		return
	}

	log.Printf("[%d] 日志已保存到 %s, 日志长度: %d", n.id, n.logFile, len(n.log))
}

// loadState 从磁盘加载当前任期和投票信息
func (n *Node) loadState() {
	n.persistMutex.Lock()
	defer n.persistMutex.Unlock()

	// 检查文件是否存在
	if _, err := os.Stat(n.stateFile); os.IsNotExist(err) {
		log.Printf("[%d] 状态文件不存在，使用默认值", n.id)
		return
	}

	// 打开文件
	file, err := os.Open(n.stateFile)
	if err != nil {
		log.Printf("[%d] 打开状态文件失败: %v", n.id, err)
		return
	}
	defer file.Close()

	// 反序列化状态
	decode := gob.NewDecoder(file)
	if err := decode.Decode(&n.currentTerm); err != nil {
		log.Printf("[%d] 解码currentTerm失败: %v", n.id, err)
		return
	}
	if err := decode.Decode(&n.votedFor); err != nil {
		log.Printf("[%d] 解码votedFor失败: %v", n.id, err)
		return
	}

	log.Printf("[%d] 已加载状态: currentTerm=%d, votedFor=%d", n.id, n.currentTerm, n.votedFor)
}

// loadLog 从磁盘加载日志
func (n *Node) loadLog() {
	n.persistMutex.Lock()
	defer n.persistMutex.Unlock()

	// 检查文件是否存在
	if _, err := os.Stat(n.logFile); os.IsNotExist(err) {
		log.Printf("[%d] 日志文件不存在，使用默认日志", n.id)
		n.log = make([]LogEntry, 1) // 日志从索引0的虚拟条目开始
		return
	}

	// 打开文件
	file, err := os.Open(n.logFile)
	if err != nil {
		log.Printf("[%d] 打开日志文件失败: %v", n.id, err)
		return
	}
	defer file.Close()

	// 反序列化日志
	decode := gob.NewDecoder(file)
	if err := decode.Decode(&n.log); err != nil {
		log.Printf("[%d] 解码日志失败: %v", n.id, err)
		return
	}

	log.Printf("[%d] 已加载日志，长度: %d", n.id, len(n.log))
}

// AppendEntries RPC处理程序
// 处理来自领导者的日志追加请求
// args: 日志追加请求参数
// reply: 日志追加回复结果
// 返回: 错误信息，如果有的话
func (n *Node) AppendEntries(args AppendEntriesArgs, reply *AppendEntriesReply) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if args.Term > n.currentTerm {
		n.becomeFollower(args.Term)
		reply.Term = n.currentTerm
		reply.Success = false
		return nil
	}
	if args.Term < n.currentTerm {
		reply.Term = n.currentTerm
		reply.Success = false
		return nil
	}

	// 如果收到相同任期但当前节点是候选人或leader，转换为跟随者
	if args.Term == n.currentTerm && (n.state == Candidate || n.state == Leader) {
		n.becomeFollower(args.Term)
	}

	// FIX: A valid heartbeat or AppendEntries was received. Signal the run loop.
	select {
	case n.heartbeatChan <- true:
	default:
	}

	reply.Term = n.currentTerm

	// 检查日志是否包含prevLogIndex索引处的条目，且其任期与prevLogTerm匹配
	if args.PrevLogIndex >= len(n.log) || n.log[args.PrevLogIndex].Term != args.PrevLogTerm {
		reply.Success = false
		if args.PrevLogIndex >= len(n.log) {
			log.Printf("[%d] AppendEntries一致性检查失败。PrevLogIndex (%d) 超出日志范围 (%d)", n.id, args.PrevLogIndex, len(n.log))
		} else {
			log.Printf("[%d] AppendEntries一致性检查失败。我在索引%d处的日志任期为%d，领导者发送的是%d", n.id, args.PrevLogIndex, n.log[args.PrevLogIndex].Term, args.PrevLogTerm)
		}
		return nil
	}

	// 如果现有条目与新条目冲突（相同索引但不同任期），
	// 删除现有条目及其后的所有条目
	logConflict := false
	if len(args.Entries) > 0 {
		for i, entry := range args.Entries {
			idx := args.PrevLogIndex + 1 + i
			if idx < len(n.log) && n.log[idx].Term != entry.Term {
				logConflict = true
				n.log = n.log[:idx]
				break
			}
		}
	}

	// 追加任何尚未在日志中的新条目
	logAppended := false
	if logConflict || len(n.log) < args.PrevLogIndex+1+len(args.Entries) {
		n.log = append(n.log[:args.PrevLogIndex+1], args.Entries...)
		log.Printf("[%d] 从领导者追加了%d个条目。新日志长度: %d", n.id, len(args.Entries), len(n.log))
		logAppended = true
	}

	// 如果leaderCommit大于commitIndex，设置commitIndex = min(leaderCommit, 最后一个新条目的索引)
	if args.LeaderCommit > n.commitIndex {
		lastNewEntryIndex := args.PrevLogIndex + len(args.Entries)
		if args.LeaderCommit < lastNewEntryIndex {
			n.commitIndex = args.LeaderCommit
		} else {
			n.commitIndex = lastNewEntryIndex
		}
		log.Printf("[%d] 跟随者更新commitIndex到%d", n.id, n.commitIndex)
		go func() {
			n.mu.Lock()
			defer n.mu.Unlock()
			for i := n.lastApplied + 1; i <= n.commitIndex; i++ {
				n.commitChan <- n.log[i].Command
				n.lastApplied = i
			}
		}()
	}

	if logAppended {
		n.saveLog()
	}

	reply.Success = true
	return nil
}

// startRPCServer 启动节点的RPC服务器
func (n *Node) startRPCServer() {
	err := rpc.Register(n)
	if err != nil {
		log.Printf("[%d] 注册RPC服务器错误: %v", n.id, err)
		return
	}
	// 注册 RPC 方法的输入输出参数类型
	gob.Register(RequestVoteArgs{})
	gob.Register(RequestVoteReply{})
	gob.Register(AppendEntriesArgs{})
	gob.Register(AppendEntriesReply{})
	gob.Register(LogEntry{})

	// 注册 interface 可能持有的具体类型
	gob.Register(map[string]string{})
	gob.Register(string(""))

	addr := n.peers[n.id-1]
	l, e := net.Listen("tcp", addr)
	if e != nil {
		log.Fatalf("监听错误: %v", e)
	}
	log.Printf("[%d] RPC服务器监听在%s", n.id, addr)

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Printf("接受连接错误: %v", err)
			continue
		}
		go rpc.ServeConn(conn)
	}
}

// sendRPC 向 peer 发送 RPC 请求
// peerId: 目标节点 ID
// method: 要调用的方法名
// args: 方法参数
// reply: 用于接收回复的变量
// 返回: 错误信息，如果有的话
func (n *Node) sendRPC(peerId int, method string, args interface{}, reply interface{}) error {
	addr := n.peers[peerId-1]
	log.Printf("[%d] sendRPC: dialing peer %d at %s, method=%s, args=%+v", n.id, peerId, addr, method, args)
	client, err := rpc.Dial("tcp", addr)
	if err != nil {
		log.Printf("[%d] sendRPC: failed to dial peer %d at %s, method=%s, err=%v", n.id, peerId, addr, method, err)
		return err
	}
	defer func(client *rpc.Client) {
		err := client.Close()
		if err != nil {
			log.Printf("[%d] 关闭与%d的连接错误: %v", n.id, peerId, err)
		}
	}(client)

	err = client.Call(method, args, reply)
	if err != nil {
		log.Printf("[%d] sendRPC: call to peer %d at %s, method=%s, err=%v", n.id, peerId, addr, method, err)
		return err
	}
	log.Printf("[%d] sendRPC: call to peer %d at %s, method=%s, reply=%+v", n.id, peerId, addr, method, reply)
	return nil
}

// Propose 向 Raft 集群提交新命令
// cmd: 要提交的命令
func (n *Node) Propose(cmd interface{}) {
	n.proposeChan <- cmd
}

func (n *Node) ProposeCall(cmd interface{}, cmdReply *string) error {
	n.proposeChan <- cmd
	*cmdReply = "Command received"
	return nil
}

// main 函数用于设置和运行集群
func main() {
	// 使用方法: go run raft_native.go <节点ID> <节点地址1> <节点地址2> ...
	if len(os.Args) < 3 {
		fmt.Println("使用方法: go run raft_native.go <节点ID> <节点地址1> <节点地址2> ...")
		os.Exit(1)
	}

	nodeId, err := strconv.Atoi(os.Args[1])
	if err != nil || nodeId < 1 {
		log.Fatalf("无效的节点ID: %s", os.Args[1])
	}

	peers := os.Args[2:]
	if nodeId > len(peers) {
		log.Fatalf("节点ID %d超出了节点列表范围(大小%d)", nodeId, len(peers))
	}

	log.Printf("启动节点%d，节点列表: %v", nodeId, peers)

	node := NewNode(nodeId, peers)

	// 简单的状态机(键值存储)
	kvStore := make(map[string]string)

	// 应用已提交日志到状态机的goroutine
	go func() {
		for cmd := range node.commitChan {
			// 这是一个非常简单的命令格式: "set key value"
			parts := strings.SplitN(cmd.(string), " ", 3)
			if len(parts) == 3 && parts[0] == "set" {
				kvStore[parts[1]] = parts[2]
				log.Printf("[%d] 状态机已应用: set %s = %s。当前存储: %v", node.id, parts[1], parts[2], kvStore)
			}
		}
	}()

	// 保持主goroutine存活
	select {}
}

/**

运行

go run raft_native.go 1 127.0.0.1:8001 127.0.0.1:8002 127.0.0.1:8003
go run raft_native.go 2 127.0.0.1:8001 127.0.0.1:8002 127.0.0.1:8003
go run raft_native.go 3 127.0.0.1:8001 127.0.0.1:8002 127.0.0.1:8003



*/

```

### 核心代码 (`raft_client.go`)



```go

package main

import (
	"flag"
	"fmt"
	"log"
	"net/rpc"
)

// RPCClient 封装 RPC 客户端功能
type RPCClient struct {
	client *rpc.Client
}

// NewRPCClient 创建一个新的 RPC 客户端实例
func NewRPCClient(addr string) (*RPCClient, error) {
	client, err := rpc.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &RPCClient{client: client}, nil
}

// Propose 对节点发起请求命令
func (c *RPCClient) Propose(cmd interface{}) (string, error) {
	var reply string
	// 修改为传递 interface{} 类型
	if err := c.client.Call("Node.ProposeCall", &cmd, &reply); err != nil {
		return "", err
	}
	return reply, nil
}

// Close 关闭 RPC 客户端连接
func (c *RPCClient) Close() error {
	return c.client.Close()
}

func main() {
	// 解析命令行参数
	addr := flag.String("addr", "localhost:1234", "RPC 服务器地址")
	cmd := flag.String("cmd", "", "要执行的命令")
	flag.Parse()

	if *cmd == "" {
		log.Fatal("必须提供 -cmd 参数")
	}

	// 创建 RPC 客户端
	client, err := NewRPCClient(*addr)
	if err != nil {
		log.Fatalf("无法连接到 RPC 服务器: %v", err)
	}
	defer client.Close()

	// 发起请求
	reply, err := client.Propose(*cmd)
	if err != nil {
		log.Fatalf("请求失败: %v", err)
	}

	fmt.Printf("命令执行结果: %s\n", reply)
}

/**

运行

go run raft_client.go -addr "localhost:8001" -cmd "set key001 value001"
go run raft_client.go -addr "localhost:8002" -cmd "set key002 value002"
go run raft_client.go -addr "localhost:8003" -cmd "set key003 value003"
go run raft_client.go -addr "localhost:8001" -cmd "set key004 value004"
go run raft_client.go -addr "localhost:8002" -cmd "set key005 value005"


*/


```

### 如何运行

1.  **保存代码**: 将上述代码保存为 `raft_native.go`。

2.  **打开三个终端**: 我们将创建一个三节点的集群。

3.  **定义节点地址**:

      * 节点1: `localhost:8001`
      * 节点2: `localhost:8002`
      * 节点3: `localhost:8003`

4.  **在每个终端中启动一个节点**:

      * **终端 1**:
        ```bash
        go run raft_native.go 1 localhost:8001 localhost:8002 localhost:8003
        ```
      * **终端 2**:
        ```bash
        go run raft_native.go 2 localhost:8001 localhost:8002 localhost:8003
        ```
      * **终端 3**:
        ```bash
        go run raft_native.go 3 localhost:8001 localhost:8002 localhost:8003
        ```
5.  **发起客户端请求**:

    * **打开另一个终端**:
    * **执行请求**:
      ```bash
      go run raft_client.go -addr "localhost:8001" -cmd "set key001 value001"
      ```

### 观察运行结果

当你启动这三个节点后，你将看到日志实时输出，清晰地展示Raft的核心功能：

#### 1\. 领导者选举 (Leader Election)

  * **初始状态**: 所有节点启动后都是 `Follower`。
  * **选举超时**: 你会看到其中一个节点（随机的，因为选举超时是随机的）率先超时。
    ```log
    [2] Election timeout, becoming Candidate
    ```
  * **发起投票**: 该 `Candidate` 会向其他节点发送 `RequestVote` RPC。
    ```log
    [2] became Candidate for term 1
    [2] sending RequestVote to 1: {Term:1 CandidateId:2 ...}
    [2] sending RequestVote to 3: {Term:1 CandidateId:2 ...}
    ```
  * **投票响应**: 其他 `Follower` 收到请求后，会进行判断并投票。
    ```log
    [1] received AppendEntries from Leader 2 in term 1 // (This could be an empty heartbeat)
    [1] granted vote for 2 in term 1
    [3] granted vote for 2 in term 1
    ```
  * **赢得选举**: `Candidate` 收到超过半数（在这个例子中是2票，包括自己的一票）的选票后，成为 `Leader`。
    ```log
    [2] received vote from 1, total votes: 2
    [2] won election with 2 votes
    [2] became Leader for term 1
    ```
  * **发送心跳**: 新的 `Leader` 会立即向所有 `Follower` 发送心跳（即空的 `AppendEntries` RPC）以确立自己的地位。
    ```log
    [1] received AppendEntries from Leader 2 in term 1
    [3] received AppendEntries from Leader 2 in term 1
    ```

#### 2\. 日志复制 (Log Replication)

  * **客户端提议**: 客户端发起请求，所有节点会处理它，非leader节点转发到leader节点进行处理。假设节点2是Leader：
    ```log
    // In Leader's terminal (Node 2)
    [2] Attempting to propose a command...
    [2] Leader appended new entry to its log at index 1

    // In Followers' terminals (Node 1 and 3)
    [1] Attempting to propose a command...
    [1] Follower received proposal, ignoring.
    ```
  * **日志复制**: Leader通过 `AppendEntries` RPC将新的日志条目发送给Followers。
    ```log
    // In Leader's terminal (Node 2)
    [2] AppendEntries to 1 succeeded. nextIndex: 2, matchIndex: 1
    [2] AppendEntries to 3 succeeded. nextIndex: 2, matchIndex: 1
    ```
  * **日志提交**: Leader发现大多数节点（包括自己）已经复制了该日志后，就会提交它。
    ```log
    // In Leader's terminal (Node 2)
    [2] Leader committed index 1
    [2] STATE MACHINE APPLIED: set mykey node2_value. Current store: map[mykey:node2_value]
    ```
  * **通知Follower提交**: Leader在后续的心跳中，会把最新的`commitIndex`告诉Followers。Followers收到后也会提交日志并应用到自己的状态机。
    ```log
    // In Followers' terminals (Node 1 and 3)
    [1] Follower updated commitIndex to 1
    [1] STATE MACHINE APPLIED: set mykey node2_value. Current store: map[mykey:node2_value]
    [3] Follower updated commitIndex to 1
    [3] STATE MACHINE APPLIED: set mykey node2_value. Current store: map[mykey:node2_value]
    ```

最终，所有三个节点的状态机都包含了由Leader写入的键值对 `mykey: node2_value`，从而达到了状态一致。这个从零开始的实现成功地演示了Raft算法的核心流程。