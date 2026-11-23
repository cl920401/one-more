package p1_test

import (
	"errors"
	"testing"

	p1 "onemore.local/p1"
)

// These development regressions use only the exported, synthetic-only API.
func scopeFixRun(t *testing.T) (*p1.Engine, p1.Record, p1.Record) {
	t.Helper()
	e := p1.New()
	t.Cleanup(func() {
		drain, err := e.Close(p1.Observer)
		if err != nil || !drain.Closed || !drain.Idle || drain.PendingRequests != 0 || drain.ReceiverQueueDepth != 0 {
			t.Errorf("close did not drain synthetic work: %+v, %v", drain, err)
		}
	})
	original := p1.Spec{
		Purpose: "本地扩围读取开发反例",
		Materials: []p1.Ref{
			{ID: "S-A", Version: "v1"},
			{ID: "S-B", Version: "v1"},
		},
		Conditions: []string{"核对原范围材料，新增范围另行确认"},
	}
	prep, err := e.CreatePreparation(p1.Employee, original)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.GrantRead(p1.Authorizer, prep.ID, original.Materials); err != nil {
		t.Fatal(err)
	}
	run, err := e.ConfirmTask(p1.Employee, prep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.GrantRead(p1.Authorizer, run.ID, original.Materials); err != nil {
		t.Fatal(err)
	}
	return e, prep, run
}

func scopeFixProposal(t *testing.T, e *p1.Engine, run p1.Record) p1.Record {
	t.Helper()
	expanded := run.Spec
	expanded.Materials = append([]p1.Ref(nil), run.Spec.Materials...)
	expanded.Materials = append(expanded.Materials, p1.Ref{ID: "S-C", Version: "v1"})
	proposal, err := e.ChangeScope(p1.Employee, run.ID, expanded)
	if err != nil {
		t.Fatal(err)
	}
	return proposal
}

func scopeFixEvidence(t *testing.T, e *p1.Engine, readCount int) p1.Evidence {
	t.Helper()
	ev, err := e.Evidence(p1.Observer)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.Reads) != readCount || len(ev.Outbound) != 0 || len(ev.Received) != 0 {
		t.Fatalf("unexpected read/dispatch/receive events: %+v", ev)
	}
	return ev
}

func TestScopeChangePauseKeepsOnlyAuthorizedOriginalReads(t *testing.T) {
	e, _, run := scopeFixRun(t)
	proposal := scopeFixProposal(t, e, run)
	a := run.Spec.Materials[0]
	b := run.Spec.Materials[1]
	c := p1.Ref{ID: "S-C", Version: "v1"}
	current, err := e.View(p1.Employee, run.ID)
	if err != nil || current.State != "paused" || len(current.Spec.Materials) != 2 || current.Spec.Materials[0] != a || current.Spec.Materials[1] != b {
		t.Fatalf("scope proposal changed original SPEC: %+v, %v", current, err)
	}
	scopeFixEvidence(t, e, 0)
	material, err := e.ReadMaterial(p1.Employee, run.ID, a)
	if err != nil || material.Ref != a || len(material.Paragraphs) == 0 {
		t.Fatalf("unaffected authorized A was blocked: %+v, %v", material, err)
	}
	if _, err := e.ReadMaterial(p1.Employee, run.ID, c); !errors.Is(err, p1.ErrDenied) {
		t.Fatalf("paused old run read new C: %v", err)
	}
	if _, err := e.ReadMaterial(p1.Employee, proposal.ID, a); !errors.Is(err, p1.ErrDenied) {
		t.Fatalf("proposal inherited original A authorization: %v", err)
	}
	if _, err := e.ReadMaterial(p1.Observer, run.ID, a); !errors.Is(err, p1.ErrDenied) {
		t.Fatalf("wrong actor read original A: %v", err)
	}
	if _, err := e.StartRequest(p1.Employee, run.ID); !errors.Is(err, p1.ErrState) {
		t.Fatalf("paused original run dispatched despite lifecycle gate: %v", err)
	}
	if err := e.RevokeRead(p1.Authorizer, run.ID, b); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ReadMaterial(p1.Employee, run.ID, b); !errors.Is(err, p1.ErrDenied) {
		t.Fatalf("paused run read revoked B: %v", err)
	}
	if _, err := e.StartRequest(p1.Employee, proposal.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatalf("unapproved proposal dispatched: %v", err)
	}
	ev := scopeFixEvidence(t, e, 1)
	if ev.Reads[0].SubjectID != run.ID || ev.Reads[0].Phase != "execution" || len(ev.Reads[0].Materials) != 1 || ev.Reads[0].Materials[0] != a {
		t.Fatalf("successful read was not limited to original A: %+v", ev.Reads[0])
	}
}

func TestScopeChangeCancellationContinuesCurrentOriginalReadScope(t *testing.T) {
	e, _, run := scopeFixRun(t)
	proposal := scopeFixProposal(t, e, run)
	a := run.Spec.Materials[0]
	b := run.Spec.Materials[1]
	c := p1.Ref{ID: "S-C", Version: "v1"}
	if err := e.RevokeRead(p1.Authorizer, run.ID, b); err != nil {
		t.Fatal(err)
	}
	if err := e.CancelScopeChange(p1.Employee, proposal.ID); err != nil {
		t.Fatal(err)
	}
	current, err := e.View(p1.Employee, run.ID)
	if err != nil || current.State != "confirmed" || !current.Partial || len(current.Spec.Materials) != 2 {
		t.Fatalf("cancel expansion did not preserve original partial run: %+v, %v", current, err)
	}
	if _, err := e.ReadMaterial(p1.Employee, run.ID, a); err != nil {
		t.Fatalf("original A could not continue after scope cancellation: %v", err)
	}
	for _, ref := range []p1.Ref{b, c} {
		if _, err := e.ReadMaterial(p1.Employee, run.ID, ref); !errors.Is(err, p1.ErrDenied) {
			t.Fatalf("scope cancellation granted revoked/new material %s: %v", ref.String(), err)
		}
	}
	if _, err := e.ReadMaterial(p1.Employee, proposal.ID, a); !errors.Is(err, p1.ErrState) {
		t.Fatalf("cancelled preparation read material: %v", err)
	}
	scopeFixEvidence(t, e, 1)
}

func TestScopeChangeWholeRunCancellationStillTerminatesReads(t *testing.T) {
	e, _, run := scopeFixRun(t)
	proposal := scopeFixProposal(t, e, run)
	if err := e.CancelRun(p1.Employee, run.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{run.ID, proposal.ID} {
		if _, err := e.ReadMaterial(p1.Employee, id, run.Spec.Materials[0]); !errors.Is(err, p1.ErrState) {
			t.Fatalf("whole-run cancellation allowed read on %s: %v", id, err)
		}
	}
	if _, err := e.StartRequest(p1.Employee, run.ID); !errors.Is(err, p1.ErrState) {
		t.Fatalf("cancelled original run dispatched: %v", err)
	}
	if err := e.CancelScopeChange(p1.Employee, proposal.ID); !errors.Is(err, p1.ErrState) {
		t.Fatalf("scope cancellation revived a cancelled run: %v", err)
	}
	current, err := e.View(p1.Employee, run.ID)
	if err != nil || current.State != "cancelled" {
		t.Fatalf("run cancellation was not terminal: %+v, %v", current, err)
	}
	scopeFixEvidence(t, e, 0)
}

func TestScopeChangeReadFixKeepsConfirmedPreparationAndClosedEngineBlocked(t *testing.T) {
	e, prep, run := scopeFixRun(t)
	a := run.Spec.Materials[0]
	if _, err := e.ReadMaterial(p1.Employee, prep.ID, a); !errors.Is(err, p1.ErrState) {
		t.Fatalf("confirmed preparation remained readable: %v", err)
	}
	scopeFixProposal(t, e, run)
	if _, err := e.ReadMaterial(p1.Employee, run.ID, a); err != nil {
		t.Fatalf("positive control for paused original A failed: %v", err)
	}
	if _, err := e.Close(p1.Observer); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ReadMaterial(p1.Employee, run.ID, a); !errors.Is(err, p1.ErrState) {
		t.Fatalf("closed engine allowed paused original read: %v", err)
	}
	scopeFixEvidence(t, e, 1)
}
