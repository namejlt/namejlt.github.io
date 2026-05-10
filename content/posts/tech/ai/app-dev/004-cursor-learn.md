---
title: "AI 应用开发-004 Cursor数据可视化与洞察实战"
date: 2025-05-26T09:00:00+08:00
draft: false
toc: true
categories: ["技术/AI/应用开发"]
tags: ["AI", "应用开发", "大模型", "cursor", "数据可视化"]
---

## 概述

在掌握了Cursor的基础用法和规则体系之后，本章节将深入实战层面，通过数据可视化与数据洞察两个核心场景，展示如何用Cursor高效完成从数据处理到可视化呈现的全流程开发。数据可视化是AI应用开发中连接"数据"与"决策"的关键桥梁，而Cursor的AI辅助能力可以让这一过程效率提升数倍。

本章将覆盖三大实战场景：多Excel报表自动化处理、疫情实时监控大屏搭建、以及DeepSeek本地私有化部署与Prompt工程结合。每个场景都包含完整的代码实现和Cursor协作技巧。

## 一、多Excel报表自动化处理

### 1.1 场景描述

在企业日常运营中，数据分析师经常需要处理多张Excel报表，进行数据清洗、汇总、对比分析。传统方式需要手动操作Excel，耗时且易错。借助Cursor，可以用自然语言描述需求，快速生成Python数据处理脚本。

### 1.2 技术选型

- **pandas**：数据清洗与转换
- **openpyxl**：Excel文件读写
- **matplotlib / plotly**：数据可视化
- **Cursor**：AI辅助编码

### 1.3 实战步骤

#### 步骤一：项目初始化与规则设定

在`.cursor/rules/rules-main.mdc`中添加数据处理规范：

```markdown
# 数据处理项目规范
- 使用Python 3.10+
- 数据处理统一使用pandas
- Excel读写使用openpyxl引擎
- 可视化优先使用plotly（交互式）
- 所有函数必须包含类型注解
- 文件路径使用pathlib.Path
- 日志使用logging模块
```

#### 步骤二：多Excel合并与清洗

在Cursor中输入Prompt：

```plaintext
请用Python实现以下功能：
1. 读取data目录下所有xlsx文件（约10张销售报表）
2. 每张报表格式相同：日期、产品名、销售额、区域
3. 合并所有报表到一个DataFrame
4. 清洗：去除空行、统一日期格式、处理异常值（负数销售额置为0）
5. 按区域和月份汇总，输出汇总表
6. 生成月度趋势图
```

Cursor生成的核心代码：

```python
import pandas as pd
from pathlib import Path
import logging
import plotly.express as px

logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)


def load_and_merge_excel(data_dir: str = "data") -> pd.DataFrame:
    data_path = Path(data_dir)
    if not data_path.exists():
        raise FileNotFoundError(f"数据目录不存在: {data_dir}")

    dfs = []
    for file in data_path.glob("*.xlsx"):
        try:
            df = pd.read_excel(file, engine="openpyxl")
            df["source_file"] = file.name
            dfs.append(df)
            logger.info(f"成功读取: {file.name}, 行数: {len(df)}")
        except Exception as e:
            logger.error(f"读取失败: {file.name}, 错误: {e}")

    if not dfs:
        raise ValueError("未找到有效的Excel文件")

    merged = pd.concat(dfs, ignore_index=True)
    logger.info(f"合并完成，总行数: {len(merged)}")
    return merged


def clean_data(df: pd.DataFrame) -> pd.DataFrame:
    df = df.dropna(subset=["日期", "产品名", "销售额"])

    df["日期"] = pd.to_datetime(df["日期"], errors="coerce")
    df = df.dropna(subset=["日期"])

    df["销售额"] = pd.to_numeric(df["销售额"], errors="coerce").fillna(0)
    df.loc[df["销售额"] < 0, "销售额"] = 0

    df["月份"] = df["日期"].dt.to_period("M").astype(str)
    logger.info(f"清洗完成，剩余行数: {len(df)}")
    return df


def summarize_by_region_month(df: pd.DataFrame) -> pd.DataFrame:
    summary = df.groupby(["区域", "月份"]).agg(
        总销售额=("销售额", "sum"),
        订单数=("销售额", "count"),
        平均销售额=("销售额", "mean")
    ).reset_index()
    return summary


def plot_monthly_trend(df: pd.DataFrame, output_path: str = "output/monthly_trend.html"):
    monthly = df.groupby("月份").agg(总销售额=("销售额", "sum")).reset_index()
    fig = px.line(monthly, x="月份", y="总销售额", title="月度销售趋势")
    fig.update_layout(xaxis_title="月份", yaxis_title="销售额（元）")
    Path(output_path).parent.mkdir(parents=True, exist_ok=True)
    fig.write_html(output_path)
    logger.info(f"趋势图已保存: {output_path}")


if __name__ == "__main__":
    merged_df = load_and_merge_excel("data")
    cleaned_df = clean_data(merged_df)
    summary_df = summarize_by_region_month(cleaned_df)
    summary_df.to_excel("output/summary.xlsx", index=False)
    plot_monthly_trend(cleaned_df)
```

#### 步骤三：Cursor协作技巧

- 使用`@file`指令指定已有的数据文件，让Cursor理解数据结构
- 逐步迭代：先让Cursor生成基础合并逻辑，再追加清洗和可视化
- 遇到pandas警告时，直接粘贴警告信息让Cursor修复

## 二、疫情实时监控大屏

### 2.1 场景描述

搭建一个疫情数据实时监控大屏，展示全国各省市疫情数据、趋势曲线和区域热力图。这是数据可视化中典型的Dashboard场景，涉及数据获取、实时更新、多图表联动。

### 2.2 技术选型

- **Streamlit**：快速搭建数据大屏
- **pandas**：数据处理
- **plotly**：交互式图表
- **pyecharts**：地图热力图
- **Cursor**：AI辅助全流程开发

### 2.3 实战步骤

#### 步骤一：数据获取层

```python
import pandas as pd
import requests
import logging
from typing import Optional

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class EpidemicDataFetcher:
    def __init__(self, api_base: str = "https://api.example.com/epidemic"):
        self.api_base = api_base

    def fetch_province_data(self) -> Optional[pd.DataFrame]:
        try:
            response = requests.get(f"{self.api_base}/provinces", timeout=10)
            response.raise_for_status()
            data = response.json()
            df = pd.DataFrame(data["data"])
            logger.info(f"获取省份数据成功，共 {len(df)} 条")
            return df
        except requests.RequestException as e:
            logger.error(f"获取省份数据失败: {e}")
            return None

    def fetch_trend_data(self, province: str = "全国") -> Optional[pd.DataFrame]:
        try:
            response = requests.get(
                f"{self.api_base}/trend",
                params={"province": province},
                timeout=10
            )
            response.raise_for_status()
            data = response.json()
            df = pd.DataFrame(data["data"])
            df["date"] = pd.to_datetime(df["date"])
            return df
        except requests.RequestException as e:
            logger.error(f"获取趋势数据失败: {e}")
            return None
```

#### 步骤二：大屏展示层

```python
import streamlit as st
import plotly.express as px
import plotly.graph_objects as go
from fetcher import EpidemicDataFetcher

st.set_page_config(page_title="疫情监控大屏", layout="wide")

fetcher = EpidemicDataFetcher()

st.title("🦠 全国疫情实时监控大屏")

province_df = fetcher.fetch_province_data()
trend_df = fetcher.fetch_trend_data()

if province_df is not None:
    col1, col2, col3, col4 = st.columns(4)
    col1.metric("现存确诊", f"{province_df['current_confirmed'].sum():,}")
    col2.metric("累计确诊", f"{province_df['total_confirmed'].sum():,}")
    col3.metric("累计治愈", f"{province_df['cured'].sum():,}")
    col4.metric("累计死亡", f"{province_df['dead'].sum():,}")

    st.markdown("---")

    col_left, col_right = st.columns(2)

    with col_left:
        st.subheader("各省份确诊排行")
        top10 = province_df.nlargest(10, "current_confirmed")
        fig_bar = px.bar(top10, x="current_confirmed", y="province", orientation="h",
                         color="current_confirmed", color_continuous_scale="Reds")
        st.plotly_chart(fig_bar, use_container_width=True)

    with col_right:
        st.subheader("每日新增趋势")
        if trend_df is not None:
            fig_trend = go.Figure()
            fig_trend.add_trace(go.Scatter(x=trend_df["date"], y=trend_df["new_confirmed"],
                                           mode="lines+markers", name="新增确诊"))
            fig_trend.add_trace(go.Scatter(x=trend_df["date"], y=trend_df["new_cured"],
                                           mode="lines+markers", name="新增治愈"))
            st.plotly_chart(fig_trend, use_container_width=True)

    st.markdown("---")
    st.subheader("省份明细数据")
    st.dataframe(province_df, use_container_width=True)

st.caption("数据更新时间：每次刷新自动获取最新数据")
```

#### 步骤三：Cursor高效开发技巧

在开发大屏时，Cursor的协作模式推荐：

1. **先描述布局**：用自然语言描述大屏的整体布局（几行几列、每个区域放什么图表）
2. **逐区域生成**：让Cursor逐个区域生成图表代码，而非一次性生成整个页面
3. **样式微调**：对图表颜色、字体大小等细节，直接在代码中修改后让Cursor学习

```plaintext
// Cursor Prompt 示例
请用Streamlit实现一个疫情监控大屏，布局如下：
- 顶部：4个指标卡片（现存确诊、累计确诊、治愈、死亡）
- 中间左：各省份确诊排行横向柱状图
- 中间右：每日新增趋势折线图
- 底部：省份明细数据表格
要求：使用plotly绑定交互式图表，支持点击省份联动趋势图
```

## 三、DeepSeek本地私有化部署与Prompt工程

### 3.1 场景描述

在数据安全要求较高的场景下，企业需要将大模型部署在本地，实现私有化推理。DeepSeek提供了开源模型，可通过Ollama在本地快速部署，结合Prompt工程实现高质量的数据洞察。

### 3.2 Ollama本地部署

#### 安装Ollama

```bash
# macOS / Linux
curl -fsSL https://ollama.com/install.sh | sh

# 拉取DeepSeek模型
ollama pull deepseek-r1:7b

# 启动服务
ollama serve
```

#### Python调用本地模型

```python
import requests
import json
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

OLLAMA_API = "http://localhost:11434/api/chat"


def chat_with_deepseek(messages: list, model: str = "deepseek-r1:7b") -> str:
    payload = {
        "model": model,
        "messages": messages,
        "stream": False
    }

    try:
        response = requests.post(OLLAMA_API, json=payload, timeout=120)
        response.raise_for_status()
        result = response.json()
        content = result["message"]["content"]
        logger.info(f"模型响应长度: {len(content)}")
        return content
    except requests.RequestException as e:
        logger.error(f"调用本地模型失败: {e}")
        return ""


def analyze_sales_data(sales_summary: str) -> str:
    system_prompt = """你是一位资深数据分析师，擅长从销售数据中提取洞察。
请按照以下结构输出分析报告：
1. 整体趋势判断（上升/下降/平稳）
2. 关键发现（列出3-5个）
3. 异常点识别
4. 行动建议（具体可执行）
请用中文回答，语言简洁专业。"""

    messages = [
        {"role": "system", "content": system_prompt},
        {"role": "user", "content": f"请分析以下销售数据：\n{sales_summary}"}
    ]

    return chat_with_deepseek(messages)


if __name__ == "__main__":
    sample_data = """
    2025年Q1销售数据：
    - 1月：总销售额580万，同比增长12%
    - 2月：总销售额420万，同比下降8%（春节影响）
    - 3月：总销售额650万，同比增长18%
    - 华东区域占比45%，华南30%，华北15%，其他10%
    - 产品A销量暴增35%，产品B下降12%
    """

    result = analyze_sales_data(sample_data)
    print(result)
```

### 3.3 Prompt工程在数据洞察中的应用

#### CoT分步骤推理

```python
def deep_analysis_with_cot(data_description: str) -> str:
    prompt = f"""请对以下数据进行深度分析，按照思维链（Chain of Thought）逐步推理：

数据：{data_description}

步骤1：识别数据中的关键指标和趋势
步骤2：分析各指标之间的关联关系
步骤3：识别异常值和潜在风险
步骤4：基于以上分析，给出具体可执行的建议

请逐步展示你的推理过程。"""

    messages = [{"role": "user", "content": prompt}]
    return chat_with_deepseek(messages)
```

#### 角色扮演与JSON格式输出

```python
def structured_analysis(data_description: str) -> dict:
    prompt = f"""你是一位企业数据顾问。请分析以下数据，并严格按照JSON格式输出。

数据：{data_description}

输出格式要求：
{{
    "trend": "上升/下降/平稳",
    "confidence": 0.0-1.0,
    "key_findings": ["发现1", "发现2", "发现3"],
    "risks": ["风险1", "风险2"],
    "recommendations": ["建议1", "建议2", "建议3"]
}}

仅输出JSON，不要输出其他内容。"""

    messages = [{"role": "user", "content": prompt}]
    result = chat_with_deepseek(messages)

    try:
        json_str = result.strip()
        if "```json" in json_str:
            json_str = json_str.split("```json")[1].split("```")[0].strip()
        return json.loads(json_str)
    except json.JSONDecodeError:
        logger.warning("JSON解析失败，返回原始文本")
        return {"raw_response": result}
```

### 3.4 本地部署最佳实践

| 维度 | 建议 |
|------|------|
| 硬件 | 7B模型至少8GB显存，14B至少16GB，32B至少32GB |
| 量化 | 生产环境推荐Q4_K_M量化，平衡性能与精度 |
| 并发 | Ollama默认支持并发请求，可通过OLLAMA_NUM_PARALLEL调整 |
| 监控 | 使用`ollama ps`查看运行状态，`nvidia-smi`监控GPU |
| 安全 | 本地部署仅监听localhost，如需远程访问务必配置认证 |

## 四、Cursor实战效率提升总结

### 4.1 数据可视化项目的Cursor工作流

```
需求描述 → Cursor生成代码骨架 → 人工审查与微调 → Cursor补充细节 → 测试验证 → 迭代优化
```

### 4.2 关键技巧回顾

1. **规则先行**：项目开始前设定好`.cursor/rules`，包含技术栈、代码风格、依赖库版本
2. **分步生成**：复杂项目拆分为小模块，逐个让Cursor生成，避免大段代码失控
3. **上下文管理**：用`@file`指定相关文件，用Notepad记录方案，保持对话连贯
4. **错误驱动**：遇到报错直接粘贴给Cursor，比手动排查更高效
5. **学习反馈**：手动修改AI代码后，用`@fixed`标记让Cursor学习你的风格

### 4.3 常见问题与解决方案

| 问题 | 解决方案 |
|------|----------|
| 生成的pandas代码有SettingWithCopyWarning | 粘贴警告信息让Cursor修复，或要求使用`.copy()` |
| Streamlit布局不符合预期 | 用ASCII图描述布局结构，让Cursor按图生成 |
| plotly图表样式不美观 | 提供参考图表截图，让Cursor模仿样式 |
| 本地模型响应慢 | 使用量化模型，或调整`num_ctx`减少上下文长度 |

## 总结

本章通过三个实战场景，展示了Cursor在数据可视化与数据洞察领域的强大能力：

1. **多Excel报表自动化**：从数据合并、清洗到汇总输出，Cursor可快速生成完整的pandas处理流水线
2. **疫情监控大屏**：Streamlit + Plotly的组合，配合Cursor的布局描述能力，可快速搭建交互式Dashboard
3. **DeepSeek本地部署**：Ollama私有化部署结合Prompt工程，实现安全可控的数据分析能力

核心要点：Cursor不是替代思考，而是加速实现。善用规则、上下文和迭代反馈，可以让AI成为数据可视化开发的高效搭档。
