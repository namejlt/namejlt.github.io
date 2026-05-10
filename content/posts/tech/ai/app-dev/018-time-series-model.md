---
title: "AI 应用开发-018 时间序列模型"
date: 2025-06-09T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型", "时间序列", "预测", "Transformer"]
---

## 概述

时间序列分析是AI应用中最常见的数据类型之一——股票价格、销售数据、服务器指标、天气变化，都是时间序列。本章将系统讲解时间序列分析的核心方法，从经典统计模型到深度学习模型，再到LLM赋能的时间序列预测，帮助读者掌握时间序列建模的完整技术栈。

## 一、时间序列基础

### 1.1 时间序列的组成

```
时间序列 = 趋势(Trend) + 季节性(Seasonality) + 残差(Residual)
```

```python
import numpy as np
import pandas as pd
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def generate_time_series(n_days: int = 365 * 3, seed: int = 42) -> pd.DataFrame:
    np.random.seed(seed)
    dates = pd.date_range(start="2022-01-01", periods=n_days, freq="D")

    trend = np.linspace(100, 200, n_days)
    seasonality = 20 * np.sin(2 * np.pi * np.arange(n_days) / 365)
    weekly = 5 * np.sin(2 * np.pi * np.arange(n_days) / 7)
    noise = np.random.normal(0, 8, n_days)

    values = trend + seasonality + weekly + noise

    df = pd.DataFrame({
        "date": dates,
        "value": values,
        "trend": trend,
        "seasonality": seasonality + weekly,
        "residual": noise
    })
    df.set_index("date", inplace=True)
    logger.info(f"生成时间序列: {len(df)} 天, 起始{df.index[0]}, 结束{df.index[-1]}")
    return df


if __name__ == "__main__":
    df = generate_time_series()
    logger.info(f"\n{df.describe()}")
```

### 1.2 时间序列特征

| 特征 | 说明 | 检测方法 |
|------|------|----------|
| 平稳性 | 统计特性不随时间变化 | ADF检验 |
| 自相关 | 当前值与历史值的相关性 | ACF/PACF图 |
| 趋势 | 长期上升或下降 | 移动平均 |
| 季节性 | 周期性波动 | STL分解 |

```python
from statsmodels.tsa.stattools import adfuller


def check_stationarity(series: pd.Series) -> dict:
    result = adfuller(series.dropna())
    return {
        "adf_statistic": result[0],
        "p_value": result[1],
        "is_stationary": result[1] < 0.05,
        "critical_values": result[4]
    }


if __name__ == "__main__":
    df = generate_time_series()
    stationarity = check_stationarity(df["value"])
    logger.info(f"平稳性检验: ADF={stationarity['adf_statistic']:.4f}, p={stationarity['p_value']:.4f}")
    logger.info(f"是否平稳: {'是' if stationarity['is_stationary'] else '否'}")
```

## 二、经典统计模型

### 2.1 ARIMA

ARIMA（AutoRegressive Integrated Moving Average）是时间序列预测的经典方法。

```python
from statsmodels.tsa.arima.model import ARIMA
from statsmodels.tsa.statespace.sarimax import SARIMAX
import numpy as np
import pandas as pd
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class ARIMAForecaster:
    def __init__(self, order: tuple = (1, 1, 1), seasonal_order: tuple = None):
        self.order = order
        self.seasonal_order = seasonal_order
        self.model = None
        self.fitted_model = None

    def fit(self, series: pd.Series):
        if self.seasonal_order:
            self.model = SARIMAX(
                series,
                order=self.order,
                seasonal_order=self.seasonal_order,
                enforce_stationarity=False,
                enforce_invertibility=False
            )
        else:
            self.model = ARIMA(series, order=self.order)

        self.fitted_model = self.model.fit()
        logger.info(f"ARIMA模型拟合完成: order={self.order}, AIC={self.fitted_model.aic:.2f}")
        return self

    def predict(self, steps: int = 30) -> pd.Series:
        if not self.fitted_model:
            raise ValueError("模型未训练")
        forecast = self.fitted_model.forecast(steps=steps)
        return forecast

    def predict_with_confidence(self, steps: int = 30, alpha: float = 0.05) -> dict:
        forecast = self.fitted_model.get_forecast(steps=steps)
        pred = forecast.predicted_mean
        conf_int = forecast.conf_int(alpha=alpha)
        return {
            "forecast": pred,
            "lower": conf_int.iloc[:, 0],
            "upper": conf_int.iloc[:, 1]
        }


if __name__ == "__main__":
    df = generate_time_series(n_days=365 * 3)
    train = df["value"].iloc[:-30]
    test = df["value"].iloc[-30:]

    forecaster = ARIMAForecaster(order=(2, 1, 2), seasonal_order=(1, 1, 1, 7))
    forecaster.fit(train)

    result = forecaster.predict_with_confidence(steps=30)

    from sklearn.metrics import mean_absolute_error, mean_squared_error
    mae = mean_absolute_error(test, result["forecast"])
    rmse = np.sqrt(mean_squared_error(test, result["forecast"]))
    mape = np.mean(np.abs((test - result["forecast"]) / test)) * 100

    logger.info(f"MAE: {mae:.4f}, RMSE: {rmse:.4f}, MAPE: {mape:.2f}%")
```

### 2.2 Prophet

Prophet是Meta开源的时间序列预测工具，自动处理趋势变化点和节假日效应。

```python
from prophet import Prophet
import pandas as pd
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class ProphetForecaster:
    def __init__(self, yearly_seasonality: bool = True, weekly_seasonality: bool = True):
        self.model = Prophet(
            yearly_seasonality=yearly_seasonality,
            weekly_seasonality=weekly_seasonality,
            daily_seasonality=False,
            changepoint_prior_scale=0.05
        )

    def fit(self, df: pd.DataFrame, date_col: str = "ds", value_col: str = "y"):
        prophet_df = pd.DataFrame({
            "ds": df[date_col] if date_col in df.columns else df.index,
            "y": df[value_col]
        })
        self.model.fit(prophet_df)
        logger.info("Prophet模型拟合完成")
        return self

    def predict(self, periods: int = 30, freq: str = "D") -> pd.DataFrame:
        future = self.model.make_future_dataframe(periods=periods, freq=freq)
        forecast = self.model.predict(future)
        return forecast[["ds", "yhat", "yhat_lower", "yhat_upper"]].tail(periods)


if __name__ == "__main__":
    df = generate_time_series()
    prophet_df = df.reset_index()[["date", "value"]].rename(columns={"date": "ds", "value": "y"})

    train_df = prophet_df.iloc[:-30]

    forecaster = ProphetForecaster()
    forecaster.fit(train_df)

    forecast = forecaster.predict(periods=30)
    logger.info(f"\n{forecast.head()}")
```

## 三、深度学习时间序列模型

### 3.1 LSTM

LSTM（Long Short-Term Memory）擅长捕获时间序列的长期依赖关系。

```python
import numpy as np
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def create_sequences(data: np.ndarray, seq_length: int = 30) -> tuple:
    X, y = [], []
    for i in range(len(data) - seq_length):
        X.append(data[i:i + seq_length])
        y.append(data[i + seq_length])
    return np.array(X), np.array(y)


class LSTMForecaster:
    def __init__(self, seq_length: int = 30, hidden_size: int = 64, num_layers: int = 2):
        self.seq_length = seq_length
        self.hidden_size = hidden_size
        self.num_layers = num_layers
        self.model = None
        self.scaler_mean = 0
        self.scaler_std = 1

    def fit(self, series: np.ndarray, epochs: int = 50, batch_size: int = 32, lr: float = 0.001):
        try:
            import torch
            import torch.nn as nn
        except ImportError:
            logger.error("PyTorch未安装，请运行: pip install torch")
            return self

        self.scaler_mean = series.mean()
        self.scaler_std = series.std()
        normalized = (series - self.scaler_mean) / self.scaler_std

        X, y = create_sequences(normalized, self.seq_length)

        X_tensor = torch.FloatTensor(X).unsqueeze(-1)
        y_tensor = torch.FloatTensor(y).unsqueeze(-1)

        class LSTMModel(nn.Module):
            def __init__(self, input_size, hidden_size, num_layers, output_size):
                super().__init__()
                self.lstm = nn.LSTM(input_size, hidden_size, num_layers, batch_first=True, dropout=0.2)
                self.fc = nn.Linear(hidden_size, output_size)

            def forward(self, x):
                out, _ = self.lstm(x)
                out = self.fc(out[:, -1, :])
                return out

        self.model = LSTMModel(1, self.hidden_size, self.num_layers, 1)
        criterion = nn.MSELoss()
        optimizer = torch.optim.Adam(self.model.parameters(), lr=lr)

        for epoch in range(epochs):
            self.model.train()
            perm = torch.randperm(len(X_tensor))
            total_loss = 0

            for i in range(0, len(X_tensor), batch_size):
                idx = perm[i:i + batch_size]
                batch_X = X_tensor[idx]
                batch_y = y_tensor[idx]

                optimizer.zero_grad()
                output = self.model(batch_X)
                loss = criterion(output, batch_y)
                loss.backward()
                optimizer.step()
                total_loss += loss.item()

            if (epoch + 1) % 10 == 0:
                avg_loss = total_loss / (len(X_tensor) // batch_size + 1)
                logger.info(f"Epoch {epoch + 1}/{epochs}, Loss: {avg_loss:.6f}")

        return self

    def predict(self, series: np.ndarray, steps: int = 30) -> np.ndarray:
        try:
            import torch
        except ImportError:
            return np.zeros(steps)

        self.model.eval()
        normalized = (series - self.scaler_mean) / self.scaler_std

        current_seq = normalized[-self.seq_length:].reshape(1, self.seq_length, 1)
        predictions = []

        with torch.no_grad():
            for _ in range(steps):
                x = torch.FloatTensor(current_seq)
                pred = self.model(x).item()
                predictions.append(pred)
                current_seq = np.roll(current_seq, -1, axis=1)
                current_seq[0, -1, 0] = pred

        predictions = np.array(predictions) * self.scaler_std + self.scaler_mean
        return predictions


if __name__ == "__main__":
    df = generate_time_series()
    values = df["value"].values

    train_data = values[:-30]
    test_data = values[-30:]

    forecaster = LSTMForecaster(seq_length=30, hidden_size=64, num_layers=2)
    forecaster.fit(train_data, epochs=50)

    predictions = forecaster.predict(train_data, steps=30)

    mae = np.mean(np.abs(test_data - predictions))
    logger.info(f"LSTM MAE: {mae:.4f}")
```

### 3.2 Transformer时间序列

Transformer的自注意力机制天然适合时间序列的长距离依赖建模。

```python
class TransformerForecaster:
    def __init__(self, seq_length: int = 30, d_model: int = 64, nhead: int = 4, num_layers: int = 2):
        self.seq_length = seq_length
        self.d_model = d_model
        self.nhead = nhead
        self.num_layers = num_layers
        self.model = None
        self.scaler_mean = 0
        self.scaler_std = 1

    def fit(self, series: np.ndarray, epochs: int = 50, batch_size: int = 32, lr: float = 0.001):
        try:
            import torch
            import torch.nn as nn
        except ImportError:
            logger.error("PyTorch未安装")
            return self

        self.scaler_mean = series.mean()
        self.scaler_std = series.std()
        normalized = (series - self.scaler_mean) / self.scaler_std

        X, y = create_sequences(normalized, self.seq_length)
        X_tensor = torch.FloatTensor(X).unsqueeze(-1)
        y_tensor = torch.FloatTensor(y).unsqueeze(-1)

        class TimeSeriesTransformer(nn.Module):
            def __init__(self, input_size, d_model, nhead, num_layers, output_size):
                super().__init__()
                self.embedding = nn.Linear(input_size, d_model)
                self.pos_encoding = nn.Parameter(torch.randn(1, 1000, d_model) * 0.01)
                encoder_layer = nn.TransformerEncoderLayer(
                    d_model=d_model, nhead=nhead, dim_feedforward=d_model * 4, dropout=0.1, batch_first=True
                )
                self.transformer = nn.TransformerEncoder(encoder_layer, num_layers=num_layers)
                self.fc = nn.Linear(d_model, output_size)

            def forward(self, x):
                x = self.embedding(x)
                x = x + self.pos_encoding[:, :x.size(1), :]
                x = self.transformer(x)
                x = self.fc(x[:, -1, :])
                return x

        self.model = TimeSeriesTransformer(1, self.d_model, self.nhead, self.num_layers, 1)
        criterion = nn.MSELoss()
        optimizer = torch.optim.AdamW(self.model.parameters(), lr=lr)

        for epoch in range(epochs):
            self.model.train()
            perm = torch.randperm(len(X_tensor))
            total_loss = 0

            for i in range(0, len(X_tensor), batch_size):
                idx = perm[i:i + batch_size]
                batch_X = X_tensor[idx]
                batch_y = y_tensor[idx]

                optimizer.zero_grad()
                output = self.model(batch_X)
                loss = criterion(output, batch_y)
                loss.backward()
                torch.nn.utils.clip_grad_norm_(self.model.parameters(), 1.0)
                optimizer.step()
                total_loss += loss.item()

            if (epoch + 1) % 10 == 0:
                logger.info(f"Epoch {epoch + 1}/{epochs}, Loss: {total_loss / len(X_tensor):.6f}")

        return self

    def predict(self, series: np.ndarray, steps: int = 30) -> np.ndarray:
        try:
            import torch
        except ImportError:
            return np.zeros(steps)

        self.model.eval()
        normalized = (series - self.scaler_mean) / self.scaler_std

        current_seq = normalized[-self.seq_length:].reshape(1, self.seq_length, 1)
        predictions = []

        with torch.no_grad():
            for _ in range(steps):
                x = torch.FloatTensor(current_seq)
                pred = self.model(x).item()
                predictions.append(pred)
                current_seq = np.roll(current_seq, -1, axis=1)
                current_seq[0, -1, 0] = pred

        return np.array(predictions) * self.scaler_std + self.scaler_mean
```

## 四、LLM赋能时间序列

### 4.1 用LLM解读时间序列

```python
import dashscope
import json
import logging
import os

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

dashscope.api_key = os.getenv("DASHSCOPE_API_KEY", "sk-xxx")


class LLMTimeSeriesAnalyzer:
    def __init__(self):
        self.model = "qwen-turbo"

    def analyze(self, series_stats: dict, forecast_result: dict = None) -> str:
        prompt = f"""你是一位时间序列分析专家。请分析以下时间序列数据：

统计信息：{json.dumps(series_stats, ensure_ascii=False, default=str)}
预测结果：{json.dumps(forecast_result, ensure_ascii=False, default=str) if forecast_result else '无'}

请给出：
1. 数据特征分析（趋势、季节性、异常点）
2. 预测结果解读
3. 业务建议"""

        response = dashscope.Generation.call(
            model=self.model,
            messages=[{"role": "user", "content": prompt}],
            result_format="message"
        )
        if response.status_code == 200:
            return response.output.choices[0].message.content
        return "分析报告生成失败"

    def detect_anomalies_description(self, anomalies: list[dict]) -> str:
        prompt = f"""以下是时间序列中检测到的异常点：

{json.dumps(anomalies, ensure_ascii=False, default=str)}

请分析这些异常点可能的原因，并给出排查建议。"""

        response = dashscope.Generation.call(
            model=self.model,
            messages=[{"role": "user", "content": prompt}],
            result_format="message"
        )
        if response.status_code == 200:
            return response.output.choices[0].message.content
        return "异常分析失败"


if __name__ == "__main__":
    df = generate_time_series()

    stats = {
        "mean": float(df["value"].mean()),
        "std": float(df["value"].std()),
        "min": float(df["value"].min()),
        "max": float(df["value"].max()),
        "trend": "上升",
        "seasonality": "存在周和年季节性"
    }

    analyzer = LLMTimeSeriesAnalyzer()
    report = analyzer.analyze(stats)
    print(report)
```

## 五、模型对比与选型

| 模型 | 适用场景 | 优势 | 劣势 |
|------|----------|------|------|
| ARIMA | 短期预测、单变量 | 可解释性强 | 需要平稳性 |
| SARIMAX | 季节性数据 | 自动处理季节性 | 参数选择复杂 |
| Prophet | 业务预测 | 自动化程度高 | 精度一般 |
| LSTM | 长期依赖 | 捕获复杂模式 | 需要大量数据 |
| Transformer | 多变量、长序列 | 全局注意力 | 计算成本高 |
| LLM辅助 | 分析解读 | 可解释、灵活 | 不直接预测 |

## 六、时间序列最佳实践

| 实践 | 说明 |
|------|------|
| 数据探索先行 | 先画图观察趋势和季节性，再选模型 |
| 基线模型 | 始终用简单模型（如移动平均）作为基线 |
| 交叉验证 | 使用时间序列专用的滚动交叉验证 |
| 多模型融合 | 结合统计模型和深度学习模型的优势 |
| 异常检测 | 预测前先处理异常值，避免影响模型 |
| 持续监控 | 模型上线后持续监控预测误差 |

## 总结

本章系统讲解了时间序列模型：

1. **基础概念**：趋势、季节性、平稳性是时间序列分析的基础
2. **经典模型**：ARIMA/SARIMAX可解释性强，适合简单场景
3. **Prophet**：Meta开源工具，自动化程度高，适合业务预测
4. **深度学习**：LSTM和Transformer捕获复杂模式，适合大规模数据
5. **LLM赋能**：用LLM解读时间序列特征和预测结果，提升可解释性

时间序列分析是AI应用的重要领域，结合传统方法和深度学习，再辅以LLM的解读能力，可以构建强大的时间序列预测系统。
