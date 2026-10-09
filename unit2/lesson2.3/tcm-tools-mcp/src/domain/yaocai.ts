export type YaocaiSourceType = "plant" | "animal" | "mineral" | "other";

export interface Yaocai {
  /** 唯一 ID */
  id: string;
  /** 药材名（正名） */
  name: string;
  /** 别名 */
  aliases?: string[];
  /** 汉语拼音（全拼） */
  pinyin?: string;
  /** 首字母（简拼） */
  initials?: string;
  /** 拉丁学名 */
  latinName?: string;
  /** 性味（如 "辛、甘，温"、"苦，寒"） */
  propertyFlavor?: string;
  /** 归经（如 ["心经", "肺经", "膀胱经"]） */
  meridian?: string[];
  /** 功效 */
  efficacy?: string;
  /** 主治 */
  indication?: string;
  /** 用法用量（常用剂量范围，如 "3～9g"） */
  dosage?: string;
  /** 毒性（无毒/小毒/有毒） */
  toxicity?: string;
  /** 炮制常用方式 */
  processingCommon?: string[];
  /** 药材来源（植物/动物/矿物/其他） */
  sourceType?: YaocaiSourceType;
  /** 基原/出处 */
  origin?: string;
  /** 注意事项/禁忌 */
  caution?: string;
  /** 化学成分/备注（可选） */
  notes?: string;
  /** 标签 */
  tags?: string[];
  /** 数据来源 */
  providerId?: string;
}
