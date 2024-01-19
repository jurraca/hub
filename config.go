package main

import "github.com/getAlby/nostr-wallet-connect/models/db"

const (
	AlbyBackendType  = "ALBY"
	LNDBackendType   = "LND"
	BreezBackendType = "BREEZ"
	CookieName       = "alby_nwc_session"

	WailsAppType = "WAILS"
	HttpAppType  = "HTTP"
)

type Config struct {
	// These config can be set either by .env or the database config table.
	// database config always takes preference.
	db.Config
	NostrSecretKey          string `envconfig:"NOSTR_PRIVKEY"`
	CookieSecret            string `envconfig:"COOKIE_SECRET"`
	CookieDomain            string `envconfig:"COOKIE_DOMAIN"`
	ClientPubkey            string `envconfig:"CLIENT_NOSTR_PUBKEY"`
	Relay                   string `envconfig:"RELAY" default:"wss://relay.getalby.com/v1"`
	PublicRelay             string `envconfig:"PUBLIC_RELAY"`
	LNDCertFile             string `envconfig:"LND_CERT_FILE"`
	LNDMacaroonFile         string `envconfig:"LND_MACAROON_FILE"`
	BreezWorkdir            string `envconfig:"BREEZ_WORK_DIR" default:".breez"`
	BasicAuthUser           string `envconfig:"BASIC_AUTH_USER"`
	BasicAuthPassword       string `envconfig:"BASIC_AUTH_PASSWORD"`
	AlbyAPIURL              string `envconfig:"ALBY_API_URL" default:"https://api.getalby.com"`
	AlbyClientId            string `envconfig:"ALBY_CLIENT_ID"`
	AlbyClientSecret        string `envconfig:"ALBY_CLIENT_SECRET"`
	OAuthRedirectUrl        string `envconfig:"OAUTH_REDIRECT_URL"`
	OAuthAuthUrl            string `envconfig:"OAUTH_AUTH_URL" default:"https://getalby.com/oauth"`
	OAuthTokenUrl           string `envconfig:"OAUTH_TOKEN_URL" default:"https://api.getalby.com/oauth/token"`
	Port                    string `envconfig:"PORT" default:"8080"`
	DatabaseUri             string `envconfig:"DATABASE_URI" default:"nostr-wallet-connect.db"`
	DatabaseMaxConns        int    `envconfig:"DATABASE_MAX_CONNS" default:"10"`
	DatabaseMaxIdleConns    int    `envconfig:"DATABASE_MAX_IDLE_CONNS" default:"5"`
	DatabaseConnMaxLifetime int    `envconfig:"DATABASE_CONN_MAX_LIFETIME" default:"1800"` // 30 minutes
	IdentityPubkey          string
}
