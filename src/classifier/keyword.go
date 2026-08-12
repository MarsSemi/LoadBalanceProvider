package classifier

import (
	"context"

	"LoadBalanceProvider/src/analyzer"
	"LoadBalanceProvider/src/domain"
)

// -------------------------------------------------------------------------------------
type Keyword struct {
	Analyzer *analyzer.RequestAnalyzer
}

// -------------------------------------------------------------------------------------
func NewKeyword() *Keyword {
	return &Keyword{Analyzer: analyzer.New()}
}

// -------------------------------------------------------------------------------------
func (_k *Keyword) Name() string {
	return "keyword"
}

// -------------------------------------------------------------------------------------
func (_k *Keyword) Classify(_ctx context.Context, _req *domain.ChatCompletionRequest, _fallback domain.RequestProfile) (domain.RequestProfile, bool, error) {
	if _k.Analyzer == nil {
		_k.Analyzer = analyzer.New()
	}
	return _k.Analyzer.Analyze(_req), true, nil
}

// -------------------------------------------------------------------------------------
