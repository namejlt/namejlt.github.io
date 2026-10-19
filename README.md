# namejlt.github.io

> 个人知识库 / 博客 —— 基于 [Hugo](https://gohugo.io/) 静态网站生成器构建，部署于 GitHub Pages。

- 站点标题：**LX 知识库**
- 访问地址：<https://namejlt.github.io/>
- 主题：[hugo-clarity](https://github.com/chipzoller/hugo-clarity)（以 Git 子模块方式引入）
- 许可证：[Apache License 2.0](./LICENSE)

---

## 项目简介

本项目是一个用于沉淀学习笔记、技术实践与阅读札记的个人知识库。内容以 Markdown 编写，由 Hugo 在构建时转换为静态 HTML 页面，并通过 GitHub Actions 自动发布到 `gh-pages` 分支、由 GitHub Pages 托管。

内容目前分为三大板块：

| 板块 | 路径 | 说明 |
| --- | --- | --- |
| 技术 | `content/posts/tech/` | AI、Golang、Java、Rust、源码阅读、实践等 |
| 阅读 | `content/posts/reading/` | 读书笔记 |
| 生活 | `content/posts/life/` | 技能、随笔等 |

每篇文章通过 `categories`（分类）与 `tags`（标签）进行组织，并配有「文章 / 标签 / 分类 / 关于」导航菜单。

---

## 技术栈

- **Hugo（Extended 版本）**：静态站点生成器，负责内容构建。
- **hugo-clarity 主题**：作为 Git 子模块（`themes/clarity`）。
- **Mermaid**：在页面底部注入 Mermaid 脚本，文章内可直接绘制流程图/时序图等。
- **GitHub Pages + GitHub Actions**：推送 `main` 分支后自动构建并部署到 `gh-pages`。
- 已启用中文（CJK）排版优化、深色模式、代码行号与 `monokai` 代码高亮。

---

## 目录结构

```text
namejlt.github.io/
├── .github/workflows/gh-pages.yml  # 自动构建部署到 GitHub Pages
├── archetypes/default.md           # 新建文章模板（TOML front matter）
├── content/                        # 站点内容（Markdown）
│   ├── about.md                    # 关于页
│   └── posts/                      # 文章
│       ├── tech/                   # 技术：ai/ practice/ readcode/ rust/ java/ golang/
│       ├── reading/                # 阅读笔记
│       └── life/                   # 生活随笔
├── layouts/partials/footer.html    # 自定义页脚（注入 Mermaid 等）
├── static/                         # 静态资源（imgs/、code/ 等）
├── themes/clarity/                 # hugo-clarity 主题（Git 子模块，勿直接修改）
├── hugo.toml                      # Hugo 站点配置
├── .gitmodules                    # 子模块定义
├── .gitignore
└── LICENSE
```

> 构建产物 `public/`、`resources/` 已在 `.gitignore` 中忽略，不纳入版本库。

---

## 本地运行

### 前置要求

- 安装 **Hugo Extended** 版本（Extended 包含 SCSS 支持，clarity 主题需要）。
  - macOS：`brew install hugo`
  - Windows（Chocolatey）：`choco install hugo-extended -y`
  - 或从 <https://github.com/gohugoio/hugo/releases> 下载带 `extended` 字样的版本。
  - 验证：`hugo version`（应显示 `extended`）。

### 步骤

1. **克隆仓库（含子模块）**

   ```bash
   git clone --recurse-submodules https://github.com/namejlt/namejlt.github.io.git
   cd namejlt.github.io
   ```

   如果已克隆但未拉取子模块，执行：

   ```bash
   git submodule update --init --recursive
   ```

2. **本地预览（带热更新）**

   ```bash
   hugo server -D
   ```

   打开 <http://localhost:1313/> 即可实时查看（包含草稿）。

3. **正式构建**

   ```bash
   hugo --minify
   ```

   生成的静态文件位于 `public/` 目录。

---

## 更新子模块（主题）

主题通过 Git 子模块管理，更新到上游最新版本：

```bash
git submodule update --remote --recursive
git add themes/clarity
git commit -m "chore: update clarity theme"
```

---

## 写作指南

### 新建文章

```bash
hugo new posts/tech/ai/my-article.md
```

这将基于 `archetypes/default.md` 生成草稿文件（默认 `draft: true`）。

### Front Matter 字段说明

文章使用 YAML front matter，常用字段如下：

```yaml
---
title: "文章标题"
date: 2025-05-20T09:00:00+08:00
draft: false          # true 为草稿，本地 -D 可见，构建时不发布
toc: true             # 是否显示目录
categories: ["技术/AI/应用开发"]   # 分类，支持用 / 表达层级
tags: ["AI", "应用开发", "大模型"]
---
```

> 也可使用 TOML 格式（`+++` 包裹），与 `archetypes/default.md` 模板一致。

### 内容编写

- 正文为 Markdown，支持标准语法。
- 已开启 `unsafe` 渲染，可在文章中直接写 HTML。
- 支持 Mermaid 图表，例如：

  ````markdown
  ```mermaid
  graph TD
    A[开始] --> B[处理]
    B --> C[结束]
  ```
  ````

- 图片建议放在 `static/imgs/` 下，通过 `/imgs/xxx.png` 引用。

### 发布流程

1. 将文章的 `draft` 改为 `false`。
2. 提交并推送到 `main` 分支：

   ```bash
   git add content/posts/...
   git commit -m "docs: 新增 xxx 文章"
   git push origin main
   ```

3. GitHub Actions 会自动构建并部署到 GitHub Pages（约 1 分钟内生效）。

---

## 部署说明

部署由 `.github/workflows/gh-pages.yml` 自动完成：

- **触发**：推送到 `main` 分支，或提交 Pull Request。
- **流程**：检出代码（含子模块）→ 安装 Hugo Extended → `hugo --minify` 构建 → 发布 `public/` 到 `gh-pages` 分支。
- **结果**：GitHub Pages 从 `gh-pages` 分支提供站点，访问 <https://namejlt.github.io/>。

无需手动构建或上传 `public/` 目录。

---

## 许可证

本项目源码基于 [Apache License 2.0](./LICENSE) 发布。  
主题 `themes/clarity` 遵循其自身的许可证（见子模块内 `LICENSE`）。
