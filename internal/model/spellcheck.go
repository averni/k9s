package model

type SpellChecker interface {
	Candidates(word string) []Candidate
}

type Candidate struct {
	Word       string
	Suggestion string
	Score      int
}

var symbols = []rune("abcdefghijklmnopqrstuvwxyz-/.")

type NaiveSpellChecker struct {
	tree              *TernarySearchTree
	minimumWordLength int
}

func NewNaiveSpellChecker(tree *TernarySearchTree, minimumWordLen int) *NaiveSpellChecker {
	return &NaiveSpellChecker{
		tree:              tree,
		minimumWordLength: minimumWordLen,
	}
}

func (s *NaiveSpellChecker) delete(word string, candidates []string) []string {
	for i := 0; i < len(word); i++ {
		candidate := word[:i] + word[i+1:]
		if s.tree.HasPrefix(candidate) {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func (s *NaiveSpellChecker) transpose(word string, candidates []string) []string {
	buf := []rune(word)
	for i := 0; i < len(word)-1; i++ {
		buf[i], buf[i+1] = buf[i+1], buf[i]
		candidate := string(buf)
		if s.tree.HasPrefix(candidate) {
			candidates = append(candidates, candidate)
		}
		buf[i], buf[i+1] = buf[i+1], buf[i]
	}
	return candidates
}

func (s *NaiveSpellChecker) replace(word string, candidates []string) []string {
	for i := 0; i < len(word); i++ {
		node := s.tree.root.Get(word[:i])
		if node == nil {
			continue
		}
		for _, symbol := range symbols {
			if node.Get(string(symbol)+word[i+1:]) != nil {
				candidates = append(candidates, word[:i]+string(symbol)+word[i+1:])
			}
		}
	}
	return candidates
}

func (s *NaiveSpellChecker) insert(word string, candidates []string) []string {
	buf := make([]byte, len(word)+1)
	for i := 0; i < len(word)+1; i++ {
		for _, symbol := range symbols {
			copy(buf[:i], word[:i])
			copy(buf[i+1:], word[i:])
			buf[i] = byte(symbol)
			candidate := string(buf)
			if s.tree.HasPrefix(candidate) {
				candidates = append(candidates, candidate)
			}
		}
	}
	return candidates
}

func unique(words []string) []string {
	results := NewTernarySearchTree()
	for _, item := range words {
		if !results.Has(item) {
			results.Insert(item)
		}
	}
	return results.Words()
}

// variations returns a list of possible words by applying
// the following rules:
// 1. deletion: remove one letter when word length > 2
// 2. transpose: swap two adjacent letters
// 3. replace: change one letter to another
// 4. insertion: add a letter
func (s *NaiveSpellChecker) variations(word string) []string {
	candidates := make([]string, 0, 100)
	candidates = s.delete(word, candidates)
	candidates = s.transpose(word, candidates)
	candidates = s.replace(word, candidates)
	candidates = s.insert(word, candidates)
	candidates = unique(candidates)
	return candidates
}

// Candidates returns a list of all possible corrections for a given word at 1 edit distance.
func (s *NaiveSpellChecker) Candidates(word string) []Candidate {
	if len(word) < s.minimumWordLength {
		return nil
	}

	results := make([]Candidate, 0, 20)
	seen := newTernarySearchTreeNode(0)
	// expand sample word into variations valid for the tree
	for _, item := range s.variations(word) {
		// generate a list of possible corrections for each variation
		for _, suggestion := range s.tree.Autocomplete(item, sortByWord) {
			if len(suggestion) < len(word) {
				continue
			}
			prevSuggestion := seen.Get(suggestion)
			if prevSuggestion == nil || !prevSuggestion.isWord() {
				seen.Insert(&suggestion, len(results))
				results = append(results, Candidate{
					Word:       item,
					Suggestion: suggestion,
				})
			} else {
				results[prevSuggestion.Position()] = Candidate{
					Word:       item,
					Suggestion: suggestion,
				}
			}
		}
	}

	return results
}
