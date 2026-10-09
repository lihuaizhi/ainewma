export interface FangjiCompositionItem {
    /** 药材名称 */
    herbName: string;
    /** 药材 ID（可选，用于关联 Yaocai） */
    herbId?: string;
    /** 剂量（原文，如 "9g"、"三钱"、"4枚"） */
    dosage?: string;
    /** 炮制方式（如 "生用"、"炒"、"去皮"、"炙用"） */
    processing?: string;
}
export interface Fangji {
    /** 唯一 ID */
    id: string;
    /** 方剂名（正名） */
    name: string;
    /** 别名（如 "桂枝汤类"、"太阳汤"） */
    aliases?: string[];
    /** 拼音（全拼，用于检索） */
    pinyin?: string;
    /** 首字母（简拼，用于检索） */
    initials?: string;
    /** 出处（如 《伤寒论》、金匮要略、仙授理伤续断秘方） */
    source?: string;
    /** 方剂分类（解表剂、泻下剂、补益剂、和解剂、理气剂...） */
    category?: string;
    /** 功效 */
    efficacy?: string;
    /** 主治（适应证） */
    indication?: string;
    /** 药物组成 */
    composition: FangjiCompositionItem[];
    /** 用法用量 */
    usage?: string;
    /** 禁忌/注意 */
    contraindication?: string;
    /** 方解/备注 */
    notes?: string;
    /** 参考文献/标签 */
    tags?: string[];
    /** 数据来源标识（local/tcmsp/etcm/...） */
    providerId?: string;
}
//# sourceMappingURL=fangji.d.ts.map