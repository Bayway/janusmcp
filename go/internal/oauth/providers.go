package oauth

// DefaultProviders returns built-in OAuth endpoint presets. Client IDs are left
// empty on purpose — set them in config (or via ${ENV}) where you also override
// anything else. Config entries with the same key win over these defaults.
func DefaultProviders() map[string]Provider {
	return map[string]Provider{
		"github": {
			AuthURL:  "https://github.com/login/oauth/authorize",
			TokenURL: "https://github.com/login/oauth/access_token",
			Scopes:   []string{"repo", "read:org"},
		},
		"supabase": {
			AuthURL:  "https://api.supabase.com/v1/oauth/authorize",
			TokenURL: "https://api.supabase.com/v1/oauth/token",
			Scopes:   []string{"all"},
		},
		"google": {
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
			Scopes:   []string{"openid", "email"},
		},
	}
}
