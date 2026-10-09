import type { ProviderRegistry } from "./registry.js";
export interface TCMProviderConfig {
    defaultFangjiProvider?: string;
    defaultYaocaiProvider?: string;
    localDataDir?: string;
    apiBaseUrl?: string;
    apiKey?: string;
    timeoutMs?: number;
}
export declare class TCMProviderFactory {
    static createLocal(registry: ProviderRegistry, cfg?: TCMProviderConfig): ProviderRegistry;
}
//# sourceMappingURL=factory.d.ts.map