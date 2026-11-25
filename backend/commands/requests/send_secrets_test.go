package requests

import (
	"strings"
	"testing"
)

func TestLocalErrorsDoNotKeepSecretValues(t *testing.T) {
	msg := `Get "https://api.example.com/?key=s3cr3t": dial tcp: no such host`
	got := hideSecrets(msg, map[string]string{"API_KEY": "s3cr3t"})
	if strings.Contains(got, "s3cr3t") || !strings.Contains(got, secretMask) {
		t.Errorf("hideSecrets = %q", got)
	}
}
