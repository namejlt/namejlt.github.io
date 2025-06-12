---
title: "如何快速实现一个小型数据库"
date: 2025-06-12T16:00:00+08:00
draft: false
toc: true
categories: ["阅读/笔记/问题"]
tags: ["数据库", "golang"]
---

## 问题


数据库用来存储和查询数据，现在有很多种类的数据，提供各种各样的数据存储和查询，分别适用各种不同场景，一个最简单的acid的数据库如何实现？


## 回答

ACID 是数据库事务的四个基本特性，是确保数据一致性和可靠性的关键标准：

- 原子性（Atomicity）：事务中的所有操作要么全部成功，要么全部失败回滚
- 一致性（Consistency）：事务执行前后数据库状态保持合法，约束条件始终满足
- 隔离性（Isolation）：多个事务并发执行时，每个事务感觉不到其他事务的存在
- 持久性（Durability）：事务提交后，其结果永久保存在数据库中，即使系统崩溃也不会丢失

我们来一步一步实现

### 数据库1.0

可以保存和查询数据，内存数据库

通过golang，map来实现一个简单的内存数据库


#### 表结构设计与实现


```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// 定义数据项结构
type Item struct {
	ID        string      `json:"id"`
	Data      interface{} `json:"data"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// 定义数据库结构
type Database struct {
	data map[string]Item
	mu   sync.RWMutex
}

// 创建新数据库实例
func NewDatabase() *Database {
	return &Database{
		data: make(map[string]Item),
	}
}

// 保存数据
func (db *Database) Save(id string, data interface{}) Item {
	db.mu.Lock()
	defer db.mu.Unlock()

	now := time.Now()
	item := Item{
		ID:        id,
		Data:      data,
		CreatedAt: now,
		UpdatedAt: now,
	}

	db.data[id] = item
	return item
}

// 查询数据
func (db *Database) Get(id string) (Item, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	item, exists := db.data[id]
	return item, exists
}

// 查询所有数据
func (db *Database) GetAll() []Item {
	db.mu.RLock()
	defer db.mu.RUnlock()

	items := make([]Item, 0, len(db.data))
	for _, item := range db.data {
		items = append(items, item)
	}
	return items
}

// 更新数据
func (db *Database) Update(id string, data interface{}) (Item, bool) {
	db.mu.Lock()
	defer db.mu.Unlock()

	item, exists := db.data[id]
	if !exists {
		return item, false
	}

	item.Data = data
	item.UpdatedAt = time.Now()
	db.data[id] = item
	return item, true
}

// 删除数据
func (db *Database) Delete(id string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	_, exists := db.data[id]
	if !exists {
		return false
	}

	delete(db.data, id)
	return true
}

// HTTP 服务器实现
func main() {
	db := NewDatabase()

	// 定义 HTTP 路由
	http.HandleFunc("/items/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// 从 URL 中获取 ID
		id := r.URL.Path[len("/items/"):]

		switch r.Method {
		case http.MethodPost:
			// 保存数据
			var data interface{}
			if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			item := db.Save(id, data)
			json.NewEncoder(w).Encode(item)

		case http.MethodGet:
			// 查询数据
			if id == "" {
				// 查询所有
				items := db.GetAll()
				json.NewEncoder(w).Encode(items)
			} else {
				// 查询单个
				item, exists := db.Get(id)
				if !exists {
					http.Error(w, "Item not found", http.StatusNotFound)
					return
				}
				json.NewEncoder(w).Encode(item)
			}

		case http.MethodPut:
			// 更新数据
			var data interface{}
			if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			item, updated := db.Update(id, data)
			if !updated {
				http.Error(w, "Item not found", http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(item)

		case http.MethodDelete:
			// 删除数据
			deleted := db.Delete(id)
			if !deleted {
				http.Error(w, "Item not found", http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// 启动服务器
	fmt.Println("Server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
```

这个实现包含以下关键部分：

1. 数据结构：定义了 Item 结构用于存储数据项，包含 ID、数据内容、创建时间和更新时间。
2. 数据库结构：Database 结构使用 map 存储数据，并通过读写锁保证并发安全。
3. 核心方法：
    Save()：保存新数据或更新现有数据
    Get()：根据 ID 查询单个数据项
    GetAll()：查询所有数据项
    Update()：更新现有数据项
    Delete()：根据 ID 删除数据项
4. HTTP 接口：通过 HTTP 服务器提供 RESTful API，支持 POST、GET、PUT 和 DELETE 方法。

### 数据库2.0

在之前的内存数据库基础上，我们可以通过增加表结构定义来实现更规范的数据管理。

- 定义表结构
- 增加表索引

#### 表结构设计与实现


```go
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Column 定义表中的列
type Column struct {
	Name       string
	Type       string // 例如: "string", "int", "float64", "bool", "time.Time"
	IsPrimaryKey bool
}

// Table 定义数据库中的表
type Table struct {
	Name    string
	Columns []Column
	// primaryKeyColumnName string // 存储主键列的名称，在CreateTable时确定
	// Rows 存储实际的行数据，key是主键的值，value是列名到其值的映射
	Rows    map[string]map[string]interface{}
	mu      sync.RWMutex // 保护Rows的读写
}

// DatabaseV2 定义数据库的第二版，支持多表和结构定义
type DatabaseV2 struct {
	tables map[string]*Table
	mu     sync.RWMutex // 保护tables的读写
}

// NewDatabaseV2 创建新的数据库V2实例
func NewDatabaseV2() *DatabaseV2 {
	return &DatabaseV2{
		tables: make(map[string]*Table),
	}
}

// CreateTable 创建一个新表并定义其结构
func (db *DatabaseV2) CreateTable(tableName string, columns []Column) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.tables[tableName]; exists {
		return fmt.Errorf("表 '%s' 已经存在", tableName)
	}

	primaryKeyCount := 0
	for _, col := range columns {
		if col.IsPrimaryKey {
			primaryKeyCount++
		}
	}
	if primaryKeyCount != 1 {
		return errors.New("每个表必须且只能有一个主键")
	}

	newTable := &Table{
		Name:    tableName,
		Columns: columns,
		Rows:    make(map[string]map[string]interface{}),
	}
	db.tables[tableName] = newTable
	return nil
}

// GetTable 获取一个表实例
func (db *DatabaseV2) GetTable(tableName string) (*Table, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	table, exists := db.tables[tableName]
	if !exists {
		return nil, fmt.Errorf("表 '%s' 不存在", tableName)
	}
	return table, nil
}

// Insert 向表中插入一行数据
func (t *Table) Insert(data map[string]interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	var primaryKeyVal string
	primaryKeyName := ""

	// 验证数据并查找主键
	rowData := make(map[string]interface{})
	for _, col := range t.Columns {
		val, ok := data[col.Name]
		if !ok {
			return fmt.Errorf("列 '%s' 缺失", col.Name)
		}
		// 简单的类型验证（这里可以扩展为更严格的类型转换和验证）
		// 注意：JSON解码数字默认为float64，需要处理
		switch col.Type {
		case "string":
			if _, isString := val.(string); !isString {
				return fmt.Errorf("列 '%s' 期望类型为 string, 实际为 %T", col.Name, val)
			}
		case "int":
			if fVal, isFloat := val.(float64); isFloat {
				val = int(fVal) // 尝试从float64转换
			} else if _, isInt := val.(int); !isInt {
				return fmt.Errorf("列 '%s' 期望类型为 int, 实际为 %T", col.Name, val)
			}
		case "float64":
			if _, isFloat := val.(float64); !isFloat {
				return fmt.Errorf("列 '%s' 期望类型为 float64, 实际为 %T", col.Name, val)
			}
		case "bool":
			if _, isBool := val.(bool); !isBool {
				return fmt.Errorf("列 '%s' 期望类型为 bool, 实际为 %T", col.Name, val)
			}
		case "time.Time":
			if _, isTime := val.(time.Time); !isTime {
				return fmt.Errorf("列 '%s' 期望类型为 time.Time, 实际为 %T", col.Name, val)
			}
		default:
			return fmt.Errorf("不支持的列类型 '%s' ", col.Type)
		}
		
		rowData[col.Name] = val

		if col.IsPrimaryKey {
			pk, ok := val.(string)
			if !ok {
				return errors.New("主键必须是字符串类型") // 简化处理，主键强制为string
			}
			primaryKeyVal = pk
			primaryKeyName = col.Name
		}
	}

	if primaryKeyVal == "" {
		return errors.New("未找到主键值")
	}

	if _, exists := t.Rows[primaryKeyVal]; exists {
		return fmt.Errorf("主键 '%s' 的值 '%s' 已经存在", primaryKeyName, primaryKeyVal)
	}

	t.Rows[primaryKeyVal] = rowData
	return nil
}

// Select 根据主键查询一行数据
func (t *Table) Select(primaryKey string) (map[string]interface{}, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	row, exists := t.Rows[primaryKey]
	if !exists {
		return nil, fmt.Errorf("未找到主键为 '%s' 的数据", primaryKey)
	}
	// 返回一个副本以防止外部修改
	copyRow := make(map[string]interface{})
	for k, v := range row {
		copyRow[k] = v
	}
	return copyRow, nil
}

// SelectAll 查询表中的所有数据
func (t *Table) SelectAll() ([]map[string]interface{}, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	rows := make([]map[string]interface{}, 0, len(t.Rows))
	for _, row := range t.Rows {
		// 返回副本
		copyRow := make(map[string]interface{})
		for k, v := range row {
			copyRow[k] = v
		}
		rows = append(rows, copyRow)
	}
	return rows, nil
}

// Update 更新表中一行数据
func (t *Table) Update(primaryKey string, newData map[string]interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	existingRow, exists := t.Rows[primaryKey]
	if !exists {
		return fmt.Errorf("未找到主键为 '%s' 的数据进行更新", primaryKey)
	}

	for _, col := range t.Columns {
		if val, ok := newData[col.Name]; ok {
			// 主键不允许更新
			if col.IsPrimaryKey {
				return fmt.Errorf("不允许更新主键列 '%s'", col.Name)
			}
			// 简单的类型验证（这里可以扩展为更严格的类型转换和验证）
			switch col.Type {
			case "string":
				if _, isString := val.(string); !isString {
					return fmt.Errorf("更新失败：列 '%s' 期望类型为 string, 实际为 %T", col.Name, val)
				}
			case "int":
				if fVal, isFloat := val.(float64); isFloat {
					val = int(fVal)
				} else if _, isInt := val.(int); !isInt {
					return fmt.Errorf("更新失败：列 '%s' 期望类型为 int, 实际为 %T", col.Name, val)
				}
			case "float64":
				if _, isFloat := val.(float64); !isFloat {
					return fmt.Errorf("更新失败：列 '%s' 期望类型为 float64, 实际为 %T", col.Name, val)
				}
			case "bool":
				if _, isBool := val.(bool); !isBool {
					return fmt.Errorf("更新失败：列 '%s' 期望类型为 bool, 实际为 %T", col.Name, val)
				}
			case "time.Time":
				if _, isTime := val.(time.Time); !isTime {
					return fmt.Errorf("更新失败：列 '%s' 期望类型为 time.Time, 实际为 %T", col.Name, val)
				}
			default:
				return fmt.Errorf("不支持的列类型 '%s' ", col.Type)
			}
			existingRow[col.Name] = val
		}
	}
	return nil
}

// Delete 删除表中一行数据
func (t *Table) Delete(primaryKey string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.Rows[primaryKey]; !exists {
		return fmt.Errorf("未找到主键为 '%s' 的数据进行删除", primaryKey)
	}
	delete(t.Rows, primaryKey)
	return nil
}

/*
// 示例用法 (可以添加到 main 函数中进行测试)
func main() {
	dbV2 := NewDatabaseV2()

	// 定义用户表的结构
	userColumns := []Column{
		{Name: "id", Type: "string", IsPrimaryKey: true},
		{Name: "name", Type: "string"},
		{Name: "age", Type: "int"},
		{Name: "email", Type: "string"},
		{Name: "is_active", Type: "bool"},
		{Name: "created_at", Type: "time.Time"},
	}

	// 创建用户表
	err := dbV2.CreateTable("users", userColumns)
	if err != nil {
		fmt.Println("创建表失败:", err)
		// log.Fatal("创建表失败:", err) // 如果在实际应用中，可以使用log.Fatal
		return
	}
	fmt.Println("表 'users' 创建成功。")

	// 获取用户表
	usersTable, err := dbV2.GetTable("users")
	if err != nil {
		fmt.Println("获取表失败:", err)
		return
	}

	// 插入数据
	err = usersTable.Insert(map[string]interface{}{
		"id": "user1",
		"name": "Alice",
		"age": 30, // JSON numbers might be float64, handle this
		"email": "alice@example.com",
		"is_active": true,
		"created_at": time.Now(),
	})
	if err != nil {
		fmt.Println("插入数据失败:", err)
	} else {
		fmt.Println("数据插入成功: user1")
	}

	err = usersTable.Insert(map[string]interface{}{
		"id": "user2",
		"name": "Bob",
		"age": 25,
		"email": "bob@example.com",
		"is_active": false,
		"created_at": time.Now(),
	})
	if err != nil {
		fmt.Println("插入数据失败:", err)
	} else {
		fmt.Println("数据插入成功: user2")
	}

	// 尝试插入重复主键
	err = usersTable.Insert(map[string]interface{}{
		"id": "user1",
		"name": "Charlie",
		"age": 35,
		"email": "charlie@example.com",
		"is_active": true,
		"created_at": time.Now(),
	})
	if err != nil {
		fmt.Println("插入重复主键失败 (预期):", err)
	}

	// 查询所有数据
	allUsers, err := usersTable.SelectAll()
	if err != nil {
		fmt.Println("查询所有数据失败:", err)
	} else {
		fmt.Println("\n所有用户:")
		for _, user := range allUsers {
			fmt.Printf("ID: %s, Name: %s, Age: %d, Email: %s, Active: %t, CreatedAt: %v\n",
				user["id"], user["name"], user["age"], user["email"], user["is_active"], user["created_at"])
		}
	}

	// 查询单个数据
	user1, err := usersTable.Select("user1")
	if err != nil {
		fmt.Println("查询 user1 失败:", err)
	} else {
		fmt.Println("\n查询 user1:")
		fmt.Printf("ID: %s, Name: %s, Age: %d\n", user1["id"], user1["name"], user1["age"])
	}

	// 更新数据
	err = usersTable.Update("user1", map[string]interface{}{
		"age": 31,
		"email": "alice.updated@example.com",
		// "id": "user1-new", // 尝试更新主键，会失败
	})
	if err != nil {
		fmt.Println("更新 user1 失败:", err)
	} else {
		fmt.Println("\n更新 user1 成功。")
		updatedUser1, _ := usersTable.Select("user1")
		fmt.Printf("更新后 user1: ID: %s, Name: %s, Age: %d, Email: %s\n",
			updatedUser1["id"], updatedUser1["name"], updatedUser1["age"], updatedUser1["email"])
	}

	// 删除数据
	err = usersTable.Delete("user2")
	if err != nil {
		fmt.Println("删除 user2 失败:", err)
	} else {
		fmt.Println("\n删除 user2 成功。")
	}

	// 再次查询所有数据
	allUsersAfterDelete, err := usersTable.SelectAll()
	if err != nil {
		fmt.Println("查询所有数据失败:", err)
	} else {
		fmt.Println("\n删除 user2 后的所有用户:")
		if len(allUsersAfterDelete) == 0 {
			fmt.Println("无用户")
		} else {
			for _, user := range allUsersAfterDelete {
				fmt.Printf("ID: %s, Name: %s, Age: %d\n", user["id"], user["name"], user["age"])
			}
		}
	}
}
*/
```

以上实现表结构定义，数据操作只能通过主键来进行

### 数据库3.0

在之前的基础上，我们将增加ACID相关实现，重点在于事务的实现

#### 表结构设计与实现

```go
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Column 定义表中的列
type Column struct {
	Name       string
	Type       string // 例如: "string", "int", "float64", "bool", "time.Time"
	IsPrimaryKey bool
}

// Table 定义数据库中的表
type Table struct {
	Name    string
	Columns []Column
	// primaryKeyColumnName string // 存储主键列的名称，在CreateTable时确定
	// Rows 存储实际的行数据，key是主键的值，value是列名到其值的映射
	Rows    map[string]map[string]interface{}
	mu      sync.RWMutex // 保护Rows的读写，主要用于非事务性操作或事务内部表的细粒度锁（这里简化为事务全局锁）
}

// deepCopy 创建Table的深拷贝，用于事务沙盒
func (t *Table) deepCopy() *Table {
	t.mu.RLock() // 读锁保护原表的Rows
	defer t.mu.RUnlock()

	newTable := &Table{
		Name:    t.Name,
		Columns: make([]Column, len(t.Columns)),
		Rows:    make(map[string]map[string]interface{}, len(t.Rows)),
	}
	copy(newTable.Columns, t.Columns) // Columns是值类型，直接拷贝即可

	// 深拷贝Rows
	for pk, row := range t.Rows {
		newRow := make(map[string]interface{}, len(row))
		for colName, val := range row {
			newRow[colName] = val // 假设值为基本类型或深拷贝（对于time.Time等需注意，这里简化处理）
		}
		newTable.Rows[pk] = newRow
	}
	return newTable
}

// DatabaseV3 定义数据库的第三版，支持多表和事务
type DatabaseV3 struct {
	tables map[string]*Table
	mu     sync.RWMutex // 保护tables map的读写（如CreateTable, GetTable），与txMu分开
	txMu   sync.Mutex   // 全局事务锁，确保同一时间只有一个事务处于活动状态
}

// NewDatabaseV3 创建新的DatabaseV3实例
func NewDatabaseV3() *DatabaseV3 {
	return &DatabaseV3{
		tables: make(map[string]*Table),
	}
}

// CreateTable 创建一个新表并定义其结构 (非事务性操作)
func (db *DatabaseV3) CreateTable(tableName string, columns []Column) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.tables[tableName]; exists {
		return fmt.Errorf("表 '%s' 已经存在", tableName)
	}

	primaryKeyCount := 0
	for _, col := range columns {
		if col.IsPrimaryKey {
			primaryKeyCount++
		}
	}
	if primaryKeyCount != 1 {
		return errors.New("每个表必须且只能有一个主键")
	}

	newTable := &Table{
		Name:    tableName,
		Columns: columns,
		Rows:    make(map[string]map[string]interface{}),
	}
	db.tables[tableName] = newTable
	return nil
}

// GetTable 获取一个表实例 (非事务性操作)
func (db *DatabaseV3) GetTable(tableName string) (*Table, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	table, exists := db.tables[tableName]
	if !exists {
		return nil, fmt.Errorf("表 '%s' 不存在", tableName)
	}
	return table, nil
}

// Transaction 定义一个数据库事务
type Transaction struct {
	db         *DatabaseV3           // 对主数据库的引用
	tempTables map[string]*Table     // 事务的私有工作空间（深拷贝的表数据）
	isActive   bool                  // 标记事务是否活跃
	isCommittedOrRolledBack bool     // 标记事务是否已提交或回滚
}

// Begin 开始一个新事务
func (db *DatabaseV3) Begin() (*Transaction, error) {
	// 获取全局事务锁，确保串行化隔离
	db.txMu.Lock()

	// 创建所有表的深拷贝作为事务的私有工作空间
	db.mu.RLock() // 保护主数据库tables在拷贝时不变
	tempTables := make(map[string]*Table, len(db.tables))
	for name, table := range db.tables {
		tempTables[name] = table.deepCopy()
	}
	db.mu.RUnlock()

	tx := &Transaction{
		db:         db,
		tempTables: tempTables,
		isActive:   true,
		isCommittedOrRolledBack: false,
	}
	return tx, nil
}

// Commit 提交事务，将更改应用到主数据库
func (tx *Transaction) Commit() error {
	if !tx.isActive || tx.isCommittedOrRolledBack {
		return errors.New("事务已提交或回滚，或不活跃")
	}

	// 延迟释放全局事务锁
	defer tx.db.txMu.Unlock()
	tx.isCommittedOrRolledBack = true // 标记事务已处理

	// 锁定主数据库的tables，将tempTables中的更改应用过去
	tx.db.mu.Lock()
	defer tx.db.mu.Unlock()

	for tableName, tempTable := range tx.tempTables {
		// 检查表是否存在于主数据库中，如果不存在可能是事务中创建的新表
		if _, exists := tx.db.tables[tableName]; exists {
			tx.db.tables[tableName].Rows = tempTable.Rows // 直接替换Rows map
		} else {
			// 如果是新创建的表，直接添加到主数据库
			tx.db.tables[tableName] = tempTable
		}
	}
	tx.isActive = false // 事务不再活跃
	return nil
}

// Rollback 回滚事务，放弃所有更改
func (tx *Transaction) Rollback() error {
	if !tx.isActive || tx.isCommittedOrRolledBack {
		return errors.New("事务已提交或回滚，或不活跃")
	}

	// 延迟释放全局事务锁
	defer tx.db.txMu.Unlock()
	tx.isCommittedOrRolledBack = true // 标记事务已处理

	tx.tempTables = nil // 丢弃临时数据
	tx.isActive = false // 事务不再活跃
	return nil
}

// getTableInTx 获取事务中的表实例
func (tx *Transaction) getTableInTx(tableName string) (*Table, error) {
	table, exists := tx.tempTables[tableName]
	if !exists {
		return nil, fmt.Errorf("事务中表 '%s' 不存在", tableName)
	}
	return table, nil
}

// Insert 向事务中的表中插入一行数据
func (tx *Transaction) Insert(tableName string, data map[string]interface{}) error {
	if !tx.isActive {
		return errors.New("事务不活跃，无法插入")
	}

	table, err := tx.getTableInTx(tableName)
	if err != nil {
		return err
	}

	var primaryKeyVal string
	primaryKeyName := ""

	// 验证数据并查找主键
	rowData := make(map[string]interface{})
	for _, col := range table.Columns {
		val, ok := data[col.Name]
		if !ok {
			return fmt.Errorf("列 '%s' 缺失", col.Name)
		}
		// 简单的类型验证（这里可以扩展为更严格的类型转换和验证）
		switch col.Type {
		case "string":
			if _, isString := val.(string); !isString {
				return fmt.Errorf("列 '%s' 期望类型为 string, 实际为 %T", col.Name, val)
			}
		case "int":
			if fVal, isFloat := val.(float64); isFloat {
				val = int(fVal) // 尝试从float64转换
			} else if _, isInt := val.(int); !isInt {
				return fmt.Errorf("列 '%s' 期望类型为 int, 实际为 %T", col.Name, val)
			}
		case "float64":
			if _, isFloat := val.(float64); !isFloat {
				return fmt.Errorf("列 '%s' 期望类型为 float64, 实际为 %T", col.Name, val)
			}
		case "bool":
			if _, isBool := val.(bool); !isBool {
				return fmt.Errorf("列 '%s' 期望类型为 bool, 实际为 %T", col.Name, val)
			}
		case "time.Time":
			if _, isTime := val.(time.Time); !isTime {
				return fmt.Errorf("列 '%s' 期望类型为 time.Time, 实际为 %T", col.Name, val)
			}
		default:
			return fmt.Errorf("不支持的列类型 '%s' ", col.Type)
		}
		
		rowData[col.Name] = val

		if col.IsPrimaryKey {
			pk, ok := val.(string)
			if !ok {
				return errors.New("主键必须是字符串类型") // 简化处理，主键强制为string
			}
			primaryKeyVal = pk
			primaryKeyName = col.Name
		}
	}

	if primaryKeyVal == "" {
		return errors.New("未找到主键值")
	}

	if _, exists := table.Rows[primaryKeyVal]; exists {
		return fmt.Errorf("主键 '%s' 的值 '%s' 已经存在", primaryKeyName, primaryKeyVal)
	}

	table.Rows[primaryKeyVal] = rowData
	return nil
}

// Select 根据主键查询事务中的一行数据
func (tx *Transaction) Select(tableName string, primaryKey string) (map[string]interface{}, error) {
	if !tx.isActive {
		return nil, errors.New("事务不活跃，无法查询")
	}

	table, err := tx.getTableInTx(tableName)
	if err != nil {
		return nil, err
	}

	row, exists := table.Rows[primaryKey]
	if !exists {
		return nil, fmt.Errorf("未找到主键为 '%s' 的数据", primaryKey)
	}
	// 返回一个副本以防止外部修改
	copyRow := make(map[string]interface{})
	for k, v := range row {
		copyRow[k] = v
	}
	return copyRow, nil
}

// SelectAll 查询事务中表的所有数据
func (tx *Transaction) SelectAll(tableName string) ([]map[string]interface{}, error) {
	if !tx.isActive {
		return nil, errors.New("事务不活跃，无法查询所有数据")
	}

	table, err := tx.getTableInTx(tableName)
	if err != nil {
		return nil, err
	}

	rows := make([]map[string]interface{}, 0, len(table.Rows))
	for _, row := range table.Rows {
		// 返回副本
		copyRow := make(map[string]interface{})
		for k, v := range row {
			copyRow[k] = v
		}
		rows = append(rows, copyRow)
	}
	return rows, nil
}

// Update 更新事务中表中一行数据
func (tx *Transaction) Update(tableName string, primaryKey string, newData map[string]interface{}) error {
	if !tx.isActive {
		return errors.New("事务不活跃，无法更新")
	}

	table, err := tx.getTableInTx(tableName)
	if err != nil {
		return err
	}

	existingRow, exists := table.Rows[primaryKey]
	if !exists {
		return fmt.Errorf("未找到主键为 '%s' 的数据进行更新", primaryKey)
	}

	for _, col := range table.Columns {
		if val, ok := newData[col.Name]; ok {
			// 主键不允许更新
			if col.IsPrimaryKey {
				return fmt.Errorf("不允许更新主键列 '%s'", col.Name)
			}
			// 简单的类型验证
			switch col.Type {
			case "string":
				if _, isString := val.(string); !isString {
					return fmt.Errorf("更新失败：列 '%s' 期望类型为 string, 实际为 %T", col.Name, val)
				}
			case "int":
				if fVal, isFloat := val.(float64); isFloat {
					val = int(fVal)
				} else if _, isInt := val.(int); !isInt {
					return fmt.Errorf("更新失败：列 '%s' 期望类型为 int, 实际为 %T", col.Name, val)
				}
			case "float64":
				if _, isFloat := val.(float64); !isFloat {
					return fmt.Errorf("更新失败：列 '%s' 期望类型为 float64, 实际为 %T", col.Name, val)
				}
			case "bool":
				if _, isBool := val.(bool); !isBool {
					return fmt.Errorf("更新失败：列 '%s' 期望类型为 bool, 实际为 %T", col.Name, val)
				}
			case "time.Time":
				if _, isTime := val.(time.Time); !isTime {
					return fmt.Errorf("更新失败：列 '%s' 期望类型为 time.Time, 实际为 %T", col.Name, val)
				}
			default:
				return fmt.Errorf("不支持的列类型 '%s' ", col.Type)
			}
			existingRow[col.Name] = val
		}
	}
	return nil
}

// Delete 删除事务中表中一行数据
func (tx *Transaction) Delete(tableName string, primaryKey string) error {
	if !tx.isActive {
		return errors.New("事务不活跃，无法删除")
	}

	table, err := tx.getTableInTx(tableName)
	if err != nil {
		return err
	}

	if _, exists := table.Rows[primaryKey]; !exists {
		return fmt.Errorf("未找到主键为 '%s' 的数据进行删除", primaryKey)
	}
	delete(table.Rows, primaryKey)
	return nil
}

/*
// 示例用法 (可以添加到 main 函数中进行测试)
func main() {
	db := NewDatabaseV3()

	// 定义用户表的结构
	userColumns := []Column{
		{Name: "id", Type: "string", IsPrimaryKey: true},
		{Name: "name", Type: "string"},
		{Name: "age", Type: "int"},
		{Name: "email", Type: "string"},
		{Name: "is_active", Type: "bool"},
		{Name: "created_at", Type: "time.Time"},
	}

	// 创建用户表 (非事务性操作)
	err := db.CreateTable("users", userColumns)
	if err != nil {
		fmt.Println("创建表失败:", err)
		return
	}
	fmt.Println("表 'users' 创建成功。")

	// --- 事务示例 1: 成功提交 ---
	fmt.Println("\n--- 事务示例 1: 成功提交 ---")
	tx1, err := db.Begin()
	if err != nil {
		fmt.Println("开始事务失败:", err)
		return
	}

	// 在事务中插入数据
	err = tx1.Insert("users", map[string]interface{}{
		"id": "user1",
		"name": "Alice",
		"age": 30,
		"email": "alice@example.com",
		"is_active": true,
		"created_at": time.Now(),
	})
	if err != nil {
		fmt.Println("事务1插入数据失败:", err)
		tx1.Rollback() // 插入失败，回滚
		return
	} else {
		fmt.Println("事务1: 数据 'user1' 插入成功。")
	}

	// 在事务中查询数据 (只会看到事务内部的更改)
	user1InTx, _ := tx1.Select("users", "user1")
	fmt.Println("事务1内部查询 user1:", user1InTx)

	// 提交事务
	err = tx1.Commit()
	if err != nil {
		fmt.Println("提交事务1失败:", err)
	} else {
		fmt.Println("事务1提交成功。")
	}

	// 提交后，查询主数据库 (应该能看到 user1)
	mainTable, _ := db.GetTable("users")
	user1InMain, _ := mainTable.Select("user1") // 注意: Select方法已不在Table上，这里需要修改
	fmt.Println("主数据库查询 user1 (提交后):", user1InMain)


	// --- 事务示例 2: 回滚 ---
	fmt.Println("\n--- 事务示例 2: 回滚 ---")
	tx2, err := db.Begin()
	if err != nil {
		fmt.Println("开始事务失败:", err)
		return
	}

	// 在事务中插入数据 (user2)
	err = tx2.Insert("users", map[string]interface{}{
		"id": "user2",
		"name": "Bob",
		"age": 25,
		"email": "bob@example.com",
		"is_active": false,
		"created_at": time.Now(),
	})
	if err != nil {
		fmt.Println("事务2插入数据失败:", err)
		tx2.Rollback()
		return
	} else {
		fmt.Println("事务2: 数据 'user2' 插入成功。")
	}

	// 在事务中更新 user1
	err = tx2.Update("users", "user1", map[string]interface{}{
		"age": 31,
	})
	if err != nil {
		fmt.Println("事务2更新 user1 失败:", err)
		tx2.Rollback()
		return
	} else {
		fmt.Println("事务2: user1 更新成功。")
	}

	// 回滚事务
	err = tx2.Rollback()
	if err != nil {
		fmt.Println("回滚事务2失败:", err)
	} else {
		fmt.Println("事务2回滚成功。")
	}

	// 回滚后，查询主数据库 (user2不应该存在，user1应该保持原样)
	allUsersAfterRollback, _ := mainTable.SelectAll() // 注意: SelectAll方法已不在Table上
	fmt.Println("主数据库所有用户 (回滚后):", allUsersAfterRollback)

	// 确认 user2 不存在
	_, existsUser2 := mainTable.Select("user2") // 注意: Select方法已不在Table上
	if existsUser2 != nil {
		fmt.Println("主数据库中 user2 不存在 (符合预期)。")
	} else {
		fmt.Println("主数据库中 user2 仍然存在 (不符合预期)。")
	}

	// 确认 user1 未被事务2的更新影响
	user1AfterRollback, _ := mainTable.Select("user1") // 注意: Select方法已不在Table上
	fmt.Printf("主数据库中 user1 的年龄: %v (应为30)。\n", user1AfterRollback["age"])


	// --- 事务示例 3: 冲突（通过全局锁避免） ---
	// 这里由于全局事务锁，不会发生真正的并发冲突，而是会阻塞
	// tx3, err := db.Begin() // 会阻塞直到 tx2 完成
	// fmt.Println("事务3开始。")
	// tx3.Rollback()
}
*/
```

#### 实现逻辑

在之前的内存数据库基础上，我们通过引入事务机制，实现ACID特性中的原子性、一致性和隔离性。持久性对于内存数据库来说，意味着提交后数据在内存中稳定存在。

**核心思想：**

*   **全局事务锁 (`txMu`)**: 确保在任何给定时间只有一个事务能够修改数据库状态（实现简单串行化隔离）。
*   **事务沙盒 (`tempTables`)**: 每个事务在开始时会创建一个其将要操作的表的深拷贝，所有修改都在这个沙盒中进行。
*   **提交 (`Commit`)**: 只有当事务成功提交时，沙盒中的更改才会被原子性地应用到主数据库。
*   **回滚 (`Rollback`)**: 如果事务失败或被取消，沙盒将被丢弃，主数据库保持不变。

这里简单实现事务，事务就是有日志或临时文件来保存一定版本数据，提交后，更新到真实表中，mysql有mvcc实现，每个事务有对应版本日志


## 总结

用 Go 语言实现一个简易数据库，我们逐步构建了其核心功能，从一个简单的内存键值存储演进到支持表结构和基本事务处理的系统。以下是各版本的主要核心逻辑点及未来向生产级数据库扩展的思考。

### 核心逻辑点

1.  **数据库 1.0：内存键值存储与并发安全**
    *   **数据存储**：核心是 Go 语言的 `map[string]Item`，提供 O(1) 的平均时间复杂度进行数据的插入、查询、更新和删除，实现了快速的内存操作。
    *   **并发控制**：通过 `sync.RWMutex` 读写锁机制，保证了在多协程并发访问 `map` 时的线程安全，避免了数据竞争问题。
    *   **HTTP 接口**：对外提供了 RESTful API 接口，使得数据库功能可以通过网络进行访问和操作，简化了客户端集成。

2.  **数据库 2.0：表结构定义与数据校验**
    *   **表结构定义**：引入 `Column` 结构体来定义表的列名、数据类型和是否为主键，以及 `Table` 结构体来管理表的元数据和实际行数据 (`map[string]map[string]interface{}`)。
    *   **主键约束**：强制要求每个表必须且只能有一个主键，并通过主键来唯一标识和操作每一行数据。
    *   **数据类型校验**：在数据插入和更新时，增加了对输入数据与表结构中定义列类型是否匹配的初步校验，增强了数据一致性。

3.  **数据库 3.0：基于深拷贝的事务实现**
    *   **ACID 特性初步实现**：主要聚焦于原子性 (Atomicity)、一致性 (Consistency) 和隔离性 (Isolation)。持久性 (Durability) 在内存数据库中意味着提交后数据在程序运行时稳定存在。
    *   **全局事务锁 (`sync.Mutex`)**：通过 `txMu` 确保在任何给定时间只有一个事务能够修改数据库状态。这实现了一种最简单的隔离级别——串行化 (Serializable)，避免了并发冲突。
    *   **事务沙盒 (`tempTables`)**：这是事务隔离的核心。当一个事务开始时 (`Begin()`)，它会对其将要操作的所有表进行深拷贝，创建一个独立的私有工作空间。所有在事务中的 CRUD 操作都在这个沙盒上进行，不会影响主数据库的实际数据。
    *   **原子提交 (`Commit()`)**：当事务成功提交时，沙盒中的所有修改会作为一个整体原子性地应用（替换）到主数据库中对应的表数据。如果在此过程中发生任何错误，则整个事务不会被提交，保证了原子性。
    *   **原子回滚 (`Rollback()`)**：如果事务失败或被显式回滚，事务沙盒将被直接丢弃，主数据库的数据保持原样，未受影响，保证了原子性和一致性。

### 扩充到 MySQL 类似设计

将当前的简易内存数据库扩展到类似 MySQL 这样的关系型数据库，需要引入更复杂的机制来处理持久化、并发、查询优化和数据管理等问题：

1.  **持久化层 (Durability & Storage)**
    *   **磁盘存储**：数据不再仅存于内存，需要设计高效的磁盘存储结构（如 B+树文件组织），将数据和索引页持久化到文件中。
    *   **WAL (Write-Ahead Logging)**：实现预写日志。所有数据修改操作在写入磁盘前，必须先将变更记录写入到持久化的日志文件中。这确保了崩溃恢复时的原子性和持久性。
    *   **Checkpointing/Fuzzy Checkpointing**：定期将内存中的"脏页"（已修改但尚未写入磁盘的数据页）刷回磁盘，以减少崩溃恢复所需的时间和数据丢失的风险。

2.  **索引系统 (Indexing)**
    *   **B+树索引**：实现高效的主键索引和二级索引。B+树结构能有效支持范围查询和等值查询。
    *   **索引管理**：包括索引的创建、重建、删除，以及在数据插入、更新、删除时自动维护索引的机制。

3.  **查询处理与优化 (Query Processing & Optimization)**
    *   **SQL 解析器**：能够解析完整的 SQL 语句（包括 SELECT, INSERT, UPDATE, DELETE, CREATE TABLE, JOIN 等）。
    *   **查询优化器**：接收解析后的查询树，生成最优的执行计划。这包括选择最佳的索引、决定表的连接顺序、优化条件过滤等，以最小化 I/O 和 CPU 消耗。
    *   **执行引擎**：按照查询优化器生成的执行计划来执行实际的数据操作。

4.  **更健壮的并发控制 (Concurrency Control)**
    *   **MVCC (Multi-Version Concurrency Control)**：这是现代关系型数据库实现高并发的关键。它允许多个事务并发读写，每个事务看到数据的一个特定版本，从而减少锁竞争。通过为每行数据维护多个版本，并结合事务 ID 和回滚段实现。
    *   **锁管理器**：实现细粒度的锁（如行级锁）来处理写-写冲突，并在 MVCC 无法解决的场景下提供必要的隔离。
    *   **隔离级别**：支持如"读已提交 (Read Committed)"、"可重复读 (Repeatable Read)"等不同隔离级别，以在并发性和数据一致性之间进行权衡。

5.  **崩溃恢复机制 (Recovery)**
    *   利用 WAL 和 Checkpointing 实现事务的原子性和持久性。在数据库启动时，通过重放 WAL 日志和检查点状态来恢复到崩溃前的最新一致状态。

6.  **存储引擎架构 (Storage Engine Architecture)**
    *   将查询执行逻辑与底层数据存储和访问逻辑解耦。例如 MySQL 的 InnoDB 存储引擎。
    *   存储引擎负责数据和索引的物理存储、读写、事务隔离、崩溃恢复等底层细节。

7.  **高可用与复制 (High Availability & Replication)**
    *   主从复制、多主复制等机制，用于数据冗余、读写分离和灾难恢复。

8.  **网络与客户端连接**
    *   实现完整的网络协议栈，支持多种客户端连接方式，如 TCP/IP。
    *   连接池管理、认证和授权机制。

将一个简易的内存数据库扩展到生产级水平是一个庞大而复杂的工程，需要深入理解数据库原理的各个方面。
