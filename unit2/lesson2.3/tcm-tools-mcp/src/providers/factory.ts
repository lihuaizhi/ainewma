import { LocalFangjiProvider } from "./local-fangji-provider.js";
import { LocalYaocaiProvider } from "./local-yaocai-provider.js";
import type { ProviderRegistry } from "./registry.js";

export interface TCMProviderConfig {
  defaultFangjiProvider?: string;
  defaultYaocaiProvider?: string;
  localDataDir?: string;
  apiBaseUrl?: string;
  apiKey?: string;
  timeoutMs?: number;
}

export class TCMProviderFactory {
  static createLocal(
    registry: ProviderRegistry,
    cfg: TCMProviderConfig = {}
  ): ProviderRegistry {
    const localFangji = new LocalFangjiProvider({
      id: "local",
      name: "Local TCM Formulas",
      enabled: true,
      dataDir: cfg.localDataDir,
      version: "0.1.0",
    });

    const localYaocai = new LocalYaocaiProvider({
      id: "local",
      name: "Local TCM Herbs",
      enabled: true,
      dataDir: cfg.localDataDir,
      version: "0.1.0",
    });

    registry.registerFangji(localFangji);
    registry.registerYaocai(localYaocai);

    if (cfg.defaultFangjiProvider) {
      registry.setDefaultFangji(cfg.defaultFangjiProvider);
    }
    if (cfg.defaultYaocaiProvider) {
      registry.setDefaultYaocai(cfg.defaultYaocaiProvider);
    }

    return registry;
  }
}
