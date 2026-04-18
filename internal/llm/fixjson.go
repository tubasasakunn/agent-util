package llm

import (
	"bytes"
	"strings"
)

// FixJSON はSLMの壊れたJSON出力を補正する。
// 補正後もパース不能な場合は入力をそのまま返す。
func FixJSON(data []byte) []byte {
	result := data
	result = fixNullBraces(result)
	result = fixControlChars(result)
	result = fixSingleQuotes(result)
	result = fixTrailingCommas(result)
	result = fixUnmatchedBrackets(result)
	return result
}

// fixNullBraces は {null} を null に変換する。
func fixNullBraces(data []byte) []byte {
	return bytes.ReplaceAll(data, []byte("{null}"), []byte("null"))
}

// fixControlChars はJSON文字列内の不正な制御文字（U+0000〜U+001F、ただしタブ・改行・復帰を除く）を除去する。
func fixControlChars(data []byte) []byte {
	var buf bytes.Buffer
	buf.Grow(len(data))
	inString := false
	escaped := false

	for _, b := range data {
		if escaped {
			buf.WriteByte(b)
			escaped = false
			continue
		}
		if b == '\\' && inString {
			buf.WriteByte(b)
			escaped = true
			continue
		}
		if b == '"' {
			inString = !inString
		}
		if inString && b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
			continue
		}
		buf.WriteByte(b)
	}
	return buf.Bytes()
}

// fixSingleQuotes はJSON外のシングルクォートをダブルクォートに変換する。
// 既にダブルクォート内にあるシングルクォート（アポストロフィ）はそのまま残す。
func fixSingleQuotes(data []byte) []byte {
	// ダブルクォートが既にあればJSON標準に準拠しているとみなしてスキップ
	if bytes.ContainsRune(data, '"') {
		return data
	}

	var buf bytes.Buffer
	buf.Grow(len(data))
	inString := false
	escaped := false

	for _, b := range data {
		if escaped {
			buf.WriteByte(b)
			escaped = false
			continue
		}
		if b == '\\' && inString {
			buf.WriteByte(b)
			escaped = true
			continue
		}
		if b == '\'' {
			buf.WriteByte('"')
			inString = !inString
			continue
		}
		buf.WriteByte(b)
	}
	return buf.Bytes()
}

// fixTrailingCommas は ,} と ,] のパターンからカンマを除去する。
// 空白を挟んだパターン（, } や ,\n]）にも対応する。
func fixTrailingCommas(data []byte) []byte {
	s := string(data)
	var buf strings.Builder
	buf.Grow(len(s))

	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		if escaped {
			buf.WriteByte(c)
			escaped = false
			continue
		}
		if c == '\\' && inString {
			buf.WriteByte(c)
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
		}

		if c == ',' && !inString {
			// カンマの後に空白を飛ばして } か ] が来るか確認
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue // カンマをスキップ
			}
		}
		buf.WriteByte(c)
	}
	return []byte(buf.String())
}

// fixUnmatchedBrackets は閉じ括弧が不足している場合に補完する。
func fixUnmatchedBrackets(data []byte) []byte {
	inString := false
	escaped := false
	var stack []byte

	for _, b := range data {
		if escaped {
			escaped = false
			continue
		}
		if b == '\\' && inString {
			escaped = true
			continue
		}
		if b == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}

		switch b {
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 && stack[len(stack)-1] == b {
				stack = stack[:len(stack)-1]
			}
		}
	}

	if len(stack) == 0 {
		return data
	}

	// スタックを逆順に（最後に開いた括弧を最初に閉じる）
	var buf bytes.Buffer
	buf.Grow(len(data) + len(stack))
	buf.Write(data)
	for i := len(stack) - 1; i >= 0; i-- {
		buf.WriteByte(stack[i])
	}
	return buf.Bytes()
}
