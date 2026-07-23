package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type config struct {
	Port              string
	DatabaseURL       string
	NewAPIBaseURL     string
	NewAPIAccessToken string
	NewAPIUserID      string
	AdminUsername     string
	AdminPassword     string
	SessionTTL        time.Duration
	CookieSecure      bool
}

func loadConfig() (config, error) {
	ttlHours, err := strconv.Atoi(env("SESSION_TTL_HOURS", "24"))
	if err != nil || ttlHours < 1 || ttlHours > 24*30 {
		return config{}, fmt.Errorf("SESSION_TTL_HOURS 必须是 1~720 的整数")
	}
	cfg := config{
		Port:              env("PORT", "8080"),
		DatabaseURL:       strings.TrimSpace(os.Getenv("DATABASE_URL")),
		NewAPIBaseURL:     strings.TrimRight(strings.TrimSpace(os.Getenv("NEWAPI_BASE_URL")), "/"),
		NewAPIAccessToken: strings.TrimSpace(os.Getenv("NEWAPI_ACCESS_TOKEN")),
		NewAPIUserID:      env("NEWAPI_USER_ID", "1"),
		AdminUsername:     env("ADMIN_USERNAME", "admin"),
		AdminPassword:     os.Getenv("ADMIN_PASSWORD"),
		SessionTTL:        time.Duration(ttlHours) * time.Hour,
		CookieSecure:      strings.EqualFold(env("COOKIE_SECURE", "false"), "true"),
	}
	if cfg.NewAPIBaseURL != "" && !strings.HasPrefix(cfg.NewAPIBaseURL, "http://") && !strings.HasPrefix(cfg.NewAPIBaseURL, "https://") {
		return config{}, fmt.Errorf("NEWAPI_BASE_URL 必须以 http:// 或 https:// 开头")
	}
	if cfg.DatabaseURL == "" {
		return config{}, fmt.Errorf("DATABASE_URL 未配置")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
