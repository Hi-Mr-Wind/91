package spider91migrate

import (
	"strings"
	"testing"
)

func TestSanitizeTitleHandlesCommonCases(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"hello", "hello"},
		{"  hello  ", "hello"},
		{"hello\nworld", "hello world"},
		{"hello / world", "hello world"}, // 单 forbidden 折叠成空格
		{"a/b\\c:d*e?f\"g<h>i|j", "a b c d e f g h i j"},
		{"a   b", "a b"}, // 多空格折叠
		{"a\t\nb", "a b"},
		{"...trim.dots...", "trim.dots"},           // 首尾点号被 trim 掉
		{"control\x01char\x1f\x7f", "controlchar"}, // 控制字符直接丢弃
		{"", "video"},                              // 空串回退
		{"  /  ", "video"},                         // 全是 forbidden+空白 → 回退
	}
	for _, c := range cases {
		got := sanitizeTitle(c.in)
		if got != c.want {
			t.Errorf("sanitizeTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeTitleTruncatesLongInputByRune(t *testing.T) {
	// 100 个中文字符（超过 80 上限）
	in := strings.Repeat("超", 100)
	got := sanitizeTitle(in)
	gotRunes := []rune(got)
	if len(gotRunes) != maxTitleRunes {
		t.Fatalf("rune count = %d, want %d", len(gotRunes), maxTitleRunes)
	}
}

func TestSanitizeTitleKeepsCJKAndUnicode(t *testing.T) {
	in := "超白大奶律师约炮第一季 (1080p)【高清】"
	got := sanitizeTitle(in)
	// 不应损失 CJK 或括号
	for _, r := range []rune{'超', '律', '约', '炮', '(', ')', '【', '】'} {
		if !strings.ContainsRune(got, r) {
			t.Errorf("sanitized %q lost rune %q", got, r)
		}
	}
}

func TestExtractViewKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"spider91-91Spider-476fa8bf4b47e672d2fa", "476fa8bf4b47e672d2fa"},
		{"spider91-91Spider-1587338723", "1587338723"},
		{"spider91-some-drive-with-dashes-vk001", "vk001"}, // LastIndex 拿尾段
		{"no-dashes-after-prefix", "prefix"},
		{"single", "single"}, // 没 dash → 原样返回
	}
	for _, c := range cases {
		if got := extractViewKey(c.in); got != c.want {
			t.Errorf("extractViewKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestViewKeySuffix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"476fa8bf4b47e672d2fa", "e672d2fa"},
		{"1587338723", "87338723"},
		{"short12", "short12"}, // 不足 8 字符 → 原样
		{"", ""},
		{"12345678", "12345678"}, // 等于 8 → 原样
		{"123456789", "23456789"},
	}
	for _, c := range cases {
		if got := viewKeySuffix(c.in); got != c.want {
			t.Errorf("viewKeySuffix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDesiredPikPakName(t *testing.T) {
	cases := []struct {
		title, viewkey, ext, want string
	}{
		{
			"超白大奶律师约炮第一季",
			"476fa8bf4b47e672d2fa",
			"mp4",
			"超白大奶律师约炮第一季-e672d2fa.mp4",
		},
		{
			"必看第三段！共三段",
			"1587338723",
			"mp4",
			"必看第三段！共三段-87338723.mp4",
		},
		{
			"with / forbidden : chars",
			"abcdefgh",
			"mp4",
			"with forbidden chars-abcdefgh.mp4",
		},
		{
			"", // 空标题
			"abcdefgh",
			"mp4",
			"video-abcdefgh.mp4",
		},
		{
			"title",
			"", // 空 viewkey → 退化成 "<title>.<ext>"
			"webm",
			"title.webm",
		},
		{
			"title",
			"abcdefgh",
			"", // 空 ext → 默认 mp4
			"title-abcdefgh.mp4",
		},
		{
			"title",
			"abcdefgh",
			".mp4", // 带前导点
			"title-abcdefgh.mp4",
		},
	}
	for _, c := range cases {
		got := desiredPikPakName(c.title, c.viewkey, c.ext)
		if got != c.want {
			t.Errorf("desiredPikPakName(%q,%q,%q) = %q, want %q", c.title, c.viewkey, c.ext, got, c.want)
		}
	}
}
