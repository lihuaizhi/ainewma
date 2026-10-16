.PHONY: build vet fmt fmt-fix test milvusd qrels qrels-full ingest-demo ingest-full eval eval-rerank eval-rerank-compare eval-rerank-llama eval-hybrid eval-hybrid-rerank rerank-server bm25-demo bm25-demo-hybrid ask clean

# Ollama.app 自带 llama-server，无需额外安装（装了 llama.cpp 也可覆盖）。
LLAMA_SERVER ?= /Applications/Ollama.app/Contents/Resources/llama-server
RERANK_MODEL ?= hans-tech/bge-reranker-v2-m3:260522

build:
	go build ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

fmt-fix:
	gofmt -w .

test:
	go test ./...

milvusd:
	go run ./cmd/milvusd

# 生成评估标注：demo 规模用前 120 页（与 make ingest-demo 的集合范围一致）
qrels:
	go run ./cmd/genqrels -max-pages 120

# 完整版标注（整本书），配合 make ingest-full 使用
qrels-full:
	go run ./cmd/genqrels

# demo 规模：只覆盖前 120 页，三套策略各约 10 分钟
ingest-demo:
	go run ./cmd/ingest -strategy chapter    -collection shl_chapter_512 -max-pages 120
	go run ./cmd/ingest -strategy fixed      -collection shl_fixed_512   -max-pages 120
	go run ./cmd/ingest -strategy small2big  -collection shl_s2b_1024_256 -max-pages 120

# 完整版（整本书，~35 分钟）：先 make qrels-full 再执行
ingest-full:
	go run ./cmd/ingest -strategy chapter    -collection shl_chapter_512
	go run ./cmd/ingest -strategy fixed      -collection shl_fixed_512
	go run ./cmd/ingest -strategy small2big  -collection shl_s2b_1024_256

# 三套策略对比（不重排）
eval:
	go run ./cmd/eval -collections shl_fixed_512,shl_chapter_512,shl_s2b_1024_256

# 看重排的收益（先 make eval 再执行这行）
eval-rerank:
	go run ./cmd/eval -collections shl_chapter_512,shl_s2b_1024_256 -rerank lexical -recall 80

# 诊断：同一向量召回下 lexical 与 bm25 重排的差异（curated qrels 专门把它放大）
eval-rerank-compare:
	go run ./cmd/eval -collections shl_chapter_512 -rerank lexical -recall 80
	go run ./cmd/eval -collections shl_chapter_512 -rerank bm25    -recall 80

# 三套策略 hybrid（向量 + BM25 + RRF）
eval-hybrid:
	go run ./cmd/eval -collections shl_fixed_512,shl_chapter_512,shl_s2b_1024_256 -mode hybrid

# 引擎拉满：hybrid 召回 + 词法重排
eval-hybrid-rerank:
	go run ./cmd/eval -collections shl_chapter_512,shl_s2b_1024_256 -mode hybrid -rerank lexical -recall 80

# 本地真 cross-encoder：起 llama-server（另开终端常驻）后，再跑这一条
# 注意：--pooling rank --rerank 是必须的，这个 GGUF 没有 bert.pooling_type 元数据。
rerank-server:
	"$(LLAMA_SERVER)" \
	  --model "$$(ollama show --modelfile $(RERANK_MODEL) | sed -n 's/^FROM //p' | head -1)" \
	  --pooling rank --rerank --host 127.0.0.1 --port 8080

# hybrid 召回 + 本地 cross-encoder 重排（先 make rerank-server）
eval-rerank-llama:
	go run ./cmd/eval -collections shl_chapter_512,shl_s2b_1024_256 -mode hybrid -rerank llama -recall 80

# Milvus 原生 BM25 demo：sparse 向量由服务端（analyzer + BM25 function）生成
bm25-demo:
	go run ./cmd/milvusbm25

# 同一 demo 的混合检索形态：向量 + BM25 两臂由 Milvus 自己用 RRF 融合
bm25-demo-hybrid:
	go run ./cmd/milvusbm25 -hybrid

ask:
	go run ./cmd/ask

clean:
	rm -rf data bin