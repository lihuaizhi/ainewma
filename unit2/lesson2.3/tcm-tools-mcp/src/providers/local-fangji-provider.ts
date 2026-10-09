import type { Fangji } from "../domain/fangji.js";
import { loadJson } from "../utils/io.js";
import {
  buildHaystack,
  containsMatch,
  normalizeText,
  scoreRelevance,
} from "../utils/search.js";
import type { IFangjiProvider, FangjiSearchOpts } from "./fangji-provider.js";
import type { SearchResult } from "./base.js";

export interface LocalFangjiProviderOptions {
  id: string;
  name: string;
  enabled: boolean;
  dataDir?: string;
  version?: string;
}

export class LocalFangjiProvider implements IFangjiProvider {
  id: string;
  name: string;
  version: string;
  enabled: boolean;
  capabilities: string[] = ["search", "getById", "getByName"];
  private dataDir?: string;
  private data: Fangji[] = [];

  constructor(opts: LocalFangjiProviderOptions) {
    this.id = opts.id;
    this.name = opts.name;
    this.enabled = opts.enabled;
    this.dataDir = opts.dataDir;
    this.version = opts.version ?? "0.1.0";
  }

  async init(): Promise<void> {
    const loaded = await loadJson<Fangji[]>("fangji.json", this.dataDir, []);
    const map = new Map<string, Fangji>();
    for (const item of loaded) {
      if (item && item.id) {
        map.set(item.id, item);
      }
    }
    this.data = Array.from(map.values());
  }

  async health(): Promise<boolean> {
    return true;
  }

  async dispose(): Promise<void> {
    this.data = [];
  }

  async search(opts: FangjiSearchOpts): Promise<SearchResult<Fangji>> {
    const {
      query,
      herbName,
      herbId,
      source,
      category,
      exact = false,
      fuzzy = true,
      page = 1,
      limit = 10,
      sortBy = "relevance",
      sortOrder = "asc",
    } = opts;

    let list: Fangji[] = [...this.data];

    if (herbName && herbName.trim().length > 0) {
      const hn = normalizeText(herbName);
      list = list.filter((f) =>
        (f.composition ?? []).some((c) =>
          containsMatch(normalizeText(c.herbName), hn)
        )
      );
    }

    if (herbId && herbId.trim().length > 0) {
      const hid = herbId.trim();
      list = list.filter((f) =>
        (f.composition ?? []).some((c) => c.herbId === hid)
      );
    }

    if (source && source.trim().length > 0) {
      const s = normalizeText(source);
      list = list.filter((f) => containsMatch(normalizeText(f.source), s));
    }

    if (category && category.trim().length > 0) {
      const c = normalizeText(category);
      list = list.filter((f) => containsMatch(normalizeText(f.category), c));
    }

    if (query && query.trim().length > 0) {
      const q = query.trim();
      const nq = normalizeText(q);

      if (exact) {
        list = list.filter((f) => {
          const nameN = normalizeText(f.name);
          if (nameN === nq) return true;
          const aliases = f.aliases ?? [];
          return aliases.some((a) => normalizeText(a) === nq);
        });
      } else {
        list = list.filter((f) => {
          const haystack = buildHaystack([
            f.name,
            f.aliases,
            f.pinyin,
            f.initials,
            f.source,
            f.category,
            f.efficacy,
            f.indication,
            f.notes,
            f.tags,
          ]);
          return containsMatch(haystack, nq);
        });
        void fuzzy;
      }
    }

    const sorted = [...list].sort((a, b) => {
      let cmp = 0;

      if (sortBy === "relevance" && query && query.trim().length > 0) {
        const qn = normalizeText(query);
        const ha = buildHaystack([
          a.name,
          a.aliases,
          a.pinyin,
          a.initials,
          a.source,
          a.efficacy,
          a.indication,
          a.tags,
        ]);
        const hb = buildHaystack([
          b.name,
          b.aliases,
          b.pinyin,
          b.initials,
          b.source,
          b.efficacy,
          b.indication,
          b.tags,
        ]);
        const sa = scoreRelevance(ha, qn);
        const sb = scoreRelevance(hb, qn);
        cmp = sb - sa;
      } else if (sortBy === "name") {
        const na = normalizeText(a.name);
        const nb = normalizeText(b.name);
        cmp = na.localeCompare(nb, "zh-Hans-CN", { numeric: true });
      } else if (sortBy === "source") {
        const saSrc = normalizeText(a.source);
        const sbSrc = normalizeText(b.source);
        cmp = saSrc.localeCompare(sbSrc, "zh-Hans-CN");
        if (cmp === 0) {
          const na = normalizeText(a.name);
          const nb = normalizeText(b.name);
          cmp = na.localeCompare(nb, "zh-Hans-CN");
        }
      }

      if (cmp === 0) {
        const na2 = normalizeText(a.name);
        const nb2 = normalizeText(b.name);
        cmp = na2.localeCompare(nb2, "zh-Hans-CN");
      }

      return sortOrder === "desc" ? -cmp : cmp;
    });

    const total = sorted.length;
    const safePage = Math.max(1, page);
    const safeLimit = Math.min(50, Math.max(1, limit));
    const start = (safePage - 1) * safeLimit;
    const end = start + safeLimit;
    const items = sorted.slice(start, end);
    const hasMore = end < total;

    return {
      items,
      total,
      page: safePage,
      limit: safeLimit,
      hasMore,
    };
  }

  async getById(id: string): Promise<Fangji | null> {
    if (!id || id.trim().length === 0) return null;
    const target = id.trim();
    const found = this.data.find((f) => f.id === target);
    return found ?? null;
  }

  async getByName(name: string): Promise<Fangji | null> {
    if (!name || name.trim().length === 0) return null;
    const n = normalizeText(name);
    const found = this.data.find((f) => {
      const nameN = normalizeText(f.name);
      if (nameN === n) return true;
      const aliases = f.aliases ?? [];
      return aliases.some((a) => normalizeText(a) === n);
    });
    return found ?? null;
  }
}
