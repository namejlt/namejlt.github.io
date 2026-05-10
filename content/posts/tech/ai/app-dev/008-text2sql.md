---
title: "AI 应用开发-008 Text2SQL自然语言查询"
date: 2025-05-30T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型", "Text2SQL", "自然语言查询"]
---

## 概述

Text2SQL（文本转SQL）技术让用户通过自然语言直接查询数据库，无需编写SQL语句。这是AI应用开发中最具商业价值的场景之一，能极大降低数据查询门槛，让业务人员自主获取数据洞察。

本章将系统讲解Text2SQL的原理、实现方案、优化策略，并通过完整项目实现一个支持自然语言查询的交互式BI系统。

## 一、Text2SQL技术原理

### 1.1 核心流程

```
自然语言问题 → Schema理解 → SQL生成 → 执行查询 → 结果解读 → 自然语言回答
```

### 1.2 关键挑战

| 挑战 | 描述 | 解决方案 |
|------|------|----------|
| Schema理解 | 模型需要理解表结构和字段含义 | Schema Prompt + 示例 |
| 复杂查询 | 多表关联、嵌套子查询 | 分步生成 + 验证 |
| 方言差异 | 不同数据库SQL语法不同 | 指定方言 + 后处理 |
| 幻觉风险 | 生成不存在的表或字段 | Schema约束 + 校验 |
| 安全风险 | SQL注入、危险操作 | 白名单 + 权限控制 |

## 二、基础Text2SQL实现

### 2.1 基于Prompt的SQL生成

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


DATABASE_SCHEMA = """
数据库表结构：

1. employees（员工表）
   - id INTEGER PRIMARY KEY
   - name VARCHAR 员工姓名
   - department VARCHAR 部门名称
   - position VARCHAR 职位
   - salary DECIMAL 薪资
   - hire_date DATE 入职日期
   - age INTEGER 年龄

2. departments（部门表）
   - id INTEGER PRIMARY KEY
   - name VARCHAR 部门名称
   - manager_id INTEGER 部门经理ID
   - budget DECIMAL 部门预算

3. projects（项目表）
   - id INTEGER PRIMARY KEY
   - name VARCHAR 项目名称
   - department_id INTEGER 所属部门ID
   - start_date DATE 开始日期
   - end_date DATE 结束日期
   - status VARCHAR 项目状态

4. employee_projects（员工项目关联表）
   - employee_id INTEGER 员工ID
   - project_id INTEGER 项目ID
   - role VARCHAR 角色
"""


class Text2SQLClient:
    def __init__(self, db_path: str = "company.db"):
        self.db_path = db_path
        self.schema = DATABASE_SCHEMA

    def generate_sql(self, question: str) -> str:
        prompt = f"""你是一个SQL专家。请根据数据库表结构，将自然语言问题转换为SQL查询。

数据库表结构：
{self.schema}

规则：
1. 只生成SQLite兼容的SQL
2. 只使用上述表中存在的表和字段
3. 使用中文注释说明关键逻辑
4. 对于聚合查询，使用有意义的别名
5. 只输出SQL语句，不要输出其他内容

自然语言问题：{question}

SQL："""

        try:
            response = dashscope.Generation.call(
                model="qwen-plus",
                messages=[{"role": "user", "content": prompt}],
                result_format="message"
            )
            if response.status_code == 200:
                sql = response.output.choices[0].message.content.strip()
                sql = self._clean_sql(sql)
                logger.info(f"生成SQL: {sql}")
                return sql
            return ""
        except Exception as e:
            logger.error(f"SQL生成失败: {e}")
            return ""

    def execute_sql(self, sql: str) -> Optional[list[dict]]:
        dangerous_keywords = ["DROP", "DELETE", "UPDATE", "INSERT", "ALTER", "CREATE", "TRUNCATE"]
        sql_upper = sql.upper()
        for keyword in dangerous_keywords:
            if keyword in sql_upper:
                logger.warning(f"检测到危险操作: {keyword}")
                return None

        try:
            conn = sqlite3.connect(self.db_path)
            conn.row_factory = sqlite3.Row
            cursor = conn.cursor()
            cursor.execute(sql)
            rows = cursor.fetchall()
            results = [dict(row) for row in rows]
            conn.close()
            logger.info(f"查询返回 {len(results)} 行")
            return results
        except sqlite3.Error as e:
            logger.error(f"SQL执行失败: {e}")
            return None

    def ask(self, question: str) -> dict:
        sql = self.generate_sql(question)
        if not sql:
            return {"question": question, "sql": "", "results": [], "answer": "无法生成SQL查询"}

        results = self.execute_sql(sql)
        if results is None:
            return {"question": question, "sql": sql, "results": [], "answer": "SQL执行失败或包含危险操作"}

        answer = self._generate_answer(question, sql, results)
        return {"question": question, "sql": sql, "results": results, "answer": answer}

    def _generate_answer(self, question: str, sql: str, results: list[dict]) -> str:
        if not results:
            return "查询结果为空。"

        prompt = f"""基于以下查询结果，用自然语言回答用户问题。

用户问题：{question}
SQL查询：{sql}
查询结果：{json.dumps(results[:20], ensure_ascii=False, default=str)}

请用简洁的中文回答："""

        try:
            response = dashscope.Generation.call(
                model="qwen-turbo",
                messages=[{"role": "user", "content": prompt}],
                result_format="message"
            )
            if response.status_code == 200:
                return response.output.choices[0].message.content
        except Exception as e:
            logger.error(f"答案生成失败: {e}")

        return f"查询返回 {len(results)} 条记录。"

    def _clean_sql(self, sql: str) -> str:
        sql = sql.strip()
        if sql.startswith("```sql"):
            sql = sql[6:]
        if sql.startswith("```"):
            sql = sql[3:]
        if sql.endswith("```"):
            sql = sql[:-3]
        return sql.strip()


def init_sample_db(db_path: str = "company.db"):
    conn = sqlite3.connect(db_path)
    cursor = conn.cursor()

    cursor.execute("""CREATE TABLE IF NOT EXISTS departments (
        id INTEGER PRIMARY KEY, name VARCHAR, manager_id INTEGER, budget DECIMAL)""")
    cursor.execute("""CREATE TABLE IF NOT EXISTS employees (
        id INTEGER PRIMARY KEY, name VARCHAR, department VARCHAR,
        position VARCHAR, salary DECIMAL, hire_date DATE, age INTEGER)""")
    cursor.execute("""CREATE TABLE IF NOT EXISTS projects (
        id INTEGER PRIMARY KEY, name VARCHAR, department_id INTEGER,
        start_date DATE, end_date DATE, status VARCHAR)""")
    cursor.execute("""CREATE TABLE IF NOT EXISTS employee_projects (
        employee_id INTEGER, project_id INTEGER, role VARCHAR)""")

    cursor.executemany("INSERT OR IGNORE INTO departments VALUES (?,?,?,?)", [
        (1, "技术部", 1, 5000000), (2, "市场部", 4, 3000000),
        (3, "财务部", 7, 2000000), (4, "人事部", 10, 1500000)])

    cursor.executemany("INSERT OR IGNORE INTO employees VALUES (?,?,?,?,?,?,?)", [
        (1, "张三", "技术部", "高级工程师", 35000, "2020-03-15", 32),
        (2, "李四", "技术部", "工程师", 25000, "2021-07-01", 28),
        (3, "王五", "技术部", "架构师", 50000, "2019-01-10", 38),
        (4, "赵六", "市场部", "市场经理", 30000, "2020-06-20", 35),
        (5, "钱七", "市场部", "市场专员", 18000, "2022-03-01", 26),
        (6, "孙八", "技术部", "工程师", 28000, "2021-09-15", 30),
        (7, "周九", "财务部", "财务经理", 32000, "2019-11-01", 36),
        (8, "吴十", "财务部", "会计", 20000, "2022-01-15", 29),
        (9, "郑十一", "人事部", "HR", 22000, "2021-05-10", 31),
        (10, "王十二", "人事部", "HR经理", 28000, "2020-02-01", 34)])

    cursor.executemany("INSERT OR IGNORE INTO projects VALUES (?,?,?,?,?,?)", [
        (1, "AI平台开发", 1, "2024-01-01", "2024-12-31", "进行中"),
        (2, "品牌升级", 2, "2024-03-01", "2024-09-30", "已完成"),
        (3, "财务系统改造", 3, "2024-06-01", "2025-03-31", "进行中")])

    cursor.executemany("INSERT OR IGNORE INTO employee_projects VALUES (?,?,?)", [
        (1, 1, "开发负责人"), (3, 1, "架构师"), (6, 1, "开发工程师"),
        (4, 2, "项目负责人"), (5, 2, "执行"), (7, 3, "项目负责人"), (8, 3, "开发")])

    conn.commit()
    conn.close()
    logger.info("示例数据库初始化完成")


if __name__ == "__main__":
    init_sample_db()

    client = Text2SQLClient()

    questions = [
        "技术部有多少员工？",
        "各部门平均薪资是多少？",
        "薪资最高的3名员工是谁？",
        "哪些员工参与了AI平台开发项目？",
        "每个部门有多少项目在进行中？"
    ]

    for q in questions:
        result = client.ask(q)
        print(f"\n问题: {result['question']}")
        print(f"SQL: {result['sql']}")
        print(f"回答: {result['answer']}")
```

## 三、Text2SQL优化策略

### 3.1 Few-Shot示例增强

```python
FEW_SHOT_EXAMPLES = """
示例1：
问题：技术部有多少员工？
SQL：SELECT COUNT(*) AS 员工数 FROM employees WHERE department = '技术部'

示例2：
问题：各部门平均薪资是多少？
SQL：SELECT department AS 部门, AVG(salary) AS 平均薪资 FROM employees GROUP BY department

示例3：
问题：薪资最高的3名员工是谁？
SQL：SELECT name AS 姓名, salary AS 薪资 FROM employees ORDER BY salary DESC LIMIT 3

示例4：
问题：哪些员工参与了AI平台开发项目？
SQL：SELECT e.name AS 姓名, ep.role AS 角色 FROM employees e
     JOIN employee_projects ep ON e.id = ep.employee_id
     JOIN projects p ON ep.project_id = p.id
     WHERE p.name = 'AI平台开发'
"""


def generate_sql_with_fewshot(question: str, schema: str) -> str:
    prompt = f"""你是一个SQL专家。请根据数据库表结构和示例，将自然语言问题转换为SQL。

数据库表结构：
{schema}

示例：
{FEW_SHOT_EXAMPLES}

规则：
1. 只生成SQLite兼容的SQL
2. 参考示例的SQL风格和写法
3. 只输出SQL语句

问题：{question}

SQL："""

    response = dashscope.Generation.call(
        model="qwen-plus",
        messages=[{"role": "user", "content": prompt}],
        result_format="message"
    )
    if response.status_code == 200:
        return response.output.choices[0].message.content.strip()
    return ""
```

### 3.2 Schema链接与校验

```python
class SchemaAwareText2SQL:
    def __init__(self, db_path: str):
        self.db_path = db_path
        self.schema_info = self._extract_schema()

    def _extract_schema(self) -> dict:
        conn = sqlite3.connect(self.db_path)
        cursor = conn.cursor()

        cursor.execute("SELECT name FROM sqlite_master WHERE type='table'")
        tables = [row[0] for row in cursor.fetchall()]

        schema = {}
        for table in tables:
            cursor.execute(f"PRAGMA table_info({table})")
            columns = cursor.fetchall()
            schema[table] = [
                {"name": col[1], "type": col[2], "notnull": bool(col[3])}
                for col in columns
            ]

        conn.close()
        return schema

    def validate_sql(self, sql: str) -> tuple[bool, str]:
        sql_lower = sql.lower()
        for table in self.schema_info:
            if table.lower() in sql_lower:
                for col in self.schema_info[table]:
                    if col["name"].lower() in sql_lower:
                        return True, "校验通过"

        for table in self.schema_info:
            if table.lower() not in sql_lower:
                continue
            for col in self.schema_info[table]:
                if col["name"] in sql:
                    return True, "校验通过"

        return False, "SQL中引用了不存在的表或字段"

    def generate_with_validation(self, question: str, max_retries: int = 2) -> str:
        for attempt in range(max_retries + 1):
            sql = self._generate_sql(question)
            is_valid, msg = self.validate_sql(sql)
            if is_valid:
                return sql
            logger.warning(f"SQL校验失败(第{attempt + 1}次): {msg}")
        return sql
```

### 3.3 查询结果可视化

```python
import plotly.express as px
import pandas as pd


def visualize_results(results: list[dict], question: str) -> Optional[str]:
    if not results:
        return None

    df = pd.DataFrame(results)

    if len(df.columns) == 2:
        numeric_cols = df.select_dtypes(include="number").columns
        if len(numeric_cols) == 1:
            if len(df) <= 10:
                fig = px.bar(df, x=df.columns[0], y=numeric_cols[0], title=question)
            else:
                fig = px.line(df, x=df.columns[0], y=numeric_cols[0], title=question)
            output_path = f"output/chart_{hash(question) % 10000}.html"
            fig.write_html(output_path)
            return output_path

    return None
```

## 四、安全与权限控制

### 4.1 SQL安全防护

```python
class SafeText2SQL:
    DANGEROUS_PATTERNS = [
        "DROP TABLE", "DELETE FROM", "UPDATE ", "INSERT INTO",
        "ALTER TABLE", "CREATE TABLE", "TRUNCATE",
        "INTO OUTFILE", "LOAD_FILE", "UNION SELECT",
        "INFORMATION_SCHEMA", "SLEEP(", "BENCHMARK("
    ]

    READONLY_KEYWORDS = ["SELECT", "WITH", "EXPLAIN"]

    def __init__(self, db_path: str, max_rows: int = 1000):
        self.db_path = db_path
        self.max_rows = max_rows

    def check_safety(self, sql: str) -> tuple[bool, str]:
        sql_upper = sql.upper().strip()

        for pattern in self.DANGEROUS_PATTERNS:
            if pattern.upper() in sql_upper:
                return False, f"检测到危险操作: {pattern}"

        first_keyword = sql_upper.split()[0] if sql_upper.split() else ""
        if first_keyword not in self.READONLY_KEYWORDS:
            return False, f"只允许SELECT查询，当前操作: {first_keyword}"

        return True, "安全检查通过"

    def execute_safe(self, sql: str) -> Optional[list[dict]]:
        is_safe, msg = self.check_safety(sql)
        if not is_safe:
            logger.warning(f"安全检查未通过: {msg}")
            return None

        safe_sql = sql
        if "LIMIT" not in sql.upper():
            safe_sql = sql.rstrip(";") + f" LIMIT {self.max_rows}"

        try:
            conn = sqlite3.connect(self.db_path)
            conn.row_factory = sqlite3.Row
            cursor = conn.cursor()
            cursor.execute(safe_sql)
            rows = cursor.fetchall()
            results = [dict(row) for row in rows]
            conn.close()
            return results
        except sqlite3.Error as e:
            logger.error(f"查询执行失败: {e}")
            return None
```

## 五、Text2SQL最佳实践

### 5.1 效果优化清单

| 优化项 | 方法 | 效果 |
|--------|------|------|
| Schema描述 | 添加字段中文注释和示例值 | 提升字段匹配准确率 |
| Few-Shot | 提供3-5个典型查询示例 | 提升SQL语法正确率 |
| 分步生成 | 先选表再生成SQL | 降低复杂查询错误率 |
| 结果校验 | 执行后检查列名和类型 | 避免运行时错误 |
| 缓存 | 相同问题缓存SQL | 提升响应速度 |

### 5.2 生产环境建议

1. **只读权限**：Text2SQL连接的数据库账号只授予SELECT权限
2. **查询超时**：设置SQL执行超时时间（如30秒）
3. **结果限制**：强制添加LIMIT，防止返回过多数据
4. **审计日志**：记录所有自然语言查询和生成的SQL
5. **人工审核**：高风险查询需人工确认后执行

## 总结

本章系统讲解了Text2SQL技术的原理与实践：

1. **核心原理**：自然语言 → Schema理解 → SQL生成 → 执行 → 结果解读
2. **基础实现**：基于Prompt的SQL生成，包含安全检查和结果解读
3. **优化策略**：Few-Shot示例、Schema链接校验、结果可视化
4. **安全防护**：SQL注入防护、只读权限、查询限制
5. **最佳实践**：从Schema描述到生产环境部署的完整优化清单

Text2SQL是AI应用中最直接产生业务价值的技术之一，关键在于Schema理解准确性和SQL生成安全性。
