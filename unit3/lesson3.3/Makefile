.PHONY: build vet fmt fmt-fix test agentask agentfw evalagent clean

build:
	go build ./...

bin:
	mkdir -p bin
	go build -o bin/ ./cmd/...

vet:
	go vet ./...

fmt:
	gofmt -l .

fmt-fix:
	gofmt -w .

test:
	go test ./...

# 需要先: export DEEPSEEK_API_KEY=...  以及本地 Milvus/Ollama 已启动
agentask:
	go run ./cmd/agentask -q "太阳病提纲证是什么？" -verbose

agentfw:
	go run ./cmd/agentfw -q "太阳病提纲证是什么？" -verbose

evalagent:
	go run ./cmd/evalagent -limit 10 -verbose

clean:
	rm -rf data bin
