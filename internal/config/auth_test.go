package config_test

import (
	"strings"
	"testing"
)

func TestLoad_AuthOmittedIsEmpty(t *testing.T) {
	cfg := loadFromString(t, minimalConfig)
	if cfg.Auth.TrustedHeader != "" || len(cfg.Auth.TrustedProxies) != 0 || cfg.Auth.Token != "" {
		t.Fatalf("auth should be empty when omitted, got %+v", cfg.Auth)
	}
}

func TestLoad_AuthProxyAndToken(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+`
auth:
  trusted_header: Remote-User
  trusted_proxies:
    - 10.0.0.0/8
    - 127.0.0.1
  token: s3cret
`)
	if cfg.Auth.TrustedHeader != "Remote-User" {
		t.Errorf("trusted_header = %q", cfg.Auth.TrustedHeader)
	}
	if len(cfg.Auth.TrustedProxies) != 2 {
		t.Errorf("trusted_proxies = %v", cfg.Auth.TrustedProxies)
	}
	if cfg.Auth.Token != "s3cret" {
		t.Errorf("token = %q", cfg.Auth.Token)
	}
}

func TestLoad_AuthTokenOnly(t *testing.T) {
	cfg := loadFromString(t, minimalConfig+`
auth:
  token: s3cret
`)
	if cfg.Auth.Token != "s3cret" || cfg.Auth.TrustedHeader != "" {
		t.Fatalf("token-only auth mis-parsed: %+v", cfg.Auth)
	}
}

func TestLoad_AuthTrustedHeaderRequiresProxies(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+`
auth:
  trusted_header: Remote-User
`)
	if err == nil || !strings.Contains(err.Error(), "trusted_proxies is required") {
		t.Fatalf("want trusted_proxies-required error, got %v", err)
	}
}

func TestLoad_AuthProxiesWithoutHeaderRejected(t *testing.T) {
	_, err := loadStringToConfig(t, minimalConfig+`
auth:
  trusted_proxies:
    - 10.0.0.0/8
`)
	if err == nil || !strings.Contains(err.Error(), "without trusted_header") {
		t.Fatalf("want proxies-without-header error, got %v", err)
	}
}

func TestLoad_AuthRejectsInvalidProxyEntry(t *testing.T) {
	for _, entry := range []string{"not-an-ip", "10.0.0.0/99"} {
		_, err := loadStringToConfig(t, minimalConfig+`
auth:
  trusted_header: Remote-User
  trusted_proxies:
    - `+entry+`
`)
		if err == nil || !strings.Contains(err.Error(), "invalid trusted_proxies") {
			t.Fatalf("entry %q: want invalid-proxy error, got %v", entry, err)
		}
	}
}
