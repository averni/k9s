package model

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// Supported SuggestModes.
const (
	SuggestAutoComplete SuggestMode = iota + 1
	SuggestFullText
)

// ----------------------------------------------------------------------------
// TernarySearchTree data structures

// termData is used to store a pointer to the word and its position.
type wordData struct {
	WordPtr  *string
	Position int
	Refcount int
}

// TernarySearchTreeNode node data structure
type TernarySearchTreeNode struct {
	Left  *TernarySearchTreeNode
	Right *TernarySearchTreeNode
	Equal *TernarySearchTreeNode
	Value rune

	Data *wordData
}

// TernarySearchTreeNode node constructor
func newTernarySearchTreeNode(value rune) *TernarySearchTreeNode {
	return &TernarySearchTreeNode{
		Value: value,
	}
}

// Position returns the position for a given word if it exists. Otherwise, returns nil.
func (t *TernarySearchTreeNode) Position() int {
	if t.Data == nil {
		return -1
	}
	return t.Data.Position
}

// Insert adds a word to the tree with the given position.
// If the word already exists, the position is updated.
func (t *TernarySearchTreeNode) Insert(wordPtr *string, position int) {
	if wordPtr == nil || len(*wordPtr) == 0 {
		return
	}

	if t.Value == 0 {
		t.Value = rune((*wordPtr)[0])
	}

	node := t
	c := rune((*wordPtr)[0])
	for pos := 0; pos < len(*wordPtr); {
		if c < node.Value {
			if node.Left == nil {
				node.Left = newTernarySearchTreeNode(c)
			}
			node = node.Left
		} else if c > node.Value {
			if node.Right == nil {
				node.Right = newTernarySearchTreeNode(c)
			}
			node = node.Right
		} else {
			if pos == len(*wordPtr)-1 {
				break
			}
			pos++
			c = rune((*wordPtr)[pos])
			if node.Equal == nil {
				node.Equal = newTernarySearchTreeNode(c)
			}
			node = node.Equal
		}
	}

	if node.Data == nil {
		node.Data = &wordData{
			WordPtr:  wordPtr,
			Position: position,
			Refcount: 1,
		}
	} else {
		node.Data.Position = position
		node.Data.Refcount++
	}
}

// Get returns the node for a given word if it exists. Otherwise, returns nil.
func (t *TernarySearchTreeNode) Get(word string) *TernarySearchTreeNode {
	if len(word) == 0 {
		return nil
	}

	node := t
	for pos := 0; pos < len(word); {
		c := rune(word[pos])
		if c < node.Value {
			node = node.Left
		} else if c > node.Value {
			node = node.Right
		} else {
			pos++
			if pos == len(word) {
				break
			}
			node = node.Equal
		}
		if node == nil {
			break
		}
	}
	return node
}

func (t *TernarySearchTreeNode) isWord() bool {
	return t.Data != nil
}

func (t *TernarySearchTreeNode) Has(word string) bool {
	node := t.Get(word)
	return node != nil && node.isWord()
}

// Delete deletes a word from the tree. Returns true if the word was deleted.
// This method only deletes the word data from the node and does not delete the node itself.
func (t *TernarySearchTreeNode) Delete(word string) int {
	deleted := -1
	node := t.Get(word)

	if node != nil && node.isWord() {
		node.Data.Refcount--
		if node.Data.Refcount <= 0 {
			deleted = node.Position()
			node.Data = nil
		}
	}

	return deleted
}

// Walk visits the tree in in-order.
func (t *TernarySearchTreeNode) Walk(fn func(*TernarySearchTreeNode)) {
	if t.Left != nil {
		t.Left.Walk(fn)
	}

	fn(t)

	if t.Equal != nil {
		t.Equal.Walk(fn)
	}

	if t.Right != nil {
		t.Right.Walk(fn)
	}
}

// Suggest returns all words that start with prefix, ordered alphabetically.
func (t *TernarySearchTreeNode) PrefixSearch(prefix string) []*wordData {
	result := make([]*wordData, 0)
	prefixNode := t.Get(prefix)

	if prefixNode == nil {
		return result
	}

	if prefixNode.isWord() {
		result = append(result, prefixNode.Data)
	}

	if prefixNode.Equal != nil {
		prefixNode.Equal.Walk(func(node *TernarySearchTreeNode) {
			if node.isWord() {
				result = append(result, node.Data)
			}
		})
	}

	return result
}

type TernarySearchTree struct {
	root        *TernarySearchTreeNode
	words       []*string
	longestWord int
	length      int
	dirty       uint
}

type sortMode int

const (
	sortByWord sortMode = iota
	sortByPosition
)

func NewTernarySearchTree() *TernarySearchTree {
	return &TernarySearchTree{
		root:  newTernarySearchTreeNode(0),
		words: make([]*string, 0, 100),
	}
}

func (t *TernarySearchTree) Insert(word string) {
	t.root.Insert(&word, len(t.words))
	t.words = append(t.words, &word)
	t.length++
	if len(word) > t.longestWord {
		t.longestWord = len(word)
	}
}

func (t *TernarySearchTree) InsertAll(words []string) {
	wordPos := make(map[*string]int, len(words))
	for pos := range words {
		if !t.root.Has(words[pos]) {
			wordPos[&words[pos]] = len(t.words)
			t.words = append(t.words, &words[pos])
			if len(words[pos]) > t.longestWord {
				t.longestWord = len(words[pos])
			}
		}
	}
	for word, pos := range wordPos {
		t.root.Insert(word, pos)
	}
	t.length += len(wordPos)
}

func (t *TernarySearchTree) Has(word string) bool {
	return t.root.Has(word)
}

func (t *TernarySearchTree) HasPrefix(prefix string) bool {
	return t.root.Get(prefix) != nil
}

func (t *TernarySearchTree) Len() int {
	return t.length
}

func (t *TernarySearchTree) Delete(word string) {
	deleted := t.root.Delete(word)
	if deleted == -1 {
		return
	}
	t.words[deleted] = nil
	t.length--
	t.dirty++
}

func (t *TernarySearchTree) Words() []string {
	words := make([]string, 0, t.length)
	for _, word := range t.words {
		if word != nil {
			words = append(words, *word)
		}
	}
	return words
}

func (t *TernarySearchTree) Reset() {
	t.root = newTernarySearchTreeNode(0)
	t.length = 0
	t.dirty = 0
	t.longestWord = 0
	if len(t.words) > 0 {
		t.words = make([]*string, 0, 100)
	}
}

func (t *TernarySearchTree) Autocomplete(prefix string, sortBy sortMode) []string {
	if len(prefix) > t.longestWord {
		return nil
	}
	matches := t.root.PrefixSearch(prefix)
	if len(matches) > 0 {
		if sortBy == sortByPosition {
			sort.Slice(matches, func(i, j int) bool {
				return matches[i].Position < matches[j].Position
			})
		}
	}
	suggestions := make([]string, len(matches))
	for i, match := range matches {
		suggestions[i] = *match.WordPtr
	}
	return suggestions
}

const DIRTY_THRESHOLD = 0.33

// Sync synchronizes the tree with the given words
func (t *TernarySearchTree) Sync(words []string) {
	if len(words) == 0 {
		t.Reset()
		return
	}
	if t.dirty > uint(float64(t.length)*DIRTY_THRESHOLD) {
		t.Reset()
	}
	indexed := t.Words()
	t.InsertAll(words)
	seen := make(map[string]struct{}, len(words))
	for _, word := range words {
		seen[word] = struct{}{}
	}
	if len(indexed) > 0 {
		for _, word := range indexed {
			if _, ok := seen[word]; !ok {
				t.Delete(word)
			}
		}
	}
}

// unit test helpers
func (t *TernarySearchTree) GetSortModeByPosition() sortMode {
	return sortByPosition
}

func (t *TernarySearchTree) GetSortModeByWord() sortMode {
	return sortByWord
}

func StringSearch(terms []*string, text string, sortBy sortMode) []string {
	matches := make([]string, 0, 20)
	for _, term := range terms {
		if term == nil || *term == "" {
			continue
		}
		index := strings.Index(*term, text)
		if index != -1 {
			matches = append(matches, *term)
		}
	}
	if len(matches) > 0 && sortBy == sortByWord {
		sort.Slice(matches, func(i, j int) bool {
			return matches[i] < matches[j]
		})
	}
	return matches
}

// ----------------------------------------------------------------------------

type Autocompleter interface {
	Index(string, []string)
	Suggest(text string) sort.StringSlice
}

type UpdateFn func(s Autocompleter)

type PromptAutocompleter struct {
	cmdHistoryTst *TernarySearchTree
	aliasTst      *TernarySearchTree
	namespacesTst *TernarySearchTree
	configSetTst  *TernarySearchTree

	mode            SuggestMode
	refreshRate     time.Duration
	updateFn        UpdateFn
	lastRefreshTime time.Time
	cluster         string
	context         string
	mx              sync.RWMutex
	refreshMx       sync.RWMutex
}

func NewPromptAutocompleter(updateFn UpdateFn, refreshRate time.Duration) *PromptAutocompleter {
	return &PromptAutocompleter{
		cmdHistoryTst:   NewTernarySearchTree(),
		aliasTst:        NewTernarySearchTree(),
		namespacesTst:   NewTernarySearchTree(),
		configSetTst:    NewTernarySearchTree(),
		mode:            SuggestAutoComplete,
		updateFn:        updateFn,
		refreshRate:     refreshRate,
		lastRefreshTime: time.Now().Add(-2 * refreshRate * time.Second),
	}
}

func (p *PromptAutocompleter) Reset() {
	p.mx.Lock()
	defer p.mx.Unlock()

	p.cmdHistoryTst.Reset()
	p.aliasTst.Reset()
	p.namespacesTst.Reset()
}

func (p *PromptAutocompleter) Index(name string, words []string) {
	p.mx.Lock()
	defer p.mx.Unlock()

	switch name {
	case "history":
		// reverse history to move most recent commands at the end
		for i, j := 0, len(words)-1; i < j; i, j = i+1, j-1 {
			words[i], words[j] = words[j], words[i]
		}
		p.cmdHistoryTst.Sync(words)
	case "aliases":
		p.aliasTst.Sync(words)
	case "namespaces":
		p.namespacesTst.Sync(words)
	case "k9sconfig-set":
		p.configSetTst.Sync(words)
	}
}

func (p *PromptAutocompleter) needRefresh() bool {
	defer p.mx.RUnlock()
	p.mx.RLock()

	return time.Since(p.lastRefreshTime) > p.refreshRate
}

// ForceRefresh forces a refresh of the autocompleter on the next call to NeedRefresh
func (p *PromptAutocompleter) forceRefresh() {
	defer p.mx.Unlock()
	p.mx.Lock()

	p.lastRefreshTime = time.Now().Add(-2 * p.refreshRate)
}

func (p *PromptAutocompleter) refreshed() {
	p.lastRefreshTime = time.Now()
}

func (p *PromptAutocompleter) Update() {
	defer p.refreshMx.Unlock()
	p.refreshMx.Lock()

	if p.needRefresh() {
		p.updateFn(p)
		p.refreshed()
	}
}

func (p *PromptAutocompleter) All() sort.StringSlice {
	p.mx.RLock()
	defer p.mx.RUnlock()

	entries := make(sort.StringSlice, 0, p.aliasTst.Len()+p.namespacesTst.Len()+p.cmdHistoryTst.Len())

	aliases := p.aliasTst.Words()
	sort.Strings(aliases)
	entries = append(entries, aliases...)

	if p.mode == SuggestFullText {
		namespaces := p.namespacesTst.Words()
		sort.Strings(namespaces)
		entries = append(entries, namespaces...)
	}

	commands := p.cmdHistoryTst.Words()
	if len(commands) > 0 {
		entries = append(entries, commands...)
	}

	return entries
}

func (p *PromptAutocompleter) Search(text string) sort.StringSlice {
	p.mx.RLock()
	defer p.mx.RUnlock()

	entries := make(sort.StringSlice, 0, 20)

	text = strings.ToLower(text)

	entries = append(entries, StringSearch(p.cmdHistoryTst.words, text, sortByPosition)...)

	entries = append(entries, StringSearch(p.namespacesTst.words, text, sortByWord)...)

	entries = append(entries, StringSearch(p.aliasTst.words, text, sortByWord)...)

	return entries
}

// ----------------------------------------------------------------------------

// Autocomplete returns all terms that start with prefix.
func (p *PromptAutocompleter) Autocomplete(text string) sort.StringSlice {
	p.mx.RLock()
	defer p.mx.RUnlock()

	entries := make(sort.StringSlice, 0, 20)
	if len(text) > 0 && text[0] == ' ' {
		return entries
	}

	text = strings.ToLower(text)

	// split text into terms
	terms := strings.Fields(text)
	if len(terms) == 1 && text[len(text)-1] == ' ' {
		terms = append(terms, "")
	}

	// autocomplete history
	matches := p.cmdHistoryTst.Autocomplete(text, sortByPosition)
	if len(matches) > 0 {
		// reorder for reverse lookup
		entries = append(entries, matches[len(matches)-1])
		entries = append(entries, matches[:len(matches)-1]...)
	}

	switch len(terms) {
	case 1:
		// autocomplete aliases only if there is no match in history
		if len(entries) == 0 {
			entries = append(entries, p.aliasTst.Autocomplete(text, sortByWord)...)
		}
	case 2:
		// don't autocomplete for blanks after the second term
		if len(terms[1]) > 0 && text[len(text)-1] == ' ' {
			break
		}
		var targetTst *TernarySearchTree
		if p.isResourceNamepaced(terms[0]) {
			targetTst = p.namespacesTst
		} else if terms[0] == "k9sconfig-set" {
			targetTst = p.configSetTst
		} else {
			break
		}
		if terms[1] == "" {
			entries = append(entries, targetTst.Words()...)
		} else {
			matches := targetTst.Autocomplete(terms[1], sortByWord)
			if len(matches) > 0 {
				blankIndex := strings.LastIndex(text, " ")
				for _, suggest := range matches {
					suggestion := text[:blankIndex+1] + suggest
					if !p.cmdHistoryTst.Has(suggestion) {
						entries = append(entries, suggestion)
					}
				}
			}
		}
	}
	return entries
}

var disableNamespaceFor = map[string]bool{
	"alias":               true,
	"aliases":             true,
	"clusterrole":         true,
	"clusterroles":        true,
	"clusterrolebinding":  true,
	"clusterrolebindings": true,
	"context":             true,
	"contexts":            true,
	"cr":                  true,
	"crb":                 true,
	"csr":                 true,
	"ctx":                 true,
	"namespace":           true,
	"namespaces":          true,
	"ns":                  true,
	"k9sconfig-set":       true,
}

// isResourceNamepaced returns true if the resource is namespaced.
// TODO: this is a temporary hack
func (p *PromptAutocompleter) isResourceNamepaced(res string) bool {
	_, ok := disableNamespaceFor[res]
	return !ok
}

// ----------------------------------------------------------------------------

func (p *PromptAutocompleter) SuggestModeChanged(mode SuggestMode) {
	p.mode = mode
	p.Update()
}

func (p *PromptAutocompleter) Suggest(text string) sort.StringSlice {
	if text == "" {
		return p.All()
	}
	switch p.mode {
	case SuggestAutoComplete:
		return p.Autocomplete(text)
	case SuggestFullText:
		return p.Search(text)
	}
	return nil
}

// ----------------------------------------------------------------------------
// Listeners:

// ClusterInfoChanged implements ClusterInfoListener.
func (p *PromptAutocompleter) ClusterInfoChanged(prev ClusterMeta, curr ClusterMeta) {
	if curr.Cluster != "n/a" && p.cluster == curr.Cluster && p.context != "n/a" && p.context == curr.Context {
		return
	}
	p.cluster = curr.Cluster
	p.context = curr.Context

	p.Reset()
	p.forceRefresh()
}

// ClusterInfoUpdated implements ClusterInfoListener.
func (*PromptAutocompleter) ClusterInfoUpdated(ClusterMeta) {}

// BufferCompleted is called when the buffer is completed
func (p *PromptAutocompleter) BufferCompleted(text, suggestion string) {}

// BufferChanged is called when the buffer is changed
func (p *PromptAutocompleter) BufferChanged(text, suggestion string) {}

// BufferActive is called when the buffer is active
func (p *PromptAutocompleter) BufferActive(state bool, kind BufferKind) {
	if state {
		p.Update()
	}
}

func (p *PromptAutocompleter) HistoryChanged(commands []string) {
	p.Index("history", commands)
}
