---
title: "一致性哈希算法如何设计与实现"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["一致性哈希", "分布式", "golang"]
---

## 问题

在分布式缓存、分布式存储等场景中，当节点增减时如何最小化数据迁移？一致性哈希算法的原理是什么？虚拟节点如何解决数据倾斜问题？如何用Go实现一个生产级的一致性哈希？

## 回答

一致性哈希是分布式系统中解决数据分布和负载均衡的核心算法。传统的哈希取模算法在节点增减时会导致大量数据迁移，一致性哈希通过巧妙的环形空间设计，将节点增减时的数据迁移量从O(N)降低到O(K/N)。

### 一、传统哈希取模的问题

**哈希取模**：`hash(key) % N`，N为节点数。

```
3个节点: hash(key) % 3 → 节点0, 节点1, 节点2
4个节点: hash(key) % 4 → 节点0, 节点1, 节点2, 节点3
```

**问题**：当节点数从3变为4时，约75%的Key需要迁移！

```
hash(key1) = 10 → 10%3=1(节点1) → 10%4=2(节点2)  迁移!
hash(key2) = 15 → 15%3=0(节点0) → 15%4=3(节点3)  迁移!
hash(key3) = 20 → 20%3=2(节点2) → 20%4=0(节点0)  迁移!
hash(key4) = 7  →  7%3=1(节点1) →  7%4=3(节点3)  迁移!
```

**结论**：哈希取模在节点变化时，几乎所有数据都需要重新分布，这在生产环境中是不可接受的。

### 二、一致性哈希的原理

一致性哈希将整个哈希值空间组织成一个虚拟的圆环（Hash Ring），环的范围是0~2^32-1。

**核心步骤**：

1. **计算节点哈希**：将每个节点的标识（如IP:Port）映射到环上的位置
2. **计算Key哈希**：将每个Key映射到环上的位置
3. **顺时针查找**：Key沿环顺时针方向找到的第一个节点就是其归属节点

```
         0
        / \
   NodeA   NodeC
    |        |
  Key1     Key3
    |        |
   NodeB---Key2---2^32-1

Key1 → 顺时针 → NodeA
Key2 → 顺时针 → NodeB
Key3 → 顺时针 → NodeC
```

**节点增减的影响**：

- **增加节点D**：只有NodeA和NodeD之间的Key需要迁移到NodeD，其他Key不受影响
- **删除节点B**：NodeB上的Key迁移到顺时针下一个节点NodeC，其他Key不受影响

**数据迁移量**：理论上，增加或删除一个节点，只有K/N的Key需要迁移（K为Key总数，N为节点数）。

### 三、数据倾斜问题与虚拟节点

**问题**：当节点数较少时，Key在环上的分布可能极不均匀。例如3个节点，可能80%的Key都落在NodeA上。

**解决方案——虚拟节点**：为每个物理节点创建多个虚拟节点，均匀分布在环上。

```
物理节点: NodeA, NodeB, NodeC
虚拟节点: NodeA#1, NodeA#2, ..., NodeA#100
          NodeB#1, NodeB#2, ..., NodeB#100
          NodeC#1, NodeC#2, ..., NodeC#100
```

虚拟节点越多，Key的分布越均匀。通常每个物理节点对应100~200个虚拟节点。

**权重支持**：性能更强的节点可以分配更多虚拟节点，从而承担更多负载。

### 四、Go语言实现

#### 4.1 基础一致性哈希

```go
package consistenthash

import (
	"hash/crc32"
	"sort"
	"strconv"
	"sync"
)

type Map struct {
	mu       sync.RWMutex
	hash     func(data []byte) uint32
	replicas int
	keys     []int
	hashMap  map[int]string
}

func New(replicas int, fn func(data []byte) uint32) *Map {
	m := &Map{
		replicas: replicas,
		hash:     fn,
		hashMap:  make(map[int]string),
	}
	if m.hash == nil {
		m.hash = crc32.ChecksumIEEE
	}
	return m
}

func (m *Map) Add(nodes ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, node := range nodes {
		for i := 0; i < m.replicas; i++ {
			hash := int(m.hash([]byte(strconv.Itoa(i) + node)))
			m.keys = append(m.keys, hash)
			m.hashMap[hash] = node
		}
	}
	sort.Ints(m.keys)
}

func (m *Map) Get(key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.keys) == 0 {
		return ""
	}

	hash := int(m.hash([]byte(key)))

	idx := sort.Search(len(m.keys), func(i int) bool {
		return m.keys[i] >= hash
	})

	if idx == len(m.keys) {
		idx = 0
	}

	return m.hashMap[m.keys[idx]]
}

func (m *Map) Remove(node string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := 0; i < m.replicas; i++ {
		hash := int(m.hash([]byte(strconv.Itoa(i) + node)))
		delete(m.hashMap, hash)

		idx := sort.SearchInts(m.keys, hash)
		if idx < len(m.keys) && m.keys[idx] == hash {
			m.keys = append(m.keys[:idx], m.keys[idx+1:]...)
		}
	}
}
```

#### 4.2 带权重的一致性哈希

```go
package consistenthash

import (
	"hash/crc32"
	"sort"
	"strconv"
	"sync"
)

type WeightedNode struct {
	Name   string
	Weight int
}

type WeightedMap struct {
	mu       sync.RWMutex
	hash     func(data []byte) uint32
	keys     []int
	hashMap  map[int]string
	nodes    map[string]int
}

func NewWeighted(fn func(data []byte) uint32) *WeightedMap {
	m := &WeightedMap{
		hash:    fn,
		hashMap: make(map[int]string),
		nodes:   make(map[string]int),
	}
	if m.hash == nil {
		m.hash = crc32.ChecksumIEEE
	}
	return m
}

func (m *WeightedMap) Add(node WeightedNode) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.nodes[node.Name]; ok {
		m.removeReplicas(node.Name, existing)
	}

	m.nodes[node.Name] = node.Weight
	replicas := node.Weight * 100

	for i := 0; i < replicas; i++ {
		hash := int(m.hash([]byte(strconv.Itoa(i) + node.Name)))
		m.keys = append(m.keys, hash)
		m.hashMap[hash] = node.Name
	}
	sort.Ints(m.keys)
}

func (m *WeightedMap) removeReplicas(name string, weight int) {
	replicas := weight * 100
	for i := 0; i < replicas; i++ {
		hash := int(m.hash([]byte(strconv.Itoa(i) + name)))
		delete(m.hashMap, hash)

		idx := sort.SearchInts(m.keys, hash)
		if idx < len(m.keys) && m.keys[idx] == hash {
			m.keys = append(m.keys[:idx], m.keys[idx+1:]...)
		}
	}
}

func (m *WeightedMap) Remove(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if weight, ok := m.nodes[name]; ok {
		m.removeReplicas(name, weight)
		delete(m.nodes, name)
	}
}

func (m *WeightedMap) Get(key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.keys) == 0 {
		return ""
	}

	hash := int(m.hash([]byte(key)))
	idx := sort.Search(len(m.keys), func(i int) bool {
		return m.keys[i] >= hash
	})

	if idx == len(m.keys) {
		idx = 0
	}

	return m.hashMap[m.keys[idx]]
}
```

#### 4.3 Jump Consistent Hash

Google发表的Jump Consistent Hash算法，无需虚拟节点，零内存占用，分布均匀：

```go
package consistenthash

func JumpHash(key uint64, numBuckets int) int {
	var b int64 = -1
	var j int64

	for j < int64(numBuckets) {
		b = j
		key = key*2862933555777941757 + 1
		j = int64(float64(b+1) * (float64(int64(1)<<31) / float64((key>>33)+1)))
	}

	return int(b)
}
```

**Jump Hash的特点**：
- **零内存**：不需要存储环和映射关系
- **分布均匀**：基于概率论，分布比虚拟节点更均匀
- **只能增加节点**：不支持删除节点（删除节点时迁移量不可控）
- **无法指定节点**：只能按序号选择，不能指定节点名称

### 五、Maglev一致性哈希

Google Maglev负载均衡器使用的一致性哈希变体，查找表方式实现O(1)查找：

```go
package consistenthash

import (
	"hash/fnv"
)

type Maglev struct {
	lookupTable []int
	tableSize   int
	nodes       []string
}

func NewMaglev(nodes []string, tableSize int) *Maglev {
	m := &Maglev{
		tableSize: tableSize,
		nodes:     nodes,
	}
	m.populate()
	return m
}

func (m *Maglev) populate() {
	n := len(m.nodes)
	m.lookupTable = make([]int, m.tableSize)
	for i := range m.lookupTable {
		m.lookupTable[i] = -1
	}

	offsets := make([]int, n)
	skips := make([]int, n)

	for i, node := range m.nodes {
		h1, h2 := hashNode(node)
		offsets[i] = int(h1 % uint32(m.tableSize))
		skips[i] = int(h2%uint32(m.tableSize-1) + 1)
	}

	filled := 0
	next := make([]int, n)

	for filled < m.tableSize {
		for i := 0; i < n; i++ {
			candidate := (offsets[i] + next[i]*skips[i]) % m.tableSize

			for m.lookupTable[candidate] != -1 {
				next[i]++
				candidate = (offsets[i] + next[i]*skips[i]) % m.tableSize
			}

			m.lookupTable[candidate] = i
			next[i]++
			filled++

			if filled >= m.tableSize {
				break
			}
		}
	}
}

func (m *Maglev) Get(key string) string {
	h := fnv.New32a()
	h.Write([]byte(key))
	idx := h.Sum32() % uint32(m.tableSize)
	return m.nodes[m.lookupTable[idx]]
}

func hashNode(node string) (uint32, uint32) {
	h1 := fnv.New32a()
	h1.Write([]byte(node))
	h2 := fnv.New32a()
	h2.Write([]byte("salt" + node))
	return h1.Sum32(), h2.Sum32()
}
```

### 六、算法对比

| 算法 | 查找复杂度 | 内存占用 | 分布均匀性 | 支持删除 | 支持权重 |
|------|-----------|---------|-----------|---------|---------|
| 哈希取模 | O(1) | O(1) | 好 | 节点变化全迁移 | 否 |
| 一致性哈希+虚拟节点 | O(logN) | O(N*V) | 较好 | 是 | 是 |
| Jump Hash | O(logN) | O(1) | 极好 | 否 | 否 |
| Maglev | O(1) | O(M) | 好 | 重建表 | 否 |

### 七、实际应用场景

1. **分布式缓存**：Memcached客户端（如ketama）、Redis Cluster的hash slot
2. **分布式存储**：Ceph的CRUSH算法、Dynamo的数据分区
3. **负载均衡**：Nginx的consistent_hash模块、Maglev
4. **分布式数据库**：Vitess的keyspace分片、TiKV的Region分布

**Redis Cluster的特殊方案**：Redis Cluster没有使用一致性哈希，而是使用了16384个Hash Slot。每个Key映射到一个Slot，每个节点负责一部分Slot。这种方案在节点增减时只需要迁移Slot，比一致性哈希更可控。

### 八、总结

一致性哈希解决了分布式系统中节点增减时的数据迁移问题，是分布式缓存和存储的基石算法。

**选型建议**：
- **通用场景**：一致性哈希 + 虚拟节点（100~200个/物理节点），实现简单，效果良好
- **内存敏感**：Jump Consistent Hash，零内存，分布极均匀，但不支持删除
- **高性能查找**：Maglev，O(1)查找，适合负载均衡器
- **生产级需求**：参考Redis Cluster的Hash Slot方案，更可控更可运维

**关键注意事项**：
1. 哈希函数的选择影响分布均匀性，推荐使用MurmurHash3或xxHash
2. 虚拟节点数量需要根据实际节点数调整，节点越多虚拟节点可以越少
3. 节点增减时需要考虑数据迁移的平滑性，避免瞬间大量迁移
