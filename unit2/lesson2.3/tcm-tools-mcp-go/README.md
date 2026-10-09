# tcm-tools-mcp-go

基于 **Provider 抽象思维** 封装的中医工具 MCP Server（Go 语言版本），提供方剂（Fangji/Prescription）和药材（Yaocai/Herb）的查询能力。

## 核心特性

- 🧩 **Provider 抽象（Provider Pattern）**：将“工具能力”（MCP Tools）与“数据来源”（Providers）完全解耦。工具层只依赖接口，不依赖具体实现，便于后续接入第三方数据源（TCMSP、ETCM、SymMap 等）。
- 🔍 **方剂查询（Fangji）**：支持按名称/别名/拼音/首字母、含药材、出处、分类、主治/功效等多维度搜索。
- 🌿 **药材查询（Yaocai）**：支持按名称/别名/拼音/首字母、性味、归经、功效、主治、来源类型等条件过滤。
- 📄 **分页与排序**：内置分页（`page/limit`，最大 50/页）和排序（`relevance/name/pinyin/source`）。
- 💾 **开箱即用**：内置本地 JSON 数据源（`LocalFangjiProvider`、`LocalYaocaiProvider`），无需外部数据库即可运行。
- 🧪 **可扩展**：`ProviderRegistry + Factory` 支持多 Provider 共存、默认提供方切换和运行时扩展。
- 🛡️ **只读查询**：纯查询工具，无写操作，天然安全。

## 工具列表

| 工具名 | 功能 | 说明 |
|---|---|---|
| `tcm_fangji_search` | 搜索方剂 | 支持关键词、含药材（`herbName/herbId`）、出处（`source`）、分类（`category`）、精确匹配（`exact`）、分页、排序。 |
| `tcm_fangji_get` | 获取方剂详情 | 按 `id` 或 `name`（支持别名）获取方剂完整信息，优先使用 `id`。 |
| `tcm_yaocai_search` | 搜索药材 | 支持关键词、性味（`propertyFlavor`）、归经（`meridian`，字符串或数组）、功效（`efficacy`）、主治（`indication`）、来源类型（`sourceType`）、分页、排序。 |
| `tcm_yaocai_get` | 获取药材详情 | 按 `id` 或 `name`（支持别名）获取药材完整信息，优先使用 `id`。 |
| `tcm_providers_list` | 查看 Providers | 列出当前可用的 Fangji/Yaocai Providers、默认提供方及当前生效的 Provider。 |

## 快速开始

### 1. 下载依赖

```bash
go mod tidy
```

### 2. 构建

```bash
go build -o bin/tcm-tools-mcp ./cmd/tcm-tools-mcp
```

### 3. 运行

```bash
./bin/tcm-tools-mcp
```

或直接运行：

```bash
go run ./cmd/tcm-tools-mcp
```

## 环境变量

| 环境变量 | 说明 | 默认值 |
|---|---|---|
| `TCM_DATA_DIR` | 本地数据目录路径 | `./data`（自动按候选路径查找） |
| `TCM_FANGJI_PROVIDER` | 默认方剂 Provider ID | `local` |
| `TCM_YAOCAI_PROVIDER` | 默认药材 Provider ID | `local` |

## MCP 客户端配置（Claude Desktop 示例）

编辑 `~/Library/Application Support/Claude/claude_desktop_config.json`：

```json
{
  "mcpServers": {
    "tcm-tools-go": {
      "command": "/Users/qiaozhiming/Develop/go/src/github.com/lihuaizhi/ainewma/unit2/lession2.3/tcm-tools-mcp-go/bin/tcm-tools-mcp",
      "env": {
        "TCM_DATA_DIR": "/Users/qiaozhiming/Develop/go/src/github.com/lihuaizhi/ainewma/unit2/lession2.3/tcm-tools-mcp-go/data"
      }
    }
  }
}
```

配置完成后重启 Claude Desktop 即可使用。

## 项目结构

```text
tcm-tools-mcp-go/
├── cmd/tcm-tools-mcp/main.go
├── data/
│   ├── fangji.json
│   └── yaocai.json
├── internal/
│   ├── domain/
│   │   ├── fangji.go
│   │   └── yaocai.go
│   ├── providers/
│   │   ├── base.go
│   │   ├── fangji_provider.go
│   │   ├── yaocai_provider.go
│   │   ├── local_fangji_provider.go
│   │   ├── local_yaocai_provider.go
│   │   ├── registry.go
│   │   └── factory.go
│   └── utils/
│       ├── io.go
│       └── search.go
├── go.mod
└── README.md
```
