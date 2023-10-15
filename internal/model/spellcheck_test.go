package model_test

import (
	"testing"

	"github.com/derailed/k9s/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestNaiveSpellCheck(t *testing.T) {
	trie := newTernarySearchTree([]string{"po", "pod", "deploy", "deployment"})
	assert.NotNil(t, trie)
	spellchecker := model.NewNaiveSpellChecker(trie, 3)
	typos := []struct {
		typo     string
		expected []model.Candidate
	}{
		// {"pood", []model.Candidate{
		// 	{Word: "pod", Suggestion: "pod", Score: 0},
		// }},
		{"pdo", []model.Candidate{
			{Word: "pod", Suggestion: "pod", Score: 0},
		}},
		{"delpoy", []model.Candidate{
			{Word: "deploy", Suggestion: "deploy", Score: 0},
			{Word: "deploy", Suggestion: "deployment", Score: 0},
		}},
		{"deply", []model.Candidate{
			{Word: "deploy", Suggestion: "deploy", Score: 0},
			{Word: "deploy", Suggestion: "deployment", Score: 0},
		}},
		{"depoly", []model.Candidate{
			{Word: "deploy", Suggestion: "deploy", Score: 0},
			{Word: "deploy", Suggestion: "deployment", Score: 0},
		}},
		{"dployment", []model.Candidate{
			{Word: "deployment", Suggestion: "deployment", Score: 0},
		}},
	}

	for _, typo := range typos {
		assert.ElementsMatch(
			t, typo.expected, spellchecker.Candidates(typo.typo),
			"Suggestions do not match for typo %s", typo.typo,
		)
	}
}
