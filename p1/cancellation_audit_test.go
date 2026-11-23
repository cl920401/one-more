package p1_test

import (
	"errors"
	"reflect"
	"testing"

	p1 "onemore.local/p1"
)

func TestTwoScopeCancellationsKeepDistinctApplicationAndOriginalRun(t *testing.T) {
	e, _, run := newRun(t)
	grant(t, e, run.ID, run.Spec.Materials)
	before, err := e.AuthorizationEvents(p1.Observer)
	must(t, err)
	expanded := spec()
	expanded.Materials = append(expanded.Materials, c)
	var proposals []p1.Record
	var cancellationEvents []p1.AuthorizationEvent

	for i := 0; i < 2; i++ {
		proposal, err := e.ChangeScope(p1.Employee, run.ID, expanded)
		must(t, err)
		proposals = append(proposals, proposal)
		must(t, e.CancelScopeChange(p1.Employee, proposal.ID))
		current, err := e.View(p1.Employee, run.ID)
		must(t, err)
		if current.State != "confirmed" || !current.Partial || !reflect.DeepEqual(current.Spec, run.Spec) {
			t.Fatal("audit change altered original behavior")
		}
		records, err := e.AuthorizationEvents(p1.Observer)
		must(t, err)
		event := records[len(records)-1]
		if event.Action != "scope-change-cancelled" || event.SubjectID != run.ID || event.RelatedRunID != run.ID ||
			event.CancellationObjectType != "scope-change-application" || event.CancellationObjectID != proposal.ID ||
			event.Epoch != current.AuthorizationEpoch {
			t.Fatalf("cancellation is not traceable to this application: %+v", event)
		}
		cancellationEvents = append(cancellationEvents, event)
		cancelled, err := e.View(p1.Employee, proposal.ID)
		must(t, err)
		if cancelled.State != "cancelled" || cancelled.RelatedRunID != run.ID {
			t.Fatal("application record not linked")
		}
		if _, err := e.ReadMaterial(p1.Employee, run.ID, c); !errors.Is(err, p1.ErrDenied) {
			t.Fatal("cancellation authorized proposed material")
		}
		if _, err := e.StartRequest(p1.Employee, proposal.ID); !errors.Is(err, p1.ErrState) {
			t.Fatal("cancelled application became executable")
		}
	}
	if proposals[0].ID == proposals[1].ID || cancellationEvents[0].ID == cancellationEvents[1].ID ||
		cancellationEvents[0].CancellationObjectID == cancellationEvents[1].CancellationObjectID {
		t.Fatal("two applications collapsed into one audit identity")
	}
	after, err := e.AuthorizationEvents(p1.Observer)
	must(t, err)
	if !reflect.DeepEqual(before, after[:len(before)]) {
		t.Fatal("prior authorization history overwritten")
	}
	for _, event := range after[len(before):] {
		if event.Action != "scope-paused" && event.Action != "scope-change-cancelled" {
			t.Fatalf("cancellation created permission event: %+v", event)
		}
		if event.SendScope != nil || len(event.Materials) != 0 {
			t.Fatal("cancellation carried new permissions or source data")
		}
	}
	ev := evidence(t, e)
	if len(ev.Reads)+len(ev.Outbound)+len(ev.Received) != 0 {
		t.Fatal("audit-only cancellation caused material or wire effects")
	}
	// The original explicit grants still work without renewal or enlargement.
	req, err := e.StartRequest(p1.Employee, run.ID)
	must(t, err)
	expectedScope, err := p1.SyntheticSendScope(run.Spec.Materials)
	must(t, err)
	if !reflect.DeepEqual(req.SendScope, expectedScope) {
		t.Fatal("cancellation changed original send grant")
	}
	must(t, e.CompleteRequest(p1.Employee, req.ID))
}

func TestWholeRunCancellationAuditDoesNotInventApplicationCancellation(t *testing.T) {
	for _, withProposal := range []bool{false, true} {
		t.Run(map[bool]string{false: "run-only", true: "with-active-application"}[withProposal], func(t *testing.T) {
			e, _, run := newRun(t)
			grant(t, e, run.ID, run.Spec.Materials)
			req, err := e.StartRequest(p1.Employee, run.ID)
			must(t, err)
			proposalID := ""
			if withProposal {
				expanded := spec()
				expanded.Materials = append(expanded.Materials, c)
				proposal, err := e.ChangeScope(p1.Employee, run.ID, expanded)
				must(t, err)
				proposalID = proposal.ID
			}
			before, err := e.AuthorizationEvents(p1.Observer)
			must(t, err)
			must(t, e.CancelRun(p1.Employee, run.ID))
			events, err := e.AuthorizationEvents(p1.Observer)
			must(t, err)
			for _, event := range events[len(before):] {
				if event.Action != "run-cancelled" || event.CancellationObjectType != "run" ||
					event.CancellationObjectID != run.ID || event.RelatedRunID != run.ID {
					t.Fatalf("whole-run cancellation falsely attributed: %+v", event)
				}
				if proposalID != "" && event.CancellationObjectID == proposalID {
					t.Fatal("application falsely named as cancellation object")
				}
			}
			want := 1
			if withProposal {
				want = 2
			}
			if len(events)-len(before) != want {
				t.Fatal("audit change added or lost cancellation facts")
			}
			if !reflect.DeepEqual(before, events[:len(before)]) {
				t.Fatal("historical prefix changed")
			}
			if err := e.CompleteRequest(p1.Employee, req.ID); !errors.Is(err, p1.ErrState) {
				t.Fatal("whole-run cancellation accepted late result")
			}
			if _, err := e.StartRequest(p1.Employee, run.ID); !errors.Is(err, p1.ErrState) {
				t.Fatal("whole-run cancellation resumed run")
			}
			if withProposal {
				if _, err := e.ConfirmTask(p1.Employee, proposalID); !errors.Is(err, p1.ErrState) {
					t.Fatal("cancelled application confirmed")
				}
			}
			end, err := e.Drain(p1.Observer)
			must(t, err)
			if !end.Idle {
				t.Fatal("cancellation left queue active")
			}
		})
	}
}

func TestRejectedOrRepeatedScopeCancellationDoesNotRewriteAudit(t *testing.T) {
	e, _, run := newRun(t)
	expanded := spec()
	expanded.Materials = append(expanded.Materials, c)
	proposal, err := e.ChangeScope(p1.Employee, run.ID, expanded)
	must(t, err)
	before, err := e.AuthorizationEvents(p1.Observer)
	must(t, err)
	if err := e.CancelScopeChange(p1.Authorizer, proposal.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("wrong actor cancellation accepted")
	}
	unchanged, err := e.AuthorizationEvents(p1.Observer)
	must(t, err)
	if !reflect.DeepEqual(before, unchanged) {
		t.Fatal("rejected cancellation wrote audit")
	}
	must(t, e.CancelScopeChange(p1.Employee, proposal.ID))
	accepted, err := e.AuthorizationEvents(p1.Observer)
	must(t, err)
	if err := e.CancelScopeChange(p1.Employee, proposal.ID); !errors.Is(err, p1.ErrState) {
		t.Fatal("repeated cancellation accepted")
	}
	unchanged, err = e.AuthorizationEvents(p1.Observer)
	must(t, err)
	if !reflect.DeepEqual(accepted, unchanged) {
		t.Fatal("repeated cancellation rewrote history")
	}
	accepted[len(accepted)-1].CancellationObjectID = "changed-snapshot"
	reread, err := e.AuthorizationEvents(p1.Observer)
	must(t, err)
	if reread[len(reread)-1].CancellationObjectID != proposal.ID {
		t.Fatal("audit snapshot mutated stored application identity")
	}
}

func TestCancellationOfRelatedRunNamesCurrentRunNotAncestor(t *testing.T) {
	e, _, original := newRun(t)
	must(t, e.CancelRun(p1.Employee, original.ID))
	initial, err := e.AuthorizationEvents(p1.Observer)
	must(t, err)
	preparation, err := e.NewRelatedPreparation(p1.Employee, original.ID, spec())
	must(t, err)
	next, err := e.ConfirmTask(p1.Employee, preparation.ID)
	must(t, err)
	if next.RelatedRunID != original.ID {
		t.Fatal("test requires an existing ancestor link")
	}
	must(t, e.CancelRun(p1.Employee, next.ID))
	events, err := e.AuthorizationEvents(p1.Observer)
	must(t, err)
	if !reflect.DeepEqual(initial, events[:len(initial)]) {
		t.Fatal("ancestor cancellation was rewritten")
	}
	event := events[len(events)-1]
	if event.SubjectID != next.ID || event.CancellationObjectType != "run" || event.CancellationObjectID != next.ID || event.RelatedRunID != next.ID {
		t.Fatalf("current cancellation misattributed to ancestor: %+v", event)
	}
}
