package security

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// -------------------------------------------------------------------------------------
func GuardedHTTPClient(_client *http.Client) *http.Client {
	if _client == nil {
		_client = &http.Client{}
	}
	_guarded := *_client
	_previousCheckRedirect := _guarded.CheckRedirect
	_guarded.CheckRedirect = func(_req *http.Request, _via []*http.Request) error {
		if _req == nil {
			return fmt.Errorf("redirect request is required")
		}
		if _err := ValidateOutboundParsedURL(_req.URL); _err != nil {
			return _err
		}
		if _previousCheckRedirect != nil {
			return _previousCheckRedirect(_req, _via)
		}
		if len(_via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	return &_guarded
}

// -------------------------------------------------------------------------------------
func ValidateOutboundURL(_rawURL string) error {
	_parsed, _err := url.Parse(strings.TrimSpace(_rawURL))
	if _err != nil {
		return fmt.Errorf("outbound URL is not valid: %w", _err)
	}
	return ValidateOutboundParsedURL(_parsed)
}

// -------------------------------------------------------------------------------------
func ValidateOutboundParsedURL(_parsed *url.URL) error {
	if _parsed == nil {
		return fmt.Errorf("outbound URL is required")
	}
	_scheme := strings.ToLower(strings.TrimSpace(_parsed.Scheme))
	if _scheme != "http" && _scheme != "https" {
		return fmt.Errorf("outbound URL scheme must be http or https")
	}
	_host := strings.ToLower(strings.TrimSpace(_parsed.Hostname()))
	if _host == "" {
		return fmt.Errorf("outbound URL host is required")
	}

	_addresses, _err := resolveOutboundHost(_host)
	if _err != nil {
		return _err
	}
	for _, _address := range _addresses {
		if isRestrictedOutboundAddr(_address) {
			return fmt.Errorf("outbound URL host %q resolves to restricted address %s", _host, _address.String())
		}
	}
	return nil
}

// -------------------------------------------------------------------------------------
func resolveOutboundHost(_host string) ([]netip.Addr, error) {
	_host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(_host)), ".")
	if _addr, _err := netip.ParseAddr(_host); _err == nil {
		return []netip.Addr{normalizeOutboundAddr(_addr)}, nil
	}

	_ctx, _cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer _cancel()
	_addresses, _err := net.DefaultResolver.LookupNetIP(_ctx, "ip", _host)
	if _err != nil {
		return nil, fmt.Errorf("outbound URL host %q cannot be resolved: %w", _host, _err)
	}
	if len(_addresses) == 0 {
		return nil, fmt.Errorf("outbound URL host %q resolved no addresses", _host)
	}
	for _idx := range _addresses {
		_addresses[_idx] = normalizeOutboundAddr(_addresses[_idx])
	}
	return _addresses, nil
}

// -------------------------------------------------------------------------------------
func normalizeOutboundAddr(_addr netip.Addr) netip.Addr {
	if _addr.Is4In6() {
		return _addr.Unmap()
	}
	return _addr
}

// -------------------------------------------------------------------------------------
func isRestrictedOutboundAddr(_addr netip.Addr) bool {
	_addr = normalizeOutboundAddr(_addr)
	return !_addr.IsValid() ||
		_addr.IsLinkLocalUnicast() ||
		_addr.IsUnspecified() ||
		_addr.IsMulticast()
}
