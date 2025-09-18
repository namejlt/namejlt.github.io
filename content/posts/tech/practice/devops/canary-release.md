---
title: "灰度发布实践"
date: 2025-09-18T13:50:00+08:00
draft: false
toc: true
categories: ["技术/实践/研发效能"]
tags: ["研发效能"]
---

## 灰度发布实践

灰度发布通常用来渐进式地将流量从旧版本切换到新版本，以验证新版本的稳定性和性能。

我们来详细讲解在Kubernetes（K8s）中实施灰度发布的完整流程。这篇解答将覆盖**核心概念、不同实现方式、研发层面的具体实操、以及如何通过自研CD程序调用K8s API来自动化灰度发布和验证**。

-----

### 第一部分：核心概念理解

#### 什么是灰度发布（Canary Release）？

灰度发布是一种**风险极低**的线上发布策略。它不是一次性将所有流量都切换到新版本，而是先引入一小部分流量（比如1%、5%）到新版本上。新版本和旧版本（稳定版）会同时在线运行。然后，通过观察新版本在真实流量下的核心业务指标（如错误率、延迟、CPU/内存使用率等），来判断其是否稳定。

如果指标正常，则逐步增大切到新版本的流量比例（如10% -\> 50% -\> 100%）。如果指标出现异常，可以立刻将所有流量切回旧版本，从而将风险和影响范围控制在最小。

#### K8s中灰度发布的核心机制

在K8s中，实现灰度发布的核心是**流量切分（Traffic Splitting）**。我们需要一个机制，能够精确地控制到达新、旧两个版本应用的请求流量比例。

与K8s自身的`RollingUpdate`（滚动更新）策略不同：

  * **滚动更新**：是Pod级别的实例替换。它会逐个停止旧的Pod，启动新的Pod。在更新过程中，流量是随机打到新旧Pod上的，你**无法精确控制流量比例**（只能通过新旧Pod数量的比例来近似控制），且回滚较慢。
  * **灰度发布**：是应用级别的流量调度。新旧两个版本的应用（通常是两组独立的Pod）**同时稳定运行**，通过上层的流量治理工具来精确分配流量。它将**应用部署**和**流量发布**这两个过程解耦，提供了更大的灵活性和安全性。

-----

### 第二部分：研发层面的具体实操

在K8s中，单纯使用原生的`Deployment`和`Service`资源很难做到精细的流量控制。因此，业界主流的实现方式是借助**Ingress Controller**或**服务网格（Service Mesh）**。

这里我们以最常用、最简单的 **Ingress-NGINX** 为例进行实操讲解。

#### 准备工作

1.  **一个K8s集群**，并已安装 **Ingress-NGINX Controller**。
2.  **一个应用**，打包成两个不同版本的Docker镜像，例如 `myapp:v1` (稳定版) 和 `myapp:v2` (灰度版)。
3.  `kubectl` 命令行工具。

#### 实操步骤

假设我们有一个稳定运行的应用v1。

**第1步：部署稳定版应用 (v1)**

我们需要一个`Deployment`来管理v1的Pod，和一个`Service`将这些Pod暴露出来。

`stable-deployment-v1.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp-v1
spec:
  replicas: 3
  selector:
    matchLabels:
      app: myapp
      version: v1
  template:
    metadata:
      labels:
        app: myapp
        version: v1
    spec:
      containers:
      - name: myapp
        image: myapp:v1
        ports:
        - containerPort: 80
```

`service.yaml`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: myapp-service
spec:
  ports:
  - port: 80
    targetPort: 80
    protocol: TCP
  selector:
    # 这个Selector非常关键，它同时选中了v1和后续的v2
    app: myapp
```

  * **注意**：`Service`的`selector`只选择了`app: myapp`，这是一个**通用标签**，目的是让这个Service能同时发现v1和v2的Pod。版本的区分将交由Ingress来处理。

**第2步：部署灰度版应用 (v2)**

现在，我们部署新版本v2。它是一个独立的`Deployment`，但使用与v1相同的`app: myapp`标签，以便被同一个`Service`发现。

`canary-deployment-v2.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp-v2
spec:
  replicas: 1 # 灰度版本通常初始副本数较少
  selector:
    matchLabels:
      app: myapp
      version: v2
  template:
    metadata:
      labels:
        app: myapp
        version: v2
    spec:
      containers:
      - name: myapp
        image: myapp:v2
        ports:
        - containerPort: 80
```

执行`kubectl apply`部署v1和v2后，`myapp-service`现在会同时代理到v1和v2的Pod。

**第3步：通过Ingress实现流量切分（核心步骤）**

这是实现灰度的关键。我们需要创建两个`Ingress`资源：一个用于主流量，一个用于灰度流量。

`ingress.yaml`:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress-stable
  annotations:
    # 这是标准的Ingress配置，指向我们的主Service
    kubernetes.io/ingress.class: "nginx"
spec:
  rules:
  - host: myapp.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: myapp-service
            port:
              number: 80
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress-canary
  annotations:
    kubernetes.io/ingress.class: "nginx"
    # 关键的Canary注解
    nginx.ingress.kubernetes.io/canary: "true"
    # 按权重切分10%的流量到灰度版本
    nginx.ingress.kubernetes.io/canary-weight: "10" 
spec:
  rules:
  - host: myapp.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            # 注意：这里的service和stable用的是同一个
            # Ingress Controller会根据注解将流量路由到不同版本的Pod
            name: myapp-service 
            port:
              number: 80
```

**注解解释：**

  * `nginx.ingress.kubernetes.io/canary: "true"`：告诉Ingress-NGINX这是一个灰度规则。
  * `nginx.ingress.kubernetes.io/canary-weight: "10"`：将10%的流量发送到这个Ingress规则所定义的后端。由于这个Ingress我们后续会关联到v2版本，所以相当于10%的流量会到v2。

**但是，上面的配置还有一个问题**：两个Ingress都指向同一个`Service`，而`Service`会随机负载均衡到v1和v2的Pod。我们如何让灰度Ingress的流量**只**到达v2的Pod呢？

**答案是：我们需要为v1和v2创建独立的Service。**

**修正后的最佳实践：**

1.  **为v1和v2创建各自的Service**

      * `myapp-service-v1` -\> `selector: {app: myapp, version: v1}`
      * `myapp-service-v2` -\> `selector: {app: myapp, version: v2}`

2.  **修改Ingress配置**

      * `myapp-ingress-stable` 的 `backend.service.name` 指向 `myapp-service-v1`。
      * `myapp-ingress-canary` 的 `backend.service.name` 指向 `myapp-service-v2`。

这样，流量路径就非常清晰了：

  * 90%的流量 -\> `myapp-ingress-stable` -\> `myapp-service-v1` -\> `v1 Pods`
  * 10%的流量 -\> `myapp-ingress-canary` -\> `myapp-service-v2` -\> `v2 Pods`

**灰度发布流程：**

1.  **初始状态**: 只有`stable`的`Deployment`, `Service`和`Ingress`。
2.  **开始灰度**: 部署`canary`的`Deployment(v2)`和`Service(v2)`。然后部署`canary`的`Ingress`，并将`canary-weight`设置为一个较小的值（如5）。
3.  **监控验证**: 观察v2版本的日志、监控指标。
4.  **增加流量**: 如果一切正常，通过`kubectl edit ingress myapp-ingress-canary`或`kubectl patch`命令，逐步调大`canary-weight`的值（如20 -\> 50）。在每个阶段都留出足够的观察时间。
5.  **全量发布**: 将`canary-weight`调整为`100`。此时所有流量都进入v2。观察一段时间确认稳定。
6.  **清理旧版**: 将`stable`的`Ingress`的`backend`指向`myapp-service-v2`，然后删除`canary`的`Ingress`。最后，安全地删除v1的`Deployment`和`Service`。v2正式成为新的v1。

-----

### 第三部分：自研CD程序调用K8s API实现自动化

现在，我们把上述手动流程，用一个自己编写的CD程序（例如用Go或Python）来自动化。

这个程序的核心是与K8s API Server进行交互。你需要使用相应的K8s客户端库（如Go的`client-go`，Python的`kubernetes`）。

#### 自动化灰度流程设计

```
CD程序启动 ->
  1. 部署新版本(v2)的Deployment和Service ->
  2. 创建灰度Ingress，设置初始权重(weight=5) ->
  3. 进入监控循环 (Loop):
     a. 等待一个观察周期 (如5分钟)
     b. [验证环节] 调用监控系统API (如Prometheus) 获取v2版本的核心指标
     c. 判断指标是否健康:
        - 如果健康:
            - 增加灰度权重 (weight += 10)
            - 调用K8s API更新灰度Ingress的annotations
            - 如果权重已达100，跳出循环，进入全量步骤
        - 如果不健康:
            - [回滚] 调用K8s API将灰度Ingress权重设为0
            - 发送告警
            - 退出程序
  4. [全量发布] 更新主Ingress指向v2的Service ->
  5. [清理] 删除灰度Ingress和旧版本(v1)的Deployment/Service ->
  6. 发布成功
```

#### 调用K8s API的关键操作

假设使用Python客户端库：

```python
from kubernetes import client, config

# 加载K8s配置 (in-cluster or from kubeconfig)
config.load_kube_config()
networking_v1_api = client.NetworkingV1Api()
apps_v1_api = client.AppsV1Api()

# 示例：更新灰度Ingress的权重
def update_canary_weight(ingress_name, namespace, new_weight):
    try:
        # 1. 获取当前的Ingress对象
        ingress = networking_v1_api.read_namespaced_ingress(name=ingress_name, namespace=namespace)
        
        # 2. 修改annotations
        ingress.metadata.annotations["nginx.ingress.kubernetes.io/canary-weight"] = str(new_weight)
        
        # 3. 使用patch或replace方法更新对象
        api_response = networking_v1_api.patch_namespaced_ingress(
            name=ingress_name,
            namespace=namespace,
            body=ingress
        )
        print(f"Ingress {ingress_name} weight updated to {new_weight}")
    except client.ApiException as e:
        print(f"Error updating ingress: {e}")
        # 触发回滚逻辑
```

其他操作类似：

  * **部署Deployment**: 使用`apps_v1_api.create_namespaced_deployment(...)`
  * **删除Deployment**: 使用`apps_v1_api.delete_namespaced_deployment(...)`
  * **创建/删除Service**: 使用`client.CoreV1Api()`对应的方法。

#### 如何自动化验证？

这是灰度发布成败的关键。你的CD程序必须能够量化地判断新版本是否“健康”。

1.  **数据源**: 最常用的是**Prometheus**。你需要确保你的应用和Ingress Controller都暴露了Prometheus格式的指标。
2.  **关键指标 (Golden Signals)**:
      * **延迟 (Latency)**: v2版本的P95/P99请求延迟是否在可接受范围内。
      * **错误率 (Errors)**: v2版本的HTTP 5xx错误率是否急剧上升。
      * **流量 (Traffic)**: v2版本的QPS是否符合预期权重。
      * **饱和度 (Saturation)**: v2 Pod的CPU和内存使用率是否过高。
3.  **验证逻辑**:
      * **定义基线**: 在灰度开始前，获取v1稳定版本的对应指标作为基线。
      * **设定阈值**: 定义一个可接受的偏离范围。例如，“v2的错误率不能超过v1的1.5倍”或“P99延迟不能超过200ms”。
      * **API查询**: CD程序在每个观察周期，通过HTTP请求查询Prometheus的API（PromQL）。
          * 查询v2错误率: `sum(rate(nginx_ingress_controller_requests{service="myapp-service-v2", status=~"5.*"}[1m])) / sum(rate(nginx_ingress_controller_requests{service="myapp-service-v2"}[1m]))`
          * 查询v2延迟: `histogram_quantile(0.99, sum(rate(nginx_ingress_controller_request_duration_seconds_bucket{service="myapp-service-v2"}[5m])) by (le))`
      * **程序化决策**: CD程序比较查询结果和阈值，如果超出阈值，则自动执行回滚操作（将`canary-weight`设为0）。

### 总结与高级实践

  * **核心思想**: 将\*\*部署（上线代码）**与**发布（切换流量）\*\*解耦，通过精细化的流量控制来管理风险。
  * **自动化是关键**: 手动执行灰度发布容易出错且效率低下。一个集成了K8s API调用和监控系统API查询的自动化CD平台是实现可靠灰度发布的必要条件。
  * **更高级的工具**:
      * **服务网格 (Service Mesh)**: 如Istio, Linkerd。它们在7层提供了更强大的流量治理能力，可以实现基于用户请求头（如`user-agent`, `cookie`）、用户ID等更复杂的灰度策略，而不仅仅是权重。
      * **渐进式交付工具**: 如**Argo Rollouts**, **Flagger**。这些工具是K8s上的开源项目，它们封装了上面我们讨论的所有自动化逻辑（部署新版、修改Ingress/Service Mesh、查询监控、自动决策），你只需要通过声明式的YAML来定义灰度策略即可，无需自己编写复杂的CD程序。**对于大多数团队来说，直接使用这类工具是更推荐的最佳实践。**