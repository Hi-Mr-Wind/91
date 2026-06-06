package spider91migrate

import (
	"strings"
	"unicode"
)

// 期望的 PikPak 文件名格式（方案 B）：
//
//	<sanitized-title>-<viewkey-后8位>.<ext>
//
// 例如：
//
//	"超白大奶职业律师约炮第一季-672d2fa0.mp4"
//
// 设计目标：
//   - 文件名一眼能看出视频内容（用 catalog 里的 title）
//   - 后缀的 viewkey 8 字符保证同标题不会撞名
//   - 全部字符在常见文件系统、PikPak、HTTP/Aliyun OSS Key 编码里都安全
//
// 字符清洗规则（sanitizeTitle）：
//   - 去除控制字符（< 0x20 或 0x7F）
//   - 替换 / \ : * ? " < > | 为空格
//   - 折叠连续空白为单个空格
//   - trim 首尾空白与点号
//   - 截断到最多 maxTitleRunes 个 unicode 字符（不是字节）
//   - 最终为空时回退到 "video"，避免无效文件名

const maxTitleRunes = 80

// sanitizeTitle 把一段任意文本转成可作为文件名一部分的字符串。
func sanitizeTitle(title string) string {
	var b strings.Builder
	b.Grow(len(title))
	prevSpace := false
	for _, r := range title {
		switch {
		case unicode.IsSpace(r):
			// 任何空白（含 \n \t 全角空格）→ 折叠成单个 ASCII 空格
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		case r < 0x20 || r == 0x7F:
			// 非空白控制字符 → 丢弃
		case isFilenameForbidden(r):
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		default:
			b.WriteRune(r)
			prevSpace = false
		}
	}
	out := strings.TrimFunc(b.String(), func(r rune) bool {
		return unicode.IsSpace(r) || r == '.'
	})
	out = truncateRunes(out, maxTitleRunes)
	if out == "" {
		out = "video"
	}
	return out
}

func isFilenameForbidden(r rune) bool {
	switch r {
	case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
		return true
	}
	return false
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		count++
		if count > maxRunes {
			return s[:i]
		}
	}
	return s
}

// extractViewKey 从 video.ID（"spider91-<driveID>-<viewkey>"）里
// 取出最后一段 viewkey。
//
// driveID 中如果有 "-" 不影响（用 LastIndex），viewkey 本身（91 网站的
// view 标识）目前都是纯 hex 或纯数字，不包含 "-"。
func extractViewKey(videoID string) string {
	if i := strings.LastIndex(videoID, "-"); i >= 0 {
		return videoID[i+1:]
	}
	return videoID
}

// viewKeySuffix 取 viewkey 的最后 N 个字符；不足 N 返回原字符串。
//
// 默认 N=8（足够稀疏避免标题撞名时的同名冲突）。
const viewKeySuffixLen = 8

func viewKeySuffix(viewkey string) string {
	r := []rune(viewkey)
	if len(r) <= viewKeySuffixLen {
		return string(r)
	}
	return string(r[len(r)-viewKeySuffixLen:])
}

// desiredPikPakName 构造 spider91 视频在 PikPak 上的期望文件名。
//
//	desiredPikPakName("超白大奶律师约炮", "476fa8bf4b47e672d2fa", "mp4")
//	  → "超白大奶律师约炮-72d2fa.mp4"  // 实际是 e672d2fa（取最后 8）
//
// ext 不带前导点；空时默认 mp4。
func desiredPikPakName(title, viewkey, ext string) string {
	clean := sanitizeTitle(title)
	suffix := viewKeySuffix(strings.TrimSpace(viewkey))
	ext = strings.TrimSpace(ext)
	ext = strings.TrimPrefix(ext, ".")
	if ext == "" {
		ext = "mp4"
	}
	if suffix == "" {
		// viewkey 缺失时退化成 "<title>.<ext>"
		return clean + "." + ext
	}
	return clean + "-" + suffix + "." + ext
}
