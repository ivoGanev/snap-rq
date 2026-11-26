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
