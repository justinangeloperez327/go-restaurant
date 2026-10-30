package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type OAuthProvider struct {
	ClientID     string
	ClientSecret string
}

type Config struct {
	Environment         string
	Port                string
	DatabaseURL         string
	APIBaseURL          string
	WebOrigin           string
	SecureCookies       bool
	Google              OAuthProvider
	Apple               OAuthProvider
	AppleTeamID         string
	AppleKeyID          string
	AppleKeyPEM         string
	StripeSecretKey     string
	StripeWebhookSecret string
	AllowCOD            bool
}

func Load() (Config, error) {
	cfg := Config{
		Environment: os.Getenv("APP_ENV"),
		Port:        os.Getenv("PORT"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		APIBaseURL:  os.Getenv("API_PUBLIC_URL"),
		WebOrigin:   os.Getenv("WEB_ORIGIN"),
		Google: OAuthProvider{
			ClientID: os.Getenv("GOOGLE_CLIENT_ID"), ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		},
		Apple:               OAuthProvider{ClientID: os.Getenv("APPLE_CLIENT_ID")},
		AppleTeamID:         os.Getenv("APPLE_TEAM_ID"),
		AppleKeyID:          os.Getenv("APPLE_KEY_ID"),
		AppleKeyPEM:         strings.ReplaceAll(os.Getenv("APPLE_PRIVATE_KEY_PEM"), `\n`, "\n"),
		StripeSecretKey:     os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		AllowCOD:            os.Getenv("ALLOW_COD") == "true",
	}
	if cfg.Environment == "" { cfg.Environment = "development" }
	if cfg.Environment != "development" && cfg.Environment != "test" && cfg.Environment != "production" {
		return Config{}, errors.New("APP_ENV must be development, test or production")
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	port, err := strconv.Atoi(cfg.Port)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("PORT must be an integer from 1 to 65535")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = "http://localhost:8080"
	}
	if cfg.WebOrigin == "" {
		cfg.WebOrigin = "http://localhost:3000"
	}
	api, err := url.Parse(cfg.APIBaseURL)
	if err != nil || api.Host == "" || (api.Scheme != "http" && api.Scheme != "https") || api.Path != "" || api.RawQuery != "" || api.Fragment != "" || api.User != nil || api.ForceQuery {
		return Config{}, errors.New("API_PUBLIC_URL must be an origin without a path")
	}
	web, err := url.Parse(cfg.WebOrigin)
	if err != nil || web.Host == "" || (web.Scheme != "http" && web.Scheme != "https") || web.Path != "" || web.RawQuery != "" || web.Fragment != "" || web.User != nil || web.ForceQuery {
		return Config{}, errors.New("WEB_ORIGIN must be an origin without a path")
	}
	if cfg.Environment == "production" && (api.Scheme != "https" || web.Scheme != "https") {
		return Config{}, errors.New("production requires HTTPS API_PUBLIC_URL and WEB_ORIGIN")
	}
	cfg.SecureCookies = api.Scheme == "https"
	if (cfg.Google.ClientID == "") != (cfg.Google.ClientSecret == "") {
		return Config{}, errors.New("GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET must both be set")
	}
	appleParts := []string{cfg.Apple.ClientID, cfg.AppleTeamID, cfg.AppleKeyID, cfg.AppleKeyPEM}
	anyApple, allApple := false, true
	for _, v := range appleParts {
		anyApple = anyApple || v != ""
		allApple = allApple && v != ""
	}
	if anyApple && !allApple {
		return Config{}, errors.New("all Apple sign-in variables must be set together")
	}
	if anyApple && !cfg.SecureCookies {
		return Config{}, errors.New("Sign in with Apple requires HTTPS API_PUBLIC_URL")
	}
	if cfg.Environment == "production" {
		if parsed, err := url.Parse(cfg.DatabaseURL); err == nil && parsed.User != nil {
			if password, ok := parsed.User.Password(); ok && password == "restaurant_dev" {
				return Config{}, errors.New("production DATABASE_URL must not use the development password")
			}
		}
		if !cfg.AllowCOD && cfg.StripeSecretKey == "" {
			return Config{}, errors.New("production needs at least one configured payment method")
		}
	}
	if cfg.Environment == "production" && (api.Hostname() == "localhost" || web.Hostname() == "localhost") {
		return Config{}, errors.New("production public origins must not use localhost")
	}
	if (cfg.StripeSecretKey == "") != (cfg.StripeWebhookSecret == "") {
		return Config{}, errors.New("STRIPE_SECRET_KEY and STRIPE_WEBHOOK_SECRET must both be set")
	}
	return cfg, nil
}
