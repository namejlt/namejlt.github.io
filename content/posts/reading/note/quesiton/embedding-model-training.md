---
title: "Embedding模型如何训练与优化"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["Embedding", "向量", "golang"]
---

## 问题

Embedding模型是RAG和语义搜索的基础，如何训练一个高质量的领域Embedding模型？对比学习、困难负样本挖掘、知识蒸馏等技术的原理是什么？如何评估Embedding质量？

## 回答

Embedding模型将文本映射到高维向量空间，使语义相似的文本在向量空间中距离更近。通用Embedding模型（如text-embedding-ada-002）在特定领域可能效果不佳，领域微调是提升效果的关键。

### 一、Embedding训练的核心方法

#### 1.1 对比学习（Contrastive Learning）

对比学习是Embedding训练最主流的方法，核心是"拉近正样本、推远负样本"。

```
锚点(Anchor): "什么是机器学习"
正样本(Positive): "机器学习的定义是什么"  → 拉近
负样本(Negative): "今天天气怎么样"        → 推远

损失函数: InfoNCE
L = -log(exp(sim(anchor, positive)/τ) / Σexp(sim(anchor, negative_i)/τ))
```

#### 1.2 困难负样本挖掘

随机负样本太简单，模型容易"偷懒"。困难负样本（Hard Negatives）与锚点语义相似但不是正样本，能显著提升模型区分能力。

```go
package embedding

import (
	"sort"
)

type HardNegativeMiner struct {
	embedder Embedder
	topK     int
}

type Embedder interface {
	Embed(text string) []float32
}

type Triple struct {
	Anchor    string
	Positive  string
	Negative  string
	Score     float32
}

func NewHardNegativeMiner(embedder Embedder, topK int) *HardNegativeMiner {
	return &HardNegativeMiner{embedder: embedder, topK: topK}
}

func (m *HardNegativeMiner) Mine(anchor string, positives []string, candidates []string) []Triple {
	anchorVec := m.embedder.Embed(anchor)

	type scored struct {
		text  string
		score float32
	}

	var scored []scored
	for _, c := range candidates {
		isPositive := false
		for _, p := range positives {
			if c == p {
				isPositive = true
				break
			}
		}
		if isPositive {
			continue
		}

		cVec := m.embedder.Embed(c)
		sim := cosineSim(anchorVec, cVec)
		scored = append(scored, scored{text: c, score: sim})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	var triples []Triple
	for i := 0; i < min(m.topK, len(scored)); i++ {
		for _, pos := range positives {
			triples = append(triples, Triple{
				Anchor:    anchor,
				Positive:  pos,
				Negative:  scored[i].text,
				Score:     scored[i].score,
			})
		}
	}

	return triples
}

func cosineSim(a, b []float32) float32 {
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (sqrt32(normA) * sqrt32(normB))
}

func sqrt32(x float32) float32 {
	return float32(sqrt(float64(x)))
}
```

### 二、训练数据构造

```go
package embedding

type TrainingDataBuilder struct {
	samples []Triple
}

func NewTrainingDataBuilder() *TrainingDataBuilder {
	return &TrainingDataBuilder{}
}

func (b *TrainingDataBuilder) AddFromQA(questions []string, answers []string) *TrainingDataBuilder {
	for i, q := range questions {
		for j, a := range answers {
			if i == j {
				continue
			}
			b.samples = append(b.samples, Triple{
				Anchor:   q,
				Positive: answers[i],
				Negative: a,
			})
		}
	}
	return b
}

func (b *TrainingDataBuilder) AddFromPairs(pairs [][2]string) *TrainingDataBuilder {
	for i, pair := range pairs {
		for j, other := range pairs {
			if i == j {
				continue
			}
			b.samples = append(b.samples, Triple{
				Anchor:    pair[0],
				Positive:  pair[1],
				Negative:  other[1],
			})
		}
	}
	return b
}

func (b *TrainingDataBuilder) Build() []Triple {
	return b.samples
}
```

### 三、知识蒸馏

用大模型（Teacher）指导小模型（Student）训练，使小模型获得接近大模型的能力。

```go
type DistillationTrainer struct {
	teacher Embedder
	student Embedder
	temp    float32
}

func NewDistillationTrainer(teacher, student Embedder, temperature float32) *DistillationTrainer {
	return &DistillationTrainer{
		teacher: teacher,
		student: student,
		temp:    temperature,
	}
}

func (t *DistillationTrainer) ComputeLoss(anchor, positive string) float32 {
	teacherAnchor := t.teacher.Embed(anchor)
	teacherPos := t.teacher.Embed(positive)
	studentAnchor := t.student.Embed(anchor)
	studentPos := t.student.Embed(positive)

	teacherSim := cosineSim(teacherAnchor, teacherPos)
	studentSim := cosineSim(studentAnchor, studentPos)

	diff := studentSim - teacherSim
	return diff * diff
}
```

### 四、Embedding质量评估

```go
package embedding

type EvaluationResult struct {
	MRR       float32
	RecallAt1  float32
	RecallAt5  float32
	RecallAt10 float32
	NDCGAt10  float32
}

type QueryDocPair struct {
	Query       string
	Relevant    []string
	NonRelevant []string
}

type Evaluator struct {
	embedder Embedder
}

func NewEvaluator(embedder Embedder) *Evaluator {
	return &Evaluator{embedder: embedder}
}

func (e *Evaluator) Evaluate(pairs []QueryDocPair) *EvaluationResult {
	var totalMRR, totalR1, totalR5, totalR10 float32

	for _, pair := range pairs {
		queryVec := e.embedder.Embed(pair.Query)

		type scored struct {
			text     string
			score    float32
			relevant bool
		}

		var all []scored
		for _, doc := range pair.Relevant {
			docVec := e.embedder.Embed(doc)
			all = append(all, scored{text: doc, score: cosineSim(queryVec, docVec), relevant: true})
		}
		for _, doc := range pair.NonRelevant {
			docVec := e.embedder.Embed(doc)
			all = append(all, scored{text: doc, score: cosineSim(queryVec, docVec), relevant: false})
		}

		sort.Slice(all, func(i, j int) bool {
			return all[i].score > all[j].score
		})

		for rank, s := range all {
			if s.relevant {
				totalMRR += 1.0 / float32(rank+1)
				break
			}
		}

		if len(all) > 0 && all[0].relevant {
			totalR1++
		}

		relevantInTop5 := 0
		for i := 0; i < min(5, len(all)); i++ {
			if all[i].relevant {
				relevantInTop5++
			}
		}
		if relevantInTop5 > 0 {
			totalR5++
		}

		relevantInTop10 := 0
		for i := 0; i < min(10, len(all)); i++ {
			if all[i].relevant {
				relevantInTop10++
			}
		}
		if relevantInTop10 > 0 {
			totalR10++
		}
	}

	n := float32(len(pairs))
	return &EvaluationResult{
		MRR:       totalMRR / n,
		RecallAt1:  totalR1 / n,
		RecallAt5:  totalR5 / n,
		RecallAt10: totalR10 / n,
	}
}
```

### 五、优化策略总结

| 策略 | 原理 | 效果 |
|------|------|------|
| 困难负样本 | 选取语义相似但非正样本的负样本 | 召回率提升5-15% |
| 多正样本 | 同一锚点配多个正样本 | 提升训练稳定性 |
| 温度系数 | 控制softmax的平滑度 | 影响训练难度 |
| 知识蒸馏 | 大模型指导小模型 | 小模型达到大模型90%+效果 |
| 指令微调 | 加入任务描述前缀 | 支持多任务Embedding |

### 六、总结

Embedding模型的质量直接决定RAG系统的上限。**核心优化路径**：

1. **数据**：高质量领域数据 + 困难负样本挖掘
2. **训练**：对比学习 + 温度系数调优
3. **蒸馏**：大模型知识迁移到小模型
4. **评估**：MRR/Recall/NDCG多指标评估
5. **迭代**：线上Bad Case分析 → 数据补充 → 重新训练

**行业实践**：
- **BGE**：BAAI开源，中文效果最好
- **E5**：微软开源，多语言
- **GTE**：阿里开源，支持多长度
- **Sentence-Transformers**：最流行的训练框架
