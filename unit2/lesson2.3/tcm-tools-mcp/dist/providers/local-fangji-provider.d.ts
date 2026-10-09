import type { Fangji } from "../domain/fangji.js";
import type { IFangjiProvider, FangjiSearchOpts } from "./fangji-provider.js";
import type { SearchResult } from "./base.js";
export interface LocalFangjiProviderOptions {
    id: string;
    name: string;
    enabled: boolean;
    dataDir?: string;
    version?: string;
}
export declare class LocalFangjiProvider implements IFangjiProvider {
    id: string;
    name: string;
    version: string;
    enabled: boolean;
    capabilities: string[];
    private dataDir?;
    private data;
    constructor(opts: LocalFangjiProviderOptions);
    init(): Promise<void>;
    health(): Promise<boolean>;
    dispose(): Promise<void>;
    search(opts: FangjiSearchOpts): Promise<SearchResult<Fangji>>;
    getById(id: string): Promise<Fangji | null>;
    getByName(name: string): Promise<Fangji | null>;
}
//# sourceMappingURL=local-fangji-provider.d.ts.map