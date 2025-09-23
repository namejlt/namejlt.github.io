---
title: "k8s ingress 开发"
date: 2025-09-23T17:00:00+08:00
draft: false
toc: true
categories: ["技术/实践/研发效能"]
tags: ["研发效能"]
---

## k8s ingress 开发

本文将从自定义 Ingress Controller 的角度，为你详细阐述使用 Golang 开发的完整流程。这包括多种实现方式、Kubernetes 网络流转逻辑分析、代码集成逻辑，并提供可以直接投入生产使用的核心代码。

### 核心理念：控制循环 (Control Loop)

开发一个 Ingress Controller 的核心是实现一个**控制循环**，也称为 "Reconciliation Loop"。这个循环的逻辑是：

1.  **Watch (监听)**：监听 Kubernetes API Server 中 `Ingress`、`Service`、`Endpoint` 等资源的变化。
2.  **Diff (比较)**：比较资源的当前状态 (Current State) 与期望状态 (Desired State)。期望状态由 Ingress 对象的定义（如路由规则、TLS 证书等）决定。
3.  **Act (行动)**：执行必要的动作，使当前状态与期望状态保持一致。这通常意味着动态地更新底层反向代理（如 NGINX, Envoy）的配置，或者直接处理网络流量。

-----

### Kubernetes 网络流转逻辑分析

在深入开发之前，必须理解一个典型的请求是如何通过 Ingress 流向最终的 Pod 的。

1.  **外部流量入口**：用户的请求首先通过公网 DNS 解析到 Ingress Controller 的 `Service` 的外部 IP 地址 (External IP)。这个 `Service` 通常是 `LoadBalancer` 或 `NodePort` 类型。

      * **LoadBalancer**: 在云厂商环境中，这会自动创建一个外部负载均衡器（如 AWS ELB, Google Cloud Load Balancer），将流量引导到集群中运行 Ingress Controller Pod 的节点上。
      * **NodePort**: 流量直接发送到集群中**任意一个节点**的指定端口 (`NodePort`)。

2.  **进入 Ingress Controller Pod**：

      * 流量到达节点后，`kube-proxy`（工作在 iptables, IPVS 或 eBPF 模式下）会根据 `Service` 的规则，将流量从 `NodePort` 或 `LoadBalancer` 的后端节点端口，转发给 Ingress Controller 的 Pod。
      * 这个转发是基于 `Service` 的 ClusterIP 和 Endpoint (Pod IP) 实现的负载均衡。

3.  **Ingress Controller 内部处理**：

      * Ingress Controller Pod 内运行着我们的 Go 程序和一个反向代理。Go 程序是**控制平面 (Control Plane)**，反向代理是**数据平面 (Data Plane)**。
      * 请求进入反向代理（例如 NGINX）。
      * 反向代理根据 Go 程序动态生成的配置，进行七层路由决策。它会检查请求的 `Host` header 和 `Path`。

4.  **路由到后端 Service/Pod**：

      * 根据 Ingress 规则 (e.g., `host: foo.bar.com`, `path: /app`)，反向代理找到了匹配的后端 `Service`。
      * 代理配置中通常**不会**直接使用 `Service` 的 ClusterIP。因为 ClusterIP 是一个虚拟 IP，通过它进行转发会再次经过 `kube-proxy`，增加网络跳数并可能导致 SNAT 地址耗尽。
      * **最佳实践是**：Ingress Controller 直接监听 `Service` 关联的 `Endpoint` 或 `EndpointSlice` 对象，获取后端 Pod 的实际 IP 地址列表 (`PodIP:Port`)。然后，将这些 `PodIP:Port` 直接配置为反向代理的上游服务器 (upstream servers)。
      * 这样，反向代理可以直接将流量负载均衡到目标 Pod，实现了最高效的路径。

5.  **到达目标 Pod**：流量最终从 Ingress Controller Pod 直接发送到运行业务应用的 Pod。

**总结**：`External Client -> Cloud Load Balancer / NodePort -> Kube-proxy -> Ingress Controller Pod (Reverse Proxy) -> Target Pod`

-----

### 开发方式示例

开发自定义 Ingress Controller 主要有两种主流方式：

#### 方式一：基于现有反向代理的包装 (Wrapper/Sidecar 模式)

这是最常见、最稳定、生态最成熟的方式。我们自己开发的 Go 程序只负责**控制平面**，监听 K8s 资源并生成配置文件，然后通过命令（如 `nginx -s reload`）动态加载到代理中。数据平面的所有复杂工作（TLS 卸载、负载均衡算法、HTTP/2 处理等）都交给成熟的代理软件。

  * **优点**：
      * 稳定可靠：NGINX, Envoy 等都是经过大规模生产验证的。
      * 功能强大：可以利用代理的全部高级功能。
      * 开发效率高：只需关注配置生成和生命周期管理。
  * **缺点**：
      * 灵活性受限：功能受限于底层代理的能力。
      * 引入额外依赖：需要管理代理的二进制文件和配置。

**代表项目**：[ingress-nginx](https://github.com/kubernetes/ingress-nginx), [Emissary-Ingress (Envoy-based)](https://www.getambassador.io/)

#### 方式二：纯 Go 实现 (Native Go Implementation)

直接在 Go 代码中实现反向代理逻辑，不依赖外部的 NGINX 或 Envoy。Go 程序同时承担**控制平面**和**数据平面**的职责。

  * **优点**：
      * 极致灵活性：可以实现任何自定义的路由逻辑、负载均衡算法或私有协议。
      * 无外部依赖：单个二进制文件，部署简单。
      * 云原生亲和度高：易于与 OpenTelemetry、Prometheus 等原生集成。
  * **缺点**：
      * 开发复杂：需要自行处理 HTTP 连接、TLS 卸载、负载均衡、健康检查等所有细节。
      * 性能和稳定性挑战：要达到 NGINX/Envoy 的性能和稳定性水平，需要大量的优化和测试工作。

**代表项目**：[Traefik](https://github.com/traefik/traefik), [APISIX Ingress Controller](https://github.com/apache/apisix-ingress-controller) (APISIX 自身是数据平面，但 Controller 的集成方式更紧密)

-----

### 完整核心代码示例 (基于方式一：包装 NGINX)

我们将实现一个生产级的、简化的 Ingress Controller。它会监听 Ingress 资源，生成 NGINX 配置文件，并平滑地重新加载 NGINX。

#### 项目结构

```
custom-ingress-controller/
├── go.mod
├── go.sum
├── main.go
├── controller/
│   └── controller.go
├── nginx/
│   ├── nginx.conf.template
│   └── nginx.go
└── deploy/
    ├── rbac.yaml
    └── deployment.yaml
```

#### 1\. `main.go` - 程序入口

这是程序的启动入口，负责初始化 Kubernetes 客户端、创建 Controller 实例并启动。

```go
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"custom-ingress-controller/controller"
	"custom-ingress-controller/nginx"
)

func main() {
	// 1. 设置 Kubernetes 客户端连接
	// 通常在集群内部署时，会使用 InClusterConfig
	// 在本地开发时，会使用 kubeconfig 文件
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		log.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating clientset: %s", err.Error())
	}

	// 2. 创建 NGINX 管理器
	// 假设 nginx.conf 模板和最终生成的配置文件都在 /etc/nginx/
	nginxManager := nginx.NewManager("/etc/nginx/nginx.conf.template", "/etc/nginx/nginx.conf")

	// 3. 创建 Informer 工厂，用于高效地监听资源变化
	// a. `NewSharedInformerFactory` is the most common way to create informers.
	// b. We are interested in all namespaces, so we pass an empty string.
	// c. We don't need to resync very often. 0 means default resync period.
	informerFactory := informers.NewSharedInformerFactory(clientset, 0)

	// 4. 初始化 Controller
	// a. `clientset` for interacting with the K8s API.
	// b. `informerFactory` for getting informers for Ingresses, Services, and Endpoints.
	// c. `nginxManager` for updating and reloading NGINX.
	c := controller.NewController(clientset, informerFactory, nginxManager)

	// 5. 启动 Informer
	// This will start all the informers that have been created by the factory.
	// `stopCh` is used to signal the informers to stop.
	stopCh := make(chan struct{})
	defer close(stopCh)
	informerFactory.Start(stopCh)

	// 6. 运行 Controller
	// This starts the control loop.
	if err = c.Run(1, stopCh); err != nil {
		log.Fatalf("Error running controller: %s", err.Error())
	}

	// 7. 优雅停机处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("Shutting down gracefully...")
}
```

#### 2\. `controller/controller.go` - 核心控制逻辑

这是 Controller 的心脏，负责处理事件、比较状态并触发 NGINX 更新。

```go
package controller

import (
	"fmt"
	"log"
	"time"

	"k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listers "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"custom-ingress-controller/nginx"
)

// Controller holds all the necessary components for the control loop.
type Controller struct {
	clientset    kubernetes.Interface
	ingressLister listers.IngressLister // Local cache for Ingresses
	ingressSynced cache.InformerSynced  // Function to check if the Ingress cache is synced
	workqueue    workqueue.RateLimitingInterface // Queue for processing Ingress changes
	nginxManager *nginx.Manager
}

// NewController creates a new instance of our Ingress controller.
func NewController(clientset kubernetes.Interface, informerFactory informers.SharedInformerFactory, nginxManager *nginx.Manager) *Controller {
	// Get the informer for Ingress resources
	ingressInformer := informerFactory.Networking().V1().Ingresses()

	c := &Controller{
		clientset:    clientset,
		ingressLister: ingressInformer.Lister(),
		ingressSynced: ingressInformer.Informer().HasSynced,
		workqueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Ingresses"),
		nginxManager: nginxManager,
	}

	log.Println("Setting up event handlers")
	// Set up an event handler for when Ingress resources change
	ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.enqueueIngress,
		UpdateFunc: func(old, new interface{}) {
			// We only care about changes to the Ingress spec.
			// Metadata changes (like annotations added by other controllers) are ignored.
			oldIngress := old.(*v1.Ingress)
			newIngress := new.(*v1.Ingress)
			if oldIngress.ResourceVersion == newIngress.ResourceVersion {
				return
			}
			c.enqueueIngress(new)
		},
		DeleteFunc: c.enqueueIngress,
	})

	// TODO: Add event handlers for Services and Endpoints.
	// When a Service or its Endpoints change, we need to find all Ingresses
	// that point to it and requeue them for processing. This is crucial
	// for updating the upstream servers in NGINX when Pods are added or removed.

	return c
}

// Run starts the controller's control loop.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	log.Println("Starting Custom Ingress controller")

	// Wait for the caches to be synced before starting workers
	log.Println("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.ingressSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	log.Println("Starting workers")
	// Launch workers to process Ingress resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	log.Println("Started workers")
	<-stopCh
	log.Println("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Ingress resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		log.Printf("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Ingress resource
// with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get all Ingress resources from the lister. This is the desired state.
	// In a real-world scenario, you might want to filter by an "ingress.class" annotation
	// to ensure this controller only processes Ingresses intended for it.
	allIngresses, err := c.ingressLister.List(labels.Everything())
	if err != nil {
		return err
	}
	
	// For simplicity, we'll re-generate the config from all Ingresses on every change.
	// A more optimized approach would be to calculate the diff.
	log.Println("A change was detected, reconciling all Ingress resources...")

	// Here you would fetch all relevant Services and Endpoints as well to build the full config.
	// For this example, we'll pass the full list of ingresses to the NGINX manager.
	// In a production implementation, you need to build a model of your desired NGINX config.
	// This model would include server blocks, locations, and upstreams derived from
	// Ingress, Service, and Endpoint resources.

	// For now, we just trigger a config generation and reload.
	return c.nginxManager.UpdateAndReload(allIngresses)
}


// enqueueIngress takes an Ingress resource and converts it into a namespace/name
// string which is then put onto the workqueue.
func (c *Controller) enqueueIngress(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	// We add the key to the workqueue.
	// Any change (add, update, delete) to an Ingress will trigger a full reconciliation.
	c.workqueue.Add(key)
}
```

#### 3\. `nginx/nginx.go` & `nginx/nginx.conf.template` - NGINX 管理

这部分代码负责将 Kubernetes 对象（Ingress 列表）转换为 NGINX 的配置文件，并执行 `nginx -s reload`。

**`nginx/nginx.conf.template`**
使用 Go 的 `text/template` 库来动态生成配置。

```nginx
# /etc/nginx/nginx.conf.template

worker_processes auto;
pid /var/run/nginx.pid;

events {
    worker_connections 1024;
}

http {
    include       /etc/nginx/mime.types;
    default_type  application/octet-stream;

    log_format  main  '$remote_addr - $remote_user [$time_local] "$request" '
                      '$status $body_bytes_sent "$http_referer" '
                      '"$http_user_agent" "$http_x_forwarded_for"';

    access_log  /var/log/nginx/access.log  main;
    sendfile        on;
    keepalive_timeout  65;

    # Dynamic part generated by the controller
    {{ range . }}
    server {
        listen 80;
        server_name {{ .Spec.Rules.Host }};

        {{ range .Spec.Rules.HTTP.Paths }}
        location {{ .Path }} {
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            
            # This is a simplified example. In production, you would look up
            # the Service's Endpoints (Pod IPs) and create an upstream block.
            # Then you would proxy_pass to the upstream name.
            # For simplicity here, we use the Service's ClusterIP via Kube DNS.
            # WARNING: This is NOT recommended for production due to extra hops and potential SNAT issues.
            proxy_pass http://{{ .Backend.Service.Name }}.{{ $.Namespace }}.svc.cluster.local:{{ .Backend.Service.Port.Number }};
        }
        {{ end }}
    }
    {{ end }}
}
```

**注意**：模板中的 `proxy_pass` 为了简化，直接使用了 Service 的 DNS 名称。**在生产环境中，这绝对不是最佳实践**。你应该监听 `EndpointSlices`，获取 Pod IP，然后动态创建 `upstream` 块，并将 `proxy_pass` 指向该 `upstream`。

**`nginx/nginx.go`**

```go
package nginx

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"text/template"

	"k8s.io/api/networking/v1"
)

// Manager handles the generation of NGINX configuration and reloading NGINX.
type Manager struct {
	templatePath string
	outputPath   string
	template     *template.Template
}

// NewManager creates a new NGINX Manager.
func NewManager(templatePath, outputPath string) *Manager {
	tmpl, err := template.ParseFiles(templatePath)
	if err != nil {
		log.Fatalf("Failed to parse NGINX template file: %v", err)
	}
	return &Manager{
		templatePath: templatePath,
		outputPath:   outputPath,
		template:     tmpl,
	}
}

// UpdateAndReload generates a new nginx.conf from the given Ingresses and reloads NGINX.
// In a real implementation, this function would take a structured configuration object,
// not just the raw Ingress list.
func (m *Manager) UpdateAndReload(ingresses []*v1.Ingress) error {
	log.Println("Generating new NGINX configuration...")

	var buffer bytes.Buffer
	// Execute the template with the list of Ingress resources.
	// Note: For production, you'd build a more sophisticated data structure
	// that includes information from Services and Endpoints.
	if err := m.template.Execute(&buffer, ingresses); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	// Write the new configuration to the output file.
	if err := os.WriteFile(m.outputPath, buffer.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write NGINX config file: %w", err)
	}

	log.Println("NGINX configuration updated. Reloading NGINX...")
	return m.reload()
}

// reload executes `nginx -s reload` to apply the new configuration.
func (m *Manager) reload() error {
	cmd := exec.Command("nginx", "-s", "reload")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed to reload NGINX: %s", string(output))
		return fmt.Errorf("reloading NGINX failed: %w", err)
	}
	log.Println("NGINX reloaded successfully.")
	return nil
}
```

#### 4\. `deploy/` - 部署清单

为了让 Controller 在集群中运行，你需要：

  * **RBAC (`rbac.yaml`)**: 创建 `ClusterRole` 和 `ClusterRoleBinding`，授予 Controller 的 `ServiceAccount` 读取 `Ingresses`, `Services`, `Endpoints`, `Secrets` (for TLS) 等资源的权限。
  * **Deployment (`deployment.yaml`)**: 定义一个 `Deployment` 来运行你的 Controller。这个 Pod 通常包含两个容器：
    1.  `custom-ingress-controller`: 运行我们编译的 Go 程序。
    2.  `nginx`: 运行官方的 NGINX 镜像。
        两者共享一个 `emptyDir` volume，用于写入和读取 `nginx.conf`。

-----

### 如何在生产中使用和扩展

1.  **Ingress Class**：在 `syncHandler` 中，你应该检查 Ingress 对象的 `spec.ingressClassName` 字段或 `kubernetes.io/ingress.class` 注解，只处理明确指定给你的 Controller 的 Ingress。
2.  **EndpointSlices**：监听 `EndpointSlices` 而不是 `Endpoints`，这是更现代、扩展性更好的 API。当 `EndpointSlices` 变化时，找到所有引用了对应 `Service` 的 `Ingress`，并将它们加入 `workqueue` 进行再同步。
3.  **TLS 支持**：监听 `Secret` 资源。当一个 `Ingress` 的 `spec.tls` 部分引用了一个 Secret，你需要获取这个 Secret，将其中的 `tls.crt` 和 `tls.key` 写入文件，并在 NGINX 的 `server` 块中配置 `ssl_certificate` 和 `ssl_certificate_key`。
4.  **状态更新**：在 `syncHandler` 成功处理一个 `Ingress` 后，你应该更新该 `Ingress` 的 `status.loadBalancer.ingress` 字段，填入 Ingress Controller `Service` 的外部 IP 或主机名。这能让用户通过 `kubectl get ingress` 看到入口地址。
5.  **指标和可观察性**：使用 Prometheus 客户端库暴露内部指标，例如：reconciliation 的次数和延迟、NGINX reload 的成功/失败次数等。
6.  **代码库依赖**：强烈建议使用 `client-go`, `apimachinery`, `api` 等官方库，它们提供了与 Kubernetes API 交互所需的所有工具，包括 Informers, Listers, Workqueues 等。

这个框架为你提供了一个坚实的基础，你可以基于此扩展功能，打造一个完全符合自己业务需求的、生产级的 Ingress Controller。

ingress本质是上监听k8s的资源变动，生成代理配置，热更新代理服务，让网络请求能被正确路由到后端服务（pod服务）。


## ingress nginx 和 istio区别

`Ingress NGINX` 和 `Istio Ingress Gateway`（通常我们称其为 `Istio Gateway`）的**核心定位、架构设计、功能范畴和配置方式**有着本质的区别。

简单来说，**Ingress NGINX 是一个功能强大的 API 网关，而 Istio Gateway 是整个服务网格（Service Mesh）的流量入口**。前者专注于“进入”集群的流量管理，而后者是为管理集群“内部”和“进出”流量的复杂策略而设计的。

下面我们从多个维度进行详细对比。

-----

### 1\. 核心定位 (Core Positioning)

  * **Ingress NGINX**:

      * **定位**: 一个纯粹的 **Ingress Controller**，其主要职责是实现了 Kubernetes 标准的 `Ingress` 资源规范。
      * **目标**: 提供一种将 HTTP 和 HTTPS 流量从外部路由到集群内部 `Service` 的可靠方法。它本质上是一个增强版的反向代理和负载均衡器，专注于七层（L7）路由。
      * **范畴**: 主要管理**南北向流量**（从集群外部到内部）。

  * **Istio Ingress Gateway**:

      * **定位**: **服务网格 (Service Mesh) 的流量入口**，是 Istio 架构中的一个核心组件。
      * **目标**: 它的目标远不止于简单的路由。它作为网格的“门户”，统一了流量管理的策略。所有进入网格的流量都要经过它的审查和策略执行，以便与网格内部的流量管理（东西向流量）无缝集成。
      * **范畴**: 既是**南北向流量**的入口，也是实现复杂**东西向流量**管理策略（如熔断、故障注入）的起点。

-----

### 2\. 架构差异 (Architectural Differences)

  * **Ingress NGINX**:

      * **架构**: 相对简单，通常是一个 `Deployment` 在集群中运行，包含一个 NGINX 实例（**数据平面**）和一个 Controller（**控制平面**）。
      * **工作模式**: Controller 监听 K8s API Server 的 `Ingress`, `Service`, `Endpoint`, `Secret` 等资源的变化，然后动态生成 NGINX 配置文件 (`nginx.conf`)，最后通过 `reload` 命令使配置生效。数据平面和控制平面紧密耦合在同一个 Pod 中。

  * **Istio Ingress Gateway**:

      * **架构**: 完全遵循 Istio 的**控制平面/数据平面分离**模型。
      * **数据平面**: Gateway 本身是一个基于 **Envoy** 代理的 `Deployment`。它只负责接收和转发流量，本身没有决策逻辑。
      * **控制平面**: 独立的 `istiod` 组件是控制平面。`istiod` 监听 Istio 的自定义资源（CRD），如 `Gateway`, `VirtualService`, `DestinationRule` 等，将这些高级规则编译成 Envoy 能理解的低级配置，并通过 xDS API 动态下发给 Ingress Gateway 的 Envoy 代理。
      * **优势**: 这种分离使得架构更清晰，扩展性更强，并且可以实现更高级的动态配置能力，无需 reload。

-----

### 3\. 配置方式 (Configuration Model)

这是两者最直观的区别。

  * **Ingress NGINX**:

      * **主要配置**: 使用 Kubernetes 的标准 `Ingress` 资源对象。这是一个相对简单的 API，主要定义基于 Host 和 Path 的路由规则。
      * **高级功能**: 对于 `Ingress` 规范中没有定义的高级功能（如重写、CORS、限流、身份验证等），需要通过在 `Ingress` 对象中添加大量的 `metadata.annotations`（注解）来实现。
      * **缺点**: 注解的方式是非标准的，可读性差，容易出错，且难以在多个 Ingress 之间复用和管理。

    **示例 (Ingress NGINX)**:

    ```yaml
    apiVersion: networking.k8s.io/v1
    kind: Ingress
    metadata:
      name: my-app-ingress
      annotations:
        nginx.ingress.kubernetes.io/rewrite-target: /
        nginx.ingress.kubernetes.io/cors-enable: "true"
    spec:
      rules:
      - host: "example.com"
        http:
          paths:
          - path: /app
            pathType: Prefix
            backend:
              service:
                name: my-app-service
                port:
                  number: 80
    ```

  * **Istio Ingress Gateway**:

      * **主要配置**: 使用 Istio 自己的 CRD，实现了**关注点分离 (Separation of Concerns)**。
          * `Gateway`: **定义入口点**。负责配置四层到六层（L4-L6）的属性，如端口、协议（HTTP, HTTPS, GRPC, TCP）、TLS 证书等。它只关心“流量从哪里进来”，不关心“进来后去哪里”。
          * `VirtualService`: **定义路由规则**。将 `Gateway` 和具体的路由逻辑绑定。它定义了七层（L7）的路由规则，如 Host/Path 匹配、Header 匹配、流量切分（灰度发布）、重定向、重试、超时等。
      * **优势**: 这种模型非常强大和灵活。网络基础设施团队可以负责管理 `Gateway` 资源，而应用开发团队可以独立管理 `VirtualService` 来控制自己应用的的路由逻辑。

    **示例 (Istio)**:

    ```yaml
    # Step 1: Define the entry point (L4-L6)
    apiVersion: networking.istio.io/v1beta1
    kind: Gateway
    metadata:
      name: my-app-gateway
    spec:
      selector:
        istio: ingressgateway # Use the default Istio Ingress Gateway
      servers:
      - port:
          number: 80
          name: http
          protocol: HTTP
        hosts:
        - "example.com"

    ---
    # Step 2: Define the routing rules (L7)
    apiVersion: networking.istio.io/v1beta1
    kind: VirtualService
    metadata:
      name: my-app-virtualservice
    spec:
      hosts:
      - "example.com"
      gateways:
      - my-app-gateway # Bind to the Gateway created above
      http:
      - match:
        - uri:
            prefix: /app
        route:
        - destination:
            host: my-app-service.default.svc.cluster.local
            port:
              number: 80
    ```

-----

### 4\. 功能广度与深度 (Feature Breadth and Depth)

| 功能点 | Ingress NGINX | Istio Ingress Gateway | 说明 |
| :--- | :--- | :--- | :--- |
| **流量路由** | Host/Path 基础路由、URL 重写 | **极其强大**：Header/QueryParam/Method 匹配、流量切分（百分比）、镜像、超时、重试 | Istio 的路由能力远超 Ingress NGINX，是其核心优势。 |
| **灰度发布** | 困难，通常需要配合其他工具（如 Flagger/Argo Rollouts）通过操作 Ingress 权重实现 | **原生支持**，通过 `VirtualService` 的 `weight` 字段可以轻松实现蓝绿部署、金丝雀发布。 | Istio 在渐进式交付方面是天生的赢家。 |
| **安全性** | TLS 终止、客户端证书认证、JWT 验证 (通过注解) | **全面**：mTLS、强大的授权策略 (`AuthorizationPolicy`)、JWT 验证、WAF 集成等 | Istio Gateway 是整个网格安全策略的一部分，可以对出入流量执行非常精细的访问控制。 |
| **可观察性** | 提供基本的 Prometheus 指标 (请求数、延迟等) 和访问日志。 | **非常丰富**：开箱即用的分布式追踪 (Tracing)、遥测 (Metrics) 和日志 (Logging)，与 Kiali 控制台深度集成。 | Istio 自动为所有流量注入 sidecar/gateway，提供了无与伦比的洞察力。 |
| **协议支持** | HTTP, HTTPS, HTTP/2, gRPC (透传) | HTTP, HTTPS, HTTP/2, gRPC, **TCP** | Istio Gateway 对 TCP 流量的管理能力也更强。 |
| **故障注入** | 不支持 | **原生支持**，可以在 `VirtualService` 中注入延迟 (Delay) 或中断 (Abort) 来进行混沌工程测试。 | 这是服务网格独有的高级功能。 |

-----

### 5\. 使用场景 (Use Cases)

  * **选择 Ingress NGINX 的场景**:

      * 你的需求相对简单，只需要标准的 HTTP/HTTPS 路由。
      * 你不需要服务网格，或者已经有其他的服务网格方案。
      * 你希望有一个轻量级、易于理解和快速上手的解决方案。
      * 团队对复杂的 CRD 和服务网格概念不熟悉。

  * **选择 Istio Ingress Gateway 的场景**:

      * 你已经在使用或计划使用 Istio 服务网格，希望统一南北向和东西向的流量管理。
      * 你需要复杂的流量路由和发布策略，如精细的灰度发布、流量镜像等。
      * 安全是你的首要考虑，需要零信任网络、mTLS 和精细的授权策略。
      * 你需要深入的可观察性，包括分布式追踪和开箱即用的遥测。
      * 你的应用是微服务架构，并且需要对服务间的调用进行复杂的治理（熔断、重试、限流等）。

### 总结表格

| 特性 | Ingress NGINX | Istio Ingress Gateway |
| :--- | :--- | :--- |
| **核心定位** | API 网关 / Ingress Controller | 服务网格入口 |
| **数据平面** | NGINX | Envoy |
| **控制平面** | Controller (in-pod) | istiod (独立) |
| **配置模型** | `Ingress` + `Annotations` | `Gateway` + `VirtualService` CRDs |
| **主要职责** | L7 路由 | L4-L7 流量治理、安全、可观察性 |
| **流量管理** | 基础 | 高级（流量切分、镜像、故障注入） |
| **安全性** | 基础 (TLS) | 全面 (mTLS, AuthorizationPolicy) |
| **可观察性** | 基础指标/日志 | 丰富（Metrics, Tracing, Logging） |
| **生态系统** | Kubernetes 生态 | Istio 服务网格生态 |
| **复杂度** | 较低 | 较高 |

**结论**: Ingress NGINX 是一个优秀的“组件”，而 Istio Gateway 是一个庞大“体系”的入口。选择哪个，完全取决于你的业务需求、技术栈以及团队对云原生复杂性的接受程度。

## 其他常用ingress

Kubernetes 生态中有许多其他非常优秀且常用的 Ingress 解决方案。它们各自有不同的设计哲学、功能侧重和适用场景。

以下是一些在生产环境中广泛使用的 Ingress Controller：

### 1. Traefik Proxy

Traefik 是一个非常受欢迎的现代云原生边缘路由器（Edge Router）。它从一开始就是为动态环境（如 Kubernetes、Docker Swarm）设计的，自动化和易用性是其核心亮点。

* **核心特点**:
    * **自动化服务发现**: Traefik 的最大卖点是能够自动发现 Kubernetes `Ingress` 或其自有 CRD (`IngressRoute`)，并动态更新路由配置，无需手动重启或重新加载。
    * **简洁的 CRD**: 提供了比标准 Ingress 更强大且更易于理解的 `IngressRoute` CRD，用于配置路由、中间件和 TLS。
    * **内置 ACME (Let's Encrypt)**: 开箱即用地支持通过 Let's Encrypt 自动生成和续订 TLS 证书，极大简化了 HTTPS 的配置。
    * **中间件 (Middleware)**: 拥有丰富的中间件生态，可以轻松地通过 CRD 应用各种功能，如身份验证、速率限制、请求头修改、熔断等。
    * **美观的 Dashboard**: 内置一个非常直观的 Web UI，可以实时查看路由、服务、中间件的状态。

* **适用场景**:
    * 追求极致自动化和简便配置的团队。
    * 需要快速为大量服务启用 HTTPS 的场景。
    * 喜欢通过声明式、功能丰富的 CRD 来管理路由的开发者。
    * 中小型项目或快速迭代的微服务环境。

### 2. Contour

Contour 是一个由 VMware 开源并捐赠给 CNCF 的 Ingress Controller，它使用 **Envoy** 作为其数据平面。它的定位是提供一个健壮、高性能且策略驱动的 Ingress 解决方案。

* **核心特点**:
    * **基于 Envoy**: 享受 Envoy 带来的高性能、高可扩展性和丰富功能（如 HTTP/2、gRPC、高级负载均衡策略等）。
    * **安全多租户**: 设计上强调安全和多租户能力。管理员可以安全地将路由配置的权限委托给不同的团队，而不会相互影响。
    * **HTTPProxy CRD**: 引入了 `HTTPProxy` CRD，作为对标准 Ingress 资源的一个更安全、功能更强大的替代品。它允许路由配置的“包含”和“委托”，非常适合多团队协作。
    * **与 Gateway API 对齐**: Contour 社区正积极地推动和实现 Kubernetes **Gateway API**，这是下一代 Ingress 规范，提供了更强的表现力和角色分离。

* **适用场景**:
    * 需要利用 Envoy 强大功能，但又不想引入像 Istio 这样完整的服务网格的企业。
    * 多团队共享一个集群，需要安全地隔离和委托路由配置权限的场景。
    * 追求 Kubernetes 最新标准，希望尽早采用 Gateway API 的用户。
    * 对性能和可扩展性有较高要求的环境。

### 3. Kong Ingress Controller

Kong 本身是一个非常知名的开源 API 网关。它的 Ingress Controller 将强大的 Kong Gateway 与 Kubernetes 无缝集成，专注于 API 管理。

* **核心特点**:
    * **强大的 API 网关功能**: 不仅仅是 Ingress，它提供了完整的 API 管理能力，如身份验证（OAuth2, JWT, API Keys）、速率限制、转换、日志记录等，这些都可以通过其插件系统实现。
    * **插件生态系统**: 拥有一个庞大且成熟的插件市场（包括开源和商业插件），可以轻松扩展网关的功能。
    * **CRD 驱动配置**: 使用一系列 CRD（如 `KongIngress`, `KongPlugin`, `KongConsumer`）来声明式地配置 Kong 的所有功能。
    * **混合模式 (Hybrid Mode)**: 支持控制平面和数据平面分离部署，数据平面可以部署在任何地方，适合大规模、多云或混合云环境。

* **适用场景**:
    * 你的核心需求是 **API 管理**，而不仅仅是简单的流量路由。
    * 需要复杂的认证、授权和流量控制策略。
    * 希望利用成熟的插件生态来快速实现各种 API 管理功能。
    * 已经在使用 Kong Gateway 并希望将其能力扩展到 Kubernetes 的企业。

### 4. APISIX Ingress Controller

APISIX 是一个源自 Apache 基金会的云原生 API 网关，以其极高的性能和动态能力而闻名。其 Ingress Controller 同样功能强大。

* **核心特点**:
    * **极致性能**: 底层基于 NGINX 和 LuaJIT，并使用 etcd 进行配置存储，实现了配置的热加载，避免了传统 NGINX 的 reload 延迟。
    * **丰富的插件和动态性**: 同样拥有强大的插件生态，支持热插拔和热更新。可以在不重启服务的情况下动态更改插件规则。
    * **多协议支持**: 除了 HTTP/HTTPS、TCP/UDP，还支持 Dubbo、MQTT 等多种协议代理。
    * **CRD 和注解兼容**: 同时支持通过自定义 CRD (`ApisixRoute`, `ApisixUpstream`) 和兼容 Ingress NGINX 的注解来进行配置，迁移成本较低。

* **适用场景**:
    * 对性能要求极高的场景，如高并发的互联网业务。
    * 需要动态、灵活配置，希望避免 `nginx reload` 带来的瞬间延迟。
    * 技术栈多样，需要代理 gRPC、Dubbo、MQTT 等多种协议。
    * 希望从 Ingress NGINX 迁移到功能更丰富的 API 网关。

### 5. HAProxy Ingress

HAProxy 是一个非常老牌、稳定且性能极高的开源负载均衡器。HAProxy Ingress Controller 将其强大的能力带入了 Kubernetes。

* **核心特点**:
    * **稳定可靠**: HAProxy 经过了数十年的大规模生产环境验证，以其稳定性和极低的资源消耗而著称。
    * **高性能**: 在 TCP (L4) 和 HTTP (L7) 负载均衡方面都表现出色。
    * **丰富的功能**: 支持精细的超时控制、重试策略、高级健康检查和丰富的负载均衡算法。
    * **配置灵活**: 可以通过 Ingress 注解、ConfigMap 或自定义的 `Backend` CRD 来进行配置。

* **适用场景**:
    * 追求极致稳定性和可靠性的生产环境。
    * 对资源消耗敏感的场景。
    * 团队对 HAProxy 有深入的了解和运维经验。
    * 需要处理大量 TCP 长连接的业务。

### 总结对比

| Ingress Controller | 核心数据平面 | 亮点优势 | 最佳使用场景 |
| :--- | :--- | :--- | :--- |
| **Ingress NGINX** | NGINX | 功能均衡、社区庞大、事实标准 | 通用 Web 流量路由 |
| **Istio Gateway** | Envoy | 服务网格集成、高级流量治理、安全 | 已采用 Istio 的微服务架构 |
| **Traefik** | 自研 (Go) | 自动化、易用、内置 Let's Encrypt | 开发者友好、快速迭代、自动化 HTTPS |
| **Contour** | Envoy | 安全多租户、Gateway API 对齐 | 多团队协作、拥抱新标准、利用 Envoy |
| **Kong** | NGINX + Lua | 强大的 API 管理、插件生态 | API 管理优先、复杂认证授权 |
| **APISIX** | NGINX + Lua | 极致性能、动态热加载、多协议 | 高性能要求、需要动态配置和扩展 |
| **HAProxy Ingress** | HAProxy | 极致稳定、高性能、资源消耗低 | 追求稳定可靠、处理 L4/L7 混合流量 |

选择哪一个 Ingress Controller，最终取决于你的具体需求：是需要简单的路由、全面的 API 管理、服务网格的集成，还是极致的性能和自动化。