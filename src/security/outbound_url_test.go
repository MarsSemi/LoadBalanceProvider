package security

import (
	"net/http"
	"testing"
)

// -------------------------------------------------------------------------------------
func TestValidateOutboundURLAllowsPublicHTTPS(t *testing.T) {
	if _err := ValidateOutboundURL("https://93.184.216.34/v1/chat/completions"); _err != nil {
		t.Fatalf("ValidateOutboundURL returned error: %v", _err)
	}
}

// -------------------------------------------------------------------------------------
func TestValidateOutboundURLAllowsLocalhost(t *testing.T) {
	if _err := ValidateOutboundURL("http://localhost:8080/webhook"); _err != nil {
		t.Fatalf("ValidateOutboundURL rejected localhost: %v", _err)
	}
}

// -------------------------------------------------------------------------------------
func TestValidateOutboundURLAllowsPrivateIP(t *testing.T) {
	if _err := ValidateOutboundURL("http://192.168.1.10:8080/v1/models"); _err != nil {
		t.Fatalf("ValidateOutboundURL rejected private IP: %v", _err)
	}
}

// -------------------------------------------------------------------------------------
func TestValidateOutboundURLBlocksUnsupportedScheme(t *testing.T) {
	if _err := ValidateOutboundURL("file:///etc/passwd"); _err == nil {
		t.Fatalf("ValidateOutboundURL allowed unsupported scheme")
	}
}

// -------------------------------------------------------------------------------------
func TestGuardedHTTPClientBlocksLinkLocalRedirectTarget(t *testing.T) {
	_client := GuardedHTTPClient(&http.Client{})
	_req, _err := http.NewRequest(http.MethodGet, "http://169.254.1.1/private", nil)
	if _err != nil {
		t.Fatalf("NewRequest error = %v", _err)
	}
	if _client.CheckRedirect == nil {
		t.Fatalf("CheckRedirect is nil")
	}
	if _err := _client.CheckRedirect(_req, nil); _err == nil {
		t.Fatalf("CheckRedirect allowed link-local redirect target")
	}
}
