package config

import "testing"

func TestSanitizeAPIKeys(t *testing.T) {
	cfg := &Config{}
	cfg.APIKeys = []string{"  k1  ", "", "k2", "k1"}
	cfg.APIKeySettings = []APIKeySetting{
		{APIKey: "k1", ExpiresAt: "2026-03-10", TokenLimit: 100},
		{APIKey: "k2", ExpiresAt: "2026-03-10T12:00:00Z", TokenLimit: 0},
		{APIKey: "k3", ExpiresAt: "2026-03-10", TokenLimit: 100},
		{APIKey: "k2", ExpiresAt: "2026-03-11", TokenLimit: 200},
		{APIKey: "k1", ExpiresAt: "", TokenLimit: -1},
		{APIKey: "", ExpiresAt: "2026-03-10", TokenLimit: 1},
		{APIKey: "k1", ExpiresAt: "not-a-date", TokenLimit: 1},
	}

	cfg.SanitizeAPIKeys()

	if len(cfg.APIKeys) != 2 || cfg.APIKeys[0] != "k1" || cfg.APIKeys[1] != "k2" {
		t.Fatalf("unexpected keys: %#v", cfg.APIKeys)
	}
	if len(cfg.APIKeySettings) != 2 {
		t.Fatalf("unexpected settings count: %d", len(cfg.APIKeySettings))
	}
	if cfg.APIKeySettings[0].APIKey != "k1" || cfg.APIKeySettings[0].ExpiresAt != "2026-03-10T23:59:59Z" || cfg.APIKeySettings[0].TokenLimit != 100 {
		t.Fatalf("unexpected first setting: %#v", cfg.APIKeySettings[0])
	}
	if cfg.APIKeySettings[1].APIKey != "k2" || cfg.APIKeySettings[1].ExpiresAt != "2026-03-10T12:00:00Z" {
		t.Fatalf("unexpected second setting: %#v", cfg.APIKeySettings[1])
	}
}
