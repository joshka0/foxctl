package main

import (
	"sort"
	"strings"
)

func topStructuralSimilarityClustersByFile(state *scoutState, peerMap map[string][]similarObservation) []structuralSimilarityCluster {
	bestByFile := make(map[string]structuralSimilarityCluster)
	visited := make(map[string]struct{})
	for file, keys := range state.FileSymbols {
		for _, key := range keys {
			if _, ok := visited[key]; ok {
				continue
			}
			localPeers := localSimilarPeers(peerMap[key], file)
			if len(localPeers) == 0 {
				visited[key] = struct{}{}
				continue
			}
			cluster := buildStructuralSimilarityCluster(state, peerMap, file, key, visited)
			if len(cluster.Members) < 2 {
				continue
			}
			selectBestClusterByScope(bestByFile, file, cluster, compareStructuralClusters)
		}
	}
	return sortedClustersBy(bestByFile, func(left, right structuralSimilarityCluster) bool {
		leftScore := scoreStructuralSimilarityCluster(len(left.Members), left.EdgeCount, left.MaxSimilarity, left.AverageSimilarity, left.UniqueFileCount, left.AverageBranches, left.AverageCallSites, left.AverageFanOut)
		rightScore := scoreStructuralSimilarityCluster(len(right.Members), right.EdgeCount, right.MaxSimilarity, right.AverageSimilarity, right.UniqueFileCount, right.AverageBranches, right.AverageCallSites, right.AverageFanOut)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if left.File != right.File {
			return left.File < right.File
		}
		return left.EntryLine < right.EntryLine
	})
}

func topCallFamilyClustersByFile(state *scoutState, peerMap map[string][]callFamilyPeer) []callFamilyCluster {
	bestByFile := make(map[string]callFamilyCluster)
	visited := make(map[string]struct{})
	for file, keys := range state.FileSymbols {
		for _, key := range keys {
			if _, ok := visited[key]; ok {
				continue
			}
			localPeers := localCallFamilyPeers(peerMap[key], file)
			if len(localPeers) == 0 {
				visited[key] = struct{}{}
				continue
			}
			cluster := buildCallFamilyClusterInScope(state, peerMap, file, key, visited, func(obs *symbolObservation) bool {
				return obs != nil && obs.File == file
			}, func(peers []callFamilyPeer) []callFamilyPeer {
				return localCallFamilyPeers(peers, file)
			})
			if len(cluster.Members) < 2 {
				continue
			}
			selectBestClusterByScope(bestByFile, file, cluster, compareCallFamilyClusters)
		}
	}
	return sortedClustersBy(bestByFile, func(left, right callFamilyCluster) bool {
		leftScore := scoreCallFamilyCluster(len(left.Members), left.EdgeCount, left.MaxSimilarity, left.AverageSimilarity, left.UniqueFileCount, left.AverageFanOut, left.AverageSymbolLines, left.AdapterSurfaceScore)
		rightScore := scoreCallFamilyCluster(len(right.Members), right.EdgeCount, right.MaxSimilarity, right.AverageSimilarity, right.UniqueFileCount, right.AverageFanOut, right.AverageSymbolLines, right.AdapterSurfaceScore)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if left.File != right.File {
			return left.File < right.File
		}
		return left.EntryLine < right.EntryLine
	})
}

func topCallFamilyClustersByDirectory(state *scoutState, peerMap map[string][]callFamilyPeer) []callFamilyCluster {
	bestByDir := make(map[string]callFamilyCluster)
	visited := make(map[string]struct{})
	for _, keys := range state.FileSymbols {
		for _, key := range keys {
			if _, ok := visited[key]; ok {
				continue
			}
			obs := state.Symbols[key]
			if obs == nil {
				continue
			}
			dir := callFamilyModuleScopeForObservation(obs)
			if strings.TrimSpace(dir) == "" {
				visited[key] = struct{}{}
				continue
			}
			localPeers := directoryCallFamilyPeers(peerMap[key], dir)
			if len(localPeers) == 0 {
				visited[key] = struct{}{}
				continue
			}
			cluster := buildCallFamilyClusterInScope(state, peerMap, dir, key, visited, func(obs *symbolObservation) bool {
				return obs != nil && callFamilyModuleScopeForObservation(obs) == dir
			}, func(peers []callFamilyPeer) []callFamilyPeer {
				return directoryCallFamilyPeers(peers, dir)
			})
			if len(cluster.Members) < 2 || cluster.UniqueFileCount < 2 {
				continue
			}
			selectBestClusterByScope(bestByDir, dir, cluster, compareCallFamilyClusters)
		}
	}
	return sortedClustersBy(bestByDir, func(left, right callFamilyCluster) bool {
		leftScore := scoreCallFamilyCluster(len(left.Members), left.EdgeCount, left.MaxSimilarity, left.AverageSimilarity, left.UniqueFileCount, left.AverageFanOut, left.AverageSymbolLines, left.AdapterSurfaceScore)
		rightScore := scoreCallFamilyCluster(len(right.Members), right.EdgeCount, right.MaxSimilarity, right.AverageSimilarity, right.UniqueFileCount, right.AverageFanOut, right.AverageSymbolLines, right.AdapterSurfaceScore)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if left.ScopePath != right.ScopePath {
			return left.ScopePath < right.ScopePath
		}
		return left.EntryLine < right.EntryLine
	})
}

func topStructuralSimilarityClustersByDirectory(state *scoutState, peerMap map[string][]similarObservation) []structuralSimilarityCluster {
	bestByDir := make(map[string]structuralSimilarityCluster)
	visited := make(map[string]struct{})
	for _, keys := range state.FileSymbols {
		for _, key := range keys {
			if _, ok := visited[key]; ok {
				continue
			}
			obs := state.Symbols[key]
			if obs == nil {
				continue
			}
			dir := moduleScopeFor(obs.File)
			localPeers := directorySimilarPeers(peerMap[key], dir)
			if len(localPeers) == 0 {
				visited[key] = struct{}{}
				continue
			}
			cluster := buildStructuralSimilarityDirectoryCluster(state, peerMap, dir, key, visited)
			if len(cluster.Members) < 2 || cluster.UniqueFileCount < 2 {
				continue
			}
			selectBestClusterByScope(bestByDir, dir, cluster, compareStructuralClusters)
		}
	}
	return sortedClustersBy(bestByDir, func(left, right structuralSimilarityCluster) bool {
		leftScore := scoreStructuralSimilarityCluster(len(left.Members), left.EdgeCount, left.MaxSimilarity, left.AverageSimilarity, left.UniqueFileCount, left.AverageBranches, left.AverageCallSites, left.AverageFanOut)
		rightScore := scoreStructuralSimilarityCluster(len(right.Members), right.EdgeCount, right.MaxSimilarity, right.AverageSimilarity, right.UniqueFileCount, right.AverageBranches, right.AverageCallSites, right.AverageFanOut)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if left.ScopePath != right.ScopePath {
			return left.ScopePath < right.ScopePath
		}
		return left.EntryLine < right.EntryLine
	})
}

func buildStructuralSimilarityCluster(state *scoutState, peerMap map[string][]similarObservation, file, startKey string, visited map[string]struct{}) structuralSimilarityCluster {
	return buildStructuralSimilarityClusterInScope(state, peerMap, file, startKey, visited, func(obs *symbolObservation) bool {
		return obs != nil && obs.File == file
	}, func(peers []similarObservation) []similarObservation {
		return localSimilarPeers(peers, file)
	})
}

func buildStructuralSimilarityDirectoryCluster(state *scoutState, peerMap map[string][]similarObservation, dir, startKey string, visited map[string]struct{}) structuralSimilarityCluster {
	return buildStructuralSimilarityClusterInScope(state, peerMap, dir, startKey, visited, func(obs *symbolObservation) bool {
		return obs != nil && moduleScopeFor(obs.File) == dir
	}, func(peers []similarObservation) []similarObservation {
		return directorySimilarPeers(peers, dir)
	})
}

func buildCallFamilyClusterInScope(
	state *scoutState,
	peerMap map[string][]callFamilyPeer,
	scopePath, startKey string,
	visited map[string]struct{},
	inScope func(*symbolObservation) bool,
	filterPeers func([]callFamilyPeer) []callFamilyPeer,
) callFamilyCluster {
	componentKeys, componentSet := collectClusterComponentKeys(state, startKey, visited, inScope,
		func(key string) []callFamilyPeer {
			return filterPeers(peerMap[key])
		},
		func(peer callFamilyPeer) string {
			return observationKey(peer.Observation.File, peer.Observation.Symbol)
		},
	)
	if len(componentKeys) == 0 {
		return callFamilyCluster{}
	}
	members := make([]*symbolObservation, 0, len(componentKeys))
	memberFilesSet := make(map[string]struct{}, len(componentKeys))
	entryLine := 0
	representativeFile := ""
	edgeCount := 0
	totalSimilarity := 0
	totalFanOut := 0
	totalSymbolLines := 0
	totalParamCount := 0
	totalReturnCount := 0
	minFanOut := 0
	maxFanOut := 0
	minSymbolLines := 0
	maxSymbolLines := 0
	minParams := 0
	maxParams := 0
	minReturns := 0
	maxReturns := 0
	containerCounts := make(map[string]int)
	maxSimilarity := 0
	strongestPair := [2]string{}
	strongestDetail := callFamilySimilarityDetails{}
	seenEdges := make(map[string]struct{})
	for _, key := range componentKeys {
		obs := state.Symbols[key]
		if obs == nil {
			continue
		}
		members = append(members, obs)
		memberFilesSet[obs.File] = struct{}{}
		totalFanOut += obs.FanOut
		totalSymbolLines += obs.SymbolLines
		totalParamCount += obs.ParamCount
		totalReturnCount += obs.ReturnCount
		minFanOut, maxFanOut = updateRange(minFanOut, maxFanOut, obs.FanOut, len(members) == 1)
		minSymbolLines, maxSymbolLines = updateRange(minSymbolLines, maxSymbolLines, obs.SymbolLines, len(members) == 1)
		minParams, maxParams = updateRange(minParams, maxParams, obs.ParamCount, len(members) == 1)
		minReturns, maxReturns = updateRange(minReturns, maxReturns, obs.ReturnCount, len(members) == 1)
		containerCounts[symbolContainer(obs.Symbol)]++
		if representativeFile == "" || obs.File < representativeFile || (obs.File == representativeFile && (entryLine == 0 || obs.Line < entryLine)) {
			representativeFile = obs.File
			entryLine = obs.Line
		}
		for _, peer := range filterPeers(peerMap[key]) {
			peerKey := observationKey(peer.Observation.File, peer.Observation.Symbol)
			if _, ok := componentSet[peerKey]; !ok {
				continue
			}
			edgeID := orderedPairKey(key, peerKey)
			if _, ok := seenEdges[edgeID]; ok {
				continue
			}
			seenEdges[edgeID] = struct{}{}
			edgeCount++
			totalSimilarity += peer.Similarity
			if peer.Similarity > maxSimilarity {
				maxSimilarity = peer.Similarity
				strongestPair = [2]string{obs.Symbol, peer.Observation.Symbol}
				strongestDetail = peer.Details
			}
		}
	}
	memberFiles := sortedKeys(memberFilesSet)
	avgSimilarity := 0
	if edgeCount > 0 {
		avgSimilarity = totalSimilarity / edgeCount
	}
	avgFanOut := 0
	avgSymbolLines := 0
	avgParamCount := 0
	avgReturnCount := 0
	if len(members) > 0 {
		avgFanOut = totalFanOut / len(members)
		avgSymbolLines = totalSymbolLines / len(members)
		avgParamCount = totalParamCount / len(members)
		avgReturnCount = totalReturnCount / len(members)
	}
	dominantContainer, dominantContainerRatio := dominantContainerStats(containerCounts, len(members))
	adapterSurfaceScore := scoreAdapterSurfaceCluster(len(members), len(memberFiles), dominantContainerRatio, 0, 0, maxFanOut-minFanOut, maxSymbolLines-minSymbolLines, maxParams-minParams, maxReturns-minReturns)
	return callFamilyCluster{
		ScopePath:              scopePath,
		File:                   representativeFile,
		Members:                members,
		MemberFiles:            memberFiles,
		UniqueFileCount:        len(memberFiles),
		EntryLine:              entryLine,
		EdgeCount:              edgeCount,
		AverageSimilarity:      avgSimilarity,
		MaxSimilarity:          maxSimilarity,
		AverageFanOut:          avgFanOut,
		AverageSymbolLines:     avgSymbolLines,
		AverageParamCount:      avgParamCount,
		AverageReturnCount:     avgReturnCount,
		DominantContainer:      dominantContainer,
		DominantContainerRatio: dominantContainerRatio,
		AdapterSurfaceScore:    adapterSurfaceScore,
		StrongestPair:          strongestPair,
		StrongestDetail:        strongestDetail,
	}
}

func buildStructuralSimilarityClusterInScope(
	state *scoutState,
	peerMap map[string][]similarObservation,
	scopePath, startKey string,
	visited map[string]struct{},
	inScope func(*symbolObservation) bool,
	filterPeers func([]similarObservation) []similarObservation,
) structuralSimilarityCluster {
	componentKeys, componentSet := collectClusterComponentKeys(state, startKey, visited, inScope,
		func(key string) []similarObservation {
			return filterPeers(peerMap[key])
		},
		func(peer similarObservation) string {
			return observationKey(peer.Observation.File, peer.Observation.Symbol)
		},
	)
	if len(componentKeys) == 0 {
		return structuralSimilarityCluster{}
	}
	members := make([]*symbolObservation, 0, len(componentKeys))
	memberFilesSet := make(map[string]struct{}, len(componentKeys))
	entryLine := 0
	representativeFile := ""
	edgeCount := 0
	totalSimilarity := 0
	totalBranches := 0
	totalCallSites := 0
	totalFanOut := 0
	totalSymbolLines := 0
	maxSimilarity := 0
	minBranches := 0
	maxBranches := 0
	minCallSites := 0
	maxCallSites := 0
	minFanOut := 0
	maxFanOut := 0
	minSymbolLines := 0
	maxSymbolLines := 0
	minParams := 0
	maxParams := 0
	minReturns := 0
	maxReturns := 0
	containerCounts := make(map[string]int)
	strongestPair := [2]string{}
	strongestDetail := orchestrationSimilarityDetails{}
	seenEdges := make(map[string]struct{})
	for _, key := range componentKeys {
		obs := state.Symbols[key]
		if obs == nil {
			continue
		}
		members = append(members, obs)
		memberFilesSet[obs.File] = struct{}{}
		totalBranches += obs.BranchCount
		totalCallSites += obs.CallSiteCount
		totalFanOut += obs.FanOut
		totalSymbolLines += obs.SymbolLines
		minBranches, maxBranches = updateRange(minBranches, maxBranches, obs.BranchCount, len(members) == 1)
		minCallSites, maxCallSites = updateRange(minCallSites, maxCallSites, obs.CallSiteCount, len(members) == 1)
		minFanOut, maxFanOut = updateRange(minFanOut, maxFanOut, obs.FanOut, len(members) == 1)
		minSymbolLines, maxSymbolLines = updateRange(minSymbolLines, maxSymbolLines, obs.SymbolLines, len(members) == 1)
		minParams, maxParams = updateRange(minParams, maxParams, obs.ParamCount, len(members) == 1)
		minReturns, maxReturns = updateRange(minReturns, maxReturns, obs.ReturnCount, len(members) == 1)
		containerCounts[symbolContainer(obs.Symbol)]++
		if representativeFile == "" || obs.File < representativeFile || (obs.File == representativeFile && (entryLine == 0 || obs.Line < entryLine)) {
			representativeFile = obs.File
			entryLine = obs.Line
		}
		for _, peer := range filterPeers(peerMap[key]) {
			peerKey := observationKey(peer.Observation.File, peer.Observation.Symbol)
			if _, ok := componentSet[peerKey]; !ok {
				continue
			}
			edgeID := orderedPairKey(key, peerKey)
			if _, ok := seenEdges[edgeID]; ok {
				continue
			}
			seenEdges[edgeID] = struct{}{}
			edgeCount++
			totalSimilarity += peer.Similarity
			if peer.Similarity > maxSimilarity {
				maxSimilarity = peer.Similarity
				strongestPair = [2]string{obs.Symbol, peer.Observation.Symbol}
				strongestDetail = peer.Details
			}
		}
	}
	memberFiles := sortedKeys(memberFilesSet)
	avgSimilarity := 0
	if edgeCount > 0 {
		avgSimilarity = totalSimilarity / edgeCount
	}
	avgBranches := 0
	avgCallSites := 0
	avgFanOut := 0
	avgSymbolLines := 0
	if len(members) > 0 {
		avgBranches = totalBranches / len(members)
		avgCallSites = totalCallSites / len(members)
		avgFanOut = totalFanOut / len(members)
		avgSymbolLines = totalSymbolLines / len(members)
	}
	dominantContainer, dominantContainerRatio := dominantContainerStats(containerCounts, len(members))
	branchRange := maxBranches - minBranches
	callSiteRange := maxCallSites - minCallSites
	fanOutRange := maxFanOut - minFanOut
	symbolLineRange := maxSymbolLines - minSymbolLines
	paramRange := maxParams - minParams
	returnRange := maxReturns - minReturns
	adapterSurfaceScore := scoreAdapterSurfaceCluster(len(members), len(memberFiles), dominantContainerRatio, branchRange, callSiteRange, fanOutRange, symbolLineRange, paramRange, returnRange)
	return structuralSimilarityCluster{
		ScopePath:              scopePath,
		File:                   representativeFile,
		MemberKeys:             componentKeys,
		Members:                members,
		MemberFiles:            memberFiles,
		UniqueFileCount:        len(memberFiles),
		EntryLine:              entryLine,
		EdgeCount:              edgeCount,
		AverageSimilarity:      avgSimilarity,
		MaxSimilarity:          maxSimilarity,
		AverageBranches:        avgBranches,
		AverageCallSites:       avgCallSites,
		AverageFanOut:          avgFanOut,
		AverageSymbolLines:     avgSymbolLines,
		BranchRange:            branchRange,
		CallSiteRange:          callSiteRange,
		FanOutRange:            fanOutRange,
		SymbolLineRange:        symbolLineRange,
		ParamRange:             paramRange,
		ReturnRange:            returnRange,
		DominantContainer:      dominantContainer,
		DominantContainerRatio: dominantContainerRatio,
		AdapterSurfaceScore:    adapterSurfaceScore,
		StrongestPair:          strongestPair,
		StrongestDetail:        strongestDetail,
	}
}

func selectBestClusterByScope[T any](bestByScope map[string]T, scopePath string, cluster T, compare func(T, T) int) {
	current, ok := bestByScope[scopePath]
	if !ok || compare(cluster, current) > 0 {
		bestByScope[scopePath] = cluster
	}
}

func sortedClustersBy[T any](bestByScope map[string]T, less func(left, right T) bool) []T {
	out := make([]T, 0, len(bestByScope))
	for _, cluster := range bestByScope {
		out = append(out, cluster)
	}
	sort.Slice(out, func(i, j int) bool {
		return less(out[i], out[j])
	})
	return out
}

func collectClusterComponentKeys[P any](
	state *scoutState,
	startKey string,
	visited map[string]struct{},
	inScope func(*symbolObservation) bool,
	peersFor func(string) []P,
	peerKey func(P) string,
) ([]string, map[string]struct{}) {
	queue := []string{startKey}
	componentKeys := make([]string, 0, 4)
	componentSet := make(map[string]struct{}, 4)
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		if _, ok := visited[key]; ok {
			continue
		}
		visited[key] = struct{}{}
		obs := state.Symbols[key]
		if obs == nil || !inScope(obs) {
			continue
		}
		componentKeys = append(componentKeys, key)
		componentSet[key] = struct{}{}
		for _, peer := range peersFor(key) {
			nextKey := peerKey(peer)
			if _, ok := visited[nextKey]; ok {
				continue
			}
			queue = append(queue, nextKey)
		}
	}
	sort.Strings(componentKeys)
	return componentKeys, componentSet
}
