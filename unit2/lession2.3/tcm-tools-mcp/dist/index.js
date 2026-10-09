import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { ProviderRegistry, TCMProviderFactory } from "./providers/index.js";
function buildServer(cfg = {}) {
    const registry = new ProviderRegistry();
    TCMProviderFactory.createLocal(registry, {
        localDataDir: cfg.localDataDir,
        defaultFangjiProvider: cfg.defaultFangjiProvider,
        defaultYaocaiProvider: cfg.defaultYaocaiProvider,
        apiBaseUrl: cfg.apiBaseUrl,
        apiKey: cfg.apiKey,
        timeoutMs: cfg.timeoutMs,
    });
    const initAll = async () => {
        try {
            await registry.getFangji()?.init?.();
        }
        catch (e) {
            console.error("[TCM MCP] Failed to init Fangji provider:", e);
        }
        try {
            await registry.getYaocai()?.init?.();
        }
        catch (e) {
            console.error("[TCM MCP] Failed to init Yaocai provider:", e);
        }
    };
    const server = new McpServer({
        name: "tcm-tools-mcp",
        version: "0.1.0",
        description: "TCM (Traditional Chinese Medicine) tools: Fangji (formulas/prescriptions) and Yaocai (herbs) query via Provider abstraction.",
    });
    // 1. 方剂搜索
    server.registerTool("tcm_fangji_search", {
        title: "Search TCM Formulas (Fangji)",
        description: "Search traditional Chinese medicine formulas by keyword, contained herb, source, or category.",
        inputSchema: {
            query: z
                .string()
                .optional()
                .describe("Keyword: formula name/alias/pinyin/initials/source/indication/efficacy/tags"),
            herbName: z.string().optional().describe("Filter by herb name contained in the formula"),
            herbId: z.string().optional().describe("Filter by herb ID contained in the formula"),
            source: z.string().optional().describe("Filter by source (e.g. 《伤寒论》)"),
            category: z.string().optional().describe("Filter by formula category"),
            exact: z.boolean().optional().default(false).describe("Exact name match only"),
            fuzzy: z.boolean().optional().default(true).describe("Enable fuzzy matching"),
            page: z.number().int().min(1).optional().default(1).describe("Page number (1-based)"),
            limit: z.number().int().min(1).max(50).optional().default(10).describe("Results per page (max 50)"),
            sortBy: z.enum(["name", "source", "relevance"]).optional().default("relevance"),
            sortOrder: z.enum(["asc", "desc"]).optional().default("asc"),
        },
    }, async (args) => {
        const p = registry.getFangji();
        if (!p)
            throw new Error("Fangji provider not available");
        const res = await p.search({
            query: args.query,
            herbName: args.herbName,
            herbId: args.herbId,
            source: args.source,
            category: args.category,
            exact: args.exact ?? false,
            fuzzy: args.fuzzy ?? true,
            page: args.page ?? 1,
            limit: args.limit ?? 10,
            sortBy: args.sortBy ?? "relevance",
            sortOrder: args.sortOrder ?? "asc",
        });
        return {
            content: [{ type: "text", text: JSON.stringify(res, null, 2) }],
        };
    });
    // 2. 方剂详情
    server.registerTool("tcm_fangji_get", {
        title: "Get TCM Formula Detail (Fangji)",
        description: "Get full details of a TCM formula by ID or name. Prefers id if both provided.",
        inputSchema: {
            id: z.string().optional().describe("Formula ID"),
            name: z.string().optional().describe("Formula name or alias"),
            exact: z.boolean().optional().default(true).describe("Use exact match for name/alias"),
        },
    }, async (args) => {
        const { id, name, exact = true } = args;
        if (!id && !name)
            throw new Error("Either id or name is required");
        const p = registry.getFangji();
        if (!p)
            throw new Error("Fangji provider not available");
        let data = null;
        if (id && id.trim().length > 0) {
            data = await p.getById(id.trim());
        }
        if (!data && name && name.trim().length > 0) {
            const nm = name.trim();
            if (exact) {
                data = await p.getByName(nm);
            }
            else {
                const sr = await p.search({
                    query: nm,
                    exact: false,
                    page: 1,
                    limit: 1,
                    sortBy: "relevance",
                });
                data = sr.items[0] ?? null;
            }
        }
        if (!data) {
            const key = id ?? name ?? "";
            throw new Error(`Fangji not found: ${key}`);
        }
        return {
            content: [{ type: "text", text: JSON.stringify(data, null, 2) }],
        };
    });
    // 3. 药材搜索
    server.registerTool("tcm_yaocai_search", {
        title: "Search TCM Herbs (Yaocai)",
        description: "Search Chinese herbs by name/alias/pinyin/initials/efficacy/indication.",
        inputSchema: {
            query: z
                .string()
                .optional()
                .describe("Keyword: herb name/alias/pinyin/initials/latin/efficacy/indication/tags"),
            propertyFlavor: z.string().optional().describe("Property-flavor (性味)"),
            meridian: z.union([z.string(), z.array(z.string())]).optional().describe("Meridian (归经)"),
            efficacy: z.string().optional().describe("Filter by efficacy keyword"),
            indication: z.string().optional().describe("Filter by indication keyword"),
            sourceType: z.enum(["plant", "animal", "mineral", "other"]).optional().describe("Source type"),
            exact: z.boolean().optional().default(false),
            fuzzy: z.boolean().optional().default(true),
            page: z.number().int().min(1).optional().default(1),
            limit: z.number().int().min(1).max(50).optional().default(10),
            sortBy: z.enum(["name", "pinyin", "relevance"]).optional().default("relevance"),
            sortOrder: z.enum(["asc", "desc"]).optional().default("asc"),
        },
    }, async (args) => {
        const p = registry.getYaocai();
        if (!p)
            throw new Error("Yaocai provider not available");
        const res = await p.search({
            query: args.query,
            propertyFlavor: args.propertyFlavor,
            meridian: args.meridian,
            efficacy: args.efficacy,
            indication: args.indication,
            sourceType: args.sourceType,
            exact: args.exact ?? false,
            fuzzy: args.fuzzy ?? true,
            page: args.page ?? 1,
            limit: args.limit ?? 10,
            sortBy: args.sortBy ?? "relevance",
            sortOrder: args.sortOrder ?? "asc",
        });
        return {
            content: [{ type: "text", text: JSON.stringify(res, null, 2) }],
        };
    });
    // 4. 药材详情
    server.registerTool("tcm_yaocai_get", {
        title: "Get TCM Herb Detail (Yaocai)",
        description: "Get full details of a Chinese herb by ID or name. Prefers id if both provided.",
        inputSchema: {
            id: z.string().optional().describe("Herb ID"),
            name: z.string().optional().describe("Herb name or alias"),
            exact: z.boolean().optional().default(true).describe("Use exact match for name/alias"),
        },
    }, async (args) => {
        const { id, name, exact = true } = args;
        if (!id && !name)
            throw new Error("Either id or name is required");
        const p = registry.getYaocai();
        if (!p)
            throw new Error("Yaocai provider not available");
        let data = null;
        if (id && id.trim().length > 0) {
            data = await p.getById(id.trim());
        }
        if (!data && name && name.trim().length > 0) {
            const nm = name.trim();
            if (exact) {
                data = await p.getByName(nm);
            }
            else {
                const sr = await p.search({
                    query: nm,
                    exact: false,
                    page: 1,
                    limit: 1,
                    sortBy: "relevance",
                });
                data = sr.items[0] ?? null;
            }
        }
        if (!data) {
            const key = id ?? name ?? "";
            throw new Error(`Yaocai not found: ${key}`);
        }
        return {
            content: [{ type: "text", text: JSON.stringify(data, null, 2) }],
        };
    });
    // 5. Provider 列表
    server.registerTool("tcm_providers_list", {
        title: "List TCM Providers",
        description: "List available Fangji and Yaocai providers and their default selection.",
        inputSchema: {},
    }, async () => {
        const fangjiList = registry.listFangji();
        const yaocaiList = registry.listYaocai();
        const defF = registry.getDefaultFangjiId();
        const defY = registry.getDefaultYaocaiId();
        const currentF = registry.getFangji();
        const currentY = registry.getYaocai();
        const payload = {
            fangji: fangjiList,
            yaocai: yaocaiList,
            defaults: {
                fangjiProviderId: defF,
                yaocaiProviderId: defY,
            },
            current: {
                fangjiProviderId: currentF?.id ?? null,
                yaocaiProviderId: currentY?.id ?? null,
            },
        };
        return {
            content: [{ type: "text", text: JSON.stringify(payload, null, 2) }],
        };
    });
    return { server, initAll };
}
async function main() {
    const dataDir = process.env.TCM_DATA_DIR;
    const defaultFangjiProvider = process.env.TCM_FANGJI_PROVIDER;
    const defaultYaocaiProvider = process.env.TCM_YAOCAI_PROVIDER;
    const cfg = {
        localDataDir: dataDir || undefined,
        defaultFangjiProvider: defaultFangjiProvider || undefined,
        defaultYaocaiProvider: defaultYaocaiProvider || undefined,
    };
    const { server, initAll } = buildServer(cfg);
    await initAll();
    const transport = new StdioServerTransport();
    await server.connect(transport);
    console.error("TCM Tools MCP Server (tcm-tools-mcp) running on stdio");
}
main().catch((e) => {
    console.error("Failed to start TCM MCP Server:", e);
    process.exit(1);
});
//# sourceMappingURL=index.js.map