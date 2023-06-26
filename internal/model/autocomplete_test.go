package model_test

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/derailed/k9s/internal/model"
	"github.com/stretchr/testify/assert"
)

func newTernarySearchTree(words []string) *model.TernarySearchTree {
	trie := model.NewTernarySearchTree()
	trie.InsertAll(words)
	// for _, word := range words {
	// 	trie.Insert(word)
	// }
	return trie
}

func TestTernarySearchTreeInsert(t *testing.T) {
	trie := newTernarySearchTree([]string{"po", "pod", "pod test", "mycrd"})
	assert.NotNil(t, trie)

	assert.True(t, trie.Has("pod"))
	assert.True(t, trie.Has("pod test"))
	assert.True(t, trie.Has("mycrd"))
	assert.False(t, trie.Has("notfound"))
	assert.Equal(t, 4, trie.Len())
}

func TestTernarySearchTreeDelete(t *testing.T) {
	trie := newTernarySearchTree([]string{"po", "pod", "pod test", "mycrd"})
	assert.NotNil(t, trie)

	trie.Delete("pod")
	assert.False(t, trie.Has("pod"))
	assert.True(t, trie.Has("pod test"))
	assert.True(t, trie.Has("mycrd"))
	assert.False(t, trie.Has("notfound"))
	assert.Equal(t, 3, trie.Len())
	trie.Delete("pod test")
	assert.False(t, trie.Has("pod test"))
	assert.Equal(t, 2, trie.Len())
}

func TestTernarySearchTreeSearch(t *testing.T) {
	terms := []string{
		"po", "pod", "pod test", "mycrd", "pod oddpo",
	}
	termsPtrs := make([]*string, len(terms))
	for i := range terms {
		termsPtrs[i] = &terms[i]
	}
	trie := newTernarySearchTree(terms)

	assert.ElementsMatch(t, []string{"po", "pod", "pod test", "pod oddpo"}, model.StringSearch(termsPtrs, "p", trie.GetSortModeByWord()))
	assert.ElementsMatch(t, []string{"pod", "pod test", "pod oddpo"}, model.StringSearch(termsPtrs, "od", trie.GetSortModeByWord()))

}

func TestTernarySearchTreeSuggest(t *testing.T) {
	trie := newTernarySearchTree([]string{"pod", "po test", "mycrd"})
	assert.NotNil(t, trie)

	assert.ElementsMatch(t, []string{"pod", "po test"}, trie.Autocomplete("po", trie.GetSortModeByWord()))
	assert.ElementsMatch(t, []string{"pod", "po test"}, trie.Autocomplete("p", trie.GetSortModeByWord()))
	assert.Equal(t, []string{"pod"}, trie.Autocomplete("pod", trie.GetSortModeByWord()))
	assert.Equal(t, []string{}, trie.Autocomplete("mycrds", trie.GetSortModeByWord()))
}

func TestTernarySearchTreeSync(t *testing.T) {
	trie := newTernarySearchTree([]string{"pod", "po test", "mycrd"})
	assert.NotNil(t, trie)

	newHistory := []string{"pod", "po test", "mycrd", "new", "new2"}

	trie.Sync(newHistory)

	assert.ElementsMatch(t, newHistory, trie.Words())

	newHistory = []string{"mycrd", "new", "new2", "new3"}
	trie.Sync(newHistory)

	assert.ElementsMatch(t, newHistory, trie.Words())
}

func TestNewPromptAutocompleter(t *testing.T) {
	updateFn := func(s model.Autocompleter) {
		s.Index("history", []string{"history1", "history2 ns2"})
		s.Index("aliases", []string{"alias1", "alias2"})
		s.Index("namespaces", []string{"ns1", "ns2"})
	}

	promptAutocompleter := model.NewPromptAutocompleter(updateFn, 200)
	fishBuff := model.NewFishBuff('>', model.CommandBuffer)
	fishBuff.AddListenerWithPriority(promptAutocompleter, 3)
	fishBuff.AddSuggestModeListener(promptAutocompleter)

	userInputs := []struct {
		input       string
		suggestions sort.StringSlice
	}{
		{"a", sort.StringSlice{"alias1", "alias2"}},
		{"ali", sort.StringSlice{"alias1", "alias2"}},
		{"alias2", sort.StringSlice{"alias2"}},
		{"alias1 n", sort.StringSlice{"alias1 ns1", "alias1 ns2"}},
		{"history2 n", sort.StringSlice{"history2 ns2", "history2 ns1"}},
	}

	for _, u := range userInputs {
		fishBuff.SetActive(true)
		for _, r := range u.input {
			fishBuff.Add(r)
		}
		suggestions := promptAutocompleter.Suggest(fishBuff.GetText())
		assert.Equal(t, u.suggestions, suggestions, "Suggestions do not match for input %s", u.input)
		fishBuff.Reset()
	}
}

func historyForBenchmarks(sorted bool) []string {

	history := make([]string, 0)

	history = append(history, alias...)

	for i := 0; i < 20; i++ {
		crd := alias[i%len(alias)]
		ns := namespaces[i%len(namespaces)]
		history = append(history, fmt.Sprintf("%s %s", crd, ns))
	}

	if sorted {
		sort.Strings(history)
	} else {
		rand.Shuffle(len(history), func(i, j int) {
			history[i], history[j] = history[j], history[i]
		})
	}
	return history
}

func linearContains(history []string, searchText string) []string {
	results := make([]string, 0, int(float64(len(history))*0.3))
	for _, h := range history {
		if strings.Contains(h, searchText) {
			results = append(results, h)
		}
	}
	return results
}

func linearHasPrefix(history []string, searchText string) []string {
	results := make([]string, 0, int(float64(len(history))*0.3))
	for _, h := range history {
		if strings.HasPrefix(h, searchText) {
			results = append(results, h)
		}
	}
	return results
}

func benchmarkAutocomplete(b *testing.B, n int, searchFn func(string) []string) {
	history := historyForBenchmarks(false)
	allSearchTexts := []string{
		"po", "de", "ing", "svc", "secre", "confi", "cm", "pv", "pvc", "sa", "ns", "ro", "crd", "cr", "job", "ds", "sts", "rs", "deploy",
	}
	for i := 0 + n; i < 10; i++ {
		allSearchTexts = append(allSearchTexts, history[i])
	}
	for i := 1 + n; i < 10; i++ {
		allSearchTexts = append(allSearchTexts, history[len(history)-i])
	}
	searchTexts := make([]string, 0)
	if n > 0 {
		for _, search := range allSearchTexts {
			if len(search) >= n {
				searchTexts = append(searchTexts, search[:n])
			}
		}
	} else {
		searchTexts = allSearchTexts
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, searchText := range searchTexts {
			for j := 1; j < len(searchText)+1; j++ {
				matches := searchFn(searchText[:j])
				_ = matches
			}
		}
	}
}

func BenchmarkAutocompleteBaselineSearch(b *testing.B) {
	history := historyForBenchmarks(false)
	b.ResetTimer()

	benchmarkAutocomplete(b, 0, func(searchText string) []string {
		return linearContains(history, searchText)
	})
}

func BenchmarkAutocompleteBaselineAutocomplete(b *testing.B) {
	history := historyForBenchmarks(false)
	b.ResetTimer()

	benchmarkAutocomplete(b, 0, func(searchText string) []string {
		return linearHasPrefix(history, searchText)
	})
}

func BenchmarkAutocompleteBaselineAutocomplete1Char(b *testing.B) {
	history := historyForBenchmarks(false)
	b.ResetTimer()

	benchmarkAutocomplete(b, 1, func(searchText string) []string {
		return linearHasPrefix(history, searchText)
	})
}

func BenchmarkAutocompleteBaselineAutocomplete2Char(b *testing.B) {
	history := historyForBenchmarks(false)
	b.ResetTimer()

	benchmarkAutocomplete(b, 2, func(searchText string) []string {
		return linearHasPrefix(history, searchText)
	})
}

func BenchmarkAutocompleteBaselineAutocomplete5Char(b *testing.B) {
	history := historyForBenchmarks(false)
	b.ResetTimer()

	benchmarkAutocomplete(b, 5, func(searchText string) []string {
		return linearHasPrefix(history, searchText)
	})
}

func BenchmarkAutocompleteBaselineAutocomplete8Char(b *testing.B) {
	history := historyForBenchmarks(false)
	b.ResetTimer()

	benchmarkAutocomplete(b, 8, func(searchText string) []string {
		return linearHasPrefix(history, searchText)
	})
}

func BenchmarkAutocompleteTernarySearchTreeAutocomplete(b *testing.B) {
	trie := newTernarySearchTree(historyForBenchmarks(false))
	b.ResetTimer()

	benchmarkAutocomplete(b, 0, func(searchText string) []string {
		return trie.Autocomplete(searchText, trie.GetSortModeByWord())
	})
}

func BenchmarkAutocompleteTernarySearchTreeAutocomplete1Char(b *testing.B) {
	trie := newTernarySearchTree(historyForBenchmarks(false))
	b.ResetTimer()

	benchmarkAutocomplete(b, 1, func(searchText string) []string {
		return trie.Autocomplete(searchText, trie.GetSortModeByWord())
	})
}

func BenchmarkAutocompleteTernarySearchTreeAutocomplete2Char(b *testing.B) {
	trie := newTernarySearchTree(historyForBenchmarks(false))
	b.ResetTimer()

	benchmarkAutocomplete(b, 2, func(searchText string) []string {
		return trie.Autocomplete(searchText, trie.GetSortModeByWord())
	})
}

func BenchmarkAutocompleteTernarySearchTreeAutocomplete5Char(b *testing.B) {
	trie := newTernarySearchTree(historyForBenchmarks(false))
	b.ResetTimer()

	benchmarkAutocomplete(b, 5, func(searchText string) []string {
		return trie.Autocomplete(searchText, trie.GetSortModeByWord())
	})
}

func BenchmarkAutocompleteTernarySearchTreeAutocomplete8Char(b *testing.B) {
	trie := newTernarySearchTree(historyForBenchmarks(false))
	b.ResetTimer()

	benchmarkAutocomplete(b, 8, func(searchText string) []string {
		return trie.Autocomplete(searchText, trie.GetSortModeByWord())
	})
}

func BenchmarkAutocompleteTernarySearchTreeSearch(b *testing.B) {
	terms := historyForBenchmarks(false)
	termsPtrs := make([]*string, len(terms))
	for i, term := range terms {
		termsPtrs[i] = &term
	}
	trie := newTernarySearchTree(terms)
	b.ResetTimer()

	benchmarkAutocomplete(b, 0, func(searchText string) []string {
		return model.StringSearch(termsPtrs, searchText, trie.GetSortModeByWord())
	})

}

func BenchmarkAutocompleteTernarySearchTreeRebuild(b *testing.B) {
	history := historyForBenchmarks(false)
	trie := newTernarySearchTree(history)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		trie.Reset()
		for _, h := range history {
			trie.Insert(h)
		}
	}
}

func BenchmarkAutocompleteTernarySearchTreeUpdate(b *testing.B) {
	history := historyForBenchmarks(false)
	split := int(float64(len(history)) * 0.75)
	deleteSplit := int(float64(len(history)) * 0.1)
	historyTrie := history[:split]
	historyNew := history[deleteSplit:]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		trie := newTernarySearchTree(historyTrie)
		b.StartTimer()

		trie.Sync(historyNew)
	}
}

// ----------------------------------------------------------------------------
// test data

var alias []string = []string{
	"po", "dp", "rs", "sts", "ds", "svc", "ep", "ing", "cm", "sec", "sa", "ns", "no", "noe", "noi", "pvc", "pv", "sc", "cs", "crd", "cr",
	"quota", "hpa", "nsquota", "nsquotas", "nsq", "nsqs", "nsqo", "nsqos", "nsqod", "nsqods", "nsqoe", "nsqoes", "nsqoi", "nsqois",
	"help", "quit", "aliases", "popeye", "helm", "dir", "contexts", "users", "groups", "portforwards", "benchmarks", "screendumps", "pulses", "xrays",
	"addon", "addonlist", "addons", "all", "apiserver", "apiserverlist", "apiservers", "apiservice", "apiservicelist", "apiservices",
	"app", "applist", "apps", "authconfig", "authconfiglist", "authconfigs", "backend", "backendlist",
	"backends", "backingimage", "backingimagedatasource", "backingimagedatasourcelist", "backingimagedatasources", "backingimagelist",
	"backingimagemanager", "backingimagemanagerlist", "backingimagemanagers", "backingimages", "backup", "backuplist", "backups",
	"backuptarget", "backuptargetlist", "backuptargets", "backupvolume", "backupvolumelist", "backupvolumes",
	"bgpconfiguration", "bgpconfigurationlist", "bgpconfigurations", "bgppeer", "bgppeerlist", "bgppeers", "blockaffinities",
	"blockaffinity", "blockaffinitylist", "caliconodestatus", "caliconodestatuses", "caliconodestatuslist", "catalog", "cluster",
	"clusterinformation", "clusterinformationlist", "clusterinformations", "clusterlist", "clusterregistrationtoken",
	"clusterregistrationtokenlist", "clusterregistrationtokens", "clusterrepo", "clusterrepolist", "clusterrepos", "clusters",
	"defaults", "defaultslist", "engine", "engineimage", "engineimagelist", "engineimages", "enginelist", "engines", "feature",
	"featurelist", "features", "felixconfiguration", "felixconfigurationlist", "felixconfigurations", "global", "globallist",
	"globalnetworkpolicies", "globalnetworkpolicy", "globalnetworkpolicylist", "globalnetworkset", "globalnetworksetlist",
	"globalnetworksets", "globals", "group", "grouplist", "groupmember", "groupmemberlist", "groupmembers", "groups", "helmchart",
	"helmchartconfig", "helmchartconfiglist", "helmchartconfigs", "helmchartlist", "helmcharts", "hostendpoint", "hostendpointlist",
	"hostendpoints", "imageset", "imagesetlist", "imagesets", "installation", "installationlist", "installations", "instancemanager",
	"instancemanagerlist", "instancemanagers", "ipamblock", "ipamblocklist", "ipamblocks", "ipamconfig", "ipamconfiglist",
	"ipamconfigs", "ipamhandle", "ipamhandlelist", "ipamhandles", "ippool", "ippoollist", "ippools", "ipreservation",
	"ipreservationlist", "ipreservations", "kubecontrollersconfiguration", "kubecontrollersconfigurationlist",
	"kubecontrollersconfigurations", "lhb", "lhbi", "lhbids", "lhbim", "lhbt", "lhbundle", "lhbv", "lhe", "lhei", "lhim", "lhn", "lho",
	"lhr", "lhrj", "lhs", "lhsb", "lhsm", "lhsnap", "lhsr", "lhv", "navlink", "navlinklist", "navlinks", "networkpolicies",
	"networkpolicy", "networkpolicylist", "networkset", "networksetlist", "networksets", "node", "nodelist", "nodes",
	"operation", "operationlist", "operations", "orphan", "orphanlist", "orphans", "pg", "plan", "planlist", "plans",
	"podsecurityadmissionconfigurationtemplate", "podsecurityadmissionconfigurationtemplatelist",
	"podsecurityadmissionconfigurationtemplates", "postgresql", "postgresqllist", "postgresqls", "preference",
	"preferencelist", "preferences", "recurringjob", "recurringjoblist", "recurringjobs", "replica", "replicalist",
	"replicas", "setting", "settinglist", "settings", "sharemanager", "sharemanagerlist", "sharemanagers", "snapshot", "snapshotlist",
	"snapshots", "supportbundle", "supportbundlelist", "supportbundles", "systembackup", "systembackuplist", "systembackups",
	"systemrestore", "systemrestorelist", "systemrestores", "tigerastatus", "tigerastatuses", "tigerastatuslist", "token",
	"tokenlist", "tokens", "upgrade", "user", "userattribute", "userattributelist", "userattributes", "userlist", "users",
	"volume", "volumelist", "volumes",
}

var namespaces []string = []string{
	"default", "kube-system", "kube-public", "kube-node-lease",
	"k9s", "testns", "production", "staging", "dev", "webservices",
	"testns-0", "testns-1", "testns-2", "testns-3", "testns-4",
	"appweb-dev", "appweb-staging", "appweb-production", "appweb-test", "appweb-qa",
	"longhorns-system", "cattle-system", "cattle-fleet-system", "cattle-impersonation-system",
	"karma-system", "karmada-cluster", "karmada-controller", "karmada-federation", "karmada-federation-system",
	"submariner-operator", "submariner", "submariner-lighthouse", "submariner-route-agent",
	"ingress-controller", "ingress-nginx", "ingress-nginx-controller", "ingress-nginx-admission",
}
