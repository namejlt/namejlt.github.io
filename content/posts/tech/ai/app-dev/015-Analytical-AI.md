---
title: "AI 应用开发-015 分析式AI"
date: 2025-06-06T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型", "分析式AI", "生成式AI", "数据分析"]
---

## 概述

分析式AI（Analytical AI）与生成式AI（Generative AI）是AI的两大范式。分析式AI侧重于从数据中发现模式、做出预测和决策，是传统机器学习和统计学习的核心；生成式AI侧重于创造新内容。理解两者的区别与联系，是构建完整AI应用的关键。

本章将系统讲解分析式AI的核心方法、与生成式AI的对比，以及如何将两者结合构建更强大的AI应用。

## 一、分析式AI vs 生成式AI

### 1.1 核心区别

| 维度 | 分析式AI | 生成式AI |
|------|----------|----------|
| 目标 | 理解和预测 | 创造和生成 |
| 输出 | 分类/预测值/决策 | 文本/图像/代码 |
| 方法 | 统计学习、机器学习 | 深度学习、Transformer |
| 评估 | 准确率、F1、AUC | 人工评估、BLEU |
| 典型任务 | 分类、回归、聚类 | 文本生成、图像生成 |
| 可解释性 | 较高 | 较低 |

### 1.2 两者结合的趋势

```
分析式AI（理解数据） + 生成式AI（生成内容） = 智能AI应用
```

典型案例：
- **RAG**：分析式AI（检索排序）+ 生成式AI（答案生成）
- **智能BI**：分析式AI（数据聚合）+ 生成式AI（报告生成）
- **风控系统**：分析式AI（风险评估）+ 生成式AI（风险报告）

## 二、分析式AI核心方法

### 2.1 分类

```python
from sklearn.datasets import load_iris
from sklearn.model_selection import train_test_split
from sklearn.ensemble import RandomForestClassifier
from sklearn.metrics import classification_report, accuracy_score
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def classification_example():
    iris = load_iris()
    X_train, X_test, y_train, y_test = train_test_split(
        iris.data, iris.target, test_size=0.2, random_state=42
    )

    clf = RandomForestClassifier(n_estimators=100, random_state=42)
    clf.fit(X_train, y_train)

    y_pred = clf.predict(X_test)
    accuracy = accuracy_score(y_test, y_pred)

    logger.info(f"分类准确率: {accuracy:.4f}")
    logger.info(f"\n{classification_report(y_test, y_pred, target_names=iris.target_names)}")

    feature_importance = sorted(
        zip(iris.feature_names, clf.feature_importances_),
        key=lambda x: x[1], reverse=True
    )
    for name, importance in feature_importance:
        logger.info(f"特征重要性: {name} = {importance:.4f}")

    return clf


if __name__ == "__main__":
    classification_example()
```

### 2.2 回归

```python
from sklearn.datasets import fetch_california_housing
from sklearn.model_selection import train_test_split
from sklearn.ensemble import GradientBoostingRegressor
from sklearn.metrics import mean_squared_error, r2_score
import numpy as np
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def regression_example():
    housing = fetch_california_housing()
    X_train, X_test, y_train, y_test = train_test_split(
        housing.data, housing.target, test_size=0.2, random_state=42
    )

    model = GradientBoostingRegressor(
        n_estimators=200,
        max_depth=5,
        learning_rate=0.1,
        random_state=42
    )
    model.fit(X_train, y_train)

    y_pred = model.predict(X_test)
    rmse = np.sqrt(mean_squared_error(y_test, y_pred))
    r2 = r2_score(y_test, y_pred)

    logger.info(f"RMSE: {rmse:.4f}")
    logger.info(f"R²: {r2:.4f}")

    return model


if __name__ == "__main__":
    regression_example()
```

### 2.3 聚类

```python
from sklearn.datasets import make_blobs
from sklearn.cluster import KMeans
from sklearn.metrics import silhouette_score
import numpy as np
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def clustering_example():
    X, _ = make_blobs(n_samples=300, centers=4, random_state=42)

    best_k = 2
    best_score = -1
    scores = {}

    for k in range(2, 8):
        kmeans = KMeans(n_clusters=k, random_state=42, n_init=10)
        labels = kmeans.fit_predict(X)
        score = silhouette_score(X, labels)
        scores[k] = score

        if score > best_score:
            best_score = score
            best_k = k

    logger.info(f"最优聚类数: {best_k}, 轮廓系数: {best_score:.4f}")
    for k, score in scores.items():
        logger.info(f"  K={k}: 轮廓系数={score:.4f}")

    final_kmeans = KMeans(n_clusters=best_k, random_state=42, n_init=10)
    final_labels = final_kmeans.fit_predict(X)

    return final_kmeans, final_labels


if __name__ == "__main__":
    clustering_example()
```

## 三、分析式AI与LLM的结合

### 3.1 LLM增强分析式AI

```python
import dashscope
import json
import logging
import os

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

dashscope.api_key = os.getenv("DASHSCOPE_API_KEY", "sk-xxx")


class LLMEnhancedAnalyzer:
    def __init__(self):
        self.model = "qwen-turbo"

    def interpret_model_result(self, model_type: str, metrics: dict, feature_importance: list = None) -> str:
        prompt = f"""请用通俗易懂的语言解释以下机器学习模型的结果：

模型类型：{model_type}
评估指标：{json.dumps(metrics, ensure_ascii=False)}
特征重要性：{json.dumps(feature_importance, ensure_ascii=False) if feature_importance else '无'}

请包含：
1. 模型效果的总体评价
2. 关键指标的含义解释
3. 最重要的特征及其业务含义
4. 改进建议"""

        response = dashscope.Generation.call(
            model=self.model,
            messages=[{"role": "user", "content": prompt}],
            result_format="message"
        )
        if response.status_code == 200:
            return response.output.choices[0].message.content
        return "分析报告生成失败"

    def suggest_features(self, domain: str, existing_features: list[str], target: str) -> str:
        prompt = f"""在{domain}领域，现有特征为{existing_features}，预测目标为{target}。
请建议5-10个可能有用的衍生特征，并说明理由。"""

        response = dashscope.Generation.call(
            model=self.model,
            messages=[{"role": "user", "content": prompt}],
            result_format="message"
        )
        if response.status_code == 200:
            return response.output.choices[0].message.content
        return "特征建议生成失败"


if __name__ == "__main__":
    analyzer = LLMEnhancedAnalyzer()

    report = analyzer.interpret_model_result(
        model_type="随机森林分类器",
        metrics={"accuracy": 0.95, "precision": 0.94, "recall": 0.96, "f1": 0.95},
        feature_importance=[("花瓣长度", 0.45), ("花瓣宽度", 0.35), ("花萼长度", 0.12), ("花萼宽度", 0.08)]
    )
    print(report)
```

### 3.2 分析式AI增强LLM

```python
class AnalyticsEnhancedRAG:
    def __init__(self):
        self.model = "qwen-turbo"

    def analyze_and_respond(self, query: str, data: list[dict]) -> str:
        import pandas as pd
        df = pd.DataFrame(data)

        stats = {
            "row_count": len(df),
            "column_count": len(df.columns),
            "numeric_stats": df.describe().to_dict()
        }

        prompt = f"""基于以下数据分析结果回答用户问题。

数据概况：{json.dumps(stats, ensure_ascii=False, default=str)[:1000]}

用户问题：{query}

请结合数据分析给出专业回答。"""

        response = dashscope.Generation.call(
            model=self.model,
            messages=[{"role": "user", "content": prompt}],
            result_format="message"
        )
        if response.status_code == 200:
            return response.output.choices[0].message.content
        return "分析回答生成失败"
```

## 四、分析式AI应用场景

| 场景 | 分析式AI | 生成式AI | 结合方式 |
|------|----------|----------|----------|
| 智能BI | 数据聚合、趋势分析 | 报告生成、图表描述 | 分析→生成报告 |
| 风控 | 风险评分、异常检测 | 风险报告、处置建议 | 评分→生成建议 |
| 推荐系统 | 协同过滤、内容匹配 | 推荐理由生成 | 推序→生成解释 |
| 舆情监控 | 情感分析、话题聚类 | 舆情报告、预警 | 分析→生成预警 |

## 总结

本章系统讲解了分析式AI的核心概念和实践：

1. **范式对比**：分析式AI侧重理解和预测，生成式AI侧重创造和生成
2. **核心方法**：分类、回归、聚类是分析式AI的三大基础任务
3. **LLM增强分析**：用LLM解释模型结果、建议特征，降低AI应用的使用门槛
4. **分析增强LLM**：用分析式AI的结果作为LLM的输入，提升回答的准确性
5. **融合趋势**：分析式+生成式是AI应用的主流范式

理解分析式AI与生成式AI的互补关系，是构建完整AI应用架构的基础。
