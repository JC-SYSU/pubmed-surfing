[English](README.md) | 中文版

# PubMed Surfing

生物医学领域的文献活离不开 PubMed——检索、摘要、引文、找期刊都绕着它转。PubMed Surfing 是一个本地 MCP stdio 服务器，把这摊事收拢成十一个定义良好的工具调用：结构化检索、批量摘要、ID 转换、引文格式化、相关文献发现、期刊画像。它通过 stdin/stdout 跟 Codex、Claude Code、OpenCode 对话，不需要账号、不需要 API key，数据也不离开本机。

## 它做什么

一个 MCP 服务器，十一个工具，三件事：

1. **找论文。** `pubmed_search` 支持原始 PubMed 查询、结构化字段（标题 / MeSH / ISSN / 年份 / 文章类型）、以及 PubMed 邻近语法，返回紧凑的命中列表（PMID、标题、作者、期刊、年份）。`pubmed_get_total_count` 只回答"有多少篇匹配"而不拉列表。`pubmed_find_related` 顺着 ELink 的 neighbor_score 从一篇 PMID 出发找相关工作。
2. **读论文。** `pubmed_fetch_abstract` 和 `pubmed_fetch_abstracts_batch` 拉取完整摘要与元数据，PubMed 标注了章节时按章节截断，优先保留背景/目的和结论。`esummary` 超时的时候，检索自动降级为只返回 PMID，客户端照样能按 ID 拉摘要，而不是整次查询失败。
3. **产出。** `pubmed_convert_ids` 通过 NCBI PMC ID Converter 在 DOI / PMID / PMCID 之间互转。`pubmed_format_citations` 从 PMID 或实时结果生成 APA、MLA、BibTeX、RIS 引文。`pubmed_verify_article_type`、`pubmed_journal_profile`、`pubmed_journal_mesh_profile` 回答期刊类问题——这期刊近年发不发 meta 分析、文章结构长什么样、主要挂哪些 MeSH 词。`pubmed_clear_cache` 清空进程内的请求缓存。

## 为 AI 上下文而限流

返回体大小是产品决策，不是 NCBI 的意外。检索默认最多返回 10 条（硬上限 100）；用 `confirm_full: true` 重试可以把上限抬到 PubMed 的 10000。传输层是另一套独立的上限：一条 GET 最多带 200 个 PMID；更多就按 NCBI 对 ESummary 的建议切成 500 一批的 form-encoded POST。一层限的是进模型上下文的东西，一层限的是单个 HTTP 请求携带的东西。两层都是有意的、往小了限，被截断时 `truncated: true` 会明说。

## 适合谁

需要客户端正经做生物医学文献工作的人——文献综述、引文整理、期刊筛选、就某个具体话题查证现状。工具只在客户端调用时执行：不监听会话、不按关键词自动触发、没有后台轮询。凡是 Go 能跑的机器都能跑：macOS、Linux、Windows。没有要托管的服务，没有数据库。

## 怎么用

**安装**——复制下面这段（右侧有复制按钮）直接粘贴给你的 agent（Codex、Claude Code、OpenCode 或任意编程 agent），它会读安装指南并动手：

```
你正在安装 PubMed Surfing MCP 服务器。源码仓库在 https://github.com/JC-SYSU/pubmed-surfing 。请按以下要求执行：

1. 把该仓库克隆到合适位置并 cd 进入。如果无法克隆，直接阅读安装规范：https://raw.githubusercontent.com/JC-SYSU/pubmed-surfing/main/INSTALL.md
2. 完整阅读 INSTALL.md（想看中文版就读 INSTALL.zh-CN.md），并逐节执行：探测本机并确定安装形态（0.5 节）、构建并验证源码（第 1 节）、发布与安装（第 2–3 节）、注册本机已有的 MCP 客户端（第 4 节）。
3. 任何向本仓库以外写入的步骤（客户端 MCP 配置、PATH、运行时目录），先把你将要做的展示给我，等我确认后再执行。
4. 全部完成后运行验收清单（第 5 节），并把每项结果报告给我。有失败的项，先按故障排查表（第 7 节）修复再汇报。
```

推荐走正式发布流程：`pubmed-surfingctl release` 产出带校验和的归档，`install` 逐项核验（旁车校验和、路径穿越拒绝、平台匹配、载荷哈希），通过 MCP 冒烟测试后才切换活动运行时。细节见 INSTALL.md。

想手动安装的话直接按 `INSTALL.md` 操作。

**日常**：没有要跑的，没有要查的。服务器就是一个 MCP 命令。

## 快速验证已装上

```bash
claude mcp list    # pubmed-surfing 应显示（Claude Code）
codex mcp list     # pubmed-surfing 应显示（Codex）
```

离线端到端验证（构建 → 发布 → 安装 → 校验）见 INSTALL.md「验收清单」。

## License

MIT。行为细节——请求上限、缓存、部分结果、平台覆盖——见 INSTALL.md。