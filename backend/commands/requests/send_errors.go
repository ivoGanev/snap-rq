package requests

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// detailsMarker separates the friendly text of a failed send from the raw
// error; the response view folds what follows it under "Details".
const detailsMarker = "\n\nDetails: "

// withDetails puts the friendly text first and keeps the raw error after it.
func withDetails(friendly, raw string) string {
	if friendly == "" {
		return raw
	}
	return friendly + detailsMarker + raw
}

// headerLineError is a header line the server could never receive as a header.
type headerLineError struct {
	Line int
	Text string
}

func (e *headerLineError) Error() string {
	return fmt.Sprintf("Header line %d isn't a header: %s\n"+
		"Headers go one per line, as  Name: value  (for example  Accept: application/json).", e.Line, e.Text)
}

// parseHeaders reads "Name: value" lines; blank lines are skipped.
func parseHeaders(text string) (http.Header, error) {
	headers := http.Header{}
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		name, value, found := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !found || !validHeaderName(name) {
			return nil, &headerLineError{Line: i + 1, Text: line}
		}
		headers.Set(name, strings.TrimSpace(value))
	}
	return headers, nil
}

// validHeaderName reports whether name is a token (RFC 9110): letters,
// digits and !#$%&'*+-.^_`|~.
func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		ok := r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("!#$%&'*+-.^_`|~", r))
		if !ok {
			return false
		}
	}
	return true
}

// explainSendError turns the client errors we recognise into a sentence a
// person can act on; "" for anything else, which is then shown raw.
func explainSendError(err error, req *http.Request, timeout time.Duration) string {
	host := req.URL.Host

	var dns *net.DNSError
	var unknownAuthority x509.UnknownAuthorityError
	var wrongHost x509.HostnameError
	var invalid x509.CertificateInvalidError
	var netErr net.Error

	switch {
	case errors.As(err, &dns):
		return fmt.Sprintf("Couldn't find the server %s. Check the address and your connection.", dns.Name)
	case isConnectionRefused(err):
		return fmt.Sprintf("Nothing is answering at %s. Is the server running?", host)
	case errors.As(err, &netErr) && netErr.Timeout():
		return fmt.Sprintf("The server didn't answer within %v.", timeout)
	case errors.As(err, &unknownAuthority):
		return certHint(host, "isn't signed by an authority this computer trusts")
	case errors.As(err, &wrongHost):
		return certHint(host, "was issued for another address")
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		return certHint(host, "has expired")
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), isConnectionReset(err):
		return "The server closed the connection before answering."
	// Go has no error types for these two.
	case strings.Contains(err.Error(), "stopped after 10 redirects"):
		return "Stopped after 10 redirects: the server may be redirecting in a loop."
	case strings.Contains(err.Error(), "unsupported protocol scheme"):
		return "The URL has to start with http:// or https://."
	}
	return ""
}

func certHint(host, reason string) string {
	return fmt.Sprintf("The certificate of %s %s. If this is your own test server, "+
		"you can turn off certificate checks in Settings.", host, reason)
}

// Windows reports these under its own Winsock codes.
const (
	wsaECONNRESET   = syscall.Errno(10054)
	wsaECONNREFUSED = syscall.Errno(10061)
)

func isConnectionRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, wsaECONNREFUSED)
}

func isConnectionReset(err error) bool {
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, wsaECONNRESET)
}
