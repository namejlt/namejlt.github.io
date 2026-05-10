---
title: "AI 应用开发-014 模型微调技术"
date: 2025-06-05T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型", "微调", "LoRA", "QLoRA"]
---

## 概述

模型微调（Fine-tuning）是在预训练模型基础上，使用特定领域数据进一步训练，使模型适配特定任务或风格的技术。与RAG的"外挂知识"不同，微调将知识"内化"到模型参数中，适合需要特定输出格式、风格或领域深度的场景。

本章将系统讲解全量微调、LoRA、QLoRA等微调方法，以及数据准备、训练流程和效果评估的完整实践。

## 一、微调方法对比

### 1.1 三种微调策略

| 方法 | 原理 | 显存需求 | 训练速度 | 效果 |
|------|------|----------|----------|------|
| 全量微调 | 更新所有参数 | 极高 | 慢 | 最好 |
| LoRA | 低秩矩阵近似 | 中等 | 快 | 接近全量 |
| QLoRA | 量化+LoRA | 低 | 较快 | 略低于LoRA |

### 1.2 何时选择微调

| 场景 | 推荐方案 | 理由 |
|------|----------|------|
| 知识更新 | RAG | 微调不适合频繁更新知识 |
| 特定输出格式 | 微调 | 格式内化到模型更稳定 |
| 领域术语理解 | 微调 | RAG难以解决术语理解问题 |
| 风格模仿 | 微调 | 写作风格需要内化 |
| 多语言适配 | 微调 | 语言能力需要参数调整 |
| 成本敏感 | QLoRA | 显存需求最低 |

## 二、数据准备

### 2.1 数据格式

微调数据通常采用指令跟随格式：

```json
{"instruction": "请将以下文本翻译为英文", "input": "今天天气真好", "output": "The weather is really nice today"}
{"instruction": "请对以下评论进行情感分析", "input": "这款产品体验非常好，强烈推荐！", "output": "正面"}
```

### 2.2 数据清洗与构建

```python
import json
import logging
import random
from pathlib import Path

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class FineTuningDataBuilder:
    def __init__(self, output_dir: str = "finetune_data"):
        self.output_dir = Path(output_dir)
        self.output_dir.mkdir(parents=True, exist_ok=True)
        self.samples = []

    def add_qa_pair(self, instruction: str, input_text: str, output_text: str):
        self.samples.append({
            "instruction": instruction,
            "input": input_text,
            "output": output_text
        })

    def add_from_conversations(self, conversations: list[dict]):
        for conv in conversations:
            if len(conv.get("conversations", [])) >= 2:
                pairs = conv["conversations"]
                for i in range(0, len(pairs) - 1, 2):
                    human_msg = pairs[i].get("value", "")
                    assistant_msg = pairs[i + 1].get("value", "") if i + 1 < len(pairs) else ""
                    if human_msg and assistant_msg:
                        self.add_qa_pair(
                            instruction="请根据用户问题给出专业回答",
                            input_text=human_msg,
                            output_text=assistant_msg
                        )

    def validate_data(self) -> dict:
        stats = {
            "total": len(self.samples),
            "empty_instruction": 0,
            "empty_output": 0,
            "too_short": 0,
            "too_long": 0
        }

        valid_samples = []
        for sample in self.samples:
            if not sample["instruction"].strip():
                stats["empty_instruction"] += 1
                continue
            if not sample["output"].strip():
                stats["empty_output"] += 1
                continue
            if len(sample["output"]) < 10:
                stats["too_short"] += 1
                continue
            if len(sample["output"]) > 4000:
                stats["too_long"] += 1
                continue
            valid_samples.append(sample)

        self.samples = valid_samples
        stats["valid"] = len(valid_samples)
        logger.info(f"数据验证: {stats}")
        return stats

    def split_and_save(self, train_ratio: float = 0.9):
        random.shuffle(self.samples)
        split_idx = int(len(self.samples) * train_ratio)

        train_data = self.samples[:split_idx]
        val_data = self.samples[split_idx:]

        train_path = self.output_dir / "train.jsonl"
        val_path = self.output_dir / "val.jsonl"

        with open(train_path, "w", encoding="utf-8") as f:
            for sample in train_data:
                f.write(json.dumps(sample, ensure_ascii=False) + "\n")

        with open(val_path, "w", encoding="utf-8") as f:
            for sample in val_data:
                f.write(json.dumps(sample, ensure_ascii=False) + "\n")

        logger.info(f"数据已保存: 训练集{len(train_data)}条, 验证集{len(val_data)}条")
        return str(train_path), str(val_path)


if __name__ == "__main__":
    builder = FineTuningDataBuilder()

    qa_pairs = [
        ("请解释什么是机器学习", "", "机器学习是人工智能的一个分支，它使计算机能够从数据中自动学习和改进，而无需显式编程。其核心思想是通过算法从历史数据中学习模式，然后对新数据做出预测或决策。"),
        ("什么是深度学习？", "", "深度学习是机器学习的一种方法，使用多层神经网络来建模数据中的复杂模式。与传统机器学习相比，深度学习能够自动提取特征，减少人工特征工程的工作量。"),
        ("RAG和微调有什么区别？", "", "RAG通过检索外部知识库来增强生成，适合知识频繁更新的场景；微调通过在特定数据上训练模型来内化知识和风格，适合需要稳定输出格式和深度领域理解的场景。"),
    ]

    for instruction, input_text, output_text in qa_pairs:
        builder.add_qa_pair(instruction, input_text, output_text)

    builder.validate_data()
    builder.split_and_save()
```

## 三、LoRA微调实战

### 3.1 LoRA原理

LoRA（Low-Rank Adaptation）的核心思想：在预训练权重矩阵旁添加低秩分解矩阵，只训练这些小矩阵，冻结原始权重。

```
原始权重 W (d×d)  →  冻结W，训练 A(d×r) × B(r×d)，r << d
输出 = W·x + (A·B)·x
```

### 3.2 使用LLaMA-Factory微调

```bash
pip install llamafactory
```

```yaml
model_name_or_path: Qwen/Qwen2.5-7B-Instruct
stage: sft
do_train: true
finetuning_type: lora
lora_target: all
lora_rank: 8
lora_alpha: 16
dataset: custom_data
template: qwen
cutoff_len: 1024
max_samples: 10000
overwrite_cache: true
preprocessing_num_workers: 4
per_device_train_batch_size: 2
gradient_accumulation_steps: 8
lr_scheduler_type: cosine
logging_steps: 10
warmup_steps: 50
num_train_epochs: 3
save_steps: 500
learning_rate: 1.0e-4
fp16: true
output_dir: saves/qwen2.5-7b-lora
```

```bash
llamafactory-cli train config/lora_sft.yaml
```

### 3.3 Python代码实现LoRA

```python
from transformers import AutoModelForCausalLM, AutoTokenizer
from peft import LoraConfig, get_peft_model, TaskType
import torch
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def setup_lora_model(model_path: str = "Qwen/Qwen2.5-7B-Instruct"):
    tokenizer = AutoTokenizer.from_pretrained(model_path, trust_remote_code=True)
    model = AutoModelForCausalLM.from_pretrained(
        model_path,
        torch_dtype=torch.float16,
        device_map="auto",
        trust_remote_code=True
    )

    lora_config = LoraConfig(
        task_type=TaskType.CAUSAL_LM,
        r=8,
        lora_alpha=16,
        lora_dropout=0.05,
        target_modules=["q_proj", "v_proj", "k_proj", "o_proj", "gate_proj", "up_proj", "down_proj"],
        bias="none"
    )

    model = get_peft_model(model, lora_config)
    model.print_trainable_parameters()

    return model, tokenizer


def inference_with_lora(model, tokenizer, query: str, max_new_tokens: int = 512):
    messages = [{"role": "user", "content": query}]
    text = tokenizer.apply_chat_template(messages, tokenize=False, add_generation_prompt=True)
    inputs = tokenizer(text, return_tensors="pt").to(model.device)

    with torch.no_grad():
        outputs = model.generate(
            **inputs,
            max_new_tokens=max_new_tokens,
            temperature=0.7,
            top_p=0.9,
            do_sample=True
        )

    response = tokenizer.decode(outputs[0][inputs["input_ids"].shape[1]:], skip_special_tokens=True)
    return response


if __name__ == "__main__":
    model, tokenizer = setup_lora_model()
    result = inference_with_lora(model, tokenizer, "什么是机器学习？")
    print(result)
```

## 四、QLoRA：更低显存的微调

### 4.1 QLoRA原理

QLoRA在LoRA基础上引入4-bit量化，将预训练模型量化到4-bit存储，训练时反量化到bf16计算，进一步降低显存需求。

```python
from transformers import BitsAndBytesConfig


def setup_qlora_model(model_path: str = "Qwen/Qwen2.5-7B-Instruct"):
    tokenizer = AutoTokenizer.from_pretrained(model_path, trust_remote_code=True)

    bnb_config = BitsAndBytesConfig(
        load_in_4bit=True,
        bnb_4bit_quant_type="nf4",
        bnb_4bit_compute_dtype=torch.bfloat16,
        bnb_4bit_use_double_quant=True
    )

    model = AutoModelForCausalLM.from_pretrained(
        model_path,
        quantization_config=bnb_config,
        device_map="auto",
        trust_remote_code=True
    )

    lora_config = LoraConfig(
        task_type=TaskType.CAUSAL_LM,
        r=8,
        lora_alpha=16,
        lora_dropout=0.05,
        target_modules=["q_proj", "v_proj"],
        bias="none"
    )

    model = get_peft_model(model, lora_config)
    model.print_trainable_parameters()

    return model, tokenizer
```

### 4.2 显存需求对比

| 模型 | 全量微调 | LoRA | QLoRA |
|------|----------|------|-------|
| 7B | ~60GB | ~20GB | ~10GB |
| 14B | ~120GB | ~40GB | ~20GB |
| 72B | ~600GB | ~160GB | ~48GB |

## 五、微调效果评估

### 5.1 评估指标

| 指标 | 说明 | 计算方式 |
|------|------|----------|
| Loss | 训练损失 | 交叉熵 |
| Perplexity | 困惑度 | exp(loss) |
| BLEU | 生成质量 | n-gram匹配 |
| ROUGE | 摘要质量 | 召回率 |
| 人工评估 | 综合质量 | 人工打分 |

### 5.2 评估脚本

```python
import json
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def evaluate_model(model, tokenizer, eval_data_path: str, max_samples: int = 100):
    with open(eval_data_path, "r", encoding="utf-8") as f:
        eval_data = [json.loads(line) for line in f][:max_samples]

    correct = 0
    total = 0

    for sample in eval_data:
        instruction = sample["instruction"]
        input_text = sample.get("input", "")
        expected = sample["output"]

        query = f"{instruction}\n{input_text}" if input_text else instruction
        prediction = inference_with_lora(model, tokenizer, query, max_new_tokens=256)

        overlap = len(set(prediction) & set(expected)) / max(len(set(expected)), 1)
        if overlap > 0.5:
            correct += 1
        total += 1

    accuracy = correct / total if total > 0 else 0
    logger.info(f"评估结果: {correct}/{total} = {accuracy:.2%}")
    return accuracy
```

## 六、微调最佳实践

### 6.1 数据质量 > 数据数量

| 数据量 | 质量 | 效果 |
|--------|------|------|
| 100条 | 高质量 | 可用 |
| 1000条 | 高质量 | 良好 |
| 10000条 | 高质量 | 优秀 |
| 100000条 | 低质量 | 可能下降 |

### 6.2 超参数建议

| 参数 | LoRA建议 | QLoRA建议 |
|------|----------|-----------|
| lora_rank | 8-64 | 8-32 |
| lora_alpha | 16-32 | 16-32 |
| learning_rate | 1e-4 | 2e-4 |
| epochs | 3-5 | 3-5 |
| batch_size | 2-8 | 2-4 |
| warmup_steps | 50-100 | 50-100 |

### 6.3 常见问题

| 问题 | 原因 | 解决方案 |
|------|------|----------|
| 过拟合 | 数据太少 | 增加数据、减少epoch、增大dropout |
| 灾难性遗忘 | 学习率太大 | 降低学习率、减少epoch |
| 训练不稳定 | 数据质量差 | 清洗数据、降低学习率 |
| 显存不足 | 模型太大 | 使用QLoRA、减小batch_size |

## 总结

本章系统讲解了模型微调技术：

1. **微调策略**：全量微调、LoRA、QLoRA各有适用场景，QLoRA是性价比最高的选择
2. **数据准备**：指令跟随格式，数据质量比数量更重要
3. **LoRA实战**：低秩矩阵近似，只训练0.1%的参数即可获得接近全量微调的效果
4. **QLoRA**：4-bit量化+LoRA，7B模型仅需10GB显存
5. **效果评估**：Loss、Perplexity、BLEU等指标结合人工评估

微调是将通用大模型转化为领域专家的关键技术，与RAG互补，共同构成AI应用的技术底座。
