package mcp

// The app's response view hides secrets by default with the same names MCP
// masking uses, so one list decides what is secret everywhere.

// DisplayHidden is how the response view shows a hidden value.
const DisplayHidden = "•••••"

// MaskedResponse is a response's headers and body with secrets hidden, and
// whether each had anything to hide.
type MaskedResponse struct {
	Headers       string `json:"headers"`
	Body          string `json:"body"`
	HeadersMasked bool   `json:"headers_masked"`
	BodyMasked    bool   `json:"body_masked"`
}

// DisplayService masks responses for the app's own response view.
type DisplayService struct{ store *Store }

func NewDisplayService(store *Store) *DisplayService { return &DisplayService{store: store} }

// MaskResponse hides the values under sensitive names in a response.
func (s *DisplayService) MaskResponse(headers, body string) MaskedResponse {
	m := NewMasker(s.store.Load().MaskExtra).ShowingAs(DisplayHidden)
	masked := MaskedResponse{Headers: m.Headers(headers), Body: m.Body(body)}
	masked.HeadersMasked = masked.Headers != headers
	masked.BodyMasked = masked.Body != body
	return masked
}
