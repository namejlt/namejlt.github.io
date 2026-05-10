---
title: "AI 应用开发-009 LangChain开发框架"
date: 2025-05-31T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型", "LangChain", "智能体"]
---

## 概述

LangChain是目前最流行的LLM应用开发框架，提供了模型调用、Prompt管理、链式编排、记忆机制、工具调用、Agent等核心抽象，让开发者能够快速构建复杂的AI应用。本章将系统讲解LangChain的核心组件和实战应用，包括Memory记忆机制、Chains链式调用、Agent智能体开发，以及与Qwen-Agent的对比。

## 一、LangChain核心架构

### 1.1 模块体系

```
LangChain
├── Model I/O        # 模型输入输出（LLM、ChatModel、Prompt模板）
├── Retrieval        # 检索增强（文档加载、分块、Embedding、向量库）
├── Chains           # 链式调用（顺序链、路由链、转换链）
├── Memory           # 记忆机制（对话记忆、摘要记忆）
├── Agents           # 智能体（ReAct、OpenAI Functions Agent）
└── Callbacks        # 回调机制（日志、追踪、流式输出）
```

### 1.2 安装与基础配置

```bash
pip install langchain langchain-community langchain-openai
```

```python
import os
from langchain_openai import ChatOpenAI
from langchain_community.chat_models import ChatTongyi

os.environ["DASHSCOPE_API_KEY"] = "sk-xxx"

llm = ChatTongyi(model="qwen-turbo")

response = llm.invoke("什么是机器学习？")
print(response.content)
```

## 二、Prompt模板与链式调用

### 2.1 PromptTemplate

```python
from langchain_core.prompts import ChatPromptTemplate, MessagesPlaceholder
from langchain_core.output_parsers import StrOutputParser


prompt = ChatPromptTemplate.from_messages([
    ("system", "你是一位{role}，请用专业但易懂的语言回答问题。"),
    ("human", "{question}")
])

chain = prompt | llm | StrOutputParser()

result = chain.invoke({
    "role": "数据科学家",
    "question": "如何选择合适的机器学习算法？"
})
print(result)
```

### 2.2 对话Prompt与历史

```python
from langchain_core.prompts import ChatPromptTemplate, MessagesPlaceholder
from langchain_core.messages import HumanMessage, AIMessage

chat_prompt = ChatPromptTemplate.from_messages([
    ("system", "你是一个友好的AI助手，名叫小智。"),
    MessagesPlaceholder(variable_name="history"),
    ("human", "{input}")
])

history = [
    HumanMessage(content="你好，我叫张三"),
    AIMessage(content="你好张三！很高兴认识你，有什么我可以帮你的吗？")
]

chain = chat_prompt | llm | StrOutputParser()
result = chain.invoke({"history": history, "input": "我叫什么名字？"})
print(result)
```

### 2.3 顺序链与路由链

```python
from langchain_core.output_parsers import JsonOutputParser
from langchain_core.pydantic_v1 import BaseModel, Field


class AnalysisResult(BaseModel):
    sentiment: str = Field(description="情感倾向：正向/负向/中性")
    keywords: list[str] = Field(description="关键词列表")
    summary: str = Field(description="一句话摘要")


analysis_prompt = ChatPromptTemplate.from_template(
    "分析以下文本的情感、关键词和摘要：\n\n{text}\n\n{format_instructions}"
)

parser = JsonOutputParser(pydantic_object=AnalysisResult)

chain = analysis_prompt | llm | parser

result = chain.invoke({
    "text": "这款产品体验非常好，功能强大，界面美观，推荐购买！",
    "format_instructions": parser.get_format_instructions()
})
print(result)
```

## 三、Memory记忆机制

### 3.1 对话记忆类型

| 类型 | 说明 | 适用场景 |
|------|------|----------|
| ConversationBufferMemory | 保存完整对话历史 | 短对话 |
| ConversationBufferWindowMemory | 保留最近K轮对话 | 中等对话 |
| ConversationSummaryMemory | LLM摘要历史对话 | 长对话 |
| VectorStoreRetrieverMemory | 向量检索相关记忆 | 大规模知识 |

### 3.2 实战：带记忆的客服系统

```python
from langchain.chains import ConversationChain
from langchain.memory import ConversationBufferWindowMemory
from langchain_community.chat_models import ChatTongyi
from langchain_core.prompts import ChatPromptTemplate, MessagesPlaceholder


class CustomerServiceBot:
    def __init__(self):
        self.llm = ChatTongyi(model="qwen-turbo", temperature=0.7)
        self.memory = ConversationBufferWindowMemory(
            k=5,
            return_messages=True
        )

        self.prompt = ChatPromptTemplate.from_messages([
            ("system", """你是一个专业的客服代表，名叫小智。你的职责是：
1. 友好地回答客户问题
2. 记住客户之前提到的重要信息
3. 如果不确定，诚实地说明并建议转人工
4. 回答要简洁专业"""),
            MessagesPlaceholder(variable_name="history"),
            ("human", "{input}")
        ])

        self.chain = self.prompt | self.llm

    def chat(self, user_input: str) -> str:
        history = self.memory.load_memory_variables({}).get("history", [])

        response = self.chain.invoke({
            "history": history,
            "input": user_input
        })

        self.memory.save_context(
            {"input": user_input},
            {"output": response.content}
        )

        return response.content


if __name__ == "__main__":
    bot = CustomerServiceBot()

    conversations = [
        "你好，我叫张三，我的订单号是ORD20250501",
        "我想查询这个订单的物流状态",
        "大概什么时候能到？",
        "我刚才说的订单号是多少？"  # 测试记忆
    ]

    for msg in conversations:
        reply = bot.chat(msg)
        print(f"用户: {msg}")
        print(f"客服: {reply}\n")
```

## 四、工具调用与Agent

### 4.1 自定义工具

```python
from langchain_core.tools import tool
import requests
import os
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


@tool
def get_weather(location: str) -> str:
    """查询指定城市的天气信息"""
    amap_key = os.getenv("AMAP_API_KEY", "xxx")

    district_url = "https://restapi.amap.com/v3/config/district"
    resp = requests.get(district_url, params={
        "key": amap_key, "keywords": location, "subdistrict": 0, "output": "JSON"
    }, timeout=10)
    data = resp.json()

    if data.get("status") != "1" or not data.get("districts"):
        return f"未找到城市: {location}"

    adcode = data["districts"][0]["adcode"]

    weather_url = "https://restapi.amap.com/v3/weather/weatherInfo"
    resp = requests.get(weather_url, params={
        "key": amap_key, "city": adcode, "extensions": "base", "output": "JSON"
    }, timeout=10)
    weather_data = resp.json()

    if weather_data.get("status") == "1" and weather_data.get("lives"):
        live = weather_data["lives"][0]
        return f"{live['province']}{live['city']}：{live['weather']}，温度{live['temperature']}°C，{live['winddirection']}风{live['windpower']}级，湿度{live['humidity']}%"

    return f"获取{location}天气失败"


@tool
def calculate(expression: str) -> str:
    """计算数学表达式，如 '2+3*4'"""
    try:
        allowed = set("0123456789+-*/().% ")
        if not all(c in allowed for c in expression):
            return "表达式包含不安全字符"
        result = eval(expression)
        return f"计算结果: {result}"
    except Exception as e:
        return f"计算错误: {e}"


@tool
def search_knowledge(query: str) -> str:
    """从知识库中搜索相关信息"""
    return f"知识库搜索结果：关于'{query}'的相关信息..."
```

### 4.2 ReAct Agent

```python
from langchain.agents import create_react_agent, AgentExecutor
from langchain_core.prompts import PromptTemplate
from langchain_community.chat_models import ChatTongyi


REACT_PROMPT = PromptTemplate.from_template("""你是一个有用的AI助手，可以使用工具来回答问题。

可用工具：
{tools}

工具名称: {tool_names}

请严格按照以下格式回答：

问题: 你需要回答的问题
思考: 你应该怎么做
行动: 要使用的工具名称（必须是 [{tool_names}] 中的一个）
行动输入: 工具的输入参数
观察: 工具的返回结果
... (思考/行动/行动输入/观察 可以重复多次)
思考: 我现在知道最终答案了
最终答案: 对原始问题的最终回答

开始！

问题: {input}
思考: {agent_scratchpad}""")


def create_agent():
    llm = ChatTongyi(model="qwen-plus", temperature=0)

    tools = [get_weather, calculate, search_knowledge]

    agent = create_react_agent(llm, tools, REACT_PROMPT)

    agent_executor = AgentExecutor(
        agent=agent,
        tools=tools,
        verbose=True,
        max_iterations=5,
        handle_parsing_errors=True
    )

    return agent_executor


if __name__ == "__main__":
    agent = create_agent()

    result = agent.invoke({"input": "上海今天天气怎么样？如果温度是25度，那么25*1.8+32是多少？"})
    print(f"\n最终答案: {result['output']}")
```

## 五、LangChain vs Qwen-Agent对比

### 5.1 框架对比

| 维度 | LangChain | Qwen-Agent |
|------|-----------|------------|
| 定位 | 通用LLM应用框架 | 阿里云原生Agent框架 |
| 模型支持 | 多模型（OpenAI、Qwen等） | 深度集成Qwen系列 |
| Agent类型 | ReAct、OpenAI Functions | ReAct、Code Interpreter |
| RAG能力 | 丰富（多种检索器） | 内置多级RAG |
| 生态 | 庞大社区 | 阿里云生态 |
| 学习曲线 | 中等 | 较低 |
| 中文优化 | 一般 | 优秀 |

### 5.2 选型建议

- **通用场景**：LangChain，生态丰富，社区活跃
- **阿里云用户**：Qwen-Agent，深度集成，中文优化好
- **快速原型**：Qwen-Agent，开箱即用
- **复杂编排**：LangChain，链式调用更灵活

## 六、LangGraph：下一代Agent框架

### 6.1 为什么需要LangGraph

LangChain的Agent是线性的（思考→行动→观察循环），无法表达复杂的分支、并行和条件逻辑。LangGraph基于图结构，支持更复杂的Agent工作流。

```python
from langgraph.graph import StateGraph, END
from typing import TypedDict, Annotated
import operator


class AgentState(TypedDict):
    messages: Annotated[list, operator.add]
    current_step: str
    retry_count: int


def decide_route(state: AgentState) -> str:
    last_msg = state["messages"][-1] if state["messages"] else ""
    if "需要检索" in last_msg:
        return "retrieve"
    elif "需要计算" in last_msg:
        return "calculate"
    else:
        return "respond"


def retrieve_node(state: AgentState) -> AgentState:
    state["messages"].append("执行知识库检索...")
    state["current_step"] = "retrieved"
    return state


def calculate_node(state: AgentState) -> AgentState:
    state["messages"].append("执行计算...")
    state["current_step"] = "calculated"
    return state


def respond_node(state: AgentState) -> AgentState:
    state["messages"].append("生成最终回答")
    state["current_step"] = "done"
    return state


workflow = StateGraph(AgentState)

workflow.add_node("router", lambda s: s)
workflow.add_node("retrieve", retrieve_node)
workflow.add_node("calculate", calculate_node)
workflow.add_node("respond", respond_node)

workflow.set_entry_point("router")

workflow.add_conditional_edges("router", decide_route, {
    "retrieve": "retrieve",
    "calculate": "calculate",
    "respond": "respond"
})

workflow.add_edge("retrieve", "respond")
workflow.add_edge("calculate", "respond")
workflow.add_edge("respond", END)

app = workflow.compile()

result = app.invoke({
    "messages": ["需要检索关于机器学习的知识"],
    "current_step": "start",
    "retry_count": 0
})
print(result)
```

## 总结

本章系统讲解了LangChain开发框架的核心组件：

1. **Prompt模板**：参数化Prompt管理，支持对话历史和格式化输出
2. **链式调用**：LCEL表达式语法，简洁高效地编排调用链
3. **Memory记忆**：多种记忆策略，让Agent具备上下文理解能力
4. **工具调用**：自定义Tool + ReAct Agent，实现自主决策和工具使用
5. **LangGraph**：基于图的Agent框架，支持复杂工作流编排

LangChain是AI应用开发的基础设施，掌握其核心概念对构建复杂AI系统至关重要。下一章将进入Function Calling项目实战，将LangChain与具体业务场景结合。
