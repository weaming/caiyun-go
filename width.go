package main

import "strings"

// wideRanges 是 East Asian Wide / Fullwidth 以及 emoji 的主要区间
var wideRanges = [][2]rune{
	{0x1100, 0x115F},   // 韩文字母
	{0x2E80, 0x303E},   // CJK 部首、标点
	{0x3041, 0x33FF},   // 假名、CJK 兼容
	{0x3400, 0x4DBF},   // CJK 扩展 A
	{0x4E00, 0x9FFF},   // CJK 基本区
	{0xA000, 0xA4CF},   // 彝文
	{0xAC00, 0xD7A3},   // 韩文音节
	{0xF900, 0xFAFF},   // CJK 兼容表意
	{0xFE30, 0xFE6F},   // CJK 兼容形式
	{0xFF00, 0xFF60},   // 全角 ASCII
	{0xFFE0, 0xFFE6},   // 全角符号
	{0x1F300, 0x1FAFF}, // emoji
	{0x20000, 0x3FFFD}, // CJK 扩展 B 及以上
}

func runeCells(r rune) int {
	if r == 0 || r == '\n' || r == '\r' || (r >= 0x7f && r < 0xa0) || (r < 0x20) {
		return 0
	}
	for _, span := range wideRanges {
		if r >= span[0] && r <= span[1] {
			return 2
		}
	}
	return 1
}

// maxLineCells 返回文本里最长一行的显示宽度（等宽字体的一格）
func maxLineCells(text string) int {
	max := 0
	for _, line := range strings.Split(text, "\n") {
		cells := 0
		for _, r := range line {
			cells += runeCells(r)
		}
		if cells > max {
			max = cells
		}
	}
	return max
}
