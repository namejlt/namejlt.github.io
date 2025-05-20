---
title: "AI 应用开发-001 API Function Call"
date: 2025-05-20T09:00:00+08:00
draft: false
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型"]
---

## 概述

AI应用开发最基础的是如何调用大模型API，以及如何使用Function Call机制。在这一阶段中，学习者将学习如何使用Python调用大模型API，例如使用DashScope平台完成情感分析、表格提取等任务，同时理解Function Call机制在工具调用中的关键作用。

未来MCP（多智能体协作协议）将成为未来AI应用开发的核心技术。通过Function Calling机制，智能体可以调用外部工具，但是没有定义如何去调用，MCP就是规范了这个调用外部工具的协议。

应用开发基本也解决了大模型的几个薄弱点，包括：

- 上下文记忆能力
- 最新信息获取能力
- 专业领域知识能力

2025年，AI应用开发趋势主要是agent开发，更适应环境的agent。

## 零、大模型历史和定义

### 大模型的历史发展

#### 1. 早期基础（2017年之前）

- 传统神经网络时代
- Word2Vec、GloVe等词向量模型
- 注意力机制的提出

#### 2. Transformer革命（2017）

- Google发布Transformer架构论文
- 自注意力机制带来突破性进展
- 为大规模语言模型奠定基础

#### 3. 预训练模型时代（2018-2019）

- BERT的出现开启预训练时代
- GPT-1展示生成式模型潜力
- RoBERTa等模型不断优化性能

#### 4. 规模化时代（2020-2022）

- GPT-3展示大规模语言模型能力
- PaLM、BLOOM等超大规模模型涌现
- 中文模型百川、文心等快速发展

#### 5. 多模态融合时代（2022至今）

- GPT-4开启多模态理解新纪元
- Claude等展示更强的推理能力
- 国内外大模型百花齐放

### 大模型的定义

#### 核心特征

1. **规模巨大**
   - 参数量通常在十亿级以上(人脑250万亿)
   - 训练数据规模庞大
   - 计算资源需求高

2. **架构特点**
   - 基于Transformer架构
   - 采用自注意力机制
   - 多层深度神经网络

3. **能力表现**
   - 强大的自然语言理解能力
   - 上下文学习能力
   - 零样本/少样本学习能力
   - 指令跟随能力

#### 主要类型

1. **基础模型**
   - 通用知识储备
   - 基础语言理解能力
   - 作为下游任务基础

2. **领域专用模型**
   - 特定领域知识深化
   - 专业任务优化
   - 垂直场景应用

3. **多模态模型**
   - 文本、图像理解
   - 跨模态转换能力
   - 多模态协同推理

#### 应用特点

1. **通用性**
   - 适应多种任务场景
   - 迁移学习能力强
   - 知识泛化能力好

2. **可扩展性**
   - 支持微调优化
   - 能力边界可拓展
   - 应用场景丰富

3. **交互性**
   - 自然语言交互
   - 上下文理解准确
   - 响应合理连贯

总而言之，大模型是通过预训练-微调范式，结合监督学习、自监督学习和强化学习等方式，利用大规模数据和计算资源，实现对人类语言的深度理解和生成能力。其核心是基于Transformer架构的注意力机制，通过学习上下文的概率分布来预测下一个token，从而实现语言理解、生成、推理等多方面能力。

## 一、API 使用

大模型是调用的核心，它有其调用规范，最具影响力的API规范是OpenAI的API规范，它的API规范是基于HTTP协议的，使用JSON格式的数据进行交互。

主要参数规范：

- 输入：prompt/messages、system prompt、temperature等
- 输出：completion/response、tokens、usage等
- 功能：stream、function calling、tool use等

其他厂商都在一定程度上参考和兼容这一规范，但同时也会根据自身特点进行创新和扩展。这种"兼容+创新"的方式，既保证了生态的统一性，又推动了接口能力的持续进化。

API使用基本按照入参规范调用即可，相关平台已经提供了SDK，开发者可以直接使用。

API通常以HTTP方式提供，以下一些API调用例子：

### API 使用 代码示例

#### 舆情分析示例

- 定义大模型角色
- 定义用户输入
- 调用大模型API
- 解析API返回结果

```python
import dashscope
import os
import logging

# 设置日志格式
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')

# 从环境变量中读取 API Key，避免硬编码
DASHSCOPE_API_KEY = os.getenv("DASHSCOPE_API_KEY", "sk-xxx")
dashscope.api_key = DASHSCOPE_API_KEY


def get_response(messages):
    """
    调用 DashScope 模型生成接口获取响应
    :param messages: 消息列表
    :return: 模型响应对象或 None（出错时）
    """
    try:
        response = dashscope.Generation.call(
            model='qwen-turbo',
            messages=messages,
            result_format='message'
        )
        return response
    except Exception as e:
        logging.error(f"调用模型失败: {e}")
        return None


def get_response_result(review):
    """
    获取模型对评论的情感分析结果
    :param review: 用户评论内容
    """
    messages = [
        {"role": "system", "content": "你是一名舆情分析师，帮我判断产品口碑的正负向，回复请用一个词语：正向 或者 负向 或者 中性"},
        {"role": "user", "content": review}
    ]

    response = get_response(messages)

    if response and hasattr(response, 'output') and response.output.choices:
        try:
            content = response.output.choices[0].message.content.strip()
            logging.info(f"评论: '{review}' -> 分析结果: {content}")
        except (IndexError, AttributeError) as e:
            logging.error(f"解析响应内容失败: {e}")
    else:
        logging.warning(f"模型未返回有效结果，评论内容: '{review}'")


# 测试调用
if __name__ == "__main__":
    get_response_result('这款音效特别好 给你意想不到的音质。')
    get_response_result('你谁啊？为什么在这')
    get_response_result('淘宝是一个平台')
    get_response_result('京东外卖和美团pk，京东给五险一金，美团没有')
    get_response_result('功能有些少啊')

```

运行结果：

```shell
2025-05-20 09:41:16,778 - INFO - 评论: '这款音效特别好 给你意想不到的音质。' -> 分析结果: 正向
2025-05-20 09:41:17,206 - INFO - 评论: '你谁啊？为什么在这' -> 分析结果: 中性
2025-05-20 09:41:17,794 - INFO - 评论: '淘宝是一个平台' -> 分析结果: 中性
2025-05-20 09:41:18,332 - INFO - 评论: '京东外卖和美团pk，京东给五险一金，美团没有' -> 分析结果: 正向
2025-05-20 09:41:18,927 - INFO - 评论: '功能有些少啊' -> 分析结果: 负向
```

#### 图片生成示例

- 调用多模态大模型接口
- 入参：prompt、negative prompt、n、size等
- 解析API返回结果

```python
from http import HTTPStatus
from urllib.parse import urlparse, unquote
from pathlib import PurePosixPath
import requests
from dashscope import ImageSynthesis
import os


def extract_filename_from_url(url: str) -> str:
    """从图片 URL 中提取文件名"""
    path = urlparse(url).path
    decoded_path = unquote(path)
    return PurePosixPath(decoded_path).parts[-1]


prompt = "电影画质，明亮，一间有着精致窗户的花店，漂亮的木质门，摆放着花朵"
negative_prompt = "人物"

print('----sync call, please wait a moment----')
rsp = ImageSynthesis.call(
    api_key=os.getenv("DASHSCOPE_API_KEY", "sk-xxx"),
    model="wanx2.1-t2i-turbo",
    prompt=prompt,
    negative_prompt=negative_prompt,
    n=1,
    size='1024*1024'
)

print('response: %s' % rsp)

if rsp.status_code == HTTPStatus.OK:
    # 检查是否有输出结果
    if hasattr(rsp.output, 'results') and isinstance(rsp.output.results, list):
        for result in rsp.output.results:
            try:
                file_name = extract_filename_from_url(result.url)
                file_path = os.path.join('.', file_name)

                print(f'Downloading image to {file_path}...')
                response = requests.get(result.url, timeout=10)
                response.raise_for_status()  # 检查 HTTP 错误状态码

                with open(file_path, 'wb') as f:
                    f.write(response.content)
            except requests.exceptions.RequestException as e:
                print(f"Failed to download image from {result.url}: {e}")
    else:
        print("No results found in the response.")
else:
    print('sync_call Failed, status_code: %s, code: %s, message: %s' %
          (rsp.status_code, rsp.code, rsp.message))

```

运行结果：
![flower](/imgs/tech/ai/app-dev/flower.png)

## 二、Function Call

Function Call 是一种通过调用外部函数来实现任务的机制。它允许 AI 模型在执行任务时调用外部函数，而不是直接生成结果。

它解决了大模型无法实时获取最新信息、专业领域知识和执行复杂任务的问题。同时它提供大模型**自动适应各种环境**的能力，让大模型更智能。

Function Call 机制的核心思想是：

1. **定义函数**：首先，开发者需要定义一组函数，这些函数可以被 AI 模型调用。每个函数都有一个名称和一组参数。
2. **调用函数**：当 AI 模型需要执行某个任务时，它会生成一个调用函数的指令。例如，它可能会生成一个指令，要求调用名为 "get_weather" 的函数，并传递参数 "New York"。
3. **执行函数**：一旦 AI 模型生成了调用函数的指令，它会将该指令传递给外部函数。外部函数会根据指令的要求执行相应的操作，并返回结果。
4. **返回结果**：外部函数执行完毕后，会将结果返回给 AI 模型。AI 模型可以使用这个结果来完成后续的任务。

Function Call 机制的优点包括：

- **灵活性**：通过 Function Call，AI 模型可以根据需要调用不同的函数，从而实现不同的任务。
- **可扩展性**：开发者可以轻松地添加新的函数，以扩展 AI 模型的功能。
- **可维护性**：代码结构清晰，易于维护和扩展。
- **安全性**：AI 模型可以调用外部函数，而不需要直接执行代码，从而提高了系统的安全性。

Function Call 机制的应用场景包括：

- **任务自动化**：AI 模型可以根据需要调用外部函数，从而实现自动化的任务。
- **插件系统**：AI 模型可以调用外部函数，从而实现插件系统，从而实现自定义的功能。
- **集成其他系统**：AI 模型可以调用外部函数，从而实现与其他系统的集成。

### Function Call 代码示例

#### 天气查询示例

- 定义查询天气函数（从高德API获取）
- 定义用户输入
- 调用大模型API（传入function定义）
- 大模型会根据情况决定是否调用函数
- 调用函数获取结果
- 大模型根据函数结果生成最终结果

```python
import json
import os
import logging

import dashscope
import requests

# 初始化日志
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# 设置 API Key
dashscope.api_key = "sk-xxx"
AMAP_API_KEY = os.getenv("AMAP_API_KEY", "xxx")
amap_api_key = AMAP_API_KEY


def get_current_weather(location, api_key=amap_api_key):
    """
    使用高德地图SDK查询指定位置的天气信息
    :param location: str, 要查询的地点名称或坐标（如 "北京" 或 "116.397428,39.90923"）
    :param api_key: str, 高德地图开发者 API Key
    :return: dict, 包含天气信息的字典；失败时返回 None
    """
    base_url = "https://restapi.amap.com/v3/weather/weatherInfo"
    city_code = get_adcode_by_city_name(location)
    if  not city_code:
        logger.warning("无法获取城市代码")
        return None
    params = {
        "key": api_key,
        "city": city_code,
        "extensions": "base",  # 返回实况天气
        "output": "JSON"
    }

    try:
        response = requests.get(base_url, params=params, timeout=10)
        response.raise_for_status()
        data = response.json()

        if data.get("status") == "1" and isinstance(data.get("lives"), list) and len(data["lives"]) > 0:
            return data["lives"][0]
        else:
            logger.warning("无法获取天气信息: %s", data.get("info", "未知错误"))
            return None
    except requests.RequestException as e:
        logger.error("请求天气数据失败: %s", e)
        return None


def get_response(messages):
    try:
        response = dashscope.Generation.call(
            model='qwen-turbo',
            messages=messages,
            functions=functions,
            result_format='message'
        )
        return response
    except Exception as e:
        logger.error("API调用出错: %s", str(e))
        return None


def run_conversation(query):
    messages = [{"role": "user", "content": query}]

    logger.info(f"第一次请求: {messages}")
    # 第一次响应
    response = get_response(messages)
    if not response or not response.output:
        logger.error("获取响应失败")
        return None
    logger.info(f"第一次响应: {response}")
    message = response.output.choices[0].message
    messages.append(message)

    # Step 2, 判断是否需要调用函数
    if hasattr(message, 'function_call') and message.function_call:
        function_call = message.function_call
        tool_name = function_call['name']
        arguments = json.loads(function_call['arguments'])
        logger.info('arguments=%s', arguments)

        # Step 3, 执行函数调用
        tool_response = get_current_weather(
            location=arguments.get('location'),
        )

        if tool_response is None:
            logger.error("获取天气信息失败")
            return None

        tool_info = {"role": "function", "name": tool_name, "content": json.dumps(tool_response)}
        messages.append(tool_info)

        # Step 4, 获取第二次响应
        logger.info(f"第二次请求: {messages}")
        response = get_response(messages)
        if not response or not response.output:
            logger.error("获取第二次响应失败")
            return None
        logger.info(f"第二次响应: {response}")
        message = response.output.choices[0].message
        return message
    return message

def get_adcode_by_city_name(city_name):
    url = "https://restapi.amap.com/v3/config/district"
    params = {
        "key": amap_api_key,
        "keywords": city_name,
        "subdistrict": 2,
        "output": "JSON"
    }
    try:
        response = requests.get(url, params=params, timeout=10)
        response.raise_for_status()
        data = response.json()

        logger.info(f"获取城市返回结果: {data}")

        if data.get("status") == "1" and data.get("districts"):
            districts = data["districts"][0]
            adcode = districts.get("adcode")
            logger.info(f"获取城市代码成功: {adcode}")
            return adcode
        logger.warning("未找到对应的城市编码")
        return None
    except requests.RequestException as e:
        logger.error("请求城市编码失败: %s", e)
        return None

functions = [
    {
        'name': 'get_current_weather',
        'description': 'Get the current weather in a given location.',
        'parameters': {
            'type': 'object',
            'properties': {
                'location': {
                    'type': 'string',
                    'description': '给出城市名称,类似：深圳市、上海市、北京市'
                }
            },
            'required': ['location']
        }
    }
]

if __name__ == "__main__":

    result = run_conversation("上海的天气怎样")
    if result:
        print("最终结果:", result)
    else:
        print("对话执行失败")

```

运行结果：

```shell
INFO:__main__:第一次请求: [{'role': 'user', 'content': '上海的天气怎样'}]
INFO:__main__:第一次响应: {"status_code": 200, "request_id": "ec1f82a7-19e4-90ec-a081-e92e87ad92b2", "code": "", "message": "", "output": {"text": null, "finish_reason": null, "choices": [{"finish_reason": "function_call", "message": {"role": "assistant", "content": "", "function_call": {"name": "get_current_weather", "arguments": "{\"location\": \"上海市\"}"}}}]}, "usage": {"input_tokens": 205, "output_tokens": 18, "total_tokens": 223, "prompt_tokens_details": {"cached_tokens": 128}}}
INFO:__main__:arguments={'location': '上海市'}
INFO:__main__:获取城市返回结果: {'status': '1', 'info': 'OK', 'infocode': '10000', 'count': '1', 'suggestion': {'keywords': [], 'cities': []}, 'districts': [{'citycode': '021', 'adcode': '310000', 'name': '上海市', 'center': '121.473667,31.230525', 'level': 'province', 'districts': [{'citycode': '021', 'adcode': '310100', 'name': '上海城区', 'center': '121.472644,31.231706', 'level': 'city', 'districts': [{'citycode': '021', 'adcode': '310151', 'name': '崇明区', 'center': '121.397662,31.623863', 'level': 'district', 'districts': []}, {'citycode': '021', 'adcode': '310115', 'name': '浦东新区', 'center': '121.544346,31.221461', 'level': 'district', 'districts': []}, {'citycode': '021', 'adcode': '310116', 'name': '金山区', 'center': '121.341774,30.742769', 'level': 'district', 'districts': []}, {'citycode': '021', 'adcode': '310110', 'name': '杨浦区', 'center': '121.525409,31.259588', 'level': 'district', 'districts': []}, {'citycode': '021', 'adcode': '310109', 'name': '虹口区', 'center': '121.504994,31.264917', 'level': 'district', 'districts': []}, {'citycode': '021', 'adcode': '310106', 'name': '静安区', 'center': '121.447348,31.227718', 'level': 'district', 'districts': []}, {'citycode': '021', 'adcode': '310105', 'name': '长宁区', 'center': '121.424751,31.220537', 'level': 'district', 'districts': []}, {'citycode': '021', 'adcode': '310120', 'name': '奉贤区', 'center': '121.473945,30.918406', 'level': 'district', 'districts': []}, {'citycode': '021', 'adcode': '310118', 'name': '青浦区', 'center': '121.124249,31.15098', 'level': 'district', 'districts': []}, {'citycode': '021', 'adcode': '310113', 'name': '宝山区', 'center': '121.489431,31.405242', 'level': 'district', 'districts': []}, {'citycode': '021', 'adcode': '310107', 'name': '普陀区', 'center': '121.39547,31.249618', 'level': 'district', 'districts': []}, {'citycode': '021', 'adcode': '310117', 'name': '松江区', 'center': '121.227676,31.03257', 'level': 'district', 'districts': []}, {'citycode': '021', 'adcode': '310104', 'name': '徐汇区', 'center': '121.436307,31.188334', 'level': 'district', 'districts': []}, {'citycode': '021', 'adcode': '310101', 'name': '黄浦区', 'center': '121.48442,31.231661', 'level': 'district', 'districts': []}, {'citycode': '021', 'adcode': '310112', 'name': '闵行区', 'center': '121.380857,31.112834', 'level': 'district', 'districts': []}, {'citycode': '021', 'adcode': '310114', 'name': '嘉定区', 'center': '121.265276,31.375566', 'level': 'district', 'districts': []}]}]}]}
INFO:__main__:获取城市代码成功: 310000
INFO:__main__:第二次请求: [{'role': 'user', 'content': '上海的天气怎样'}, Message({'role': 'assistant', 'content': '', 'function_call': {'name': 'get_current_weather', 'arguments': '{"location": "上海市"}'}}), {'role': 'function', 'name': 'get_current_weather', 'content': '{"province": "\\u4e0a\\u6d77", "city": "\\u4e0a\\u6d77\\u5e02", "adcode": "310000", "weather": "\\u5c0f\\u96e8", "temperature": "27", "winddirection": "\\u897f\\u5317", "windpower": "\\u22643", "humidity": "81", "reporttime": "2025-05-20 11:01:07", "temperature_float": "27.0", "humidity_float": "81.0"}'}]
最终结果: {"role": "assistant", "content": "上海市当前的天气情况如下：\n- 天气状况：小雨\n- 温度：27°C\n- 风向：西北风\n- 风力：≤3级\n- 湿度：81%\n\n以上信息更新时间为：2025-05-20 11:01:07。"}
INFO:__main__:第二次响应: {"status_code": 200, "request_id": "ced7a6fb-cb38-9374-a7e0-31999aa6c2b4", "code": "", "message": "", "output": {"text": null, "finish_reason": null, "choices": [{"finish_reason": "stop", "message": {"role": "assistant", "content": "上海市当前的天气情况如下：\n- 天气状况：小雨\n- 温度：27°C\n- 风向：西北风\n- 风力：≤3级\n- 湿度：81%\n\n以上信息更新时间为：2025-05-20 11:01:07。"}}]}, "usage": {"input_tokens": 379, "output_tokens": 81, "total_tokens": 460, "prompt_tokens_details": {"cached_tokens": 0}}}
```

## 三、MCP 使用

MCP是对Function Call的规范，让大模型对外调用的能力更规范化、标准化，也更安全。

模型上下文协议（Model Context Protocol，MCP），是由 Anthropic推出的开源协议，旨在实现大语言模型与外部数据源和工具的集成，用来在大模型和数据源之间建立安全双向的连接

MCP的发展会以年为单位，让更多的业务场景适用，同时也会有更多的工具和数据源加入。

### 整体架构

MCP采用客户端-服务器架构，主机应用可以连接多个服务器。

![mcp](/imgs/tech/ai/app-dev/mcp.png)

### 核心工作流程

- 上下文请求
主机发起包含语义意图的标准化请求。
- 智能路由
客户端自动选择最优服务端组合。
- 安全访问
Server通过认证机制访问本地/云端资源。
- 上下文组装
多源数据经清洗后形成结构化上下文。
- 响应交付
标准化格式返回LLM可理解的上下文包。

### 技术优势

1. **开发效率提升**
   - 代码生成工具链（OpenAPI 3.0集成）
   - 统一测试框架（支持契约测试）
   - 插件市场生态（NPM风格的包管理）

2. **运行时稳定性**
   - 熔断降级策略（Hystrix兼容）
   - 调用链全链路追踪（OpenTracing支持）
   - 幂等性保障机制（Token桶算法）

3. **安全治理能力**
   - 工具沙箱隔离（基于gVisor/Linux Namespace）
   - 敏感数据过滤（支持正则/规则引擎）
   - 审计日志合规（满足GDPR/等保2.0要求）

### 典型应用场景

1. **企业级智能助手**
   - 多模态工具调用编排
   - 长时记忆会话管理
   - 知识库联动检索

2. **自动化运维系统**
   - 故障诊断工具链集成
   - 工作流自动生成
   - 多系统协同调度

3. **智能体协作网络**
   - 任务分解与分配算法
   - 结果验证与聚合策略
   - 通信协议自适应优化

MCP的创新价值在于构建了大模型与外部系统之间的标准化桥梁，既保留了LLM的灵活性，又赋予了企业级应用所需的可控性、可扩展性和安全性。通过统一的协议规范，开发者可以更高效地构建复杂AI系统，同时降低集成成本和维护难度。

### MCP 代码示例

#### 天气查询示例 - MCP实现

- mcp_server
- mcp_client

mcp_server

```python
import requests
from fastapi import FastAPI, HTTPException
import dashscope
from typing import Dict, Any
import logging
import os

# 初始化日志和 DashScope API Key
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)
dashscope.api_key = "sk-378c651d6bfd41808ed6db0be33e5f2b"
AMAP_API_KEY = os.getenv("AMAP_API_KEY", "5e27bb5d7efc223abe757a08b9d01e0c")
amap_api_key = AMAP_API_KEY


def get_current_weather(location):
    """
    使用高德地图SDK查询指定位置的天气信息
    :param location: str, 要查询的地点名称或坐标（如 "北京" 或 "116.397428,39.90923"）
    :param api_key: str, 高德地图开发者 API Key
    :return: dict, 包含天气信息的字典；失败时返回 None
    """
    base_url = "https://restapi.amap.com/v3/weather/weatherInfo"
    city_code = get_adcode_by_city_name(location)
    if not city_code:
        logger.warning("无法获取城市代码")
        return None
    params = {
        "key": amap_api_key,
        "city": city_code,
        "extensions": "base",  # 返回实况天气
        "output": "JSON"
    }

    try:
        response = requests.get(base_url, params=params, timeout=10)
        response.raise_for_status()
        data = response.json()

        if data.get("status") == "1" and isinstance(data.get("lives"), list) and len(data["lives"]) > 0:
            return data["lives"][0]
        else:
            logger.warning("无法获取天气信息: %s", data.get("info", "未知错误"))
            return None
    except requests.RequestException as e:
        logger.error("请求天气数据失败: %s", e)
        return None


def get_adcode_by_city_name(city_name):
    url = "https://restapi.amap.com/v3/config/district"
    params = {
        "key": amap_api_key,
        "keywords": city_name,
        "subdistrict": 2,
        "output": "JSON"
    }
    try:
        response = requests.get(url, params=params, timeout=10)
        response.raise_for_status()
        data = response.json()

        logger.info(f"获取城市返回结果: {data}")

        if data.get("status") == "1" and data.get("districts"):
            districts = data["districts"][0]
            adcode = districts.get("adcode")
            logger.info(f"获取城市代码成功: {adcode}")
            return adcode
        logger.warning("未找到对应的城市编码")
        return None
    except requests.RequestException as e:
        logger.error("请求城市编码失败: %s", e)
        return None


app = FastAPI()


@app.post("/mcp/invoke")
async def invoke_function(data: Dict[str, Any]):
    function_name = data.get("function")
    arguments = data.get("arguments", {})

    logger.info(f"收到 MCP 调用请求: {function_name} with {arguments}")

    if function_name == "get_current_weather":
        result = get_current_weather(location=arguments.get("location"))
        if result is None:
            raise HTTPException(status_code=400, detail="获取天气失败")
        return {"result": result}
    elif function_name == "qwen-turbo":
        try:
            response = dashscope.Generation.call(
                model='qwen-turbo',
                messages=arguments.get("messages"),
                functions=[{
                    'name': 'get_current_weather',
                    'description': 'Get the current weather in a given location.',
                    'parameters': {
                        'type': 'object',
                        'properties': {
                            'location': {'type': 'string', 'description': '给出城市名称,类似：深圳市、上海市、北京市'}
                        },
                        'required': ['location']
                    }
                }],
                result_format='message'
            )

            logger.info(f"调用 Qwen 成功: {response}")

            choice = response.output.choices[0]

            if choice.finish_reason == 'function_call':
                ret = convert_dashscope_response(choice.message)
            else:
                ret = {
                    "role": choice.message.get("role") if isinstance(choice.message, dict) else getattr(choice.message,
                                                                                                        "role", None),
                    "content": choice.message.get("content") if isinstance(choice.message, dict) else getattr(
                        choice.message, "content", None),
                    "function_call": None
                }

            return {"result": ret}
        except Exception as e:
            logger.error(f"调用 Qwen 失败: {e}")
            raise HTTPException(status_code=500, detail="模型调用失败")
    else:
        raise HTTPException(status_code=400, detail="未知函数调用")


def convert_dashscope_response(message):
    """
    将 DashScope 的响应转换为可序列化的 dict 格式
    支持 message 为 dict 或 object 两种形式
    """
    try:
        if isinstance(message, dict):
            return {
                "role": message.get("role"),
                "content": message.get("content"),
                "function_call": message.get("function_call")
            }
        else:
            return {
                "role": getattr(message, "role", None),
                "content": getattr(message, "content", None),
                "function_call": getattr(message, "function_call", None)
            }
    except Exception as e:
        logger.error(f"解析模型响应失败: {e}")
        return {"error": "模型响应格式不正确"}


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=8000)

```

mcp_client

```python
import requests
import json
import logging

# 设置日志
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# MCP 服务地址
MCP_SERVER_URL = "http://localhost:8000/mcp/invoke"

def call_mcp(function_name: str, arguments: dict):
    payload = {
        "function": function_name,
        "arguments": arguments
    }

    try:
        logger.info(f"调用 MCP 函数: {function_name}, 参数: {arguments}")
        response = requests.post(MCP_SERVER_URL, json=payload, timeout=30)
        response.raise_for_status()
        return response.json().get("result")
    except requests.RequestException as e:
        logger.error(f"MCP 调用失败: {e}")
        return None

def run_conversation(query):
    messages = [{"role": "user", "content": query}]
    logger.info(f"第一次请求: {messages}")

    # 第一次调用 Qwen 模型
    qwen_response = call_mcp("qwen-turbo", {"messages": messages})
    if not qwen_response:
        logger.error("第一次响应失败")
        return None

    logger.info(f"第一次响应: {qwen_response}")
    messages.append(qwen_response)

    # 判断是否需要调用工具
    if 'function_call' in qwen_response and qwen_response['function_call']:
        tool_name = qwen_response['function_call']['name']
        arguments = qwen_response['function_call']['arguments']

        # 注意：这里的 arguments 可能是字符串形式的 JSON，需解析
        if isinstance(arguments, str):
            try:
                arguments = json.loads(arguments)
            except json.JSONDecodeError as e:
                logger.error(f"解析 function_call 参数失败: {e}")
                return None

        # 调用天气查询函数
        tool_response = call_mcp(tool_name, arguments)
        if tool_response is None:
            logger.error("获取天气信息失败")
            return None

        # 将工具结果附加到消息中
        messages.append({"role": "function", "name": tool_name, "content": json.dumps(tool_response)})

        # 第二次调用 Qwen 模型
        second_response = call_mcp("qwen-turbo", {"messages": messages})
        if not second_response:
            logger.error("第二次响应失败")
            return None

        return second_response

    return qwen_response


if __name__ == "__main__":

    result = run_conversation("上海的天气怎样")
    if result:
        print("最终结果:", result)
    else:
        print("对话执行失败")

```

运行结果：

```shell
INFO:__main__:第一次请求: [{'role': 'user', 'content': '上海的天气怎样'}]
INFO:__main__:调用 MCP 函数: qwen-turbo, 参数: {'messages': [{'role': 'user', 'content': '上海的天气怎样'}]}
INFO:__main__:第一次响应: {'role': 'assistant', 'content': '', 'function_call': {'name': 'get_current_weather', 'arguments': '{"location": "上海市"}'}}
INFO:__main__:调用 MCP 函数: get_current_weather, 参数: {'location': '上海市'}
INFO:__main__:调用 MCP 函数: qwen-turbo, 参数: {'messages': [{'role': 'user', 'content': '上海的天气怎样'}, {'role': 'assistant', 'content': '', 'function_call': {'name': 'get_current_weather', 'arguments': '{"location": "上海市"}'}}, {'role': 'function', 'name': 'get_current_weather', 'content': '{"province": "\\u4e0a\\u6d77", "city": "\\u4e0a\\u6d77\\u5e02", "adcode": "310000", "weather": "\\u5c0f\\u96e8", "temperature": "25", "winddirection": "\\u897f", "windpower": "\\u22643", "humidity": "91", "reporttime": "2025-05-20 13:33:19", "temperature_float": "25.0", "humidity_float": "91.0"}'}]}
最终结果: {'role': 'assistant', 'content': '上海市现在的天气情况如下：\n- 天气状况：小雨\n- 温度：25°C\n- 风向：西\n- 风力：≤3级\n- 相对湿度：91%\n\n希望这些信息对你有帮助！', 'function_call': None}
```

## 总结

大模型是AI应用开发的核心，它的API调用是基础，Function Call机制是扩展，MCP是规范。

通过理解这些核心概念并在实践中不断探索和应用，开发者可以更好地把握AI应用开发的要点，构建出更加强大和可靠的AI应用系统。
