import type { ProviderMeta } from "./base.js";
import type { IFangjiProvider } from "./fangji-provider.js";
import type { IYaocaiProvider } from "./yaocai-provider.js";
export declare class ProviderRegistry {
    private fangjiProviders;
    private yaocaiProviders;
    private defaultFangjiId?;
    private defaultYaocaiId?;
    registerFangji(p: IFangjiProvider): void;
    registerYaocai(p: IYaocaiProvider): void;
    private getFirstEnabledFangjiId;
    private getFirstEnabledYaocaiId;
    setDefaultFangji(id: string): void;
    setDefaultYaocai(id: string): void;
    getFangji(id?: string): IFangjiProvider | undefined;
    getYaocai(id?: string): IYaocaiProvider | undefined;
    listFangji(): ProviderMeta[];
    listYaocai(): ProviderMeta[];
    getDefaultFangjiId(): string | null;
    getDefaultYaocaiId(): string | null;
    clear(): void;
}
//# sourceMappingURL=registry.d.ts.map