package models

// AppSettings contains user-configurable application preferences.
type AppSettings struct {
	ValidateSSLCertificates bool `json:"validate_ssl_certificates"`
	RequestTimeoutSeconds   int  `json:"request_timeout_seconds"`
}
