---
title: "AI 应用开发-007 RAG实践：本地知识库搭建"
date: 2025-05-29T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型", "RAG", "知识库"]
---

## 概述

本章将上一章的RAG理论落地为完整项目，使用DeepSeek + FAISS搭建一个本地知识库问答系统。项目覆盖PDF文本提取、文档分块、向量索引构建、语义搜索与问答链的全流程开发，并介绍GraphRAG的全局-局部搜索机制和Qwen-Agent的多级RAG应用。

## 一、项目架构设计

### 1.1 整体架构

```
PDF文档 → 文本提取 → 文档分块 → Embedding → FAISS索引
                                                    ↓
用户提问 → Query Embedding → 向量检索 → 上下文组装 → LLM生成 → 答案
```

### 1.2 技术栈

| 组件 | 选型 | 说明 |
|------|------|------|
| LLM | DeepSeek-R1 (Ollama) | 本地推理，数据安全 |
| Embedding | text-embedding-v3 | 阿里云DashScope |
| 向量数据库 | FAISS | 轻量高效 |
| PDF解析 | PyMuPDF | 高质量文本提取 |
| 框架 | LangChain | 编排与集成 |

## 二、PDF文本提取

### 2.1 使用PyMuPDF提取文本

```python
import fitz
import logging
from pathlib import Path
from dataclasses import dataclass

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


@dataclass
class DocumentChunk:
    content: str
    source: str
    page: int
    chunk_id: int


class PDFExtractor:
    def __init__(self, chunk_size: int = 500, chunk_overlap: int = 50):
        self.chunk_size = chunk_size
        self.chunk_overlap = chunk_overlap

    def extract_text(self, pdf_path: str) -> list[dict]:
        doc = fitz.open(pdf_path)
        pages = []
        for page_num in range(len(doc)):
            page = doc[page_num]
            text = page.get_text("text")
            if text.strip():
                pages.append({
                    "content": text.strip(),
                    "source": Path(pdf_path).name,
                    "page": page_num + 1
                })
        doc.close()
        logger.info(f"提取完成: {pdf_path}, 共 {len(pages)} 页")
        return pages

    def extract_and_chunk(self, pdf_path: str) -> list[DocumentChunk]:
        pages = self.extract_text(pdf_path)
        all_chunks = []
        chunk_id = 0

        for page_info in pages:
            chunks = self._recursive_chunk(page_info["content"])
            for chunk in chunks:
                all_chunks.append(DocumentChunk(
                    content=chunk,
                    source=page_info["source"],
                    page=page_info["page"],
                    chunk_id=chunk_id
                ))
                chunk_id += 1

        logger.info(f"分块完成: {pdf_path}, 共 {len(all_chunks)} 个块")
        return all_chunks

    def _recursive_chunk(self, text: str) -> list[str]:
        if len(text) <= self.chunk_size:
            return [text]

        separators = ["\n\n", "\n", "。", "！", "？", ".", " "]
        for sep in separators:
            if sep in text:
                parts = text.split(sep)
                chunks = []
                current = ""
                for part in parts:
                    if len(current) + len(part) > self.chunk_size and current:
                        chunks.append(current.strip())
                        overlap = current[-self.chunk_overlap:] if self.chunk_overlap > 0 else ""
                        current = overlap + part + sep
                    else:
                        current += part + sep
                if current.strip():
                    chunks.append(current.strip())
                return chunks

        return [text[i:i + self.chunk_size] for i in range(0, len(text), self.chunk_size - self.chunk_overlap)]


def batch_extract(pdf_dir: str) -> list[DocumentChunk]:
    extractor = PDFExtractor(chunk_size=500, chunk_overlap=50)
    all_chunks = []
    pdf_path = Path(pdf_dir)

    for file in pdf_path.glob("*.pdf"):
        chunks = extractor.extract_and_chunk(str(file))
        all_chunks.extend(chunks)

    logger.info(f"批量提取完成，共 {len(all_chunks)} 个文档块")
    return all_chunks
```

## 三、向量索引构建

### 3.1 Embedding生成与FAISS索引

```python
import dashscope
import numpy as np
import faiss
import json
import pickle
import logging
import os
from typing import Optional

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

dashscope.api_key = os.getenv("DASHSCOPE_API_KEY", "sk-xxx")


class KnowledgeBase:
    def __init__(self, dimension: int = 1024):
        self.dimension = dimension
        self.index = faiss.IndexFlatIP(dimension)
        self.chunks: list[DocumentChunk] = []
        self.chunk_embeddings: list[list[float]] = []

    def _get_embeddings_batch(self, texts: list[str], batch_size: int = 25) -> list[list[float]]:
        all_embeddings = []
        for i in range(0, len(texts), batch_size):
            batch = texts[i:i + batch_size]
            try:
                resp = dashscope.TextEmbedding.call(
                    model="text-embedding-v3",
                    input=batch
                )
                if resp.status_code == 200:
                    batch_embs = [item["embedding"] for item in resp.output["embeddings"]]
                    all_embeddings.extend(batch_embs)
                    logger.info(f"Embedding批次 {i // batch_size + 1} 完成，数量: {len(batch)}")
                else:
                    logger.error(f"Embedding失败: {resp.code} - {resp.message}")
            except Exception as e:
                logger.error(f"Embedding异常: {e}")
        return all_embeddings

    def build_index(self, chunks: list[DocumentChunk]):
        self.chunks = chunks
        texts = [chunk.content for chunk in chunks]

        logger.info(f"开始构建索引，文档块数: {len(texts)}")
        embeddings = self._get_embeddings_batch(texts)

        if len(embeddings) != len(chunks):
            logger.error(f"Embedding数量不匹配: {len(embeddings)} vs {len(chunks)}")
            return

        self.chunk_embeddings = embeddings
        vectors = np.array(embeddings, dtype=np.float32)
        faiss.normalize_L2(vectors)

        self.index = faiss.IndexFlatIP(self.dimension)
        self.index.add(vectors)
        logger.info(f"索引构建完成，总文档块: {self.index.ntotal}")

    def search(self, query: str, top_k: int = 5) -> list[tuple[DocumentChunk, float]]:
        query_emb = self._get_embeddings_batch([query])
        if not query_emb:
            return []

        query_vec = np.array(query_emb, dtype=np.float32)
        faiss.normalize_L2(query_vec)

        scores, indices = self.index.search(query_vec, top_k)

        results = []
        for score, idx in zip(scores[0], indices[0]):
            if 0 <= idx < len(self.chunks):
                results.append((self.chunks[idx], float(score)))
        return results

    def save(self, index_path: str = "kb_index.bin", meta_path: str = "kb_meta.pkl"):
        faiss.write_index(self.index, index_path)
        with open(meta_path, "wb") as f:
            pickle.dump({
                "chunks": self.chunks,
                "dimension": self.dimension
            }, f)
        logger.info(f"知识库已保存: {index_path}, {meta_path}")

    def load(self, index_path: str = "kb_index.bin", meta_path: str = "kb_meta.pkl"):
        self.index = faiss.read_index(index_path)
        with open(meta_path, "rb") as f:
            meta = pickle.load(f)
            self.chunks = meta["chunks"]
            self.dimension = meta["dimension"]
        logger.info(f"知识库已加载，文档块数: {self.index.ntotal}")
```

## 四、问答链实现

### 4.1 基于DeepSeek的问答链

```python
import requests
import json
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

OLLAMA_API = "http://localhost:11434/api/chat"


class RAGQAClient:
    def __init__(self, knowledge_base: KnowledgeBase, model: str = "deepseek-r1:7b"):
        self.kb = knowledge_base
        self.model = model

    def _call_llm(self, messages: list[dict]) -> str:
        payload = {
            "model": self.model,
            "messages": messages,
            "stream": False
        }
        try:
            response = requests.post(OLLAMA_API, json=payload, timeout=120)
            response.raise_for_status()
            result = response.json()
            return result["message"]["content"]
        except requests.RequestException as e:
            logger.error(f"LLM调用失败: {e}")
            return ""

    def ask(self, question: str, top_k: int = 5) -> dict:
        search_results = self.kb.search(question, top_k=top_k)

        if not search_results:
            return {
                "question": question,
                "answer": "抱歉，在知识库中未找到相关信息。",
                "sources": []
            }

        context_parts = []
        sources = []
        for chunk, score in search_results:
            context_parts.append(f"[来源: {chunk.source} 第{chunk.page}页, 相关度: {score:.3f}]\n{chunk.content}")
            sources.append({
                "source": chunk.source,
                "page": chunk.page,
                "score": score
            })

        context = "\n\n---\n\n".join(context_parts)

        system_prompt = """你是一个专业的知识库问答助手。请基于提供的上下文信息回答用户问题。

要求：
1. 只基于上下文中的信息回答，不要编造内容
2. 如果上下文中没有相关信息，请明确说明
3. 回答要准确、完整、有条理
4. 引用信息来源"""

        messages = [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": f"上下文信息：\n{context}\n\n用户问题：{question}"}
        ]

        answer = self._call_llm(messages)

        return {
            "question": question,
            "answer": answer,
            "sources": sources
        }


if __name__ == "__main__":
    chunks = batch_extract("./pdfs")
    kb = KnowledgeBase()
    kb.build_index(chunks)
    kb.save()

    qa = RAGQAClient(kb)

    questions = [
        "什么是机器学习？",
        "深度学习和机器学习有什么区别？",
        "如何评估模型性能？"
    ]

    for q in questions:
        result = qa.ask(q)
        print(f"\n问题: {result['question']}")
        print(f"回答: {result['answer'][:200]}...")
        print(f"来源: {len(result['sources'])} 个文档块")
```

## 五、GraphRAG全局-局部搜索

### 5.1 GraphRAG在知识库中的应用

传统RAG擅长回答具体问题，但面对"总结所有文档的核心观点"这类全局性问题时效果不佳。GraphRAG通过构建知识图谱，实现双层搜索。

```python
from dataclasses import dataclass, field
from collections import defaultdict
import json
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


@dataclass
class KnowledgeEntity:
    name: str
    entity_type: str
    description: str
    source_docs: list[str] = field(default_factory=list)


@dataclass
class KnowledgeRelation:
    source_entity: str
    target_entity: str
    relation: str
    description: str


@dataclass
class CommunitySummary:
    community_id: str
    entities: list[str]
    summary: str
    level: int


class GraphRAGBuilder:
    def __init__(self, llm_call_fn):
        self.llm_call = llm_call_fn
        self.entities: list[KnowledgeEntity] = []
        self.relations: list[KnowledgeRelation] = []
        self.communities: list[CommunitySummary] = []
        self.entity_index: dict[str, list[str]] = defaultdict(list)

    def extract_from_chunk(self, chunk: DocumentChunk) -> None:
        prompt = f"""从以下文本中提取实体和关系，以JSON格式输出。

文本：{chunk.content}

输出格式：
{{
    "entities": [{{"name": "名称", "type": "类型", "description": "描述"}}],
    "relations": [{{"source": "源实体", "target": "目标实体", "relation": "关系", "description": "描述"}}]
}}"""

        response = self.llm_call([{"role": "user", "content": prompt}])
        try:
            json_str = response.strip()
            if "```json" in json_str:
                json_str = json_str.split("```json")[1].split("```")[0].strip()
            data = json.loads(json_str)

            for e in data.get("entities", []):
                entity = KnowledgeEntity(
                    name=e["name"],
                    entity_type=e["type"],
                    description=e["description"],
                    source_docs=[chunk.source]
                )
                self.entities.append(entity)
                self.entity_index[e["name"]].append(chunk.source)

            for r in data.get("relations", []):
                relation = KnowledgeRelation(
                    source_entity=r["source"],
                    target_entity=r["target"],
                    relation=r["relation"],
                    description=r["description"]
                )
                self.relations.append(relation)

        except (json.JSONDecodeError, KeyError) as e:
            logger.warning(f"实体提取失败: {e}")

    def build_communities(self) -> None:
        adjacency = defaultdict(set)
        for r in self.relations:
            adjacency[r.source_entity].add(r.target_entity)
            adjacency[r.target_entity].add(r.source_entity)

        for e in self.entities:
            if e.name not in adjacency:
                adjacency[e.name] = set()

        visited = set()
        community_id = 0

        for entity_name in adjacency:
            if entity_name not in visited:
                component = self._bfs(entity_name, adjacency, visited)
                entity_descs = []
                for name in component:
                    matching = [e for e in self.entities if e.name == name]
                    if matching:
                        entity_descs.append(f"- {name}({matching[0].entity_type}): {matching[0].description}")

                summary = self._generate_community_summary(component, entity_descs)
                self.communities.append(CommunitySummary(
                    community_id=f"c_{community_id}",
                    entities=component,
                    summary=summary,
                    level=0
                ))
                community_id += 1

        logger.info(f"社区构建完成，共 {len(self.communities)} 个社区")

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

    def _generate_community_summary(self, entities: list[str], descriptions: list[str]) -> str:
        desc_text = "\n".join(descriptions[:20])
        prompt = f"""请为以下一组相关实体生成简洁的社区摘要：

实体列表：
{desc_text}

社区摘要："""
        return self.llm_call([{"role": "user", "content": prompt}])

    def global_search(self, query: str) -> str:
        community_contexts = []
        for c in self.communities:
            community_contexts.append(f"社区 {c.community_id}:\n{c.summary}")

        context = "\n\n".join(community_contexts[:10])
        prompt = f"""基于以下社区摘要回答全局性问题。

社区摘要：
{context}

问题：{query}

请综合所有社区信息给出全面回答："""
        return self.llm_call([{"role": "user", "content": prompt}])

    def local_search(self, query: str, vector_store: KnowledgeBase, top_k: int = 5) -> str:
        results = vector_store.search(query, top_k=top_k)
        context = "\n\n".join([chunk.content for chunk, _ in results])
        prompt = f"""基于以下文档片段回答问题。

文档内容：
{context}

问题：{query}

回答："""
        return self.llm_call([{"role": "user", "content": prompt}])
```

## 六、Qwen-Agent多级RAG

### 6.1 多级RAG架构

Qwen-Agent提供了多级RAG能力，支持从简单检索到复杂推理的渐进式检索策略。

```python
import dashscope
import logging
import os

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

dashscope.api_key = os.getenv("DASHSCOPE_API_KEY", "sk-xxx")


class MultiLevelRAG:
    def __init__(self, knowledge_base: KnowledgeBase):
        self.kb = knowledge_base

    def _call_qwen(self, messages: list[dict], model: str = "qwen-turbo") -> str:
        try:
            response = dashscope.Generation.call(
                model=model,
                messages=messages,
                result_format="message"
            )
            if response.status_code == 200:
                return response.output.choices[0].message.content
            return ""
        except Exception as e:
            logger.error(f"Qwen调用失败: {e}")
            return ""

    def level1_direct_search(self, query: str, top_k: int = 3) -> str:
        results = self.kb.search(query, top_k=top_k)
        context = "\n".join([chunk.content for chunk, _ in results])
        messages = [
            {"role": "system", "content": "基于上下文回答问题，不要编造信息。"},
            {"role": "user", "content": f"上下文：{context}\n\n问题：{query}"}
        ]
        return self._call_qwen(messages)

    def level2_query_rewrite_search(self, query: str) -> str:
        rewrite_messages = [
            {"role": "user", "content": f"将以下问题改写为更适合检索的查询：\n{query}"}
        ]
        rewritten = self._call_qwen(rewrite_messages)
        logger.info(f"查询改写: {query} → {rewritten}")

        results = self.kb.search(rewritten, top_k=5)
        context = "\n".join([chunk.content for chunk, _ in results])
        messages = [
            {"role": "system", "content": "基于上下文回答问题，不要编造信息。"},
            {"role": "user", "content": f"上下文：{context}\n\n原始问题：{query}"}
        ]
        return self._call_qwen(messages)

    def level3_multi_step_search(self, query: str) -> str:
        decompose_messages = [
            {"role": "user", "content": f"将以下复杂问题拆解为2-3个子问题：\n{query}"}
        ]
        sub_questions = self._call_qwen(decompose_messages)
        logger.info(f"问题拆解: {sub_questions}")

        all_contexts = []
        for line in sub_questions.strip().split("\n"):
            line = line.strip()
            if line and (line[0].isdigit() or line.startswith("-")):
                sub_q = line.lstrip("0123456789.-) ")
                results = self.kb.search(sub_q, top_k=2)
                for chunk, score in results:
                    all_contexts.append(f"[相关度:{score:.3f}] {chunk.content}")

        context = "\n\n".join(all_contexts[:10])
        messages = [
            {"role": "system", "content": "基于上下文综合回答问题，不要编造信息。"},
            {"role": "user", "content": f"上下文：{context}\n\n问题：{query}"}
        ]
        return self._call_qwen(messages)

    def smart_search(self, query: str) -> str:
        classify_messages = [
            {"role": "user", "content": f"判断以下问题的复杂度，只输出 simple/medium/complex：\n{query}"}
        ]
        complexity = self._call_qwen(classify_messages).strip().lower()
        logger.info(f"问题复杂度: {complexity}")

        if complexity == "simple":
            return self.level1_direct_search(query)
        elif complexity == "medium":
            return self.level2_query_rewrite_search(query)
        else:
            return self.level3_multi_step_search(query)
```

## 七、RAG效果优化实践

### 7.1 常见问题与优化方向

| 问题 | 原因 | 优化方案 |
|------|------|----------|
| 检索不到相关文档 | 分块过大/查询不精确 | 优化分块策略 + 查询改写 |
| 回答包含无关信息 | 检索结果噪声多 | 增加重排序 + 上下文压缩 |
| 回答编造信息 | LLM幻觉 | 强化Prompt约束 + 引用溯源 |
| 回答不完整 | 检索结果不够全面 | 混合检索 + 查询扩展 |
| 响应慢 | 多次API调用 | 缓存 + 批处理 |

### 7.2 Prompt优化模板

```python
RAG_SYSTEM_PROMPT = """你是一个严谨的知识库问答助手。请严格遵循以下规则：

1. 只基于提供的上下文信息回答，不得使用外部知识
2. 如果上下文中没有相关信息，回答"根据现有知识库，我无法回答这个问题"
3. 回答时标注信息来源（文档名和页码）
4. 对于数值类问题，精确引用原文数据
5. 对于概念类问题，综合多个相关片段给出完整解释

回答格式：
- 直接回答问题
- 列出关键依据
- 标注信息来源"""
```

## 总结

本章通过完整项目实践了RAG技术的落地：

1. **PDF文本提取**：使用PyMuPDF高质量提取文本，递归分块保证语义完整
2. **向量索引构建**：FAISS + DashScope Embedding构建高效语义检索
3. **问答链实现**：DeepSeek本地推理 + 上下文注入，实现安全可控的知识问答
4. **GraphRAG**：知识图谱 + 社区摘要，解决全局性问题的推理需求
5. **多级RAG**：根据问题复杂度自适应选择检索策略，平衡效果与效率

核心经验：RAG的效果取决于"检索质量"和"生成质量"两个维度，检索是基础，生成是关键，两者需要协同优化。
