---
title: "AI 应用开发-010 Function Calling项目实战"
date: 2025-06-01T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型", "Function Calling", "项目实战"]
---

## 概述

Function Calling是大模型连接外部世界的桥梁，让AI从"只能说话"进化为"能做事"。本章将通过三个完整项目——数据库查询可视化、智能日程管理、金融投顾助手，深入实践Function Calling在真实业务场景中的应用，涵盖多工具编排、错误处理和结果可视化。

## 一、项目一：数据库查询可视化助手

### 1.1 项目目标

用户通过自然语言查询数据库，系统自动生成SQL、执行查询、并将结果可视化展示。

### 1.2 完整实现

```python
import dashscope
import sqlite3
import json
import logging
import os
from typing import Optional

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

dashscope.api_key = os.getenv("DASHSCOPE_API_KEY", "sk-xxx")


DB_SCHEMA = """
表: sales（销售记录）
- id INTEGER PRIMARY KEY
- product VARCHAR 产品名称
- category VARCHAR 类别
- amount DECIMAL 销售金额
- region VARCHAR 区域
- sale_date DATE 销售日期
"""


functions = [
    {
        "name": "query_database",
        "description": "执行SQL查询并返回结果",
        "parameters": {
            "type": "object",
            "properties": {
                "sql": {
                    "type": "string",
                    "description": "SQLite查询语句，仅支持SELECT"
                }
            },
            "required": ["sql"]
        }
    },
    {
        "name": "visualize_data",
        "description": "将查询结果可视化为图表",
        "parameters": {
            "type": "object",
            "properties": {
                "chart_type": {
                    "type": "string",
                    "enum": ["bar", "line", "pie"],
                    "description": "图表类型"
                },
                "title": {
                    "type": "string",
                    "description": "图表标题"
                },
                "x_field": {
                    "type": "string",
                    "description": "X轴字段名"
                },
                "y_field": {
                    "type": "string",
                    "description": "Y轴字段名"
                }
            },
            "required": ["chart_type", "title", "x_field", "y_field"]
        }
    }
]


def query_database(sql: str) -> str:
    dangerous = ["DROP", "DELETE", "UPDATE", "INSERT", "ALTER", "CREATE"]
    if any(kw in sql.upper() for kw in dangerous):
        return json.dumps({"error": "仅允许SELECT查询"})

    try:
        conn = sqlite3.connect("sales.db")
        conn.row_factory = sqlite3.Row
        cursor = conn.cursor()
        cursor.execute(sql)
        rows = [dict(row) for row in cursor.fetchall()]
        conn.close()
        return json.dumps(rows[:100], ensure_ascii=False, default=str)
    except Exception as e:
        return json.dumps({"error": str(e)})


def visualize_data(chart_type: str, title: str, x_field: str, y_field: str) -> str:
    import plotly.express as px
    import pandas as pd

    try:
        conn = sqlite3.connect("sales.db")
        df = pd.read_sql("SELECT * FROM sales", conn)
        conn.close()

        if chart_type == "bar":
            fig = px.bar(df, x=x_field, y=y_field, title=title)
        elif chart_type == "line":
            fig = px.line(df, x=x_field, y=y_field, title=title)
        elif chart_type == "pie":
            fig = px.pie(df, names=x_field, values=y_field, title=title)
        else:
            return json.dumps({"error": f"不支持的图表类型: {chart_type}"})

        output_path = f"output/{title.replace(' ', '_')}.html"
        fig.write_html(output_path)
        return json.dumps({"status": "success", "path": output_path})
    except Exception as e:
        return json.dumps({"error": str(e)})


FUNCTION_MAP = {
    "query_database": query_database,
    "visualize_data": visualize_data
}


def run_conversation(query: str, max_rounds: int = 5) -> str:
    messages = [{"role": "user", "content": query}]

    system_msg = {
        "role": "system",
        "content": f"""你是一个数据分析助手。你可以查询数据库并生成可视化图表。

数据库Schema：
{DB_SCHEMA}

请根据用户需求，先查询数据，再生成可视化。"""
    }
    messages.insert(0, system_msg)

    for round_num in range(max_rounds):
        response = dashscope.Generation.call(
            model="qwen-plus",
            messages=messages,
            functions=functions,
            result_format="message"
        )

        if response.status_code != 200:
            return f"API调用失败: {response.message}"

        choice = response.output.choices[0]
        message = choice.message
        messages.append(message)

        if choice.finish_reason == "function_call":
            func_call = message.function_call
            func_name = func_call["name"]
            func_args = json.loads(func_call["arguments"])

            logger.info(f"调用函数: {func_name}, 参数: {func_args}")

            func_result = FUNCTION_MAP[func_name](**func_args)

            messages.append({
                "role": "function",
                "name": func_name,
                "content": func_result
            })
        else:
            return message.content

    return "达到最大轮次限制"


def init_sales_db():
    conn = sqlite3.connect("sales.db")
    cursor = conn.cursor()
    cursor.execute("""CREATE TABLE IF NOT EXISTS sales (
        id INTEGER PRIMARY KEY, product VARCHAR, category VARCHAR,
        amount DECIMAL, region VARCHAR, sale_date DATE)""")

    import random
    random.seed(42)
    products = [
        ("笔记本电脑", "电子产品"), ("手机", "电子产品"), ("平板", "电子产品"),
        ("运动鞋", "服装"), ("羽绒服", "服装"), ("T恤", "服装"),
        ("咖啡机", "家电"), ("吸尘器", "家电"), ("微波炉", "家电")
    ]
    regions = ["华东", "华南", "华北", "西南", "华中"]

    if cursor.execute("SELECT COUNT(*) FROM sales").fetchone()[0] == 0:
        for i in range(200):
            product, category = random.choice(products)
            amount = round(random.uniform(100, 10000), 2)
            region = random.choice(regions)
            month = random.randint(1, 12)
            cursor.execute(
                "INSERT INTO sales VALUES (?,?,?,?,?,?)",
                (i + 1, product, category, amount, region, f"2025-{month:02d}-{random.randint(1,28):02d}")
            )
    conn.commit()
    conn.close()


if __name__ == "__main__":
    init_sales_db()

    queries = [
        "各产品类别的总销售额是多少？用柱状图展示",
        "华东和华南区域的销售趋势对比，用折线图展示"
    ]

    for q in queries:
        print(f"\n用户: {q}")
        result = run_conversation(q)
        print(f"助手: {result}")
```

## 二、项目二：智能日程管理助手

### 2.1 项目目标

通过自然语言管理日程，支持添加、查询、删除日程，并支持多轮对话。

### 2.2 完整实现

```python
import dashscope
import json
import logging
import os
from datetime import datetime, timedelta

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

dashscope.api_key = os.getenv("DASHSCOPE_API_KEY", "sk-xxx")


schedule_db: list[dict] = []


schedule_functions = [
    {
        "name": "add_schedule",
        "description": "添加一条日程",
        "parameters": {
            "type": "object",
            "properties": {
                "title": {"type": "string", "description": "日程标题"},
                "date": {"type": "string", "description": "日期，格式YYYY-MM-DD"},
                "time": {"type": "string", "description": "时间，格式HH:MM"},
                "duration_minutes": {"type": "integer", "description": "持续时长（分钟）"},
                "location": {"type": "string", "description": "地点（可选）"}
            },
            "required": ["title", "date", "time"]
        }
    },
    {
        "name": "query_schedule",
        "description": "查询日程",
        "parameters": {
            "type": "object",
            "properties": {
                "date": {"type": "string", "description": "查询日期，格式YYYY-MM-DD"},
                "date_range_start": {"type": "string", "description": "范围开始日期"},
                "date_range_end": {"type": "string", "description": "范围结束日期"}
            }
        }
    },
    {
        "name": "delete_schedule",
        "description": "删除日程",
        "parameters": {
            "type": "object",
            "properties": {
                "schedule_id": {"type": "integer", "description": "日程ID"}
            },
            "required": ["schedule_id"]
        }
    }
]


def add_schedule(title: str, date: str, time: str, duration_minutes: int = 60, location: str = "") -> str:
    schedule_id = len(schedule_db) + 1
    schedule = {
        "id": schedule_id,
        "title": title,
        "date": date,
        "time": time,
        "duration_minutes": duration_minutes,
        "location": location
    }
    schedule_db.append(schedule)
    logger.info(f"添加日程: {schedule}")
    return json.dumps({"status": "success", "schedule_id": schedule_id}, ensure_ascii=False)


def query_schedule(date: str = "", date_range_start: str = "", date_range_end: str = "") -> str:
    results = schedule_db
    if date:
        results = [s for s in results if s["date"] == date]
    if date_range_start and date_range_end:
        results = [s for s in results if date_range_start <= s["date"] <= date_range_end]
    results.sort(key=lambda x: (x["date"], x["time"]))
    return json.dumps(results, ensure_ascii=False)


def delete_schedule(schedule_id: int) -> str:
    global schedule_db
    original_len = len(schedule_db)
    schedule_db = [s for s in schedule_db if s["id"] != schedule_id]
    if len(schedule_db) < original_len:
        return json.dumps({"status": "success", "message": f"已删除日程ID: {schedule_id}"})
    return json.dumps({"status": "error", "message": f"未找到日程ID: {schedule_id}"})


SCHEDULE_FUNC_MAP = {
    "add_schedule": add_schedule,
    "query_schedule": query_schedule,
    "delete_schedule": delete_schedule
}


def schedule_chat(query: str, messages: list = None, max_rounds: int = 5) -> str:
    if messages is None:
        messages = [{
            "role": "system",
            "content": "你是一个智能日程管理助手。今天是2025-06-01。帮助用户管理日程，包括添加、查询和删除。"
        }]

    messages.append({"role": "user", "content": query})

    for _ in range(max_rounds):
        response = dashscope.Generation.call(
            model="qwen-plus",
            messages=messages,
            functions=schedule_functions,
            result_format="message"
        )

        if response.status_code != 200:
            return f"API调用失败: {response.message}"

        choice = response.output.choices[0]
        message = choice.message
        messages.append(message)

        if choice.finish_reason == "function_call":
            func_call = message.function_call
            func_name = func_call["name"]
            func_args = json.loads(func_call["arguments"])

            logger.info(f"调用: {func_name}({func_args})")
            result = SCHEDULE_FUNC_MAP[func_name](**func_args)

            messages.append({"role": "function", "name": func_name, "content": result})
        else:
            return message.content

    return "达到最大轮次限制"


if __name__ == "__main__":
    queries = [
        "帮我明天下午3点添加一个产品评审会议，2小时，在3号会议室",
        "明天有什么日程？",
        "帮我查一下这周的所有日程"
    ]

    messages = None
    for q in queries:
        print(f"\n用户: {q}")
        result = schedule_chat(q, messages)
        print(f"助手: {result}")
```

## 三、项目三：金融投顾助手

### 3.1 项目目标

通过Function Calling实时获取用户持仓数据和市场行情，结合Prompt工程生成个性化投顾建议。

### 3.2 完整实现

```python
import dashscope
import json
import logging
import os
import random

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

dashscope.api_key = os.getenv("DASHSCOPE_API_KEY", "sk-xxx")


finance_functions = [
    {
        "name": "get_user_portfolio",
        "description": "获取用户持仓信息",
        "parameters": {
            "type": "object",
            "properties": {
                "user_id": {"type": "string", "description": "用户ID"}
            },
            "required": ["user_id"]
        }
    },
    {
        "name": "get_stock_price",
        "description": "获取股票实时行情",
        "parameters": {
            "type": "object",
            "properties": {
                "symbol": {"type": "string", "description": "股票代码，如600519"}
            },
            "required": ["symbol"]
        }
    },
    {
        "name": "get_market_news",
        "description": "获取市场新闻",
        "parameters": {
            "type": "object",
            "properties": {
                "category": {"type": "string", "enum": ["宏观", "行业", "个股"], "description": "新闻类别"}
            }
        }
    }
]


MOCK_PORTFOLIO = {
    "user001": {
        "name": "张三",
        "holdings": [
            {"symbol": "600519", "name": "贵州茅台", "shares": 100, "cost": 1800.0},
            {"symbol": "000858", "name": "五粮液", "shares": 200, "cost": 150.0},
            {"symbol": "601318", "name": "中国平安", "shares": 500, "cost": 45.0}
        ],
        "risk_level": "稳健型"
    }
}


def get_user_portfolio(user_id: str) -> str:
    portfolio = MOCK_PORTFOLIO.get(user_id)
    if not portfolio:
        return json.dumps({"error": f"未找到用户: {user_id}"})
    return json.dumps(portfolio, ensure_ascii=False)


def get_stock_price(symbol: str) -> str:
    random.seed(hash(symbol))
    base_prices = {"600519": 1850, "000858": 155, "601318": 48}
    base = base_prices.get(symbol, random.uniform(10, 200))
    price = round(base * random.uniform(0.95, 1.05), 2)
    change = round(random.uniform(-3, 3), 2)
    return json.dumps({
        "symbol": symbol,
        "price": price,
        "change_percent": change,
        "volume": random.randint(10000, 1000000)
    }, ensure_ascii=False)


def get_market_news(category: str = "宏观") -> str:
    news_map = {
        "宏观": ["央行维持LPR不变，市场流动性充裕", "GDP增速超预期，消费复苏明显"],
        "行业": ["白酒行业Q1业绩整体向好", "保险业数字化转型加速"],
        "个股": ["贵州茅台发布年报，营收增长15%", "中国平安推出AI理赔服务"]
    }
    return json.dumps({"category": category, "headlines": news_map.get(category, [])}, ensure_ascii=False)


FINANCE_FUNC_MAP = {
    "get_user_portfolio": get_user_portfolio,
    "get_stock_price": get_stock_price,
    "get_market_news": get_market_news
}


def finance_advisor(query: str, user_id: str = "user001") -> str:
    system_prompt = f"""你是一位专业的金融投资顾问。请基于用户持仓和市场数据给出投资建议。

注意：
1. 所有投资建议仅供参考，不构成投资推荐
2. 基于实时数据给出分析
3. 考虑用户的风险偏好
4. 建议要具体可执行"""

    messages = [
        {"role": "system", "content": system_prompt},
        {"role": "user", "content": query}
    ]

    for _ in range(5):
        response = dashscope.Generation.call(
            model="qwen-plus",
            messages=messages,
            functions=finance_functions,
            result_format="message"
        )

        if response.status_code != 200:
            return "服务暂时不可用"

        choice = response.output.choices[0]
        message = choice.message
        messages.append(message)

        if choice.finish_reason == "function_call":
            func_call = message.function_call
            func_name = func_call["name"]
            func_args = json.loads(func_call["arguments"])

            if func_name == "get_user_portfolio" and "user_id" not in func_args:
                func_args["user_id"] = user_id

            result = FINANCE_FUNC_MAP[func_name](**func_args)
            messages.append({"role": "function", "name": func_name, "content": result})
        else:
            return message.content

    return "分析超时，请稍后重试"


if __name__ == "__main__":
    queries = [
        "帮我分析一下我的持仓情况，给出调整建议",
        "贵州茅台现在什么价格？值得加仓吗？",
        "最近市场有什么重要新闻？对我的持仓有什么影响？"
    ]

    for q in queries:
        print(f"\n用户: {q}")
        result = finance_advisor(q)
        print(f"顾问: {result}\n")
```

## 四、Function Calling最佳实践

### 4.1 工具设计原则

| 原则 | 说明 | 示例 |
|------|------|------|
| 单一职责 | 每个工具只做一件事 | `get_weather` vs `get_weather_and_news` |
| 清晰描述 | 函数描述要具体明确 | "查询指定城市的实时天气"而非"查天气" |
| 参数校验 | 在工具内部做参数校验 | 检查日期格式、股票代码有效性 |
| 错误处理 | 返回结构化错误信息 | `{"error": "城市不存在"}` |
| 幂等性 | 相同输入返回相同结果 | 查询类操作天然幂等 |

### 4.2 多工具编排技巧

1. **工具数量控制**：单次对话不超过10个工具，避免模型选择困难
2. **工具分组**：按场景分组工具，不同场景使用不同工具集
3. **依赖管理**：如果工具B依赖工具A的结果，在Prompt中说明执行顺序
4. **结果缓存**：对频繁调用的工具结果进行缓存

### 4.3 常见问题与解决

| 问题 | 原因 | 解决方案 |
|------|------|----------|
| 模型不调用工具 | Prompt不够明确 | 在Prompt中明确指示何时使用工具 |
| 参数格式错误 | 模型生成不规范 | 工具内部做参数校验和类型转换 |
| 循环调用 | 模型反复调用同一工具 | 设置max_rounds限制 |
| 响应慢 | 工具执行耗时 | 异步执行 + 超时控制 |

## 总结

本章通过三个实战项目深入实践了Function Calling技术：

1. **数据库查询可视化**：自然语言→SQL→执行→可视化，展示了多工具协作流程
2. **智能日程管理**：增删查改操作，展示了CRUD类Function Calling的实现
3. **金融投顾助手**：实时数据获取+分析建议，展示了数据驱动的智能决策

核心经验：Function Calling的关键在于工具设计——描述要清晰、参数要规范、错误要可控。好的工具设计可以让模型更准确地选择和调用工具。
