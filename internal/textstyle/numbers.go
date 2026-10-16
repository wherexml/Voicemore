package textstyle

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const chineseDigits = "零〇一二两兩三四五六七八九"
const chineseNumber = chineseDigits + "十百千万萬亿億"

var numberSpan = regexp.MustCompile(`[负負]?[` + chineseNumber + `]+(?:[点點][` + chineseDigits + `]+)?`)
var percentSpan = regexp.MustCompile(`百分之([负負]?[` + chineseNumber + `]+(?:[点點][` + chineseDigits + `]+)?)`)
var clockSpan = regexp.MustCompile(`([` + chineseDigits + `十]+)[点點]([` + chineseDigits + `十]+)分(?:钟|鐘)?`)

func digit(r rune) (int64, bool) {
	for i, c := range []rune("零一二三四五六七八九") {
		if r == c {
			return int64(i), true
		}
	}
	if r == '〇' {
		return 0, true
	}
	if r == '两' || r == '兩' {
		return 2, true
	}
	return 0, false
}

// Reject abbreviated quantities (一百二) rather than guessing their magnitude.
func integerNumber(s string) (string, bool) {
	var digits strings.Builder
	allDigits := true
	for _, r := range s {
		if n, ok := digit(r); ok {
			digits.WriteByte(byte('0' + n))
		} else {
			allDigits = false
		}
	}
	if allDigits {
		return digits.String(), s != ""
	}
	var total, pending int64
	previousUnit := int64(10000)
	hasPending, zero := false, false
	for _, r := range s {
		if n, ok := digit(r); ok {
			if n == 0 {
				zero = true
				continue
			}
			if hasPending {
				return "", false
			}
			pending, hasPending = n, true
			continue
		}
		unit := map[rune]int64{'十': 10, '百': 100, '千': 1000}[r]
		if unit == 0 || unit >= previousUnit {
			return "", false
		}
		if !hasPending {
			if unit != 10 || total != 0 {
				return "", false
			}
			pending = 1
		}
		total += pending * unit
		pending, hasPending, zero, previousUnit = 0, false, false, unit
	}
	if hasPending && previousUnit >= 100 && !zero {
		return "", false
	}
	return strconv.FormatInt(total+pending, 10), true
}

func numericValue(s string) (string, bool) {
	sign := ""
	if strings.HasPrefix(s, "负") || strings.HasPrefix(s, "負") {
		sign = "-"
		s = string([]rune(s)[1:])
	}
	// Keep familiar large-unit notation: 十五万 → 15万, not a guessed shorthand.
	unit := ""
	r := []rune(s)
	if len(r) > 1 && strings.ContainsRune("万萬亿億", r[len(r)-1]) {
		unit = string(r[len(r)-1])
		s = string(r[:len(r)-1])
	}
	pieces := strings.FieldsFunc(s, func(r rune) bool { return r == '点' || r == '點' })
	if len(pieces) < 1 || len(pieces) > 2 {
		return "", false
	}
	i, ok := integerNumber(pieces[0])
	if !ok {
		return "", false
	}
	if len(pieces) == 2 {
		var fraction strings.Builder
		for _, r := range pieces[1] {
			n, ok := digit(r)
			if !ok {
				return "", false
			}
			fraction.WriteByte(byte('0' + n))
		}
		i += "." + fraction.String()
	}
	return sign + i + unit, true
}

func startsAny(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func arabicNumbers(t string) string {
	t = percentSpan.ReplaceAllStringFunc(t, func(s string) string {
		n, ok := numericValue(strings.TrimPrefix(s, "百分之"))
		if ok && !strings.ContainsAny(n, "万萬亿億") {
			return n + "%"
		}
		return s
	})
	t = clockSpan.ReplaceAllStringFunc(t, func(s string) string {
		if strings.HasSuffix(s, "分钟") || strings.HasSuffix(s, "分鐘") {
			return s
		}
		m := clockSpan.FindStringSubmatch(s)
		h, ok := integerNumber(m[1])
		min, ok2 := integerNumber(m[2])
		hn, _ := strconv.Atoi(h)
		mn, _ := strconv.Atoi(min)
		if ok && ok2 && hn <= 24 && mn < 60 {
			return h + "点" + min + "分"
		}
		return s
	})
	var b strings.Builder
	pos := 0
	for _, m := range numberSpan.FindAllStringIndex(t, -1) {
		b.WriteString(t[pos:m[0]])
		s := t[m[0]:m[1]]
		before := t[:m[0]]
		after := t[m[1]:]
		n, ok := numericValue(s)
		prev := []rune(before)
		next := []rune(after)
		left := len(prev) == 0 || boundary(prev[len(prev)-1]) || unicode.IsSpace(prev[len(prev)-1])
		right := len(next) == 0 || boundary(next[0]) || unicode.IsSpace(next[0])
		suffix := startsAny(after, "年", "月", "日", "号", "號", "个", "個", "人", "次", "秒", "分钟", "分鐘", "小时", "小時", "天", "周", "岁", "歲", "元", "块", "塊", "米", "公里", "公斤", "克", "吨", "噸", "度", "倍", "项", "項", "条", "條", "页", "頁", "台", "份", "张", "張", "件", "套", "万", "萬", "亿", "億")
		if startsAny(after, "点", "點") && (s != "一" || strings.HasSuffix(before, "上午") || strings.HasSuffix(before, "下午") || strings.HasSuffix(before, "凌晨") || strings.HasSuffix(before, "中午") || strings.HasSuffix(before, "晚上")) {
			suffix = true
		}
		prefix := strings.HasSuffix(before, "第") || strings.HasSuffix(before, "编号") || strings.HasSuffix(before, "編號") || strings.HasSuffix(before, "手机号") || strings.HasSuffix(before, "電話") || strings.HasSuffix(before, "电话")
		rangeStart := false
		if startsAny(after, "到", "至") {
			loc := numberSpan.FindStringIndex(string([]rune(after)[1:]))
			rangeStart = loc != nil && loc[0] == 0
		}
		longDigits := len([]rune(s)) >= 3 && !strings.ContainsAny(s, "十百千万萬亿億两兩")
		decimal := strings.ContainsAny(s, "点點")
		allow := suffix || prefix || rangeStart || longDigits || decimal || left && right
		// Keep singular classifier expressions in Chinese (including 第一次).
		// This changes notation only: only/each/count qualifiers remain intact,
		// and explicit Arabic input, larger counts, prices and dates are untouched.
		if s == "一" && startsAny(after, "个", "個", "项", "項", "条", "條", "份", "张", "張", "件", "套", "台", "次", "遍", "回") {
			allow = false
		}
		// Lexical phrases and ambiguous short money expressions are not quantities.
		for _, phrase := range []string{"一年一度", "三天两头", "三天兩頭", "五一劳动节", "五一勞動節"} {
			for offset := 0; offset < len(t); {
				found := strings.Index(t[offset:], phrase)
				if found < 0 {
					break
				}
				start := offset + found
				if m[0] < start+len(phrase) && m[1] > start {
					allow = false
				}
				offset = start + len(phrase)
			}
		}
		if startsAny(after, "块", "塊") && len(next) > 1 && strings.ContainsRune(chineseNumber, next[1]) {
			allow = false
		}
		// Never rewrite a fragment of an existing identifier or mixed numeral.
		if len(prev) > 0 && (latin(prev[len(prev)-1]) || unicode.IsDigit(prev[len(prev)-1])) {
			allow = false
		}
		if len(next) > 0 && (latin(next[0]) || unicode.IsDigit(next[0])) {
			allow = false
		}
		if ok && allow {
			b.WriteString(n)
		} else {
			b.WriteString(s)
		}
		pos = m[1]
	}
	b.WriteString(t[pos:])
	return b.String()
}
