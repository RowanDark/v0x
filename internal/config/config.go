package config

import "time"

// Config holds all runtime configuration derived from CLI flags.
//
// SECURITY NOTE: Credentials passed via CLI flags (--auth-*) may appear in
// shell history. Prefer environment variables or a config file for sensitive values.
type Config struct {
	URL           string
	Depth         int
	Delay         int
	MinWordLength int
	UserAgent     string
	Output        string
	Format        string
	Headless      bool
	Verbose       bool
	Timeout       time.Duration

	// Form-based login
	AuthFormURL       string
	AuthFormUser      string
	AuthFormPass      string
	AuthFormUserField string // default: "username"
	AuthFormPassField string // default: "password"
	AuthFormSubmit    string // default: "[type=submit]"
	AuthVerifySelector string // CSS selector that must be present after login

	// HTTP Basic auth
	AuthBasicUser string
	AuthBasicPass string

	// Cookie injection ("name=value; name2=value2")
	AuthCookie string

	// Bearer token / custom header
	AuthBearer string
	AuthHeader string // "Name: Value" format
}
