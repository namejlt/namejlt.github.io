package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 数据类型常量
const (
	TypeInteger = iota
	TypeString
	TypeBoolean
)

// 文件存储相关常量
const (
	headerSize        = 1024
	pageSize          = 4096
	maxFileNameLength = 255
)

// 页类型
const (
	pageTypeHeader = iota
	pageTypeData
	pageTypeIndex
)

// 表结构定义
type Table struct {
	Name    string
	Columns []Column
	Indexes []Index
}

type Column struct {
	Name     string
	Type     int
	Nullable bool
}

type Index struct {
	Name    string
	Columns []string
	Unique  bool
}

// 数据库模式
type Schema struct {
	Tables map[string]Table
	mu     sync.RWMutex
}

// 事务日志记录
type TransactionLog struct {
	ID         int64
	Timestamp  time.Time
	Operations []LogOperation
	Committed  bool
}

type LogOperation struct {
	Table    string
	Action   string // INSERT, UPDATE, DELETE
	Key      []byte
	Value    []byte
	OldValue []byte
}

// 数据页
type DataPage struct {
	PageType  int
	PageID    int64
	NextPage  int64
	FreeSpace int
	Records   []Record
}

type Record struct {
	Key   []byte
	Value []byte
}

// 数据库实例
type Database struct {
	Schema    Schema
	DataDir   string
	walFile   *os.File
	logs      []TransactionLog
	activeTxs map[int64]*Transaction
	mu        sync.RWMutex
	fileLocks map[string]*sync.RWMutex
}

// 事务
type Transaction struct {
	ID         int64
	db         *Database
	committed  bool
	rolledBack bool
	operations []LogOperation
}

// 查询统计
type QueryStats struct {
	Query         string
	ExecutionTime time.Duration
	RowsAffected  int
}

// 初始化数据库
func NewDatabase(dataDir string) (*Database, error) {
	// 创建数据目录
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return nil, err
		}
	}

	// 打开WAL文件
	walPath := filepath.Join(dataDir, "write_ahead_log.wal")
	walFile, err := os.OpenFile(walPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	db := &Database{
		Schema:    Schema{Tables: make(map[string]Table)},
		DataDir:   dataDir,
		walFile:   walFile,
		activeTxs: make(map[int64]*Transaction),
		fileLocks: make(map[string]*sync.RWMutex),
	}

	// 恢复数据库状态
	if err := db.recoverFromLog(); err != nil {
		return nil, err
	}

	return db, nil
}

// 从日志恢复数据库
func (db *Database) recoverFromLog() error {
	// 读取WAL文件并恢复未完成的事务
	// 简化实现，实际需要解析日志文件
	return nil
}

// 创建表
func (db *Database) CreateTable(table Table) error {
	db.Schema.mu.Lock()
	defer db.Schema.mu.Unlock()

	if _, exists := db.Schema.Tables[table.Name]; exists {
		return fmt.Errorf("table %s already exists", table.Name)
	}

	// 创建表文件
	tablePath := filepath.Join(db.DataDir, table.Name+".db")
	file, err := os.Create(tablePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 初始化文件头
	header := make([]byte, headerSize)
	binary.BigEndian.PutUint32(header[0:4], uint32(pageTypeHeader))
	binary.BigEndian.PutUint64(header[4:12], 1) // 第一页ID

	// 写入表结构
	tableData, err := json.Marshal(table)
	if err != nil {
		return err
	}
	copy(header[12:], tableData)

	if _, err := file.Write(header); err != nil {
		return err
	}

	// 初始化第一个数据页
	firstPage := DataPage{
		PageType:  pageTypeData,
		PageID:    1,
		NextPage:  -1,
		FreeSpace: pageSize - 32, // 保留一些空间用于页头
		Records:   make([]Record, 0),
	}

	if err := db.writePage(table.Name, firstPage); err != nil {
		return err
	}

	db.Schema.Tables[table.Name] = table
	return nil
}

// 写入页
func (db *Database) writePage(tableName string, page DataPage) error {
	tablePath := filepath.Join(db.DataDir, tableName+".db")

	// 获取文件锁
	db.getFileLock(tablePath).Lock()
	defer db.getFileLock(tablePath).Unlock()

	file, err := os.OpenFile(tablePath, os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// 序列化页
	pageData, err := json.Marshal(page)
	if err != nil {
		return err
	}

	// 计算偏移量
	offset := headerSize + (page.PageID-1)*pageSize

	// 确保文件足够大
	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}
	if int64(offset+pageSize) > fileInfo.Size() {
		if err := file.Truncate(int64(offset + pageSize)); err != nil {
			return err
		}
	}

	// 写入页数据
	if _, err := file.Seek(int64(offset), 0); err != nil {
		return err
	}

	// 写入页头
	pageHeader := make([]byte, 32)
	binary.BigEndian.PutUint32(pageHeader[0:4], uint32(page.PageType))
	binary.BigEndian.PutUint64(pageHeader[4:12], uint64(page.PageID))
	binary.BigEndian.PutUint64(pageHeader[12:20], uint64(page.NextPage))
	binary.BigEndian.PutUint32(pageHeader[20:24], uint32(len(page.Records)))
	binary.BigEndian.PutUint32(pageHeader[24:28], uint32(page.FreeSpace))

	if _, err := file.Write(pageHeader); err != nil {
		return err
	}

	// 写入记录数
	if _, err := file.Write(pageData); err != nil {
		return err
	}

	return nil
}

// 读取页
func (db *Database) readPage(tableName string, pageID int64) (DataPage, error) {
	tablePath := filepath.Join(db.DataDir, tableName+".db")

	// 获取文件锁
	db.getFileLock(tablePath).RLock()
	defer db.getFileLock(tablePath).RUnlock()

	file, err := os.Open(tablePath)
	if err != nil {
		return DataPage{}, err
	}
	defer file.Close()

	// 计算偏移量
	offset := headerSize + (pageID-1)*pageSize

	// 定位到页
	if _, err := file.Seek(int64(offset), 0); err != nil {
		return DataPage{}, err
	}

	// 读取页头
	pageHeader := make([]byte, 32)
	if _, err := io.ReadFull(file, pageHeader); err != nil {
		return DataPage{}, err
	}

	page := DataPage{
		PageType:  int(binary.BigEndian.Uint32(pageHeader[0:4])),
		PageID:    int64(binary.BigEndian.Uint64(pageHeader[4:12])),
		NextPage:  int64(binary.BigEndian.Uint64(pageHeader[12:20])),
		FreeSpace: int(binary.BigEndian.Uint32(pageHeader[24:28])),
		Records:   make([]Record, 0),
	}

	// 读取记录
	recordCount := int(binary.BigEndian.Uint32(pageHeader[20:24]))
	for i := 0; i < recordCount; i++ {
		// 读取键长度
		keyLenBytes := make([]byte, 4)
		if _, err := io.ReadFull(file, keyLenBytes); err != nil {
			return DataPage{}, err
		}
		keyLen := int(binary.BigEndian.Uint32(keyLenBytes))

		// 读取键
		key := make([]byte, keyLen)
		if _, err := io.ReadFull(file, key); err != nil {
			return DataPage{}, err
		}

		// 读取值长度
		valLenBytes := make([]byte, 4)
		if _, err := io.ReadFull(file, valLenBytes); err != nil {
			return DataPage{}, err
		}
		valLen := int(binary.BigEndian.Uint32(valLenBytes))

		// 读取值
		value := make([]byte, valLen)
		if _, err := io.ReadFull(file, value); err != nil {
			return DataPage{}, err
		}

		page.Records = append(page.Records, Record{Key: key, Value: value})
	}

	return page, nil
}

// 开始事务
func (db *Database) BeginTransaction() (*Transaction, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	txID := time.Now().UnixNano()
	tx := &Transaction{
		ID:         txID,
		db:         db,
		committed:  false,
		rolledBack: false,
		operations: make([]LogOperation, 0),
	}

	db.activeTxs[txID] = tx

	// 记录事务开始日志
	logEntry := TransactionLog{
		ID:         txID,
		Timestamp:  time.Now(),
		Operations: nil,
		Committed:  false,
	}

	db.logs = append(db.logs, logEntry)
	db.writeTransactionLog(logEntry)

	return tx, nil
}

// 提交事务
func (tx *Transaction) Commit() error {
	tx.db.mu.Lock()
	defer tx.db.mu.Unlock()

	if tx.committed || tx.rolledBack {
		return fmt.Errorf("transaction already %s",
			func() string {
				if tx.committed {
					return "committed"
				}
				return "rolled back"
			}())
	}

	// 写入所有操作到日志
	for _, op := range tx.operations {
		tx.db.writeOperationToLog(tx.ID, op)
	}

	// 应用所有操作
	for _, op := range tx.operations {
		switch op.Action {
		case "INSERT":
			if err := tx.db.insertRecord(op.Table, op.Key, op.Value); err != nil {
				return err
			}
			// 其他操作...
		}
	}

	// 标记事务为已提交
	for i, log := range tx.db.logs {
		if log.ID == tx.ID {
			tx.db.logs[i].Committed = true
			break
		}
	}

	tx.committed = true
	delete(tx.db.activeTxs, tx.ID)
	return nil
}

// 回滚事务
func (tx *Transaction) Rollback() error {
	tx.db.mu.Lock()
	defer tx.db.mu.Unlock()

	if tx.committed || tx.rolledBack {
		return fmt.Errorf("transaction already %s",
			func() string {
				if tx.committed {
					return "committed"
				}
				return "rolled back"
			}())
	}

	// 回滚操作 (简化实现)
	tx.rolledBack = true
	delete(tx.db.activeTxs, tx.ID)
	return nil
}

// 插入记录
func (tx *Transaction) Insert(tableName string, key []byte, value []byte) error {
	if tx.committed || tx.rolledBack {
		return fmt.Errorf("transaction already %s",
			func() string {
				if tx.committed {
					return "committed"
				}
				return "rolled back"
			}())
	}

	// 记录操作
	op := LogOperation{
		Table:    tableName,
		Action:   "INSERT",
		Key:      key,
		Value:    value,
		OldValue: nil,
	}

	tx.operations = append(tx.operations, op)
	return nil
}

// 实际插入记录到存储
func (db *Database) insertRecord(tableName string, key, value []byte) error {
	// 查找表
	_, exists := db.Schema.Tables[tableName]
	if !exists {
		return fmt.Errorf("table %s does not exist", tableName)
	}

	// 查找第一个有足够空间的页
	var page DataPage
	var pageID int64 = 1
	var err error

	for {
		page, err = db.readPage(tableName, pageID)
		if err != nil {
			return err
		}

		// 计算新记录需要的空间
		recordSize := 8 + len(key) + len(value) // 4字节键长度 + 4字节值长度 + 键 + 值

		if page.FreeSpace >= recordSize {
			break // 找到足够空间的页
		}

		if page.NextPage == -1 {
			// 需要创建新页
			newPage := DataPage{
				PageType:  pageTypeData,
				PageID:    page.PageID + 1,
				NextPage:  -1,
				FreeSpace: pageSize - 32,
				Records:   make([]Record, 0),
			}

			if err := db.writePage(tableName, newPage); err != nil {
				return err
			}

			// 更新当前页的NextPage
			page.NextPage = newPage.PageID
			if err := db.writePage(tableName, page); err != nil {
				return err
			}

			page = newPage
			break
		}

		pageID = page.NextPage
	}

	// 添加记录
	page.Records = append(page.Records, Record{Key: key, Value: value})
	page.FreeSpace -= (8 + len(key) + len(value))

	// 写入更新后的页
	return db.writePage(tableName, page)
}

// 查询数据
func (db *Database) Query(tableName string, key []byte) ([]byte, error) {
	db.Schema.mu.RLock()
	defer db.Schema.mu.RUnlock()

	// 检查表是否存在
	if _, exists := db.Schema.Tables[tableName]; !exists {
		return nil, fmt.Errorf("table %s does not exist", tableName)
	}

	// 遍历所有数据页查找记录
	var pageID int64 = 1

	for {
		page, err := db.readPage(tableName, pageID)
		if err != nil {
			return nil, err
		}

		// 查找记录
		for _, record := range page.Records {
			if string(record.Key) == string(key) {
				return record.Value, nil
			}
		}

		// 如果没有更多页，返回未找到
		if page.NextPage == -1 {
			break
		}

		pageID = page.NextPage
	}

	return nil, nil // 未找到记录
}

// 写入事务日志
func (db *Database) writeTransactionLog(log TransactionLog) error {
	// 序列化日志
	logData, err := json.Marshal(log)
	if err != nil {
		return err
	}

	// 添加日志长度前缀
	logLen := len(logData)
	lenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBytes, uint32(logLen))

	// 写入日志
	// 需要使用互斥锁来保护WAL文件的写入
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, err := db.walFile.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	if _, err := db.walFile.Write(lenBytes); err != nil {
		return err
	}

	if _, err := db.walFile.Write(logData); err != nil {
		return err
	}

	return nil
}

// 写入操作日志
func (db *Database) writeOperationToLog(txID int64, op LogOperation) error {
	// 查找对应的事务日志
	for i, log := range db.logs {
		if log.ID == txID {
			db.logs[i].Operations = append(db.logs[i].Operations, op)
			return db.writeTransactionLog(db.logs[i])
		}
	}

	return fmt.Errorf("transaction %d not found", txID)
}

// 获取文件锁
func (db *Database) getFileLock(filePath string) *sync.RWMutex {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.fileLocks[filePath]; !exists {
		db.fileLocks[filePath] = &sync.RWMutex{}
	}

	return db.fileLocks[filePath]
}

// HTTP处理函数
func setupHTTPHandlers(db *Database) {
	http.HandleFunc("/createTable", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var table Table
		if err := json.NewDecoder(r.Body).Decode(&table); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		if err := db.CreateTable(table); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Table %s created successfully", table.Name)
	})

	http.HandleFunc("/transaction", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var request struct {
			Operations []struct {
				TableName string      `json:"tableName"`
				Key       string      `json:"key"`
				Value     interface{} `json:"value"`
			} `json:"operations"`
		}

		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		tx, err := db.BeginTransaction()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for _, op := range request.Operations {
			// 序列化值
			valueBytes, err := json.Marshal(op.Value)
			if err != nil {
				tx.Rollback()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			if err := tx.Insert(op.TableName, []byte(op.Key), valueBytes); err != nil {
				tx.Rollback()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Transaction committed successfully")
	})

	http.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		tableName := r.URL.Query().Get("table")
		key := r.URL.Query().Get("key")

		if tableName == "" || key == "" {
			http.Error(w, "Missing table or key parameter", http.StatusBadRequest)
			return
		}

		value, err := db.Query(tableName, []byte(key))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if value == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(value)
	})
}

func main() {
	db, err := NewDatabase("./data")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.walFile.Close()

	setupHTTPHandlers(db)

	log.Println("Server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
