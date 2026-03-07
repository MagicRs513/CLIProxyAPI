package configaccess

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

// Register ensures the config-access provider is available to the access manager.
func Register(cfg *sdkconfig.SDKConfig) {
	if cfg == nil {
		sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey)
		return
	}

	keys := normalizeKeys(cfg.APIKeys)
	if len(keys) == 0 {
		sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey)
		return
	}

	settings := normalizeKeySettings(cfg.APIKeySettings)

	sdkaccess.RegisterProvider(
		sdkaccess.AccessProviderTypeConfigAPIKey,
		newProvider(sdkaccess.DefaultAccessProviderName, keys, settings),
	)
}

type keySetting struct {
	expiresAt  time.Time
	tokenLimit int64
}

type provider struct {
	name string
	keys map[string]struct{}

	settings map[string]keySetting
}

func newProvider(name string, keys []string, settings map[string]keySetting) *provider {
	providerName := strings.TrimSpace(name)
	if providerName == "" {
		providerName = sdkaccess.DefaultAccessProviderName
	}
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[key] = struct{}{}
	}
	settingsCopy := make(map[string]keySetting, len(settings))
	for key, setting := range settings {
		settingsCopy[key] = setting
	}
	return &provider{
		name:     providerName,
		keys:     keySet,
		settings: settingsCopy,
	}
}

func (p *provider) Identifier() string {
	if p == nil || p.name == "" {
		return sdkaccess.DefaultAccessProviderName
	}
	return p.name
}

func (p *provider) Authenticate(_ context.Context, r *http.Request) (*sdkaccess.Result, *sdkaccess.AuthError) {
	if p == nil {
		return nil, sdkaccess.NewNotHandledError()
	}
	if len(p.keys) == 0 {
		return nil, sdkaccess.NewNotHandledError()
	}
	authHeader := r.Header.Get("Authorization")
	authHeaderGoogle := r.Header.Get("X-Goog-Api-Key")
	authHeaderAnthropic := r.Header.Get("X-Api-Key")
	queryKey := ""
	queryAuthToken := ""
	if r.URL != nil {
		queryKey = r.URL.Query().Get("key")
		queryAuthToken = r.URL.Query().Get("auth_token")
	}
	if authHeader == "" && authHeaderGoogle == "" && authHeaderAnthropic == "" && queryKey == "" && queryAuthToken == "" {
		return nil, sdkaccess.NewNoCredentialsError()
	}

	apiKey := extractBearerToken(authHeader)

	candidates := []struct {
		value  string
		source string
	}{
		{apiKey, "authorization"},
		{authHeaderGoogle, "x-goog-api-key"},
		{authHeaderAnthropic, "x-api-key"},
		{queryKey, "query-key"},
		{queryAuthToken, "query-auth-token"},
	}

	for _, candidate := range candidates {
		if candidate.value == "" {
			continue
		}
		if _, ok := p.keys[candidate.value]; ok {
			if allowed, reason := p.allow(candidate.value); !allowed {
				return nil, sdkaccess.NewInvalidCredentialErrorWithMessage(reason)
			}
			return &sdkaccess.Result{
				Provider:  p.Identifier(),
				Principal: candidate.value,
				Metadata: map[string]string{
					"source": candidate.source,
				},
			}, nil
		}
	}

	return nil, sdkaccess.NewInvalidCredentialError()
}

func (p *provider) allow(apiKey string) (bool, string) {
	if p == nil || len(p.settings) == 0 {
		return true, ""
	}
	setting, ok := p.settings[apiKey]
	if !ok {
		return true, ""
	}
	now := time.Now().UTC()
	if !setting.expiresAt.IsZero() && now.After(setting.expiresAt) {
		return false, "API key expired"
	}
	if setting.tokenLimit > 0 {
		used := usageByAPIKey(apiKey)
		if used >= setting.tokenLimit {
			return false, "API key token quota exceeded"
		}
	}
	return true, ""
}

func usageByAPIKey(apiKey string) int64 {
	stats := usage.GetRequestStatistics()
	if stats == nil {
		return 0
	}
	snapshot := stats.Snapshot()
	apiStats, ok := snapshot.APIs[apiKey]
	if !ok {
		return 0
	}
	return apiStats.TotalTokens
}

func normalizeKeySettings(entries []sdkconfig.APIKeySetting) map[string]keySetting {
	if len(entries) == 0 {
		return nil
	}
	settings := make(map[string]keySetting, len(entries))
	for _, entry := range entries {
		apiKey := strings.TrimSpace(entry.APIKey)
		if apiKey == "" {
			continue
		}
		if _, exists := settings[apiKey]; exists {
			continue
		}
		if entry.TokenLimit < 0 {
			continue
		}
		var expiresAt time.Time
		expiresRaw := strings.TrimSpace(entry.ExpiresAt)
		if expiresRaw != "" {
			parsed, err := time.Parse(time.RFC3339, expiresRaw)
			if err != nil {
				continue
			}
			expiresAt = parsed.UTC()
		}
		if expiresAt.IsZero() && entry.TokenLimit == 0 {
			continue
		}
		settings[apiKey] = keySetting{expiresAt: expiresAt, tokenLimit: entry.TokenLimit}
	}
	if len(settings) == 0 {
		return nil
	}
	return settings
}

func extractBearerToken(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return header
	}
	if strings.ToLower(parts[0]) != "bearer" {
		return header
	}
	return strings.TrimSpace(parts[1])
}

func normalizeKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		if _, exists := seen[trimmedKey]; exists {
			continue
		}
		seen[trimmedKey] = struct{}{}
		normalized = append(normalized, trimmedKey)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
