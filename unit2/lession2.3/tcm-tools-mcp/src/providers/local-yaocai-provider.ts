import type { Yaocai } from "../domain/yaocai.js";
import { loadJson } from "../utils/io.js";
import {
  buildHaystack,
  containsMatch,
  normalizeText,
  scoreRelevance,
  toStringArray,
} from "../utils/search.js";
import type { IYaocaiProvider, YaocaiSearchOpts } from "./yaocai-provider.js";
import type { SearchResult } from "./base.js";

export interface LocalYaocaiProviderOptions {
  id: string;
  name: string;
  enabled: boolean;
  dataDir?: string;
  version?: string;
}

export class LocalYaocaiProvider implements IYaocaiProvider {
  id: string;
  name: string;
  version: string;
  enabled: boolean;
  capabilities: string[] = ["search", "getById", "getByName"];
  private dataDir?: string;
  private data: Yaocai[] = [];

  constructor(opts: LocalYaocaiProviderOptions) {
    this.id = opts.id;
    this.name = opts.name;
    this.enabled = opts.enabled;
    this.dataDir = opts.dataDir;
    this.version = opts.version ?? "0.1.0";
  }

  async init(): Promise<void> {
    const loaded = await loadJson<Yaocai[]>("yaocai.json", this.dataDir, []);
    const map = new Map<string, Yaocai>();
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

  async search(opts: YaocaiSearchOpts): Promise<SearchResult<Yaocai>> {
    const {
      query,
      propertyFlavor,
      meridian,
      efficacy,
      indication,
      sourceType,
      exact = false,
      fuzzy = true,
      page = 1,
      limit = 10,
      sortBy = "relevance",
      sortOrder = "asc",
    } = opts;

    let list: Yaocai[] = [...this.data];

    if (propertyFlavor && propertyFlavor.trim().length > 0) {
      const pf = normalizeText(propertyFlavor);
      list = list.filter((y) =>
        containsMatch(normalizeText(y.propertyFlavor), pf)
      );
    }

    if (meridian) {
      const mArr = toStringArray(meridian).map((m) => normalizeText(m));
      if (mArr.length > 0) {
        list = list.filter((y) => {
          const yM = toStringArray(y.meridian).map((m) => normalizeText(m));
          return mArr.some((mNeed) =>
            yM.some((mHas) => containsMatch(mHas, mNeed) || mHas === mNeed)
          );
        });
      }
    }

    if (efficacy && efficacy.trim().length > 0) {
      const ef = normalizeText(efficacy);
      list = list.filter((y) => containsMatch(normalizeText(y.efficacy), ef));
    }

    if (indication && indication.trim().length > 0) {
      const ind = normalizeText(indication);
      list = list.filter((y) =>
        containsMatch(normalizeText(y.indication), ind)
      );
    }

    if (sourceType) {
      list = list.filter((y) => y.sourceType === sourceType);
    }

    if (query && query.trim().length > 0) {
      const q = query.trim();
      const nq = normalizeText(q);

      if (exact) {
        list = list.filter((y) => {
          const nameN = normalizeText(y.name);
          if (nameN === nq) return true;
          const aliases = y.aliases ?? [];
          return aliases.some((a) => normalizeText(a) === nq);
        });
      } else {
        list = list.filter((y) => {
          const haystack = buildHaystack([
            y.name,
            y.aliases,
            y.pinyin,
            y.initials,
            y.latinName,
            y.propertyFlavor,
            y.meridian,
            y.efficacy,
            y.indication,
            y.origin,
            y.notes,
            y.tags,
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
          a.latinName,
          a.efficacy,
          a.indication,
          a.tags,
        ]);
        const hb = buildHaystack([
          b.name,
          b.aliases,
          b.pinyin,
          b.initials,
          b.latinName,
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
      } else if (sortBy === "pinyin") {
        const pa = normalizeText(a.pinyin ?? a.name);
        const pb = normalizeText(b.pinyin ?? b.name);
        cmp = pa.localeCompare(pb, "zh-Hans-CN");
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

  async getById(id: string): Promise<Yaocai | null> {
    if (!id || id.trim().length === 0) return null;
    const target = id.trim();
    const found = this.data.find((y) => y.id === target);
    return found ?? null;
  }

  async getByName(name: string): Promise<Yaocai | null> {
    if (!name || name.trim().length === 0) return null;
    const n = normalizeText(name);
    const found = this.data.find((y) => {
      const nameN = normalizeText(y.name);
      if (nameN === n) return true;
      const aliases = y.aliases ?? [];
      return aliases.some((a) => normalizeText(a) === n);
    });
    return found ?? null;
  }
}
