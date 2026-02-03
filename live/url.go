package live

import (
	"net/url"
	"regexp"
	"strings"
)

var m3u8URLRe = regexp.MustCompile(`https?://[^\s"'<>]+?\.m3u8[^\s"'<>]*`)

func findFirstM3U8URL(text string) string {
	return strings.TrimSpace(m3u8URLRe.FindString(text))
}

func resolveURL(baseRaw string, refRaw string) (string, bool) {
	base, err := url.Parse(strings.TrimSpace(baseRaw))
	if err != nil || base == nil {
		return "", false
	}
	ref, err := url.Parse(strings.TrimSpace(refRaw))
	if err != nil || ref == nil {
		return "", false
	}
	if ref.IsAbs() {
		return ref.String(), true
	}
	return base.ResolveReference(ref).String(), true
}
