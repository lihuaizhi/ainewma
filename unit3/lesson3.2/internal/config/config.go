// Package config holds the tunable parameters for the lesson3.2 RAG pipeline.
package config

// Chunking strategies supported by the ingestion pipeline.
const (
	// StrategyFixed ignores chapter boundaries and cuts the whole book by size.
	StrategyFixed = "fixed"
	// StrategyChapter splits the book by 六经 chapter first, then by size.
	StrategyChapter = "chapter"
	// StrategySmallToBig indexes small child chunks but feeds the enclosing
	// parent window to the LLM.
	StrategySmallToBig = "small2big"
)

const (
	// MilvusAddr is the fixed gRPC address of the lesson3.2 milvusd process.
	// It differs from lesson3.1 so both can run side by side.
	MilvusAddr = "localhost:19531"
	// MilvusDBFile is where milvus-lite persists data.
	MilvusDBFile = "./data/lesson3.2.db"

	// CollectionName is the default collection holding 伤寒论 chunks.
	CollectionName = "shl_chapter_512"
	// VectorDim must match the embedding model output (bge-m3 => 1024).
	VectorDim = 1024

	// OllamaBaseURL is the local Ollama endpoint (embeddings + chat).
	OllamaBaseURL = "http://localhost:11434"
	// EmbedModel is the embedding model served by Ollama.
	EmbedModel = "bge-m3"
	// EmbedBatch is how many chunks are sent to Ollama per request.
	EmbedBatch = 16
	// ChatModel is the generation model served by Ollama.
	ChatModel = "llama3.2:latest"

	// DeepSeekBaseURL is the official DeepSeek API endpoint (OpenAI-compatible).
	DeepSeekBaseURL = "https://api.deepseek.com"
	// DeepSeekModel is the default chat model on the official API.
	DeepSeekModel = "deepseek-chat"
	// DeepSeekAPIKeyEnv is the env var that carries the DeepSeek API key.
	DeepSeekAPIKeyEnv = "DEEPSEEK_API_KEY"

	// ChunkSize / ChunkOverlap are used by the fixed and chapter strategies.
	ChunkSize    = 512
	ChunkOverlap = 64
	// ParentSize is the window size (runes) of the small-to-big parent.
	ParentSize = 1024
	// ChildSize / ChildOverlap are used by the small-to-big children.
	ChildSize    = 256
	ChildOverlap = 32

	// DefaultPDFPath points at the book shipped with lesson3.1.
	DefaultPDFPath = "../lesson3.1/倪海厦人纪系列之伤寒论.pdf"
	// QrelsPath is the default retrieval evaluation file.
	QrelsPath = "./testdata/qrels.jsonl"

	// RetrieveRecall is the candidate pool size before reranking.
	RetrieveRecall = 30
	// RetrieveTopK is how many chunks are fed to the LLM.
	RetrieveTopK = 5
	// RRFK is the default fusion constant for the hybrid retriever.
	RRFK = 60

	// RerankURL is the default base URL for the network re-rankers
	// ("tei" / "llama"). llama-server defaults to :8080 too.
	RerankURL = "http://localhost:8080"
)
