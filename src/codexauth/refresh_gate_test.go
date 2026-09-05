package codexauth

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type refreshSmokeTransport func(*http.Request) (*http.Response, error)

func (f refreshSmokeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOAuthRefreshDeduplicatedSmoke(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err := s.Save(OAuthTokenRecord{ProviderID: "p", AccessToken: "old", RefreshToken: "test-refresh", ExpiresAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	var calls atomic.Int32
	http.DefaultTransport = refreshSmokeTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"new","refresh_token":"new-refresh","expires_in":3600}`))}, nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			auth, err := NewStore(s.Path).EnsureContext(context.Background(), "p")
			if err != nil || auth.AccessToken != "new" {
				t.Errorf("refresh: %v", err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("refresh calls=%d", calls.Load())
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.refreshAfterUnauthorized(context.Background(), "p", "old")
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("already-replaced token was refreshed again")
	}
}
