package codexauth

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type refreshGate struct {
	lock      chan struct{}
	lastError error
	failedAt  time.Time
}

var refreshGates sync.Map

func (s *Store) withRefreshGate(ctx context.Context, id string, action func() (Auth, error)) (Auth, error) {
	if strings.TrimSpace(id) == "" {
		return Auth{}, fmt.Errorf("provider id is required")
	}
	entry, _ := refreshGates.LoadOrStore(s.Path+"\x00"+strings.TrimSpace(id), &refreshGate{lock: make(chan struct{}, 1)})
	gate := entry.(*refreshGate)
	select {
	case gate.lock <- struct{}{}:
	case <-ctx.Done():
		return Auth{}, ctx.Err()
	}
	defer func() { <-gate.lock }()
	if ctx.Err() != nil {
		return Auth{}, ctx.Err()
	}
	if gate.lastError != nil && time.Since(gate.failedAt) < 2*time.Second {
		return Auth{}, gate.lastError
	}
	auth, err := action()
	if ctx.Err() == nil {
		gate.lastError = err
		gate.failedAt = time.Now()
	}
	return auth, err
}

func (s *Store) EnsureContext(ctx context.Context, id string) (Auth, error) {
	return s.withRefreshGate(ctx, id, func() (Auth, error) { return s.ensureLocked(ctx, id) })
}

func RefreshAfterUnauthorized(ctx context.Context, id, failedAccessToken string) (Auth, error) {
	return NewStore(defaultTokenPath).refreshAfterUnauthorized(ctx, id, failedAccessToken)
}

func (s *Store) refreshAfterUnauthorized(ctx context.Context, id, failedAccessToken string) (Auth, error) {
	return s.withRefreshGate(ctx, id, func() (Auth, error) {
		record, err := s.Get(id)
		if err != nil {
			return Auth{}, err
		}
		if record.AccessToken != failedAccessToken && tokenUsable(record.ExpiresAt) && record.AccessToken != "" {
			return Auth{AccessToken: record.AccessToken, AccountID: accountID(record)}, nil
		}
		if record.RefreshToken == "" {
			return Auth{}, fmt.Errorf("codex oauth refresh token is missing")
		}
		token, err := refreshTokenContext(ctx, record)
		if err != nil {
			return Auth{}, err
		}
		next := mergeToken(record, token)
		if err := s.Save(next); err != nil {
			return Auth{}, err
		}
		return Auth{AccessToken: next.AccessToken, AccountID: accountID(next)}, nil
	})
}
