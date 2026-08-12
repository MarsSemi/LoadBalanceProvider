package benchmark

// -------------------------------------------------------------------------------------
func Catalog() []CatalogGroup {
	return []CatalogGroup{
		{
			Title: "COMMONSENSE & REASONING",
			Items: []CatalogItem{
				{ID: "hellaswag", Name: "HellaSwag", Description: "Commonsense reasoning", Options: []string{"20", "50", "200", "500", "Full"}, DefaultSampleSize: 20},
				{ID: "arc_challenge", Name: "ARC-C", Description: "Science reasoning", Options: []string{"20", "50", "300", "1000", "Full"}, DefaultSampleSize: 20},
				{ID: "winogrande", Name: "Winogrande", Description: "Coreference resolution", Options: []string{"20", "50", "300", "1000", "Full"}, DefaultSampleSize: 20},
				{ID: "truthfulqa", Name: "TruthfulQA", Description: "Truthfulness", Options: []string{"20", "50", "300", "Full (817)"}, DefaultSampleSize: 20},
			},
		},
		{
			Title: "MATH",
			Items: []CatalogItem{
				{ID: "gsm8k", Name: "GSM8K", Description: "Math reasoning", Options: []string{"20", "50", "100", "300", "Full"}, DefaultSampleSize: 20},
				{ID: "mathqa", Name: "MathQA", Description: "Quantitative reasoning - 5-way", Options: []string{"20", "50", "300", "1000", "Full"}, DefaultSampleSize: 20},
			},
		},
		{
			Title: "CODING",
			Items: []CatalogItem{
				{ID: "humaneval", Name: "HumanEval", Description: "Function completion", Options: []string{"20", "50", "100", "Full (164)"}, DefaultSampleSize: 20, RequiresExecution: true},
				{ID: "mbpp", Name: "MBPP", Description: "Python problems", Options: []string{"20", "50", "200", "500", "Full"}, DefaultSampleSize: 20, RequiresExecution: true},
				{ID: "livecodebench", Name: "LiveCodeBench", Description: "Code generation", Options: []string{"20", "50", "100", "300", "Full"}, DefaultSampleSize: 20, RequiresExecution: true},
			},
		},
		{
			Title: "SAFETY & ALIGNMENT",
			Items: []CatalogItem{
				{ID: "bbq", Name: "BBQ", Description: "Social bias - 11 categories", Options: []string{"20", "50", "300", "1000", "Full"}, DefaultSampleSize: 20},
				{ID: "safetybench", Name: "SafetyBench", Description: "Safety - 7 categories", Options: []string{"20", "50", "300", "1000", "Full"}, DefaultSampleSize: 20},
			},
		},
		{
			Title: "KNOWLEDGE",
			Items: []CatalogItem{
				{ID: "mmlu", Name: "MMLU", Description: "Knowledge - 57 subjects", Options: []string{"20", "50", "300", "1000", "Full"}, DefaultSampleSize: 20},
				{ID: "mmlu_pro", Name: "MMLU-Pro", Description: "Hard knowledge - 14 subjects (10-way)", Options: []string{"20", "50", "300", "1000", "Full"}, DefaultSampleSize: 20},
				{ID: "kmmlu", Name: "KMMLU", Description: "Korean knowledge - 45 subjects", Options: []string{"20", "50", "300", "1000", "Full"}, DefaultSampleSize: 20},
				{ID: "cmmlu", Name: "CMMLU", Description: "Chinese knowledge - 67 subjects", Options: []string{"20", "50", "300", "1000", "Full"}, DefaultSampleSize: 20},
				{ID: "jmmlu", Name: "JMMLU", Description: "Japanese knowledge - 112 subjects", Options: []string{"20", "50", "300", "1000", "Full"}, DefaultSampleSize: 20},
			},
		},
	}
}

// -------------------------------------------------------------------------------------
func CatalogByID() map[string]CatalogItem {
	_result := map[string]CatalogItem{}
	for _, _group := range Catalog() {
		for _, _item := range _group.Items {
			_result[_item.ID] = _item
		}
	}
	return _result
}
