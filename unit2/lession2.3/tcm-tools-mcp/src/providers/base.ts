export interface ProviderMeta {
  /** Provider 唯一标识（如 "local", "tcmsp", "etcm"） */
  id: string;
  /** 展示名称 */
  name: string;
  /** 版本 */
  version?: string;
  /** 是否启用 */
  enabled: boolean;
  /** 支持的能力 */
  capabilities?: string[];
}

export interface Pagination {
  /** 页码（1-based） */
  page: number;
  /** 每页数量 */
  limit: number;
}

export interface SearchResult<T> {
  /** 查询结果列表 */
  items: T[];
  /** 总数 */
  total: number;
  /** 当前页码（1-based） */
  page: number;
  /** 每页数量 */
  limit: number;
  /** 是否还有更多 */
  hasMore?: boolean;
}

export interface BaseProvider extends ProviderMeta {
  /** 初始化（可加载缓存/建立连接等） */
  init?(): Promise<void>;
  /** 健康检查 */
  health?(): Promise<boolean>;
  /** 销毁资源 */
  dispose?(): Promise<void>;
}
