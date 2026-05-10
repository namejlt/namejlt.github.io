---
title: "RAG检索增强生成系统如何设计与实现"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["RAG", "大模型", "golang"]
---

## 问题

大语言模型存在知识截止、幻觉和缺乏私有数据等问题，RAG（Retrieval-Augmented Generation）通过外部知识检索来增强生成质量。一个生产级RAG系统的核心架构是什么？文档解析、分块、向量化、检索、重排各环节如何设计？如何解决检索召回率低和上下文噪声问题？

## 回答

RAG是当前企业落地大模型最主流的架构模式。它将"检索"和"生成"两个阶段结合：先从知识库中检索相关文档，再将检索结果作为上下文输入LLM生成答案。RAG的核心挑战不在于"生成"，而在于"检索"——如果检索不到正确的文档，再强大的LLM也无法给出准确答案。

### 一、RAG系统架构

```
用户提问
   ↓
[查询改写/扩展] → [向量化] → [向量检索] → [重排序] → [上下文组装] → [LLM生成]
                                  ↑
[文档入库] → [解析] → [分块] → [向量化] → [向量库]
```

**核心流程**：
1. **离线索引**：文档 → 解析 → 分块 → Embedding → 存入向量库
2. **在线查询**：问题 → Embedding → 向量检索 → 重排序 → LLM生成

### 二、文档解析

文档解析是RAG的第一步，质量直接影响后续所有环节。

```go
package rag

import (
	"bytes"
	"io"
	"strings"
)

type Document struct {
	ID      string
	Content string
	Metadata map[string]string
}

type Parser interface {
	Parse(data []byte) ([]Document, error)
}

type TextParser struct{}

func (p *TextParser) Parse(data []byte) ([]Document, error) {
	content := string(data)
	return []Document{{Content: content}}, nil
}

type MarkdownParser struct{}

func (p *MarkdownParser) Parse(data []byte) ([]Document, error) {
	content := string(data)
	sections := strings.Split(content, "\n## ")

	var docs []Document
	for i, section := range sections {
		if i > 0 {
			section = "## " + section
		}
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}

		title := extractTitle(section)
		docs = append(docs, Document{
			Content:  section,
			Metadata: map[string]string{"title": title, "type": "markdown"},
		})
	}
	return docs, nil
}

func extractTitle(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			return strings.TrimLeft(line, "# ")
		}
	}
	return ""
}

type PDFParser struct{}

func (p *PDFParser) Parse(data []byte) ([]Document, error) {
	return nil, nil
}
```

**生产级解析需要处理**：
- PDF：表格提取、OCR图片文字识别、版面分析
- Word：样式层级、表格、嵌入对象
- HTML：正文提取（去导航/广告）、结构化标记保留
- 图片：OCR + 多模态模型描述

### 三、文档分块（Chunking）

分块策略直接决定检索粒度和上下文完整性。

```go
package rag

import (
	"unicode/utf8"
)

type ChunkConfig struct {
	ChunkSize    int
	OverlapSize  int
	Separator    string
}

type Chunker struct {
	config ChunkConfig
}

func NewChunker(config ChunkConfig) *Chunker {
	return &Chunker{config: config}
}

func (c *Chunker) Chunk(doc Document) []Chunk {
	text := doc.Content
	var chunks []Chunk

	if c.config.Separator != "" {
		chunks = c.chunkBySeparator(text, doc)
	} else {
		chunks = c.chunkBySize(text, doc)
	}

	return chunks
}

func (c *Chunker) chunkBySize(text string, doc Document) []Chunk {
	var chunks []Chunk
	runes := []rune(text)
	start := 0

	for start < len(runes) {
		end := start + c.config.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}

		chunkText := string(runes[start:end])
		chunks = append(chunks, Chunk{
			Content:  chunkText,
			Metadata: copyMetadata(doc.Metadata),
		})

		if end >= len(runes) {
			break
		}

		overlapStart := end - c.config.OverlapSize
		if overlapStart < start {
			overlapStart = start
		}
		start = overlapStart
	}

	return chunks
}

func (c *Chunker) chunkBySeparator(text string, doc Document) []Chunk {
	sections := splitBySeparator(text, c.config.Separator)
	var chunks []Chunk

	currentChunk := ""
	for _, section := range sections {
		if utf8.RuneCountInString(currentChunk)+utf8.RuneCountInString(section) > c.config.ChunkSize && currentChunk != "" {
			chunks = append(chunks, Chunk{
				Content:  currentChunk,
				Metadata: copyMetadata(doc.Metadata),
			})
			currentChunk = section
		} else {
			if currentChunk != "" {
				currentChunk += c.config.Separator
			}
			currentChunk += section
		}
	}

	if currentChunk != "" {
		chunks = append(chunks, Chunk{
			Content:  currentChunk,
			Metadata: copyMetadata(doc.Metadata),
		})
	}

	return chunks
}

type Chunk struct {
	Content  string
	Metadata map[string]string
}

func splitBySeparator(text, sep string) []string {
	var result []string
	start := 0
	for {
		idx := indexOf(text[start:], sep)
		if idx == -1 {
			result = append(result, text[start:])
			break
		}
		result = append(result, text[start:start+idx])
		start = start + idx + len(sep)
	}
	return result
}

func copyMetadata(m map[string]string) map[string]string {
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
```

**分块策略对比**：

| 策略 | 原理 | 优点 | 缺点 |
|------|------|------|------|
| 固定大小 | 按字符数切分 | 简单、均匀 | 可能切断语义 |
| 重叠切分 | 相邻块有重叠 | 保持上下文连贯 | 存储冗余 |
| 语义分块 | 按段落/标题切分 | 语义完整 | 块大小不均 |
| 递归分块 | 多级分隔符递归 | 兼顾语义和大小 | 实现复杂 |

**最佳实践**：ChunkSize=512~1024 tokens，OverlapSize=ChunkSize的10%~20%，优先按语义边界切分。

### 四、向量化与存储

```go
package rag

import (
	"context"
	"fmt"
)

type EmbeddingService interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type VectorStore interface {
	Upsert(ctx context.Context, docs []VectorDocument) error
	Search(ctx context.Context, query []float32, topK int, filter map[string]string) ([]SearchResult, error)
	Delete(ctx context.Context, ids []string) error
}

type VectorDocument struct {
	ID       string
	Vector   []float32
	Content  string
	Metadata map[string]string
}

type SearchResult struct {
	ID       string
	Content  string
	Score    float32
	Metadata map[string]string
}

type RAGIndexer struct {
	embedder    EmbeddingService
	vectorStore VectorStore
	chunker     *Chunker
}

func NewRAGIndexer(embedder EmbeddingService, store VectorStore, chunker *Chunker) *RAGIndexer {
	return &RAGIndexer{
		embedder:    embedder,
		vectorStore: store,
		chunker:     chunker,
	}
}

func (idx *RAGIndexer) IndexDocument(ctx context.Context, doc Document) error {
	chunks := idx.chunker.Chunk(doc)

	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = chunk.Content
	}

	embeddings, err := idx.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("embedding failed: %w", err)
	}

	var vecDocs []VectorDocument
	for i, chunk := range chunks {
		vecDocs = append(vecDocs, VectorDocument{
			ID:       fmt.Sprintf("%s_chunk_%d", doc.ID, i),
			Vector:   embeddings[i],
			Content:  chunk.Content,
			Metadata: chunk.Metadata,
		})
	}

	return idx.vectorStore.Upsert(ctx, vecDocs)
}
```

### 五、检索与重排序

```go
package rag

import (
	"context"
	"fmt"
	"sort"
)

type Retriever struct {
	embedder    EmbeddingService
	vectorStore VectorStore
	reranker    Reranker
	topK        int
	rerankTopK  int
}

type Reranker interface {
	Rerank(ctx context.Context, query string, documents []SearchResult) ([]SearchResult, error)
}

func NewRetriever(embedder EmbeddingService, store VectorStore, reranker Reranker, topK, rerankTopK int) *Retriever {
	return &Retriever{
		embedder:    embedder,
		vectorStore: store,
		reranker:    reranker,
		topK:        topK,
		rerankTopK:  rerankTopK,
	}
}

func (r *Retriever) Retrieve(ctx context.Context, query string, filter map[string]string) ([]SearchResult, error) {
	embeddings, err := r.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("query embedding failed: %w", err)
	}

	results, err := r.vectorStore.Search(ctx, embeddings[0], r.topK, filter)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	if r.reranker != nil && len(results) > r.rerankTopK {
		results, err = r.reranker.Rerank(ctx, query, results)
		if err != nil {
			return results, nil
		}
	}

	if len(results) > r.rerankTopK {
		results = results[:r.rerankTopK]
	}

	return results, nil
}

type CrossEncoderReranker struct {
	llm LLMClient
}

func NewCrossEncoderReranker(llm LLMClient) *CrossEncoderReranker {
	return &CrossEncoderReranker{llm: llm}
}

func (r *CrossEncoderReranker) Rerank(ctx context.Context, query string, documents []SearchResult) ([]SearchResult, error) {
	type scoredDoc struct {
		doc   SearchResult
		score float32
	}

	var scored []scoredDoc
	for _, doc := range documents {
		prompt := fmt.Sprintf(`Rate the relevance of the following document to the query on a scale of 0 to 1.

Query: %s

Document: %s

Relevance score (0-1):`, query, doc.Content)

		resp, err := r.llm.Generate(ctx, prompt)
		if err != nil {
			scored = append(scored, scoredDoc{doc: doc, score: 0})
			continue
		}

		score := parseScore(resp)
		scored = append(scored, scoredDoc{doc: doc, score: score})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	var results []SearchResult
	for _, s := range scored {
		s.doc.Score = s.score
		results = append(results, s.doc)
	}

	return results, nil
}
```

### 六、上下文组装与生成

```go
package rag

import (
	"context"
	"fmt"
	"strings"
)

type LLMClient interface {
	Generate(ctx context.Context, prompt string) (string, error)
	GenerateWithMessages(ctx context.Context, messages []Message) (string, error)
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type RAGGenerator struct {
	llm       LLMClient
	retriever *Retriever
}

func NewRAGGenerator(llm LLMClient, retriever *Retriever) *RAGGenerator {
	return &RAGGenerator{
		llm:       llm,
		retriever: retriever,
	}
}

func (g *RAGGenerator) Generate(ctx context.Context, query string, filter map[string]string) (*RAGResponse, error) {
	results, err := g.retriever.Retrieve(ctx, query, filter)
	if err != nil {
		return nil, fmt.Errorf("retrieval failed: %w", err)
	}

	contextText := g.buildContext(results)

	prompt := fmt.Sprintf(`Based on the following reference materials, please answer the user's question. If the answer cannot be found in the reference materials, please say "Based on the available information, I cannot answer this question." Do not fabricate information.

Reference materials:
%s

User question: %s

Please answer based on the reference materials above:`, contextText, query)

	answer, err := g.llm.Generate(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	return &RAGResponse{
		Query:   query,
		Answer:  answer,
		Sources: results,
	}, nil
}

func (g *RAGGenerator) buildContext(results []SearchResult) string {
	var sb strings.Builder
	for i, result := range results {
		sb.WriteString(fmt.Sprintf("[Reference %d] (Source: %s)\n%s\n\n", i+1, result.Metadata["source"], result.Content))
	}
	return sb.String()
}

type RAGResponse struct {
	Query   string
	Answer  string
	Sources []SearchResult
}
```

### 七、高级优化策略

#### 查询改写

```go
type QueryRewriter struct {
	llm LLMClient
}

func (r *QueryRewriter) Rewrite(ctx context.Context, query string) ([]string, error) {
	prompt := fmt.Sprintf(`Please generate 3 different search queries that could help find information to answer the following question. Each query should focus on a different aspect.

Original question: %s

Generate 3 search queries:`, query)

	resp, err := r.llm.Generate(ctx, prompt)
	if err != nil {
		return []string{query}, nil
	}

	queries := parseQueries(resp)
	queries = append([]string{query}, queries...)
	return queries, nil
}
```

#### HyDE（Hypothetical Document Embedding）

```go
type HyDERetriever struct {
	llm       LLMClient
	embedder  EmbeddingService
	store     VectorStore
}

func (r *HyDERetriever) Retrieve(ctx context.Context, query string, topK int) ([]SearchResult, error) {
	hypotheticalAnswer, err := r.llm.Generate(ctx, fmt.Sprintf(
		"Please write a detailed answer to the following question:\n%s", query))
	if err != nil {
		return nil, err
	}

	embeddings, err := r.embedder.Embed(ctx, []string{hypotheticalAnswer})
	if err != nil {
		return nil, err
	}

	return r.store.Search(ctx, embeddings[0], topK, nil)
}
```

### 八、RAG系统评估

| 指标 | 含义 | 计算方式 |
|------|------|----------|
| 召回率 | 相关文档被检索到的比例 | 检索到的相关文档数 / 总相关文档数 |
| 精确率 | 检索结果中相关文档的比例 | 检索到的相关文档数 / 检索结果总数 |
| MRR | 首个相关文档的排名倒数 | 1/首个相关文档的排名 |
| 忠实度 | 生成答案与检索上下文的一致性 | LLM评估 |
| 答案相关性 | 生成答案与问题的相关性 | LLM评估 |

### 九、总结

RAG系统的质量取决于检索质量，检索质量取决于分块策略、Embedding模型和重排序。**核心优化方向**：

1. **分块**：语义分块优于固定大小分块，保留文档结构信息
2. **检索**：多路召回（向量+关键词+知识图谱），召回率优先
3. **重排序**：Cross-Encoder重排序显著提升精确率
4. **生成**：明确指示LLM基于上下文回答，避免幻觉
5. **评估**：建立自动化评估流水线，持续优化各环节
