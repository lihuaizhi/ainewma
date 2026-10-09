import type { Yaocai } from "../domain/yaocai.js";
import type { IYaocaiProvider, YaocaiSearchOpts } from "./yaocai-provider.js";
import type { SearchResult } from "./base.js";
export interface LocalYaocaiProviderOptions {
    id: string;
    name: string;
    enabled: boolean;
    dataDir?: string;
    version?: string;
}
export declare class LocalYaocaiProvider implements IYaocaiProvider {
    id: string;
    name: string;
    version: string;
    enabled: boolean;
    capabilities: string[];
    private dataDir?;
    private data;
    constructor(opts: LocalYaocaiProviderOptions);
    init(): Promise<void>;
    health(): Promise<boolean>;
    dispose(): Promise<void>;
    search(opts: YaocaiSearchOpts): Promise<SearchResult<Yaocai>>;
    getById(id: string): Promise<Yaocai | null>;
    getByName(name: string): Promise<Yaocai | null>;
}
//# sourceMappingURL=local-yaocai-provider.d.ts.map