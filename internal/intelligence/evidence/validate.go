package evidence

func ValidateAllowedAuthorityEffects(plane EvidencePlane, authority EvidenceAuthority, effects []AuthorityEffect) error {
	if !plane.IsValid() {
		return ValidationError{Reason: ValidationReasonUnknownPlane}
	}
	if !authority.IsValid() {
		return ValidationError{Reason: ValidationReasonUnknownAuthority}
	}
	if len(effects) == 0 {
		return ValidationError{Reason: ValidationReasonEmptyEffects}
	}
	seen := make(map[AuthorityEffect]struct{}, len(effects))
	for _, effect := range effects {
		if !effect.IsValid() {
			return ValidationError{Reason: ValidationReasonUnknownEffect}
		}
		if _, ok := seen[effect]; ok {
			return ValidationError{Reason: ValidationReasonDuplicateEffect}
		}
		seen[effect] = struct{}{}
	}
	if _, ok := seen[AuthorityEffectNone]; ok && len(seen) != 1 {
		return ValidationError{Reason: ValidationReasonNoneNotExclusive}
	}
	for effect := range seen {
		if err := validateEffectForPlaneAndAuthority(plane, authority, effect); err != nil {
			return err
		}
	}
	return nil
}

func ValidateEvidenceMeta(meta EvidenceMeta) error {
	if !meta.Source.IsValid() {
		return ValidationError{Reason: ValidationReasonUnknownSource}
	}
	if !meta.SourcePlane.IsValid() {
		return ValidationError{Reason: ValidationReasonUnknownPlane}
	}
	if !meta.EvidenceClass.IsValid() {
		return ValidationError{Reason: ValidationReasonUnknownClass}
	}
	if !meta.EvidenceAuthority.IsValid() {
		return ValidationError{Reason: ValidationReasonUnknownAuthority}
	}
	if err := ValidateAllowedAuthorityEffects(meta.SourcePlane, meta.EvidenceAuthority, meta.AllowedAuthorityEffects); err != nil {
		return err
	}
	if !isCoherentTuple(meta) {
		return ValidationError{Reason: ValidationReasonIncoherentEvidenceTuple}
	}
	return nil
}

func ValidateRenderSurface(meta EvidenceMeta, surface RenderSurface) error {
	if err := ValidateEvidenceMeta(meta); err != nil {
		return err
	}
	if !surface.IsValid() {
		return ValidationError{Reason: ValidationReasonIllegalRenderSurface}
	}
	switch surface {
	case RenderSurfaceEvidencePack:
		return nil
	case RenderSurfaceReview:
		if meta.SourcePlane == EvidencePlaneGitEmpirical {
			return ValidationError{Reason: ValidationReasonIllegalRenderSurface}
		}
		return requireEffect(meta, AuthorityEffectReviewSignal)
	case RenderSurfaceReviewWarning:
		if meta.SourcePlane != EvidencePlaneGitEmpirical {
			return ValidationError{Reason: ValidationReasonIllegalRenderSurface}
		}
		return requireEffect(meta, AuthorityEffectReviewWarning)
	case RenderSurfaceEvidenceHint:
		if hasEffect(meta, AuthorityEffectInstructionSource) || hasEffect(meta, AuthorityEffectNone) {
			return ValidationError{Reason: ValidationReasonIllegalRenderSurface}
		}
		if hasEffect(meta, AuthorityEffectRetrievalRanking) ||
			hasEffect(meta, AuthorityEffectReviewSignal) ||
			hasEffect(meta, AuthorityEffectReviewWarning) {
			return nil
		}
		return ValidationError{Reason: ValidationReasonMissingRequiredEffect}
	case RenderSurfaceInstruction, RenderSurfacePolicy, RenderSurfaceHardConstraint,
		RenderSurfaceToolAuthorization, RenderSurfaceRuntimeGuardrail:
		if meta.SourcePlane != EvidencePlaneDurable {
			return ValidationError{Reason: ValidationReasonIllegalRenderSurface}
		}
		return requireEffect(meta, AuthorityEffectInstructionSource)
	default:
		return ValidationError{Reason: ValidationReasonIllegalRenderSurface}
	}
}

func validateEffectForPlaneAndAuthority(plane EvidencePlane, authority EvidenceAuthority, effect AuthorityEffect) error {
	switch plane {
	case EvidencePlaneStructural:
		switch effect {
		case AuthorityEffectNone, AuthorityEffectRetrievalRanking, AuthorityEffectReviewSignal:
			return nil
		default:
			return ValidationError{Reason: ValidationReasonIllegalEffectForPlane}
		}
	case EvidencePlaneSemanticAnchor:
		switch effect {
		case AuthorityEffectRetrievalRanking, AuthorityEffectReviewSignal:
			return nil
		default:
			return ValidationError{Reason: ValidationReasonIllegalEffectForPlane}
		}
	case EvidencePlaneGitEmpirical:
		switch effect {
		case AuthorityEffectRetrievalRanking, AuthorityEffectReviewWarning:
			return nil
		case AuthorityEffectReviewSignal:
			return ValidationError{Reason: ValidationReasonIllegalEffectForPlane}
		default:
			return ValidationError{Reason: ValidationReasonIllegalEffectForPlane}
		}
	case EvidencePlaneDurable:
		switch effect {
		case AuthorityEffectNone:
			return nil
		case AuthorityEffectRetrievalRanking:
			if authority == EvidenceAuthorityContextSignal || authority == EvidenceAuthorityReviewed || authority == EvidenceAuthorityPolicy {
				return nil
			}
			return ValidationError{Reason: ValidationReasonIllegalEffectForAuthority}
		case AuthorityEffectReviewSignal, AuthorityEffectInstructionSource:
			if authority == EvidenceAuthorityReviewed || authority == EvidenceAuthorityPolicy {
				return nil
			}
			return ValidationError{Reason: ValidationReasonIllegalEffectForAuthority}
		default:
			return ValidationError{Reason: ValidationReasonIllegalEffectForPlane}
		}
	default:
		return ValidationError{Reason: ValidationReasonUnknownPlane}
	}
}

func isCoherentTuple(meta EvidenceMeta) bool {
	switch meta.Source {
	case EvidenceSourceSemanticAnchor:
		return meta.SourcePlane == EvidencePlaneSemanticAnchor &&
			meta.EvidenceClass == EvidenceClassSourceComment &&
			meta.EvidenceAuthority == EvidenceAuthorityEvidenceOnly &&
			effectsExactly(meta, AuthorityEffectRetrievalRanking, AuthorityEffectReviewSignal)
	case EvidenceSourceGitCoChange:
		return meta.SourcePlane == EvidencePlaneGitEmpirical &&
			meta.EvidenceClass == EvidenceClassContextSignal &&
			meta.EvidenceAuthority == EvidenceAuthorityContextSignal &&
			effectsSubset(meta, AuthorityEffectRetrievalRanking, AuthorityEffectReviewWarning)
	case EvidenceSourceStructuralGraph:
		return meta.SourcePlane == EvidencePlaneStructural &&
			meta.EvidenceClass == EvidenceClassCodeFact &&
			meta.EvidenceAuthority == EvidenceAuthorityFact &&
			effectsSubset(meta, AuthorityEffectNone, AuthorityEffectRetrievalRanking, AuthorityEffectReviewSignal)
	case EvidenceSourceDurableMemory:
		return meta.SourcePlane == EvidencePlaneDurable &&
			meta.EvidenceClass == EvidenceClassReviewedMemory &&
			(meta.EvidenceAuthority == EvidenceAuthorityContextSignal ||
				meta.EvidenceAuthority == EvidenceAuthorityReviewed ||
				meta.EvidenceAuthority == EvidenceAuthorityPolicy)
	default:
		return false
	}
}

func requireEffect(meta EvidenceMeta, effect AuthorityEffect) error {
	if hasEffect(meta, effect) {
		return nil
	}
	return ValidationError{Reason: ValidationReasonMissingRequiredEffect}
}

func hasEffect(meta EvidenceMeta, effect AuthorityEffect) bool {
	for _, candidate := range meta.AllowedAuthorityEffects {
		if candidate == effect {
			return true
		}
	}
	return false
}

func effectsExactly(meta EvidenceMeta, effects ...AuthorityEffect) bool {
	if len(meta.AllowedAuthorityEffects) != len(effects) {
		return false
	}
	return effectsSubset(meta, effects...)
}

func effectsSubset(meta EvidenceMeta, allowed ...AuthorityEffect) bool {
	allowedSet := make(map[AuthorityEffect]struct{}, len(allowed))
	for _, effect := range allowed {
		allowedSet[effect] = struct{}{}
	}
	for _, effect := range meta.AllowedAuthorityEffects {
		if _, ok := allowedSet[effect]; !ok {
			return false
		}
	}
	return true
}
