package main

import "testing"

func TestTopStructuralSimilarityClustersByFileSelectsBestClusterAndSortsDeterministically(t *testing.T) {
	a1 := &symbolObservation{File: "pkg/a.go", Line: 10, Symbol: "AOne", Language: "go", BranchCount: 3, CallSiteCount: 9, FanOut: 6, SymbolLines: 24, ParamCount: 2, ReturnCount: 1}
	a2 := &symbolObservation{File: "pkg/a.go", Line: 30, Symbol: "ATwo", Language: "go", BranchCount: 3, CallSiteCount: 9, FanOut: 6, SymbolLines: 24, ParamCount: 2, ReturnCount: 1}
	a3 := &symbolObservation{File: "pkg/a.go", Line: 50, Symbol: "AThree", Language: "go", BranchCount: 3, CallSiteCount: 9, FanOut: 6, SymbolLines: 24, ParamCount: 2, ReturnCount: 1}
	a4 := &symbolObservation{File: "pkg/a.go", Line: 70, Symbol: "AFour", Language: "go", BranchCount: 3, CallSiteCount: 9, FanOut: 6, SymbolLines: 24, ParamCount: 2, ReturnCount: 1}
	b1 := &symbolObservation{File: "pkg/b.go", Line: 11, Symbol: "BOne", Language: "go", BranchCount: 3, CallSiteCount: 9, FanOut: 6, SymbolLines: 24, ParamCount: 2, ReturnCount: 1}
	b2 := &symbolObservation{File: "pkg/b.go", Line: 31, Symbol: "BTwo", Language: "go", BranchCount: 3, CallSiteCount: 9, FanOut: 6, SymbolLines: 24, ParamCount: 2, ReturnCount: 1}

	state := clusterSelectionStateFromObservations(a1, a2, a3, a4, b1, b2)
	state.FileSymbols["pkg/a.go"] = []string{
		observationKey(a1.File, a1.Symbol),
		observationKey(a2.File, a2.Symbol),
		observationKey(a3.File, a3.Symbol),
		observationKey(a4.File, a4.Symbol),
	}
	state.FileSymbols["pkg/b.go"] = []string{
		observationKey(b1.File, b1.Symbol),
		observationKey(b2.File, b2.Symbol),
	}

	peerMap := map[string][]similarObservation{}
	addStructuralPeer(peerMap, a1, a2, 92)
	addStructuralPeer(peerMap, a3, a4, 80)
	addStructuralPeer(peerMap, b1, b2, 92)

	clusters := topStructuralSimilarityClustersByFile(state, peerMap)
	if len(clusters) != 2 {
		t.Fatalf("clusters=%#v want 2", clusters)
	}
	if clusters[0].File != "pkg/a.go" || clusters[1].File != "pkg/b.go" {
		t.Fatalf("expected deterministic file order [pkg/a.go pkg/b.go], got [%s %s]", clusters[0].File, clusters[1].File)
	}
	if clusters[0].EntryLine != 10 {
		t.Fatalf("expected best pkg/a.go cluster to use strong pair entry line 10, got %d", clusters[0].EntryLine)
	}
	if clusters[0].MaxSimilarity != 92 {
		t.Fatalf("expected best pkg/a.go cluster max similarity 92, got %d", clusters[0].MaxSimilarity)
	}
}

func TestTopCallFamilyClustersByFileSelectsBestClusterAndSortsDeterministically(t *testing.T) {
	a1 := &symbolObservation{File: "pkg/a.ts", Line: 10, Symbol: "AOne", Language: "typescript", FanOut: 4, SymbolLines: 18, ParamCount: 2, ReturnCount: 1}
	a2 := &symbolObservation{File: "pkg/a.ts", Line: 30, Symbol: "ATwo", Language: "typescript", FanOut: 4, SymbolLines: 18, ParamCount: 2, ReturnCount: 1}
	a3 := &symbolObservation{File: "pkg/a.ts", Line: 50, Symbol: "AThree", Language: "typescript", FanOut: 4, SymbolLines: 18, ParamCount: 2, ReturnCount: 1}
	a4 := &symbolObservation{File: "pkg/a.ts", Line: 70, Symbol: "AFour", Language: "typescript", FanOut: 4, SymbolLines: 18, ParamCount: 2, ReturnCount: 1}
	b1 := &symbolObservation{File: "pkg/b.ts", Line: 12, Symbol: "BOne", Language: "typescript", FanOut: 4, SymbolLines: 18, ParamCount: 2, ReturnCount: 1}
	b2 := &symbolObservation{File: "pkg/b.ts", Line: 32, Symbol: "BTwo", Language: "typescript", FanOut: 4, SymbolLines: 18, ParamCount: 2, ReturnCount: 1}

	state := clusterSelectionStateFromObservations(a1, a2, a3, a4, b1, b2)
	state.FileSymbols["pkg/a.ts"] = []string{
		observationKey(a1.File, a1.Symbol),
		observationKey(a2.File, a2.Symbol),
		observationKey(a3.File, a3.Symbol),
		observationKey(a4.File, a4.Symbol),
	}
	state.FileSymbols["pkg/b.ts"] = []string{
		observationKey(b1.File, b1.Symbol),
		observationKey(b2.File, b2.Symbol),
	}

	peerMap := map[string][]callFamilyPeer{}
	addCallFamilyPeer(peerMap, a1, a2, 95)
	addCallFamilyPeer(peerMap, a3, a4, 84)
	addCallFamilyPeer(peerMap, b1, b2, 95)

	clusters := topCallFamilyClustersByFile(state, peerMap)
	if len(clusters) != 2 {
		t.Fatalf("clusters=%#v want 2", clusters)
	}
	if clusters[0].File != "pkg/a.ts" || clusters[1].File != "pkg/b.ts" {
		t.Fatalf("expected deterministic file order [pkg/a.ts pkg/b.ts], got [%s %s]", clusters[0].File, clusters[1].File)
	}
	if clusters[0].EntryLine != 10 {
		t.Fatalf("expected best pkg/a.ts cluster to use strong pair entry line 10, got %d", clusters[0].EntryLine)
	}
	if clusters[0].MaxSimilarity != 95 {
		t.Fatalf("expected best pkg/a.ts cluster max similarity 95, got %d", clusters[0].MaxSimilarity)
	}
}

func TestCallFamilyClusterSelectionDiffersByFileAndDirectoryScope(t *testing.T) {
	left := &symbolObservation{File: "pkg/mod/one.ts", Line: 10, Symbol: "PatchOne", Language: "typescript", FanOut: 4, SymbolLines: 18, ParamCount: 2, ReturnCount: 1}
	right := &symbolObservation{File: "pkg/mod/two.ts", Line: 12, Symbol: "PatchTwo", Language: "typescript", FanOut: 4, SymbolLines: 18, ParamCount: 2, ReturnCount: 1}

	state := clusterSelectionStateFromObservations(left, right)
	state.FileSymbols["pkg/mod/one.ts"] = []string{observationKey(left.File, left.Symbol)}
	state.FileSymbols["pkg/mod/two.ts"] = []string{observationKey(right.File, right.Symbol)}

	peerMap := map[string][]callFamilyPeer{}
	addCallFamilyPeer(peerMap, left, right, 90)

	fileScoped := topCallFamilyClustersByFile(state, peerMap)
	if len(fileScoped) != 0 {
		t.Fatalf("expected no file-scoped cluster for cross-file-only peers, got %#v", fileScoped)
	}

	dirScoped := topCallFamilyClustersByDirectory(state, peerMap)
	if len(dirScoped) != 1 {
		t.Fatalf("expected one directory-scoped cluster, got %#v", dirScoped)
	}
	if dirScoped[0].ScopePath != "pkg/mod" {
		t.Fatalf("unexpected scope path: %q", dirScoped[0].ScopePath)
	}
	if dirScoped[0].UniqueFileCount != 2 {
		t.Fatalf("expected unique file count 2, got %d", dirScoped[0].UniqueFileCount)
	}
}

func TestBuildStructuralSimilarityClusterInScopeMarksOutOfScopePeersVisited(t *testing.T) {
	start := &symbolObservation{File: "pkg/one.go", Line: 5, Symbol: "Start", Language: "go", BranchCount: 2, CallSiteCount: 6, FanOut: 4, SymbolLines: 15}
	outOfScope := &symbolObservation{File: "pkg/two.go", Line: 7, Symbol: "Out", Language: "go", BranchCount: 2, CallSiteCount: 6, FanOut: 4, SymbolLines: 15}

	state := clusterSelectionStateFromObservations(start, outOfScope)
	peerMap := map[string][]similarObservation{}
	addStructuralPeer(peerMap, start, outOfScope, 88)
	visited := make(map[string]struct{})

	cluster := buildStructuralSimilarityClusterInScope(
		state,
		peerMap,
		"pkg/one.go",
		observationKey(start.File, start.Symbol),
		visited,
		func(obs *symbolObservation) bool { return obs != nil && obs.File == "pkg/one.go" },
		func(peers []similarObservation) []similarObservation { return peers },
	)

	if len(cluster.Members) != 1 || cluster.Members[0].Symbol != "Start" {
		t.Fatalf("expected only in-scope member in cluster, got %#v", cluster.Members)
	}
	outKey := observationKey(outOfScope.File, outOfScope.Symbol)
	if _, ok := visited[outKey]; !ok {
		t.Fatalf("expected out-of-scope peer %q to be marked visited", outKey)
	}
}

func TestBuildCallFamilyClusterInScopeMarksOutOfScopePeersVisited(t *testing.T) {
	start := &symbolObservation{File: "pkg/one.ts", Line: 5, Symbol: "Start", Language: "typescript", FanOut: 4, SymbolLines: 15, ParamCount: 1, ReturnCount: 1}
	outOfScope := &symbolObservation{File: "pkg/two.ts", Line: 7, Symbol: "Out", Language: "typescript", FanOut: 4, SymbolLines: 15, ParamCount: 1, ReturnCount: 1}

	state := clusterSelectionStateFromObservations(start, outOfScope)
	peerMap := map[string][]callFamilyPeer{}
	addCallFamilyPeer(peerMap, start, outOfScope, 91)
	visited := make(map[string]struct{})

	cluster := buildCallFamilyClusterInScope(
		state,
		peerMap,
		"pkg/one.ts",
		observationKey(start.File, start.Symbol),
		visited,
		func(obs *symbolObservation) bool { return obs != nil && obs.File == "pkg/one.ts" },
		func(peers []callFamilyPeer) []callFamilyPeer { return peers },
	)

	if len(cluster.Members) != 1 || cluster.Members[0].Symbol != "Start" {
		t.Fatalf("expected only in-scope member in cluster, got %#v", cluster.Members)
	}
	outKey := observationKey(outOfScope.File, outOfScope.Symbol)
	if _, ok := visited[outKey]; !ok {
		t.Fatalf("expected out-of-scope peer %q to be marked visited", outKey)
	}
}

func clusterSelectionStateFromObservations(observations ...*symbolObservation) *scoutState {
	state := &scoutState{
		Symbols:     make(map[string]*symbolObservation, len(observations)),
		FileSymbols: make(map[string][]string),
	}
	for _, obs := range observations {
		key := observationKey(obs.File, obs.Symbol)
		state.Symbols[key] = obs
		if _, ok := state.FileSymbols[obs.File]; !ok {
			state.FileSymbols[obs.File] = nil
		}
	}
	return state
}

func addStructuralPeer(peerMap map[string][]similarObservation, left, right *symbolObservation, similarity int) {
	leftKey := observationKey(left.File, left.Symbol)
	rightKey := observationKey(right.File, right.Symbol)
	peerMap[leftKey] = append(peerMap[leftKey], similarObservation{Observation: right, Similarity: similarity})
	peerMap[rightKey] = append(peerMap[rightKey], similarObservation{Observation: left, Similarity: similarity})
}

func addCallFamilyPeer(peerMap map[string][]callFamilyPeer, left, right *symbolObservation, similarity int) {
	leftKey := observationKey(left.File, left.Symbol)
	rightKey := observationKey(right.File, right.Symbol)
	peerMap[leftKey] = append(peerMap[leftKey], callFamilyPeer{Observation: right, Similarity: similarity})
	peerMap[rightKey] = append(peerMap[rightKey], callFamilyPeer{Observation: left, Similarity: similarity})
}
