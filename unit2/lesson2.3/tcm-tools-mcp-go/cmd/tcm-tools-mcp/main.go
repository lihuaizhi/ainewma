package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/lihuaizhi/ainewma/unit2/lession2.3/tcm-tools-mcp-go/internal/providers"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func toInterfaceSlice(v interface{}) []interface{} {
	switch t := v.(type) {
	case []interface{}:
		return t
	case []string:
		res := make([]interface{}, len(t))
		for i, s := range t {
			res[i] = s
		}
		return res
	default:
		return []interface{}{}
	}
}

func main() {
	reg := providers.NewProviderRegistry()
	factory := providers.NewTCMProviderFactory()
	cfg := providers.TCMProviderConfig{
		LocalDataDir:          getEnv("TCM_DATA_DIR", ""),
		DefaultFangjiProvider: getEnv("TCM_FANGJI_PROVIDER", ""),
		DefaultYaocaiProvider: getEnv("TCM_YAOCAI_PROVIDER", ""),
	}
	reg = factory.CreateLocal(reg, cfg)

	if p := reg.GetFangji(); p != nil {
		_ = p.Init()
	}
	if p := reg.GetYaocai(); p != nil {
		_ = p.Init()
	}

	s := server.NewMCPServer("tcm-tools-mcp-go", "0.1.0",
		server.WithLogging(),
		server.WithRecovery(),
	)

	// 1. tcm_fangji_search
	fangjiSearch := mcp.NewTool("tcm_fangji_search",
		mcp.WithDescription("Search traditional Chinese medicine formulas by keyword, contained herb, source, or category."),
		mcp.WithString("query", mcp.Description("Keyword: formula name/alias/pinyin/initials/source/indication/efficacy/tags")),
		mcp.WithString("herbName", mcp.Description("Filter by herb name contained in the formula")),
		mcp.WithString("herbId", mcp.Description("Filter by herb ID contained in the formula")),
		mcp.WithString("source", mcp.Description("Filter by source (e.g. 《伤寒论》)")),
		mcp.WithString("category", mcp.Description("Filter by formula category")),
		mcp.WithBoolean("exact", mcp.Description("Exact name match only"), mcp.DefaultBool(false)),
		mcp.WithBoolean("fuzzy", mcp.Description("Enable fuzzy matching"), mcp.DefaultBool(true)),
		mcp.WithNumber("page", mcp.Description("Page number (1-based)"), mcp.DefaultNumber(1), mcp.Min(1)),
		mcp.WithNumber("limit", mcp.Description("Results per page (max 50)"), mcp.DefaultNumber(10), mcp.Min(1), mcp.Max(50)),
		mcp.WithString("sortBy", mcp.Description("Sort field: name|source|relevance"), mcp.DefaultString("relevance")),
		mcp.WithString("sortOrder", mcp.Description("Sort order: asc|desc"), mcp.DefaultString("asc")),
	)
	s.AddTool(fangjiSearch, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p := reg.GetFangji()
		if p == nil {
			return mcp.NewToolResultError("Fangji provider not available"), nil
		}
		getStr := func(k string) string {
			if v, ok := req.GetArguments()[k].(string); ok {
				return v
			}
			return ""
		}
		getBool := func(k string, def bool) bool {
			if v, ok := req.GetArguments()[k].(bool); ok {
				return v
			}
			return def
		}
		getInt := func(k string, def int) int {
			switch v := req.GetArguments()[k].(type) {
			case float64:
				return int(v)
			case int:
				return v
			default:
				return def
			}
		}
		res, err := p.Search(providers.FangjiSearchOpts{
			Query:     getStr("query"),
			HerbName:  getStr("herbName"),
			HerbID:    getStr("herbId"),
			Source:    getStr("source"),
			Category:  getStr("category"),
			Exact:     getBool("exact", false),
			Fuzzy:     getBool("fuzzy", true),
			Page:      getInt("page", 1),
			Limit:     getInt("limit", 10),
			SortBy:    getStr("sortBy"),
			SortOrder: getStr("sortOrder"),
		})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		b, _ := json.MarshalIndent(res, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	})

	// 2. tcm_fangji_get
	fangjiGet := mcp.NewTool("tcm_fangji_get",
		mcp.WithDescription("Get full details of a TCM formula by ID or name. Prefers id if both provided."),
		mcp.WithString("id", mcp.Description("Formula ID")),
		mcp.WithString("name", mcp.Description("Formula name or alias")),
		mcp.WithBoolean("exact", mcp.Description("Use exact match for name/alias"), mcp.DefaultBool(true)),
	)
	s.AddTool(fangjiGet, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p := reg.GetFangji()
		if p == nil {
			return mcp.NewToolResultError("Fangji provider not available"), nil
		}
		getStr := func(k string) string {
			if v, ok := req.GetArguments()[k].(string); ok {
				return v
			}
			return ""
		}
		getBool := func(k string, def bool) bool {
			if v, ok := req.GetArguments()[k].(bool); ok {
				return v
			}
			return def
		}
		id := getStr("id")
		name := getStr("name")
		if id == "" && name == "" {
			return mcp.NewToolResultError("Either id or name is required"), nil
		}
		var data interface{}
		if id != "" {
			if d, err := p.GetByID(id); err == nil && d != nil {
				data = d
			}
		}
		if data == nil && name != "" {
			if getBool("exact", true) {
				if d, err := p.GetByName(name); err == nil && d != nil {
					data = d
				}
			} else {
				if sr, err := p.Search(providers.FangjiSearchOpts{Query: name, Exact: false, Page: 1, Limit: 1, SortBy: "relevance"}); err == nil && len(sr.Items) > 0 {
					data = sr.Items[0]
				}
			}
		}
		if data == nil {
			key := id
			if key == "" {
				key = name
			}
			return mcp.NewToolResultError(fmt.Sprintf("Fangji not found: %s", key)), nil
		}
		b, _ := json.MarshalIndent(data, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	})

	// 3. tcm_yaocai_search
	yaocaiSearch := mcp.NewTool("tcm_yaocai_search",
		mcp.WithDescription("Search Chinese herbs by name/alias/pinyin/initials/efficacy/indication."),
		mcp.WithString("query", mcp.Description("Keyword: herb name/alias/pinyin/initials/latin/efficacy/indication/tags")),
		mcp.WithString("propertyFlavor", mcp.Description("Property-flavor (性味)")),
		mcp.WithString("meridian", mcp.Description("Meridian (归经), string or array")),
		mcp.WithArray("meridian", mcp.Description("Meridian array")),
		mcp.WithString("efficacy", mcp.Description("Filter by efficacy keyword")),
		mcp.WithString("indication", mcp.Description("Filter by indication keyword")),
		mcp.WithString("sourceType", mcp.Description("Source type: plant|animal|mineral|other")),
		mcp.WithBoolean("exact", mcp.Description("Exact name match only"), mcp.DefaultBool(false)),
		mcp.WithBoolean("fuzzy", mcp.Description("Enable fuzzy matching"), mcp.DefaultBool(true)),
		mcp.WithNumber("page", mcp.Description("Page number (1-based)"), mcp.DefaultNumber(1), mcp.Min(1)),
		mcp.WithNumber("limit", mcp.Description("Results per page (max 50)"), mcp.DefaultNumber(10), mcp.Min(1), mcp.Max(50)),
		mcp.WithString("sortBy", mcp.Description("Sort field: name|pinyin|relevance"), mcp.DefaultString("relevance")),
		mcp.WithString("sortOrder", mcp.Description("Sort order: asc|desc"), mcp.DefaultString("asc")),
	)
	s.AddTool(yaocaiSearch, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p := reg.GetYaocai()
		if p == nil {
			return mcp.NewToolResultError("Yaocai provider not available"), nil
		}
		getStr := func(k string) string {
			if v, ok := req.GetArguments()[k].(string); ok {
				return v
			}
			return ""
		}
		getBool := func(k string, def bool) bool {
			if v, ok := req.GetArguments()[k].(bool); ok {
				return v
			}
			return def
		}
		getInt := func(k string, def int) int {
			switch v := req.GetArguments()[k].(type) {
			case float64:
				return int(v)
			case int:
				return v
			default:
				return def
			}
		}
		args := req.GetArguments()
		var meridian interface{}
		if v, ok := args["meridian"].(string); ok && v != "" {
			meridian = v
		} else if _, ok := args["meridian"].([]interface{}); ok {
			meridian = toInterfaceSlice(args["meridian"])
		} else if _, ok := args["meridian"].([]string); ok {
			meridian = args["meridian"]
		}
		var st domain.YaocaiSourceType
		if sst := getStr("sourceType"); sst != "" {
			st = domain.YaocaiSourceType(sst)
		}
		res, err := p.Search(providers.YaocaiSearchOpts{
			Query:          getStr("query"),
			PropertyFlavor: getStr("propertyFlavor"),
			Meridian:       meridian,
			Efficacy:       getStr("efficacy"),
			Indication:     getStr("indication"),
			SourceType:     st,
			Exact:          getBool("exact", false),
			Fuzzy:          getBool("fuzzy", true),
			Page:           getInt("page", 1),
			Limit:          getInt("limit", 10),
			SortBy:         getStr("sortBy"),
			SortOrder:      getStr("sortOrder"),
		})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		b, _ := json.MarshalIndent(res, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	})

	// 4. tcm_yaocai_get
	yaocaiGet := mcp.NewTool("tcm_yaocai_get",
		mcp.WithDescription("Get full details of a Chinese herb by ID or name. Prefers id if both provided."),
		mcp.WithString("id", mcp.Description("Herb ID")),
		mcp.WithString("name", mcp.Description("Herb name or alias")),
		mcp.WithBoolean("exact", mcp.Description("Use exact match for name/alias"), mcp.DefaultBool(true)),
	)
	s.AddTool(yaocaiGet, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p := reg.GetYaocai()
		if p == nil {
			return mcp.NewToolResultError("Yaocai provider not available"), nil
		}
		getStr := func(k string) string {
			if v, ok := req.GetArguments()[k].(string); ok {
				return v
			}
			return ""
		}
		getBool := func(k string, def bool) bool {
			if v, ok := req.GetArguments()[k].(bool); ok {
				return v
			}
			return def
		}
		id := getStr("id")
		name := getStr("name")
		if id == "" && name == "" {
			return mcp.NewToolResultError("Either id or name is required"), nil
		}
		var data interface{}
		if id != "" {
			if d, err := p.GetByID(id); err == nil && d != nil {
				data = d
			}
		}
		if data == nil && name != "" {
			if getBool("exact", true) {
				if d, err := p.GetByName(name); err == nil && d != nil {
					data = d
				}
			} else {
				if sr, err := p.Search(providers.YaocaiSearchOpts{Query: name, Exact: false, Page: 1, Limit: 1, SortBy: "relevance"}); err == nil && len(sr.Items) > 0 {
					data = sr.Items[0]
				}
			}
		}
		if data == nil {
			key := id
			if key == "" {
				key = name
			}
			return mcp.NewToolResultError(fmt.Sprintf("Yaocai not found: %s", key)), nil
		}
		b, _ := json.MarshalIndent(data, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	})

	// 5. tcm_providers_list
	providersList := mcp.NewTool("tcm_providers_list",
		mcp.WithDescription("List available Fangji and Yaocai providers and their default selection."),
	)
	s.AddTool(providersList, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		payload := map[string]interface{}{
			"fangji": reg.ListFangji(),
			"yaocai": reg.ListYaocai(),
			"defaults": map[string]interface{}{
				"fangjiProviderId": reg.GetDefaultFangjiID(),
				"yaocaiProviderId": reg.GetDefaultYaocaiID(),
			},
			"current": map[string]interface{}{
				"fangjiProviderId": func() string {
					if p := reg.GetFangji(); p != nil {
						return p.GetMeta().ID
					}
					return ""
				}(),
				"yaocaiProviderId": func() string {
					if p := reg.GetYaocai(); p != nil {
						return p.GetMeta().ID
					}
					return ""
				}(),
			},
		}
		b, _ := json.MarshalIndent(payload, "", "  ")
		return mcp.NewToolResultText(string(b)), nil
	})

	if err := server.ServeStdio(s); err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	}
}
