---
title: "如何快速设计一个任务调度系统"
date: 2025-06-13T11:00:00+08:00
draft: false
toc: true
featured: false
usePageBundles: true
categories: ["阅读/笔记/问题"]
tags: ["调度系统", "golang"]
---

## 问题


现有业务中存在很多指定时间执行和循环时间执行的任务，这些任务需要被调度执行，并且需要被管理，比如查看任务状态，查看任务日志，查看任务执行时间，查看任务执行结果等等。
这里需要一个任务调度系统去实现它，而一个功能完备的任务调度系统如何快速实现？


## 回答

### 1. 核心机制是什么

一个功能完备的任务调度系统，其核心机制围绕着**任务**的生命周期管理和**分布式协调**展开。它主要由以下几个核心组件及其相互协作机制构成：

#### 1.1 任务 (Job/Task)

*   **定义**: 任务是调度系统的基本工作单元。它包含了需要被执行的具体逻辑、执行策略和相关配置。
*   **关键属性**:
    *   **任务 ID**: 唯一标识一个任务。
    *   **任务名称/描述**: 用于识别和理解任务的用途。
    *   **任务类型**: 指明任务的具体执行方式，例如 HTTP 回调、Shell 命令、Go 函数调用、RPC 调用等。
    *   **调度策略**: 定义任务的触发方式，常见的有：
        *   **定时任务**: 在指定时间点或按照 Cron 表达式周期性执行。
        *   **循环任务**: 每隔固定时间间隔执行一次。
        *   **一次性任务**: 只执行一次。
        *   **依赖任务**: 依赖其他任务执行成功后触发。
    *   **执行参数**: 任务执行时所需的输入参数。
    *   **路由策略**: 在有多个执行器时，任务如何被分发到哪个执行器上执行（如随机、轮询、指定机器等）。
    *   **超时时间**: 任务执行允许的最大时间，超过则认为失败。
    *   **重试策略**: 任务失败后的重试次数和间隔。
*   **状态**: 任务在系统中的不同生命周期阶段，如"待调度"、"调度中"、"执行中"、"执行成功"、"执行失败"、"暂停"等。

#### 1.2 调度器 (Scheduler/Dispatcher)

*   **核心功能**: 负责根据任务的调度策略，在预设的时间点触发任务的执行。它是任务调度系统的大脑。
*   **职责**:
    *   **任务加载与解析**: 从持久化存储中加载任务定义，并解析其调度策略。
    *   **时间管理**: 维护一个内部时钟或时间轮，精确地在任务预设时间点触发任务。
    *   **任务分发**: 将达到调度时间的任务发送给合适的执行器进行执行。这通常涉及与执行器的通信机制（如 RPC、HTTP）。
    *   **调度策略支持**: 能够解析并执行 Cron 表达式、固定间隔等多种调度规则。
    *   **任务去重**: 在分布式部署时，确保同一个任务不会被多个调度器重复触发。
*   **分布式挑战**: 在多个调度器实例存在时，需要解决任务的"抢占式调度"问题，通常通过分布式锁或主备模式来确保任务的唯一调度。

#### 1.3 执行器 (Executor/Worker)

*   **核心功能**: 负责接收调度器发送的任务指令，并实际执行任务中定义的业务逻辑。它是任务调度系统的"手脚"。
*   **职责**:
    *   **任务注册**: 启动时向调度中心注册自身，以便调度器知道有哪些可用的执行器。
    *   **任务接收**: 监听并接收调度器发送的任务执行请求。
    *   **任务执行**: 根据任务类型和参数，调用相应的处理逻辑来执行任务。
    *   **结果上报**: 任务执行完成后，将执行结果（成功/失败、日志、返回值）回调给调度中心或调度器。
    *   **任务隔离**: 确保不同任务之间执行的资源隔离，避免相互影响。
*   **伸缩性**: 执行器通常可以独立部署和横向扩展，以应对不断增长的任务执行量。

#### 1.4 调度中心 (Admin/Coordinator)

*   **核心功能**: 提供统一的任务管理界面和 API，是整个系统的控制面板。它负责任务的配置、管理、监控和日志记录。
*   **职责**:
    *   **任务管理**: 任务的创建、编辑、删除、暂停、恢复等。
    *   **执行器管理**: 注册、注销、监控执行器的健康状态。
    *   **任务实例管理**: 跟踪每次任务执行的详细信息，包括执行时间、执行器、状态、日志等。
    *   **日志管理**: 收集和存储所有任务的执行日志，提供查询和分析功能。
    *   **告警**: 在任务执行失败或出现异常时，触发告警通知。
    *   **权限管理**: 对不同用户提供不同的操作权限。
*   **架构**: 通常是一个独立的 Web 应用或服务，通过 API 与调度器和执行器进行交互。

#### 1.5 数据持久化 (Persistence)

*   **核心功能**: 存储任务的定义、执行历史、日志以及系统配置等关键数据，确保系统重启或故障后数据不丢失。
*   **存储内容**:
    *   任务元数据 (Job Metadata)
    *   任务执行日志 (Job Execution Logs)
    *   执行器注册信息 (Executor Registration)
    *   系统配置 (System Configurations)
*   **常见选择**: 关系型数据库（如 MySQL、PostgreSQL）是主流选择，提供事务支持和复杂查询能力；NoSQL 数据库（如 MongoDB）可用于日志存储；缓存系统（如 Redis）可用于分布式锁、临时任务状态等。

#### 1.6 分布式协调与高可用

*   **调度器高可用**: 通常采用主备模式或分布式选举机制（如基于 ZooKeeper、etcd、Redis）来确保只有一个调度器实例在特定时间点负责调度任务，避免重复调度。当主调度器故障时，备用调度器能够快速接管。
*   **任务分发**: 调度器将任务分发给空闲或负载较低的执行器。
*   **心跳机制**: 调度中心和调度器通过心跳机制监控执行器的存活状态。
*   **故障转移**: 当执行器故障时，调度器能够将未完成的任务重新分配给其他健康的执行器。
*   **幂等性**: 任务执行逻辑应具备幂等性，即多次执行相同任务不会产生副作用，以应对重复调度或重试场景。
*   **一致性**: 确保在分布式环境下，任务状态、执行结果等数据的一致性。

这些核心机制共同协作，构成了一个稳定、可靠、可扩展的任务调度系统。

### 2. 核心机制用 Golang 如何实现

在 Go 语言中实现任务调度系统的核心机制，可以充分利用 Go 的并发特性（Goroutine 和 Channel）、丰富的标准库以及强大的第三方库。

#### 2.1 任务定义与管理

任务的定义通常使用 Go 的结构体 (struct) 来表示。为了便于在不同组件间传输和持久化，可以利用 `encoding/json` 或 `protobuf` 进行序列化和反序列化。

```go
// task/model.go
package task

import (
	"time"
)

// JobType 定义任务的类型，例如 HTTP_CALLBACK, SHELL_COMMAND, GO_FUNCTION
type JobType string

const (
	JobTypeHTTPCallback JobType = "HTTP_CALLBACK"
	JobTypeShellCommand JobType = "SHELL_COMMAND"
	JobTypeGoFunction   JobType = "GO_FUNCTION" // 表示由执行器内部Go函数直接执行
)

// JobStatus 定义任务的状态
type JobStatus string

const (
	JobStatusPending   JobStatus = "PENDING"   // 待调度
	JobStatusScheduling JobStatus = "SCHEDULING" // 调度中
	JobStatusRunning   JobStatus = "RUNNING"   // 执行中
	JobStatusSuccess   JobStatus = "SUCCESS"   // 执行成功
	JobStatusFailed    JobStatus = "FAILED"    // 执行失败
	JobStatusPaused    JobStatus = "PAUSED"    // 已暂停
)

// Job 定义一个任务的结构体
type Job struct {
	ID          string    `json:"id"`           // 任务唯一ID
	Name        string    `json:"name"`         // 任务名称
	Description string    `json:"description"`  // 任务描述
	JobType     JobType   `json:"job_type"`     // 任务类型
	CronSpec    string    `json:"cron_spec"`    // Cron表达式，用于定时任务
	Interval    int       `json:"interval"`     // 间隔时间（秒），用于循环任务
	Payload     string    `json:"payload"`      // 任务执行参数（JSON字符串或其他格式）
	ExecutorID  string    `json:"executor_id"`  // 指定执行器ID，可选
	Timeout     int       `json:"timeout"`      // 执行超时时间（秒）
	RetryTimes  int       `json:"retry_times"`  // 重试次数
	Status      JobStatus `json:"status"`       // 任务状态
	CreatedAt   time.Time `json:"created_at"`   // 创建时间
	UpdatedAt   time.Time `json:"updated_at"`   // 更新时间
}

// JobExecutionLog 记录任务的每次执行日志
type JobExecutionLog struct {
	ID          string    `json:"id"`
	JobID       string    `json:"job_id"`
	ExecutorID  string    `json:"executor_id"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	Status      JobStatus `json:"status"` // 本次执行的状态 (SUCCESS/FAILED)
	Output      string    `json:"output"` // 任务执行的输出/日志
	Error       string    `json:"error"`  // 错误信息
	Duration    int64     `json:"duration"` // 执行耗时（毫秒）
	CreatedAt   time.Time `json:"created_at"`
}

// ExecutorInfo 记录执行器的信息
type ExecutorInfo struct {
	ID        string    `json:"id"`        // 执行器唯一ID
	Address   string    `json:"address"`   // 执行器地址 (e.g., "http://127.0.0.1:8081")
	Heartbeat time.Time `json:"heartbeat"` // 最后一次心跳时间
	Status    string    `json:"status"`    // 执行器状态 (e.g., "UP", "DOWN")
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

```

#### 2.2 调度器 (Scheduler) 实现

调度器的核心是根据时间触发任务。Go 语言的 `time` 包可以用于基本的定时，但对于复杂的 Cron 表达式，推荐使用第三方库。

**a. 时间管理与 Cron 表达式解析**

我们可以使用 `github.com/robfig/cron/v3` 这个优秀的库来解析和管理 Cron 任务。

```go
// scheduler/scheduler.go
package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"your_project_name/task" // 假设你的任务定义在 your_project_name/task 包中
)

// Scheduler 结构体
type Scheduler struct {
	cron       *cron.Cron
	jobStore   task.JobStore // 用于获取和更新任务状态的接口
	executorMgr ExecutorManager // 用于管理和选择执行器的接口
	// 分布式锁，用于多调度器实例环境
	distributedLocker DistributedLocker
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
}

// NewScheduler 创建一个新的调度器实例
func NewScheduler(jobStore task.JobStore, executorMgr ExecutorManager, locker DistributedLocker) *Scheduler {
	c := cron.New(cron.WithChain(
		cron.Recover(cron.DefaultLogger), // 任务panic时恢复
		cron.DelayIfStillRunning(cron.DefaultLogger), // 如果上一次任务还在运行，则延迟本次任务
	))
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		cron:       c,
		jobStore:   jobStore,
		executorMgr: executorMgr,
		distributedLocker: locker,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start 启动调度器
func (s *Scheduler) Start() {
	log.Println("Scheduler started...")
	s.cron.Start()
	// 启动一个Goroutine定期加载和注册任务
	go s.loadAndRegisterJobs()
	<-s.ctx.Done() // 等待上下文取消
	log.Println("Scheduler stopped.")
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	s.cancel()
	<-s.cron.Stop().Done() // 等待所有正在执行的任务完成
	log.Println("Cron scheduler stopped.")
}

// loadAndRegisterJobs 定期从数据库加载任务并注册到cron
func (s *Scheduler) loadAndRegisterJobs() {
	ticker := time.NewTicker(30 * time.Second) // 每30秒加载一次任务
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			// 在分布式环境中，这里需要获取分布式锁，确保只有一个调度器进行任务加载和注册
			if s.distributedLocker != nil {
				lockKey := "scheduler_job_load_lock"
				locked, err := s.distributedLocker.Lock(lockKey, 10*time.Second) // 尝试获取锁
				if err != nil {
					log.Printf("Failed to acquire distributed lock for job loading: %v", err)
					continue
				}
				if !locked {
					log.Println("Another scheduler instance is loading jobs, skipping.")
					continue // 未获取到锁，跳过本次加载
				}
				defer func() {
					if err := s.distributedLocker.Unlock(lockKey); err != nil {
						log.Printf("Failed to release distributed lock: %v", err)
					}
				}()
			}

			s.mu.Lock() // 保护对cron实例的并发操作
			log.Println("Loading and registering jobs...")
			jobs, err := s.jobStore.GetRunnableJobs() // 获取所有待运行的任务
			if err != nil {
				log.Printf("Failed to get runnable jobs: %v", err)
				s.mu.Unlock()
				continue
			}

			// 清除已删除或暂停的任务
			s.clearInactiveJobs(jobs)

			for _, job := range jobs {
				s.addOrUpdateJob(job)
			}
			s.mu.Unlock()
			log.Println("Job loading and registration complete.")
		}
	}
}

// addOrUpdateJob 添加或更新任务到cron
func (s *Scheduler) addOrUpdateJob(job task.Job) {
	entryID, exists := s.getEntryID(job.ID) // 检查任务是否已存在
	if exists {
		// 如果任务已存在且Cron表达式未改变，则跳过
		entry := s.cron.Entry(entryID)
		if entry.Schedule.String() == job.CronSpec { // 这是一个简化的比较，实际可能需要更严谨的比较
			return
		}
		// 如果Cron表达式改变，则移除旧任务，重新添加
		s.cron.Remove(entryID)
	}

	// 添加新任务或更新任务
	// 任务执行逻辑，这是一个闭包，捕获了job的信息
	jobFunc := func() {
		// 在这里，调度器选择一个合适的执行器来执行任务
		log.Printf("Job %s (%s) triggered.", job.Name, job.ID)
		executor, err := s.executorMgr.SelectExecutor(job.ExecutorID) // 根据ID或路由策略选择执行器
		if err != nil {
			log.Printf("Failed to select executor for job %s: %v", job.ID, err)
			s.jobStore.LogExecution(job.ID, "", task.JobStatusFailed, "", fmt.Sprintf("Failed to select executor: %v", err), 0)
			return
		}

		// 通过RPC/HTTP调用执行器接口执行任务
		go s.executeJobOnExecutor(job, executor)
	}

	var entryIDToAdd cron.EntryID
	var err error
	if job.CronSpec != "" {
		entryIDToAdd, err = s.cron.AddFunc(job.CronSpec, jobFunc)
	} else if job.Interval > 0 {
		entryIDToAdd, err = s.cron.AddJob(cron.Every(time.Duration(job.Interval)*time.Second), cron.FuncJob(jobFunc))
	} else {
		log.Printf("Job %s (%s) has no valid cron spec or interval.", job.Name, job.ID)
		return
	}

	if err != nil {
		log.Printf("Failed to add job %s (%s) to cron: %v", job.Name, job.ID, err)
		return
	}
	s.setEntryID(job.ID, entryIDToAdd)
	log.Printf("Job %s (%s) added/updated with EntryID: %d", job.Name, job.ID, entryIDToAdd)
}

// clearInactiveJobs 移除不再活跃的任务
func (s *Scheduler) clearInactiveJobs(activeJobs []task.Job) {
	activeJobIDs := make(map[string]struct{})
	for _, job := range activeJobs {
		activeJobIDs[job.ID] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 遍历当前cron中所有的Entry，如果其JobID不在activeJobIDs中，则移除
	for _, entry := range s.cron.Entries() {
		// 获取EntryID对应的JobID，这需要我们维护一个映射关系
		jobID, exists := s.getJobIDByEntryID(entry.ID)
		if !exists { // 可能是新的Entry，或者EntryID没有对应的JobID，则跳过
			continue
		}
		if _, isActive := activeJobIDs[jobID]; !isActive {
			s.cron.Remove(entry.ID)
			s.removeEntryID(jobID)
			log.Printf("Removed inactive job with JobID: %s, EntryID: %d", jobID, entry.ID)
		}
	}
}


// executeJobOnExecutor 实际调用执行器执行任务
func (s *Scheduler) executeJobOnExecutor(job task.Job, executor task.ExecutorInfo) {
	logID, err := s.jobStore.LogExecution(job.ID, executor.ID, task.JobStatusRunning, "", "", 0)
	if err != nil {
		log.Printf("Failed to log job start for %s: %v", job.ID, err)
		return
	}

	// 模拟RPC/HTTP调用执行器
	// 实际中会使用gRPC client或HTTP client
	result, err := s.executorMgr.Execute(executor, job) // 这是一个抽象的执行器调用方法
	if err != nil {
		log.Printf("Failed to execute job %s on executor %s: %v", job.ID, executor.ID, err)
		s.jobStore.UpdateExecutionLog(logID, task.JobStatusFailed, "", fmt.Sprintf("Execution failed: %v", err), result.Duration)
		return
	}

	if result.Status == task.JobStatusSuccess {
		log.Printf("Job %s executed successfully on executor %s.", job.ID, executor.ID)
		s.jobStore.UpdateExecutionLog(logID, task.JobStatusSuccess, result.Output, "", result.Duration)
	} else {
		log.Printf("Job %s failed on executor %s: %s", job.ID, executor.ID, result.Error)
		s.jobStore.UpdateExecutionLog(logID, task.JobStatusFailed, result.Output, result.Error, result.Duration)
	}

	// 更新任务在数据库中的状态，例如，对于一次性任务，执行完成后更新为成功或失败
	if job.CronSpec == "" && job.Interval == 0 {
		s.jobStore.UpdateJobStatus(job.ID, result.Status)
	}
}

// 辅助函数，维护 JobID 到 EntryID 的映射，用于快速查找和更新
// 在实际项目中，这个映射可能需要持久化或使用更健壮的并发结构
var jobIDToEntryID = make(map[string]cron.EntryID)
var entryIDToJobID = make(map[cron.EntryID]string)
var entryIDMapMu sync.RWMutex

func (s *Scheduler) getEntryID(jobID string) (cron.EntryID, bool) {
	entryIDMapMu.RLock()
	defer entryIDMapMu.RUnlock()
	id, ok := jobIDToEntryID[jobID]
	return id, ok
}

func (s *Scheduler) setEntryID(jobID string, entryID cron.EntryID) {
	entryIDMapMu.Lock()
	defer entryIDMapMu.Unlock()
	jobIDToEntryID[jobID] = entryID
	entryIDToJobID[entryID] = jobID
}

func (s *Scheduler) removeEntryID(jobID string) {
	entryIDMapMu.Lock()
	defer entryIDMapMu.Unlock()
	if entryID, ok := jobIDToEntryID[jobID]; ok {
		delete(jobIDToEntryID, jobID)
		delete(entryIDToJobID, entryID)
	}
}

func (s *Scheduler) getJobIDByEntryID(entryID cron.EntryID) (string, bool) {
	entryIDMapMu.RLock()
	defer entryIDMapMu.RUnlock()
	jobID, ok := entryIDToJobID[entryID]
	return jobID, ok
}


// JobStore 接口定义了调度器与任务存储交互的方法
type JobStore interface {
	GetRunnableJobs() ([]task.Job, error)
	GetJobByID(id string) (task.Job, error)
	UpdateJobStatus(id string, status task.JobStatus) error
	LogExecution(jobID, executorID string, status task.JobStatus, output, errMsg string, duration int64) (string, error)
	UpdateExecutionLog(logID string, status task.JobStatus, output, errMsg string, duration int64) error
	// Add other methods for CRUD operations on jobs if needed
}

// ExecutorManager 接口定义了调度器与执行器管理交互的方法
type ExecutorManager interface {
	SelectExecutor(executorID string) (task.ExecutorInfo, error) // 根据策略选择执行器
	Execute(executor task.ExecutorInfo, job task.Job) (ExecutorResult, error) // 调用执行器执行任务
	// Add other methods for executor registration, heartbeat, etc.
}

// ExecutorResult 定义执行器返回的结果
type ExecutorResult struct {
	Status   task.JobStatus `json:"status"`
	Output   string         `json:"output"`
	Error    string         `json:"error"`
	Duration int64          `json:"duration"` // 毫秒
}

// DistributedLocker 定义分布式锁接口
type DistributedLocker interface {
	Lock(key string, expiration time.Duration) (bool, error)
	Unlock(key string) error
}

```

**b. 任务调度与分发**

上述 `Scheduler` 中的 `executeJobOnExecutor` 方法内部就是任务分发的逻辑。当任务触发时，调度器会：

1.  **选择执行器**: 通过 `ExecutorManager` 接口根据预设的路由策略（如指定执行器 ID、轮询、随机、负载均衡等）选择一个健康的执行器。
2.  **构建请求**: 准备好任务执行所需的参数（任务 ID、payload 等）。
3.  **发送请求**: 使用 HTTP 或 gRPC 等方式向选定的执行器发送执行任务的请求。
4.  **处理结果**: 接收执行器返回的执行结果，并更新任务日志和任务状态。

#### 2.3 执行器 (Executor) 实现

执行器是实际执行任务逻辑的组件。它通常是一个独立的 Go 服务。

```go
// executor/executor.go
package executor

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"your_project_name/task" // 假设你的任务定义在 your_project_name/task 包中
)

// ExecutorConfig 执行器配置
type ExecutorConfig struct {
	ID        string
	Port      string
	AdminAddr string // 调度中心地址
}

// Executor 结构体
type Executor struct {
	cfg        ExecutorConfig
	router     *gin.Engine
	httpClient *http.Client
	// goroutine池用于限制并发执行的任务数量
	workerPool chan struct{}
	mu         sync.Mutex
	// 注册到调度中心的服务发现客户端
	serviceDiscoveryClient ServiceDiscoveryClient
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewExecutor 创建一个新的执行器实例
func NewExecutor(cfg ExecutorConfig, sdc ServiceDiscoveryClient) *Executor {
	r := gin.Default()
	r.Use(gin.Recovery()) // 崩溃恢复

	e := &Executor{
		cfg:        cfg,
		router:     r,
		httpClient: &http.Client{Timeout: 30 * time.Second}, // 设置HTTP客户端超时
		workerPool: make(chan struct{}, 100), // 限制同时执行的任务数量为100
		serviceDiscoveryClient: sdc,
	}
	e.setupRoutes()
	e.ctx, e.cancel = context.WithCancel(context.Background())
	return e
}

// setupRoutes 设置执行器对外暴露的HTTP接口
func (e *Executor) setupRoutes() {
	e.router.POST("/execute", e.handleExecuteJob)
	e.router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "UP"})
	})
}

// Start 启动执行器
func (e *Executor) Start() {
	log.Printf("Executor %s starting on :%s", e.cfg.ID, e.cfg.Port)

	// 启动心跳Goroutine
	go e.startHeartbeat()

	// 启动HTTP服务
	server := &http.Server{
		Addr:    ":" + e.cfg.Port,
		Handler: e.router,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Executor server listen error: %v", err)
		}
	}()

	<-e.ctx.Done() // 等待上下文取消
	log.Println("Shutting down executor server...")

	// 优雅关闭HTTP服务
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Executor server forced to shutdown: %v", err)
	}
	log.Println("Executor stopped.")
}

// Stop 停止执行器
func (e *Executor) Stop() {
	e.cancel()
}

// handleExecuteJob 处理任务执行请求
func (e *Executor) handleExecuteJob(c *gin.Context) {
	var req struct {
		JobID      string `json:"job_id"`
		JobType    task.JobType `json:"job_type"`
		Payload    string `json:"payload"`
		Timeout    int    `json:"timeout"` // 任务超时时间
		LogID      string `json:"log_id"`  // 调度器传来的日志ID
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	log.Printf("Received job %s (%s) for execution.", req.JobID, req.JobType)

	// 使用goroutine池来限制并发
	select {
	case e.workerPool <- struct{}{}: // 尝试获取一个 worker slot
		go func() {
			defer func() { <-e.workerPool }() // 释放 worker slot
			e.executeTask(req.JobID, req.JobType, req.Payload, req.Timeout, req.LogID)
		}()
		c.JSON(http.StatusOK, gin.H{"message": "Job received and started execution."})
	default:
		errMsg := "Executor is busy, too many concurrent tasks."
		log.Printf("Job %s rejected: %s", req.JobID, errMsg)
		e.reportExecutionResult(req.JobID, req.LogID, task.JobStatusFailed, "", errMsg, 0)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errMsg})
	}
}

// executeTask 实际执行任务逻辑
func (e *Executor) executeTask(jobID string, jobType task.JobType, payload string, timeout int, logID string) {
	startTime := time.Now()
	var status task.JobStatus
	var output string
	var errMsg string

	// 设置任务执行的上下文和超时
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	defer func() {
		duration := time.Since(startTime).Milliseconds()
		e.reportExecutionResult(jobID, logID, status, output, errMsg, duration)
	}()

	switch jobType {
	case task.JobTypeHTTPCallback:
		// 执行HTTP回调任务
		log.Printf("Executing HTTP_CALLBACK for job %s with payload: %s", jobID, payload)
		// 假设payload是包含URL的JSON
		var httpReqPayload struct {
			URL    string            `json:"url"`
			Method string            `json:"method"`
			Headers map[string]string `json:"headers"`
			Body   string            `json:"body"`
		}
		if err := c.BindJSON(&req); err != nil { // 这里应该用一个独立的结构体来解析
			status = task.JobStatusFailed
			errMsg = fmt.Sprintf("Invalid HTTP payload: %v", err)
			return
		}
		req, err := http.NewRequestWithContext(ctx, httpReqPayload.Method, httpReqPayload.URL, nil)
		if err != nil {
			status = task.JobStatusFailed
			errMsg = fmt.Sprintf("Failed to create HTTP request: %v", err)
			return
		}
		for k, v := range httpReqPayload.Headers {
			req.Header.Set(k, v)
		}
		if httpReqPayload.Body != "" {
			req.Body = io.NopCloser(bytes.NewBufferString(httpReqPayload.Body))
		}

		resp, err := e.httpClient.Do(req)
		if err != nil {
			status = task.JobStatusFailed
			errMsg = fmt.Sprintf("HTTP request failed: %v", err)
			if ctx.Err() == context.DeadlineExceeded {
				errMsg = "HTTP request timed out."
			}
			return
		}
		defer resp.Body.Close()

		bodyBytes, _ := io.ReadAll(resp.Body)
		output = string(bodyBytes)
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			status = task.JobStatusSuccess
		} else {
			status = task.JobStatusFailed
			errMsg = fmt.Sprintf("HTTP status code %d: %s", resp.StatusCode, output)
		}

	case task.JobTypeShellCommand:
		// 执行Shell命令任务
		log.Printf("Executing SHELL_COMMAND for job %s with command: %s", jobID, payload)
		cmd := exec.CommandContext(ctx, "/bin/bash", "-c", payload) // 注意安全性
		cmd.Stdout = &bytes.Buffer{}
		cmd.Stderr = &bytes.Buffer{}

		err := cmd.Run()
		output = cmd.Stdout.(*bytes.Buffer).String()
		if err != nil {
			status = task.JobStatusFailed
			errMsg = fmt.Sprintf("Shell command failed: %v, Stderr: %s", err, cmd.Stderr.(*bytes.Buffer).String())
			if ctx.Err() == context.DeadlineExceeded {
				errMsg = "Shell command timed out."
			}
			return
		}
		status = task.JobStatusSuccess

	case task.JobTypeGoFunction:
		// 执行Go函数任务
		log.Printf("Executing GO_FUNCTION for job %s with payload: %s", jobID, payload)
		// 这里需要根据payload来调用实际的Go函数。
		// 这通常意味着Go函数需要预先注册，并通过payload中的名称来查找调用。
		// 例如：
		// handler, ok := registeredGoFunctions[payload]
		// if !ok {
		//     status = task.JobStatusFailed
		//     errMsg = fmt.Sprintf("Go function '%s' not registered.", payload)
		//     return
		// }
		// funcOutput, funcErr := handler(ctx, payload) // 假设handler接收context和payload
		// output = funcOutput
		// if funcErr != nil {
		//     status = task.JobStatusFailed
		//     errMsg = fmt.Sprintf("Go function execution failed: %v", funcErr)
		//     return
		// }
		// status = task.JobStatusSuccess
		status = task.JobStatusSuccess // 模拟成功
		output = "Go function executed successfully (simulated)."

	default:
		status = task.JobStatusFailed
		errMsg = fmt.Sprintf("Unsupported job type: %s", jobType)
	}
}

// reportExecutionResult 上报任务执行结果给调度中心
func (e *Executor) reportExecutionResult(jobID, logID string, status task.JobStatus, output, errMsg string, duration int64) {
	reportURL := fmt.Sprintf("%s/api/job/log/update", e.cfg.AdminAddr) // 假设调度中心有这样的接口

	result := scheduler.ExecutorResult{
		Status:   status,
		Output:   output,
		Error:    errMsg,
		Duration: duration,
	}

	jsonData, err := json.Marshal(result)
	if err != nil {
		log.Printf("Failed to marshal execution result for job %s: %v", jobID, err)
		return
	}

	req, err := http.NewRequest("POST", reportURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Failed to create result report request for job %s: %v", jobID, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		log.Printf("Failed to report execution result for job %s: %v", jobID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Failed to report execution result for job %s, status: %d, response: %s", jobID, resp.StatusCode, string(bodyBytes))
	} else {
		log.Printf("Successfully reported execution result for job %s (Status: %s)", jobID, status)
	}
}

// startHeartbeat 启动心跳机制，定期向调度中心注册/报告存活
func (e *Executor) startHeartbeat() {
	ticker := time.NewTicker(5 * time.Second) // 每5秒发送一次心跳
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			// 向调度中心注册或更新心跳
			err := e.serviceDiscoveryClient.RegisterExecutor(task.ExecutorInfo{
				ID:        e.cfg.ID,
				Address:   fmt.Sprintf("http://127.0.0.1:%s", e.cfg.Port), // 实际应该是可访问的外部地址
				Heartbeat: time.Now(),
				Status:    "UP",
			})
			if err != nil {
				log.Printf("Failed to send heartbeat to admin: %v", err)
			}
		}
	}
}

// ServiceDiscoveryClient 定义服务发现客户端接口
type ServiceDiscoveryClient interface {
	RegisterExecutor(info task.ExecutorInfo) error
	GetHealthyExecutors() ([]task.ExecutorInfo, error)
	// Add other methods for executor management
}

```

#### 2.4 调度中心 (Admin/Coordinator)

调度中心是整个系统的管理入口，通常是一个 Web 服务，提供 RESTful API 和用户界面。在 Go 语言中，可以使用 `gin` 或 `echo` 等 Web 框架来快速构建。

其核心功能包括：

*   **API 接口**:
    *   `/api/job`: 任务的增删改查。
    *   `/api/executor`: 执行器的注册、管理、健康检查。
    *   `/api/job/log`: 任务执行日志的查询。
    *   `/api/job/trigger`: 手动触发任务。
*   **数据库交互**: 使用 ORM 库（如 `GORM`）与数据库进行交互，进行数据存储和查询。
*   **数据模型**: 维护 `task.Job`、`task.JobExecutionLog`、`task.ExecutorInfo` 等数据模型。
*   **服务发现**: 接收执行器的注册和心跳，维护一个健康执行器列表，供调度器查询。

```go
// admin/server.go
package admin

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"your_project_name/task"
	"your_project_name/scheduler"
)

// AdminServerConfig 配置
type AdminServerConfig struct {
	Port string
}

// AdminServer 调度中心服务器
type AdminServer struct {
	cfg        AdminServerConfig
	router     *gin.Engine
	jobStore   task.JobStore // 任务存储接口
	executorStore ExecutorStore // 执行器存储接口 (需要新定义)
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewAdminServer 创建新的调度中心服务器
func NewAdminServer(cfg AdminServerConfig, js task.JobStore, es ExecutorStore) *AdminServer {
	r := gin.Default()
	r.Use(gin.Recovery())

	s := &AdminServer{
		cfg:        cfg,
		router:     r,
		jobStore:   js,
		executorStore: es,
	}
	s.setupRoutes()
	s.ctx, s.cancel = context.WithCancel(context.Background())
	return s
}

// setupRoutes 设置API路由
func (s *AdminServer) setupRoutes() {
	api := s.router.Group("/api")
	{
		// 任务管理API
		api.POST("/job", s.createJob)
		api.PUT("/job/:id", s.updateJob)
		api.DELETE("/job/:id", s.deleteJob)
		api.GET("/job/:id", s.getJob)
		api.GET("/jobs", s.listJobs)
		api.POST("/job/:id/pause", s.pauseJob)
		api.POST("/job/:id/resume", s.resumeJob)
		api.POST("/job/:id/trigger", s.triggerJobManually) // 手动触发

		// 执行器管理API (接收执行器心跳和注册)
		api.POST("/executor/register", s.registerExecutor)
		api.GET("/executors", s.listExecutors)

		// 任务日志API
		api.GET("/job/logs/:job_id", s.listJobLogs)
		api.POST("/job/log/update", s.updateJobLog) // 接收执行器回调的日志更新
	}
}

// Start 启动Admin Server
func (s *AdminServer) Start() {
	log.Printf("Admin Server starting on :%s", s.cfg.Port)
	server := &http.Server{
		Addr:    ":" + s.cfg.Port,
		Handler: s.router,
	}

	// 启动定期清理不健康执行器的Goroutine
	go s.cleanupInactiveExecutors()

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Admin server listen error: %v", err)
		}
	}()

	<-s.ctx.Done()
	log.Println("Shutting down admin server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Admin server forced to shutdown: %v", err)
	}
	log.Println("Admin Server stopped.")
}

// Stop 停止Admin Server
func (s *AdminServer) Stop() {
	s.cancel()
}

// API 实现 (示例，具体实现需要与jobStore和executorStore交互)

func (s *AdminServer) createJob(c *gin.Context) { /* ... */ }
func (s *AdminServer) updateJob(c *gin.Context) { /* ... */ }
func (s *AdminServer) deleteJob(c *gin.Context) { /* ... */ }
func (s *AdminServer) getJob(c *gin.Context) { /* ... */ }
func (s *AdminServer) listJobs(c *gin.Context) { /* ... */ }
func (s *AdminServer) pauseJob(c *gin.Context) { /* ... */ }
func (s *AdminServer) resumeJob(c *gin.Context) { /* ... */ }

// triggerJobManually 手动触发任务
func (s *AdminServer) triggerJobManually(c *gin.Context) {
	jobID := c.Param("id")
	job, err := s.jobStore.GetJobByID(jobID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	executor, err := s.executorStore.SelectHealthyExecutor(job.ExecutorID) // 调度中心选择一个执行器
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("No healthy executor available: %v", err)})
		return
	}

	// 模拟调度器调用执行器的逻辑，或者直接触发调度器执行
	// 实际中可能通过RPC通知调度器立即执行该任务
	// 这里简化为直接通过HTTP/RPC调用执行器
	go func() {
		log.Printf("Manually triggering job %s on executor %s...", job.ID, executor.ID)
		// 模拟执行器调用
		client := &http.Client{Timeout: time.Duration(job.Timeout)*time.Second + 5*time.Second} // 增加一点余量
		executeURL := fmt.Sprintf("%s/execute", executor.Address)

		reqBody := gin.H{
			"job_id": job.ID,
			"job_type": job.JobType,
			"payload": job.Payload,
			"timeout": job.Timeout,
			"log_id": "", // 首次触发，日志ID由执行器生成或由调度中心分配
		}
		jsonBytes, _ := json.Marshal(reqBody)
		req, err := http.NewRequest("POST", executeURL, bytes.NewBuffer(jsonBytes))
		if err != nil {
			log.Printf("Failed to create manual trigger request for job %s: %v", job.ID, err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Manual trigger failed for job %s: %v", job.ID, err)
			// TODO: 更新日志为失败
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			log.Printf("Manual trigger failed for job %s, status: %d, response: %s", job.ID, resp.StatusCode, string(bodyBytes))
			// TODO: 更新日志为失败
		} else {
			log.Printf("Manual trigger sent for job %s successfully.", job.ID)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "Job manually triggered."})
}

// registerExecutor 接收执行器注册和心跳
func (s *AdminServer) registerExecutor(c *gin.Context) {
	var execInfo task.ExecutorInfo
	if err := c.BindJSON(&execInfo); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}
	execInfo.Heartbeat = time.Now() // 记录心跳时间
	execInfo.Status = "UP"
	err := s.executorStore.SaveExecutor(execInfo)
	if err != nil {
		log.Printf("Failed to save/update executor %s: %v", execInfo.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register executor"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Executor registered/heartbeat received."})
}

func (s *AdminServer) listExecutors(c *gin.Context) {
	executors, err := s.executorStore.GetHealthyExecutors()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve executors"})
		return
	}
	c.JSON(http.StatusOK, executors)
}

func (s *AdminServer) listJobLogs(c *gin.Context) { /* ... */ }

// updateJobLog 接收执行器上报的任务执行结果
func (s *AdminServer) updateJobLog(c *gin.Context) {
	var result scheduler.ExecutorResult
	if err := c.BindJSON(&result); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}
	// 执行器上报的日志ID应与调度中心创建的日志ID对应
	// 这里简化为根据logID更新
	err := s.jobStore.UpdateExecutionLog(c.Query("log_id"), result.Status, result.Output, result.Error, result.Duration)
	if err != nil {
		log.Printf("Failed to update execution log %s: %v", c.Query("log_id"), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update execution log"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Execution log updated."})
}

// cleanupInactiveExecutors 定期清理不健康的执行器
func (s *AdminServer) cleanupInactiveExecutors() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			log.Println("Cleaning up inactive executors...")
			err := s.executorStore.CleanupInactive(15 * time.Second) // 15秒没有心跳则认为不活跃
			if err != nil {
				log.Printf("Error cleaning up inactive executors: %v", err)
			}
		}
	}
}


// ExecutorStore 接口定义了调度中心与执行器存储交互的方法
type ExecutorStore interface {
	SaveExecutor(info task.ExecutorInfo) error
	GetExecutorByID(id string) (task.ExecutorInfo, error)
	GetHealthyExecutors() ([]task.ExecutorInfo, error) // 获取所有健康的执行器
	SelectHealthyExecutor(executorID string) (task.ExecutorInfo, error) // 调度中心选择执行器
	CleanupInactive(threshold time.Duration) error // 清理不活跃的执行器
}

```

#### 2.5 持久化 (Persistence)

对于任务调度系统，数据持久化至关重要。

*   **关系型数据库**: `MySQL` 或 `PostgreSQL` 是存储任务元数据 (`task.Job`)、执行日志 (`task.JobExecutionLog`) 和执行器信息 (`task.ExecutorInfo`) 的理想选择。它们提供事务支持、复杂查询和数据完整性。
*   **Go ORM 库**:
    *   `GORM` (github.com/go-gorm/gorm): 功能丰富、易于使用的 ORM 库，支持多种数据库，提供了模型定义、CRUD 操作、关联查询等。
    *   `SQLX` (github.com/jmoiron/sqlx): 对 Go 标准库 `database/sql` 的扩展，提供更方便的结构体与 SQL 结果集的映射。

```go
// storage/mysql.go (示例，非完整代码)
package storage

import (
	"log"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"your_project_name/task" // 假设你的任务定义在 your_project_name/task 包中
)

// MySQLJobStore 实现 task.JobStore 接口
type MySQLJobStore struct {
	db *gorm.DB
}

// NewMySQLJobStore 创建MySQL Job Store实例
func NewMySQLJobStore(dsn string) (*MySQLJobStore, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info), // 打印SQL日志
	})
	if err != nil {
		return nil, err
	}

	// 自动迁移数据库Schema
	err = db.AutoMigrate(
		&task.Job{},
		&task.JobExecutionLog{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to auto migrate database: %w", err)
	}

	return &MySQLJobStore{db: db}, nil
}

func (s *MySQLJobStore) GetRunnableJobs() ([]task.Job, error) {
	var jobs []task.Job
	// 筛选出状态为PENDING, SUCCESS, FAILED（允许重试）且未暂停的任务
	// 这里需要根据实际调度逻辑来筛选，例如只获取未暂停且调度类型为Cron/Interval的
	err := s.db.Where("status != ?", task.JobStatusPaused).Find(&jobs).Error
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

func (s *MySQLJobStore) GetJobByID(id string) (task.Job, error) {
	var job task.Job
	err := s.db.Where("id = ?", id).First(&job).Error
	if err != nil {
		return task.Job{}, err
	}
	return job, nil
}

func (s *MySQLJobStore) UpdateJobStatus(id string, status task.JobStatus) error {
	return s.db.Model(&task.Job{}).Where("id = ?", id).Update("status", status).Error
}

func (s *MySQLJobStore) LogExecution(jobID, executorID string, status task.JobStatus, output, errMsg string, duration int64) (string, error) {
	logEntry := task.JobExecutionLog{
		ID:         uuid.NewString(), // 使用UUID生成ID
		JobID:      jobID,
		ExecutorID: executorID,
		StartTime:  time.Now(),
		Status:     status,
		Output:     output,
		Error:      errMsg,
		Duration:   duration,
		CreatedAt:  time.Now(),
	}
	err := s.db.Create(&logEntry).Error
	if err != nil {
		return "", err
	}
	return logEntry.ID, nil
}

func (s *MySQLJobStore) UpdateExecutionLog(logID string, status task.JobStatus, output, errMsg string, duration int64) error {
	return s.db.Model(&task.JobExecutionLog{}).Where("id = ?", logID).Updates(map[string]interface{}{
		"status":   status,
		"output":   output,
		"error":    errMsg,
		"end_time": time.Now(),
		"duration": duration,
	}).Error
}


// MySQLExecutorStore 实现 ExecutorStore 接口
type MySQLExecutorStore struct {
	db *gorm.DB
}

// NewMySQLExecutorStore 创建MySQL Executor Store实例
func NewMySQLExecutorStore(dsn string) (*MySQLExecutorStore, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, err
	}
	err = db.AutoMigrate(&task.ExecutorInfo{})
	if err != nil {
		return nil, fmt.Errorf("failed to auto migrate executor table: %w", err)
	}
	return &MySQLExecutorStore{db: db}, nil
}

func (s *MySQLExecutorStore) SaveExecutor(info task.ExecutorInfo) error {
	// Upsert操作：如果存在则更新，不存在则创建
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"address", "heartbeat", "status", "updated_at"}),
	}).Create(&info).Error
}

func (s *MySQLExecutorStore) GetExecutorByID(id string) (task.ExecutorInfo, error) {
	var execInfo task.ExecutorInfo
	err := s.db.Where("id = ?", id).First(&execInfo).Error
	if err != nil {
		return task.ExecutorInfo{}, err
	}
	return execInfo, nil
}

func (s *MySQLExecutorStore) GetHealthyExecutors() ([]task.ExecutorInfo, error) {
	var executors []task.ExecutorInfo
	// 假设10秒内有心跳的为健康执行器
	threshold := time.Now().Add(-10 * time.Second)
	err := s.db.Where("status = ? AND heartbeat >= ?", "UP", threshold).Find(&executors).Error
	if err != nil {
		return nil, err
	}
	return executors, nil
}

func (s *MySQLExecutorStore) SelectHealthyExecutor(executorID string) (task.ExecutorInfo, error) {
	// 优先选择指定ID的，如果指定ID不存在或不健康，则随机选择一个健康的
	if executorID != "" {
		execInfo, err := s.GetExecutorByID(executorID)
		if err == nil && execInfo.Status == "UP" && execInfo.Heartbeat.After(time.Now().Add(-10*time.Second)) {
			return execInfo, nil
		}
	}

	// 随机选择一个健康的执行器
	executors, err := s.GetHealthyExecutors()
	if err != nil || len(executors) == 0 {
		return task.ExecutorInfo{}, fmt.Errorf("no healthy executors available: %w", err)
	}
	// 简单的随机选择
	return executors[rand.Intn(len(executors))], nil
}

func (s *MySQLExecutorStore) CleanupInactive(threshold time.Duration) error {
	// 将超过阈值未更新心跳的执行器标记为DOWN
	inactiveTime := time.Now().Add(-threshold)
	return s.db.Model(&task.ExecutorInfo{}).Where("heartbeat < ? AND status = ?", inactiveTime, "UP").Update("status", "DOWN").Error
}


```

#### 2.6 分布式协调

在分布式环境下，Go 语言在分布式协调方面可以依赖以下工具和库：

*   **分布式锁**:
    *   **基于 Redis**: 使用 `go-redis/redis` 库实现 Redis 的分布式锁。通常需要结合 Redlock 算法来增强可靠性。
    *   **基于 ZooKeeper/etcd**: 使用 `github.com/go-zookeeper/zk` 或 `go.etcd.io/etcd/client/v3` 来实现分布式锁和选主。etcd 更适合 Go 生态，因为它提供了更现代的客户端 API 和 Raft 协议的实现。

```go
// distributed/redis_lock.go (示例，简化版)
package distributed

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8" // Or "github.com/gomodule/redigo/redis"
	"your_project_name/scheduler"
)

// RedisLocker 实现 scheduler.DistributedLocker 接口
type RedisLocker struct {
	client *redis.Client
}

// NewRedisLocker 创建 Redis Locker 实例
func NewRedisLocker(addr string) *RedisLocker {
	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &RedisLocker{client: rdb}
}

func (l *RedisLocker) Lock(key string, expiration time.Duration) (bool, error) {
	// 使用SETNX命令，如果key不存在则设置，并返回true，否则返回false
	// 这里使用SetNX，更安全的Redlock算法会更复杂
	// SetNX(key, value, expiration)
	// value 可以是随机字符串，用于避免误删其他客户端的锁
	ok, err := l.client.SetNX(context.Background(), key, "locked", expiration).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (l *RedisLocker) Unlock(key string) error {
	// 释放锁时需要验证value是否匹配，避免误删
	// 这里简化为直接删除
	_, err := l.client.Del(context.Background(), key).Result()
	return err
}

// ServiceDiscoveryClient 的实现示例 (基于HTTP回调，实际可能用Consul/etcd)
// service_discovery/http_sd.go (示例)
package servicediscovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"your_project_name/task" // 假设你的任务定义在 your_project_name/task 包中
)

type HTTPSDClient struct {
	adminAddr string // 调度中心的地址
	httpClient *http.Client
}

func NewHTTPSDClient(adminAddr string) *HTTPSDClient {
	return &HTTPSDClient{
		adminAddr: adminAddr,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *HTTPSDClient) RegisterExecutor(info task.ExecutorInfo) error {
	registerURL := fmt.Sprintf("%s/api/executor/register", c.adminAddr)
	jsonData, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal executor info: %w", err)
	}

	req, err := http.NewRequest("POST", registerURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create register request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send register request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}
	log.Printf("Executor %s registered/heartbeat sent successfully.", info.ID)
	return nil
}

func (c *HTTPSDClient) GetHealthyExecutors() ([]task.ExecutorInfo, error) {
	listURL := fmt.Sprintf("%s/api/executors", c.adminAddr)
	resp, err := c.httpClient.Get(listURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get healthy executors: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get healthy executors with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var executors []task.ExecutorInfo
	if err := json.NewDecoder(resp.Body).Decode(&executors); err != nil {
		return nil, fmt.Errorf("failed to decode executors response: %w", err)
	}
	return executors, nil
}

```

通过上述 Go 语言的实现细节和代码示例，我们初步构建了任务调度系统的核心组件。第三章节将提供一个更完整的、类似 XXL-Job 的 Go 语言实现代码。


### 3. 一个完备的类比 XXL-Job 的任务调度系统的 Golang 实现代码完整版

#### 分析设计

*   **目标**: 提供一个相对"完整"的 Go 语言任务调度系统实现，其核心功能类似于 XXL-Job。这里说的"完整"是指包含了前两章节讨论的核心组件及其基本交互逻辑，并能作为一个可运行的示例。考虑到 XXL-Job 的功能极其丰富，此处的"完整版"将专注于核心调度、执行、管理功能，省略部分高级特性（如复杂的路由策略、多租户、精确的故障转移、图形化界面等），但会为这些特性预留扩展点。
*   **架构设计**:
    *   **模块化**: 将系统划分为 `config`, `task`, `storage`, `scheduler`, `executor`, `admin`, `distributed`, `service_discovery` 等模块。
    *   **独立服务**: `scheduler`、`executor`、`admin` 将作为独立的 Go 应用程序运行。
    *   **通信**: 各组件之间主要通过 HTTP/JSON 进行通信。
    *   **持久化**: 使用 MySQL 存储任务、日志和执行器信息。
    *   **分布式协调**: 使用 Redis 实现分布式锁（简化版）。
*   **核心功能点**:
    *   任务的 CRUD 操作。
    *   Cron 和固定间隔任务的调度。
    *   任务分发到执行器。
    *   执行器接收并执行 HTTP/Shell 命令/Go 函数类型任务。
    *   任务执行结果上报及日志记录。
    *   执行器心跳与健康检查。
    *   调度器在分布式环境下的互斥调度（通过分布式锁）。
*   **运行说明**: 提供如何启动各个组件以及数据库、Redis 的简要说明。

#### 详细代码实现与说明

我们将构建一个包含以下目录结构的项目：

```
your_project_name/
├── main.go               # 程序的入口点，用于启动不同的服务
├── config/               # 配置管理
│   └── config.go
├── task/                 # 任务模型定义
│   └── model.go
├── storage/              # 数据持久化层（MySQL实现）
│   └── mysql.go
├── scheduler/            # 调度器逻辑
│   └── scheduler.go
├── executor/             # 执行器逻辑
│   └── executor.go
├── admin/                # 调度中心管理逻辑（Web API）
│   └── server.go
├── distributed/          # 分布式组件（如Redis锁）
│   └── redis_lock.go
├── service_discovery/    # 服务发现客户端（Admin Server 充当服务端）
│   └── http_sd.go
├── go.mod
├── go.sum
└── db_schema.sql         # 数据库表结构
```

**1. `db_schema.sql` (数据库表结构)**

```sql
-- db_schema.sql
-- 任务表
CREATE TABLE IF NOT EXISTS `jobs` (
    `id` VARCHAR(36) PRIMARY KEY,
    `name` VARCHAR(255) NOT NULL,
    `description` TEXT,
    `job_type` VARCHAR(50) NOT NULL,
    `cron_spec` VARCHAR(255) DEFAULT '',
    `interval` INT DEFAULT 0,
    `payload` TEXT,
    `executor_id` VARCHAR(36) DEFAULT '',
    `timeout` INT DEFAULT 60,
    `retry_times` INT DEFAULT 0,
    `status` VARCHAR(50) NOT NULL,
    `created_at` DATETIME NOT NULL,
    `updated_at` DATETIME NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 任务执行日志表
CREATE TABLE IF NOT EXISTS `job_execution_logs` (
    `id` VARCHAR(36) PRIMARY KEY,
    `job_id` VARCHAR(36) NOT NULL,
    `executor_id` VARCHAR(36) NOT NULL,
    `start_time` DATETIME NOT NULL,
    `end_time` DATETIME,
    `status` VARCHAR(50) NOT NULL,
    `output` LONGTEXT,
    `error` TEXT,
    `duration` BIGINT DEFAULT 0, -- 毫秒
    `created_at` DATETIME NOT NULL,
    KEY `idx_job_id` (`job_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 执行器信息表
CREATE TABLE IF NOT EXISTS `executor_infos` (
    `id` VARCHAR(36) PRIMARY KEY,
    `address` VARCHAR(255) NOT NULL,
    `heartbeat` DATETIME NOT NULL,
    `status` VARCHAR(50) NOT NULL,
    `created_at` DATETIME NOT NULL,
    `updated_at` DATETIME NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

```

**2. `go.mod` (Go 模块文件)**

```go
// go.mod
module your_project_name

go 1.22

require (
	github.com/gin-gonic/gin v1.10.0
	github.com/go-redis/redis/v8 v8.11.5 // for distributed lock
	github.com/go-sql-driver/mysql v1.8.1 // for gorm mysql driver
	github.com/gofrs/uuid v4.4.0+incompatible // for generating UUIDs
	github.com/robfig/cron/v3 v3.0.1 // for cron scheduling
	gorm.io/driver/mysql v1.5.7 // gorm mysql driver
	gorm.io/gorm v1.25.10 // ORM
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/bytedance/sonic v1.11.6 // indirect
	github.com/bytedance/sonic/loader v0.1.1 // indirect
	github.com/cloudwego/base64x v0.1.4 // indirect
	github.com/cloudwego/iasm v0.2.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.3 // indirect
	github.com/gin-contrib/sse v0.1.0 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.20.0 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/cpuid/v2 v2.2.7 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/pelletier/go-toml/v2 v2.2.2 // indirect
	github.com/robfig/cron v1.2.0 // indirect
	github.com/ugorji/go/codec v1.2.12 // indirect
	golang.org/x/crypto v0.23.0 // indirect
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/text v0.15.0 // indirect
	google.golang.org/protobuf v1.34.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
```

**3. `config/config.go`**

```go
// config/config.go
package config

import (
	"fmt"
	"os"
)

// AppConfig 应用程序配置
type AppConfig struct {
	AdminPort  string
	SchedulerPort string // 调度器不直接对外暴露端口，这里只是用于标识或内部通信
	ExecutorPort string
	MySQLDSN   string
	RedisAddr  string
}

// LoadConfig 从环境变量加载配置
func LoadConfig() (*AppConfig, error) {
	cfg := &AppConfig{
		AdminPort:  os.Getenv("ADMIN_PORT"),
		SchedulerPort: os.Getenv("SCHEDULER_PORT"), // Placeholder
		ExecutorPort: os.Getenv("EXECUTOR_PORT"),
		MySQLDSN:   os.Getenv("MYSQL_DSN"),
		RedisAddr:  os.Getenv("REDIS_ADDR"),
	}

	if cfg.AdminPort == "" {
		cfg.AdminPort = "8080" // 默认端口
	}
	if cfg.SchedulerPort == "" {
		cfg.SchedulerPort = "8081" // Placeholder
	}
	if cfg.ExecutorPort == "" {
		cfg.ExecutorPort = "8082" // 默认端口
	}

	if cfg.MySQLDSN == "" {
		return nil, fmt.Errorf("MYSQL_DSN environment variable not set")
	}
	if cfg.RedisAddr == "" {
		return nil, fmt.Errorf("REDIS_ADDR environment variable not set")
	}

	return cfg, nil
}
```

**4. `task/model.go`**

```go
// task/model.go (与前面章节相同，无需修改)
package task

import (
	"time"
)

// JobType 定义任务的类型，例如 HTTP_CALLBACK, SHELL_COMMAND, GO_FUNCTION
type JobType string

const (
	JobTypeHTTPCallback JobType = "HTTP_CALLBACK"
	JobTypeShellCommand JobType = "SHELL_COMMAND"
	JobTypeGoFunction   JobType = "GO_FUNCTION" // 表示由执行器内部Go函数直接执行
)

// JobStatus 定义任务的状态
type JobStatus string

const (
	JobStatusPending   JobStatus = "PENDING"   // 待调度
	JobStatusScheduling JobStatus = "SCHEDULING" // 调度中
	JobStatusRunning   JobStatus = "RUNNING"   // 执行中
	JobStatusSuccess   JobStatus = "SUCCESS"   // 执行成功
	JobStatusFailed    JobStatus = "FAILED"    // 执行失败
	JobStatusPaused    JobStatus = "PAUSED"    // 已暂停
)

// Job 定义一个任务的结构体
type Job struct {
	ID          string    `json:"id" gorm:"primarykey"`           // 任务唯一ID
	Name        string    `json:"name"`         // 任务名称
	Description string    `json:"description"`  // 任务描述
	JobType     JobType   `json:"job_type"`     // 任务类型
	CronSpec    string    `json:"cron_spec" gorm:"default:''"`    // Cron表达式，用于定时任务
	Interval    int       `json:"interval" gorm:"default:0"`     // 间隔时间（秒），用于循环任务
	Payload     string    `json:"payload"`      // 任务执行参数（JSON字符串或其他格式）
	ExecutorID  string    `json:"executor_id" gorm:"default:''"`  // 指定执行器ID，可选
	Timeout     int       `json:"timeout" gorm:"default:60"`      // 执行超时时间（秒）
	RetryTimes  int       `json:"retry_times" gorm:"default:0"`  // 重试次数
	Status      JobStatus `json:"status"`       // 任务状态
	CreatedAt   time.Time `json:"created_at"`   // 创建时间
	UpdatedAt   time.Time `json:"updated_at"`   // 更新时间
}

// JobExecutionLog 记录任务的每次执行日志
type JobExecutionLog struct {
	ID          string    `json:"id" gorm:"primarykey"`
	JobID       string    `json:"job_id"`
	ExecutorID  string    `json:"executor_id"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	Status      JobStatus `json:"status"` // 本次执行的状态 (SUCCESS/FAILED)
	Output      string    `json:"output" gorm:"type:longtext"` // 任务执行的输出/日志
	Error       string    `json:"error"`  // 错误信息
	Duration    int64     `json:"duration" gorm:"default:0"` // 执行耗时（毫秒）
	CreatedAt   time.Time `json:"created_at"`
}

// ExecutorInfo 记录执行器的信息
type ExecutorInfo struct {
	ID        string    `json:"id" gorm:"primarykey"`        // 执行器唯一ID
	Address   string    `json:"address"`   // 执行器地址 (e.g., "http://127.0.0.1:8081")
	Heartbeat time.Time `json:"heartbeat"` // 最后一次心跳时间
	Status    string    `json:"status"`    // 执行器状态 (e.g., "UP", "DOWN")
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

```

**5. `storage/mysql.go`**

```go
// storage/mysql.go (与前面章节基本相同，添加了CRUD方法)
package storage

import (
	"fmt"
	"log"
	"time"

	"github.com/gofrs/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"your_project_name/task" // 假设你的任务定义在 your_project_name/task 包中
)

// MySQLJobStore 实现 task.JobStore 接口
type MySQLJobStore struct {
	db *gorm.DB
}

// NewMySQLJobStore 创建MySQL Job Store实例
func NewMySQLJobStore(dsn string) (*MySQLJobStore, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info), // 打印SQL日志
	})
	if err != nil {
		return nil, err
	}

	// 自动迁移数据库Schema
	err = db.AutoMigrate(
		&task.Job{},
		&task.JobExecutionLog{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to auto migrate database: %w", err)
	}

	return &MySQLJobStore{db: db}, nil
}

// Implement JobStore interface
func (s *MySQLJobStore) CreateJob(job *task.Job) error {
	job.ID = uuid.NewString()
	job.CreatedAt = time.Now()
	job.UpdatedAt = time.Now()
	return s.db.Create(job).Error
}

func (s *MySQLJobStore) UpdateJob(job *task.Job) error {
	job.UpdatedAt = time.Now()
	return s.db.Save(job).Error // Save will update all fields if primary key exists
}

func (s *MySQLJobStore) DeleteJob(id string) error {
	return s.db.Delete(&task.Job{}, "id = ?", id).Error
}

func (s *MySQLJobStore) GetJobByID(id string) (task.Job, error) {
	var job task.Job
	err := s.db.Where("id = ?", id).First(&job).Error
	if err != nil {
		return task.Job{}, err
	}
	return job, nil
}

func (s *MySQLJobStore) ListJobs(page, pageSize int) ([]task.Job, int64, error) {
	var jobs []task.Job
	var total int64
	s.db.Model(&task.Job{}).Count(&total)
	err := s.db.Limit(pageSize).Offset((page - 1) * pageSize).Find(&jobs).Error
	if err != nil {
		return nil, 0, err
	}
	return jobs, total, nil
}


func (s *MySQLJobStore) GetRunnableJobs() ([]task.Job, error) {
	var jobs []task.Job
	// 筛选出状态为PENDING, SUCCESS, FAILED（允许重试），且未暂停，且有调度策略的任务
	err := s.db.Where("status != ? AND (cron_spec != '' OR `interval` > 0)", task.JobStatusPaused).Find(&jobs).Error
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

func (s *MySQLJobStore) UpdateJobStatus(id string, status task.JobStatus) error {
	return s.db.Model(&task.Job{}).Where("id = ?", id).Update("status", status).Error
}

func (s *MySQLJobStore) LogExecution(jobID, executorID string, status task.JobStatus, output, errMsg string, duration int64) (string, error) {
	logEntry := task.JobExecutionLog{
		ID:         uuid.NewString(), // 使用UUID生成ID
		JobID:      jobID,
		ExecutorID: executorID,
		StartTime:  time.Now(),
		Status:     status,
		Output:     output,
		Error:      errMsg,
		Duration:   duration,
		CreatedAt:  time.Now(),
	}
	err := s.db.Create(&logEntry).Error
	if err != nil {
		return "", err
	}
	return logEntry.ID, nil
}

func (s *MySQLJobStore) UpdateExecutionLog(logID string, status task.JobStatus, output, errMsg string, duration int64) error {
	return s.db.Model(&task.JobExecutionLog{}).Where("id = ?", logID).Updates(map[string]interface{}{
		"status":   status,
		"output":   output,
		"error":    errMsg,
		"end_time": time.Now(),
		"duration": duration,
	}).Error
}

func (s *MySQLJobStore) GetJobLogs(jobID string, page, pageSize int) ([]task.JobExecutionLog, int64, error) {
	var logs []task.JobExecutionLog
	var total int64
	query := s.db.Model(&task.JobExecutionLog{}).Where("job_id = ?", jobID)
	query.Count(&total)
	err := query.Order("created_at DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// MySQLExecutorStore 实现 admin.ExecutorStore 和 scheduler.ExecutorManager 的部分接口
type MySQLExecutorStore struct {
	db *gorm.DB
}

// NewMySQLExecutorStore 创建MySQL Executor Store实例
func NewMySQLExecutorStore(dsn string) (*MySQLExecutorStore, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, err
	}
	err = db.AutoMigrate(&task.ExecutorInfo{})
	if err != nil {
		return nil, fmt.Errorf("failed to auto migrate executor table: %w", err)
	}
	return &MySQLExecutorStore{db: db}, nil
}

// Implement admin.ExecutorStore interface
func (s *MySQLExecutorStore) SaveExecutor(info task.ExecutorInfo) error {
	info.UpdatedAt = time.Now()
	// Upsert操作：如果存在则更新，不存在则创建
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"address", "heartbeat", "status", "updated_at"}),
	}).Create(&info).Error
}

func (s *MySQLExecutorStore) GetExecutorByID(id string) (task.ExecutorInfo, error) {
	var execInfo task.ExecutorInfo
	err := s.db.Where("id = ?", id).First(&execInfo).Error
	if err != nil {
		return task.ExecutorInfo{}, err
	}
	return execInfo, nil
}

func (s *MySQLExecutorStore) GetHealthyExecutors() ([]task.ExecutorInfo, error) {
	var executors []task.ExecutorInfo
	// 假设15秒内有心跳的为健康执行器
	threshold := time.Now().Add(-15 * time.Second)
	err := s.db.Where("status = ? AND heartbeat >= ?", "UP", threshold).Find(&executors).Error
	if err != nil {
		return nil, err
	}
	return executors, nil
}

func (s *MySQLExecutorStore) SelectHealthyExecutor(executorID string) (task.ExecutorInfo, error) {
	// 优先选择指定ID的，如果指定ID不存在或不健康，则随机选择一个健康的
	if executorID != "" {
		execInfo, err := s.GetExecutorByID(executorID)
		if err == nil && execInfo.Status == "UP" && execInfo.Heartbeat.After(time.Now().Add(-15*time.Second)) {
			return execInfo, nil
		}
	}

	// 随机选择一个健康的执行器
	executors, err := s.GetHealthyExecutors()
	if err != nil || len(executors) == 0 {
		return task.ExecutorInfo{}, fmt.Errorf("no healthy executors available: %w", err)
	}
	// 简单的随机选择
	return executors[rand.Intn(len(executors))], nil
}

func (s *MySQLExecutorStore) CleanupInactive(threshold time.Duration) error {
	// 将超过阈值未更新心跳的执行器标记为DOWN
	inactiveTime := time.Now().Add(-threshold)
	return s.db.Model(&task.ExecutorInfo{}).Where("heartbeat < ? AND status = ?", inactiveTime, "UP").Update("status", "DOWN").Error
}

```

**6. `distributed/redis_lock.go`**

```go
// distributed/redis_lock.go (与前面章节相同)
package distributed

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8"
	"your_project_name/scheduler" // Ensure this path is correct
)

// RedisLocker 实现 scheduler.DistributedLocker 接口
type RedisLocker struct {
	client *redis.Client
}

// NewRedisLocker 创建 Redis Locker 实例
func NewRedisLocker(addr string) *RedisLocker {
	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &RedisLocker{client: rdb}
}

func (l *RedisLocker) Lock(key string, expiration time.Duration) (bool, error) {
	// 使用SETNX命令，如果key不存在则设置，并返回true，否则返回false
	// 这里使用SetNX，更安全的Redlock算法会更复杂
	// value 可以是随机字符串，用于避免误删其他客户端的锁
	ok, err := l.client.SetNX(context.Background(), key, "locked", expiration).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (l *RedisLocker) Unlock(key string) error {
	// 释放锁时需要验证value是否匹配，避免误删
	// 这里简化为直接删除
	_, err := l.client.Del(context.Background(), key).Result()
	return err
}

```

**7. `service_discovery/http_sd.go`**

```go
// service_discovery/http_sd.go (与前面章节相同，添加了必要的导入)
package servicediscovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
	"your_project_name/task" // 假设你的任务定义在 your_project_name/task 包中
	"your_project_name/scheduler"
)

type HTTPSDClient struct {
	adminAddr string // 调度中心的地址
	httpClient *http.Client
}

func NewHTTPSDClient(adminAddr string) *HTTPSDClient {
	return &HTTPSDClient{
		adminAddr: adminAddr,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *HTTPSDClient) RegisterExecutor(info task.ExecutorInfo) error {
	registerURL := fmt.Sprintf("%s/api/executor/register", c.adminAddr)
	jsonData, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal executor info: %w", err)
	}

	req, err := http.NewRequest("POST", registerURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create register request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send register request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}
	log.Printf("Executor %s registered/heartbeat sent successfully.", info.ID)
	return nil
}

func (c *HTTPSDClient) GetHealthyExecutors() ([]task.ExecutorInfo, error) {
	listURL := fmt.Sprintf("%s/api/executors", c.adminAddr)
	resp, err := c.httpClient.Get(listURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get healthy executors: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get healthy executors with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var executors []task.ExecutorInfo
	if err := json.NewDecoder(resp.Body).Decode(&executors); err != nil {
		return nil, fmt.Errorf("failed to decode executors response: %w", err)
	}
	return executors, nil
}


// HTTPExecutorManager 实现 scheduler.ExecutorManager 接口，通过HTTP调用执行器
type HTTPExecutorManager struct {
	adminAddr string // 调度中心的地址，用于获取执行器列表
	httpClient *http.Client
	sdClient   *HTTPSDClient // 服务发现客户端，用于获取健康的执行器
}

func NewHTTPExecutorManager(adminAddr string) *HTTPExecutorManager {
	return &HTTPExecutorManager{
		adminAddr: adminAddr,
		httpClient: &http.Client{Timeout: 60 * time.Second}, // 任务执行超时可能较长
		sdClient: NewHTTPSDClient(adminAddr),
	}
}

func (m *HTTPExecutorManager) SelectExecutor(executorID string) (task.ExecutorInfo, error) {
	// 如果指定了executorID，尝试获取该执行器
	if executorID != "" {
		executors, err := m.sdClient.GetHealthyExecutors()
		if err != nil {
			return task.ExecutorInfo{}, fmt.Errorf("failed to get healthy executors: %w", err)
		}
		for _, exec := range executors {
			if exec.ID == executorID {
				return exec, nil
			}
		}
		return task.ExecutorInfo{}, fmt.Errorf("specified executor %s not found or unhealthy", executorID)
	}

	// 否则随机选择一个健康的执行器
	executors, err := m.sdClient.GetHealthyExecutors()
	if err != nil {
		return task.ExecutorInfo{}, fmt.Errorf("failed to get healthy executors: %w", err)
	}
	if len(executors) == 0 {
		return task.ExecutorInfo{}, fmt.Errorf("no healthy executors available")
	}
	return executors[rand.Intn(len(executors))], nil
}

func (m *HTTPExecutorManager) Execute(executor task.ExecutorInfo, job task.Job) (scheduler.ExecutorResult, error) {
	executeURL := fmt.Sprintf("%s/execute", executor.Address)
	reqBody := struct {
		JobID   string       `json:"job_id"`
		JobType task.JobType `json:"job_type"`
		Payload string       `json:"payload"`
		Timeout int          `json:"timeout"`
		LogID   string       `json:"log_id"` // TODO: 在实际调度中，调度器先创建log，然后把logID传给执行器
	}{
		JobID:   job.ID,
		JobType: job.JobType,
		Payload: job.Payload,
		Timeout: job.Timeout,
		LogID:   "", // 暂时留空，由执行器生成或在调用前生成
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return scheduler.ExecutorResult{}, fmt.Errorf("failed to marshal job request: %w", err)
	}

	req, err := http.NewRequest("POST", executeURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return scheduler.ExecutorResult{}, fmt.Errorf("failed to create execute request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return scheduler.ExecutorResult{Status: task.JobStatusFailed, Error: fmt.Sprintf("HTTP request to executor failed: %v", err)}, err
	}
	defer resp.Body.Close()

	var result scheduler.ExecutorResult
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		result.Status = task.JobStatusFailed
		result.Error = fmt.Sprintf("Executor returned status %d: %s", resp.StatusCode, string(bodyBytes))
		return result, fmt.Errorf(result.Error)
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return scheduler.ExecutorResult{Status: task.JobStatusFailed, Error: fmt.Sprintf("Failed to decode executor response: %v", err)}, err
	}

	return result, nil
}

```

**8. `scheduler/scheduler.go`**

```go
// scheduler/scheduler.go (与前面章节基本相同，集成了新的ExecutorManager)
package scheduler

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"your_project_name/task" // 假设你的任务定义在 your_project_name/task 包中
)

// Scheduler 结构体
type Scheduler struct {
	cron       *cron.Cron
	jobStore   JobStore // 用于获取和更新任务状态的接口
	executorMgr ExecutorManager // 用于管理和选择执行器的接口
	// 分布式锁，用于多调度器实例环境
	distributedLocker DistributedLocker
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex // 保护对cron实例和jobIDToEntryID映射的并发操作

	// 辅助映射，用于管理cron entry
	jobIDToEntryID sync.Map // map[string]cron.EntryID
	entryIDToJobID sync.Map // map[cron.EntryID]string
}

// NewScheduler 创建一个新的调度器实例
func NewScheduler(jobStore JobStore, executorMgr ExecutorManager, locker DistributedLocker) *Scheduler {
	c := cron.New(cron.WithChain(
		cron.Recover(cron.DefaultLogger),            // 任务panic时恢复
		cron.DelayIfStillRunning(cron.DefaultLogger), // 如果上一次任务还在运行，则延迟本次任务
	))
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		cron:       c,
		jobStore:   jobStore,
		executorMgr: executorMgr,
		distributedLocker: locker,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start 启动调度器
func (s *Scheduler) Start() {
	log.Println("Scheduler started...")
	s.cron.Start()
	// 启动一个Goroutine定期加载和注册任务
	go s.loadAndRegisterJobs()
	<-s.ctx.Done() // 等待上下文取消
	log.Println("Scheduler stopped.")
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	s.cancel()
	<-s.cron.Stop().Done() // 等待所有正在执行的任务完成
	log.Println("Cron scheduler stopped.")
}

// loadAndRegisterJobs 定期从数据库加载任务并注册到cron
func (s *Scheduler) loadAndRegisterJobs() {
	ticker := time.NewTicker(30 * time.Second) // 每30秒加载一次任务
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			// 在分布式环境中，这里需要获取分布式锁，确保只有一个调度器进行任务加载和注册
			if s.distributedLocker != nil {
				lockKey := "scheduler_job_load_lock"
				// 锁的过期时间应该大于任务加载时间，以防死锁
				locked, err := s.distributedLocker.Lock(lockKey, 20*time.Second)
				if err != nil {
					log.Printf("Failed to acquire distributed lock for job loading: %v", err)
					continue
				}
				if !locked {
					log.Println("Another scheduler instance is loading jobs, skipping.")
					continue // 未获取到锁，跳过本次加载
				}
				defer func() {
					if err := s.distributedLocker.Unlock(lockKey); err != nil {
						log.Printf("Failed to release distributed lock: %v", err)
					}
				}()
			}

			s.mu.Lock() // 保护对cron实例的并发操作
			log.Println("Loading and registering jobs...")
			jobs, err := s.jobStore.GetRunnableJobs() // 获取所有待运行的任务
			if err != nil {
				log.Printf("Failed to get runnable jobs: %v", err)
				s.mu.Unlock()
				continue
			}

			// 清除已删除或暂停的任务
			s.clearInactiveJobs(jobs)

			for _, job := range jobs {
				s.addOrUpdateJob(job)
			}
			s.mu.Unlock()
			log.Println("Job loading and registration complete.")
		}
	}
}

// addOrUpdateJob 添加或更新任务到cron
func (s *Scheduler) addOrUpdateJob(job task.Job) {
	entryIDVal, exists := s.jobIDToEntryID.Load(job.ID)
	var entryID cron.EntryID
	if exists {
		entryID = entryIDVal.(cron.EntryID)
	}

	if exists {
		// 如果任务已存在且Cron表达式未改变，则跳过
		// 注意：这里需要更严谨的判断任务是否真的没有变化（包括Payload、Interval等）
		entry := s.cron.Entry(entryID)
		if entry.Schedule.String() == job.CronSpec { // 这是一个简化的比较
			// 如果是Every类型的任务，需要检查Interval
			if job.CronSpec == "" && job.Interval > 0 {
				// 获取Every的持续时间，需要反射或者在AddJob时保存下来
				// 暂时跳过，认为已存在的Every任务不作更新
				return
			}
			return
		}
		// 如果调度策略改变，则移除旧任务，重新添加
		s.cron.Remove(entryID)
		s.removeEntryID(job.ID) // 移除映射
	}

	// 添加新任务或更新任务
	// 任务执行逻辑，这是一个闭包，捕获了job的信息
	jobFunc := func() {
		// 在这里，调度器选择一个合适的执行器来执行任务
		log.Printf("Job %s (%s) triggered.", job.Name, job.ID)
		executor, err := s.executorMgr.SelectExecutor(job.ExecutorID) // 根据ID或路由策略选择执行器
		if err != nil {
			log.Printf("Failed to select executor for job %s: %v", job.ID, err)
			// Log initial failure to database
			s.jobStore.LogExecution(job.ID, "N/A", task.JobStatusFailed, "", fmt.Sprintf("Failed to select executor: %v", err), 0)
			return
		}

		// 通过RPC/HTTP调用执行器接口执行任务
		go s.executeJobOnExecutor(job, executor)
	}

	var entryIDToAdd cron.EntryID
	var err error
	if job.CronSpec != "" {
		entryIDToAdd, err = s.cron.AddFunc(job.CronSpec, jobFunc)
	} else if job.Interval > 0 {
		// 使用Cron.Every来支持间隔任务
		entryIDToAdd, err = s.cron.AddJob(cron.Every(time.Duration(job.Interval)*time.Second), cron.FuncJob(jobFunc))
	} else {
		log.Printf("Job %s (%s) has no valid cron spec or interval. Skipping.", job.Name, job.ID)
		return
	}

	if err != nil {
		log.Printf("Failed to add job %s (%s) to cron: %v", job.Name, job.ID, err)
		return
	}
	s.setEntryID(job.ID, entryIDToAdd)
	log.Printf("Job %s (%s) added/updated with EntryID: %d", job.Name, job.ID, entryIDToAdd)
}

// clearInactiveJobs 移除不再活跃的任务
func (s *Scheduler) clearInactiveJobs(activeJobs []task.Job) {
	activeJobIDs := make(map[string]struct{})
	for _, job := range activeJobs {
		activeJobIDs[job.ID] = struct{}{}
	}

	s.jobIDToEntryID.Range(func(key, value interface{}) bool {
		jobID := key.(string)
		entryID := value.(cron.EntryID)
		if _, isActive := activeJobIDs[jobID]; !isActive {
			s.cron.Remove(entryID)
			s.removeEntryID(jobID)
			log.Printf("Removed inactive job with JobID: %s, EntryID: %d", jobID, entryID)
		}
		return true
	})
}


// executeJobOnExecutor 实际调用执行器执行任务
func (s *Scheduler) executeJobOnExecutor(job task.Job, executor task.ExecutorInfo) {
	// 在此处生成logID，并传递给执行器
	logID, err := s.jobStore.LogExecution(job.ID, executor.ID, task.JobStatusRunning, "", "", 0)
	if err != nil {
		log.Printf("Failed to log job start for %s: %v", job.ID, err)
		return
	}

	// 设置logID到任务中，以便executor回调时使用
	jobWithLogID := job
	jobWithLogID.Payload = updatePayloadWithLogID(job.Payload, logID) // 假设payload可以更新

	result, err := s.executorMgr.Execute(executor, jobWithLogID) // 这是一个抽象的执行器调用方法
	if err != nil {
		log.Printf("Failed to execute job %s on executor %s: %v", job.ID, executor.ID, err)
		s.jobStore.UpdateExecutionLog(logID, task.JobStatusFailed, "", fmt.Sprintf("Execution failed: %v", err), result.Duration)
		return
	}

	if result.Status == task.JobStatusSuccess {
		log.Printf("Job %s executed successfully on executor %s.", job.ID, executor.ID)
		s.jobStore.UpdateExecutionLog(logID, task.JobStatusSuccess, result.Output, "", result.Duration)
	} else {
		log.Printf("Job %s failed on executor %s: %s", job.ID, executor.ID, result.Error)
		s.jobStore.UpdateExecutionLog(logID, task.JobStatusFailed, result.Output, result.Error, result.Duration)
	}

	// 更新任务在数据库中的状态，例如，对于一次性任务，执行完成后更新为成功或失败
	if job.CronSpec == "" && job.Interval == 0 {
		s.jobStore.UpdateJobStatus(job.ID, result.Status)
	}
}

// updatePayloadWithLogID 辅助函数，将logID添加到payload中 (示例，实际根据payload结构实现)
func updatePayloadWithLogID(payload string, logID string) string {
	// 假设payload是JSON字符串
	// 实际情况可能更复杂，需要解析JSON并添加字段
	return fmt.Sprintf(`{"original_payload": %s, "log_id": "%s"}`, payload, logID)
}

// 辅助函数，维护 JobID 到 EntryID 的映射，使用sync.Map保证并发安全
func (s *Scheduler) setEntryID(jobID string, entryID cron.EntryID) {
	s.jobIDToEntryID.Store(jobID, entryID)
	s.entryIDToJobID.Store(entryID, jobID)
}

func (s *Scheduler) removeEntryID(jobID string) {
	if entryIDVal, ok := s.jobIDToEntryID.Load(jobID); ok {
		entryID := entryIDVal.(cron.EntryID)
		s.jobIDToEntryID.Delete(jobID)
		s.entryIDToJobID.Delete(entryID)
	}
}

// JobStore 接口定义了调度器与任务存储交互的方法
type JobStore interface {
	GetRunnableJobs() ([]task.Job, error)
	GetJobByID(id string) (task.Job, error)
	UpdateJobStatus(id string, status task.JobStatus) error
	LogExecution(jobID, executorID string, status task.JobStatus, output, errMsg string, duration int64) (string, error)
	UpdateExecutionLog(logID string, status task.JobStatus, output, errMsg string, duration int64) error
	// Add other methods for CRUD operations on jobs if needed for scheduler internal use
}

// ExecutorManager 接口定义了调度器与执行器管理交互的方法
type ExecutorManager interface {
	SelectExecutor(executorID string) (task.ExecutorInfo, error) // 根据策略选择执行器
	Execute(executor task.ExecutorInfo, job task.Job) (ExecutorResult, error) // 调用执行器执行任务
	// Add other methods for executor registration, heartbeat, etc.
}

// ExecutorResult 定义执行器返回的结果
type ExecutorResult struct {
	Status   task.JobStatus `json:"status"`
	Output   string         `json:"output"`
	Error    string         `json:"error"`
	Duration int64          `json:"duration"` // 毫秒
}

// DistributedLocker 定义分布式锁接口
type DistributedLocker interface {
	Lock(key string, expiration time.Duration) (bool, error)
	Unlock(key string) error
}

```

**9. `executor/executor.go`**

```go
// executor/executor.go (与前面章节基本相同，集成了新的ServiceDiscoveryClient和结果回调)
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"your_project_name/scheduler" // 假设你的任务定义在 your_project_name/task 包中
	"your_project_name/task"
)

// ExecutorConfig 执行器配置
type ExecutorConfig struct {
	ID        string
	Port      string
	AdminAddr string // 调度中心地址
}

// Executor 结构体
type Executor struct {
	cfg        ExecutorConfig
	router     *gin.Engine
	httpClient *http.Client
	// goroutine池用于限制并发执行的任务数量
	workerPool chan struct{}
	mu         sync.Mutex
	// 注册到调度中心的服务发现客户端
	serviceDiscoveryClient ServiceDiscoveryClient
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewExecutor 创建一个新的执行器实例
func NewExecutor(cfg ExecutorConfig, sdc ServiceDiscoveryClient) *Executor {
	r := gin.Default()
	r.Use(gin.Recovery()) // 崩溃恢复

	e := &Executor{
		cfg:        cfg,
		router:     r,
		httpClient: &http.Client{Timeout: 30 * time.Second}, // 设置HTTP客户端超时
		workerPool: make(chan struct{}, 100), // 限制同时执行的任务数量为100
		serviceDiscoveryClient: sdc,
	}
	e.setupRoutes()
	e.ctx, e.cancel = context.WithCancel(context.Background())
	return e
}

// setupRoutes 设置执行器对外暴露的HTTP接口
func (e *Executor) setupRoutes() {
	e.router.POST("/execute", e.handleExecuteJob)
	e.router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "UP", "id": e.cfg.ID})
	})
}

// Start 启动执行器
func (e *Executor) Start() {
	log.Printf("Executor %s starting on :%s", e.cfg.ID, e.cfg.Port)

	// 启动心跳Goroutine
	go e.startHeartbeat()

	// 启动HTTP服务
	server := &http.Server{
		Addr:    ":" + e.cfg.Port,
		Handler: e.router,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Executor server listen error: %v", err)
		}
	}()

	<-e.ctx.Done() // 等待上下文取消
	log.Println("Shutting down executor server...")

	// 优雅关闭HTTP服务
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Executor server forced to shutdown: %v", err)
	}
	log.Println("Executor stopped.")
}

// Stop 停止执行器
func (e *Executor) Stop() {
	e.cancel()
}

// handleExecuteJob 处理任务执行请求
func (e *Executor) handleExecuteJob(c *gin.Context) {
	var req struct {
		JobID      string `json:"job_id"`
		JobType    task.JobType `json:"job_type"`
		Payload    string `json:"payload"`
		Timeout    int    `json:"timeout"` // 任务超时时间
		LogID      string `json:"log_id"`  // 调度器传来的日志ID
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	log.Printf("Received job %s (%s) for execution. LogID: %s", req.JobID, req.JobType, req.LogID)

	// 使用goroutine池来限制并发
	select {
	case e.workerPool <- struct{}{}: // 尝试获取一个 worker slot
		go func() {
			defer func() { <-e.workerPool }() // 释放 worker slot
			e.executeTask(req.JobID, req.JobType, req.Payload, req.Timeout, req.LogID)
		}()
		c.JSON(http.StatusOK, gin.H{"message": "Job received and started execution."})
	default:
		errMsg := "Executor is busy, too many concurrent tasks."
		log.Printf("Job %s rejected: %s", req.JobID, errMsg)
		e.reportExecutionResult(req.JobID, req.LogID, task.JobStatusFailed, "", errMsg, 0)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errMsg})
	}
}

// executeTask 实际执行任务逻辑
func (e *Executor) executeTask(jobID string, jobType task.JobType, payload string, timeout int, logID string) {
	startTime := time.Now()
	var status task.JobStatus
	var output string
	var errMsg string

	// 设置任务执行的上下文和超时
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	defer func() {
		duration := time.Since(startTime).Milliseconds()
		e.reportExecutionResult(jobID, logID, status, output, errMsg, duration)
	}()

	switch jobType {
	case task.JobTypeHTTPCallback:
		// 执行HTTP回调任务
		log.Printf("Executing HTTP_CALLBACK for job %s with payload: %s", jobID, payload)
		var httpReqPayload struct {
			URL    string            `json:"url"`
			Method string            `json:"method"`
			Headers map[string]string `json:"headers"`
			Body   string            `json:"body"`
		}
		// 尝试解析payload为HTTP请求结构
		if err := json.Unmarshal([]byte(payload), &httpReqPayload); err != nil {
			status = task.JobStatusFailed
			errMsg = fmt.Sprintf("Invalid HTTP payload JSON: %v", err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, httpReqPayload.Method, httpReqPayload.URL, nil)
		if err != nil {
			status = task.JobStatusFailed
			errMsg = fmt.Sprintf("Failed to create HTTP request: %v", err)
			return
		}
		for k, v := range httpReqPayload.Headers {
			req.Header.Set(k, v)
		}
		if httpReqPayload.Body != "" {
			req.Body = io.NopCloser(bytes.NewBufferString(httpReqPayload.Body))
		}

		resp, err := e.httpClient.Do(req)
		if err != nil {
			status = task.JobStatusFailed
			errMsg = fmt.Sprintf("HTTP request failed: %v", err)
			if ctx.Err() == context.DeadlineExceeded {
				errMsg = "HTTP request timed out."
			}
			return
		}
		defer resp.Body.Close()

		bodyBytes, _ := io.ReadAll(resp.Body)
		output = string(bodyBytes)
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			status = task.JobStatusSuccess
		} else {
			status = task.JobStatusFailed
			errMsg = fmt.Sprintf("HTTP status code %d: %s", resp.StatusCode, output)
		}

	case task.JobTypeShellCommand:
		// 执行Shell命令任务
		log.Printf("Executing SHELL_COMMAND for job %s with command: %s", jobID, payload)
		cmd := exec.CommandContext(ctx, "/bin/bash", "-c", payload) // 注意安全性，生产环境需更严格的沙箱或白名单
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		cmd.Stdout = stdout
		cmd.Stderr = stderr

		err := cmd.Run()
		output = stdout.String()
		if err != nil {
			status = task.JobStatusFailed
			errMsg = fmt.Sprintf("Shell command failed: %v, Stderr: %s", err, stderr.String())
			if ctx.Err() == context.DeadlineExceeded {
				errMsg = "Shell command timed out."
			}
			return
		}
		status = task.JobStatusSuccess

	case task.JobTypeGoFunction:
		// 执行Go函数任务
		log.Printf("Executing GO_FUNCTION for job %s with payload: %s", jobID, payload)
		// 这是一个模拟，实际需要一个注册机制来调用Go函数
		// 例如：
		// funcMap := map[string]func(ctx context.Context, payload string) (string, error){
		//     "my_go_func": func(ctx context.Context, payload string) (string, error) { /* ... */ return "success", nil },
		// }
		// if fn, ok := funcMap[payload]; ok {
		//     funcOutput, funcErr := fn(ctx, payload)
		//     output = funcOutput
		//     if funcErr != nil {
		//         status = task.JobStatusFailed
		//         errMsg = fmt.Sprintf("Go function execution failed: %v", funcErr)
		//         return
		//     }
		//     status = task.JobStatusSuccess
		// } else {
		//     status = task.JobStatusFailed
		//     errMsg = fmt.Sprintf("Go function '%s' not registered.", payload)
		// }
		status = task.JobStatusSuccess // 模拟成功
		output = "Go function executed successfully (simulated)."

	default:
		status = task.JobStatusFailed
		errMsg = fmt.Sprintf("Unsupported job type: %s", jobType)
	}
}

// reportExecutionResult 上报任务执行结果给调度中心
func (e *Executor) reportExecutionResult(jobID, logID string, status task.JobStatus, output, errMsg string, duration int64) {
	reportURL := fmt.Sprintf("%s/api/job/log/update", e.cfg.AdminAddr)

	result := scheduler.ExecutorResult{
		Status:   status,
		Output:   output,
		Error:    errMsg,
		Duration: duration,
	}

	jsonData, err := json.Marshal(result)
	if err != nil {
		log.Printf("Failed to marshal execution result for job %s: %v", jobID, err)
		return
	}

	req, err := http.NewRequest("POST", reportURL, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Failed to create result report request for job %s: %v", jobID, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	// 将logID作为查询参数传递，以便Admin Server更新
	q := req.URL.Query()
	q.Add("log_id", logID)
	req.URL.RawQuery = q.Encode()


	resp, err := e.httpClient.Do(req)
	if err != nil {
		log.Printf("Failed to report execution result for job %s: %v", jobID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Failed to report execution result for job %s, status: %d, response: %s", jobID, resp.StatusCode, string(bodyBytes))
	} else {
		log.Printf("Successfully reported execution result for job %s (Status: %s) to Admin.", jobID, status)
	}
}

// startHeartbeat 启动心跳机制，定期向调度中心注册/报告存活
func (e *Executor) startHeartbeat() {
	ticker := time.NewTicker(10 * time.Second) // 每10秒发送一次心跳
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			// 向调度中心注册或更新心跳
			execInfo := task.ExecutorInfo{
				ID:        e.cfg.ID,
				Address:   fmt.Sprintf("http://127.0.0.1:%s", e.cfg.Port), // 实际应该是可访问的外部地址
				Heartbeat: time.Now(),
				Status:    "UP",
			}
			err := e.serviceDiscoveryClient.RegisterExecutor(execInfo)
			if err != nil {
				log.Printf("Failed to send heartbeat to admin: %v", err)
			}
		}
	}
}

// ServiceDiscoveryClient 定义服务发现客户端接口
type ServiceDiscoveryClient interface {
	RegisterExecutor(info task.ExecutorInfo) error
	GetHealthyExecutors() ([]task.ExecutorInfo, error)
	// Add other methods for executor management
}

```

**10. `admin/server.go`**

```go
// admin/server.go (与前面章节基本相同，集成了JobStore和ExecutorStore)
package admin

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"your_project_name/scheduler"
	"your_project_name/task"
)

// AdminServerConfig 配置
type AdminServerConfig struct {
	Port string
}

// AdminServer 调度中心服务器
type AdminServer struct {
	cfg        AdminServerConfig
	router     *gin.Engine
	jobStore   JobStore // 任务存储接口
	executorStore ExecutorStore // 执行器存储接口
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewAdminServer 创建新的调度中心服务器
func NewAdminServer(cfg AdminServerConfig, js JobStore, es ExecutorStore) *AdminServer {
	r := gin.Default()
	r.Use(gin.Recovery())

	s := &AdminServer{
		cfg:        cfg,
		router:     r,
		jobStore:   js,
		executorStore: es,
	}
	s.setupRoutes()
	s.ctx, s.cancel = context.WithCancel(context.Background())
	return s
}

// setupRoutes 设置API路由
func (s *AdminServer) setupRoutes() {
	api := s.router.Group("/api")
	{
		// 任务管理API
		api.POST("/job", s.createJob)
		api.PUT("/job/:id", s.updateJob)
		api.DELETE("/job/:id", s.deleteJob)
		api.GET("/job/:id", s.getJob)
		api.GET("/jobs", s.listJobs)
		api.POST("/job/:id/pause", s.pauseJob)
		api.POST("/job/:id/resume", s.resumeJob)
		api.POST("/job/:id/trigger", s.triggerJobManually) // 手动触发

		// 执行器管理API (接收执行器心跳和注册)
		api.POST("/executor/register", s.registerExecutor)
		api.GET("/executors", s.listExecutors)

		// 任务日志API
		api.GET("/job/logs/:job_id", s.listJobLogs)
		api.POST("/job/log/update", s.updateJobLog) // 接收执行器回调的日志更新
	}
}

// Start 启动Admin Server
func (s *AdminServer) Start() {
	log.Printf("Admin Server starting on :%s", s.cfg.Port)
	server := &http.Server{
		Addr:    ":" + s.cfg.Port,
		Handler: s.router,
	}

	// 启动定期清理不健康执行器的Goroutine
	go s.cleanupInactiveExecutors()

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Admin server listen error: %v", err)
		}
	}()

	<-s.ctx.Done()
	log.Println("Shutting down admin server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Admin server forced to shutdown: %v", err)
	}
	log.Println("Admin Server stopped.")
}

// Stop 停止Admin Server
func (s *AdminServer) Stop() {
	s.cancel()
}

// API 实现

func (s *AdminServer) createJob(c *gin.Context) {
	var job task.Job
	if err := c.BindJSON(&job); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}
	if err := s.jobStore.CreateJob(&job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to create job: %v", err)})
		return
	}
	c.JSON(http.StatusCreated, job)
}

func (s *AdminServer) updateJob(c *gin.Context) {
	jobID := c.Param("id")
	var job task.Job
	if err := c.BindJSON(&job); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}
	job.ID = jobID // 确保ID匹配
	if err := s.jobStore.UpdateJob(&job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to update job: %v", err)})
		return
	}
	c.JSON(http.StatusOK, job)
}

func (s *AdminServer) deleteJob(c *gin.Context) {
	jobID := c.Param("id")
	if err := s.jobStore.DeleteJob(jobID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to delete job: %v", err)})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

func (s *AdminServer) getJob(c *gin.Context) {
	jobID := c.Param("id")
	job, err := s.jobStore.GetJobByID(jobID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}
	c.JSON(http.StatusOK, job)
}

func (s *AdminServer) listJobs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "10"))
	jobs, total, err := s.jobStore.ListJobs(page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to list jobs: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": jobs, "total": total})
}

func (s *AdminServer) pauseJob(c *gin.Context) {
	jobID := c.Param("id")
	if err := s.jobStore.UpdateJobStatus(jobID, task.JobStatusPaused); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to pause job: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Job paused successfully."})
}

func (s *AdminServer) resumeJob(c *gin.Context) {
	jobID := c.Param("id")
	// 恢复任务状态，这里假设恢复为PENDING，实际可能需要根据任务类型或历史状态来决定
	if err := s.jobStore.UpdateJobStatus(jobID, task.JobStatusPending); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to resume job: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Job resumed successfully."})
}

// triggerJobManually 手动触发任务
func (s *AdminServer) triggerJobManually(c *gin.Context) {
	jobID := c.Param("id")
	job, err := s.jobStore.GetJobByID(jobID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	executor, err := s.executorStore.SelectHealthyExecutor(job.ExecutorID) // 调度中心选择一个执行器
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("No healthy executor available: %v", err)})
		return
	}

	// 1. 先在数据库中记录任务执行日志，获取logID
	logID, err := s.jobStore.LogExecution(job.ID, executor.ID, task.JobStatusRunning, "", "", 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to log manual trigger: %v", err)})
		return
	}

	// 2. 通过HTTP/RPC调用执行器
	go func() {
		log.Printf("Manually triggering job %s on executor %s...", job.ID, executor.ID)
		client := &http.Client{Timeout: time.Duration(job.Timeout)*time.Second + 5*time.Second} // 增加一点余量
		executeURL := fmt.Sprintf("%s/execute", executor.Address)

		reqBody := struct {
			JobID   string       `json:"job_id"`
			JobType task.JobType `json:"job_type"`
			Payload string       `json:"payload"`
			Timeout int          `json:"timeout"`
			LogID   string       `json:"log_id"` // 传递日志ID给执行器
		}{
			JobID: job.ID,
			JobType: job.JobType,
			Payload: job.Payload,
			Timeout: job.Timeout,
			LogID:   logID,
		}
		jsonBytes, _ := json.Marshal(reqBody)
		req, err := http.NewRequest("POST", executeURL, bytes.NewBuffer(jsonBytes))
		if err != nil {
			log.Printf("Failed to create manual trigger request for job %s: %v", job.ID, err)
			s.jobStore.UpdateExecutionLog(logID, task.JobStatusFailed, "", fmt.Sprintf("Admin failed to send request: %v", err), 0)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Manual trigger failed for job %s: %v", job.ID, err)
			s.jobStore.UpdateExecutionLog(logID, task.JobStatusFailed, "", fmt.Sprintf("Executor connection failed: %v", err), 0)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			log.Printf("Manual trigger failed for job %s, status: %d, response: %s", job.ID, resp.StatusCode, string(bodyBytes))
			s.jobStore.UpdateExecutionLog(logID, task.JobStatusFailed, string(bodyBytes), fmt.Sprintf("Executor rejected request with status %d", resp.StatusCode), 0)
		} else {
			log.Printf("Manual trigger sent for job %s successfully.", job.ID)
			// Admin 仅负责触发，后续结果由 Executor 回调 Admin 的 updateJobLog
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "Job manually triggered.", "log_id": logID})
}

// registerExecutor 接收执行器注册和心跳
func (s *AdminServer) registerExecutor(c *gin.Context) {
	var execInfo task.ExecutorInfo
	if err := c.BindJSON(&execInfo); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}
	execInfo.Heartbeat = time.Now() // 记录心跳时间
	execInfo.Status = "UP"
	if execInfo.CreatedAt.IsZero() {
		execInfo.CreatedAt = time.Now()
	}
	execInfo.UpdatedAt = time.Now()

	err := s.executorStore.SaveExecutor(execInfo)
	if err != nil {
		log.Printf("Failed to save/update executor %s: %v", execInfo.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register executor"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Executor registered/heartbeat received."})
}

func (s *AdminServer) listExecutors(c *gin.Context) {
	executors, err := s.executorStore.GetHealthyExecutors()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve executors"})
		return
	}
	c.JSON(http.StatusOK, executors)
}

func (s *AdminServer) listJobLogs(c *gin.Context) {
	jobID := c.Param("job_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "10"))

	logs, total, err := s.jobStore.GetJobLogs(jobID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to retrieve job logs: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": logs, "total": total})
}

// updateJobLog 接收执行器上报的任务执行结果
func (s *AdminServer) updateJobLog(c *gin.Context) {
	logID := c.Query("log_id") // 从查询参数获取日志ID
	if logID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing log_id parameter"})
		return
	}
	var result scheduler.ExecutorResult
	if err := c.BindJSON(&result); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	err := s.jobStore.UpdateExecutionLog(logID, result.Status, result.Output, result.Error, result.Duration)
	if err != nil {
		log.Printf("Failed to update execution log %s: %v", logID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update execution log"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Execution log updated."})
}

// cleanupInactiveExecutors 定期清理不健康的执行器
func (s *AdminServer) cleanupInactiveExecutors() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			log.Println("Cleaning up inactive executors...")
			// 15秒没有心跳则认为不活跃
			err := s.executorStore.CleanupInactive(15 * time.Second)
			if err != nil {
				log.Printf("Error cleaning up inactive executors: %v", err)
			}
		}
	}
}


// JobStore 接口定义了调度中心与任务存储交互的方法
type JobStore interface {
	CreateJob(job *task.Job) error
	UpdateJob(job *task.Job) error
	DeleteJob(id string) error
	GetJobByID(id string) (task.Job, error)
	ListJobs(page, pageSize int) ([]task.Job, int64, error)
	UpdateJobStatus(id string, status task.JobStatus) error
	LogExecution(jobID, executorID string, status task.JobStatus, output, errMsg string, duration int64) (string, error)
	UpdateExecutionLog(logID string, status task.JobStatus, output, errMsg string, duration int64) error
	GetJobLogs(jobID string, page, pageSize int) ([]task.JobExecutionLog, int64, error)
	GetRunnableJobs() ([]task.Job, error) // For scheduler to fetch jobs
}

// ExecutorStore 接口定义了调度中心与执行器存储交互的方法
type ExecutorStore interface {
	SaveExecutor(info task.ExecutorInfo) error
	GetExecutorByID(id string) (task.ExecutorInfo, error)
	GetHealthyExecutors() ([]task.ExecutorInfo, error) // 获取所有健康的执行器
	SelectHealthyExecutor(executorID string) (task.ExecutorInfo, error) // 调度中心选择执行器
	CleanupInactive(threshold time.Duration) error // 清理不活跃的执行器
}
```

**11. `main.go` (应用程序入口)**

```go
// main.go
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"your_project_name/admin"
	"your_project_name/config"
	"your_project_name/distributed"
	"your_project_name/executor"
	"your_project_name/scheduler"
	"your_project_name/service_discovery"
	"your_project_name/storage"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 初始化数据库存储
	jobStore, err := storage.NewMySQLJobStore(cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("Failed to connect to MySQL for job store: %v", err)
	}
	executorStore, err := storage.NewMySQLExecutorStore(cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("Failed to connect to MySQL for executor store: %v", err)
	}

	// 根据命令行参数或环境变量启动不同的服务
	serviceType := os.Getenv("SERVICE_TYPE") // ADMIN, SCHEDULER, EXECUTOR

	switch serviceType {
	case "ADMIN":
		log.Println("Starting Admin Server...")
		adminServer := admin.NewAdminServer(admin.AdminServerConfig{Port: cfg.AdminPort}, jobStore, executorStore)
		adminServer.Start()
		defer adminServer.Stop() // 确保优雅关闭
	case "SCHEDULER":
		log.Println("Starting Scheduler...")
		// 初始化分布式锁
		redisLocker := distributed.NewRedisLocker(cfg.RedisAddr)
		// 初始化执行器管理器
		executorManager := servicediscovery.NewHTTPExecutorManager(fmt.Sprintf("http://127.0.0.1:%s", cfg.AdminPort)) // 假设Admin Server在本地
		scheduler := scheduler.NewScheduler(jobStore, executorManager, redisLocker)
		scheduler.Start()
		defer scheduler.Stop() // 确保优雅关闭
	case "EXECUTOR":
		log.Println("Starting Executor...")
		executorID := os.Getenv("EXECUTOR_ID") // 每个执行器需要一个唯一的ID
		if executorID == "" {
			log.Fatal("EXECUTOR_ID environment variable not set")
		}
		// 初始化服务发现客户端
		sdClient := servicediscovery.NewHTTPSDClient(fmt.Sprintf("http://127.0.0.1:%s", cfg.AdminPort)) // 假设Admin Server在本地
		executor := executor.NewExecutor(executor.ExecutorConfig{
			ID:        executorID,
			Port:      cfg.ExecutorPort,
			AdminAddr: fmt.Sprintf("http://127.0.0.1:%s", cfg.AdminPort),
		}, sdClient)
		executor.Start()
		defer executor.Stop() // 确保优雅关闭
	default:
		log.Fatalf("Unknown SERVICE_TYPE: %s. Must be ADMIN, SCHEDULER, or EXECUTOR.", serviceType)
	}

	// 监听终止信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	log.Println("Shutting down gracefully...")
}

```

### 4. 如何运行这个系统

1.  **准备环境**:
    *   安装 Go (1.18+ 推荐)。
    *   安装 Docker (或本地安装 MySQL 和 Redis)。

2.  **启动 MySQL 和 Redis**:
    你可以使用 Docker 来快速启动它们。
    ```bash
    # 启动MySQL
    docker run --name mysql-db -e MYSQL_ROOT_PASSWORD=root -e MYSQL_DATABASE=task_scheduler -p 3306:3306 -d mysql:8.0

    # 启动Redis
    docker run --name redis-server -p 6379:6379 -d redis:latest
    ```
    等待容器启动完成。

3.  **创建数据库表**:
    连接到你的 MySQL 数据库（例如使用 `mysql -u root -p`），然后执行 `db_schema.sql` 中的 SQL 语句。

    ```bash
    mysql -u root -proot task_scheduler < db_schema.sql
    ```

4.  **初始化 Go 模块**:
    在项目根目录（`your_project_name/`）下运行：
    ```bash
    go mod tidy
    ```

5.  **启动 Admin Server**:
    在第一个终端中，设置环境变量并运行 `main.go`：
    ```bash
    export SERVICE_TYPE=ADMIN
    export ADMIN_PORT=8080
    export MYSQL_DSN="root:root@tcp(127.0.0.1:3306)/task_scheduler?charset=utf8mb4&parseTime=True&loc=Local"
    export REDIS_ADDR="127.0.0.1:6379"
    go run main.go
    ```
    你应该会看到 Admin Server 启动的日志。

6.  **启动 Executor (可以启动多个实例)**:
    在第二个终端中，设置环境变量并运行 `main.go`。`EXECUTOR_ID` 必须是唯一的。
    ```bash
    export SERVICE_TYPE=EXECUTOR
    export EXECUTOR_ID=executor-001
    export EXECUTOR_PORT=8082 # 可以是其他端口，如8083, 8084等
    export ADMIN_PORT=8080 # 指向Admin Server的端口
    export MYSQL_DSN="root:root@tcp(127.0.0.1:3306)/task_scheduler?charset=utf8mb4&parseTime=True&loc=Local"
    export REDIS_ADDR="127.0.0.1:6379"
    go run main.go
    ```
    执行器启动后，会定期向 Admin Server 发送心跳。你可以在 Admin Server 的日志中看到执行器注册的信息，也可以通过 Admin API (`GET /api/executors`) 查看。

7.  **启动 Scheduler (可以启动多个实例，但只有一个会是活跃的)**:
    在第三个终端中，设置环境变量并运行 `main.go`。
    ```bash
    export SERVICE_TYPE=SCHEDULER
    export ADMIN_PORT=8080 # 指向Admin Server的端口
    export MYSQL_DSN="root:root@tcp(127.0.0.1:3306)/task_scheduler?charset=utf8mb4&parseTime=True&loc=Local"
    export REDIS_ADDR="127.0.0.1:6379"
    go run main.go
    ```
    调度器启动后，会尝试获取分布式锁并定期从数据库加载任务。如果你启动多个调度器，只有一个能拿到锁并进行任务加载。

8.  **测试任务调度 (使用 cURL 或 Postman)**:

    *   **创建任务**:
        例如，创建一个每 10 秒执行一次的 Shell 命令任务：
        ```bash
        curl -X POST http://127.0.0.1:8080/api/job -H "Content-Type: application/json" -d '{
            "name": "My First Cron Job",
            "description": "Prints current time to stdout",
            "job_type": "SHELL_COMMAND",
            "cron_spec": "*/10 * * * * *",
            "payload": "echo \"Hello from cron job! Current time: $(date)\"",
            "timeout": 30,
            "retry_times": 0,
            "status": "PENDING"
        }'
        ```
        或者一个 HTTP 回调任务：
        ```bash
        curl -X POST http://127.0.0.1:8080/api/job -H "Content-Type: application/json" -d '{
            "name": "HTTP Callback Test",
            "description": "Calls a test HTTP endpoint",
            "job_type": "HTTP_CALLBACK",
            "interval": 20,
            "payload": "{\"url\":\"http://httpbin.org/post\",\"method\":\"POST\",\"headers\":{\"Content-Type\":\"application/json\"},\"body\":\"{\\\"data\\\":\\\"test_payload\\\"}\"}",
            "timeout": 15,
            "retry_times": 1,
            "status": "PENDING"
        }'
        ```
        确保 `payload` 中的 JSON 字符串被正确转义。

    *   **查看任务列表**:
        ```bash
        curl http://127.0.0.1:8080/api/jobs
        ```

    *   **手动触发任务**: (替换 `YOUR_JOB_ID` 为你创建的任务 ID)
        ```bash
        curl -X POST http://127.0.0.1:8080/api/job/YOUR_JOB_ID/trigger
        ```

    *   **查看任务执行日志**: (替换 `YOUR_JOB_ID` 为你创建的任务 ID)
        ```bash
        curl http://127.0.0.1:8080/api/job/logs/YOUR_JOB_ID
        ```

通过以上步骤，你将能运行一个基本的、具备调度和执行能力的任务调度系统。这为后续的扩展和优化提供了基础。

## 总结

一个任务调度系统的核心在于其能够高效、可靠地在预设时间点触发并执行各种类型的任务。我们通过 **任务定义**、**调度器**、**执行器** 和 **调度中心** 这四个主要组件，并结合 **数据持久化** 和 **分布式协调** 来实现了这一目标。

*   **核心机制简述**:
    *   **任务**: 定义了执行逻辑、调度策略和配置，是系统的基本执行单元。
    *   **调度器**: 负责根据任务的调度策略（如 Cron 表达式、固定间隔）在分布式环境下（通过分布式锁保证唯一性）准确触发任务。
    *   **执行器**: 接收调度器指令，实际执行任务（如 HTTP 回调、Shell 命令、Go 函数），并将执行结果和日志反馈给调度中心。
    *   **调度中心**: 提供统一的管理界面和 API，用于任务的增删改查、执行器管理、任务日志查询和监控。
    *   **数据持久化**: 确保任务定义、执行日志、执行器状态等关键数据不丢失，通常使用关系型数据库。
    *   **分布式协调**: 通过分布式锁（如基于 Redis）保证多调度器实例环境下的任务唯一调度，以及执行器注册与心跳机制。

*   **实现中的难点**:
    1.  **时间精度与并发管理**: 在高并发场景下，如何保证任务调度的准时性和执行的并发限制，避免资源耗尽。
    2.  **分布式环境下的数据一致性**: 多个调度器实例之间，以及调度器与执行器、调度中心之间的数据同步和状态一致性，防止任务重复调度或漏调。
    3.  **任务执行的隔离与安全性**: 特别是对于 Shell 命令等类型任务，如何在执行器层面提供安全沙箱环境，防止恶意命令或资源争抢。
    4.  **系统容错与故障恢复**: 如何设计任务失败重试机制、调度器/执行器故障时的自动发现、故障转移和数据恢复能力，保证系统的高可用性。
    5.  **监控与告警体系**: 构建全面的监控指标（如任务成功率、失败率、执行耗时、各组件资源利用率）和及时的告警机制是确保系统稳定运行的关键。
    6.  **灵活的任务类型扩展**: 抽象任务接口，使其能够方便地扩展新的任务类型而无需修改核心调度逻辑。

*   **运行中重点关注**:
    *   **日志**: 确保任务执行日志、调度器日志和执行器日志的完整性、可追溯性，便于问题排查。
    *   **监控**: 实时关注任务的执行状态（成功/失败）、执行器健康状况、分布式锁的争用情况，以及各服务实例的 CPU、内存、网络等资源使用情况。
    *   **告警**: 对任务执行失败、超时、执行器离线、调度器异常等关键事件设置告警，并确保告警的及时性。
    *   **系统负载**: 监控任务总量、并发量和每个组件的负载，及时发现瓶颈。

*   **后续扩展方向（类比 XXL-Job）**:
    要将此基础系统扩展为像 XXL-Job 那样功能完备的企业级调度平台，可以考虑以下方向：
    1.  **可视化管理界面**: 提供直观的 Web UI，进行任务配置、执行器管理、日志查询、统计报表等。
    2.  **更丰富的调度策略**: 支持依赖任务、子任务、生命周期钩子等高级调度能力。
    3.  **高级路由策略**: 除了简单的随机/指定路由，可实现负载均衡、故障转移、忙碌转移、分片广播等。
    4.  **任务参数化与版本控制**: 任务支持动态参数传递、任务模板化，以及任务版本的管理和回滚。
    5.  **任务分片与并行处理**: 支持大任务拆分为多个子任务并行执行，提高吞吐量。
    6.  **权限与安全**: 细粒度的用户权限控制、API 安全认证、任务执行的沙箱机制等。
    7.  **高可用与弹性伸缩**: 更完善的调度器集群、执行器动态扩缩容，与 Kubernetes 等容器编排平台深度集成。
    8.  **故障转移与恢复**: 自动检测故障，并进行任务的自动重试、失败回调、通知告警。
    9.  **数据统计与趋势分析**: 提供历史数据统计、执行趋势分析，辅助运营决策。
    10. **多租户支持**: 允许不同团队或业务线独立管理各自的任务。

通过逐步迭代和完善这些功能，一个从零开始构建的 Golang 任务调度系统可以逐步成长为媲美 XXL-Job 的强大平台。

