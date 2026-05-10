---
title: "AI 应用开发-017 机器学习实践"
date: 2025-06-08T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型", "机器学习", "特征工程", "模型评估"]
---

## 概述

机器学习是AI应用的数据引擎，掌握机器学习实践能力是从"调API"到"建系统"的关键跨越。本章将系统讲解机器学习的完整工作流：数据预处理、特征工程、模型训练、评估调优和部署上线，并通过客户流失预测项目串联全流程。

## 一、机器学习工作流

```
业务理解 → 数据获取 → 数据预处理 → 特征工程 → 模型训练 → 模型评估 → 模型部署 → 监控迭代
```

### 1.1 各阶段时间占比

| 阶段 | 占比 | 说明 |
|------|------|------|
| 数据获取与预处理 | 40% | 最耗时的阶段 |
| 特征工程 | 25% | 决定模型上限 |
| 模型训练与调优 | 20% | 算法选择与超参搜索 |
| 评估与部署 | 15% | 验证与上线 |

## 二、数据预处理

### 2.1 常见数据问题与处理

```python
import pandas as pd
import numpy as np
import logging
from sklearn.impute import SimpleImputer
from sklearn.preprocessing import StandardScaler, LabelEncoder

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class DataPreprocessor:
    def __init__(self):
        self.numeric_imputer = SimpleImputer(strategy="median")
        self.categorical_imputer = SimpleImputer(strategy="most_frequent")
        self.scaler = StandardScaler()
        self.label_encoders = {}

    def fit_transform(self, df: pd.DataFrame, target_col: str = None) -> pd.DataFrame:
        df = df.copy()

        numeric_cols = df.select_dtypes(include=[np.number]).columns.tolist()
        if target_col and target_col in numeric_cols:
            numeric_cols.remove(target_col)

        categorical_cols = df.select_dtypes(include=["object"]).columns.tolist()
        if target_col and target_col in categorical_cols:
            categorical_cols.remove(target_col)

        if numeric_cols:
            df[numeric_cols] = self.numeric_imputer.fit_transform(df[numeric_cols])
            df[numeric_cols] = self.scaler.fit_transform(df[numeric_cols])
            logger.info(f"数值列处理完成: {len(numeric_cols)} 列")

        if categorical_cols:
            df[categorical_cols] = self.categorical_imputer.fit_transform(df[categorical_cols])
            for col in categorical_cols:
                le = LabelEncoder()
                df[col] = le.fit_transform(df[col])
                self.label_encoders[col] = le
            logger.info(f"分类列处理完成: {len(categorical_cols)} 列")

        return df

    def handle_outliers(self, df: pd.DataFrame, columns: list[str], method: str = "iqr") -> pd.DataFrame:
        df = df.copy()
        for col in columns:
            if method == "iqr":
                Q1 = df[col].quantile(0.25)
                Q3 = df[col].quantile(0.75)
                IQR = Q3 - Q1
                lower = Q1 - 1.5 * IQR
                upper = Q3 + 1.5 * IQR
                df[col] = df[col].clip(lower, upper)
            elif method == "zscore":
                mean = df[col].mean()
                std = df[col].std()
                df[col] = df[col].clip(mean - 3 * std, mean + 3 * std)
        return df


if __name__ == "__main__":
    data = {
        "age": [25, 30, np.nan, 45, 50, 200, 35],
        "income": [50000, 60000, 55000, np.nan, 80000, 90000, 65000],
        "city": ["北京", "上海", "北京", np.nan, "广州", "上海", "北京"],
        "churn": [0, 1, 0, 1, 0, 0, 1]
    }
    df = pd.DataFrame(data)

    preprocessor = DataPreprocessor()
    df_processed = preprocessor.fit_transform(df, target_col="churn")
    logger.info(f"预处理完成:\n{df_processed}")
```

## 三、特征工程

### 3.1 特征构建方法

| 方法 | 说明 | 示例 |
|------|------|------|
| 数值变换 | 对数、平方根 | log(income) |
| 交叉特征 | 两特征组合 | age × income |
| 时间特征 | 提取时间属性 | 月份、星期、小时 |
| 统计特征 | 聚合统计 | 用户近7天消费均值 |
| 文本特征 | TF-IDF、Embedding | 商品描述向量 |

```python
class FeatureEngineer:
    def __init__(self):
        self.feature_names = []

    def create_features(self, df: pd.DataFrame) -> pd.DataFrame:
        df = df.copy()

        if "income" in df.columns and "age" in df.columns:
            df["income_per_age"] = df["income"] / (df["age"] + 1)
            df["log_income"] = np.log1p(df["income"].abs())

        if "signup_date" in df.columns:
            df["signup_date"] = pd.to_datetime(df["signup_date"], errors="coerce")
            df["signup_month"] = df["signup_date"].dt.month
            df["signup_dow"] = df["signup_date"].dt.dayofweek
            df["days_since_signup"] = (pd.Timestamp.now() - df["signup_date"]).dt.days

        numeric_cols = df.select_dtypes(include=[np.number]).columns
        for col in numeric_cols:
            if df[col].std() > 0:
                df[f"{col}_zscore"] = (df[col] - df[col].mean()) / df[col].std()

        self.feature_names = df.columns.tolist()
        logger.info(f"特征构建完成，共 {len(self.feature_names)} 个特征")
        return df


if __name__ == "__main__":
    data = {
        "age": [25, 30, 35, 45, 50],
        "income": [50000, 60000, 55000, 70000, 80000],
        "signup_date": ["2023-01-15", "2023-06-20", "2024-03-10", "2022-11-05", "2024-01-01"],
        "churn": [0, 1, 0, 1, 0]
    }
    df = pd.DataFrame(data)

    fe = FeatureEngineer()
    df_features = fe.create_features(df)
    logger.info(f"特征数量: {len(df_features.columns)}")
```

## 四、模型训练与评估

### 4.1 客户流失预测项目

```python
from sklearn.model_selection import train_test_split, cross_val_score
from sklearn.ensemble import RandomForestClassifier, GradientBoostingClassifier
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import (
    accuracy_score, precision_score, recall_score, f1_score,
    roc_auc_score, classification_report, confusion_matrix
)
import pandas as pd
import numpy as np
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def generate_churn_data(n_samples: int = 1000) -> pd.DataFrame:
    np.random.seed(42)

    data = {
        "age": np.random.randint(18, 70, n_samples),
        "income": np.random.randint(20000, 150000, n_samples),
        "tenure_months": np.random.randint(1, 60, n_samples),
        "monthly_charges": np.random.uniform(20, 200, n_samples),
        "total_charges": np.random.uniform(100, 10000, n_samples),
        "num_support_calls": np.random.randint(0, 10, n_samples),
        "has_premium": np.random.choice([0, 1], n_samples, p=[0.7, 0.3]),
        "contract_type": np.random.choice([0, 1, 2], n_samples, p=[0.5, 0.3, 0.2])
    }

    df = pd.DataFrame(data)

    churn_prob = (
        0.1
        - 0.01 * df["tenure_months"]
        + 0.002 * df["num_support_calls"]
        - 0.1 * df["has_premium"]
        - 0.05 * df["contract_type"]
        + 0.001 * df["monthly_charges"]
    )
    churn_prob = np.clip(churn_prob, 0.05, 0.95)
    df["churn"] = np.random.binomial(1, churn_prob)

    return df


def train_and_evaluate():
    df = generate_churn_data(1000)
    logger.info(f"数据集: {len(df)} 条, 流失率: {df['churn'].mean():.2%}")

    feature_cols = [col for col in df.columns if col != "churn"]
    X = df[feature_cols]
    y = df["churn"]

    X_train, X_test, y_train, y_test = train_test_split(X, y, test_size=0.2, random_state=42, stratify=y)

    models = {
        "逻辑回归": LogisticRegression(max_iter=1000, random_state=42),
        "随机森林": RandomForestClassifier(n_estimators=100, random_state=42),
        "梯度提升": GradientBoostingClassifier(n_estimators=100, random_state=42)
    }

    results = {}
    for name, model in models.items():
        model.fit(X_train, y_train)
        y_pred = model.predict(X_test)
        y_prob = model.predict_proba(X_test)[:, 1]

        metrics = {
            "accuracy": accuracy_score(y_test, y_pred),
            "precision": precision_score(y_test, y_pred),
            "recall": recall_score(y_test, y_pred),
            "f1": f1_score(y_test, y_pred),
            "auc": roc_auc_score(y_test, y_prob)
        }
        results[name] = metrics

        cv_scores = cross_val_score(model, X_train, y_train, cv=5, scoring="f1")
        logger.info(f"\n{name}:")
        logger.info(f"  准确率: {metrics['accuracy']:.4f}")
        logger.info(f"  精确率: {metrics['precision']:.4f}")
        logger.info(f"  召回率: {metrics['recall']:.4f}")
        logger.info(f"  F1: {metrics['f1']:.4f}")
        logger.info(f"  AUC: {metrics['auc']:.4f}")
        logger.info(f"  5折CV F1: {cv_scores.mean():.4f} ± {cv_scores.std():.4f}")

    best_model_name = max(results, key=lambda k: results[k]["f1"])
    logger.info(f"\n最佳模型: {best_model_name}, F1={results[best_model_name]['f1']:.4f}")

    return models[best_model_name], feature_cols


if __name__ == "__main__":
    best_model, features = train_and_evaluate()

    importances = best_model.feature_importances_ if hasattr(best_model, "feature_importances_") else None
    if importances is not None:
        for feat, imp in sorted(zip(features, importances), key=lambda x: x[1], reverse=True):
            logger.info(f"  {feat}: {imp:.4f}")
```

## 五、超参数调优

```python
from sklearn.model_selection import GridSearchCV


def hyperparameter_tuning(X_train, y_train):
    param_grid = {
        "n_estimators": [50, 100, 200],
        "max_depth": [3, 5, 7],
        "learning_rate": [0.01, 0.1, 0.2],
        "min_samples_split": [2, 5, 10]
    }

    model = GradientBoostingClassifier(random_state=42)

    grid_search = GridSearchCV(
        model, param_grid, cv=5, scoring="f1", n_jobs=-1, verbose=0
    )
    grid_search.fit(X_train, y_train)

    logger.info(f"最佳参数: {grid_search.best_params_}")
    logger.info(f"最佳F1: {grid_search.best_score_:.4f}")

    return grid_search.best_estimator_
```

## 六、模型部署

### 6.1 模型持久化

```python
import joblib


def save_model(model, preprocessor, path: str = "model"):
    joblib.dump(model, f"{path}/model.pkl")
    joblib.dump(preprocessor, f"{path}/preprocessor.pkl")
    logger.info(f"模型已保存: {path}")


def load_model(path: str = "model"):
    model = joblib.load(f"{path}/model.pkl")
    preprocessor = joblib.load(f"{path}/preprocessor.pkl")
    return model, preprocessor


def predict_single(model, preprocessor, features: dict) -> dict:
    df = pd.DataFrame([features])
    prediction = model.predict(df)[0]
    probability = model.predict_proba(df)[0][1]

    return {
        "churn_prediction": int(prediction),
        "churn_probability": float(probability),
        "risk_level": "高" if probability > 0.7 else "中" if probability > 0.3 else "低"
    }
```

### 6.2 FastAPI部署

```python
from fastapi import FastAPI
from pydantic import BaseModel

app = FastAPI(title="客户流失预测API")


class CustomerFeatures(BaseModel):
    age: int
    income: int
    tenure_months: int
    monthly_charges: float
    total_charges: float
    num_support_calls: int
    has_premium: int
    contract_type: int


@app.post("/predict")
def predict(customer: CustomerFeatures):
    features = customer.model_dump()
    result = predict_single(model, preprocessor, features)
    return result


@app.get("/health")
def health():
    return {"status": "healthy"}
```

## 七、机器学习最佳实践

| 实践 | 说明 |
|------|------|
| 数据先行 | 先确保数据质量，再考虑模型选择 |
| 简单起步 | 从简单模型开始，逐步增加复杂度 |
| 交叉验证 | 始终使用交叉验证评估模型 |
| 特征重要 | 特征工程比模型选择更重要 |
| 防止泄漏 | 确保训练数据不含未来信息 |
| 监控漂移 | 上线后持续监控数据分布和模型性能 |

## 总结

本章系统讲解了机器学习实践：

1. **工作流**：从业务理解到模型部署的完整流程
2. **数据预处理**：缺失值、异常值、标准化是数据质量的基础
3. **特征工程**：决定模型上限的关键环节
4. **模型训练**：多模型对比、交叉验证、超参调优
5. **模型部署**：持久化 + FastAPI，快速上线预测服务

机器学习是AI应用的数据引擎，与LLM的生成能力互补，共同构建完整的AI系统。
