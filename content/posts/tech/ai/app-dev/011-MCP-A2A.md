---
title: "AI 应用开发-011 MCP协议与A2A智能体协作"
date: 2025-06-02T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型", "MCP", "A2A", "智能体协作"]
---

## 概述

MCP（Model Context Protocol）和A2A（Agent-to-Agent）是2025年AI应用开发最重要的两个协议标准。MCP规范了大模型与外部工具的连接方式，A2A定义了智能体之间的协作协议。两者结合，让AI从"单兵作战"进化为"团队协作"。

本章将深入讲解MCP协议的设计哲学、实战开发，以及A2A协议的多智能体协作机制，并通过篮球活动安排项目展示多智能体协作的完整流程。

## 一、MCP协议深度解析

### 1.1 MCP的设计哲学

Function Calling解决了"大模型如何调用工具"的问题，但没有解决"如何规范地调用工具"。MCP的核心价值在于标准化：

| 维度 | Function Calling | MCP |
|------|-----------------|-----|
| 定义方式 | 每个应用自定义 | 统一协议标准 |
| 工具发现 | 硬编码 | 动态发现 |
| 连接管理 | 手动 | 自动 |
| 安全控制 | 应用层 | 协议层 |
| 生态复用 | 不可复用 | 插件化复用 |

### 1.2 MCP核心概念

```
┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│   Host (宿主)     │     │  Client (客户端)  │     │  Server (服务端)  │
│                  │     │                  │     │                  │
│  Cursor/Claude   │────→│  协议适配层       │────→│  工具实现层       │
│  等AI应用        │     │  连接管理         │     │  数据源访问       │
│                  │     │  请求路由         │     │  能力注册         │
└──────────────────┘     └──────────────────┘     └──────────────────┘
```

MCP定义了三类核心能力：

1. **Tools（工具）**：可被LLM调用的函数
2. **Resources（资源）**：可被LLM读取的数据
3. **Prompts（提示模板）**：预定义的Prompt模板

### 1.3 MCP传输方式

| 方式 | 适用场景 | 特点 |
|------|----------|------|
| stdio | 本地开发 | 进程间通信，低延迟 |
| SSE | 远程服务 | HTTP长连接，跨网络 |
| Streamable HTTP | 最新标准 | 兼容SSE，更灵活 |

### 1.4 MCP Server开发实战

```python
from mcp.server.fastmcp import FastMCP
import requests
import logging
import os

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

mcp = FastMCP("weather-tools")

AMAP_API_KEY = os.getenv("AMAP_API_KEY", "xxx")


@mcp.tool()
def get_weather(location: str) -> str:
    """查询指定城市的实时天气信息"""
    district_url = "https://restapi.amap.com/v3/config/district"
    resp = requests.get(district_url, params={
        "key": AMAP_API_KEY, "keywords": location, "subdistrict": 0
    }, timeout=10)
    data = resp.json()

    if data.get("status") != "1" or not data.get("districts"):
        return f"未找到城市: {location}"

    adcode = data["districts"][0]["adcode"]

    weather_url = "https://restapi.amap.com/v3/weather/weatherInfo"
    resp = requests.get(weather_url, params={
        "key": AMAP_API_KEY, "city": adcode, "extensions": "base"
    }, timeout=10)
    weather_data = resp.json()

    if weather_data.get("status") == "1" and weather_data.get("lives"):
        live = weather_data["lives"][0]
        return f"{live['city']}：{live['weather']}，温度{live['temperature']}°C，{live['winddirection']}风{live['windpower']}级"

    return f"获取{location}天气失败"


@mcp.tool()
def search_restaurants(location: str, cuisine: str = "") -> str:
    """搜索指定位置的餐厅"""
    return f"在{location}搜索{'{}美食'.format(cuisine) if cuisine else '餐厅'}：推荐1.老北京涮肉 2.川味火锅 3.日式料理"


@mcp.tool()
def book_venue(venue_name: str, date: str, time: str, duration: int = 2) -> str:
    """预订运动场地"""
    return f"已预订{venue_name}，日期{date}，时间{time}，时长{duration}小时"


@mcp.resource("weather://config")
def get_weather_config() -> str:
    """获取天气服务配置信息"""
    return "天气数据源：高德地图API，更新频率：实时"


if __name__ == "__main__":
    mcp.run()
```

### 1.5 MCP Client开发

```python
from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client
import asyncio
import json
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class MCPClientWrapper:
    def __init__(self, server_script: str = "mcp_server.py"):
        self.server_params = StdioServerParameters(
            command="python",
            args=[server_script]
        )
        self.session = None
        self.available_tools = []

    async def connect(self):
        self.stdio_context = stdio_client(self.server_params)
        read_stream, write_stream = await self.stdio_context.__aenter__()

        self.session_context = ClientSession(read_stream, write_stream)
        self.session = await self.session_context.__aenter__()

        await self.session.initialize()

        tools_result = await self.session.list_tools()
        self.available_tools = tools_result.tools
        logger.info(f"已连接MCP Server，可用工具: {[t.name for t in self.available_tools]}")

    async def call_tool(self, tool_name: str, arguments: dict) -> str:
        if not self.session:
            raise RuntimeError("未连接MCP Server")

        result = await self.session.call_tool(tool_name, arguments)
        return result.content[0].text if result.content else ""

    async def disconnect(self):
        if self.session_context:
            await self.session_context.__aexit__(None, None, None)
        if self.stdio_context:
            await self.stdio_context.__aexit__(None, None, None)


async def main():
    client = MCPClientWrapper()
    await client.connect()

    weather = await client.call_tool("get_weather", {"location": "北京"})
    print(f"天气: {weather}")

    restaurants = await client.call_tool("search_restaurants", {"location": "北京", "cuisine": "火锅"})
    print(f"餐厅: {restaurants}")

    await client.disconnect()


if __name__ == "__main__":
    asyncio.run(main())
```

## 二、A2A协议：智能体间协作

### 2.1 A2A的设计动机

单个Agent的能力有限，复杂任务需要多个专业Agent协作完成。A2A（Agent-to-Agent）协议定义了Agent之间的通信和协作标准。

```
┌────────────┐    A2A协议    ┌────────────┐    A2A协议    ┌────────────┐
│ 天气Agent  │◄────────────►│ 日程Agent  │◄────────────►│ 场地Agent  │
│            │              │            │              │            │
│ 查天气     │              │ 管日程     │              │ 订场地     │
└────────────┘              └────────────┘              └────────────┘
       ▲                          ▲                          ▲
       │                          │                          │
       └──────────────────────────┼──────────────────────────┘
                                  │
                          ┌───────┴───────┐
                          │  协调Agent    │
                          │  任务分解     │
                          │  结果聚合     │
                          └───────────────┘
```

### 2.2 A2A核心概念

| 概念 | 说明 |
|------|------|
| Agent Card | Agent的能力描述卡片 |
| Task | Agent间传递的任务单元 |
| Message | 任务中的消息 |
| Part | 消息中的内容片段（文本/文件/数据） |

### 2.3 A2A协作实战：篮球活动安排

```python
import json
import logging
from dataclasses import dataclass, field
from typing import Optional
from enum import Enum

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class TaskStatus(Enum):
    PENDING = "pending"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
    FAILED = "failed"


@dataclass
class AgentCard:
    name: str
    description: str
    capabilities: list[str]
    endpoint: str = ""


@dataclass
class Task:
    id: str
    description: str
    assigned_to: str
    status: TaskStatus = TaskStatus.PENDING
    result: str = ""


class WeatherAgent:
    card = AgentCard(
        name="weather_agent",
        description="查询天气信息",
        capabilities=["get_weather"]
    )

    def execute(self, task: Task) -> Task:
        task.status = TaskStatus.IN_PROGRESS
        logger.info(f"[WeatherAgent] 执行任务: {task.description}")

        if "天气" in task.description or "天气" in task.description.lower():
            location = "上海"
            task.result = f"{location}今天晴，25°C，适合户外活动"
        else:
            task.result = "无法识别天气查询请求"

        task.status = TaskStatus.COMPLETED
        return task


class ScheduleAgent:
    card = AgentCard(
        name="schedule_agent",
        description="管理日程安排",
        capabilities=["check_availability", "add_schedule"]
    )

    def __init__(self):
        self.schedules = {
            "2025-06-05": ["09:00-10:00 团队晨会", "14:00-16:00 技术评审"],
            "2025-06-06": ["10:00-11:00 客户会议"],
            "2025-06-07": []
        }

    def execute(self, task: Task) -> Task:
        task.status = TaskStatus.IN_PROGRESS
        logger.info(f"[ScheduleAgent] 执行任务: {task.description}")

        if "空闲" in task.description or "日程" in task.description:
            free_slots = []
            for date, items in self.schedules.items():
                if not items:
                    free_slots.append(f"{date}：全天空闲")
                else:
                    free_slots.append(f"{date}：已有{len(items)}项安排")
            task.result = "日程情况：\n" + "\n".join(free_slots)
        else:
            task.result = "已添加日程安排"

        task.status = TaskStatus.COMPLETED
        return task


class VenueAgent:
    card = AgentCard(
        name="venue_agent",
        description="预订运动场地",
        capabilities=["search_venue", "book_venue"]
    )

    def execute(self, task: Task) -> Task:
        task.status = TaskStatus.IN_PROGRESS
        logger.info(f"[VenueAgent] 执行任务: {task.description}")

        if "预订" in task.description or "场地" in task.description:
            task.result = "已预订：上海体育馆篮球场，6月7日 15:00-17:00，费用200元/小时"
        else:
            task.result = "推荐场地：1.上海体育馆 2.源深体育中心 3.浦东游泳馆篮球场"

        task.status = TaskStatus.COMPLETED
        return task


class CoordinatorAgent:
    def __init__(self):
        self.weather_agent = WeatherAgent()
        self.schedule_agent = ScheduleAgent()
        self.venue_agent = VenueAgent()
        self.agents = {
            "weather": self.weather_agent,
            "schedule": self.schedule_agent,
            "venue": self.venue_agent
        }

    def plan_tasks(self, user_request: str) -> list[Task]:
        tasks = []

        if "篮球" in user_request or "运动" in user_request:
            tasks.append(Task(
                id="t1",
                description="查询活动日期的天气情况",
                assigned_to="weather"
            ))
            tasks.append(Task(
                id="t2",
                description="查看空闲日程",
                assigned_to="schedule"
            ))
            tasks.append(Task(
                id="t3",
                description="搜索并预订篮球场地",
                assigned_to="venue"
            ))

        return tasks

    def execute(self, user_request: str) -> str:
        logger.info(f"[Coordinator] 收到请求: {user_request}")

        tasks = self.plan_tasks(user_request)
        if not tasks:
            return "抱歉，我无法处理这个请求。"

        results = {}
        for task in tasks:
            agent = self.agents.get(task.assigned_to)
            if agent:
                completed_task = agent.execute(task)
                results[task.id] = completed_task.result

        summary = self._summarize(user_request, results)
        return summary

    def _summarize(self, request: str, results: dict) -> str:
        weather_info = results.get("t1", "天气信息未知")
        schedule_info = results.get("t2", "日程信息未知")
        venue_info = results.get("t3", "场地信息未知")

        return f"""篮球活动安排方案：

【天气情况】{weather_info}

【日程安排】{schedule_info}

【场地预订】{venue_info}

综合建议：天气适宜、日程空闲、场地已预订，建议按计划进行篮球活动。"""


if __name__ == "__main__":
    coordinator = CoordinatorAgent()

    request = "帮我安排一次篮球活动，看看天气和日程，订个场地"
    result = coordinator.execute(request)
    print(result)
```

## 三、MCP与A2A的协同

### 3.1 协同架构

```
用户请求 → Coordinator Agent → MCP调用各专业Agent → A2A协议协调 → 结果聚合
```

MCP解决"Agent如何调用工具"，A2A解决"Agent之间如何协作"。两者互补：

- **MCP**：Agent ↔ 工具（纵向连接）
- **A2A**：Agent ↔ Agent（横向协作）

### 3.2 实战建议

| 维度 | MCP | A2A |
|------|-----|-----|
| 关注点 | 工具连接 | Agent协作 |
| 粒度 | 函数级 | 任务级 |
| 状态 | 无状态 | 有状态 |
| 适用 | 单Agent多工具 | 多Agent协作 |

## 四、MCP生态与工具市场

### 4.1 常用MCP Server

| Server | 功能 | 安装方式 |
|--------|------|----------|
| @anthropic/mcp-filesystem | 文件系统操作 | npx安装 |
| @anthropic/mcp-github | GitHub操作 | npx安装 |
| @anthropic/mcp-postgres | PostgreSQL查询 | npx安装 |
| mcp-server-fetch | 网页抓取 | pip安装 |

### 4.2 Cursor中配置MCP

```json
{
  "mcpServers": {
    "weather": {
      "command": "python",
      "args": ["mcp_weather_server.py"],
      "env": {
        "AMAP_API_KEY": "${AMAP_API_KEY}"
      }
    },
    "database": {
      "command": "npx",
      "args": ["-y", "@anthropic/mcp-postgres", "postgresql://localhost/mydb"]
    }
  }
}
```

## 总结

本章深入讲解了MCP和A2A两个核心协议：

1. **MCP协议**：标准化大模型与外部工具的连接，支持动态发现、安全访问和插件化复用
2. **MCP开发**：FastMCP框架快速开发Server，支持Tools、Resources、Prompts三类能力
3. **A2A协议**：定义Agent间协作标准，通过Agent Card、Task、Message实现结构化通信
4. **多智能体协作**：Coordinator模式协调多个专业Agent完成复杂任务
5. **MCP + A2A**：纵向工具连接 + 横向Agent协作，构建完整的AI应用生态

MCP和A2A代表了AI应用开发从"单体"到"生态"的演进方向，是2025年最值得关注的技术趋势。
