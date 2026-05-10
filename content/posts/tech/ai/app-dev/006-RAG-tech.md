---
title: "AI 应用开发-006 RAG技术原理"
date: 2025-05-28T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型", "RAG", "检索增强生成"]
---

## 概述

RAG（Retrieval-Augmented Generation，检索增强生成）是解决大模型"知识过时"和"幻觉"问题的核心技术。它通过在生成回答前先检索外部知识库，将相关上下文注入Prompt，使大模型的回答基于真实数据，而非仅依赖训练时的记忆。

本章将系统讲解RAG的技术原理、架构演进、关键组件，以及从Naive RAG到Agentic RAG的技术发展脉络。

## 一、为什么需要RAG

### 1.1 大模型的三大局限

| 局限 | 表现 | RAG如何解决 |
|------|------|------------|
| 知识过时 | 训练数据截止后的事件无法回答 | 实时检索最新数据 |
| 幻觉问题 | 编造不存在的事实 | 基于检索到的真实文档回答 |
| 领域缺失 | 专业领域知识不足 | 接入领域知识库 |

### 1.2 RAG vs 微调 vs 预训练

| 维度 | RAG | 微调 | 预训练 |
|------|-----|------|--------|
| 知识更新 | 实时 | 需重新训练 | 需重新训练 |
| 成本 | 低 | 中 | 极高 |
| 适用场景 | 知识密集型问答 | 风格/格式适配 | 基础能力提升 |
| 可解释性 | 高（可溯源） | 低 | 低 |
| 数据安全 | 可控（本地知识库） | 需上传数据 | 需上传数据 |

**结论**：在大多数企业场景中，RAG是性价比最高的方案。

## 二、RAG核心架构

### 2.1 基本流程

```
用户提问 → Query编码 → 向量检索 → 上下文组装 → LLM生成 → 返回答案
```

RAG的核心是"先检索，后生成"，将检索到的相关文档片段作为上下文，与用户问题一起输入LLM。

### 2.2 Naive RAG架构

最基础的RAG实现，包含三个阶段：

1. **索引阶段**：文档分块 → Embedding → 存入向量数据库
2. **检索阶段**：Query Embedding → 向量相似度搜索 → Top-K文档
3. **生成阶段**：Prompt组装（Query + 检索结果）→ LLM生成回答

```
┌─────────────┐     ┌──────────────┐     ┌──────────────┐
│  索引阶段    │     │  检索阶段     │     │  生成阶段     │
│             │     │              │     │              │
│ 文档 → 分块  │────→│ Query → 向量  │────→│ Prompt组装   │
│ → Embedding │     │ → Top-K检索   │     │ → LLM生成    │
│ → 向量库    │     │              │     │              │
└─────────────┘     └──────────────┘     └──────────────┘
```

### 2.3 Naive RAG的不足

- **检索质量低**：单一向量检索可能返回不相关文档
- **分块不合理**：固定长度分块会截断语义完整性
- **缺乏推理**：无法判断检索结果是否真正回答了问题
- **上下文冗余**：Top-K文档可能包含大量无关信息

## 三、RAG技术演进

### 3.1 Advanced RAG

Advanced RAG在Naive RAG基础上，对检索前、检索中、检索后三个环节进行优化。

#### 检索前优化（Pre-Retrieval）

```python
def query_rewrite(original_query: str, llm_client) -> str:
    prompt = f"""请将以下用户问题改写为更适合检索的查询语句。
要求：
1. 保留核心意图
2. 补充隐含信息
3. 使用更精确的关键词

原始问题：{original_query}
改写后的查询："""

    response = llm_client.call(prompt)
    return response


def query_expand(query: str, llm_client, num_expansions: int = 3) -> list[str]:
    prompt = f"""请为以下查询生成{num_expansions}个不同角度的查询变体，
以便从知识库中检索到更全面的信息。

原始查询：{query}
变体查询："""

    response = llm_client.call(prompt)
    variants = [v.strip() for v in response.split("\n") if v.strip()]
    return [query] + variants[:num_expansions]
```

#### 检索中优化（Retrieval）

混合检索（Hybrid Search）结合向量检索和关键词检索的优势：

```python
import numpy as np
from typing import List, Tuple


def hybrid_search(
    query: str,
    query_embedding: list[float],
    vector_store,
    bm25_scores: list[tuple[str, float]],
    alpha: float = 0.7,
    top_k: int = 5
) -> list[tuple[str, float]]:
    vector_results = vector_store.search(query_embedding, top_k=top_k * 2)
    vector_dict = {doc: score for doc, score in vector_results}
    bm25_dict = {doc: score for doc, score in bm25_scores}

    all_docs = set(vector_dict.keys()) | set(bm25_dict.keys())

    max_vec = max(vector_dict.values()) if vector_dict else 1.0
    max_bm25 = max(bm25_dict.values()) if bm25_dict else 1.0

    combined = []
    for doc in all_docs:
        vec_score = vector_dict.get(doc, 0.0) / max_vec
        bm25_score = bm25_dict.get(doc, 0.0) / max_bm25
        final_score = alpha * vec_score + (1 - alpha) * bm25_score
        combined.append((doc, final_score))

    combined.sort(key=lambda x: x[1], reverse=True)
    return combined[:top_k]
```

#### 检索后优化（Post-Retrieval）

重排序（Reranking）对初步检索结果进行精细化排序：

```python
def rerank_with_cross_encoder(
    query: str,
    documents: list[str],
    cross_encoder_model,
    top_k: int = 5
) -> list[tuple[str, float]]:
    pairs = [(query, doc) for doc in documents]
    scores = cross_encoder_model.predict(pairs)

    ranked = list(zip(documents, scores.tolist()))
    ranked.sort(key=lambda x: x[1], reverse=True)
    return ranked[:top_k]
```

### 3.2 Modular RAG

Modular RAG将RAG系统拆分为可插拔的模块，每个模块可独立优化和替换。

| 模块 | 功能 | 可选方案 |
|------|------|----------|
| 索引 | 文档处理与存储 | FAISS, Milvus, Elasticsearch |
| 检索 | 语义/关键词/混合 | 向量检索, BM25, 混合检索 |
| 重排 | 结果精排 | Cross-Encoder, Cohere Rerank |
| 压缩 | 上下文精简 | LLMLingua, Compressor |
| 生成 | 答案生成 | GPT-4, Qwen, DeepSeek |

### 3.3 GraphRAG

GraphRAG是微软提出的一种结合知识图谱的RAG架构，解决了传统RAG在全局性问题上的不足。

#### 核心思想

传统RAG擅长局部细节问答，但面对"总结所有文档的核心观点"这类全局性问题时表现不佳。GraphRAG通过构建文档的知识图谱，实现全局-局部双层搜索。

#### 工作流程

```
1. 文档分块 → 实体抽取 → 关系抽取 → 构建知识图谱
2. 社区检测 → 生成社区摘要 → 建立层次化索引
3. 查询时：局部查询走向量检索，全局查询走社区摘要
```

```python
from dataclasses import dataclass
from typing import Optional


@dataclass
class Entity:
    name: str
    type: str
    description: str


@dataclass
class Relation:
    source: str
    target: str
    relation_type: str
    description: str


@dataclass
class Community:
    id: str
    entities: list[str]
    summary: str
    level: int


class GraphRAGIndexer:
    def __init__(self, llm_client):
        self.llm_client = llm_client
        self.entities: list[Entity] = []
        self.relations: list[Relation] = []
        self.communities: list[Community] = []

    def extract_entities_and_relations(self, text: str) -> tuple[list[Entity], list[Relation]]:
        prompt = f"""请从以下文本中抽取实体和关系，按JSON格式输出。

文本：{text}

输出格式：
{{
    "entities": [{{"name": "实体名", "type": "类型", "description": "描述"}}],
    "relations": [{{"source": "源实体", "target": "目标实体", "relation_type": "关系类型", "description": "描述"}}]
}}"""

        response = self.llm_client.call(prompt)
        return self._parse_extraction(response)

    def build_communities(self) -> list[Community]:
        entity_names = [e.name for e in self.entities]
        adjacency = {name: set() for name in entity_names}
        for r in self.relations:
            if r.source in adjacency:
                adjacency[r.source].add(r.target)
            if r.target in adjacency:
                adjacency[r.target].add(r.source)

        visited = set()
        communities = []
        community_id = 0

        for entity in entity_names:
            if entity not in visited:
                community_entities = self._bfs(entity, adjacency, visited)
                community = Community(
                    id=f"community_{community_id}",
                    entities=community_entities,
                    summary="",
                    level=0
                )
                communities.append(community)
                community_id += 1

        self.communities = communities
        return communities

    def _bfs(self, start: str, adjacency: dict, visited: set) -> list[str]:
        queue = [start]
        component = []
        while queue:
            node = queue.pop(0)
            if node not in visited:
                visited.add(node)
                component.append(node)
                for neighbor in adjacency.get(node, set()):
                    if neighbor not in visited:
                        queue.append(neighbor)
        return component

    def _parse_extraction(self, response: str) -> tuple[list[Entity], list[Relation]]:
        import json
        try:
            data = json.loads(response)
            entities = [Entity(**e) for e in data.get("entities", [])]
            relations = [Relation(**r) for r in data.get("relations", [])]
            return entities, relations
        except json.JSONDecodeError:
            return [], []
```

### 3.4 Agentic RAG

Agentic RAG是RAG技术的最新演进，将Agent的自主决策能力引入RAG流程。

#### 核心特征

- **自适应检索**：Agent根据问题类型自主决定是否检索、检索什么、检索几次
- **工具调用**：Agent可调用多种工具（搜索引擎、数据库、API等）
- **反思迭代**：Agent可评估检索结果质量，决定是否重新检索
- **多步推理**：复杂问题可拆解为多步，每步独立检索和推理

```python
from enum import Enum
from typing import Optional


class AgentAction(Enum):
    RETRIEVE = "retrieve"
    GENERATE = "generate"
    REFINE_QUERY = "refine_query"
    STOP = "stop"


class AgenticRAG:
    def __init__(self, llm_client, vector_store, max_iterations: int = 5):
        self.llm_client = llm_client
        self.vector_store = vector_store
        self.max_iterations = max_iterations

    def decide_action(self, query: str, context: str, iteration: int) -> AgentAction:
        if iteration >= self.max_iterations:
            return AgentAction.GENERATE

        prompt = f"""基于当前状态，决定下一步操作：

用户问题：{query}
当前上下文：{context[:500]}
迭代次数：{iteration}

可选操作：
- RETRIEVE: 需要更多信息，执行检索
- GENERATE: 信息充分，生成回答
- REFINE_QUERY: 当前查询不够精确，需要改写
- STOP: 无法回答，停止

请只输出操作名称："""

        response = self.llm_client.call(prompt).strip().upper()
        try:
            return AgentAction[response]
        except KeyError:
            return AgentAction.GENERATE

    def run(self, query: str) -> str:
        context = ""
        current_query = query

        for iteration in range(self.max_iterations):
            action = self.decide_action(query, context, iteration)

            if action == AgentAction.RETRIEVE:
                query_emb = self._get_embedding(current_query)
                results = self.vector_store.search(query_emb, top_k=3)
                new_context = "\n".join([doc for doc, _ in results])
                context += f"\n--- 检索结果 {iteration + 1} ---\n{new_context}"

            elif action == AgentAction.REFINE_QUERY:
                current_query = self._refine_query(query, context)

            elif action == AgentAction.GENERATE:
                return self._generate_answer(query, context)

            elif action == AgentAction.STOP:
                return "抱歉，我无法回答这个问题。"

        return self._generate_answer(query, context)

    def _get_embedding(self, text: str) -> list[float]:
        pass

    def _refine_query(self, original_query: str, context: str) -> str:
        prompt = f"""基于原始问题和已有上下文，改写查询以获取更精确的信息。

原始问题：{original_query}
已有上下文摘要：{context[:300]}

改写后的查询："""
        return self.llm_client.call(prompt).strip()

    def _generate_answer(self, query: str, context: str) -> str:
        prompt = f"""基于以下上下文回答用户问题。如果上下文中没有相关信息，请说明。

上下文：
{context}

用户问题：{query}

回答："""
        return self.llm_client.call(prompt)
```

## 四、RAG关键组件详解

### 4.1 文档分块策略

分块是RAG效果的基础，不当的分块会严重损害检索质量。

| 策略 | 优点 | 缺点 | 适用场景 |
|------|------|------|----------|
| 固定长度 | 简单 | 语义截断 | 通用场景 |
| 句子级 | 语义完整 | 块过小 | 短文本 |
| 段落级 | 语义连贯 | 块过大 | 长文档 |
| 语义分块 | 最优质量 | 计算成本高 | 高质量需求 |
| 递归分块 | 灵活 | 需调参 | 生产环境 |

```python
import re
from typing import Generator


def recursive_chunk(text: str, chunk_size: int = 500, chunk_overlap: int = 50,
                    separators: list[str] = None) -> list[str]:
    if separators is None:
        separators = ["\n\n", "\n", "。", "！", "？", ".", " "]

    if len(text) <= chunk_size:
        return [text]

    separator = separators[0]
    remaining_separators = separators[1:]

    if separator in text:
        splits = text.split(separator)
    else:
        if remaining_separators:
            return recursive_chunk(text, chunk_size, chunk_overlap, remaining_separators)
        splits = [text[i:i + chunk_size] for i in range(0, len(text), chunk_size - chunk_overlap)]

    chunks = []
    current_chunk = ""

    for split in splits:
        if len(current_chunk) + len(split) > chunk_size and current_chunk:
            chunks.append(current_chunk.strip())
            overlap_text = current_chunk[-chunk_overlap:] if chunk_overlap > 0 else ""
            current_chunk = overlap_text + split + separator
        else:
            current_chunk += split + separator

    if current_chunk.strip():
        chunks.append(current_chunk.strip())

    return chunks
```

### 4.2 上下文窗口管理

大模型的上下文窗口有限，需要合理管理检索结果的数量和长度。

```python
def manage_context(
    retrieved_docs: list[tuple[str, float]],
    max_tokens: int = 4000,
    tokenizer=None
) -> str:
    context_parts = []
    current_tokens = 0

    for doc, score in retrieved_docs:
        doc_tokens = len(tokenizer.encode(doc)) if tokenizer else len(doc) // 2

        if current_tokens + doc_tokens > max_tokens:
            remaining = max_tokens - current_tokens
            if remaining > 100:
                truncated = doc[:remaining * 2]
                context_parts.append(f"[相似度:{score:.3f}] {truncated}...")
            break

        context_parts.append(f"[相似度:{score:.3f}] {doc}")
        current_tokens += doc_tokens

    return "\n\n".join(context_parts)
```

### 4.3 评估指标

RAG系统需要从检索质量和生成质量两个维度评估。

| 指标 | 含义 | 计算方式 |
|------|------|----------|
| Recall@K | 前K个结果中相关文档的召回率 | 相关文档数 / 总相关文档数 |
| Precision@K | 前K个结果中相关文档的精确率 | 相关文档数 / K |
| MRR | 首个相关文档排名的倒数 | 1 / 首个相关文档排名 |
| Faithfulness | 生成答案与检索上下文的一致性 | LLM评估 |
| Relevancy | 生成答案与用户问题的相关性 | LLM评估 |

## 五、RAG技术选型指南

### 5.1 按场景选择RAG架构

| 场景 | 推荐架构 | 理由 |
|------|----------|------|
| 简单FAQ | Naive RAG | 问题明确，检索简单 |
| 企业知识库 | Advanced RAG | 需要高质量检索和重排 |
| 全局分析 | GraphRAG | 需要跨文档推理 |
| 复杂决策 | Agentic RAG | 需要多步推理和工具调用 |

### 5.2 技术栈推荐

| 组件 | 推荐方案 | 备选方案 |
|------|----------|----------|
| Embedding模型 | text-embedding-v3 / bge-large-zh | gte-Qwen2 |
| 向量数据库 | Milvus (生产) / FAISS (原型) | Qdrant, Chroma |
| 分块策略 | 递归分块 | 语义分块 |
| 重排序 | Cohere Rerank / bge-reranker | Cross-Encoder |
| 框架 | LangChain / LlamaIndex | 自研 |

## 总结

本章系统讲解了RAG技术的原理与演进：

1. **Naive RAG**：基础的三阶段架构（索引-检索-生成），适合简单场景
2. **Advanced RAG**：在检索前中后三个环节优化，显著提升检索质量
3. **GraphRAG**：结合知识图谱，解决全局性问题的推理需求
4. **Agentic RAG**：引入Agent自主决策，实现自适应检索和多步推理
5. **关键组件**：分块策略、上下文管理、评估指标是RAG效果调优的核心

RAG不是一成不变的技术，而是根据场景需求不断演进的架构。下一章将进入RAG实战，通过完整项目将理论落地。
