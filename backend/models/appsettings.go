package models

// AppSettings contains user-configurable application preferences.
type AppSettings struct {
	ValidateSSLCertificates bool `json:"validate_ssl_certificates"`
	RequestTimeoutSeconds   int  `json:"request_timeout_seconds"`
	UndoHistoryLimit        int  `json:"undo_history_limit"`
	// UseCookieJar stores the cookies servers set and sends them back on later requests.
	UseCookieJar bool `json:"use_cookie_jar"`
}
