package p1

import "slices"

// RejectScopeNotApproved is the only supported rejection reason. Free-text
// reasons and material bodies are deliberately excluded from rejection audit.
const RejectScopeNotApproved = "scope-not-approved"

type ScopeRejection struct {
	ApplicationID      string
	OriginalRunID      string
	DecidedBy          string
	ReasonCode         string
	RequestedMaterials []Ref
	PurposeChanged     bool
	ConditionsChanged  bool
}

func cloneRejection(rejection *ScopeRejection) *ScopeRejection {
	if rejection == nil {
		return nil
	}
	out := *rejection
	out.RequestedMaterials = append([]Ref(nil), rejection.RequestedMaterials...)
	return &out
}

// finishScopeChange preserves the original scope and grants while ending an
// application. Callers hold e.mu and validate the application/run relationship.
// A pre-pause request never becomes eligible for delivery after this transition.
func (e *Engine) finishScopeChange(p, old *subject, applicationState string) {
	p.record.State = applicationState
	e.cancelPending(p.record.ID, applicationState)
	old.record.State = old.beforePause
	if old.beforePause == "pending" || old.beforePause == "queued" {
		e.cancelPending(old.record.ID, "withheld")
		old.record.State = "confirmed"
	}
	old.record.Partial = true
}

// RejectScopeChange is an explicit authorizer decision, distinct from employee
// cancellation. It grants nothing and changes no confirmed task conditions.
func (e *Engine) RejectScopeChange(actor, preparationID, reasonCode string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if actor != Authorizer {
		return ErrDenied
	}
	if e.closed {
		return ErrState
	}
	if reasonCode != RejectScopeNotApproved {
		return ErrInput
	}
	p, ok := e.subjects[preparationID]
	if !ok {
		return ErrDenied
	}
	if p.record.Phase != "preparation" || p.record.RelatedRunID == "" {
		return ErrState
	}
	switch p.record.State {
	case "confirmed", "cancelled", "rejected", "closed":
		return ErrState
	}
	old, ok := e.subjects[p.record.RelatedRunID]
	if !ok || old.record.Phase != "execution" || old.record.State != "paused" {
		return ErrState
	}

	rejection := &ScopeRejection{
		ApplicationID:      p.record.ID,
		OriginalRunID:      old.record.ID,
		DecidedBy:          actor,
		ReasonCode:         reasonCode,
		RequestedMaterials: append([]Ref(nil), p.record.Spec.Materials...),
		PurposeChanged:     p.record.Spec.Purpose != old.record.Spec.Purpose,
		ConditionsChanged:  !slices.Equal(p.record.Spec.Conditions, old.record.Spec.Conditions),
	}
	p.record.Rejection = cloneRejection(rejection)
	e.finishScopeChange(p, old, "rejected")
	e.authorization(old, "scope-change-rejected", nil)
	event := &e.authorizations[len(e.authorizations)-1]
	event.RelatedRunID = old.record.ID
	event.ScopeRejection = cloneRejection(rejection)
	return nil
}
