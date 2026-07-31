// Package videoname contains the filename rules shared by scanners, uploads,
// and crawler migrations. A video's display title is the final filename with
// only its last extension removed.
package videoname

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

const maxFileNameBytes = 255
const collisionSuffixBytes = 9 // "-" plus the last eight upload-id bytes.

// TitleFromFileName removes only the last extension from a filename.
func TitleFromFileName(fileName string) string {
	name := strings.TrimSpace(filepath.Base(fileName))
	ext := filepath.Ext(name)
	if ext != "" {
		name = strings.TrimSuffix(name, ext)
	}
	return strings.TrimSpace(name)
}

// ValidateUploadTitle checks that title can safely become a single physical
// filename component. Space for a possible collision suffix is reserved so a
// later same-name upload can always be made unique without truncating the title.
func ValidateUploadTitle(title, ext string) error {
	if title == "" || strings.TrimSpace(title) != title {
		return fmt.Errorf("video title must not be empty or have leading/trailing whitespace")
	}
	if strings.HasPrefix(title, ".") || strings.HasSuffix(title, ".") {
		return fmt.Errorf("video title must not start or end with a dot")
	}
	for _, r := range title {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return fmt.Errorf("video title contains characters that cannot be used in a filename")
		}
	}
	ext = strings.TrimSpace(ext)
	if len([]byte(title))+len([]byte(ext))+collisionSuffixBytes > maxFileNameBytes {
		return fmt.Errorf("video title is too long for a filename")
	}
	return nil
}

// UploadFileName builds a physical filename and optionally appends a stable
// collision suffix derived from the upload id.
func UploadFileName(title, ext, uploadID string, collision bool) string {
	if collision {
		suffix := uploadID
		if len(suffix) > 8 {
			suffix = suffix[len(suffix)-8:]
		}
		title += "-" + suffix
	}
	return title + ext
}
