package pipeline

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// shanghanlunFormulas is the curated list of 伤寒论 prescriptions. A chunk's
// `formula` field is filled with every name that appears in its text.
var shanghanlunFormulas = []string{
	"桂枝汤", "桂枝加桂汤", "桂枝加芍药汤", "桂枝加大黄汤", "桂枝加附子汤",
	"桂枝加葛根汤", "桂枝加厚朴杏子汤", "桂枝加龙骨牡蛎汤", "桂枝去芍药汤",
	"桂枝去芍药加附子汤", "桂枝去芍药加蜀漆牡蛎龙骨救逆汤", "桂枝去桂加茯苓白术汤",
	"桂枝新加汤", "桂枝甘草汤", "桂枝甘草龙骨牡蛎汤", "桂枝麻黄各半汤",
	"桂枝二麻黄一汤", "桂枝二越婢一汤", "桂枝附子汤", "桂枝人参汤",
	"麻黄汤", "麻黄杏仁甘草石膏汤", "麻黄细辛附子汤", "麻黄附子甘草汤",
	"麻黄连轺赤小豆汤", "麻黄升麻汤", "大青龙汤", "小青龙汤",
	"葛根汤", "葛根加半夏汤", "葛根黄芩黄连汤",
	"小柴胡汤", "大柴胡汤", "柴胡桂枝汤", "柴胡桂枝干姜汤",
	"柴胡加龙骨牡蛎汤", "柴胡加芒硝汤", "四逆散",
	"大承气汤", "小承气汤", "调胃承气汤", "桃核承气汤", "麻子仁丸",
	"栀子豉汤", "栀子甘草豉汤", "栀子生姜豉汤", "栀子厚朴汤",
	"栀子干姜汤", "枳实栀子豉汤",
	"半夏泻心汤", "生姜泻心汤", "甘草泻心汤", "大黄黄连泻心汤",
	"附子泻心汤", "黄连汤", "黄芩汤", "黄芩加半夏生姜汤",
	"干姜黄芩黄连人参汤",
	"白虎汤", "白虎加人参汤", "竹叶石膏汤",
	"五苓散", "猪苓汤", "茯苓甘草汤", "茯苓桂枝白术甘草汤",
	"茯苓桂枝甘草大枣汤", "苓桂术甘汤", "文蛤散", "牡蛎泽泻散",
	"大陷胸汤", "大陷胸丸", "小陷胸汤", "三物白散", "白散",
	"四逆汤", "通脉四逆汤", "通脉四逆加猪胆汤", "四逆加人参汤",
	"茯苓四逆汤", "干姜附子汤", "白通汤", "白通加猪胆汁汤",
	"附子汤", "真武汤", "甘草干姜汤", "芍药甘草汤", "芍药甘草附子汤",
	"理中丸", "理中汤",
	"吴茱萸汤", "厚朴生姜半夏甘草人参汤", "旋覆代赭汤",
	"抵当汤", "抵当丸", "十枣汤", "瓜蒂散", "赤石脂禹余粮汤",
	"甘草汤", "桔梗汤", "苦酒汤", "半夏散及汤", "猪肤汤",
	"黄连阿胶汤", "桃花汤", "乌梅丸", "当归四逆汤",
	"当归四逆加吴茱萸生姜汤", "白头翁汤", "烧裈散",
}

// DetectFormulas returns the sorted, de-duplicated formula names found in text.
func DetectFormulas(text string) []string {
	var found []string
	for _, f := range shanghanlunFormulas {
		if strings.Contains(text, f) {
			found = append(found, f)
		}
	}
	sort.Strings(found)
	return found
}

var reClause = regexp.MustCompile(`^\s*([0-9]{1,3}|[一二三四五六七八九十百]{1,4})\s*[、.，,:：]`)

// detectClause is a best-effort extraction of a 条文 number. This annotated,
// scanned edition does not number every 条文, so -1 is returned when unknown.
func detectClause(text string) int64 {
	m := reClause.FindStringSubmatch(text)
	if m == nil {
		return -1
	}
	n, err := parseNumber(m[1])
	if err != nil {
		return -1
	}
	return n
}

var cnDigits = map[rune]int{
	'零': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4,
	'五': 5, '六': 6, '七': 7, '八': 8, '九': 9,
}

func parseNumber(s string) (int64, error) {
	if n, err := strconv.Atoi(s); err == nil {
		return int64(n), nil
	}

	runes := []rune(s)
	if len(runes) == 1 {
		if d, ok := cnDigits[runes[0]]; ok {
			return int64(d), nil
		}
		if runes[0] == '十' {
			return 10, nil
		}
	}

	var total, section int
	for _, r := range runes {
		switch {
		case r == '十':
			if section == 0 {
				section = 1
			}
			total += section * 10
			section = 0
		case r == '百':
			if section == 0 {
				section = 1
			}
			total += section * 100
			section = 0
		default:
			d, ok := cnDigits[r]
			if !ok {
				return 0, strconv.ErrSyntax
			}
			section = d
		}
	}
	return int64(total + section), nil
}
