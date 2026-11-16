package models

// Cookie is one cookie stored in a project's cookie jar. The jar is scoped
// per project, so the same name on the same host can differ between projects.
type Cookie struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"project_id"`
	Domain    string `json:"domain"`
	// HostOnly cookies go only to Domain itself; others also go to subdomains.
	HostOnly bool   `json:"host_only"`
	Path     string `json:"path"`
	Name     string `json:"name"`
	Value    string `json:"value"`
	// ExpiresAt is a Unix time in seconds; 0 marks a session cookie.
	ExpiresAt int64  `json:"expires_at"`
	Secure    bool   `json:"secure"`
	HTTPOnly  bool   `json:"http_only"`
	SameSite  string `json:"same_site"`
	CreatedAt int64  `json:"created_at"`
}
