---
title: "deepwiki源码阅读-04 修改验证"
date: 2025-05-27T18:40:00+08:00
draft: false
toc: true
categories: ["技术/源码阅读/deepwiki"]
tags: ["源码阅读", "deepwiki"]
---

## 概述

deepwiki通过wiki生成和wiki展示的方式，把代码仓库的内容展示给用户。

我们可以扩展wiki生成的部分

- 让其更加方便的支持各种大模型
- wiki生成可以自定义结构

我们也可以更改此自动生成wiki的方式，生成其他内容，如爬取一定格式资料，生成内容分析

## 项目扩展

### LLM扩展

大模型具体使用，一个在rag里面embedding，一个在websocket_wiki里面llm

目前均通过配置获取，且每个类型有其对应的client

现在有openai_client、openrouter_client等

embedder的扩展，通过配置实现

```python
def get_embedder() -> adal.Embedder:
    embedder_config = configs["embedder"]

    # --- Initialize Embedder ---
    model_client_class = embedder_config["model_client"]
    if "initialize_kwargs" in embedder_config:
        model_client = model_client_class(**embedder_config["initialize_kwargs"])
    else:
        model_client = model_client_class()
    embedder = adal.Embedder(
        model_client=model_client,
        model_kwargs=embedder_config["model_kwargs"],
    )
    return embedder
```

LLM的扩展

```python
class OpenRouterClient(ModelClient):

```

模仿开发即可

### wiki扩展

现在deepwiki仅用于代码分析和其wiki生成，如果我们做一个图书管理，精华检索，那我们可以把deepwiki的wiki生成部分，改成文章分析的方式。

具体调整

- 拉取内容模块
- 过滤内容文档模块
- 分析内容提示词prompt模块

## 总结

deepwiki利用大模型的能力，提供快速代码分析，生成wiki的能力。
但我们可以对这个形式进行扩充，对更多的标准格式的内容进行管理，更快更好的获取需要的知识。
