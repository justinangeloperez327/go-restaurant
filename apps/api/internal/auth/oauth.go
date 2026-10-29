package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/justinangeloperez327/go-restaurant/apps/api/internal/config"
	"golang.org/x/oauth2"
)

const sessionCookie = "gr_session"

type provider struct {
	config   oauth2.Config
	verifier *oidc.IDTokenVerifier
	apple    bool
}

type Service struct {
	store     Store
	cfg       config.Config
	providers map[string]provider
	appleKey  *ecdsa.PrivateKey
}

func NewService(cfg config.Config, pool *pgxpool.Pool) (*Service, error) {
	s := &Service{store: Store{DB: pool}, cfg: cfg, providers: make(map[string]provider)}
	if cfg.Google.ClientID != "" {
		s.providers["google"] = provider{
			config: oauth2.Config{
				ClientID: cfg.Google.ClientID, ClientSecret: cfg.Google.ClientSecret,
				RedirectURL: cfg.APIBaseURL + "/api/v1/auth/google/callback",
				Endpoint: oauth2.Endpoint{
					AuthURL: "https://accounts.google.com/o/oauth2/v2/auth",
					TokenURL: "https://oauth2.googleapis.com/token",
					AuthStyle: oauth2.AuthStyleInParams,
				},
				Scopes: []string{"openid", "profile", "email"},
			},
			verifier: oidc.NewVerifier("https://accounts.google.com",
				oidc.NewRemoteKeySet(context.Background(), "https://www.googleapis.com/oauth2/v3/certs"),
				&oidc.Config{ClientID: cfg.Google.ClientID, SupportedSigningAlgs: []string{oidc.RS256}}),
		}
	}
	if cfg.Apple.ClientID != "" {
		key, err := jwt.ParseECPrivateKeyFromPEM([]byte(cfg.AppleKeyPEM))
		if err != nil {
			return nil, fmt.Errorf("invalid Apple signing key: %w", err)
		}
		if key.Curve.Params().Name != "P-256" {
			return nil, errors.New("Apple signing key must be P-256")
		}
		s.appleKey = key
		s.providers["apple"] = provider{
			apple: true,
			config: oauth2.Config{
				ClientID: cfg.Apple.ClientID,
				RedirectURL: cfg.APIBaseURL + "/api/v1/auth/apple/callback",
				Endpoint: oauth2.Endpoint{
					AuthURL: "https://appleid.apple.com/auth/authorize",
					TokenURL: "https://appleid.apple.com/auth/token",
					AuthStyle: oauth2.AuthStyleInParams,
				},
				Scopes: []string{"name", "email"},
			},
			verifier: oidc.NewVerifier("https://appleid.apple.com",
				oidc.NewRemoteKeySet(context.Background(), "https://appleid.apple.com/auth/keys"),
				&oidc.Config{ClientID: cfg.Apple.ClientID, SupportedSigningAlgs: []string{oidc.RS256}}),
		}
	}
	return s, nil
}

func randomString() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func flowCookie(providerName string) string { return "gr_oauth_" + providerName }

func (s *Service) cookie(name, value, path string, age int, apple bool) *http.Cookie {
	c := &http.Cookie{
		Name: name, Value: value, Path: path,
		HttpOnly: true, Secure: s.cfg.SecureCookies, MaxAge: age,
		SameSite: http.SameSiteLaxMode,
	}
	if apple {
		c.Secure = true
		c.SameSite = http.SameSiteNoneMode
	}
	return c
}

func (s *Service) providersList(w http.ResponseWriter, _ *http.Request) {
	providers := make([]string, 0, 2)
	for _, name := range []string{"google", "apple"} {
		if _, ok := s.providers[name]; ok {
			providers = append(providers, name)
		}
	}
	jsonResponse(w, http.StatusOK, map[string]any{"providers": providers})
}

func (s *Service) start(w http.ResponseWriter, r *http.Request, name string) {
	p, ok := s.providers[name]
	if !ok {
		errorResponse(w, http.StatusServiceUnavailable, "provider not configured")
		return
	}
	state, err := randomString()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "could not start login")
		return
	}
	nonce, err := randomString()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "could not start login")
		return
	}
	verifier := ""
	options := []oauth2.AuthCodeOption{oidc.Nonce(nonce)}
	if p.apple {
		options = append(options, oauth2.SetAuthURLParam("response_mode", "form_post"))
	} else {
		verifier = oauth2.GenerateVerifier()
		options = append(options, oauth2.S256ChallengeOption(verifier))
	}
	if err := s.store.BeginFlow(r.Context(), state, name, verifier, nonce); err != nil {
		errorResponse(w, http.StatusServiceUnavailable, "could not start login")
		return
	}
	http.SetCookie(w, s.cookie(flowCookie(name), state,
		"/api/v1/auth/"+name+"/callback", 600, p.apple))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, p.config.AuthCodeURL(state, options...), http.StatusFound)
}

func (s *Service) appleSecret() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer: s.cfg.AppleTeamID, Subject: s.cfg.Apple.ClientID,
		Audience: jwt.ClaimStrings{"https://appleid.apple.com"},
		IssuedAt: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = s.cfg.AppleKeyID
	return token.SignedString(s.appleKey)
}

func verifiedEmail(verified any) bool {
	switch value := verified.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(value, "true")
	default:
		return false
	}
}

func (s *Service) callback(w http.ResponseWriter, r *http.Request, name string) {
	p, ok := s.providers[name]
	if !ok {
		errorResponse(w, http.StatusServiceUnavailable, "provider not configured")
		return
	}
	if (p.apple && r.Method != http.MethodPost) || (!p.apple && r.Method != http.MethodGet) {
		errorResponse(w, http.StatusMethodNotAllowed, "invalid callback method")
		return
	}
	if p.apple {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
	}
	if err := r.ParseForm(); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid callback")
		return
	}
	receivedState, code := r.Form.Get("state"), r.Form.Get("code")
	stored, err := r.Cookie(flowCookie(name))
	if err != nil || receivedState == "" || len(receivedState) > 256 ||
		subtle.ConstantTimeCompare([]byte(receivedState), []byte(stored.Value)) != 1 {
		errorResponse(w, http.StatusBadRequest, "invalid login state")
		return
	}
	http.SetCookie(w, s.cookie(flowCookie(name), "", "/api/v1/auth/"+name+"/callback", -1, p.apple))
	if code == "" || r.Form.Get("error") != "" {
		errorResponse(w, http.StatusBadRequest, "login was not completed")
		return
	}
	flow, err := s.store.ConsumeFlow(r.Context(), receivedState, name)
	if err != nil {
		errorResponse(w, http.StatusBadRequest, "expired or previously used login state")
		return
	}

	oauthConfig := p.config
	var options []oauth2.AuthCodeOption
	if p.apple {
		oauthConfig.ClientSecret, err = s.appleSecret()
		if err != nil {
			errorResponse(w, http.StatusBadGateway, "identity provider unavailable")
			return
		}
	} else {
		options = append(options, oauth2.VerifierOption(flow.Verifier))
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	token, err := oauthConfig.Exchange(ctx, code, options...)
	if err != nil {
		errorResponse(w, http.StatusBadGateway, "identity provider unavailable")
		return
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok || rawID == "" {
		errorResponse(w, http.StatusBadGateway, "identity token missing")
		return
	}
	idToken, err := p.verifier.Verify(ctx, rawID)
	if err != nil || idToken.Subject == "" ||
		subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(flow.Nonce)) != 1 {
		errorResponse(w, http.StatusUnauthorized, "invalid identity token")
		return
	}
	var claims struct {
		Email string `json:"email"`
		EmailVerified any `json:"email_verified"`
		Name string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		errorResponse(w, http.StatusUnauthorized, "invalid identity claims")
		return
	}
	if !verifiedEmail(claims.EmailVerified) {
		claims.Email = ""
	}
	if p.apple && claims.Name == "" {
		var profile struct {
			Name struct {
				First string `json:"firstName"`
				Last string `json:"lastName"`
			} `json:"name"`
		}
		if raw := r.Form.Get("user"); len(raw) < 8192 {
			if json.Unmarshal([]byte(raw), &profile) == nil {
				claims.Name = strings.TrimSpace(profile.Name.First + " " + profile.Name.Last)
			}
		}
	}
	userID, err := s.store.ResolveIdentity(ctx, name, idToken.Subject, claims.Email, claims.Name)
	if err != nil {
		errorResponse(w, http.StatusForbidden, "could not sign in")
		return
	}
	rawSession, err := randomString()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, "could not create session")
		return
	}
	if err := s.store.NewSession(ctx, userID, rawSession); err != nil {
		errorResponse(w, http.StatusServiceUnavailable, "could not create session")
		return
	}
	http.SetCookie(w, s.cookie(sessionCookie, rawSession, "/", 7*24*60*60, false))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, s.cfg.WebOrigin, http.StatusSeeOther)
}

func (s *Service) allowedOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	a, err := url.Parse(origin)
	if err != nil || a.Path != "" || a.RawQuery != "" {
		return false
	}
	return origin == s.cfg.WebOrigin || origin == s.cfg.APIBaseURL
}
