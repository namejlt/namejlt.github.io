---
title: "大模型微调方案如何选择"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["大模型", "微调", "LoRA"]
---

## 问题

大模型微调有全量微调（Full Fine-tuning）、LoRA、QLoRA等方案，它们各自的原理、资源需求和适用场景是什么？如何根据模型大小、数据量、GPU资源选择最优方案？

## 回答

微调是让预训练大模型适应特定领域或任务的关键技术。不同的微调方案在效果、成本和效率上有巨大差异，选错方案可能导致资源浪费或效果不佳。

### 一、微调方案概览

| 方案 | 可训练参数量 | 显存需求 | 效果 | 训练速度 |
|------|------------|---------|------|---------|
| Full Fine-tuning | 100% | 极高 | 最好 | 最慢 |
| LoRA | 0.1%~1% | 中 | 接近全量 | 快 |
| QLoRA | 0.1%~1% | 低 | 接近LoRA | 中 |
| Prompt Tuning | <0.01% | 极低 | 一般 | 极快 |
| Adapter | 1%~5% | 中 | 较好 | 中 |

### 二、全量微调（Full Fine-tuning）

全量微调更新模型的所有参数。

**显存计算**（以7B模型为例）：

```
模型参数: 7B × 2 bytes (FP16) = 14 GB
梯度: 7B × 2 bytes = 14 GB
优化器状态 (AdamW): 7B × 8 bytes = 56 GB
激活值: ~4 GB
───────────────────────────
总计: ~88 GB → 至少需要4×A100(40GB)
```

**问题**：
- 显存需求巨大，7B模型需要4张A100
- 训练时间长，成本高
- 容易过拟合（数据量不足时）
- 灾难性遗忘（忘记预训练知识）

### 三、LoRA（Low-Rank Adaptation）

LoRA的核心思想：模型微调时的权重变化矩阵是低秩的，可以用两个小矩阵的乘积近似。

**原理**：

```
原始权重: W (d×d)
微调变化: ΔW = A × B
  其中 A: d×r, B: r×d, r << d

前向传播: y = (W + ΔW) × x = W × x + A × B × x

可训练参数: 2 × d × r (原始为 d × d)
当 r=8, d=4096: 2×4096×8 = 65536 vs 4096×4096 = 16777216
参数量减少256倍!
```

**Go实现LoRA推理**：

```go
package lora

import (
	"math/rand"
)

type Matrix struct {
	Rows int
	Cols int
	Data []float32
}

func NewMatrix(rows, cols int) *Matrix {
	return &Matrix{
		Rows: rows,
		Cols: cols,
		Data: make([]float32, rows*cols),
	}
}

func (m *Matrix) At(i, j int) float32 {
	return m.Data[i*m.Cols+j]
}

func (m *Matrix) Set(i, j int, v float32) {
	m.Data[i*m.Cols+j] = v
}

func MatMul(a, b *Matrix) *Matrix {
	if a.Cols != b.Rows {
		panic("matrix dimensions mismatch")
	}
	result := NewMatrix(a.Rows, b.Cols)
	for i := 0; i < a.Rows; i++ {
		for j := 0; j < b.Cols; j++ {
			var sum float32
			for k := 0; k < a.Cols; k++ {
				sum += a.At(i, k) * b.At(k, j)
			}
			result.Set(i, j, sum)
		}
	}
	return result
}

func MatAdd(a, b *Matrix) *Matrix {
	result := NewMatrix(a.Rows, a.Cols)
	for i := range a.Data {
		result.Data[i] = a.Data[i] + b.Data[i]
	}
	return result
}

type LoRALayer struct {
	OriginalWeight *Matrix
	Alpha          float32
	Scale          float32
	A              *Matrix
	B              *Matrix
}

func NewLoRALayer(weight *Matrix, rank int, alpha float32) *LoRALayer {
	d := weight.Rows
	scale := alpha / float32(rank)

	a := NewMatrix(d, rank)
	b := NewMatrix(rank, d)

	for i := range a.Data {
		a.Data[i] = rand.Float32() * 0.01
	}

	return &LoRALayer{
		OriginalWeight: weight,
		Alpha:          alpha,
		Scale:          scale,
		A:              a,
		B:              b,
	}
}

func (l *LoRALayer) Forward(x *Matrix) *Matrix {
	original := MatMul(x, l.OriginalWeight)

	delta := MatMul(l.A, l.B)
	for i := range delta.Data {
		delta.Data[i] *= l.Scale
	}

	adaptation := MatMul(x, delta)

	return MatAdd(original, adaptation)
}

func (l *LoRALayer) MergeWeights() *Matrix {
	delta := MatMul(l.A, l.B)
	for i := range delta.Data {
		delta.Data[i] *= l.Scale
	}
	return MatAdd(l.OriginalWeight, delta)
}

type LoRAModel struct {
	layers map[string]*LoRALayer
}

func NewLoRAModel() *LoRAModel {
	return &LoRAModel{
		layers: make(map[string]*LoRALayer),
	}
}

func (m *LoRAModel) AddLayer(name string, weight *Matrix, rank int, alpha float32) {
	m.layers[name] = NewLoRALayer(weight, rank, alpha)
}

func (m *LoRAModel) GetLayer(name string) (*LoRALayer, bool) {
	l, ok := m.layers[name]
	return l, ok
}

func (m *LoRAModel) TrainableParams() int64 {
	var total int64
	for _, layer := range m.layers {
		total += int64(layer.A.Rows*layer.A.Cols + layer.B.Rows*layer.B.Cols)
	}
	return total
}

func (m *LoRAModel) MergeAll() map[string]*Matrix {
	merged := make(map[string]*Matrix)
	for name, layer := range m.layers {
		merged[name] = layer.MergeWeights()
	}
	return merged
}
```

**LoRA的关键参数**：

| 参数 | 推荐值 | 说明 |
|------|--------|------|
| rank (r) | 8~64 | 越大表达能力越强，但参数越多 |
| alpha | 2×rank | 缩放因子，控制LoRA的影响强度 |
| target_modules | q_proj, v_proj | 通常只对Attention的Q/V矩阵加LoRA |

### 四、QLoRA

QLoRA在LoRA基础上增加了量化，进一步降低显存需求。

**核心创新**：
1. **4-bit NormalFloat量化**：信息论最优的量化数据类型
2. **双重量化**：对量化常数本身再次量化，节省0.37bit/参数
3. **分页优化器**：利用CPU内存分页，避免OOM

**显存对比**（7B模型）：

```
Full Fine-tuning: ~88 GB → 4×A100
LoRA (FP16):      ~28 GB → 1×A100
QLoRA (4-bit):    ~10 GB → 1×RTX 4090
```

### 五、方案选型决策

```
GPU显存是否充足（≥4×A100）？
├── 是 → 数据量是否很大（>100K）？
│   ├── 是 → Full Fine-tuning
│   └── 否 → LoRA
└── 否 → 是否有单张A100？
    ├── 是 → LoRA
    └── 否 → QLoRA（消费级GPU可用）
```

**数据量与方案的关系**：

| 数据量 | 推荐方案 | 原因 |
|--------|---------|------|
| <1K | Prompt Engineering / Few-Shot | 数据太少，微调容易过拟合 |
| 1K~10K | LoRA / QLoRA | 足够学习领域特征 |
| 10K~100K | LoRA (rank=64) | 增大rank提升表达能力 |
| >100K | Full Fine-tuning | 数据充足，全量微调效果最好 |

### 六、微调数据准备

```go
package finetune

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type TrainingSample struct {
	Messages []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Dataset struct {
	Samples []TrainingSample
}

func LoadDataset(path string) (*Dataset, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var samples []TrainingSample
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var s TrainingSample
		if err := json.Unmarshal([]byte(line), s); err != nil {
			continue
		}
		samples = append(samples, s)
	}

	return &Dataset{Samples: samples}, nil
}

func (d *Dataset) Validate() error {
	for i, s := range d.Samples {
		if len(s.Messages) < 2 {
			return fmt.Errorf("sample %d: must have at least 2 messages", i)
		}
		if s.Messages[0].Role != "system" && s.Messages[0].Role != "user" {
			return fmt.Errorf("sample %d: first message must be system or user", i)
		}
		for _, m := range s.Messages {
			if m.Role != "system" && m.Role != "user" && m.Role != "assistant" {
				return fmt.Errorf("sample %d: invalid role %q", i, m.Role)
			}
		}
	}
	return nil
}

func (d *Dataset) Stats() {
	roles := make(map[string]int)
	totalTokens := 0
	for _, s := range d.Samples {
		for _, m := range s.Messages {
			roles[m.Role]++
			totalTokens += len(m.Content) / 4
		}
	}
	fmt.Printf("Total samples: %d\n", len(d.Samples))
	fmt.Printf("Estimated tokens: %d\n", totalTokens)
	for role, count := range roles {
		fmt.Printf("  %s messages: %d\n", role, count)
	}
}
```

### 七、总结

微调方案的选择取决于三个因素：**GPU资源、数据量、效果要求**。

**核心结论**：
1. **LoRA是性价比最高的方案**，90%的场景下足够
2. **QLoRA让消费级GPU也能微调7B模型**，极大降低门槛
3. **Full Fine-tuning只在数据量极大时才有必要**
4. **数据质量比微调方案更重要**，1000条高质量数据胜过10000条噪声数据
5. **rank=16, alpha=32是安全的默认配置**

**行业实践**：
- **Hugging Face PEFT**：最流行的LoRA/QLoRA训练库
- **LLaMA-Factory**：一站式微调平台，支持多种方案
- **Axolotl**：配置驱动的微调工具
