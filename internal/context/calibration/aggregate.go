package calibration

import (
	"math"
	"sort"
	"time"
)

const (
	// SignalDecayDays is the half-life for signal weight decay.
	SignalDecayDays = 30
	// MinSignalWeight is the minimum weight a signal can have after decay.
	MinSignalWeight = 0.1
	// MinConfidenceThreshold is the minimum confidence to change a dimension.
	MinConfidenceThreshold = 0.5
)

// AggregateSignals processes new signals and updates the profile dimensions.
// Returns true if any dimension changed significantly.
func AggregateSignals(profile *Profile, signals []Signal) bool {
	if profile == nil {
		return false
	}
	if len(signals) == 0 {
		return false
	}

	// Group signals by dimension
	byDimension := make(map[string][]Signal)
	for _, sig := range signals {
		byDimension[sig.Dimension] = append(byDimension[sig.Dimension], sig)
	}

	// Also add existing signals from the profile
	addExistingSignals(profile, byDimension)

	changed := false

	// Process each dimension
	for dimension, dimSignals := range byDimension {
		if processSignalsForDimension(profile, dimension, dimSignals) {
			changed = true
		}
	}

	return changed
}

// addExistingSignals adds existing signals from the profile to the dimension map.
func addExistingSignals(profile *Profile, byDimension map[string][]Signal) {
	if profile == nil {
		return
	}
	// Communication signals
	for _, sig := range profile.Communication.Signals {
		byDimension[sig.Dimension] = append(byDimension[sig.Dimension], sig)
	}
	// Tone signals
	for _, sig := range profile.Tone.Signals {
		byDimension[sig.Dimension] = append(byDimension[sig.Dimension], sig)
	}
	// Working style signals
	for _, sig := range profile.WorkingStyle.Signals {
		byDimension[sig.Dimension] = append(byDimension[sig.Dimension], sig)
	}
	// Cognition signals
	for _, sig := range profile.Cognition.Signals {
		byDimension[sig.Dimension] = append(byDimension[sig.Dimension], sig)
	}
	// Trust signals
	for _, sig := range profile.Trust.Signals {
		byDimension[sig.Dimension] = append(byDimension[sig.Dimension], sig)
	}
}

// processSignalsForDimension updates a specific dimension based on weighted signals.
func processSignalsForDimension(profile *Profile, dimension string, signals []Signal) bool {
	if len(signals) == 0 {
		return false
	}

	// Calculate weighted direction
	var increaseWeight, decreaseWeight, confirmWeight float32
	for _, sig := range signals {
		weight := signalWeight(sig.At) * sig.Confidence
		switch sig.Direction {
		case "increase":
			increaseWeight += weight
		case "decrease":
			decreaseWeight += weight
		case "confirm":
			confirmWeight += weight
		}
	}

	totalWeight := increaseWeight + decreaseWeight + confirmWeight
	if totalWeight == 0 {
		return false
	}

	// Calculate confidence
	confidence := totalWeight / float32(len(signals))
	if confidence > 1.0 {
		confidence = 1.0
	}

	// Keep only recent signals (last 50)
	recentSignals := keepRecentSignals(signals, 50)

	// Update the appropriate dimension
	return updateDimension(profile, dimension, increaseWeight, decreaseWeight, confirmWeight, confidence, recentSignals)
}

// signalWeight calculates the weight of a signal based on its age.
// Uses exponential decay with half-life of SignalDecayDays.
func signalWeight(signalTime time.Time) float32 {
	age := time.Since(signalTime)
	days := age.Hours() / 24

	// Exponential decay: weight = 2^(-days/halflife)
	weight := math.Pow(2, -days/SignalDecayDays)

	if weight < MinSignalWeight {
		return MinSignalWeight
	}
	return float32(weight)
}

// keepRecentSignals keeps the N most recent signals.
func keepRecentSignals(signals []Signal, n int) []Signal {
	if len(signals) <= n {
		return signals
	}

	// Sort by time descending
	sorted := make([]Signal, len(signals))
	copy(sorted, signals)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].At.After(sorted[j].At)
	})

	return sorted[:n]
}

// updateDimension updates a specific dimension in the profile.
func updateDimension(profile *Profile, dimension string, incWeight, decWeight, confWeight, confidence float32, signals []Signal) bool {
	switch dimension {
	// Communication dimensions
	case "communication.verbosity":
		return updateVerbosity(profile, incWeight, decWeight, confWeight, confidence, signals)
	case "communication.technical_depth":
		return updateTechnicalDepth(profile, incWeight, decWeight, confWeight, confidence, signals)
	case "communication.code_preference":
		return updateCodePreference(profile, incWeight, decWeight, confWeight, confidence, signals)
	case "communication.explanation_style":
		return updateExplanationStyle(profile, signals)

	// Tone dimensions
	case "tone.formality":
		return updateFormality(profile, incWeight, decWeight, confWeight, confidence, signals)
	case "tone.patience":
		return updatePatience(profile, incWeight, decWeight, confWeight, confidence, signals)
	case "tone.assertiveness":
		return updateAssertiveness(profile, signals)

	// Working style dimensions
	case "working_style.problem_approach":
		return updateProblemApproach(profile, signals)
	case "working_style.feedback_style":
		return updateFeedbackStyle(profile, signals)
	case "working_style.collaboration_mode":
		return updateCollaborationMode(profile, signals)

	// Cognition dimensions
	case "cognition.mental_model":
		return updateMentalModel(profile, signals)
	case "cognition.learning_style":
		return updateLearningStyle(profile, signals)
	case "cognition.motivations":
		return updateMotivations(profile, signals)

	// Trust dimensions
	case "trust.autonomy_level":
		return updateAutonomyLevel(profile, incWeight, decWeight, confWeight, confidence, signals)
	case "trust.verification_need":
		return updateVerificationNeed(profile, signals)
	case "trust.pushback_pattern":
		return updatePushbackPattern(profile, signals)
	case "trust.corrections_style":
		return updateCorrectionsStyle(profile, signals)

	default:
		return false
	}
}

// Update functions for each dimension

func updateVerbosity(p *Profile, inc, dec, conf, confidence float32, signals []Signal) bool {
	old := p.Communication.Verbosity
	p.Communication.Signals = signals
	p.Communication.Confidence = confidence

	if inc > dec+conf && confidence >= MinConfidenceThreshold {
		// Increase means user wants more detail
		switch old {
		case VerbosityConcise:
			p.Communication.Verbosity = VerbosityModerate
		case VerbosityModerate:
			p.Communication.Verbosity = VerbosityDetailed
		}
	} else if dec > inc+conf && confidence >= MinConfidenceThreshold {
		// Decrease means user wants less detail
		switch old {
		case VerbosityDetailed:
			p.Communication.Verbosity = VerbosityModerate
		case VerbosityModerate:
			p.Communication.Verbosity = VerbosityConcise
		}
	}

	return old != p.Communication.Verbosity
}

func updateTechnicalDepth(p *Profile, inc, dec, conf, confidence float32, signals []Signal) bool {
	old := p.Communication.TechnicalDepth

	if inc > dec+conf && confidence >= MinConfidenceThreshold {
		switch old {
		case DepthHighLevel:
			p.Communication.TechnicalDepth = DepthModerate
		case DepthModerate:
			p.Communication.TechnicalDepth = DepthDeepDive
		}
	} else if dec > inc+conf && confidence >= MinConfidenceThreshold {
		switch old {
		case DepthDeepDive:
			p.Communication.TechnicalDepth = DepthModerate
		case DepthModerate:
			p.Communication.TechnicalDepth = DepthHighLevel
		}
	}

	return old != p.Communication.TechnicalDepth
}

func updateCodePreference(p *Profile, inc, dec, conf, confidence float32, signals []Signal) bool {
	old := p.Communication.CodePreference

	if inc > dec+conf && confidence >= MinConfidenceThreshold {
		switch old {
		case CodePseudocode:
			p.Communication.CodePreference = CodeSnippets
		case CodeSnippets:
			p.Communication.CodePreference = CodeFull
		}
	} else if dec > inc+conf && confidence >= MinConfidenceThreshold {
		switch old {
		case CodeFull:
			p.Communication.CodePreference = CodeSnippets
		case CodeSnippets:
			p.Communication.CodePreference = CodePseudocode
		}
	}

	return old != p.Communication.CodePreference
}

func updateExplanationStyle(p *Profile, signals []Signal) bool {
	// Count mentions of different styles
	styles := make(map[string]float32)
	for _, sig := range signals {
		weight := signalWeight(sig.At) * sig.Confidence
		switch sig.Direction {
		case "examples-first", "theory-first", "mixed":
			styles[sig.Direction] += weight
		}
	}

	// Find dominant style
	var maxStyle string
	var maxWeight float32
	for style, weight := range styles {
		if weight > maxWeight {
			maxStyle = style
			maxWeight = weight
		}
	}

	if maxStyle != "" && maxStyle != p.Communication.ExplanationStyle {
		p.Communication.ExplanationStyle = maxStyle
		return true
	}
	return false
}

func updateFormality(p *Profile, inc, dec, conf, confidence float32, signals []Signal) bool {
	old := p.Tone.Formality
	p.Tone.Signals = signals
	p.Tone.Confidence = confidence

	if inc > dec+conf && confidence >= MinConfidenceThreshold {
		switch old {
		case FormalityCasual:
			p.Tone.Formality = FormalityAdaptive
		case FormalityAdaptive:
			p.Tone.Formality = FormalityFormal
		}
	} else if dec > inc+conf && confidence >= MinConfidenceThreshold {
		switch old {
		case FormalityFormal:
			p.Tone.Formality = FormalityAdaptive
		case FormalityAdaptive:
			p.Tone.Formality = FormalityCasual
		}
	}

	return old != p.Tone.Formality
}

func updatePatience(p *Profile, inc, dec, conf, confidence float32, signals []Signal) bool {
	old := p.Tone.Patience

	if inc > dec+conf && confidence >= MinConfidenceThreshold {
		switch old {
		case PatienceImpatient:
			p.Tone.Patience = PatienceModerate
		case PatienceModerate:
			p.Tone.Patience = PatiencePatient
		}
	} else if dec > inc+conf && confidence >= MinConfidenceThreshold {
		switch old {
		case PatiencePatient:
			p.Tone.Patience = PatienceModerate
		case PatienceModerate:
			p.Tone.Patience = PatienceImpatient
		}
	}

	return old != p.Tone.Patience
}

func updateAssertiveness(p *Profile, signals []Signal) bool {
	styles := make(map[string]float32)
	for _, sig := range signals {
		weight := signalWeight(sig.At) * sig.Confidence
		switch sig.Direction {
		case "directive", "collaborative", "deferential":
			styles[sig.Direction] += weight
		}
	}

	var maxStyle string
	var maxWeight float32
	for style, weight := range styles {
		if weight > maxWeight {
			maxStyle = style
			maxWeight = weight
		}
	}

	if maxStyle != "" && maxStyle != p.Tone.Assertiveness {
		p.Tone.Assertiveness = maxStyle
		return true
	}
	return false
}

func updateProblemApproach(p *Profile, signals []Signal) bool {
	approaches := make(map[string]float32)
	for _, sig := range signals {
		weight := signalWeight(sig.At) * sig.Confidence
		switch sig.Direction {
		case "iterative", "big-picture", "test-driven", "exploratory":
			approaches[sig.Direction] += weight
		}
	}

	var maxApproach string
	var maxWeight float32
	for approach, weight := range approaches {
		if weight > maxWeight {
			maxApproach = approach
			maxWeight = weight
		}
	}

	if maxApproach != "" && maxApproach != p.WorkingStyle.ProblemApproach {
		p.WorkingStyle.ProblemApproach = maxApproach
		p.WorkingStyle.Signals = signals
		return true
	}
	return false
}

func updateFeedbackStyle(p *Profile, signals []Signal) bool {
	styles := make(map[string]float32)
	for _, sig := range signals {
		weight := signalWeight(sig.At) * sig.Confidence
		switch sig.Direction {
		case "direct", "diplomatic", "detailed-critique":
			styles[sig.Direction] += weight
		}
	}

	var maxStyle string
	var maxWeight float32
	for style, weight := range styles {
		if weight > maxWeight {
			maxStyle = style
			maxWeight = weight
		}
	}

	if maxStyle != "" && maxStyle != p.WorkingStyle.FeedbackStyle {
		p.WorkingStyle.FeedbackStyle = maxStyle
		return true
	}
	return false
}

func updateCollaborationMode(p *Profile, signals []Signal) bool {
	modes := make(map[string]float32)
	for _, sig := range signals {
		weight := signalWeight(sig.At) * sig.Confidence
		switch sig.Direction {
		case "pair-programming", "review-based", "autonomous":
			modes[sig.Direction] += weight
		}
	}

	var maxMode string
	var maxWeight float32
	for mode, weight := range modes {
		if weight > maxWeight {
			maxMode = mode
			maxWeight = weight
		}
	}

	if maxMode != "" && maxMode != p.WorkingStyle.CollaborationMode {
		p.WorkingStyle.CollaborationMode = maxMode
		return true
	}
	return false
}

func updateMentalModel(p *Profile, signals []Signal) bool {
	models := make(map[string]float32)
	for _, sig := range signals {
		weight := signalWeight(sig.At) * sig.Confidence
		switch sig.Direction {
		case "visual", "sequential", "hierarchical", "associative":
			models[sig.Direction] += weight
		}
	}

	var maxModel string
	var maxWeight float32
	for model, weight := range models {
		if weight > maxWeight {
			maxModel = model
			maxWeight = weight
		}
	}

	if maxModel != "" && maxModel != p.Cognition.MentalModel {
		p.Cognition.MentalModel = maxModel
		p.Cognition.Signals = signals
		return true
	}
	return false
}

func updateLearningStyle(p *Profile, signals []Signal) bool {
	styles := make(map[string]float32)
	for _, sig := range signals {
		weight := signalWeight(sig.At) * sig.Confidence
		switch sig.Direction {
		case "examples-first", "theory-first", "hands-on", "reading":
			styles[sig.Direction] += weight
		}
	}

	var maxStyle string
	var maxWeight float32
	for style, weight := range styles {
		if weight > maxWeight {
			maxStyle = style
			maxWeight = weight
		}
	}

	if maxStyle != "" && maxStyle != p.Cognition.LearningStyle {
		p.Cognition.LearningStyle = maxStyle
		return true
	}
	return false
}

func updateMotivations(p *Profile, signals []Signal) bool {
	motivations := make(map[string]float32)
	for _, sig := range signals {
		weight := signalWeight(sig.At) * sig.Confidence
		switch sig.Direction {
		case "speed", "correctness", "elegance", "learning", "pragmatism":
			motivations[sig.Direction] += weight
		}
	}

	// Get top 3 motivations
	type kv struct {
		k string
		v float32
	}
	var sorted []kv
	for k, v := range motivations {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].v > sorted[j].v
	})

	newMotivations := make([]string, 0, 3)
	for i, item := range sorted {
		if i >= 3 {
			break
		}
		newMotivations = append(newMotivations, item.k)
	}

	if len(newMotivations) == 0 {
		return false
	}

	// Only update if motivations actually changed
	if len(newMotivations) == len(p.Cognition.Motivations) {
		same := true
		for i, m := range newMotivations {
			if p.Cognition.Motivations[i] != m {
				same = false
				break
			}
		}
		if same {
			return false
		}
	}

	p.Cognition.Motivations = newMotivations
	return true
}

func updateAutonomyLevel(p *Profile, inc, dec, conf, confidence float32, signals []Signal) bool {
	old := p.Trust.AutonomyLevel
	p.Trust.Signals = signals
	p.Trust.Confidence = confidence

	if inc > dec+conf && confidence >= MinConfidenceThreshold {
		switch old {
		case "low":
			p.Trust.AutonomyLevel = "moderate"
		case "moderate":
			p.Trust.AutonomyLevel = "high"
		}
	} else if dec > inc+conf && confidence >= MinConfidenceThreshold {
		switch old {
		case "high":
			p.Trust.AutonomyLevel = "moderate"
		case "moderate":
			p.Trust.AutonomyLevel = "low"
		}
	}

	return old != p.Trust.AutonomyLevel
}

func updateVerificationNeed(p *Profile, signals []Signal) bool {
	levels := make(map[string]float32)
	for _, sig := range signals {
		weight := signalWeight(sig.At) * sig.Confidence
		switch sig.Direction {
		case "always", "sometimes", "rarely":
			levels[sig.Direction] += weight
		}
	}

	var maxLevel string
	var maxWeight float32
	for level, weight := range levels {
		if weight > maxWeight {
			maxLevel = level
			maxWeight = weight
		}
	}

	if maxLevel != "" && maxLevel != p.Trust.VerificationNeed {
		p.Trust.VerificationNeed = maxLevel
		return true
	}
	return false
}

func updatePushbackPattern(p *Profile, signals []Signal) bool {
	patterns := make(map[string]float32)
	for _, sig := range signals {
		weight := signalWeight(sig.At) * sig.Confidence
		switch sig.Direction {
		case "frequent", "selective", "rare":
			patterns[sig.Direction] += weight
		}
	}

	var maxPattern string
	var maxWeight float32
	for pattern, weight := range patterns {
		if weight > maxWeight {
			maxPattern = pattern
			maxWeight = weight
		}
	}

	if maxPattern != "" && maxPattern != p.Trust.PushbackPattern {
		p.Trust.PushbackPattern = maxPattern
		return true
	}
	return false
}

func updateCorrectionsStyle(p *Profile, signals []Signal) bool {
	styles := make(map[string]float32)
	for _, sig := range signals {
		weight := signalWeight(sig.At) * sig.Confidence
		switch sig.Direction {
		case "direct", "diplomatic", "questioning":
			styles[sig.Direction] += weight
		}
	}

	var maxStyle string
	var maxWeight float32
	for style, weight := range styles {
		if weight > maxWeight {
			maxStyle = style
			maxWeight = weight
		}
	}

	if maxStyle != "" && maxStyle != p.Trust.CorrectionsStyle {
		p.Trust.CorrectionsStyle = maxStyle
		return true
	}
	return false
}

// UpdateExpertise updates the expertise map based on domain signals.
func UpdateExpertise(p *Profile, domains []Domain) {
	if p == nil {
		return
	}

	// Merge new domains with existing
	domainMap := make(map[string]*Domain)

	// Add existing
	for i := range p.Expertise.StrongDomains {
		d := &p.Expertise.StrongDomains[i]
		domainMap[d.Name] = d
	}
	for i := range p.Expertise.LearningAreas {
		d := &p.Expertise.LearningAreas[i]
		domainMap[d.Name] = d
	}
	for i := range p.Expertise.KnowledgeGaps {
		d := &p.Expertise.KnowledgeGaps[i]
		domainMap[d.Name] = d
	}

	// Merge new
	for _, d := range domains {
		if existing, ok := domainMap[d.Name]; ok {
			existing.SignalCount++
			existing.LastSeen = d.LastSeen
			if d.Level != "" {
				existing.Level = d.Level
			}
		} else {
			// Create a copy to avoid loop variable capture
			dcopy := d
			domainMap[d.Name] = &dcopy
		}
	}

	// Redistribute by level
	p.Expertise.StrongDomains = nil
	p.Expertise.LearningAreas = nil
	p.Expertise.KnowledgeGaps = nil

	for _, d := range domainMap {
		switch d.Level {
		case "expert", "proficient":
			p.Expertise.StrongDomains = append(p.Expertise.StrongDomains, *d)
		case "familiar", "learning":
			p.Expertise.LearningAreas = append(p.Expertise.LearningAreas, *d)
		case "novice":
			p.Expertise.KnowledgeGaps = append(p.Expertise.KnowledgeGaps, *d)
		}
	}
}
