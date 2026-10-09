import type { Yaocai, YaocaiSourceType } from "../domain/yaocai.js";
import type { BaseProvider, SearchResult } from "./base.js";
export interface YaocaiSearchOpts {
    /** 关键词（药材名/别名/拼音/首字母/拉丁/功效/主治/标签） */
    query?: string;
    /** 性味过滤（支持部分匹配，如 "辛温"、"甘平"） */
    propertyFlavor?: string;
    /** 归经过滤（支持字符串或字符串数组） */
    meridian?: string | string[];
    /** 功效关键词过滤 */
    efficacy?: string;
    /** 主治关键词过滤 */
    indication?: string;
    /** 药材来源类型 */
    sourceType?: YaocaiSourceType;
    /** 精确匹配 */
    exact?: boolean;
    /** 模糊匹配（默认 true） */
    fuzzy?: boolean;
    /** 分页：页码（1-based） */
    page?: number;
    /** 每页数量（默认 10，建议 ≤ 50） */
    limit?: number;
    /** 排序字段 */
    sortBy?: "name" | "pinyin" | "relevance";
    /** 升降序 */
    sortOrder?: "asc" | "desc";
}
export interface IYaocaiProvider extends BaseProvider {
    /** 搜索药材 */
    search(opts: YaocaiSearchOpts): Promise<SearchResult<Yaocai>>;
    /** 按 ID 获取药材详情 */
    getById(id: string): Promise<Yaocai | null>;
    /** 按名称获取药材详情（支持别名） */
    getByName(name: string): Promise<Yaocai | null>;
}
//# sourceMappingURL=yaocai-provider.d.ts.map