package evidence

type ValidationReason string

const (
	ValidationReasonUnknownPlane              ValidationReason = "unknown_plane"
	ValidationReasonUnknownAuthority          ValidationReason = "unknown_authority"
	ValidationReasonUnknownSource             ValidationReason = "unknown_source"
	ValidationReasonUnknownClass              ValidationReason = "unknown_class"
	ValidationReasonUnknownEffect             ValidationReason = "unknown_effect"
	ValidationReasonEmptyEffects              ValidationReason = "empty_effects"
	ValidationReasonDuplicateEffect           ValidationReason = "duplicate_effect"
	ValidationReasonNoneNotExclusive          ValidationReason = "none_not_exclusive"
	ValidationReasonIllegalEffectForPlane     ValidationReason = "illegal_effect_for_plane"
	ValidationReasonIllegalEffectForAuthority ValidationReason = "illegal_effect_for_authority"
	ValidationReasonIllegalRenderSurface      ValidationReason = "illegal_render_surface"
	ValidationReasonMissingRequiredEffect     ValidationReason = "missing_required_effect"
	ValidationReasonIncoherentEvidenceTuple   ValidationReason = "incoherent_evidence_tuple"
)

type ValidationError struct {
	Reason ValidationReason
}

func (e ValidationError) Error() string {
	if e.Reason == "" {
		return "evidence validation failed"
	}
	return "evidence validation failed: " + string(e.Reason)
}
