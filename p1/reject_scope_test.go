package p1_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	p1 "onemore.local/p1"
)

func rejectScopeCleanup(t *testing.T, e *p1.Engine) {
	t.Helper()
	t.Cleanup(func() {
		drain, err := e.Close(p1.Observer)
		if err != nil || !drain.Closed || !drain.Idle || drain.PendingRequests != 0 || drain.ReceiverQueueDepth != 0 {
			t.Errorf("rejection example did not drain: %+v, %v", drain, err)
		}
	})
}

func rejectScopeEvents(t *testing.T, e *p1.Engine) []p1.AuthorizationEvent {
	t.Helper()
	events, err := e.AuthorizationEvents(p1.Observer)
	must(t, err)
	return events
}

func TestRejectScopeChangeRecordsExactRequestWithoutGrantingOrSending(t *testing.T) {
	e, _, run := newRun(t)
	rejectScopeCleanup(t, e)
	must(t, e.GrantRead(p1.Authorizer, run.ID, []p1.Ref{a}))
	expanded := spec()
	expanded.Materials = append(expanded.Materials, c)
	expanded.Purpose = "private-synthetic-purpose-marker"
	expanded.Conditions = []string{"private-synthetic-condition-marker"}
	proposal, err := e.ChangeScope(p1.Employee, run.ID, expanded)
	must(t, err)
	beforeEvents := rejectScopeEvents(t, e)
	beforeEvidence := evidence(t, e)
	beforeRun, err := e.View(p1.Employee, run.ID)
	must(t, err)

	must(t, e.RejectScopeChange(p1.Authorizer, proposal.ID, p1.RejectScopeNotApproved))
	current, err := e.View(p1.Employee, run.ID)
	must(t, err)
	application, err := e.View(p1.Employee, proposal.ID)
	must(t, err)
	want := &p1.ScopeRejection{
		ApplicationID: proposal.ID, OriginalRunID: run.ID,
		DecidedBy: p1.Authorizer, ReasonCode: p1.RejectScopeNotApproved,
		RequestedMaterials: []p1.Ref{a, b, c}, PurposeChanged: true, ConditionsChanged: true,
	}
	if application.State != "rejected" || !reflect.DeepEqual(application.Rejection, want) {
		t.Fatalf("missing exact application rejection: %+v", application)
	}
	if current.State != "confirmed" || !current.Partial || !reflect.DeepEqual(current.Spec, run.Spec) || current.AuthorizationEpoch != beforeRun.AuthorizationEpoch+1 {
		t.Fatalf("rejection changed original conditions or epoch semantics: %+v", current)
	}
	events := rejectScopeEvents(t, e)
	if len(events) != len(beforeEvents)+1 || !reflect.DeepEqual(events[:len(beforeEvents)], beforeEvents) {
		t.Fatal("rejection added grants or rewrote earlier authorization events")
	}
	last := events[len(events)-1]
	if last.Action != "scope-change-rejected" || last.Actor != p1.Authorizer || last.SubjectID != run.ID || last.RelatedRunID != run.ID || last.Epoch != current.AuthorizationEpoch || !reflect.DeepEqual(last.ScopeRejection, want) {
		t.Fatalf("rejection not correlated with authorizer/original run/application: %+v", last)
	}
	if last.CancellationObjectType != "" || last.CancellationObjectID != "" || len(last.Materials) != 0 || last.SendScope != nil {
		t.Fatalf("rejection masqueraded as cancellation or a new grant: %+v", last)
	}
	encoded, err := json.Marshal(last)
	must(t, err)
	if strings.Contains(string(encoded), "private-synthetic-purpose-marker") || strings.Contains(string(encoded), "private-synthetic-condition-marker") {
		t.Fatal("rejection audit contains free-text purpose/conditions")
	}
	if !reflect.DeepEqual(evidence(t, e), beforeEvidence) {
		t.Fatal("rejecting a proposal performed material or transport I/O")
	}
	if _, err := e.ReadMaterial(p1.Employee, run.ID, a); err != nil {
		t.Fatalf("original authorized read was lost: %v", err)
	}
	for _, ref := range []p1.Ref{b, c} {
		if _, err := e.ReadMaterial(p1.Employee, run.ID, ref); !errors.Is(err, p1.ErrDenied) {
			t.Fatalf("rejection granted material %s: %v", ref.String(), err)
		}
	}
	if _, err := e.ReadMaterial(p1.Employee, proposal.ID, c); !errors.Is(err, p1.ErrState) {
		t.Fatalf("rejected preparation remained readable: %v", err)
	}
	if _, err := e.StartRequest(p1.Employee, run.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatalf("rejection granted send permission: %v", err)
	}
	if _, err := e.StartRequest(p1.Employee, proposal.ID); !errors.Is(err, p1.ErrState) {
		t.Fatalf("rejected preparation dispatched: %v", err)
	}
	if _, err := e.ConfirmTask(p1.Employee, proposal.ID); !errors.Is(err, p1.ErrState) {
		t.Fatalf("rejected proposal became a new run: %v", err)
	}
	ev := evidence(t, e)
	if len(ev.Reads) != 1 || len(ev.Outbound) != 0 || len(ev.Received) != 0 {
		t.Fatalf("denied new scope/send produced side effects: %+v", ev)
	}

	// Both outward-facing rejection snapshots must be independently owned.
	application.Rejection.RequestedMaterials[0].ID = "changed-record-copy"
	last.ScopeRejection.RequestedMaterials[1].ID = "changed-event-copy"
	readBack, err := e.View(p1.Employee, proposal.ID)
	must(t, err)
	if !reflect.DeepEqual(readBack.Rejection, want) || !reflect.DeepEqual(rejectScopeEvents(t, e)[len(events)-1].ScopeRejection, want) {
		t.Fatal("returned rejection copies can rewrite stored request evidence")
	}
}

func TestRejectScopeChangeDeniedCallsHaveNoEffects(t *testing.T) {
	e, confirmedPrep, run := newRun(t)
	rejectScopeCleanup(t, e)
	expanded := spec()
	expanded.Materials = append(expanded.Materials, c)
	proposal, err := e.ChangeScope(p1.Employee, run.ID, expanded)
	must(t, err)
	beforeEvents := rejectScopeEvents(t, e)
	beforeEvidence := evidence(t, e)
	beforeRun, err := e.View(p1.Employee, run.ID)
	must(t, err)
	beforeProposal, err := e.View(p1.Employee, proposal.ID)
	must(t, err)
	for _, tc := range []struct {
		name, actor, id, reason string
		want                    error
	}{
		{"employee", p1.Employee, proposal.ID, p1.RejectScopeNotApproved, p1.ErrDenied},
		{"observer", p1.Observer, proposal.ID, p1.RejectScopeNotApproved, p1.ErrDenied},
		{"empty reason", p1.Authorizer, proposal.ID, "", p1.ErrInput},
		{"free text reason", p1.Authorizer, proposal.ID, "private-rejection-reason-marker", p1.ErrInput},
		{"noncanonical reason", p1.Authorizer, proposal.ID, p1.RejectScopeNotApproved + " ", p1.ErrInput},
		{"unknown application", p1.Authorizer, "missing", p1.RejectScopeNotApproved, p1.ErrDenied},
		{"run object", p1.Authorizer, run.ID, p1.RejectScopeNotApproved, p1.ErrState},
		{"confirmed preparation", p1.Authorizer, confirmedPrep.ID, p1.RejectScopeNotApproved, p1.ErrState},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := e.RejectScopeChange(tc.actor, tc.id, tc.reason); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			currentRun, err := e.View(p1.Employee, run.ID)
			must(t, err)
			currentProposal, err := e.View(p1.Employee, proposal.ID)
			must(t, err)
			if !reflect.DeepEqual(currentRun, beforeRun) || !reflect.DeepEqual(currentProposal, beforeProposal) || !reflect.DeepEqual(rejectScopeEvents(t, e), beforeEvents) || !reflect.DeepEqual(evidence(t, e), beforeEvidence) {
				t.Fatal("denied rejection changed state, authorization or boundary evidence")
			}
		})
	}
}

func TestRejectScopeChangePreservesSentRequestAndExplicitPartialAdoption(t *testing.T) {
	e, _, run := newRun(t)
	rejectScopeCleanup(t, e)
	grant(t, e, run.ID, run.Spec.Materials)
	initial, err := e.StartRequest(p1.Employee, run.ID)
	must(t, err)
	expanded := spec()
	expanded.Materials = append(expanded.Materials, c)
	proposal, err := e.ChangeScope(p1.Employee, run.ID, expanded)
	must(t, err)
	beforeReject := evidence(t, e)
	must(t, e.RejectScopeChange(p1.Authorizer, proposal.ID, p1.RejectScopeNotApproved))
	if !reflect.DeepEqual(evidence(t, e), beforeReject) {
		t.Fatal("rejection dispatched or re-read a pending request")
	}
	if err := e.CompleteRequest(p1.Employee, initial.ID); !errors.Is(err, p1.ErrState) {
		t.Fatalf("pre-rejection result remained deliverable: %v", err)
	}
	if _, err := e.ReadMaterial(p1.Employee, run.ID, a); err != nil {
		t.Fatalf("original local read could not continue: %v", err)
	}
	if _, err := e.ReadMaterial(p1.Employee, run.ID, c); !errors.Is(err, p1.ErrDenied) {
		t.Fatalf("new material was inherited: %v", err)
	}
	// An explicit new request may use only the original, still-valid grant.
	continued, err := e.StartRequest(p1.Employee, run.ID)
	must(t, err)
	must(t, e.CompleteRequest(p1.Employee, continued.ID))
	requests, err := e.Requests(p1.Observer)
	must(t, err)
	if len(requests) != 2 || requests[0].ID != initial.ID || requests[0].State != "withheld" || requests[0].SimulatedUnits != 1 || requests[1].ID != continued.ID || requests[1].State != "returned" || requests[1].SimulatedUnits != 1 {
		t.Fatalf("new completion rewrote original request/counter: %+v", requests)
	}
	if err := e.RecordDecision(p1.Employee, run.ID, "采用", "部分草稿", true); !errors.Is(err, p1.ErrState) {
		t.Fatalf("partial draft marked original task complete: %v", err)
	}
	must(t, e.RecordDecision(p1.Employee, run.ID, "采用", "仅采用原范围部分草稿", false))
	current, err := e.View(p1.Employee, run.ID)
	must(t, err)
	if !current.Partial || !reflect.DeepEqual(current.Spec, run.Spec) || current.Decision == nil || current.Decision.OriginalTaskComplete || current.Decision.Outcome != "采用" {
		t.Fatalf("partial adoption lost original unfinished task: %+v", current)
	}
	drain, err := e.Drain(p1.Observer)
	must(t, err)
	if !drain.Idle || drain.PendingRequests != 0 || drain.ReceiverQueueDepth != 0 {
		t.Fatalf("rejection left request queues pending: %+v", drain)
	}
}

func TestRejectScopeChangeKeepsDistinctApplicationHistory(t *testing.T) {
	e, _, run := newRun(t)
	rejectScopeCleanup(t, e)
	expanded := spec()
	expanded.Materials = append(expanded.Materials, c)
	first, err := e.ChangeScope(p1.Employee, run.ID, expanded)
	must(t, err)
	must(t, e.RejectScopeChange(p1.Authorizer, first.ID, p1.RejectScopeNotApproved))
	firstSnapshot, err := e.View(p1.Employee, first.ID)
	must(t, err)
	firstEvents := rejectScopeEvents(t, e)
	second, err := e.ChangeScope(p1.Employee, run.ID, expanded)
	must(t, err)
	must(t, e.RejectScopeChange(p1.Authorizer, second.ID, p1.RejectScopeNotApproved))
	secondSnapshot, err := e.View(p1.Employee, second.ID)
	must(t, err)
	if first.ID == second.ID || firstSnapshot.Rejection == nil || secondSnapshot.Rejection == nil || firstSnapshot.Rejection.ApplicationID != first.ID || secondSnapshot.Rejection.ApplicationID != second.ID || firstSnapshot.Rejection.PurposeChanged || firstSnapshot.Rejection.ConditionsChanged {
		t.Fatal("successive rejections lost exact object or unchanged flags")
	}
	allEvents := rejectScopeEvents(t, e)
	if len(allEvents) != len(firstEvents)+2 || !reflect.DeepEqual(allEvents[:len(firstEvents)], firstEvents) {
		t.Fatal("second application rewrote first rejection history")
	}
	if err := e.RejectScopeChange(p1.Authorizer, first.ID, p1.RejectScopeNotApproved); !errors.Is(err, p1.ErrState) {
		t.Fatalf("already rejected application was rejected again: %v", err)
	}
	if !reflect.DeepEqual(rejectScopeEvents(t, e), allEvents) {
		t.Fatal("duplicate rejection appended an audit event")
	}
	for _, event := range allEvents {
		if event.Action == "scope-change-rejected" && (event.ScopeRejection == nil || event.ScopeRejection.OriginalRunID != run.ID || event.CancellationObjectType != "" || event.CancellationObjectID != "") {
			t.Fatalf("rejection history misidentified application/run: %+v", event)
		}
	}
	if _, err := e.ReadMaterial(p1.Employee, run.ID, a); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("repeated rejection manufactured a read grant")
	}
	if ev := evidence(t, e); len(ev.Reads)+len(ev.Outbound)+len(ev.Received) != 0 {
		t.Fatal("rejection history exercise performed boundary I/O")
	}
}

func TestRejectScopeChangeHonorsCancelledClosedAndUnpausedSubjects(t *testing.T) {
	for _, state := range []string{"cancelled", "closed", "unpaused-related-run"} {
		t.Run(state, func(t *testing.T) {
			e, _, run := newRun(t)
			rejectScopeCleanup(t, e)
			expanded := spec()
			expanded.Materials = append(expanded.Materials, c)
			proposal, err := e.ChangeScope(p1.Employee, run.ID, expanded)
			must(t, err)
			switch state {
			case "cancelled":
				must(t, e.CancelScopeChange(p1.Employee, proposal.ID))
			case "closed":
				_, err = e.Close(p1.Observer)
				must(t, err)
			case "unpaused-related-run":
				must(t, e.CancelRun(p1.Employee, run.ID))
				proposal, err = e.NewRelatedPreparation(p1.Employee, run.ID, expanded)
				must(t, err)
			}
			beforeEvents := rejectScopeEvents(t, e)
			beforeEvidence := evidence(t, e)
			beforeRecord, err := e.View(p1.Employee, proposal.ID)
			must(t, err)
			if err := e.RejectScopeChange(p1.Authorizer, proposal.ID, p1.RejectScopeNotApproved); !errors.Is(err, p1.ErrState) {
				t.Fatalf("rejected %s subject: %v", state, err)
			}
			afterRecord, err := e.View(p1.Employee, proposal.ID)
			must(t, err)
			if !reflect.DeepEqual(beforeRecord, afterRecord) || !reflect.DeepEqual(beforeEvents, rejectScopeEvents(t, e)) || !reflect.DeepEqual(beforeEvidence, evidence(t, e)) {
				t.Fatal("terminal/unpaused rejection mutated evidence")
			}
		})
	}
}

func TestRejectScopeChangeConditionsCompareExactOrder(t *testing.T) {
	e := p1.New()
	rejectScopeCleanup(t, e)
	original := spec()
	original.Conditions = []string{"先核对期限", "再核对报价"}
	prep, err := e.CreatePreparation(p1.Employee, original)
	must(t, err)
	run, err := e.ConfirmTask(p1.Employee, prep.ID)
	must(t, err)
	expanded := spec()
	expanded.Conditions = []string{"再核对报价", "先核对期限"}
	proposal, err := e.ChangeScope(p1.Employee, run.ID, expanded)
	must(t, err)
	must(t, e.RejectScopeChange(p1.Authorizer, proposal.ID, p1.RejectScopeNotApproved))
	record, err := e.View(p1.Employee, proposal.ID)
	must(t, err)
	if record.Rejection == nil || !record.Rejection.ConditionsChanged || record.Rejection.PurposeChanged {
		t.Fatalf("ordered conditions were treated as the same request: %+v", record.Rejection)
	}
}
