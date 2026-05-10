---
title: "向量数据库索引算法如何选择与实现"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["向量数据库", "索引", "golang"]
---

## 问题

在AI应用中，向量检索是核心能力。HNSW、IVF、PQ等向量索引算法各自的原理和适用场景是什么？如何根据数据规模、查询延迟、召回率要求选择合适的索引？如何用Go实现一个HNSW索引？

## 回答

向量数据库是AI应用的基础设施，索引算法直接决定了检索的性能和精度。不同的索引算法在构建速度、查询延迟、内存占用和召回率之间有不同的权衡。

### 一、向量检索的核心挑战

给定一个查询向量q，从N个向量中找到与q最相似的K个向量（KNN问题）。

**暴力搜索**：计算q与所有N个向量的距离，排序取TopK。时间复杂度O(N)，数据量大时不可接受。

**索引的目标**：在保证召回率的前提下，将搜索范围从N缩小到一个很小的子集。

### 二、主流索引算法

#### 2.1 HNSW（Hierarchical Navigable Small World）

当前最主流的向量索引算法，基于图结构。

**核心思想**：构建一个多层图，上层是稀疏的"高速公路"（长距离连接），下层是密集的局部连接。搜索时从顶层开始，逐层向下逼近目标。

```
Layer 2:    A ──────────────── Z
            |                  |
Layer 1:    A ──── D ──── M ── Z
            |   / | \    |  / |
Layer 0:    A-B-C-D-E-F-G-M-N-Z
```

**搜索过程**：
1. 从Layer 2的入口点开始，找到最近的节点
2. 跳到Layer 1，从该节点附近继续搜索
3. 跳到Layer 0，做精细搜索，返回TopK

**Go实现**：

```go
package vectorindex

import (
	"container/heap"
	"math"
	"math/rand"
	"sync"
)

type Vector = []float32

func cosineDistance(a, b Vector) float32 {
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 1.0
	}
	return 1.0 - dot/(float32(math.Sqrt(float64(normA)))*float32(math.Sqrt(float64(normB))))
}

type HNSWNode struct {
	ID     int
	Vector Vector
	Layers [][]int
}

type HNSW struct {
	mu         sync.RWMutex
	nodes      map[int]*HNSWNode
	M          int
	MMax0      int
	efConstruction int
	maxLevel   int
	entryPoint int
	levelMult  float64
	rng        *rand.Rand
}

func NewHNSW(M, efConstruction int) *HNSW {
	return &HNSW{
		nodes:      make(map[int]*HNSWNode),
		M:          M,
		MMax0:      M * 2,
		efConstruction: efConstruction,
		levelMult:  1.0 / math.Log(float64(M)),
		rng:        rand.New(rand.NewSource(42)),
	}
}

func (h *HNSW) randomLevel() int {
	level := 0
	for h.rng.Float64() < math.Exp(-float64(level)/h.levelMult) && level < 16 {
		level++
	}
	return level
}

func (h *HNSW) Insert(id int, vec Vector) {
	h.mu.Lock()
	defer h.mu.Unlock()

	level := h.randomLevel()
	node := &HNSWNode{
		ID:     id,
		Vector: vec,
		Layers: make([][]int, level+1),
	}
	h.nodes[id] = node

	if len(h.nodes) == 1 {
		h.entryPoint = id
		h.maxLevel = level
		return
	}

	ep := h.entryPoint

	for l := h.maxLevel; l > level; l-- {
		ep = h.searchLayer(vec, ep, 1, l)[0].ID
	}

	for l := min(level, h.maxLevel); l >= 0; l-- {
		candidates := h.searchLayer(vec, ep, h.efConstruction, l)

		M := h.M
		if l == 0 {
			M = h.MMax0
		}

		neighbors := h.selectNeighbors(vec, candidates, M)

		node.Layers[l] = make([]int, 0, len(neighbors))
		for _, n := range neighbors {
			node.Layers[l] = append(node.Layers[l], n.ID)

			neighborNode := h.nodes[n.ID]
			neighborNode.Layers[l] = append(neighborNode.Layers[l], id)

			if len(neighborNode.Layers[l]) > M {
				neighborVec := neighborNode.Vector
				pruned := h.selectNeighbors(neighborVec, h.toCandidates(neighborNode.Layers[l]), M)
				neighborNode.Layers[l] = make([]int, 0, len(pruned))
				for _, p := range pruned {
					neighborNode.Layers[l] = append(neighborNode.Layers[l], p.ID)
				}
			}
		}

		if len(neighbors) > 0 {
			ep = neighbors[0].ID
		}
	}

	if level > h.maxLevel {
		h.maxLevel = level
		h.entryPoint = id
	}
}

type candidate struct {
	ID       int
	Distance float32
}

func (h *HNSW) searchLayer(query Vector, entryPoint int, ef int, layer int) []candidate {
	visited := make(map[int]bool)
	epNode := h.nodes[entryPoint]
	epDist := cosineDistance(query, epNode.Vector)

	candidates := &minHeap{}
	heap.Init(candidates)
	heap.Push(candidates, &candidate{ID: entryPoint, Distance: epDist})

	results := &maxHeap{}
	heap.Init(results)
	heap.Push(results, &candidate{ID: entryPoint, Distance: epDist})

	visited[entryPoint] = true

	for candidates.Len() > 0 {
		c := heap.Pop(candidates).(*candidate)
		furthest := (*results)[0]

		if c.Distance > furthest.Distance {
			break
		}

		node := h.nodes[c.ID]
		if layer >= len(node.Layers) {
			continue
		}

		for _, neighborID := range node.Layers[layer] {
			if visited[neighborID] {
				continue
			}
			visited[neighborID] = true

			neighborNode := h.nodes[neighborID]
			dist := cosineDistance(query, neighborNode.Vector)

			if results.Len() < ef || dist < (*results)[0].Distance {
				heap.Push(candidates, &candidate{ID: neighborID, Distance: dist})
				heap.Push(results, &candidate{ID: neighborID, Distance: dist})

				if results.Len() > ef {
					heap.Pop(results)
				}
			}
		}
	}

	var result []candidate
	for results.Len() > 0 {
		result = append(result, *heap.Pop(results).(*candidate))
	}
	return result
}

func (h *HNSW) selectNeighbors(query Vector, candidates []candidate, M int) []candidate {
	if len(candidates) <= M {
		return candidates
	}

	sorted := make([]candidate, len(candidates))
	copy(sorted, candidates)

	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i].Distance > sorted[j].Distance {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	if M < len(sorted) {
		return sorted[:M]
	}
	return sorted
}

func (h *HNSW) toCandidates(ids []int) []candidate {
	var result []candidate
	for _, id := range ids {
		node := h.nodes[id]
		result = append(result, candidate{ID: id, Distance: 0})
		_ = node
	}
	return result
}

func (h *HNSW) Search(query Vector, K int, ef int) []candidate {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.nodes) == 0 {
		return nil
	}

	ep := h.entryPoint

	for l := h.maxLevel; l > 0; l-- {
		results := h.searchLayer(query, ep, 1, l)
		if len(results) > 0 {
			ep = results[0].ID
		}
	}

	if ef < K {
		ef = K
	}

	results := h.searchLayer(query, ep, ef, 0)

	if len(results) > K {
		results = results[:K]
	}

	return results
}

type minHeap []*candidate

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i].Distance < h[j].Distance }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x interface{}) { *h = append(*h, x.(*candidate)) }
func (h *minHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

type maxHeap []*candidate

func (h maxHeap) Len() int           { return len(h) }
func (h maxHeap) Less(i, j int) bool { return h[i].Distance > h[j].Distance }
func (h maxHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *maxHeap) Push(x interface{}) { *h = append(*h, x.(*candidate)) }
func (h *maxHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

#### 2.2 IVF（Inverted File Index）

将向量空间划分为多个聚类（Voronoi单元），搜索时只查询最近的几个聚类。

```
全部向量 → K-Means聚类 → 每个向量归属最近的聚类中心
查询时 → 计算q与所有聚类中心的距离 → 只搜索最近的nprobe个聚类
```

**优点**：构建快，内存占用小
**缺点**：召回率依赖nprobe参数，nprobe越大越慢但越准

#### 2.3 PQ（Product Quantization）

将高维向量切分为多个子空间，每个子空间独立量化，大幅压缩存储。

```
128维向量 → 切分为8个16维子向量
每个子向量 → 量化为256个码字之一（1字节）
128维 → 8字节（压缩16倍）
```

**距离计算**：预计算每个码字之间的距离表，查询时查表求和，避免浮点运算。

### 三、索引算法对比

| 算法 | 召回率 | 查询延迟 | 内存占用 | 构建速度 | 适用规模 |
|------|--------|---------|---------|---------|---------|
| 暴力搜索 | 100% | O(N) | 高 | - | <10万 |
| HNSW | 99%+ | O(logN) | 高 | 慢 | 百万~亿 |
| IVF+PQ | 95%+ | O(N/nprobe) | 低 | 快 | 亿级 |
| IVF+HNSW | 98%+ | O(logN/nprobe) | 中 | 中 | 亿级 |

### 四、选型决策

```
数据规模 < 100万？
├── 是 → HNSW（高召回，低延迟）
└── 否 → 内存是否充足？
    ├── 是 → HNSW（最佳召回率）
    └── 否 → IVF+PQ（内存友好）
        └── 召回率不够？ → IVF+HNSW+PQ（混合索引）
```

### 五、总结

向量索引的选择需要在召回率、延迟和内存之间权衡。**HNSW是当前综合表现最好的算法**，适合大多数百万级到千万级的应用场景。对于更大规模的数据，IVF+PQ或混合索引是更实际的选择。

**行业实践**：
- **Milvus**：支持IVF+PQ、HNSW、DiskANN等多种索引
- **Pinecone**：自研索引，支持实时更新
- **Weaviate**：默认HNSW
- **Qdrant**：HNSW优化实现，支持过滤搜索
