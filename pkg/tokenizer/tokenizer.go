// Package tokenizer 提供无需联网的本地 Token 估算算法。
//
// 用于流式响应（SSE）时实时估算输出 Token 数量，在前端展示成本趋势。
// 注意：这只是展示成本趋势，精确计费仍依靠上游 API 返回的 usage 字段。
package tokenizer

import (
	"strings"
	"unicode"
)

// EstimateTokens 混合近似计数（极速，无字典加载）。
//
// 算法：
//   - ASCII 字符按约 1 token / 4 字符估算（贴近 GPT 对英文的编码密度）
//   - 中文/非 ASCII rune 按 ~2 token 估算
//   - 连续英文长词（>6）加权 1.2 倍
//   - 最终映射到近似 tiktoken 数量级
//
// 该结果用于前端展示成本趋势的相对值，非精确计费。
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	runes := []rune(text)
	asciiCount := 0
	nonASCIICount := 0

	// 识别长英文单词，做加权
	longWordBoost := 0
	var wordLen int
	for _, r := range runes {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
			wordLen++
			continue
		}
		if wordLen > 6 {
			longWordBoost += wordLen / 6
		}
		wordLen = 0
	}
	if wordLen > 6 {
		longWordBoost += wordLen / 6
	}

	for _, r := range runes {
		if r < 128 {
			asciiCount++
		} else {
			nonASCIICount++
		}
	}

	// 英文/数字约 0.25 token/字符（4 字符 ~ 1 token）
	asciiTokens := asciiCount / 4
	// 中文约 2 token/字
	nonASCIITokens := nonASCIICount * 2
	// 长词加权
	total := asciiTokens + nonASCIITokens + longWordBoost
	if total < 1 && len(runes) > 0 {
		total = 1
	}
	return total
}

// EstimateTokensFromDeltas 累加多个流式分片的 Token 估算。
func EstimateTokensFromDeltas(deltas []string) int {
	joined := strings.Join(deltas, "")
	return EstimateTokens(joined)
}
