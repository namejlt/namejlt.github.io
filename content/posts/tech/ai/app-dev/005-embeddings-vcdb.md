---
title: "AI 应用开发-005 Embeddings与向量数据库"
date: 2025-05-27T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型", "embedding", "向量数据库"]
---

## 概述

Embeddings（嵌入）是连接非结构化数据与AI计算的桥梁，向量数据库则是存储和检索这些嵌入的引擎。两者结合，构成了现代AI应用中语义搜索、推荐系统、RAG等核心能力的技术基石。

本章将从词向量的基本概念出发，逐步深入到现代Embedding模型、向量数据库原理与实战，最终通过酒店推荐系统和文本抄袭检测两个项目，帮助读者掌握Embeddings与向量数据库的完整技术栈。

## 一、从词向量到文本嵌入

### 1.1 为什么需要Embedding

计算机只能处理数值，而人类语言是符号化的。Embedding的核心目标就是将语言符号映射为稠密的数值向量，使得语义相近的词在向量空间中距离也相近。

```
"猫" → [0.23, -0.15, 0.87, ...]  (768维)
"狗" → [0.21, -0.13, 0.85, ...]  (语义相近，向量相近)
"汽车" → [0.78, 0.42, -0.31, ...] (语义不同，向量距离远)
```

### 1.2 词向量发展历程

#### N-Gram语言模型

N-Gram是最早的统计语言模型，基于"一个词的出现概率只与前面N-1个词相关"的假设。

```python
from collections import Counter, defaultdict
import math


class BigramModel:
    def __init__(self):
        self.unigram_counts = Counter()
        self.bigram_counts = Counter()
        self.vocab_size = 0

    def train(self, sentences: list[str]):
        for sentence in sentences:
            tokens = ["<s>"] + sentence.split() + ["</s>"]
            self.unigram_counts.update(tokens)
            for i in range(len(tokens) - 1):
                self.bigram_counts[(tokens[i], tokens[i + 1])] += 1
        self.vocab_size = len(self.unigram_counts)

    def probability(self, word: str, context: str) -> float:
        bigram_count = self.bigram_counts.get((context, word), 0)
        unigram_count = self.unigram_counts.get(context, 0)
        if unigram_count == 0:
            return 1.0 / self.vocab_size
        return (bigram_count + 1) / (unigram_count + self.vocab_size)

    def sentence_probability(self, sentence: str) -> float:
        tokens = ["<s>"] + sentence.split() + ["</s>"]
        log_prob = 0.0
        for i in range(1, len(tokens)):
            prob = self.probability(tokens[i], tokens[i - 1])
            log_prob += math.log(prob)
        return math.exp(log_prob)


if __name__ == "__main__":
    corpus = [
        "我 喜欢 机器 学习",
        "我 喜欢 深度 学习",
        "他 喜欢 自然 语言 处理"
    ]
    model = BigramModel()
    model.train(corpus)
    print(f"P('学习'|'喜欢') = {model.probability('学习', '喜欢'):.4f}")
    print(f"句子概率: '我 喜欢 机器 学习' = {model.sentence_probability('我 喜欢 机器 学习'):.6f}")
```

#### Word2Vec

Word2Vec通过预测上下文词（Skip-gram）或由上下文预测中心词（CBOW）来学习词向量，首次实现了大规模词向量的高效训练。

```python
from gensim.models import Word2Vec
import logging

logging.basicConfig(level=logging.INFO)


def train_word2vec(sentences: list[list[str]]) -> Word2Vec:
    model = Word2Vec(
        sentences=sentences,
        vector_size=128,
        window=5,
        min_count=1,
        workers=4,
        epochs=50,
        sg=1
    )
    logging.info(f"词表大小: {len(model.wv)}")
    return model


def find_similar_words(model: Word2Vec, word: str, topn: int = 5):
    try:
        similar = model.wv.most_similar(word, topn=topn)
        for w, score in similar:
            print(f"  {w}: {score:.4f}")
        return similar
    except KeyError:
        logging.warning(f"词 '{word}' 不在词表中")
        return []


if __name__ == "__main__":
    corpus = [
        ["机器", "学习", "是", "人工智能", "的", "核心"],
        ["深度", "学习", "是", "机器", "学习", "的", "分支"],
        ["自然", "语言", "处理", "使用", "深度", "学习"],
        ["计算机", "视觉", "也", "使用", "深度", "学习"],
        ["人工智能", "改变", "了", "世界"],
    ]

    model = train_word2vec(corpus)
    print("与'学习'最相似的词:")
    find_similar_words(model, "学习")
    print("与'人工智能'最相似的词:")
    find_similar_words(model, "人工智能")
```

### 1.3 现代Embedding模型

从Word2Vec到Transformer时代的Embedding模型，核心变化是从静态词向量到动态上下文向量。

| 模型 | 年代 | 维度 | 特点 |
|------|------|------|------|
| Word2Vec | 2013 | 100-300 | 静态词向量，一词一向量 |
| GloVe | 2014 | 100-300 | 全局统计信息+局部上下文 |
| BERT | 2018 | 768-1024 | 上下文相关，动态向量 |
| text-embedding-ada-002 | 2023 | 1536 | OpenAI专用嵌入模型 |
| bge-large-zh-v1.5 | 2024 | 1024 | 中文最佳开源嵌入模型 |
| gte-Qwen2-1.5B-instruct | 2025 | 1536 | 阿里最新指令感知嵌入模型 |

#### 使用DashScope Embedding API

```python
import dashscope
import numpy as np
import logging
import os

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

dashscope.api_key = os.getenv("DASHSCOPE_API_KEY", "sk-xxx")


def get_embeddings(texts: list[str], model: str = "text-embedding-v3") -> list[list[float]]:
    try:
        resp = dashscope.TextEmbedding.call(
            model=model,
            input=texts
        )
        if resp.status_code == 200:
            embeddings = [item["embedding"] for item in resp.output["embeddings"]]
            logger.info(f"生成嵌入成功，数量: {len(embeddings)}, 维度: {len(embeddings[0])}")
            return embeddings
        else:
            logger.error(f"嵌入生成失败: {resp.code} - {resp.message}")
            return []
    except Exception as e:
        logger.error(f"调用嵌入API异常: {e}")
        return []


def cosine_similarity(vec_a: list[float], vec_b: list[float]) -> float:
    a = np.array(vec_a)
    b = np.array(vec_b)
    return float(np.dot(a, b) / (np.linalg.norm(a) * np.linalg.norm(b)))


if __name__ == "__main__":
    texts = [
        "机器学习是人工智能的核心技术",
        "深度学习是机器学习的重要分支",
        "今天天气真好，适合出去玩"
    ]

    embeddings = get_embeddings(texts)

    if embeddings:
        sim_01 = cosine_similarity(embeddings[0], embeddings[1])
        sim_02 = cosine_similarity(embeddings[0], embeddings[2])
        print(f"'机器学习...' vs '深度学习...' 相似度: {sim_01:.4f}")
        print(f"'机器学习...' vs '今天天气...' 相似度: {sim_02:.4f}")
```

## 二、向量数据库

### 2.1 为什么需要向量数据库

传统数据库基于精确匹配（B-Tree索引），无法处理语义相似性查询。向量数据库专为高维向量的近似最近邻搜索（ANN）设计，是AI应用的基础设施。

| 特性 | 传统数据库 | 向量数据库 |
|------|-----------|-----------|
| 查询方式 | 精确匹配 | 语义相似性 |
| 索引结构 | B-Tree | HNSW / IVF |
| 适用场景 | 关系型数据 | 非结构化数据检索 |
| 代表产品 | MySQL, PostgreSQL | FAISS, Milvus, Qdrant |

### 2.2 FAISS实战

FAISS是Meta开源的向量检索库，适合单机场景，性能极高。

```python
import numpy as np
import faiss
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class FAISSVectorStore:
    def __init__(self, dimension: int = 1024):
        self.dimension = dimension
        self.index = faiss.IndexFlatIP(dimension)
        self.documents = []

    def add_documents(self, embeddings: list[list[float]], documents: list[str]):
        vectors = np.array(embeddings, dtype=np.float32)
        faiss.normalize_L2(vectors)
        self.index.add(vectors)
        self.documents.extend(documents)
        logger.info(f"添加文档完成，总数: {self.index.ntotal}")

    def search(self, query_embedding: list[float], top_k: int = 5) -> list[tuple[str, float]]:
        query_vector = np.array([query_embedding], dtype=np.float32)
        faiss.normalize_L2(query_vector)
        scores, indices = self.index.search(query_vector, top_k)

        results = []
        for score, idx in zip(scores[0], indices[0]):
            if idx < len(self.documents) and idx >= 0:
                results.append((self.documents[idx], float(score)))
        return results

    def save(self, path: str = "faiss_index.bin"):
        faiss.write_index(self.index, path)
        logger.info(f"索引已保存: {path}")

    def load(self, path: str = "faiss_index.bin"):
        self.index = faiss.read_index(path)
        logger.info(f"索引已加载，文档数: {self.index.ntotal}")


if __name__ == "__main__":
    np.random.seed(42)
    dim = 128
    num_docs = 1000

    store = FAISSVectorStore(dimension=dim)

    fake_embeddings = np.random.randn(num_docs, dim).tolist()
    fake_docs = [f"文档_{i}: 这是第{i}篇关于AI技术的文章" for i in range(num_docs)]

    store.add_documents(fake_embeddings, fake_docs)

    query = np.random.randn(dim).tolist()
    results = store.search(query, top_k=5)

    print("搜索结果:")
    for doc, score in results:
        print(f"  相似度: {score:.4f} | {doc}")
```

### 2.3 Milvus实战

Milvus是分布式向量数据库，适合大规模生产环境。

```python
from pymilvus import MilvusClient, DataType
import numpy as np
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class MilvusVectorStore:
    def __init__(self, uri: str = "milvus_demo.db", collection_name: str = "documents"):
        self.client = MilvusClient(uri=uri)
        self.collection_name = collection_name
        self.dimension = 1024

    def create_collection(self):
        if self.client.has_collection(self.collection_name):
            self.client.drop_collection(self.collection_name)

        schema = self.client.create_schema(auto_id=True, enable_dynamic_field=True)
        schema.add_field(field_name="id", datatype=DataType.INT64, is_primary=True)
        schema.add_field(field_name="vector", datatype=DataType.FLOAT_VECTOR, dim=self.dimension)
        schema.add_field(field_name="text", datatype=DataType.VARCHAR, max_length=2048)
        schema.add_field(field_name="source", datatype=DataType.VARCHAR, max_length=256)

        index_params = self.client.prepare_index_params()
        index_params.add_index(
            field_name="vector",
            index_type="IVF_FLAT",
            metric_type="COSINE",
            params={"nlist": 128}
        )
        index_params.add_index(field_name="id", index_type="STL_SORT")

        self.client.create_collection(
            collection_name=self.collection_name,
            schema=schema,
            index_params=index_params
        )
        logger.info(f"集合 '{self.collection_name}' 创建成功")

    def insert_documents(self, embeddings: list[list[float]], texts: list[str], sources: list[str]):
        data = []
        for emb, text, source in zip(embeddings, texts, sources):
            data.append({
                "vector": emb,
                "text": text,
                "source": source
            })
        self.client.insert(collection_name=self.collection_name, data=data)
        logger.info(f"插入 {len(data)} 条文档")

    def search(self, query_embedding: list[float], top_k: int = 5) -> list[dict]:
        results = self.client.search(
            collection_name=self.collection_name,
            data=[query_embedding],
            limit=top_k,
            output_fields=["text", "source"]
        )
        return results[0] if results else []


if __name__ == "__main__":
    store = MilvusVectorStore()
    store.create_collection()

    np.random.seed(42)
    fake_embeddings = np.random.randn(100, 1024).tolist()
    fake_texts = [f"这是第{i}篇技术文档" for i in range(100)]
    fake_sources = [f"doc_{i}.pdf" for i in range(100)]

    store.insert_documents(fake_embeddings, fake_texts, fake_sources)

    query = np.random.randn(1024).tolist()
    results = store.search(query, top_k=3)
    for hit in results:
        print(f"  相似度: {hit['distance']:.4f} | 文本: {hit['entity']['text']}")
```

### 2.4 向量数据库选型对比

| 特性 | FAISS | Milvus | Qdrant | Chroma |
|------|-------|--------|--------|--------|
| 部署方式 | 嵌入式 | 分布式 | 分布式/嵌入式 | 嵌入式 |
| 规模 | 百万级 | 十亿级 | 亿级 | 十万级 |
| 持久化 | 手动 | 自动 | 自动 | 自动 |
| 过滤 | 不支持 | 支持 | 支持 | 支持 |
| 适用场景 | 原型验证 | 生产环境 | 中大规模 | 快速原型 |

## 三、实战项目：酒店推荐系统

### 3.1 项目架构

```
用户输入偏好 → Embedding模型编码 → 向量数据库检索 → 返回Top-K酒店
```

### 3.2 完整实现

```python
import dashscope
import numpy as np
import faiss
import json
import logging
import os

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

dashscope.api_key = os.getenv("DASHSCOPE_API_KEY", "sk-xxx")


HOTELS = [
    {"id": 1, "name": "海景度假酒店", "desc": "位于三亚湾，拥有私人沙滩和无边泳池，适合家庭度假", "price": 899, "tags": "海景 度假 家庭 泳池"},
    {"id": 2, "name": "商务精选酒店", "desc": "位于CBD核心区，高速WiFi和会议室，适合商务出差", "price": 599, "tags": "商务 会议 WiFi 市中心"},
    {"id": 3, "name": "山间温泉民宿", "desc": "隐于山间，天然温泉和有机餐饮，适合周末放松", "price": 459, "tags": "温泉 山景 民宿 放松"},
    {"id": 4, "name": "古城文化客栈", "desc": "位于丽江古城内，纳西族风格装修，体验当地文化", "price": 389, "tags": "古城 文化 客栈 特色"},
    {"id": 5, "name": "亲子主题酒店", "desc": "儿童乐园和亲子活动，专属儿童泳池和餐饮", "price": 699, "tags": "亲子 儿童 乐园 家庭"},
    {"id": 6, "name": "设计师精品酒店", "desc": "现代艺术风格，每间房由不同设计师打造，适合文艺青年", "price": 559, "tags": "设计 艺术 文艺 精品"},
]


def get_embeddings(texts: list[str]) -> list[list[float]]:
    resp = dashscope.TextEmbedding.call(
        model="text-embedding-v3",
        input=texts
    )
    if resp.status_code == 200:
        return [item["embedding"] for item in resp.output["embeddings"]]
    return []


class HotelRecommender:
    def __init__(self):
        self.index = None
        self.hotels = []

    def build_index(self):
        texts = [f"{h['name']} {h['desc']} {h['tags']}" for h in HOTELS]
        embeddings = get_embeddings(texts)
        if not embeddings:
            logger.error("构建索引失败：无法获取嵌入")
            return

        vectors = np.array(embeddings, dtype=np.float32)
        faiss.normalize_L2(vectors)

        self.index = faiss.IndexFlatIP(vectors.shape[1])
        self.index.add(vectors)
        self.hotels = HOTELS
        logger.info(f"酒店索引构建完成，共 {len(self.hotels)} 家酒店")

    def recommend(self, query: str, top_k: int = 3) -> list[dict]:
        query_emb = get_embeddings([query])
        if not query_emb or not self.index:
            return []

        query_vec = np.array(query_emb, dtype=np.float32)
        faiss.normalize_L2(query_vec)

        scores, indices = self.index.search(query_vec, top_k)

        results = []
        for score, idx in zip(scores[0], indices[0]):
            if 0 <= idx < len(self.hotels):
                hotel = self.hotels[idx].copy()
                hotel["similarity"] = float(score)
                results.append(hotel)
        return results


if __name__ == "__main__":
    recommender = HotelRecommender()
    recommender.build_index()

    queries = ["适合带孩子玩的酒店", "出差住哪里方便", "想泡温泉放松一下"]
    for q in queries:
        print(f"\n查询: {q}")
        results = recommender.recommend(q)
        for r in results:
            print(f"  [{r['similarity']:.4f}] {r['name']} - ¥{r['price']} | {r['desc']}")
```

## 四、实战项目：文本抄袭检测

### 4.1 项目思路

将待检测文档分句后生成Embedding，通过余弦相似度计算句子级别的语义相似性，识别潜在的抄袭片段。

### 4.2 完整实现

```python
import dashscope
import numpy as np
import logging
import os
from dataclasses import dataclass

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

dashscope.api_key = os.getenv("DASHSCOPE_API_KEY", "sk-xxx")


@dataclass
class PlagiarismMatch:
    source_sentence: str
    target_sentence: str
    similarity: float


def get_embeddings(texts: list[str]) -> list[list[float]]:
    resp = dashscope.TextEmbedding.call(
        model="text-embedding-v3",
        input=texts
    )
    if resp.status_code == 200:
        return [item["embedding"] for item in resp.output["embeddings"]]
    return []


def split_sentences(text: str) -> list[str]:
    import re
    sentences = re.split(r'[。！？；\n]', text)
    return [s.strip() for s in sentences if len(s.strip()) > 5]


class PlagiarismDetector:
    def __init__(self, threshold: float = 0.85):
        self.threshold = threshold

    def detect(self, source_text: str, target_text: str) -> list[PlagiarismMatch]:
        source_sentences = split_sentences(source_text)
        target_sentences = split_sentences(target_text)

        if not source_sentences or not target_sentences:
            return []

        all_sentences = source_sentences + target_sentences
        embeddings = get_embeddings(all_sentences)

        if not embeddings:
            return []

        source_embs = np.array(embeddings[:len(source_sentences)], dtype=np.float32)
        target_embs = np.array(embeddings[len(source_sentences):], dtype=np.float32)

        source_embs = source_embs / np.linalg.norm(source_embs, axis=1, keepdims=True)
        target_embs = target_embs / np.linalg.norm(target_embs, axis=1, keepdims=True)

        similarity_matrix = np.dot(source_embs, target_embs.T)

        matches = []
        for i in range(len(source_sentences)):
            for j in range(len(target_sentences)):
                sim = float(similarity_matrix[i][j])
                if sim >= self.threshold:
                    matches.append(PlagiarismMatch(
                        source_sentence=source_sentences[i],
                        target_sentence=target_sentences[j],
                        similarity=sim
                    ))

        matches.sort(key=lambda x: x.similarity, reverse=True)
        return matches


if __name__ == "__main__":
    source = """
    机器学习是人工智能的一个分支，它使计算机能够从数据中学习而无需显式编程。
    深度学习是机器学习的一种方法，使用多层神经网络来建模数据中的复杂模式。
    自然语言处理是人工智能的重要应用领域，旨在让计算机理解和生成人类语言。
    """

    target = """
    机器学习属于人工智能领域，让计算机通过数据自动学习而不需要手动编写规则。
    深度学习利用多层神经网络来捕捉数据中的复杂关系，是机器学习的重要方法。
    今天天气不错，适合出去散步和运动。
    """

    detector = PlagiarismDetector(threshold=0.80)
    results = detector.detect(source, target)

    print("抄袭检测结果:")
    if results:
        for match in results:
            print(f"\n  相似度: {match.similarity:.4f}")
            print(f"  原文: {match.source_sentence}")
            print(f"  对比: {match.target_sentence}")
    else:
        print("  未检测到抄袭")
```

## 五、余弦相似度深入理解

余弦相似度是Embedding检索中最常用的度量方式，衡量两个向量方向的相似性，而非大小。

```python
import numpy as np


def cosine_similarity(a: np.ndarray, b: np.ndarray) -> float:
    return float(np.dot(a, b) / (np.linalg.norm(a) * np.linalg.norm(b)))


def euclidean_distance(a: np.ndarray, b: np.ndarray) -> float:
    return float(np.linalg.norm(a - b))


def dot_product(a: np.ndarray, b: np.ndarray) -> float:
    return float(np.dot(a, b))


if __name__ == "__main__":
    vec_a = np.array([1.0, 2.0, 3.0])
    vec_b = np.array([1.0, 2.0, 3.0])
    vec_c = np.array([-1.0, -2.0, -3.0])
    vec_d = np.array([2.0, 4.0, 6.0])

    print("相同向量:")
    print(f"  余弦相似度: {cosine_similarity(vec_a, vec_b):.4f}")
    print("相反向量:")
    print(f"  余弦相似度: {cosine_similarity(vec_a, vec_c):.4f}")
    print("同方向不同大小:")
    print(f"  余弦相似度: {cosine_similarity(vec_a, vec_d):.4f}")
    print(f"  欧氏距离: {euclidean_distance(vec_a, vec_d):.4f}")
```

关键结论：
- 余弦相似度范围[-1, 1]，1表示完全相同方向
- 对向量归一化后，余弦相似度等价于点积
- FAISS中使用`IndexFlatIP`（内积）+ 归一化 = 余弦相似度检索

## 总结

本章系统介绍了Embeddings与向量数据库的核心技术：

1. **词向量演进**：从N-Gram到Word2Vec再到现代Transformer Embedding，理解嵌入的本质是将语义映射到向量空间
2. **向量数据库**：FAISS适合快速原型，Milvus适合生产环境，选型需根据数据规模和功能需求
3. **实战项目**：酒店推荐系统展示了语义检索的完整流程，抄袭检测展示了句子级相似度计算
4. **核心算法**：余弦相似度是语义检索的基石，理解其原理对调优检索效果至关重要

下一章将基于本章的Embedding和向量数据库基础，深入RAG（检索增强生成）技术，解决大模型"知识过时"的核心问题。
