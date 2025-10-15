---
title: "flink实践"
date: 2025-10-15T10:30:00+08:00
draft: false
toc: true
featured: false
categories: ["技术/实践/后端"]
tags: ["flink"]
---

## flink实践

本文将从业界最佳实践的角度，详细分析您提出的关于 Apache Flink 的几个核心问题，并提供相应的 Golang 代码示例和流程图。

### **核心摘要**

Apache Flink 是一个为分布式、高性能、高可用、高精度的流处理应用程序设计的开源框架和分布式处理引擎。其核心优势在于其强大的流处理能力、状态管理和事件时间处理机制。在现代数据架构中，Flink 常常作为实时数据处理的核心引擎，与 Kafka、Hadoop 生态、各类数据库等紧密集成。

-----

### **1. Flink 的部署方式分析（特别是无 Hadoop 场景）**

从业界最佳实践来看，Flink 的部署方式应根据您的现有技术栈、资源管理需求、以及对运维复杂度的容忍度来选择。

#### **主流部署模式对比**

| 部署模式 | 优点 | 缺点 | 适用场景 |
| :--- | :--- | :--- | :--- |
| **Standalone (独立集群)** | - **简单快速**：部署最简单，不依赖任何外部资源管理器。<br>- **资源独占**：Flink 完全控制其分配的资源，性能稳定。<br>- **无 Hadoop 依赖**：可以完全脱离 Hadoop 生态运行。 | - **资源隔离性差**：多租户场景下，无法做到精细的资源隔离和管理。<br>- **弹性伸缩能力弱**：需要手动增删节点，自动化程度低。<br>- **高可用（HA）配置相对复杂**：需要依赖 ZooKeeper 和 HDFS/S3 等外部存储。 | - 开发测试环境。<br>- 对资源隔离要求不高的中小型生产环境。<br>- 期望最小化依赖，快速上线的场景。 |
| **On YARN** | - **资源统一管理**：与 Hadoop 生态无缝集成，由 YARN 统一调度和管理资源。<br>- **弹性伸缩**：可以根据负载动态申请和释放资源。<br>- **多租户支持**：天然支持多租户和资源队列。 | - **强依赖 Hadoop**：必须部署和维护一个 Hadoop YARN 集群。<br>- **配置复杂**：需要理解 YARN 的配置和工作原理。<br>- **延迟可能更高**：资源申请需要通过 YARN 的调度，可能引入额外延迟。 | - 已经拥有成熟 Hadoop/YARN 集群的企业。<br>- 需要进行批处理和流处理混合部署的场景。<br>- 对资源利用率和多租户隔离有高要求的复杂生产环境。 |
| **On Kubernetes (K8s)** | - **云原生**：拥抱云原生生态，是未来的主流方向。<br>- **高度自动化和弹性**：利用 K8s 的强大能力（如自动伸缩、自愈、滚动更新）管理 Flink 作业。<br>- **资源隔离强**：基于容器的隔离，比 YARN 更彻底。<br>- **环境一致性**：打包成镜像，保证开发、测试、生产环境的一致性。 | - **学习曲线陡峭**：需要专业的 K8s 运维知识。<br>- **网络和存储配置复杂**：需要处理容器网络、持久化存储等问题。<br>- **社区仍在快速发展**：相比 YARN，在某些方面可能不够成熟。 | - 拥有 K8s 技术栈和运维能力的企业。<br>- 追求高度自动化运维和云原生架构的场景。<br>- 新建的、没有历史 Hadoop 技术债的实时计算平台。 |

#### **无 Hadoop 部署实践 (Standalone + S3/MinIO)**

在很多现代云原生架构中，为了轻量化和解耦，我们希望避免部署重量级的 HDFS。这时，**Standalone 模式配合对象存储（如 AWS S3, MinIO）** 是一个绝佳的选择。

  * **状态后端 (State Backend)**：将 Flink 的状态（例如 `RocksDBStateBackend`）存储在 S3 或 MinIO 上。这使得 Flink 作业可以无状态地重启和恢复。
  * **检查点 (Checkpoint) 和保存点 (Savepoint)**：同样存储在对象存储上，实现作业的高可用和版本迁移。
  * **高可用 (HA)**：使用 ZooKeeper 进行 JobManager 的主备选举，并将元数据存储在对象存储上。

#### **部署决策流程图**

```mermaid
graph TD
    A[开始: Flink 部署决策] --> B{是否已有 Hadoop YARN 集群?};
    B -- 是 --> C[优先考虑 Flink on YARN];
    B -- 否 --> D{是否拥有或计划构建 Kubernetes (K8s) 平台?};
    D -- 是 --> E[优先考虑 Flink on Kubernetes (Native/Operator)];
    D -- 否 --> F{是开发测试环境或小型生产环境?};
    F -- 是 --> G[推荐 Standalone 模式];
    F -- 否 --> H{对资源隔离和弹性有高要求?};
    H -- 是 --> I["重新评估引入 K8s 的可行性 (长期收益高)"];
    H -- 否 --> G;
    G --> J["配置 Checkpoint/State 到对象存储 (如 S3/MinIO)"];
    C --> K[结束];
    E --> K;
    I --> K;
    J --> K;

    subgraph Legend
        direction LR
        L1[决策节点]
        L2[推荐方案]
        L3[关键配置]
    end

    style A fill:#f9f,stroke:#333,stroke-width:2px
    style K fill:#f9f,stroke:#333,stroke-width:2px
    style C fill:#9f9,stroke:#333,stroke-width:2px
    style E fill:#9f9,stroke:#333,stroke-width:2px
    style G fill:#9f9,stroke:#333,stroke-width:2px
    style I fill:#ff9,stroke:#333,stroke-width:2px
    style J fill:#add,stroke:#333,stroke-width:1px
```

-----

### **2. Flink CDC (Change Data Capture) 机制使用**

Flink CDC Connectors 是一个革命性的功能，它允许 Flink 直接从数据库的事务日志（如 MySQL binlog, PostgreSQL WAL）中捕获数据变更，并将这些变更作为事件流进行处理。

#### **核心机制**

1.  **连接器**：Flink CDC 为多种数据库（MySQL, PostgreSQL, MongoDB, Oracle 等）提供了专门的连接器。
2.  **日志读取**：连接器伪装成一个数据库的从库，实时读取并解析数据库的事务日志。
3.  **格式转换**：将日志中的 `INSERT`, `UPDATE`, `DELETE` 操作转换成 Flink 的数据流格式（通常是 `RowData` 或 JSON）。每条消息都包含了变更前（before）和变更后（after）的数据，以及操作类型（op）。
4.  **精确一次 (Exactly-Once)**：通过 Flink 的 Checkpoint 机制，CDC 连接器可以记录其在日志中的读取位置。当作业失败恢复时，它可以从上次成功 checkpoint 的位置继续读取，从而保证数据不重不漏。

#### **使用场景**

  * **实时数据同步**：将业务库的数据变更实时同步到数据仓库（如 Doris, StarRocks）或数据湖（如 Hudi, Iceberg）。
  * **实时物化视图**：根据上游多个表的变化，实时计算并更新物化视图。
  * **数据缓存更新**：当数据库变更时，实时更新 Redis 或其他缓存。

#### **Golang 代码示例 (通过 Flink REST API 提交 CDC SQL 作业)**

在 Go 中，我们通常不直接编写 Flink 的底层代码，而是通过 Flink 的 **REST API** 来管理和提交作业。这是一种语言无关的最佳实践，将 Go 的业务逻辑与 Flink 的计算逻辑解耦。

下面的示例演示了如何使用 Go 向 Flink 集群提交一个从 MySQL 同步数据到 Kafka 的 CDC SQL 作业。

```go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
)

// FlinkJobSubmitRequest 定义了提交作业的请求体
type FlinkJobSubmitRequest struct {
	Statement string `json:"statement"`
}

// FlinkJobSubmitResponse 定义了提交作业的响应
type FlinkJobSubmitResponse struct {
	JobId string `json:"jobid"`
}

// submitFlinkSQLJob 向 Flink SQL Gateway 提交一个 SQL 作业
func submitFlinkSQLJob(flinkGatewayURL, sqlStatement string) (string, error) {
	requestBody, err := json.Marshal(FlinkJobSubmitRequest{Statement: sqlStatement})
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Flink 1.15+ 使用 v1/statements endpoint
	resp, err := http.Post(fmt.Sprintf("%s/v1/statements", flinkGatewayURL), "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		return "", fmt.Errorf("failed to send request to Flink Gateway: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to submit job, status: %s, body: %s", resp.Status, string(bodyBytes))
	}

    // 实际场景中，SQL Gateway 的提交流程更复杂，需要轮询结果
    // 这里简化为直接获取一个标识
    // 假设返回的 Body 中有 operationHandle, 然后用 handle 去查状态和最终的 JobID
	log.Println("Job submission initiated successfully.")
    // 在真实场景中，你需要解析响应，获取一个 operation handle，然后轮询这个 handle 的状态
    // 当作业成功启动后，才能获取到 JobID。这里为了简化，我们假设它直接返回一个可追踪的ID。
    // 这是一个示意性的ID。
	return "mock_job_id_12345", nil
}

func main() {
	// Flink SQL Gateway 的地址
	gatewayURL := "http://localhost:8083"

	// 定义 Flink CDC SQL 作业
	// 这个 SQL 创建了一个 MySQL CDC 源表和 Kafka 目标表，并将数据从源写入目标
	cdcSQL := `
-- 设置作业参数
SET 'execution.checkpointing.interval' = '10s';
SET 'table.exec.state.ttl' = '3600s';

-- 1. 创建 MySQL CDC 源表 (users)
CREATE TABLE mysql_users (
    id INT,
    name STRING,
    email STRING,
    PRIMARY KEY (id) NOT ENFORCED
) WITH (
    'connector' = 'mysql-cdc',
    'hostname' = 'mysql-server',
    'port' = '3306',
    'username' = 'flink-user',
    'password' = 'flink-password',
    'database-name' = 'my_database',
    'table-name' = 'users'
);

-- 2. 创建 Kafka 目标表 (users_cdc_topic)
CREATE TABLE kafka_users_cdc (
    id INT,
    name STRING,
    email STRING,
    PRIMARY KEY (id) NOT ENFORCED -- 定义主键以便使用 upsert-kafka
) WITH (
    'connector' = 'upsert-kafka',
    'topic' = 'users_cdc_topic',
    'properties.bootstrap.servers' = 'kafka-broker:9092',
    'key.format' = 'json',
    'value.format' = 'json'
);

-- 3. 创建一个 Insert-Only 的 Kafka Sink，用于审计
CREATE TABLE kafka_audit_log (
    change_log STRING
) WITH (
    'connector' = 'kafka',
    'topic' = 'audit_log_topic',
    'properties.bootstrap.servers' = 'kafka-broker:9092',
    'format' = 'json'
);


-- 4. 定义数据处理和写入逻辑 (创建并执行一个 statement set)
EXECUTE STATEMENT SET
BEGIN
    -- 将 CDC 数据流实时写入到 upsert-kafka，实现数据同步
    INSERT INTO kafka_users_cdc
    SELECT id, name, email FROM mysql_users;

    -- 同时，将变更的详细信息写入另一个审计 topic
    INSERT INTO kafka_audit_log
    SELECT
      CAST(CURRENT_TIMESTAMP AS STRING) || ' - User ' || CAST(id AS STRING) || ' was updated.'
    FROM mysql_users;
END;
`

	log.Println("Submitting Flink CDC SQL job...")
	jobID, err := submitFlinkSQLJob(gatewayURL, cdcSQL)
	if err != nil {
		log.Fatalf("Error submitting job: %v", err)
	}

	log.Printf("Flink job submitted successfully! Track Job ID: %s", jobID)
}

```

#### **Flink CDC 数据流转流程图**

```mermaid
graph TD
    subgraph 数据库
        A[MySQL] --> B[Binlog 事务日志];
    end

    subgraph Flink 集群
        C[Flink MySQL CDC Connector] -- 实时读取 --> B;
        C -- 解析为<br>INSERT/UPDATE/DELETE 事件 --> D[DataStream API<br>或 Table API 流];
        D -- Checkpointing<br>记录 Binlog 位置 --> E[State Backend<br>(如 RocksDB on S3)];
        D --> F[业务逻辑处理<br>(转换/聚合/过滤)];
        F --> G[Flink Kafka Sink];
    end

    subgraph 外部系统
        G -- 写入 --> H[Kafka Topic];
    end

    style A fill:#D5E8D4,stroke:#82B366
    style H fill:#DAE8FC,stroke:#6C8EBF
    style E fill:#FFE6CC,stroke:#D79B00
```

-----

### **3. Flink 对接 Kafka 的具体使用**

Kafka 是 Flink 最常见、最核心的搭档。Flink 既可以作为 Kafka 的消费者（Source），也可以作为生产者（Sink）。

#### **核心要点**

  * **Connector**: 使用官方的 `flink-connector-kafka`。
  * **序列化/反序列化**: Flink 需要知道如何解析 Kafka topic 中的二进制数据。常见的格式有 JSON, Avro, Protobuf 等。
  * **Exactly-Once 语义**: 这是 Flink + Kafka 组合的王牌特性。通过 Flink 的两阶段提交（Two-Phase Commit）协议和 Kafka 的事务性写入，可以实现端到端的精确一次处理。
      * **Source 端**: Flink 在 Checkpoint 时会保存 Kafka consumer 的 offset。
      * **Sink 端**: Flink Kafka Sink 使用 Kafka 事务。在一个 Checkpoint 周期内，所有数据被写入一个临时的事务中。当 Checkpoint 完成时，Flink 才提交这个事务，使得数据对下游消费者可见。
  * **消费模式**:
      * `setStartFromGroupOffsets` (默认): 从 ZK/Kafka 中记录的 consumer group offset 开始消费。
      * `setStartFromEarliest()`: 从最早的 offset 开始。
      * `setStartFromLatest()`: 从最新的 offset 开始。
      * `setStartFromTimestamp(...)`: 从指定时间戳开始。

#### **Golang 代码示例 (通过 Flink REST API 提交 DataStream 作业 JAR)**

对于非 SQL 的复杂 DataStream 作业，最佳实践是使用 Java/Scala 编写作业逻辑，打包成 JAR，然后使用 Go 调用 Flink 的 REST API 来上传并运行这个 JAR。

**第一步：编写 Flink Java/Scala 作业 (示例)**

```java
// WordCount.java
public class WordCount {
    public static void main(String[] args) throws Exception {
        StreamExecutionEnvironment env = StreamExecutionEnvironment.getExecutionEnvironment();

        Properties properties = new Properties();
        properties.setProperty("bootstrap.servers", "kafka:9092");
        properties.setProperty("group.id", "wordcount-group");

        FlinkKafkaConsumer<String> kafkaSource = new FlinkKafkaConsumer<>(
            "input-topic", new SimpleStringSchema(), properties);

        DataStream<String> stream = env.addSource(kafkaSource);

        DataStream<Tuple2<String, Integer>> counts = stream
            .flatMap((String line, Collector<Tuple2<String, Integer>> out) -> {
                for (String word : line.split("\\s")) {
                    out.collect(new Tuple2<>(word, 1));
                }
            })
            .keyBy(value -> value.f0)
            .sum(1);

        FlinkKafkaProducer<String> kafkaSink = new FlinkKafkaProducer<>(
            "output-topic",
            new SimpleStringSchema(),
            properties);

        counts.map(Tuple2::toString).addSink(kafkaSink);

        env.execute("Kafka WordCount Job");
    }
}
```

将这个 Java 程序打包成 `wordcount.jar`。

**第二步：使用 Go 提交 JAR 作业**

```go
package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

// FlinkJarUploadResponse 定义了上传 JAR 的响应
type FlinkJarUploadResponse struct {
	FileName string `json:"filename"`
	Status   string `json:"status"`
}

// FlinkJarRunResponse 定义了运行 JAR 的响应
type FlinkJarRunResponse struct {
	JobID string `json:"jobid"`
}

// uploadJar 上传 JAR 文件到 Flink 集群
func uploadJar(flinkJobManagerURL, jarPath string) (string, error) {
	file, err := os.Open(jarPath)
	if err != nil {
		return "", fmt.Errorf("failed to open jar file: %w", err)
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("jarfile", filepath.Base(jarPath))
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return "", fmt.Errorf("failed to copy file to form: %w", err)
	}
	writer.Close()

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/jars/upload", flinkJobManagerURL), body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send upload request: %w", err)
	}
	defer resp.Body.Close()

    // ... 解析响应并返回 JarID (例如: a1b2c3d4-....jar)
	// 这里为了简化，直接返回硬编码的 JarID
	return "a1b2c3d4-e5f6-g7h8-i9j0-k1l2m3n4o5p6_wordcount.jar", nil
}

// runJar 运行已上传的 JAR
func runJar(flinkJobManagerURL, jarID string) (string, error) {
	runURL := fmt.Sprintf("%s/jars/%s/run", flinkJobManagerURL, jarID)
	resp, err := http.Post(runURL, "application/json", nil)
	if err != nil {
		return "", fmt.Errorf("failed to send run request: %w", err)
	}
	defer resp.Body.Close()
    
    // ... 解析响应获取 JobID
	return "job_abc_123", nil
}

func main() {
	jobManagerURL := "http://localhost:8081"
	jarPath := "./wordcount.jar"

	log.Println("Uploading JAR to Flink cluster...")
	jarID, err := uploadJar(jobManagerURL, jarPath)
	if err != nil {
		log.Fatalf("Error uploading JAR: %v", err)
	}
	log.Printf("JAR uploaded successfully. JarID: %s", jarID)

	log.Println("Running Flink job from JAR...")
	jobID, err := runJar(jobManagerURL, jarID)
	if err != nil {
		log.Fatalf("Error running JAR: %v", err)
	}
	log.Printf("Job started successfully! JobID: %s", jobID)
}
```

#### **Flink-Kafka 交互流程图**

```mermaid
graph TD
    subgraph Kafka 集群
        A[Kafka Topic (Source)]
        B[Kafka Topic (Sink)]
    end

    subgraph Flink 作业
        C[Flink Kafka Consumer] -- 消费数据 --> A;
        C -- Offset Commit to Kafka/ZK<br> during Checkpoint --> A;
        C --> D[DataStream / Table API];
        D -- 业务逻辑 --> E[处理算子<br>e.g., map, filter, keyBy, window];
        E --> F[Flink Kafka Producer];
        F -- Two-Phase Commit 事务写入 --> B;
        
        G[JobManager] -- 协调 --> H[Checkpoint Coordinator];
        H -- 触发 Checkpoint --> C;
        H -- 触发 Checkpoint --> E;
        H -- 触发 Checkpoint --> F;
        I[State Backend] -- 存储状态快照 --> H;
    end

    style A fill:#DAE8FC,stroke:#6C8EBF
    style B fill:#DAE8FC,stroke:#6C8EBF
    style I fill:#FFE6CC,stroke:#D79B00
```

-----

### **4. Flink 的核心原理**

理解 Flink 的核心原理是设计健壮、高性能实时应用的基础。

#### **1. 统一的流批一体架构**

Flink 的核心思想是“一切皆为流”。批处理被看作是流处理的一种特例（有界流）。这使得一套 API 和引擎可以同时处理实时流数据和历史批数据，大大简化了开发和运维。

#### **2. 分布式数据流 (Dataflow)**

  * **程序结构**: Flink 程序由 **Source**（数据源）、**Transformation**（转换操作，如 `map`, `filter`, `keyBy`）和 **Sink**（数据汇）组成。
  * **逻辑图 (Logical/Dataflow Graph)**: 用户编写的代码会被 Flink 优化器转换成一个有向无环图（DAG）。
  * **物理执行图 (Execution Graph)**: JobManager 将逻辑图转换成物理执行图，它描述了具体的算子任务（Task）如何在 TaskManager 的 Slot（计算资源槽）中并行执行。

#### **3. 核心基石：Checkpointing（检查点）**

Checkpoint 是 Flink 实现容错和 Exactly-Once 语义的基石。

  * **机制**: JobManager 定期向所有 Source 注入一个特殊的数据结构，称为 **Barrier（栅栏）**。这个 Barrier 会随着数据流在整个 DAG 中流动。
  * **对齐 (Alignment)**: 当一个算子任务收到了所有上游输入流的 Barrier 时，意味着这个 Barrier 之前的数据都已经处理完毕。此时，该算子会将其当前的状态（例如，窗口计算的中间结果）异步快照到配置的持久化存储（如 HDFS 或 S3）中，然后将 Barrier 向下游广播。
  * **故障恢复**: 当作业失败时，Flink 会停止所有任务，从上一个**完整成功**的 Checkpoint 恢复所有算子的状态，并让 Source 从 Checkpoint 中记录的 offset 位置重新开始消费数据。由于状态已经恢复，所以重新处理的数据不会导致结果重复计算，从而实现 Exactly-Once。

#### **4. 状态管理 (State Management)**

  * **定义**: 状态是指在处理数据流过程中需要记住的信息，例如 `sum` 操作中的当前总和，或者窗口中的所有元素。
  * **State Backend**: Flink 提供了不同的状态后端来管理状态的存储。
      * `HashMapStateBackend`: 状态存储在 TaskManager 的 JVM 堆内存中，快照到 JobManager 内存（不推荐生产使用）或文件系统。速度快，但受限于内存大小。
      * `EmbeddedRocksDBStateBackend`: 状态存储在 TaskManager 本地磁盘上的 RocksDB 实例中，增量快照到持久化文件系统。可以支持超大规模的状态，是生产环境的首选。

#### **5. 时间概念**

  * **事件时间 (Event Time)**: 事件实际发生的时间，通常嵌入在数据记录中。使用事件时间可以处理乱序数据和延迟数据，得到确定性的、可重现的结果。
  * **处理时间 (Processing Time)**: 数据到达 Flink 算子被处理时，机器的系统时间。简单，但结果不确定，受网络延迟和背压影响。
  * **摄入时间 (Ingestion Time)**: 数据进入 Flink Source 的时间。
  * **Watermark (水位线)**: Watermark 是 Flink 用来衡量事件时间进度的机制。`Watermark(t)` 表示时间戳小于等于 `t` 的事件已经全部到达。它是处理乱序数据的关键，用来触发窗口计算。

#### **Flink 作业生命周期与核心原理流程图**

```mermaid
graph TD
    A[Go App: 提交作业 JAR/SQL] --> B[Flink JobManager];
    subgraph Flink 集群
        B -- 1. 接收作业 --> C[解析代码, 生成 Dataflow Graph];
        C -- 2. 优化 & 并行化 --> D[生成 Execution Graph];
        D -- 3. 部署任务 --> E[TaskManager Slot];
        E -- 4. 启动 Task (Source/Operator/Sink) --> F[数据开始流动];
        
        G[Checkpoint Coordinator<br>in JobManager] -- 5a. 定期触发 Checkpoint --> H[Source Tasks];
        H -- 注入 Barrier --> F;
        F -- Barrier 流动 & 对齐 --> I[Operator Tasks];
        I -- 6. 状态快照 --> J[State Backend (e.g., S3)];
        J -- 报告快照完成 --> G;
        F -- 数据写入 --> K[External Sink (e.g., Kafka)];

        subgraph "故障发生 (TaskManager 宕机)"
            L[TaskManager Down]
        end
        
        B -- 检测到故障 --> M[作业重启];
        M -- 7. 从最新成功的 Checkpoint 恢复 --> J;
        J -- 加载状态 --> N[新的 TaskManager Slot];
        H -- 从 Checkpoint 中的 offset 重新消费 --> O[External Source (e.g., Kafka)];
        N -- 8. 作业从恢复的状态继续处理 --> F;
    end
    
    style J fill:#FFE6CC,stroke:#D79B00
    style L fill:#FFCCCC,stroke:#CC0000
    style M fill:#FFCCCC,stroke:#CC0000
```

最后，要根据实际情况来做调整，在实际项目中，选择最适合您团队技术栈、业务需求和运维能力的方案是关键。