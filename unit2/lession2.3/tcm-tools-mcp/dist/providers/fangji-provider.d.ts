import type { Fangji } from "../domain/fangji.js";
import type { BaseProvider, SearchResult } from "./base.js";
export interface FangjiSearchOpts {
    /** 关键词（支持方剂名/别名/拼音/首字母/出处/主治/功效/标签） */
    query?: string;
    /** 按药材名过滤（方中包含该药材） */
    herbName?: string;
    /** 按药材ID过滤 */
    herbId?: string;
    /** 按出处过滤 */
    source?: string;
    /** 按分类过滤 */
    category?: string;
    /** 精确匹配名称 */
    exact?: boolean;
    /** 是否模糊匹配（默认 true） */
    fuzzy?: boolean;
    /** 分页：页码（1-based） */
    page?: number;
    /** 每页数量（默认 10，建议 ≤ 50） */
    limit?: number;
    /** 排序字段 */
    sortBy?: "name" | "source" | "relevance";
    /** 升降序 */
    sortOrder?: "asc" | "desc";
}
export interface IFangjiProvider extends BaseProvider {
    /** 搜索方剂 */
    search(opts: FangjiSearchOpts): Promise<SearchResult<Fangji>>;
    /** 按 ID 获取方剂详情 */
    getById(id: string): Promise<Fangji | null>;
    /** 按名称获取方剂详情（支持别名） */
    getByName(name: string): Promise<Fangji | null>;
}
//# sourceMappingURL=fangji-provider.d.ts.map