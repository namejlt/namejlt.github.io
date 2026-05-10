---
title: "AI 应用开发-012 Agent智能体应用开发"
date: 2025-06-03T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型", "Agent", "智能体", "世界模型"]
---

## 概述

Agent（智能体）是AI应用开发的最高形态——具备感知、决策、行动、反思的自主系统。2025年，Agent技术从"概念验证"进入"生产落地"阶段，OpenAI Agents SDK、CrewAI、AutoGen等框架百花齐放，世界模型（World Model）的引入让Agent具备了环境模拟能力。

本章将系统讲解Agent的架构分类、开发框架、世界模型概念，以及从故障诊断Agent到知识客服系统的完整实战。

## 一、Agent架构分类

### 1.1 按决策方式分类

| 架构 | 特点 | 适用场景 |
|------|------|----------|
| 反应式Agent | 感知→行动，无内部状态 | 简单规则场景 |
| 深思式Agent | 感知→推理→行动，有内部模型 | 复杂决策场景 |
| 混合式Agent | 反应+深思，分层架构 | 大多数实际场景 |

### 1.2 按协作方式分类

| 模式 | 说明 | 示例 |
|------|------|------|
| 单Agent | 一个Agent完成所有任务 | 个人助手 |
| 多Agent协作 | 多个Agent分工协作 | 软件开发团队 |
| 层级式 | 管理Agent分配任务给执行Agent | 企业流程 |
| 对等式 | Agent间平等协作 | 头脑风暴 |

### 1.3 Agent核心能力

```
感知(Perception) → 记忆(Memory) → 推理(Reasoning) → 行动(Action) → 反思(Reflection)
     ↑                                                              │
     └──────────────────────────────────────────────────────────────┘
```

## 二、主流Agent框架对比

| 框架 | 语言 | 特点 | 适用场景 |
|------|------|------|----------|
| OpenAI Agents SDK | Python | OpenAI官方，轻量 | OpenAI生态 |
| LangChain Agent | Python | 生态丰富，LCEL | 通用场景 |
| CrewAI | Python | 角色扮演，多Agent | 团队协作 |
| AutoGen | Python | 微软开源，对话式 | 多Agent对话 |
| Dify | - | 低代码，可视化 | 快速搭建 |

## 三、实战：故障诊断Agent

### 3.1 项目设计

故障诊断Agent需要：收集症状→查询知识库→推理可能原因→给出解决方案。

```python
import dashscope
import json
import logging
import os
from enum import Enum
from typing import Optional

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

dashscope.api_key = os.getenv("DASHSCOPE_API_KEY", "sk-xxx")


class AgentState(Enum):
    COLLECTING = "collecting"
    DIAGNOSING = "diagnosing"
    SOLVING = "solving"
    DONE = "done"


KNOWLEDGE_BASE = {
    "服务器宕机": {
        "symptoms": ["无法访问", "ping不通", "CPU100%", "内存溢出"],
        "causes": ["内存泄漏", "磁盘满", "DDoS攻击", "进程僵死"],
        "solutions": ["重启服务", "清理磁盘", "配置防火墙", "kill僵死进程"]
    },
    "数据库慢查询": {
        "symptoms": ["查询超时", "响应慢", "CPU高", "锁等待"],
        "causes": ["缺少索引", "大表全表扫描", "连接池满", "死锁"],
        "solutions": ["添加索引", "优化SQL", "增大连接池", "杀掉死锁会话"]
    },
    "网络异常": {
        "symptoms": ["丢包", "延迟高", "连接超时", "DNS解析失败"],
        "causes": ["带宽不足", "路由环路", "DNS配置错误", "防火墙阻断"],
        "solutions": ["扩容带宽", "修复路由", "修正DNS", "调整防火墙规则"]
    }
}


class DiagnosticAgent:
    def __init__(self):
        self.state = AgentState.COLLECTING
        self.symptoms: list[str] = []
        self.diagnosis: str = ""
        self.messages = [{
            "role": "system",
            "content": """你是一个IT故障诊断专家。请按以下流程工作：
1. 收集症状：询问用户具体的故障表现
2. 诊断分析：根据症状推断可能的原因
3. 给出方案：提供具体的解决步骤

知识库信息：
""" + json.dumps(KNOWLEDGE_BASE, ensure_ascii=False, indent=2)
        }]

    def chat(self, user_input: str) -> str:
        self.messages.append({"role": "user", "content": user_input})

        try:
            response = dashscope.Generation.call(
                model="qwen-plus",
                messages=self.messages,
                result_format="message"
            )
            if response.status_code == 200:
                reply = response.output.choices[0].message.content
                self.messages.append({"role": "assistant", "content": reply})
                self._update_state(user_input, reply)
                return reply
        except Exception as e:
            logger.error(f"Agent调用失败: {e}")

        return "诊断服务暂时不可用"

    def _update_state(self, user_input: str, reply: str):
        if self.state == AgentState.COLLECTING:
            if any(kw in user_input for kw in ["无法访问", "超时", "慢", "宕机", "异常"]):
                self.symptoms.append(user_input)
                self.state = AgentState.DIAGNOSING
        elif self.state == AgentState.DIAGNOSING:
            if "原因" in reply or "可能" in reply:
                self.state = AgentState.SOLVING
        elif self.state == AgentState.SOLVING:
            if "步骤" in reply or "方案" in reply:
                self.state = AgentState.DONE


if __name__ == "__main__":
    agent = DiagnosticAgent()

    print("故障诊断Agent已启动，请描述您的故障现象：")
    interactions = [
        "我们的服务器无法访问了，ping也不通",
        "CPU使用率100%，而且内存也快满了",
        "请给出具体的解决方案"
    ]

    for msg in interactions:
        reply = agent.chat(msg)
        print(f"\n用户: {msg}")
        print(f"Agent: {reply}")
        print(f"状态: {agent.state.value}")
```

## 四、实战：知识客服系统

### 4.1 基于RAG的客服Agent

```python
import dashscope
import json
import logging
import os

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

dashscope.api_key = os.getenv("DASHSCOPE_API_KEY", "sk-xxx")


FAQ_KNOWLEDGE = [
    {"q": "如何退换货？", "a": "收到商品7天内可申请退换货，请在订单详情页点击'申请售后'，选择退换原因并提交。审核通过后，快递员将上门取件。"},
    {"q": "配送需要多长时间？", "a": "一线城市次日达，二三线城市2-3天，偏远地区3-5天。可在订单页查看实时物流。"},
    {"q": "如何使用优惠券？", "a": "结算时在'优惠券'栏输入券码或选择可用券，系统自动抵扣。注意券的使用条件和有效期。"},
    {"q": "支持哪些支付方式？", "a": "支持微信支付、支付宝、银行卡、信用卡、花呗分期等。"},
    {"q": "如何联系人工客服？", "a": "在APP中点击'我的'→'客服中心'→'转人工'，或拨打400-888-8888。"}
]


class KnowledgeServiceAgent:
    def __init__(self):
        self.conversation_history = []

    def _search_faq(self, query: str) -> list[str]:
        results = []
        for faq in FAQ_KNOWLEDGE:
            query_words = set(query)
            faq_words = set(faq["q"])
            overlap = len(query_words & faq_words)
            if overlap > 0:
                results.append(faq["a"])
        return results[:3]

    def chat(self, user_input: str) -> str:
        faq_results = self._search_faq(user_input)

        context = "\n".join(faq_results) if faq_results else "未找到相关FAQ"

        messages = [
            {"role": "system", "content": f"""你是一个专业的电商客服。请基于以下知识回答用户问题。

知识库：
{context}

规则：
1. 优先基于知识库回答
2. 如果知识库没有相关信息，诚实说明并建议转人工
3. 回答要简洁友好"""},
        ]

        messages.extend(self.conversation_history[-6:])
        messages.append({"role": "user", "content": user_input})

        try:
            response = dashscope.Generation.call(
                model="qwen-turbo",
                messages=messages,
                result_format="message"
            )
            if response.status_code == 200:
                reply = response.output.choices[0].message.content
                self.conversation_history.append({"role": "user", "content": user_input})
                self.conversation_history.append({"role": "assistant", "content": reply})
                return reply
        except Exception as e:
            logger.error(f"客服Agent失败: {e}")

        return "抱歉，服务暂时不可用，请稍后重试或联系人工客服。"


if __name__ == "__main__":
    agent = KnowledgeServiceAgent()

    questions = [
        "我想退货，怎么操作？",
        "大概多久能送到？",
        "你们支持花呗吗？",
        "我要投诉，怎么联系人工？"
    ]

    for q in questions:
        reply = agent.chat(q)
        print(f"用户: {q}")
        print(f"客服: {reply}\n")
```

## 五、世界模型与Agent的未来

### 5.1 什么是世界模型

世界模型（World Model）是Agent对环境的内部表示，让Agent能够在行动前模拟结果，从而做出更优决策。

```
传统Agent：感知 → 直接行动
世界模型Agent：感知 → 内部模拟 → 选择最优行动
```

### 5.2 世界模型的应用

| 应用 | 说明 |
|------|------|
| 游戏AI | 模拟游戏状态，规划最优策略 |
| 机器人 | 模拟物理环境，规划运动轨迹 |
| 自动驾驶 | 模拟交通场景，预测其他车辆行为 |
| 业务决策 | 模拟市场变化，评估决策影响 |

### 5.3 Agent发展趋势

1. **从规则到学习**：Agent从预定义规则转向自主学习策略
2. **从单模态到多模态**：Agent能理解文本、图像、语音等多种输入
3. **从短期到长期记忆**：Agent具备持久化记忆，跨会话保持一致性
4. **从独立到协作**：多Agent协作完成复杂任务
5. **从被动到主动**：Agent主动发现问题并提出建议

## 总结

本章系统讲解了Agent智能体应用开发：

1. **架构分类**：反应式、深思式、混合式，按场景选择合适架构
2. **框架选型**：OpenAI Agents SDK、LangChain、CrewAI各有优势
3. **故障诊断Agent**：收集→诊断→解决的完整流程
4. **知识客服Agent**：RAG + 对话记忆的客服系统
5. **世界模型**：Agent的下一步进化方向，从"反应"到"预判"

Agent是AI应用开发的终极形态，掌握Agent开发能力是成为AI应用架构师的关键。
