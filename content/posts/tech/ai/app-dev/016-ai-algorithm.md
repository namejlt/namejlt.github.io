---
title: "AI 应用开发-016 AI算法基础"
date: 2025-06-07T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型", "算法", "Transformer", "注意力机制"]
---

## 概述

理解AI算法基础是从"调包侠"进阶为"AI工程师"的必经之路。本章将系统讲解支撑现代AI的核心算法：从梯度下降到反向传播，从注意力机制到Transformer，从RLHF到DPO，帮助读者建立扎实的算法认知。

## 一、优化算法：梯度下降

### 1.1 梯度下降原理

梯度下降是机器学习最基础的优化算法，沿着损失函数梯度的反方向更新参数，逐步逼近最优解。

```python
import numpy as np
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def gradient_descent(f, grad_f, x0, learning_rate=0.01, max_iter=1000, tol=1e-6):
    x = x0
    history = [x]

    for i in range(max_iter):
        grad = grad_f(x)
        x_new = x - learning_rate * grad
        history.append(x_new)

        if abs(x_new - x) < tol:
            logger.info(f"收敛于第{i + 1}次迭代，x={x_new:.6f}")
            break
        x = x_new

    return x, history


def f(x):
    return x ** 2 + 2 * x + 1


def grad_f(x):
    return 2 * x + 2


if __name__ == "__main__":
    x_min, history = gradient_descent(f, grad_f, x0=5.0, learning_rate=0.1)
    logger.info(f"最小值点: x={x_min:.6f}, f(x)={f(x_min):.6f}")
```

### 1.2 优化器演进

| 优化器 | 年代 | 特点 |
|--------|------|------|
| SGD | 1951 | 基础随机梯度下降 |
| Momentum | 1986 | 引入动量，加速收敛 |
| AdaGrad | 2011 | 自适应学习率 |
| RMSProp | 2012 | 解决AdaGrad学习率递减问题 |
| Adam | 2015 | 结合Momentum和RMSProp，最常用 |
| AdamW | 2019 | 解耦权重衰减，训练Transformer首选 |

```python
class AdamOptimizer:
    def __init__(self, lr: float = 0.001, beta1: float = 0.9, beta2: float = 0.999, eps: float = 1e-8):
        self.lr = lr
        self.beta1 = beta1
        self.beta2 = beta2
        self.eps = eps
        self.m = 0
        self.v = 0
        self.t = 0

    def step(self, grad: float) -> float:
        self.t += 1
        self.m = self.beta1 * self.m + (1 - self.beta1) * grad
        self.v = self.beta2 * self.v + (1 - self.beta2) * grad ** 2

        m_hat = self.m / (1 - self.beta1 ** self.t)
        v_hat = self.v / (1 - self.beta2 ** self.t)

        return self.lr * m_hat / (np.sqrt(v_hat) + self.eps)


if __name__ == "__main__":
    optimizer = AdamOptimizer(lr=0.1)
    x = 5.0

    for step in range(100):
        grad = grad_f(x)
        update = optimizer.step(grad)
        x -= update

        if step % 20 == 0:
            logger.info(f"Step {step}: x={x:.6f}, f(x)={f(x):.6f}")
```

## 二、反向传播

### 2.1 计算图与链式法则

反向传播是训练神经网络的核心算法，通过链式法则高效计算损失函数对每个参数的梯度。

```python
import numpy as np


class SimpleNeuralNetwork:
    def __init__(self, input_size: int, hidden_size: int, output_size: int):
        self.w1 = np.random.randn(input_size, hidden_size) * 0.01
        self.b1 = np.zeros((1, hidden_size))
        self.w2 = np.random.randn(hidden_size, output_size) * 0.01
        self.b2 = np.zeros((1, output_size))

    def relu(self, x):
        return np.maximum(0, x)

    def relu_grad(self, x):
        return (x > 0).astype(float)

    def softmax(self, x):
        exp_x = np.exp(x - np.max(x, axis=1, keepdims=True))
        return exp_x / np.sum(exp_x, axis=1, keepdims=True)

    def forward(self, x):
        self.z1 = x @ self.w1 + self.b1
        self.a1 = self.relu(self.z1)
        self.z2 = self.a1 @ self.w2 + self.b2
        self.a2 = self.softmax(self.z2)
        return self.a2

    def backward(self, x, y, learning_rate: float = 0.01):
        m = x.shape[0]

        dz2 = self.a2 - y
        dw2 = self.a1.T @ dz2 / m
        db2 = np.sum(dz2, axis=0, keepdims=True) / m

        da1 = dz2 @ self.w2.T
        dz1 = da1 * self.relu_grad(self.z1)
        dw1 = x.T @ dz1 / m
        db1 = np.sum(dz1, axis=0, keepdims=True) / m

        self.w2 -= learning_rate * dw2
        self.b2 -= learning_rate * db2
        self.w1 -= learning_rate * dw1
        self.b1 -= learning_rate * db1

    def compute_loss(self, y_pred, y_true):
        m = y_true.shape[0]
        loss = -np.sum(y_true * np.log(y_pred + 1e-8)) / m
        return loss

    def train(self, x, y, epochs: int = 100, lr: float = 0.01):
        for epoch in range(epochs):
            y_pred = self.forward(x)
            loss = self.compute_loss(y_pred, y)
            self.backward(x, y, lr)

            if epoch % 20 == 0:
                accuracy = np.mean(np.argmax(y_pred, axis=1) == np.argmax(y, axis=1))
                logger.info(f"Epoch {epoch}: loss={loss:.4f}, accuracy={accuracy:.4f}")


if __name__ == "__main__":
    np.random.seed(42)
    x = np.random.randn(100, 4)
    y_indices = np.random.randint(0, 3, 100)
    y = np.zeros((100, 3))
    y[np.arange(100), y_indices] = 1

    nn = SimpleNeuralNetwork(4, 16, 3)
    nn.train(x, y, epochs=200, lr=0.05)
```

## 三、注意力机制与Transformer

### 3.1 自注意力机制

自注意力（Self-Attention）是Transformer的核心，让序列中的每个位置都能关注到其他所有位置。

```python
import numpy as np


def self_attention(Q: np.ndarray, K: np.ndarray, V: np.ndarray, mask: np.ndarray = None) -> np.ndarray:
    d_k = Q.shape[-1]
    scores = Q @ K.transpose(0, 2, 1) / np.sqrt(d_k)

    if mask is not None:
        scores = scores + mask * -1e9

    attention_weights = softmax(scores)
    output = attention_weights @ V
    return output


def softmax(x: np.ndarray, axis: int = -1) -> np.ndarray:
    exp_x = np.exp(x - np.max(x, axis=axis, keepdims=True))
    return exp_x / np.sum(exp_x, axis=axis, keepdims=True)


class MultiHeadAttention:
    def __init__(self, d_model: int = 512, num_heads: int = 8):
        self.d_model = d_model
        self.num_heads = num_heads
        self.d_k = d_model // num_heads

        self.W_q = np.random.randn(d_model, d_model) * 0.01
        self.W_k = np.random.randn(d_model, d_model) * 0.01
        self.W_v = np.random.randn(d_model, d_model) * 0.01
        self.W_o = np.random.randn(d_model, d_model) * 0.01

    def forward(self, x: np.ndarray) -> np.ndarray:
        batch_size, seq_len, _ = x.shape

        Q = x @ self.W_q
        K = x @ self.W_k
        V = x @ self.W_v

        Q = Q.reshape(batch_size, seq_len, self.num_heads, self.d_k).transpose(0, 2, 1, 3)
        K = K.reshape(batch_size, seq_len, self.num_heads, self.d_k).transpose(0, 2, 1, 3)
        V = V.reshape(batch_size, seq_len, self.num_heads, self.d_k).transpose(0, 2, 1, 3)

        scores = Q @ K.transpose(0, 1, 3, 2) / np.sqrt(self.d_k)
        attention_weights = softmax(scores)
        context = attention_weights @ V

        context = context.transpose(0, 2, 1, 3).reshape(batch_size, seq_len, self.d_model)
        output = context @ self.W_o
        return output


if __name__ == "__main__":
    np.random.seed(42)
    mha = MultiHeadAttention(d_model=512, num_heads=8)
    x = np.random.randn(2, 10, 512)
    output = mha.forward(x)
    logger.info(f"输入形状: {x.shape}, 输出形状: {output.shape}")
```

### 3.2 Transformer架构

```
输入 → Embedding + 位置编码 → [多头注意力 → 残差+归一化 → FFN → 残差+归一化] × N → 输出
```

| 组件 | 作用 | 关键参数 |
|------|------|----------|
| Token Embedding | 词→向量 | vocab_size, d_model |
| Position Encoding | 注入位置信息 | max_len, d_model |
| Multi-Head Attention | 捕获全局依赖 | num_heads, d_model |
| Feed-Forward | 非线性变换 | d_ff |
| Layer Norm | 稳定训练 | eps |
| Residual | 缓解梯度消失 | - |

### 3.3 Transformer变体

| 模型 | 年代 | 创新点 |
|------|------|--------|
| BERT | 2018 | 双向编码器，MLM预训练 |
| GPT | 2018-2024 | 自回归解码器，规模扩展 |
| T5 | 2020 | 编码器-解码器，统一文本任务 |
| Llama | 2023-2025 | GQA、RoPE、SwiGLU |
| Qwen3 | 2025 | MoE、GQA、长上下文 |
| DeepSeek-R1 | 2025 | MoE、MLA、推理链 |

## 四、RLHF与DPO

### 4.1 RLHF：基于人类反馈的强化学习

RLHF是大模型对齐人类偏好的核心技术，ChatGPT的成功关键。

```
步骤1：SFT（监督微调）→ 基础对话能力
步骤2：RM（奖励模型）→ 学习人类偏好
步骤3：PPO（强化学习）→ 优化模型输出
```

### 4.2 DPO：直接偏好优化

DPO绕过奖励模型，直接用偏好数据优化策略模型，更简单高效。

```python
import numpy as np


def dpo_loss(policy_chosen_logps: np.ndarray, policy_rejected_logps: np.ndarray,
             ref_chosen_logps: np.ndarray, ref_rejected_logps: np.ndarray,
             beta: float = 0.1) -> float:
    chosen_rewards = beta * (policy_chosen_logps - ref_chosen_logps)
    rejected_rewards = beta * (policy_rejected_logps - ref_rejected_logps)

    loss = -np.log(1 / (1 + np.exp(rejected_rewards - chosen_rewards)))
    return float(np.mean(loss))


if __name__ == "__main__":
    np.random.seed(42)
    n = 100

    policy_chosen = np.random.randn(n)
    policy_rejected = np.random.randn(n) - 0.5
    ref_chosen = np.random.randn(n)
    ref_rejected = np.random.randn(n) - 0.3

    loss = dpo_loss(policy_chosen, policy_rejected, ref_chosen, ref_rejected)
    logger.info(f"DPO Loss: {loss:.4f}")
```

## 五、算法选型指南

| 任务 | 推荐算法 | 理由 |
|------|----------|------|
| 文本生成 | Transformer (GPT) | 自回归，生成质量高 |
| 文本理解 | Transformer (BERT) | 双向编码，理解能力强 |
| 序列标注 | BiLSTM-CRF | 标注一致性 |
| 图像分类 | ViT / ResNet | 视觉特征提取 |
| 推荐排序 | DNN + Attention | 多特征交叉 |
| 模型对齐 | DPO | 比RLHF更简单高效 |

## 总结

本章系统讲解了AI算法基础：

1. **梯度下降**：从SGD到AdamW，优化器是训练效率的关键
2. **反向传播**：链式法则高效计算梯度，是深度学习的基石
3. **注意力机制**：自注意力让模型捕获全局依赖，是Transformer的核心
4. **Transformer**：现代大模型的统一架构，从BERT到Qwen3的演进
5. **RLHF/DPO**：大模型对齐人类偏好的两大方法，DPO更简洁高效

理解算法原理，才能在应用开发中做出正确的技术决策。
