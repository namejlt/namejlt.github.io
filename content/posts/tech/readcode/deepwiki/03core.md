---
title: "deepwiki源码阅读-03 核心逻辑分析"
date: 2025-05-27T18:30:00+08:00
draft: false
toc: true
categories: ["技术/源码阅读/deepwiki"]
tags: ["源码阅读", "deepwiki"]
---

## 概述

项目主要是把代码仓库拉到本地，进行代码ebedding，然后用向量数据库存储，然后用LLM进行文档生成，最后前端通过图标和结构化wiki展示。

可以把这个项目想象成一个智能的图书馆管理员。用户输入的代码仓库就像是一堆杂乱无章的书籍，项目要做的就是把这些书籍整理成一个有序的图书馆（Wiki）。

1. **克隆并分析仓库**：就像管理员先把所有书籍（代码仓库）搬到图书馆里，然后了解每本书的大致内容（分析代码结构）。
2. **创建代码嵌入**：这相当于给每本书贴上一个独特的标签（向量），方便后续快速查找。这些标签会被存放在一个标签库（向量数据库）中。
3. **使用AI生成文档**：管理员根据这些标签和书籍内容，用自己的知识（大模型）来撰写每本书的简介和推荐信息（生成文档）。
4. **创建可视化图表**：管理员还会绘制一些图表，展示不同书籍之间的关联和分类（可视化代码关系）。
5. **组织成结构化Wiki**：最后，管理员把所有的简介、推荐信息和图表整理成一个目录（Wiki），方便读者（用户）查找和阅读。

其核心在于数据生成，主要逻辑在后端，以下是针对数据生成过程的分析

## 核心逻辑分析

### 后端

#### 克隆仓库并生成嵌入向量数据库

这部分代码可能在 `data_pipeline.py` 中，用于克隆仓库和分析代码结构。

其核心在`class DatabaseManager`类中。

- 重置数据库 reset_database
- 下载代码仓库到本地 _create_repo
- 读取所有代码文件 read_all_documents
- 转换文档为向量并保存 transform_documents_and_save_to_db

这里面的核心在于 transform_documents_and_save_to_db

```python
def transform_documents_and_save_to_db(
    documents: List[Document], db_path: str, is_ollama_embedder: bool = None
) -> LocalDB:
    """
    Transforms a list of documents and saves them to a local database.

    Args:
        documents (list): A list of `Document` objects.
        db_path (str): The path to the local database file.
        is_ollama_embedder (bool, optional): Whether to use Ollama for embedding.
                                           If None, will be determined from configuration.
    """
    # Get the data transformer
    data_transformer = prepare_data_pipeline(is_ollama_embedder)

    # Save the documents to a local database
    db = LocalDB()
    db.register_transformer(transformer=data_transformer, key="split_and_embed")
    db.load(documents)
    db.transform(key="split_and_embed")
    os.makedirs(os.path.dirname(db_path), exist_ok=True)
    db.save_state(filepath=db_path)
    return db
```

经过此文件操作，代码仓库被clone到本地，并转换为向量保存在本地数据库中。

这里面进行文档分块，嵌入的逻辑，配置在嵌入模型的配置中

```python
splitter = TextSplitter(**configs["text_splitter"])
```

```json
  "text_splitter": {
    "split_by": "word",
    "chunk_size": 350,
    "chunk_overlap": 100
  }
```

#### 创建RAG系统

在 `rag.py` 构建了一个完整的 RAG 系统，它能够：

1. 管理对话历史。
2. 从代码仓库中检索相关文档。
3. 将检索到的文档作为上下文，结合对话历史和用户查询，生成高质量的答案。
4. 支持多种模型提供者和嵌入器。
5. 包含健壮的错误处理机制，以确保系统的稳定运行。

核心主要在于 `class RAG` 类中。

RAG 核心类 ( RAG ) ：

- 继承自 adal.Component 。
- 初始化 ( **init** ) ：
  - 设置模型提供者（ google 、 openai 、 openrouter 、 ollama ）和模型名称。
  - 判断是否使用 Ollama 嵌入器。
  - 初始化 Memory 实例来管理对话历史。
  - 获取嵌入器 ( get_embedder )，并为 Ollama 嵌入器提供一个单字符串嵌入的补丁 ( single_string_embedder )。
  - 初始化 DatabaseManager ( initialize_db_manager )。
  - 设置 RAGAnswer 的输出解析器 ( adal.DataClassParser ) 和格式化指令，确保生成模型的输出符合预期。
  - 根据配置初始化生成器 ( adal.Generator )，包括模板、提示参数、模型客户端和模型参数。
- 数据库管理器初始化 ( initialize_db_manager ) ：
  - 创建 DatabaseManager 实例，用于管理本地数据库。
- 嵌入验证和过滤 ( _validate_and_filter_embeddings ) ：
  - 验证文档的嵌入向量，确保它们有效且大小一致。
  - 过滤掉无效或大小不匹配的嵌入，以避免后续检索器的问题。
- 准备检索器 ( prepare_retriever ) ：
  - 这是 RAG 的关键步骤，用于准备检索器。
  - 它调用 db_manager.prepare_database 来加载或处理仓库数据，生成 transformed_docs （包含嵌入向量的文档）。
  - 对 transformed_docs 进行嵌入验证和过滤。
  - 使用 FAISSRetriever 创建检索器实例，该检索器基于 FAISS 库，用于高效地进行相似性搜索。
  - 处理创建检索器时可能出现的错误，特别是嵌入大小不一致的问题。
- 调用 ( call ) ：
  - 接收用户查询和语言。
  - 使用 self.retriever(query) 检索相关文档。
  - 将检索到的文档填充到 retrieved_documents 中。
  - 返回 RAGAnswer 和检索到的文档。
  - 包含错误处理，当 RAG 调用失败时，返回一个包含错误信息的 RAGAnswer 。

#### 使用AI生成文档

在 `websocket_wiki.py` 中可以调用大模型生成文档，另外也用来处理实时聊天。

该文档核心是围绕`handle_websocket_chat(websocket: WebSocket)`展开

核心逻辑分析：

1. WebSocket 连接处理 ：

   - `handle_websocket_chat` 函数负责接受 WebSocket 连接，并处理客户端发送的聊天完成请求。它能够接收包含仓库 URL、聊天消息、文件路径、访问令牌和仓库类型等信息的请求。
2. 请求数据解析与验证 ：

   - 接收到的 JSON 数据被解析为 `ChatCompletionRequest` 模型实例。在处理请求之前，会进行一系列验证，例如检查输入消息是否过大（通过 count_tokens 函数），以及最后一条消息是否来自用户。
3. RAG 实例初始化与准备 ：

   - 为每个请求创建一个新的 `RAG` 实例。这个 RAG 实例是整个wiki生成过程的关键。它根据请求中指定的 provider 和 model 进行初始化。
   - prepare_retriever 方法用于准备检索器，它会根据 repo_url 、 repo_type 、 token 以及可选的 excluded_dirs 、 excluded_files 、 included_dirs 、 included_files 来处理和索引仓库文档。这确保了 RAG 系统能够从正确的代码库中检索相关信息。
4. 对话历史管理 ：

   - 函数会处理 ChatCompletionRequest 中包含的 messages 列表，将之前的用户和助手消息添加到 RAG 实例的内存中，以维护对话上下文。这对于多轮对话和“深度研究”功能至关重要。
5. “深度研究”模式 ：

   - 该模块支持一个“深度研究”模式，通过检查用户消息中是否包含 [DEEP RESEARCH] 标签来激活。在此模式下，系统会进行多轮迭代研究，每次迭代都会根据之前的研究结果调整系统提示，以提供更深入、更集中的信息。这包括“研究计划”、“研究更新”和“最终结论”等阶段。
6. 上下文检索与提示构建 ：

   - 如果输入大小允许且未禁用 RAG，系统会根据用户查询（或针对特定文件路径的查询）从 RAG 实例中检索相关文档。这些文档被格式化为上下文文本，并与系统提示、对话历史和可选的文件内容一起构建最终的 LLM 提示。
   - 系统提示会根据是否处于“深度研究”模式以及当前的迭代次数进行动态调整，以指导 LLM 的行为和输出格式。
7. LLM 交互与流式响应 ：

   - 根据请求中指定的 provider （如 ollama 、 openrouter 、 openai 或 google ），模块会初始化相应的 LLM 客户端或模型。
   - 构建好的提示被发送给选定的 LLM 进行内容生成。响应以流式方式处理，每个生成的文本块都会通过 WebSocket 发送回客户端，从而实现实时、逐字显示。
   - 模块还包含了错误处理机制，特别是针对令牌限制错误，它会尝试在没有 RAG 上下文的情况下重试请求，以提供一个回退方案。

#### wiki的生成

一个仓库新增后，wiki的初始化就是由handle_websocket_chat完成

具体流程：

1. 接收请求

   ```python
    class ChatCompletionRequest(BaseModel):
        """
        Model for requesting a chat completion.
        """
        repo_url: str = Field(..., description="URL of the repository to query")
        messages: List[ChatMessage] = Field(..., description="List of chat messages")
        filePath: Optional[str] = Field(None, description="Optional path to a file in the repository to include in the prompt")
        token: Optional[str] = Field(None, description="Personal access token for private repositories")
        type: Optional[str] = Field("github", description="Type of repository (e.g., 'github', 'gitlab', 'bitbucket')")

        # model parameters
        provider: str = Field("google", description="Model provider (google, openai, openrouter, ollama)")
        model: Optional[str] = Field(None, description="Model name for the specified provider")

        language: Optional[str] = Field("en", description="Language for content generation (e.g., 'en', 'ja', 'zh', 'es', 'kr', 'vi')")
        excluded_dirs: Optional[str] = Field(None, description="Comma-separated list of directories to exclude from processing")
        excluded_files: Optional[str] = Field(None, description="Comma-separated list of file patterns to exclude from processing")
        included_dirs: Optional[str] = Field(None, description="Comma-separated list of directories to include exclusively")
        included_files: Optional[str] = Field(None, description="Comma-separated list of file patterns to include exclusively")

   ```

2. RAG 实例创建

   ```python
    # ... existing code ...
        request_rag = RAG(provider=request.provider, model=request.model)
    # ... existing code ...
    ```

3. RAG 准备，核心是data_pipeline里面做的工作

   ```python
   request_rag.prepare_retriever(request.repo_url, request.type, request.token, excluded_dirs, excluded_files, included_dirs, included_files)
    ```

    这里 FAISS retriever created successfully

4. 获取rag数据，组合历史对话，通过模版，提交LLM

这里都是nothink模式，提交LLM后，获取响应数据为wiki内容

这里的wiki会被保存在本地

这里给出一个模板

```python
system_prompt = f"""<role>
You are an expert code analyst examining the {repo_type} repository: {repo_url} ({repo_name}).
You are conducting a multi-turn Deep Research process to thoroughly investigate the specific topic in the user's query.
Your goal is to provide detailed, focused information EXCLUSIVELY about this topic.
IMPORTANT:You MUST respond in {language_name} language.
</role>

<guidelines>
- This is the first iteration of a multi-turn research process focused EXCLUSIVELY on the user's query
- Start your response with "## Research Plan"
- Outline your approach to investigating this specific topic
- If the topic is about a specific file or feature (like "Dockerfile"), focus ONLY on that file or feature
- Clearly state the specific topic you're researching to maintain focus throughout all iterations
- Identify the key aspects you'll need to research
- Provide initial findings based on the information available
- End with "## Next Steps" indicating what you'll investigate in the next iteration
- Do NOT provide a final conclusion yet - this is just the beginning of the research
- Do NOT include general repository information unless directly relevant to the query
- Focus EXCLUSIVELY on the specific topic being researched - do not drift to related topics
- Your research MUST directly address the original question
- NEVER respond with just "Continue the research" as an answer - always provide substantive research findings
- Remember that this topic will be maintained across all research iterations
</guidelines>

<style>
- Be concise but thorough
- Use markdown formatting to improve readability
- Cite specific files and code sections when relevant
</style>"""
```

## 总结

### 新手容易误解的点

1. **API 密钥管理**：在使用大模型 API 时，需要妥善管理 API 密钥，避免泄露。建议将密钥存储在环境变量中，而不是硬编码在代码里。
2. **向量数据库的使用**：创建代码嵌入后，需要将其存储在向量数据库中，以便后续检索。新手可能对向量数据库的使用和配置不太熟悉，需要学习相关知识。
3. **模型选择和调优**：不同的大模型有不同的特点和性能，新手可能不知道如何选择合适的模型，以及如何调整模型参数以获得更好的结果。
4. **错误处理**：在克隆仓库、创建嵌入和调用大模型时，可能会出现各种错误，如网络问题、权限问题等。新手需要学会如何正确处理这些错误，以保证程序的稳定性。
