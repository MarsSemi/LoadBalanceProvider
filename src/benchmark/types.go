package benchmark

import "time"

// -------------------------------------------------------------------------------------
type StartRequest struct {
	ProviderID      string         `json:"provider_id"`
	Model           string         `json:"model"`
	Benchmarks      map[string]int `json:"benchmarks"`
	BatchSize       int            `json:"batch_size"`
	EnableThinking  bool           `json:"enable_thinking"`
	BenchmarkRoot   string         `json:"-"`
	ProviderName    string         `json:"-"`
	ProviderBaseURL string         `json:"-"`
	ProviderAPIKey  string         `json:"-"`
	ChatAPI         string         `json:"-"`
}

// -------------------------------------------------------------------------------------
type CatalogGroup struct {
	Title string        `json:"title"`
	Items []CatalogItem `json:"items"`
}

// -------------------------------------------------------------------------------------
type CatalogItem struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Options           []string `json:"options"`
	DefaultSampleSize int      `json:"default_sample_size"`
	RequiresExecution bool     `json:"requires_execution,omitempty"`
}

// -------------------------------------------------------------------------------------
type Question struct {
	ID          string   `json:"id"`
	Question    string   `json:"question"`
	Context     string   `json:"context,omitempty"`
	Choices     []string `json:"choices,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	Answer      string   `json:"answer"`
	Category    string   `json:"category,omitempty"`
	Prompt      string   `json:"prompt,omitempty"`
	TestCode    string   `json:"test_code,omitempty"`
	EntryPoint  string   `json:"entry_point,omitempty"`
	StdinInput  string   `json:"stdin_input,omitempty"`
	Stdout      string   `json:"stdout,omitempty"`
	BenchmarkID string   `json:"-"`
}

// -------------------------------------------------------------------------------------
type QuestionResult struct {
	QuestionID  string  `json:"question_id"`
	Correct     bool    `json:"correct"`
	Expected    string  `json:"expected"`
	Predicted   string  `json:"predicted"`
	DurationMS  int64   `json:"duration_ms"`
	Question    string  `json:"question,omitempty"`
	Category    string  `json:"category,omitempty"`
	RawResponse string  `json:"raw_response,omitempty"`
	Error       string  `json:"error,omitempty"`
	Score       float64 `json:"score"`
}

// -------------------------------------------------------------------------------------
type Result struct {
	BenchmarkID    string           `json:"benchmark_id"`
	BenchmarkName  string           `json:"benchmark_name"`
	Accuracy       float64          `json:"accuracy"`
	TotalQuestions int              `json:"total_questions"`
	CorrectCount   int              `json:"correct_count"`
	DurationMS     int64            `json:"duration_ms"`
	QuestionRows   []QuestionResult `json:"question_results,omitempty"`
}

// -------------------------------------------------------------------------------------
type Progress struct {
	BenchmarkID    string `json:"benchmark_id"`
	BenchmarkName  string `json:"benchmark_name"`
	Current        int    `json:"current"`
	Total          int    `json:"total"`
	BenchmarkIndex int    `json:"benchmark_index"`
	BenchmarkTotal int    `json:"benchmark_total"`
	Label          string `json:"label"`
}

// -------------------------------------------------------------------------------------
type Job struct {
	ID              string    `json:"id"`
	Status          string    `json:"status"`
	ProviderID      string    `json:"provider_id"`
	ProviderName    string    `json:"provider_name"`
	Model           string    `json:"model"`
	BatchSize       int       `json:"batch_size"`
	EnableThinking  bool      `json:"enable_thinking"`
	CreatedAt       time.Time `json:"created_at"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	EndedAt         time.Time `json:"ended_at,omitempty"`
	Error           string    `json:"error,omitempty"`
	Progress        Progress  `json:"progress"`
	Results         []Result  `json:"results"`
	Requested       []string  `json:"requested"`
	cancel          func()
	cancelRequested bool
}
