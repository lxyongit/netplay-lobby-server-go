package main

// Config is the struct that holds the lobby server configuration
type Config struct {
	Server    ServerConfig
	Database  DatabaseConfig
	Relay     map[string]string
	Blacklist BlacklistConfig
	Traversal TraversalConfig
}

// Empty Address disables traversal. AdvertiseHost must resolve directly to the
// TCP listener (no HTTP/CDN proxy). PublicPort permits an explicit port mapping.
type TraversalConfig struct {
	Address       string
	AdvertiseHost string
	PublicPort    int
}

// ServerConfig holds the basic server config.
type ServerConfig struct {
	Address      string
	GeoLite2Path string
	TemplatePath string
}

// DatabaseConfig holds the database config.
type DatabaseConfig struct {
	Type       string
	Connection string
}

// BlacklistConfig configures the different blacklists.
type BlacklistConfig struct {
	Strings []string // General blacklisted words as RE
	IPs     []string
}
