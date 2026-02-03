package kssign

import (
	"crypto/md5"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"time"
)

const salt = "arfw2r4k4rdwrlmchvcu7q61fs"

var saltMD5 = md5Hex(salt)

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ParseQueryLikeFrontend parses a raw query string in the same tolerant way as
// KouShare's front_web bundle: it does not URL-decode, and it treats segments
// without '=' as a continuation of the previous segment.
func ParseQueryLikeFrontend(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}
	}

	parts := strings.Split(raw, "&")
	out := make(map[string]string, len(parts))

	var pending string
	flush := func() {
		if pending == "" {
			return
		}
		key, val, ok := strings.Cut(pending, "=")
		if !ok || key == "" {
			pending = ""
			return
		}
		out[key] = val
		pending = ""
	}

	for _, part := range parts {
		if part == "" {
			continue
		}
		// If the token has no '=', or starts with '=', or is just '=' padding,
		// treat it as a continuation of the previous token.
		if !strings.Contains(part, "=") || strings.HasPrefix(part, "=") || isAllEquals(part) {
			if pending == "" {
				pending = part
			} else {
				pending = pending + "&" + part
			}
			continue
		}
		flush()
		pending = part
	}
	flush()

	return out
}

func isAllEquals(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '=' {
			return false
		}
	}
	return true
}

// ParseURLQueryLikeFrontend extracts and parses the substring after '?', if any.
func ParseURLQueryLikeFrontend(url string) map[string]string {
	idx := strings.IndexByte(url, '?')
	if idx < 0 || idx+1 >= len(url) {
		return map[string]string{}
	}
	return ParseQueryLikeFrontend(url[idx+1:])
}

// Sign computes KouShare's "Ks-Sign" and "Ks-Timestamp" headers.
// Timestamp is in milliseconds since epoch.
func Sign(params map[string]string, method string) (sign string, timestampMs int64) {
	timestampMs = time.Now().UnixMilli()
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}

	clean := make(map[string]string, len(params))
	for k, v := range params {
		if v == "" {
			continue
		}
		clean[k] = v
	}

	keys := make([]string, 0, len(clean))
	for k := range clean {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(clean[k])
	}

	signBase := "method=" + method + "&timestamp=" + itoaInt64(timestampMs) + "&saltmd5=" + saltMD5
	signInput := signBase
	if b.Len() > 0 {
		signInput = b.String() + "&" + signBase
	}
	return md5Hex(signInput), timestampMs
}

func itoaInt64(v int64) string {
	return strconv.FormatInt(v, 10)
}
