export class ProviderRegistry {
    fangjiProviders = new Map();
    yaocaiProviders = new Map();
    defaultFangjiId;
    defaultYaocaiId;
    registerFangji(p) {
        if (!p)
            return;
        if (p.enabled) {
            this.fangjiProviders.set(p.id, p);
        }
        if (!this.defaultFangjiId && p.enabled) {
            this.defaultFangjiId = p.id;
        }
        if (this.defaultFangjiId &&
            (!this.fangjiProviders.has(this.defaultFangjiId) ||
                !this.fangjiProviders.get(this.defaultFangjiId)?.enabled)) {
            this.defaultFangjiId = this.getFirstEnabledFangjiId();
        }
    }
    registerYaocai(p) {
        if (!p)
            return;
        if (p.enabled) {
            this.yaocaiProviders.set(p.id, p);
        }
        if (!this.defaultYaocaiId && p.enabled) {
            this.defaultYaocaiId = p.id;
        }
        if (this.defaultYaocaiId &&
            (!this.yaocaiProviders.has(this.defaultYaocaiId) ||
                !this.yaocaiProviders.get(this.defaultYaocaiId)?.enabled)) {
            this.defaultYaocaiId = this.getFirstEnabledYaocaiId();
        }
    }
    getFirstEnabledFangjiId() {
        for (const [id, p] of this.fangjiProviders) {
            if (p.enabled)
                return id;
        }
        return undefined;
    }
    getFirstEnabledYaocaiId() {
        for (const [id, p] of this.yaocaiProviders) {
            if (p.enabled)
                return id;
        }
        return undefined;
    }
    setDefaultFangji(id) {
        if (id && this.fangjiProviders.has(id)) {
            const p = this.fangjiProviders.get(id);
            if (p?.enabled) {
                this.defaultFangjiId = id;
            }
        }
    }
    setDefaultYaocai(id) {
        if (id && this.yaocaiProviders.has(id)) {
            const p = this.yaocaiProviders.get(id);
            if (p?.enabled) {
                this.defaultYaocaiId = id;
            }
        }
    }
    getFangji(id) {
        const targetId = id ?? this.defaultFangjiId;
        if (!targetId)
            return undefined;
        const p = this.fangjiProviders.get(targetId);
        return p?.enabled ? p : undefined;
    }
    getYaocai(id) {
        const targetId = id ?? this.defaultYaocaiId;
        if (!targetId)
            return undefined;
        const p = this.yaocaiProviders.get(targetId);
        return p?.enabled ? p : undefined;
    }
    listFangji() {
        return Array.from(this.fangjiProviders.values()).map((p) => ({
            id: p.id,
            name: p.name,
            version: p.version,
            enabled: p.enabled,
            capabilities: p.capabilities ? [...p.capabilities] : undefined,
        }));
    }
    listYaocai() {
        return Array.from(this.yaocaiProviders.values()).map((p) => ({
            id: p.id,
            name: p.name,
            version: p.version,
            enabled: p.enabled,
            capabilities: p.capabilities ? [...p.capabilities] : undefined,
        }));
    }
    getDefaultFangjiId() {
        return this.defaultFangjiId ?? null;
    }
    getDefaultYaocaiId() {
        return this.defaultYaocaiId ?? null;
    }
    clear() {
        this.fangjiProviders.clear();
        this.yaocaiProviders.clear();
        this.defaultFangjiId = undefined;
        this.defaultYaocaiId = undefined;
    }
}
//# sourceMappingURL=registry.js.map