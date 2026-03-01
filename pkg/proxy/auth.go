package proxy

import "net/http"

// AuthType defines how the proxy authenticates to upstream services.
type AuthType string

const (
	AuthBearer AuthType = "bearer_token"
	AuthBasic  AuthType = "basic_auth"
	AuthHeader AuthType = "header"
)

// AuthConfig holds resolved upstream credentials.
// Values are resolved from environment variables at server startup.
type AuthConfig struct {
	Type        AuthType
	Token       string // For bearer_token
	Username    string // For basic_auth
	Password    string // For basic_auth
	HeaderName  string // For header
	HeaderValue string // For header
}

// Apply sets the appropriate authentication on an HTTP request.
func (a *AuthConfig) Apply(req *http.Request) {
	switch a.Type {
	case AuthBearer:
		req.Header.Set("Authorization", "Bearer "+a.Token)
	case AuthBasic:
		req.SetBasicAuth(a.Username, a.Password)
	case AuthHeader:
		req.Header.Set(a.HeaderName, a.HeaderValue)
	}
}
