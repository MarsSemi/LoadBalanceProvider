package balancer

import "testing"

func TestNormalizeTokenRateReplacesImplausibleTransportBurst(t *testing.T) {
	if _got := NormalizeTokenRate(120, 3000, 32442); _got != 40 {
		t.Fatalf("normalized rate = %.1f, want 40.0", _got)
	}
}

func TestNormalizeTokenRateKeepsSupportedHighRate(t *testing.T) {
	if _got := NormalizeTokenRate(4000, 2000, 1800); _got != 1800 {
		t.Fatalf("normalized rate = %.1f, want 1800.0", _got)
	}
}
