package config

import (
	"os"
	"strings"
)

var (
	apiBaseURL   = "https://api.koushare.com"
	webBaseURL   = "https://www.koushare.com"
	loginBaseURL = "https://login.koushare.com"
)

func init() {
	if v := strings.TrimSpace(os.Getenv("KOUSHARE_API_BASE")); v != "" {
		SetAPIBaseURL(v)
	}
	if v := strings.TrimSpace(os.Getenv("KOUSHARE_WEB_BASE")); v != "" {
		SetWebBaseURL(v)
	}
	if v := strings.TrimSpace(os.Getenv("KOUSHARE_LOGIN_BASE")); v != "" {
		SetLoginBaseURL(v)
	}
}

func normalizeBaseURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimRight(value, "/")
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return "https://" + value
}

func APIBaseURL() string { return apiBaseURL }

func WebBaseURL() string { return webBaseURL }

func LoginBaseURL() string { return loginBaseURL }

func SetAPIBaseURL(value string) {
	if v := normalizeBaseURL(value); v != "" {
		apiBaseURL = v
	}
}

func SetWebBaseURL(value string) {
	if v := normalizeBaseURL(value); v != "" {
		webBaseURL = v
	}
}

func SetLoginBaseURL(value string) {
	if v := normalizeBaseURL(value); v != "" {
		loginBaseURL = v
	}
}
