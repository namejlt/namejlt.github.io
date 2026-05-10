---
title: "大模型长上下文如何处理"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["大模型", "长上下文", "golang"]
---

## 问题

大模型的上下文窗口有限（4K~128K tokens），长文档、多轮对话等场景如何突破上下文限制？滑动窗口、摘要压缩、MapReduce等方案的原理和实现是什么？

## 回答

长上下文处理是大模型应用的核心挑战。虽然GPT-4 Turbo支持128K、Claude支持200K，但实际使用中长上下文会导致注意力稀释、成本飙升和延迟增加。工程层面的优化不可或缺。

### 一、长上下文的挑战

| 问题 | 原因 | 影响 |
|------|------|------|
| 注意力稀释 | 注意力被分散到大量无关Token | 回答质量下降 |
| 成本飙升 | Token数线性增加费用 | 不可承受 |
| 延迟增加 | 自注意力O(n²)复杂度 | 响应变慢 |
| 中间遗忘 | "Lost in the Middle"现象 | 中间信息被忽略 |

### 二、方案1：滑动窗口

将长文本按窗口切分，逐窗口处理，最后汇总。

```go
package longcontext

import (
	"fmt"
	"strings"
)

type SlidingWindowProcessor struct {
	windowSize   int
	overlapSize  int
	llm          LLMClient
}

func NewSlidingWindowProcessor(llm LLMClient, windowSize, overlapSize int) *SlidingWindowProcessor {
	return &SlidingWindowProcessor{
		windowSize:  windowSize,
		overlapSize: overlapSize,
		llm:         llm,
	}
}

func (p *SlidingWindowProcessor) Process(ctx context.Context, text string, query string) (string, error) {
	chunks := p.splitText(text)

	var results []string
	for i, chunk := range chunks {
		prompt := fmt.Sprintf(`Based on the following text chunk, answer the question. If the answer is not in this chunk, say "Not found in this chunk."

Text chunk %d/%d:
%s

Question: %s

Answer:`, i+1, len(chunks), chunk, query)

		resp, err := p.llm.Generate(ctx, []Message{{Role: "user", Content: prompt}})
		if err != nil {
			continue
		}

		if !strings.Contains(resp, "Not found in this chunk") {
			results = append(results, resp)
		}
	}

	if len(results) == 0 {
		return "Unable to find answer in the document.", nil
	}

	if len(results) == 1 {
		return results[0], nil
	}

	summaryPrompt := fmt.Sprintf(`Based on the following partial answers, provide a comprehensive final answer.

Question: %s

Partial answers:
%s

Comprehensive answer:`, query, strings.Join(results, "\n\n"))

	return p.llm.Generate(ctx, []Message{{Role: "user", Content: summaryPrompt}})
}

func (p *SlidingWindowProcessor) splitText(text string) []string {
	tokens := tokenize(text)
	var chunks []string

	step := p.windowSize - p.overlapSize
	for i := 0; i < len(tokens); i += step {
		end := i + p.windowSize
		if end > len(tokens) {
			end = len(tokens)
		}
		chunks = append(chunks, detokenize(tokens[i:end]))
		if end >= len(tokens) {
			break
		}
	}

	return chunks
}
```

### 三、方案2：摘要压缩

对历史对话或长文档进行摘要，用摘要替代原文。

```go
type ConversationCompressor struct {
	llm        LLMClient
	maxTokens  int
	summaryRatio float32
}

func NewConversationCompressor(llm LLMClient, maxTokens int) *ConversationCompressor {
	return &ConversationCompressor{
		llm:        llm,
		maxTokens:  maxTokens,
		summaryRatio: 0.3,
	}
}

func (c *ConversationCompressor) Compress(ctx context.Context, messages []Message) ([]Message, error) {
	totalTokens := estimateTokens(messages)
	if totalTokens <= c.maxTokens {
		return messages, nil
	}

	systemMsg := messages[0]
	conversation := messages[1:]

	splitPoint := len(conversation) / 2

	olderMessages := conversation[:splitPoint]
	recentMessages := conversation[splitPoint:]

	var olderText strings.Builder
	for _, msg := range olderMessages {
		olderText.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Content))
	}

	summaryPrompt := fmt.Sprintf(`Please summarize the following conversation, preserving key information, decisions, and context that may be needed later:

%s

Summary:`, olderText.String())

	summary, err := c.llm.Generate(ctx, []Message{{Role: "user", Content: summaryPrompt}})
	if err != nil {
		return nil, err
	}

	compressed := []Message{systemMsg}
	compressed = append(compressed, Message{
		Role:    "system",
		Content: fmt.Sprintf("Previous conversation summary: %s", summary),
	})
	compressed = append(compressed, recentMessages...)

	return compressed, nil
}

func estimateTokens(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content) / 4
	}
	return total
}
```

### 四、方案3：MapReduce

将长文本分片并行处理（Map），再汇总结果（Reduce）。

```go
type MapReduceProcessor struct {
	llm       LLMClient
	chunkSize int
	maxWorkers int
}

func NewMapReduceProcessor(llm LLMClient, chunkSize, maxWorkers int) *MapReduceProcessor {
	return &MapReduceProcessor{
		llm:        llm,
		chunkSize:  chunkSize,
		maxWorkers: maxWorkers,
	}
}

func (p *MapReduceProcessor) Process(ctx context.Context, text string, query string) (string, error) {
	chunks := splitByTokenCount(text, p.chunkSize)

	type mapResult struct {
		index  int
		result string
		err    error
	}

	resultCh := make(chan mapResult, len(chunks))
	sem := make(chan struct{}, p.maxWorkers)

	for i, chunk := range chunks {
		sem <- struct{}{}
		go func(idx int, c string) {
			defer func() { <-sem }()

			prompt := fmt.Sprintf(`Answer the following question based on this text chunk:

Text: %s

Question: %s

Answer:`, c, query)

			resp, err := p.llm.Generate(ctx, []Message{{Role: "user", Content: prompt}})
			resultCh <- mapResult{index: idx, result: resp, err: err}
		}(i, chunk)
	}

	mapResults := make([]string, len(chunks))
	for i := 0; i < len(chunks); i++ {
		r := <-resultCh
		if r.err != nil {
			mapResults[r.index] = "Error processing this chunk"
		} else {
			mapResults[r.index] = r.result
		}
	}

	reducePrompt := fmt.Sprintf(`Based on the following partial answers from different parts of a document, provide a comprehensive and coherent final answer.

Question: %s

Partial answers:
%s

Final comprehensive answer:`, query, strings.Join(mapResults, "\n\n---\n\n"))

	return p.llm.Generate(ctx, []Message{{Role: "user", Content: reducePrompt}})
}
```

### 五、方案4：RAG增强

结合向量检索，只将与问题相关的片段送入LLM。

```go
type RAGLongContextProcessor struct {
	embedder  Embedder
	store     VectorStore
	llm       LLMClient
	topK      int
}

func (p *RAGLongContextProcessor) Index(ctx context.Context, docID string, text string) error {
	chunks := splitByTokenCount(text, 512)

	var vecDocs []VectorDocument
	for i, chunk := range chunks {
		vec := p.embedder.Embed(chunk)
		vecDocs = append(vecDocs, VectorDocument{
			ID:       fmt.Sprintf("%s_%d", docID, i),
			Vector:   vec,
			Content:  chunk,
			Metadata: map[string]string{"doc_id": docID, "chunk_index": fmt.Sprintf("%d", i)},
		})
	}

	return p.store.Upsert(ctx, vecDocs)
}

func (p *RAGLongContextProcessor) Query(ctx context.Context, query string) (string, error) {
	queryVec := p.embedder.Embed(query)
	results, err := p.store.Search(ctx, queryVec, p.topK, nil)
	if err != nil {
		return "", err
	}

	var contextParts []string
	for i, r := range results {
		contextParts = append(contextParts, fmt.Sprintf("[Excerpt %d]\n%s", i+1, r.Content))
	}

	prompt := fmt.Sprintf(`Based on the following excerpts from a long document, answer the question.

Excerpts:
%s

Question: %s

Answer:`, strings.Join(contextParts, "\n\n"), query)

	return p.llm.Generate(ctx, []Message{{Role: "user", Content: prompt}})
}
```

### 六、方案对比

| 方案 | 上下文利用率 | 准确性 | 成本 | 延迟 | 适用场景 |
|------|------------|--------|------|------|---------|
| 滑动窗口 | 中 | 中 | 中 | 中 | 中等长度文档 |
| 摘要压缩 | 低 | 低 | 低 | 低 | 多轮对话 |
| MapReduce | 高 | 中高 | 高 | 低(并行) | 超长文档 |
| RAG增强 | 高 | 高 | 低 | 低 | 知识库问答 |

### 七、总结

长上下文处理的核心思路是**"减少输入、保留关键"**：

1. **RAG**是最优方案：只检索相关片段，成本最低、效果最好
2. **MapReduce**适合全局性问题：需要遍历全文的场景
3. **摘要压缩**适合对话场景：保留上下文连贯性
4. **滑动窗口**是最简方案：适合快速实现

**Lost in the Middle的应对**：将关键信息放在上下文的开头和结尾，中间放次要信息。这是当前LLM的已知特性。
