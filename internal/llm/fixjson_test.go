package llm

import "testing"

func TestFixJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// 正常なJSONはそのまま通す
		{name: "valid_json", input: `{"key": "value"}`, want: `{"key": "value"}`},
		{name: "valid_array", input: `[1, 2, 3]`, want: `[1, 2, 3]`},

		// {null} パターン (investigation/001 Test C)
		{name: "null_braces", input: `{"arguments": {null}}`, want: `{"arguments": null}`},
		{name: "null_braces_multiple", input: `{"a": {null}, "b": {null}}`, want: `{"a": null, "b": null}`},

		// シングルクォート
		{name: "single_quotes", input: `{'key': 'value'}`, want: `{"key": "value"}`},
		{name: "single_quotes_nested", input: `{'a': {'b': 'c'}}`, want: `{"a": {"b": "c"}}`},

		// 末尾カンマ
		{name: "trailing_comma_object", input: `{"a": 1, "b": 2,}`, want: `{"a": 1, "b": 2}`},
		{name: "trailing_comma_array", input: `[1, 2, 3,]`, want: `[1, 2, 3]`},
		{name: "trailing_comma_whitespace", input: `{"a": 1 , }`, want: `{"a": 1  }`},

		// 閉じ括弧の補完
		{name: "missing_close_brace", input: `{"key": "value"`, want: `{"key": "value"}`},
		{name: "missing_close_bracket", input: `[1, 2, 3`, want: `[1, 2, 3]`},
		{name: "missing_nested", input: `{"a": [1, 2`, want: `{"a": [1, 2]}`},

		// 制御文字
		{name: "null_byte", input: "{\"a\": \"hello\x00world\"}", want: `{"a": "helloworld"}`},

		// investigation/001の実際のレスポンスパターン
		{
			name:  "real_router_response",
			input: `{"tool": "none", "arguments": {null}, "reasoning": "テスト"}`,
			want:  `{"tool": "none", "arguments": null, "reasoning": "テスト"}`,
		},

		// 複合パターン
		{name: "combined_null_and_trailing", input: `{"a": {null},}`, want: `{"a": null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(FixJSON([]byte(tt.input)))
			if got != tt.want {
				t.Errorf("FixJSON(%q)\n  got:  %s\n  want: %s", tt.input, got, tt.want)
			}
		})
	}
}

func TestFixNullBraces(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "single", input: `{null}`, want: `null`},
		{name: "in_object", input: `{"a": {null}}`, want: `{"a": null}`},
		{name: "no_match", input: `{"a": null}`, want: `{"a": null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(fixNullBraces([]byte(tt.input)))
			if got != tt.want {
				t.Errorf("fixNullBraces(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFixSingleQuotes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "basic", input: `{'key': 'val'}`, want: `{"key": "val"}`},
		{name: "already_double", input: `{"key": "val"}`, want: `{"key": "val"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(fixSingleQuotes([]byte(tt.input)))
			if got != tt.want {
				t.Errorf("fixSingleQuotes(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFixTrailingCommas(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "object", input: `{"a": 1,}`, want: `{"a": 1}`},
		{name: "array", input: `[1,]`, want: `[1]`},
		{name: "with_space", input: `{"a": 1, }`, want: `{"a": 1 }`},
		{name: "no_trailing", input: `{"a": 1}`, want: `{"a": 1}`},
		{name: "comma_in_string", input: `{"a": "1,}"}`, want: `{"a": "1,}"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(fixTrailingCommas([]byte(tt.input)))
			if got != tt.want {
				t.Errorf("fixTrailingCommas(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFixUnmatchedBrackets(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "missing_brace", input: `{"a": 1`, want: `{"a": 1}`},
		{name: "missing_bracket", input: `[1, 2`, want: `[1, 2]`},
		{name: "missing_nested", input: `{"a": [1`, want: `{"a": [1]}`},
		{name: "balanced", input: `{"a": 1}`, want: `{"a": 1}`},
		{name: "bracket_in_string", input: `{"a": "{"}`, want: `{"a": "{"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(fixUnmatchedBrackets([]byte(tt.input)))
			if got != tt.want {
				t.Errorf("fixUnmatchedBrackets(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
