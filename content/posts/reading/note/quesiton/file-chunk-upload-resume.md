---
title: "大文件分片上传与断点续传如何设计"
date: 2026-05-10T19:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["文件上传", "分片", "golang"]
---

## 问题

大文件上传面临网络不稳定、上传超时、内存占用大等问题。如何设计分片上传和断点续传方案？如何保证上传的完整性？如何实现秒传？

## 回答

大文件上传是Web应用的常见需求。直接上传整个文件存在超时、内存溢出和无法续传等问题。分片上传将大文件切分为小块，逐块上传，支持并行、断点续传和秒传。

### 一、架构设计

```
客户端:
  选择文件 → 计算文件Hash → 检查是否已存在(秒传)
    → 不存在 → 切分分片 → 并行上传分片 → 通知合并

服务端:
  接收分片 → 存储分片 → 记录进度
  合并请求 → 校验完整性 → 合并分片 → 删除临时文件
```

### 二、服务端实现

```go
package upload

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"sync"
)

type UploadService struct {
	uploadDir  string
	chunkDir   string
	mu         sync.Mutex
	uploads    map[string]*UploadProgress
}

type UploadProgress struct {
	FileID    string
	FileName  string
	TotalSize int64
	ChunkSize int
	TotalChunks int
	UploadedChunks map[int]bool
	FileHash  string
}

func NewUploadService(uploadDir, chunkDir string) *UploadService {
	return &UploadService{
		uploadDir: uploadDir,
		chunkDir:  chunkDir,
		uploads:   make(map[string]*UploadProgress),
	}
}

func (s *UploadService) InitUpload(fileID, fileName string, totalSize int64, chunkSize, totalChunks int, fileHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	progress := &UploadProgress{
		FileID:         fileID,
		FileName:       fileName,
		TotalSize:      totalSize,
		ChunkSize:      chunkSize,
		TotalChunks:    totalChunks,
		UploadedChunks: make(map[int]bool),
		FileHash:       fileHash,
	}

	s.uploads[fileID] = progress

	chunkPath := filepath.Join(s.chunkDir, fileID)
	os.MkdirAll(chunkPath, 0755)

	return nil
}

func (s *UploadService) UploadChunk(fileID string, chunkIndex int, file multipart.File) error {
	s.mu.Lock()
	progress, ok := s.uploads[fileID]
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("upload session not found: %s", fileID)
	}

	chunkPath := filepath.Join(s.chunkDir, fileID, fmt.Sprintf("chunk_%d", chunkIndex))

	f, err := os.Create(chunkPath)
	if err != nil {
		return fmt.Errorf("failed to create chunk file: %w", err)
	}
	defer f.Close()

	written, err := io.Copy(f, file)
	if err != nil {
		os.Remove(chunkPath)
		return fmt.Errorf("failed to write chunk: %w", err)
	}

	if written == 0 {
		os.Remove(chunkPath)
		return fmt.Errorf("empty chunk")
	}

	s.mu.Lock()
	progress.UploadedChunks[chunkIndex] = true
	s.mu.Unlock()

	return nil
}

func (s *UploadService) MergeChunks(fileID string) error {
	s.mu.Lock()
	progress, ok := s.uploads[fileID]
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("upload session not found: %s", fileID)
	}

	if len(progress.UploadedChunks) != progress.TotalChunks {
		return fmt.Errorf("not all chunks uploaded: %d/%d", len(progress.UploadedChunks), progress.TotalChunks)
	}

	finalPath := filepath.Join(s.uploadDir, progress.FileName)
	finalFile, err := os.Create(finalPath)
	if err != nil {
		return fmt.Errorf("failed to create final file: %w", err)
	}
	defer finalFile.Close()

	for i := 0; i < progress.TotalChunks; i++ {
		chunkPath := filepath.Join(s.chunkDir, fileID, fmt.Sprintf("chunk_%d", i))
		chunkFile, err := os.Open(chunkPath)
		if err != nil {
			return fmt.Errorf("failed to open chunk %d: %w", i, err)
		}

		_, err = io.Copy(finalFile, chunkFile)
		chunkFile.Close()
		if err != nil {
			return fmt.Errorf("failed to copy chunk %d: %w", i, err)
		}
	}

	if progress.FileHash != "" {
		finalFile.Seek(0, 0)
		hash := md5.New()
		if _, err := io.Copy(hash, finalFile); err != nil {
			return fmt.Errorf("failed to compute hash: %w", err)
		}

		actualHash := hex.EncodeToString(hash.Sum(nil))
		if actualHash != progress.FileHash {
			os.Remove(finalPath)
			return fmt.Errorf("hash mismatch: expected %s, got %s", progress.FileHash, actualHash)
		}
	}

	s.cleanupChunks(fileID)

	s.mu.Lock()
	delete(s.uploads, fileID)
	s.mu.Unlock()

	return nil
}

func (s *UploadService) GetProgress(fileID string) (*UploadProgress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	progress, ok := s.uploads[fileID]
	if !ok {
		return nil, fmt.Errorf("upload session not found")
	}
	return progress, nil
}

func (s *UploadService) CheckInstantUpload(fileHash string) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	filePath := filepath.Join(s.uploadDir, fileHash)
	if _, err := os.Stat(filePath); err == nil {
		return true, filePath
	}
	return false, ""
}

func (s *UploadService) cleanupChunks(fileID string) {
	chunkPath := filepath.Join(s.chunkDir, fileID)
	os.RemoveAll(chunkPath)
}
```

### 三、HTTP接口

```go
func UploadHandler(service *UploadService) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/upload/init", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			FileID      string `json:"file_id"`
			FileName    string `json:"file_name"`
			TotalSize   int64  `json:"total_size"`
			ChunkSize   int    `json:"chunk_size"`
			TotalChunks int    `json:"total_chunks"`
			FileHash    string `json:"file_hash"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if ok, _ := service.CheckInstantUpload(req.FileHash); ok {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"instant": true,
				"message": "file already exists",
			})
			return
		}

		service.InitUpload(req.FileID, req.FileName, req.TotalSize, req.ChunkSize, req.TotalChunks, req.FileHash)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"instant": false,
			"file_id": req.FileID,
		})
	})

	mux.HandleFunc("/upload/chunk", func(w http.ResponseWriter, r *http.Request) {
		fileID := r.FormValue("file_id")
		chunkIndex, _ := strconv.Atoi(r.FormValue("chunk_index"))

		file, _, err := r.FormFile("chunk")
		if err != nil {
			http.Error(w, "chunk data required", http.StatusBadRequest)
			return
		}
		defer file.Close()

		if err := service.UploadChunk(fileID, chunkIndex, file); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"chunk":  chunkIndex,
		})
	})

	mux.HandleFunc("/upload/merge", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			FileID string `json:"file_id"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if err := service.MergeChunks(req.FileID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "merged",
		})
	})

	mux.HandleFunc("/upload/progress", func(w http.ResponseWriter, r *http.Request) {
		fileID := r.URL.Query().Get("file_id")
		progress, err := service.GetProgress(fileID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"uploaded_chunks": len(progress.UploadedChunks),
			"total_chunks":    progress.TotalChunks,
			"progress":        float64(len(progress.UploadedChunks)) / float64(progress.TotalChunks) * 100,
		})
	})

	return mux
}
```

### 四、秒传实现

秒传的核心是文件Hash比对：

```
客户端: 计算文件MD5 → 发送到服务端检查
服务端: Hash已存在 → 返回"已上传" → 客户端跳过上传
        Hash不存在 → 返回"需上传" → 正常分片上传
```

**大文件Hash优化**：对文件首尾各取1MB + 文件大小组合计算Hash，避免全文件计算。

### 五、分片大小选择

| 文件大小 | 推荐分片大小 | 原因 |
|---------|------------|------|
| <100MB | 2MB | 分片数少，管理简单 |
| 100MB~1GB | 5MB | 平衡并行度和开销 |
| 1GB~10GB | 10MB | 减少HTTP请求次数 |
| >10GB | 20MB | 减少元数据管理开销 |

### 六、总结

大文件分片上传的核心设计：

1. **分片上传**：大文件切分为小块，支持并行和断点续传
2. **断点续传**：服务端记录已上传分片，客户端查询后跳过
3. **秒传**：通过文件Hash判断是否已存在
4. **完整性校验**：合并后校验Hash，确保数据完整
5. **清理机制**：合并后删除临时分片，避免磁盘浪费

**行业实践**：阿里OSS分片上传、腾讯COS分片上传、七牛云分片上传
