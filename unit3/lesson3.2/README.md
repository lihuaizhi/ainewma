# lesson3.2 RAG：检索增强生成（《伤寒论》问答助手）

在 lesson3.1 的向量知识库之上，补上 **检索增强生成** 的完整链路，并且把「用什么切块 / 要不要重排 / 要不要混合检索」
变成可以**离线评估**的旋钮：

- **Chunking 策略对比**：`fixed`（忽略章节）/ `chapter`（按六经篇章切）/ `small2big`（小块检索 + 大窗口喂模型）
- **检索**：向量召回（候选池）→ 可选 **BM25+RRF 混合**（hybrid）→ 可选 **重排**（lexical / llama-server cross-encoder）→ top-k
- **生成**：把命中的块组装成带引用的提示词，由本地 Ollama `llama3.2` 或 **DeepSeek 官方 API** 流式回答
- **离线评估**：从原书自动生成 qrels 标注，算 **Recall@k 与 MRR**，横向比较各个集合/策略

---

## 1. 整体架构

```
                         gRPC (localhost:19531)
┌─────────────────┐  ┌───────────────────────────────────────────┐
│ cmd/milvusd     │  │ cmd/ingest  cmd/ask  cmd/eval  cmd/genqrels│
│ milvus-lite 常驻 │◀▶│                                            │
└─────────────────┘  │  ┌──────────┐  ┌─────────┐  ┌───────────┐  │
                     │  │ pipeline  │  │retrieve │  │  generate │  │
                     │  │ 切块/元数据│  │召回+融合+ │  │ 流式回答  │  │
                     │  └────▲─────┘  │  重排     │  └─────▲─────┘  │
                     │       │  bge-m3 │┌──────┐  │       │ llama3.2│
                     │       └─────────┘│ bm25 │  └─┬── /api/chat──│
                     │                  └──────┘    └──── deepseek ┘
                     └────────────────────────────────────────────┘
```

与 lesson3.1 的差异：端口换成 `19531`、独立数据库文件（两个 lesson 可以同时跑）；新增
`retrieve` / `rerank` / `bm25` / `prompt` / `generate` / `eval` 六个包，其中 `retrieve` 支持
「向量 + BM25」双路召回，用 **RRF（Reciprocal Rank Fusion）** 融合。

## 2. 目录结构

```
lesson3.2/
├── cmd/
│   ├── milvusd/        # 服务端：milvus-lite（端口 19531）
│   ├── ingest/         # 处理器：策略化切块 + 向量化 + 入库
│   ├── ask/            # RAG：召回 → 重排 → 提示词 → 流式回答
│   ├── genqrels/       # 从原书自动生成检索评估标注 testdata/qrels.jsonl
│   ├── eval/           # 离线评估：Recall@k / MRR，横向比较多集合
│   └── milvusbm25/     # demo：Milvus 原生 BM25（analyzer + sparse 向量 + 全文检索 + RRF）
├── internal/
│   ├── config/         # 全部可调参数（集合名、块大小、模型、RRF 常数等）
│   ├── pipeline/       # 预处理：抽取 / 清洗 / 三种切块策略 / 元数据
│   ├── embed/          # Ollama bge-m3 客户端（复用 lesson3.1）
│   ├── store/          # Milvus schema、写入、检索、全量取行（新增 parent_text）
│   ├── bm25/           # 本地 BM25 索引（字符 bigram/单字），zero-dependency
│   ├── retrieve/       # 向量/混合召回 + RRF 融合 + 可选重排 + 元数据过滤
│   ├── rerank/         # 重排器：none | lexical（离线基线）| llama（llama-server cross-encoder）| tei
│   ├── prompt/         # 上下文组装（带 [序号] 引用）
│   ├── generate/       # Ollama / DeepSeek 官方 API 流式客户端
│   └── eval/           # qrels 加载/生成、Recall@k、MRR
├── testdata/qrels.jsonl
└── data/               # milvus-lite 数据库文件（运行后生成）
```

## 3. 三种 Chunking 策略

比较对象是三套参数化切块策略，`ingest -strategy` 选择，每套写进一个独立集合：

| 策略 | 集合名（默认） | 做法 | trade-off |
| --- | --- | --- | --- |
| `fixed` | `shl_fixed_512` | 忽略章节，全书 512 字/64 重叠 | 最朴素，正文不丢，但可能切断一章/一文 |
| `chapter` | `shl_chapter_512` | 先按六经篇名切出 8 章，再章内 512/64 | 保住篇章边界，lesson3.1 已验证 |
| `small2big` | `shl_s2b_1024_256` | 建索引用小块（256/32），每块冗余存所在 1024 字「父窗口」 | 小块召回更准，给模型的上下文更完整 |

small2big 在 schema 上新增了一个字段 `parent_text`：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `text` | VarChar(8192) | 小块的正文（参与向量化 & 检索） |
| `parent_text` | VarChar(8192) | 小块所在的父窗口全文（`fixed/chapter` 策略下为空） |

`store.Hit.Context()` 会优先返回 `parent_text`，`ask` 组装提示词和 `eval` 判定相关性时都基于它，
所以检索命中「小块」、喂给模型的是「大窗口」。

> 为什么父窗口与子块必须基于同一份「规范化文本」？
> knowledge 的 `FixedSizeChunking` 在切分前会做逐行 trim 的清洗，
> 直接拿原文本切片对不齐。因此 `buildSmallToBig` 先用 `normalizeText`（与 knowledge 一致的清洗规则）
> 生成父窗口与子块，再在规范化文本里回查子块起点、映射父窗口（见 `internal/pipeline/chunk.go`，
> 另有 `chunk_test.go` 保证三种策略的不变量）。

## 4. 检索、混合检索与重排

`internal/retrieve` 把一次检索做成一件事：

```
query ──▶ 向量召回候选池（bge-m3，默认 top-30）
          BM25 候选池（字符 bigram 词法，hybrid 模式，默认 top-30）
                        ──RRF 融合（默认 k=60）──▶ 可选重排：lexical / llama / tei ──▶ top-k（默认 5）
                        ──元数据过滤：-volume / -formula
```

两种模式通过 `-mode` 切换：

| 模式 | 说明 |
| --- | --- |
| `vector` | 纯向量 ANN，lesson3.2 基线 |
| `hybrid` | 向量 + **BM25** 双路各召回一个候选池，用 **RRF** 融合再进重排 |

BM25 索引（`internal/bm25`）在进程内按集合**懒加载**：第一次检索时用 `store.All` 把该集合的
全部块取出来建索引（几百到几千块，毫秒级），因此**不需要重新 ingest 也能开 hybrid**。
分词是「字符单字 + bigram」——零依赖，对古文够用；BM25 的 `k1=1.2 / b=0.75`、RRF 的 `k=60`
都是 `-rrf-k` 可调的经典值。

> 混合检索为什么在这里管用：本书原文几乎逐字出现在某个块里，向量相似度会被 512 字块里夹带的
> 「注解/白话」稀释；而 BM25 只看字面重合，天然免疫这个稀释。RRF 不看分数只看排名，所以两个
> 尺度差异巨大的检索器可以直接相加。实测效果见第 6 节：`R@1` 从 0.333 提到 0.619，叠加重排后满分。

### Milvus 原生 BM25（`cmd/milvusbm25` demo）

上面那条 BM25 臂（`internal/bm25`）是**进程内自己实现**的：`store.All` 拉全量块 → 自分词 → 建倒排 → 算分。
Milvus 2.5+ 其实**原生支持 BM25 全文检索**，可以把这一整套搬到服务端。`cmd/milvusbm25` 是个最小 demo：

```bash
make bm25-demo          # 纯 BM25 全文检索（服务端生成 sparse 向量）
make bm25-demo-hybrid   # 向量 + BM25 两臂，Milvus 自己用 RRF 融合（需本地 Ollama bge-m3）
```

实现要点（见 `cmd/milvusbm25/main.go`）：

| 组件 | 代码 | 作用 |
| --- | --- | --- |
| 分词器 | `Field.WithEnableAnalyzer(true).WithAnalyzerParams({"tokenizer":"jieba"})` | 对 `text` 字段做中文分词（`standard` 会把 CJK 切成单字，这里用 `jieba`） |
| BM25 函数 | `entity.NewFunction().WithType(entity.FunctionTypeBM25).WithInputFields("text").WithOutputFields("sparse")` | 服务端把分词结果转成 **sparse 向量**（pymilvus 里就叫 `BM25EmbeddingFunction`） |
| 索引 | `index.NewSparseInvertedIndex(entity.BM25, 0.2)` | `SPARSE_INVERTED_INDEX` + metric `BM25` |
| 全文检索 | `NewSearchOption(col, k, []entity.Vector{entity.Text(q)}).WithANNSField("sparse")` | 直接传**原始 query 文本**，Milvus 内部跑同一套 analyzer + BM25 |
| 混合融合 | `NewHybridSearchOption(...).WithReranker(NewRRFReranker())` | 两臂的**排名**由 Milvus 用 RRF 融合，等价于我们的 `retrieve.rrfFuse` |

关键点：客户端**只发原文文本**——写入时不生成 sparse 向量，检索时不计算 BM25 分数，**进程内没有倒排索引**，
自然也就没有「千万级把全量文档读进内存」的问题。这正是把 BM25 臂从「进程内」换成「Milvus 内建」的落点。

实测输出（milvus-lite，10 条条文语料）：

```
$ make bm25-demo
collection "bm25_demo" ready: 10 docs, mode = BM25 full-text only

query: 太陽病 頭痛 發熱 無汗 而喘
  #1  score=6.5179  [s5] 太陽病，頭痛發熱，身疼腰痛，骨節疼痛，惡風無汗而喘者，麻黃湯主之。
  #2  score=1.8206  [s2] 太陽病，發熱，汗出，惡風，脈緩者，名為中風。
  ...
```

> 顺带确认了一个此前没验证过的点：本项目用的 **milvus-lite 是支持 analyzer / BM25 function / sparse 向量 /
> 全文检索 / hybrid RRF 的**（`RunAnalyzer` 这个 RPC 倒是 `Unimplemented`）。所以这个 demo 不需要换成
> standalone Milvus 或 Docker，直接跑在现有 milvusd 上即可。

重排器接口在 `internal/rerank`：

| 重排器 | 说明 | 何时用 |
| --- | --- | --- |
| `none` | 保持向量顺序（默认基线） | 对比「有没有重排」 |
| `lexical` | 免费离线基线：query 字符 bigram 在块内的覆盖率 + 精确命中加分 | 没有 cross-encoder 时先看第二阶段的收益 |
| `llama` | 调 llama.cpp `llama-server` 的 `/v1/rerank`（Cohere/Jina 协议），跑本地 `bge-reranker-v2-m3` | **推荐**：单机就能上真正的 cross-encoder，不用 Docker/GPU |
| `tei` | 调 HuggingFace Text-Embeddings-Inference 的 `/rerank` | 已经有 TEI 服务时 |

### 用 llama-server 起本地 cross-encoder（推荐）

`bge-reranker-v2-m3` 这个 GGUF 的元数据里**没有 `bert.pooling_type`**，直接喂给 Ollama 会被当成
生成模型加载并触发 llama.cpp 断言（[#24967](https://github.com/ggml-org/llama.cpp/issues/24967)）。
用 llama-server 显式指定 rank pooling 即可绕开，模型照样是 Ollama 里那个本地文件：

```bash
# Ollama.app 自带 llama-server，无需额外安装；装了 llama.cpp 也可用 PATH 里的
make rerank-server        # 等价于下面这条，前台常驻，另开终端用
# llama-server --model "$(ollama show --modelfile hans-tech/bge-reranker-v2-m3:260522 \
#                          | sed -n 's/^FROM //p')" \
#   --pooling rank --rerank --host 127.0.0.1 --port 8080

go run ./cmd/ask -q "太阳病提纲证是什么？" -rerank llama            # 检索 + 重排
go run ./cmd/eval -collections shl_chapter_512 -mode hybrid -rerank llama -recall 80
```

> 代价：没有 GPU 时 cross-encoder 大约 **0.6s/(query,doc)**（本机 Intel i5 实测），所以 80 的候选池
> 每问约 50s。交互用 `-recall 30`，跑评估再上 80。`tei` 那条路仍然保留，有 GPU 时更快。

## 5. 提示词与生成

`internal/prompt` 把命中的块渲染成带编号的上下文。`internal/generate` 提供两个流式后端：

| 后端 | 命令参数 | 说明 |
| --- | --- | --- |
| Ollama | `-provider ollama`（默认） | `/api/chat` 流式，默认 `llama3.2:latest` |
| DeepSeek 官方 API | `-provider deepseek` | OpenAI 兼容协议（`openai-go/v3` SDK），默认 `deepseek-chat` |

两后端共享同一个 `temperature=0.2` 的提示词，要求模型**只依据语境并标注 [序号]**：

```
【参考资料】
[1] 篇章：辨太阳病脉证并治法上｜页码：第18页
    [1] 的内容……
[2] …

【问题】
桂枝汤的组成是什么？
```

```bash
# DeepSeek 官方 API（DEEPSEEK_API_KEY 或 -api-key）
go run ./cmd/ask -q "太阳病提纲证是什么？" -provider deepseek
go run ./cmd/ask -q "桂枝汤由哪几味药组成？" -provider deepseek -chat-model deepseek-chat
```

## 6. 离线评估

**标注（qrels）怎么来的**：原书里每一条编号条文形如「一五四：太阳病，其脉浮……」。

- `cmd/genqrels` 用正则把它们全部抽出：条文正文 = `query`；
- 相关性锚点 = 该句前 12 字（**锚定原文而非 chunk_id**，这样换任意切块策略标注都依然有效）；
- 产出 `testdata/qrels.jsonl`。`-max-pages` 参数与 `ingest -max-pages` 配套，保证标注和集合覆盖同一段书。

**指标**：

- `Recall@k`＝标准答案出现在前 k 条里。单点标准答案时就是 `Hit@k`；
- `MRR`＝第一条正确答案排名的倒数（1/rank）取均值，更看重"排得靠前"。

**怎么对比**：先为每套配置各建一个集合，再一次性评分（示例为 demo 规模，用 `-max-pages` 把标注和集合对齐到同一页范围）：

```bash
# 1. 出标注（demo：前 120 页；全书去掉 -max-pages）
go run ./cmd/genqrels -out testdata/qrels.jsonl -max-pages 120

# 2. 每套配置各建一个集合（范围与标注一致）
go run ./cmd/ingest -strategy fixed     -collection shl_fixed_512     -max-pages 120
go run ./cmd/ingest -strategy chapter   -collection shl_chapter_512   -max-pages 120
go run ./cmd/ingest -strategy small2big -collection shl_s2b_1024_256  -max-pages 120

# 3. 一次性评分 / 看重排增益 / 看混合检索增益
go run ./cmd/eval -collections shl_fixed_512,shl_chapter_512,shl_s2b_1024_256
go run ./cmd/eval -collections shl_chapter_512 -rerank lexical                    # 纯向量 + 重排
go run ./cmd/eval -collections shl_fixed_512,shl_chapter_512,shl_s2b_1024_256 -mode hybrid
go run ./cmd/eval -collections shl_chapter_512,shl_s2b_1024_256 -mode hybrid -rerank lexical -recall 80
go run ./cmd/eval -collections shl_chapter_512,shl_s2b_1024_256 -mode hybrid -rerank llama   -recall 80  # 先 make rerank-server
```

输出示例（`-max-pages 120`、候选池 30）。下面几张表是 `testdata/qrels.jsonl` **仅含 21 条自动条文 query 时**测得的结果；该文件末尾另有手工追加的 curated 条目（见本节末「诊断」），加入后数值会略变：

**纯向量（`-mode vector`，默认）**

```
collection               R@1    R@3    R@5    R@10    MRR@10
shl_fixed_512            0.333  0.571  0.619  0.714   0.473
shl_chapter_512          0.333  0.571  0.619  0.714   0.473
shl_s2b_1024_256         0.762  0.762  0.810  0.857   0.780
```

**混合检索（`-mode hybrid` = 向量 + BM25 + RRF，候选池 30）**

```
collection               R@1    R@3    R@5    R@10    MRR@10
shl_fixed_512            0.619  0.810  0.857  0.905   0.730
shl_chapter_512          0.619  0.810  0.857  0.905   0.730
shl_s2b_1024_256         0.857  0.905  0.952  1.000   0.892
```

**叠加 lexical 重排（`-mode hybrid -rerank lexical`，候选池 80）**

```
collection               R@1    R@3    R@5    R@10    MRR@10
shl_chapter_512          1.000  1.000  1.000  1.000   1.000
shl_s2b_1024_256         1.000  1.000  1.000  1.000   1.000
```

读懂这张表（循序渐进）：

1. 不看重排时，`fixed` 和 `chapter` 完全相同（同为 512 字/64 重叠，章节边界几乎没有破坏正文）；
   `small2big` 光是换小块就把 `R@1` 从 0.333 提到 0.762，MRR 从 0.473 提到 0.780——
   原因在于 512 字的块里塞满了注解，恰把「原文里的那条条文」从向量相似度上"稀释"掉了，而 256 字的子块稀释更少。
2. 光加 lexical 重排（纯向量）时 `chapter` 卡在 0.810 不再涨：**那条条文块根本没进 80 个的候选池**，
   重排救不了"参考答案不在池子里"的错。
3. 换成 hybrid 后 512 集合 `R@1` 直接翻倍（0.333 → 0.619），`R@10` 到 0.905，s2b 的 `R@10` 到 1.0——
   BM25 吃的是「字面重合」，正文里的原文逐字命中，天然补上了向量稀释丢掉的答案。
4. hybrid 再叠加重排，两套集合全部 1.000：召回池对了（BM25 捞回原文块），排名也对了（lexical 把原文块顶到第一）。

这就是 lesson 的核心结论：**先保证"正确答案能进候选池"（块要小/切得对，或加一路精确匹配的 BM25），
再用重排把排名做准**。对"找原文"这类任务，hybrid 的性价比甚至高于换块策略。

**诊断：为什么 `bm25` 与 `lexical` 在自动 qrels 上几乎无差别？**

`testdata/qrels.jsonl` 末尾追加了 2 条 curated query，专门放大两者的差异（`any_of` 即 gold 锚点）：

| query | any_of | lexical 名次 | bm25 名次 |
| --- | --- | --- | --- |
| 大下之后复发汗小便不利者亡津液故也甘澜水 | 甘澜水 | 21 | 2 |
| 太阳病发热汗出者此为营弱卫强故使汗邪风者宜桂枝汤禹余粮丸 | 禹余粮丸 | 13 | 2 |

（`-collections shl_chapter_512 -mode vector -recall 80`；名次为 gold 在重排结果中的位置）

构造手法：**长常见词正文 + 稀有术语**。`lexical` 的分数是「命中的 query bigram 数 ÷ query 总 bigram 数」，
不带 IDF，于是命中一堆常见 bigram 的长 chunk 会被顶上来；`bm25` 用 IDF 让稀有术语所在的 gold 排到第一。
聚合到评估里：`-rerank lexical` 得 R@10 0.760 / MRR 0.733，`-rerank bm25` 得 R@10 0.840 / MRR 0.780
（`make eval-rerank-compare`）。

要点：这类 query 在语义上是「跨主题」的，是为暴露 `lexical` 无 IDF 的弱点**刻意构造**，不是自然评测样本；
自动 qrels 之所以两者一致，是因为 query 直接取自条文正文、锚点又是正文前缀，字面重合近乎完美 oracle。
想让两者更稳定地分开，应改评估集本身（语义改写 query、加入字面干扰项、多/graded 相关度、用 NDCG），
而不是在重排层反复叠加同一个「字面重合」信号。

## 7. 运行

前置条件：Go 1.25.8+、本地 Ollama 装有 `bge-m3` 与 `llama3.2`；milvusd 保持运行。
要用真 cross-encoder 重排，再另起一个 `make rerank-server`（第 4 节）。

```bash
# 0. 终端 A：启动向量库（保持运行）
go run ./cmd/milvusd

# 1. 生成评估标注（可选，已有 testdata/qrels.jsonl）
go run ./cmd/genqrels

# 2. 建集合（demo 规模：先只处理前 120 页，1 本约 10 分钟；全套约 35 分钟）
go run ./cmd/ingest -strategy chapter -max-pages 120
go run ./cmd/ingest -strategy fixed   -max-pages 120
go run ./cmd/ingest -strategy small2big -max-pages 120

# 3. 离线评估
go run ./cmd/eval -collections shl_fixed_512,shl_chapter_512,shl_s2b_1024_256
go run ./cmd/eval -collections shl_fixed_512,shl_chapter_512,shl_s2b_1024_256 -mode hybrid   # 加混合检索

# 3.5 Milvus 原生 BM25 demo（不依赖 ingest，内置 10 条条文；hybrid 需 Ollama bge-m3）
go run ./cmd/milvusbm25
go run ./cmd/milvusbm25 -hybrid
go run ./cmd/milvusbm25 -q "汗出 惡風 桂枝湯" -k 3

# 4. 问答（RAG，默认 hybrid 检索）
go run ./cmd/ask -q "太阳病的主证是什么？"
go run ./cmd/ask -q "桂枝汤的组成有哪几味？" -formula "桂枝汤"
go run ./cmd/ask -q "为什么下利要禁攻？" -volume "辨太阳病脉证并治法下"
go run ./cmd/ask -q "小柴胡汤让你想到什么？" -rerank lexical -mode vector
# 换生成模型：DeepSeek 官方 API
go run ./cmd/ask -q "太阳病提纲证是什么？" -provider deepseek
go run ./cmd/ask                    # 交互式
go run ./cmd/ask -q "..." -no-gen   # 只看检索结果，不断言
```

`ingest` 常用参数：`-strategy/-collection/-chunk-size/-overlap/-parent-size/-child-size/-child-overlap/-max-pages/-limit/-drop`。
`ask` 常用参数：`-topk/-recall/-rerank/-rerank-url/-volume/-formula/-mode/-rrf-k/-provider/-api-key/-chat-model`。
`eval` 常用参数：`-collections/-ks/-recall/-rerank/-rerank-url/-mode/-rrf-k`。
其中 `-rerank` 取 `none | lexical | llama | tei`，`-rerank-url` 默认 `http://localhost:8080`（`llama` 与 `tei` 共用）。

### ask 输出示例

```
$ go run ./cmd/ask -q "太阳病的主证是什么？" -collection shl_s2b_1024_256 -topk 3 -no-gen

────────────────── 引用来源 ──────────────────
[1] 辨太阳病脉证并治法中  第69页  score=0.5381
    方剂: 小柴胡汤、桂枝汤
    ……中医的少阳就是淋巴系统，太阳表证在人的皮肤表面……
    ……
```

> `ask` 的「回答」部分默认由本机 `llama3.2` 生成，中文与原文引用能力有限（见下面第 5 条限制），
> 演示时最常见的是"答得对方向但细节不精确"。检索与重排的客观指标请以 `cmd/eval` 的表格为准；
> 想换更强生成模型，`-provider deepseek` 直接走 DeepSeek 官方 API，或把 `-chat-model` 换成任意 Ollama 模型（如 qwen 系列）。

## 8. 设计取舍与已知限制

1. **小规模 demo 的耗时**：bge-m3 在 CPU 上约 1.8 秒/块。三套策略 × 全书一遍大约 35–40 分钟，
   `-max-pages` 可以先跑小块。标注必须和集合用同一个页面范围，否则「没被收录的条文」会虚低（等价于假阴性）。
   混合检索本身不额外耗时（BM25 走本地进程内索引，只首次全量取行）。
2. **small2big 的父窗口是定长不重叠窗口**，可能把一条条文从中间切开，父窗口里的上下文靠"接壤"保证；
   更精致的做法是父=整条条文（需要可靠的分条算法，这本 PDF 的条文号不全，详见 lesson3.1 已知限制）。
3. **lexical 重排是字面 bigram**，不是语义模型；它在这里的价值是"不用任何外部服务也能演示第二阶段的收益"。
   真正想用语义重排，就 `-rerank llama`（本地 `llama-server` 跑 `bge-reranker-v2-m3` 这个 cross-encoder）。
4. **BM25 是字符字面检索**：对同义改写/白话问法没有召回能力，所以它是"救原文精确命中"的利器，替代不了向量臂；
   另外 `ask -filter` 传的原始表达式只作用在向量臂，BM25 臂只认结构化的 `-volume/-formula` 过滤。
5. **qrels 自动标注偏"查找"任务**，query 就是原文句子，衡量的是"能否找回原文"。它很难覆盖
   「开放性问题」（如"为什么"），真正的生产评估应再手标一批问题式 query。
6. **生成模型**：本机 llama3.2 中文能力有限；DeepSeek 官方 API（`-provider deepseek` + `DEEPSEEK_API_KEY`）
   切换后只影响生成质量，不影响上面的检索指标。
7. 其他来自 lesson3.1 的限制依然成立（milvus-lite 不支持 HNSW / `json_contains`，用 `formula_text like` 兜底；
   文本约 1.25% 乱码已清洗）。

   补充：milvus-lite **支持** analyzer / BM25 function / sparse 向量 / 全文检索 / hybrid RRF（见第 4 节
   `cmd/milvusbm25` demo），只有 `RunAnalyzer` 这个 RPC 返回 `Unimplemented`。