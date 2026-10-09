import type { ProviderMeta } from "./base.js";
import type { IFangjiProvider } from "./fangji-provider.js";
import type { IYaocaiProvider } from "./yaocai-provider.js";

export class ProviderRegistry {
  private fangjiProviders = new Map<string, IFangjiProvider>();
  private yaocaiProviders = new Map<string, IYaocaiProvider>();
  private defaultFangjiId?: string;
  private defaultYaocaiId?: string;

  registerFangji(p: IFangjiProvider): void {
    if (!p) return;
    if (p.enabled) {
      this.fangjiProviders.set(p.id, p);
    }
    if (!this.defaultFangjiId && p.enabled) {
      this.defaultFangjiId = p.id;
    }
    if (
      this.defaultFangjiId &&
      (!this.fangjiProviders.has(this.defaultFangjiId) ||
        !this.fangjiProviders.get(this.defaultFangjiId)?.enabled)
    ) {
      this.defaultFangjiId = this.getFirstEnabledFangjiId();
    }
  }

  registerYaocai(p: IYaocaiProvider): void {
    if (!p) return;
    if (p.enabled) {
      this.yaocaiProviders.set(p.id, p);
    }
    if (!this.defaultYaocaiId && p.enabled) {
      this.defaultYaocaiId = p.id;
    }
    if (
      this.defaultYaocaiId &&
      (!this.yaocaiProviders.has(this.defaultYaocaiId) ||
        !this.yaocaiProviders.get(this.defaultYaocaiId)?.enabled)
    ) {
      this.defaultYaocaiId = this.getFirstEnabledYaocaiId();
    }
  }

  private getFirstEnabledFangjiId(): string | undefined {
    for (const [id, p] of this.fangjiProviders) {
      if (p.enabled) return id;
    }
    return undefined;
  }

  private getFirstEnabledYaocaiId(): string | undefined {
    for (const [id, p] of this.yaocaiProviders) {
      if (p.enabled) return id;
    }
    return undefined;
  }

  setDefaultFangji(id: string): void {
    if (id && this.fangjiProviders.has(id)) {
      const p = this.fangjiProviders.get(id);
      if (p?.enabled) {
        this.defaultFangjiId = id;
      }
    }
  }

  setDefaultYaocai(id: string): void {
    if (id && this.yaocaiProviders.has(id)) {
      const p = this.yaocaiProviders.get(id);
      if (p?.enabled) {
        this.defaultYaocaiId = id;
      }
    }
  }

  getFangji(id?: string): IFangjiProvider | undefined {
    const targetId = id ?? this.defaultFangjiId;
    if (!targetId) return undefined;
    const p = this.fangjiProviders.get(targetId);
    return p?.enabled ? p : undefined;
  }

  getYaocai(id?: string): IYaocaiProvider | undefined {
    const targetId = id ?? this.defaultYaocaiId;
    if (!targetId) return undefined;
    const p = this.yaocaiProviders.get(targetId);
    return p?.enabled ? p : undefined;
  }

  listFangji(): ProviderMeta[] {
    return Array.from(this.fangjiProviders.values()).map((p) => ({
      id: p.id,
      name: p.name,
      version: p.version,
      enabled: p.enabled,
      capabilities: p.capabilities ? [...p.capabilities] : undefined,
    }));
  }

  listYaocai(): ProviderMeta[] {
    return Array.from(this.yaocaiProviders.values()).map((p) => ({
      id: p.id,
      name: p.name,
      version: p.version,
      enabled: p.enabled,
      capabilities: p.capabilities ? [...p.capabilities] : undefined,
    }));
  }

  getDefaultFangjiId(): string | null {
    return this.defaultFangjiId ?? null;
  }

  getDefaultYaocaiId(): string | null {
    return this.defaultYaocaiId ?? null;
  }

  clear(): void {
    this.fangjiProviders.clear();
    this.yaocaiProviders.clear();
    this.defaultFangjiId = undefined;
    this.defaultYaocaiId = undefined;
  }
}
