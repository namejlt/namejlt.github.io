---
title: "AI 应用开发-013 多模型协作与编排"
date: 2025-06-04T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型", "多模型", "模型编排", "MoE"]
---

## 概述

单一模型难以胜任所有任务，多模型协作与编排是解决复杂AI应用的关键策略。通过路由分发、级联增强、混合专家（MoE）等模式，让不同模型各司其职，发挥各自优势。本章将深入讲解多模型协作的架构模式、编排策略和实战应用。

## 一、为什么需要多模型协作

### 1.1 单模型的局限

| 局限 | 表现 | 多模型方案 |
|------|------|-----------|
| 能力边界 | 通用模型专业能力不足 | 路由到专业模型 |
| 成本效率 | 所有请求用最强模型太贵 | 简单问题用小模型 |
| 延迟 | 大模型推理慢 | 简单场景用快速模型 |
| 可靠性 | 单点故障 | 多模型互备 |
| 合规 | 数据不能出境 | 本地模型处理敏感数据 |

### 1.2 主流大模型能力对比

| 模型 | 参数量 | 优势 | 适用场景 |
|------|--------|------|----------|
| GPT-4o | - | 综合能力最强 | 复杂推理、多模态 |
| Claude 3.5 | - | 长文本、代码 | 文档分析、编程 |
| Qwen3 | 235B-MoE | 中文优秀、开源 | 中文应用 |
| DeepSeek-R1 | 671B-MoE | 推理链、开源 | 数学推理、代码 |
| Llama 4 | 400B+ | 开源生态 | 英文场景 |
| Qwen-Turbo | - | 快速、便宜 | 简单对话、分类 |

## 二、多模型协作架构

### 2.1 路由模式

根据请求特征将任务路由到最合适的模型。

```python
import dashscope
import logging
import os
import time
from enum import Enum

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

dashscope.api_key = os.getenv("DASHSCOPE_API_KEY", "sk-xxx")


class TaskComplexity(Enum):
    SIMPLE = "simple"
    MEDIUM = "medium"
    COMPLEX = "complex"


class ModelRouter:
    def __init__(self):
        self.models = {
            TaskComplexity.SIMPLE: "qwen-turbo",
            TaskComplexity.MEDIUM: "qwen-plus",
            TaskComplexity.COMPLEX: "qwen-max"
        }

    def classify_complexity(self, query: str) -> TaskComplexity:
        complex_keywords = ["分析", "推理", "比较", "评估", "设计", "架构", "优化"]
        medium_keywords = ["解释", "总结", "翻译", "改写", "提取"]

        if any(kw in query for kw in complex_keywords):
            return TaskComplexity.COMPLEX
        elif any(kw in query for kw in medium_keywords):
            return TaskComplexity.MEDIUM
        else:
            return TaskComplexity.SIMPLE

    def route(self, query: str) -> str:
        complexity = self.classify_complexity(query)
        model = self.models[complexity]
        logger.info(f"路由: {query[:30]}... → {model} (复杂度: {complexity.value})")
        return model


class MultiModelClient:
    def __init__(self):
        self.router = ModelRouter()

    def chat(self, query: str) -> dict:
        model = self.router.route(query)
        start_time = time.time()

        try:
            response = dashscope.Generation.call(
                model=model,
                messages=[{"role": "user", "content": query}],
                result_format="message"
            )
            elapsed = time.time() - start_time

            if response.status_code == 200:
                return {
                    "query": query,
                    "model": model,
                    "answer": response.output.choices[0].message.content,
                    "latency": round(elapsed, 2),
                    "tokens": response.usage.total_tokens if response.usage else 0
                }
        except Exception as e:
            logger.error(f"模型调用失败: {e}")

        return {"query": query, "model": model, "answer": "服务不可用", "latency": 0, "tokens": 0}


if __name__ == "__main__":
    client = MultiModelClient()

    queries = [
        "你好",
        "请解释什么是RAG技术",
        "请分析RAG和微调在知识密集型应用中的优劣，并给出架构设计建议"
    ]

    for q in queries:
        result = client.chat(q)
        print(f"问题: {q}")
        print(f"模型: {result['model']}, 延迟: {result['latency']}s")
        print(f"回答: {result['answer'][:100]}...\n")
```

### 2.2 级联模式

先由小模型尝试，如果置信度不够则升级到大模型。

```python
class CascadeClient:
    def __init__(self, confidence_threshold: float = 0.7):
        self.models = ["qwen-turbo", "qwen-plus", "qwen-max"]
        self.confidence_threshold = confidence_threshold

    def chat(self, query: str) -> dict:
        for model in self.models:
            try:
                response = dashscope.Generation.call(
                    model=model,
                    messages=[{"role": "user", "content": query}],
                    result_format="message"
                )
                if response.status_code == 200:
                    answer = response.output.choices[0].message.content
                    confidence = self._estimate_confidence(answer)

                    if confidence >= self.confidence_threshold:
                        return {
                            "model": model,
                            "answer": answer,
                            "confidence": confidence,
                            "escalated": model != self.models[0]
                        }
                    else:
                        logger.info(f"置信度不足({confidence:.2f})，升级到更强模型")
            except Exception as e:
                logger.error(f"模型{model}调用失败: {e}")
                continue

        return {"model": self.models[-1], "answer": "所有模型均无法回答", "confidence": 0, "escalated": True}

    def _estimate_confidence(self, answer: str) -> float:
        uncertain_phrases = ["不确定", "可能", "也许", "不太清楚", "无法确定", "据我所知"]
        uncertain_count = sum(1 for phrase in uncertain_phrases if phrase in answer)
        confidence = max(0.3, 1.0 - uncertain_count * 0.15)
        if len(answer) < 20:
            confidence *= 0.7
        return confidence
```

### 2.3 并行模式

多个模型同时回答，选择最优结果或融合多个结果。

```python
import concurrent.futures


class ParallelClient:
    def __init__(self):
        self.models = ["qwen-turbo", "qwen-plus"]

    def _call_model(self, model: str, query: str) -> dict:
        try:
            response = dashscope.Generation.call(
                model=model,
                messages=[{"role": "user", "content": query}],
                result_format="message"
            )
            if response.status_code == 200:
                return {"model": model, "answer": response.output.choices[0].message.content}
        except Exception as e:
            return {"model": model, "answer": f"调用失败: {e}"}

    def chat(self, query: str) -> dict:
        with concurrent.futures.ThreadPoolExecutor(max_workers=len(self.models)) as executor:
            futures = {
                executor.submit(self._call_model, model, query): model
                for model in self.models
            }
            results = []
            for future in concurrent.futures.as_completed(futures):
                results.append(future.result())

        return {
            "query": query,
            "results": results,
            "best": max(results, key=lambda r: len(r["answer"]))
        }
```

## 三、混合专家（MoE）架构

### 3.1 MoE原理

MoE（Mixture of Experts）是DeepSeek、Qwen3等最新模型采用的核心架构，通过稀疏激活机制，让模型在推理时只激活部分专家，大幅降低计算成本。

```
输入 → 门控网络(Gate) → 选择Top-K专家 → 专家并行计算 → 加权融合 → 输出
```

### 3.2 应用层MoE

在应用层模拟MoE，根据任务类型选择不同的"专家"模型：

```python
class ApplicationMoE:
    def __init__(self):
        self.experts = {
            "code": {"model": "deepseek-r1:7b", "description": "代码专家"},
            "math": {"model": "deepseek-r1:7b", "description": "数学推理专家"},
            "creative": {"model": "qwen-plus", "description": "创意写作专家"},
            "analysis": {"model": "qwen-max", "description": "深度分析专家"},
            "chat": {"model": "qwen-turbo", "description": "日常对话专家"}
        }

    def gate(self, query: str) -> list[str]:
        if any(kw in query for kw in ["代码", "编程", "函数", "bug", "debug"]):
            return ["code"]
        elif any(kw in query for kw in ["计算", "数学", "方程", "证明"]):
            return ["math"]
        elif any(kw in query for kw in ["写", "创作", "故事", "文案"]):
            return ["creative"]
        elif any(kw in query for kw in ["分析", "对比", "评估", "深度"]):
            return ["analysis"]
        else:
            return ["chat"]

    def execute(self, query: str) -> dict:
        selected_experts = self.gate(query)
        expert = self.experts[selected_experts[0]]

        return {
            "query": query,
            "expert": expert["description"],
            "model": expert["model"],
            "selected_experts": selected_experts
        }
```

## 四、多模型编排最佳实践

### 4.1 编排策略选择

| 策略 | 适用场景 | 优势 | 劣势 |
|------|----------|------|------|
| 路由 | 明确的任务分类 | 成本低、延迟低 | 分类可能不准 |
| 级联 | 置信度要求高 | 质量有保障 | 可能多次调用 |
| 并行 | 结果质量要求高 | 可选最优 | 成本高 |
| MoE | 多领域专业任务 | 专业性强 | 需要领域知识 |

### 4.2 成本优化

```python
class CostOptimizer:
    MODEL_COSTS = {
        "qwen-turbo": 0.002,
        "qwen-plus": 0.004,
        "qwen-max": 0.04,
        "deepseek-r1:7b": 0.0
    }

    def estimate_cost(self, model: str, input_tokens: int, output_tokens: int) -> float:
        cost_per_1k = self.MODEL_COSTS.get(model, 0.01)
        return (input_tokens + output_tokens) / 1000 * cost_per_1k

    def recommend_model(self, query: str, budget: float = 0.01) -> str:
        estimated_tokens = len(query) * 2 + 500

        for model in ["qwen-turbo", "qwen-plus", "qwen-max"]:
            cost = self.estimate_cost(model, estimated_tokens, 500)
            if cost <= budget:
                return model

        return "qwen-turbo"
```

## 总结

本章系统讲解了多模型协作与编排技术：

1. **路由模式**：根据任务复杂度选择合适模型，平衡成本与质量
2. **级联模式**：小模型先行，置信度不足时升级，保证回答质量
3. **并行模式**：多模型同时推理，选择最优结果
4. **MoE架构**：应用层模拟混合专家，按领域选择专业模型
5. **成本优化**：根据预算智能选择模型，控制API调用成本

多模型协作是AI应用工程化的核心能力，掌握编排策略可以显著提升应用效果和降低成本。
