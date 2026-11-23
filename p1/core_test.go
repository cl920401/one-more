package p1_test

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	p1 "onemore.local/p1"
)

var a = p1.Ref{ID: "S-A", Version: "v1"}
var b = p1.Ref{ID: "S-B", Version: "v1"}
var c = p1.Ref{ID: "S-C", Version: "v1"}

func spec() p1.Spec {
	return p1.Spec{Purpose: "内部合成需求核对", Materials: []p1.Ref{a, b},
		Conditions: []string{"核对期限、报价、验收标准和精确引用"}}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newRun(t *testing.T) (*p1.Engine, p1.Record, p1.Record) {
	t.Helper()
	e := p1.New()
	prep, err := e.CreatePreparation(p1.Employee, spec())
	must(t, err)
	run, err := e.ConfirmTask(p1.Employee, prep.ID)
	must(t, err)
	return e, prep, run
}

func grant(t *testing.T, e *p1.Engine, id string, refs []p1.Ref) {
	t.Helper()
	must(t, e.GrantRead(p1.Authorizer, id, refs))
	scope, err := p1.SyntheticSendScope(refs)
	must(t, err)
	must(t, e.GrantSend(p1.Authorizer, id, scope))
}

func evidence(t *testing.T, e *p1.Engine) p1.Evidence {
	t.Helper()
	ev, err := e.Evidence(p1.Observer)
	must(t, err)
	return ev
}

func produce(t *testing.T, e *p1.Engine, run p1.Record) p1.Draft {
	t.Helper()
	grant(t, e, run.ID, run.Spec.Materials)
	req, err := e.StartRequest(p1.Employee, run.ID)
	must(t, err)
	must(t, e.CompleteRequest(p1.Employee, req.ID))
	draft, err := e.ViewDraft(p1.Employee, run.ID)
	must(t, err)
	return draft
}

func TestPreparationIsASeparateAuthorizedPhase(t *testing.T) {
	e := p1.New()
	prep, err := e.CreatePreparation(p1.Employee, spec())
	must(t, err)
	if _, err := e.StartRequest(p1.Employee, prep.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatalf("unapproved draft: %v", err)
	}
	if ev := evidence(t, e); len(ev.Reads)+len(ev.Outbound)+len(ev.Received) != 0 {
		t.Fatal("fixed form performed body I/O")
	}
	grant(t, e, prep.ID, prep.Spec.Materials)
	req, err := e.StartRequest(p1.Employee, prep.ID)
	must(t, err)
	if _, err := e.ConfirmTask(p1.Employee, prep.ID); !errors.Is(err, p1.ErrState) {
		t.Fatal("confirmed while drafting pending")
	}
	must(t, e.CompleteRequest(p1.Employee, req.ID))
	run, err := e.ConfirmTask(p1.Employee, prep.ID)
	must(t, err)
	if run.ID == prep.ID || run.PreparationID != prep.ID || run.SpecVersion == "" {
		t.Fatal("phase identity lost")
	}
	if _, err := e.StartRequest(p1.Employee, run.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("preparation grants inherited")
	}
	if _, err := e.StartRequest(p1.Employee, prep.ID); !errors.Is(err, p1.ErrState) {
		t.Fatal("closed preparation executed")
	}
	requests, err := e.Requests(p1.Observer)
	must(t, err)
	if len(requests) != 1 || requests[0].Phase != "preparation" || requests[0].SubjectID != prep.ID || requests[0].SimulatedUnits != 1 {
		t.Fatalf("preparation request missing: %+v", requests)
	}
}

func TestUnauthorizedReadsAndReadDoesNotGrantSend(t *testing.T) {
	e, _, run := newRun(t)
	if _, err := e.ReadMaterial(p1.Employee, run.ID, a); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("unapproved read")
	}
	if err := e.GrantRead(p1.Employee, run.ID, []p1.Ref{a}); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("employee self-authorized")
	}
	must(t, e.GrantRead(p1.Authorizer, run.ID, []p1.Ref{a}))
	if _, err := e.ReadMaterial(p1.Employee, run.ID, c); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("out-of-scope read")
	}
	_, err := e.ReadMaterial(p1.Employee, run.ID, a)
	must(t, err)
	if _, err := e.StartRequest(p1.Employee, run.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("local read implied send")
	}
	ev := evidence(t, e)
	if len(ev.Reads) != 1 || len(ev.Outbound) != 0 || len(ev.Received) != 0 {
		t.Fatalf("unexpected side effects: %+v", ev)
	}
	must(t, e.GrantRead(p1.Authorizer, run.ID, []p1.Ref{b}))
	if _, err := e.StartRequest(p1.Employee, run.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("all reads implied send")
	}
	grant(t, e, run.ID, run.Spec.Materials)
	req, err := e.StartRequest(p1.Employee, run.ID)
	must(t, err)
	must(t, e.CompleteRequest(p1.Employee, req.ID))
	ev = evidence(t, e)
	if len(ev.Reads) != 3 || len(ev.Outbound) != 1 || len(ev.Received) != 1 {
		t.Fatal("positive control did not exercise all boundaries")
	}
	if ev.Outbound[0].RequestID != req.ID || ev.Received[0].RequestID != req.ID || ev.Reads[1].RequestID != req.ID {
		t.Fatal("boundary correlation lost")
	}
	if ev.Outbound[0].AuthorizationEpoch != req.AuthorizationEpoch || ev.Received[0].At.Before(ev.Outbound[0].At) {
		t.Fatal("authorization/time correlation lost")
	}
	audit, err := e.AuthorizationEvents(p1.Observer)
	must(t, err)
	if len(audit) == 0 || audit[len(audit)-1].Epoch != req.AuthorizationEpoch {
		t.Fatal("missing authorization point")
	}
}

func TestDraftHasThreeCategoriesExactCitationsAndNoAutomaticDecision(t *testing.T) {
	e, _, run := newRun(t)
	d := produce(t, e, run)
	seen := map[string]bool{}
	for _, entry := range d.Entries {
		seen[entry.Category] = true
		if len(entry.Citations) == 0 {
			t.Fatal("uncited entry")
		}
		for _, cite := range entry.Citations {
			paragraph, err := e.OpenCitation(p1.Employee, run.ID, cite)
			must(t, err)
			if paragraph.ID != cite.Paragraph || paragraph.Text == "" {
				t.Fatal("citation not resolvable")
			}
		}
	}
	if len(d.Entries) != 4 || len(seen) != 3 || d.CustomerConfirmed {
		t.Fatalf("unexpected draft: %+v", d)
	}
	rec, err := e.View(p1.Employee, run.ID)
	must(t, err)
	if rec.Decision != nil {
		t.Fatal("automatic employee adoption")
	}
	must(t, e.RecordDecision(p1.Employee, run.ID, "采用", "合成员工明确采用部分草稿", false))
	rec, err = e.View(p1.Employee, run.ID)
	must(t, err)
	if rec.Decision == nil || rec.Decision.OriginalTaskComplete || rec.Decision.Version != d.Version {
		t.Fatal("adoption conflated with completion")
	}
	if _, err := e.ChangeScope(p1.Employee, run.ID, spec()); !errors.Is(err, p1.ErrState) {
		t.Fatal("completed decision history rewritten")
	}
}

func TestPendingResultCannotBypassRevocation(t *testing.T) {
	for _, kind := range []string{"read", "send", "revoke-and-regrant"} {
		t.Run(kind, func(t *testing.T) {
			e, _, run := newRun(t)
			grant(t, e, run.ID, run.Spec.Materials)
			req, err := e.StartRequest(p1.Employee, run.ID)
			must(t, err)
			if kind == "send" {
				must(t, e.RevokeSend(p1.Authorizer, run.ID))
			} else {
				must(t, e.RevokeRead(p1.Authorizer, run.ID, a))
			}
			if kind == "revoke-and-regrant" {
				must(t, e.GrantRead(p1.Authorizer, run.ID, []p1.Ref{a}))
			}
			if err := e.CompleteRequest(p1.Employee, req.ID); !errors.Is(err, p1.ErrDenied) {
				t.Fatal("stale result delivered")
			}
			if _, err := e.ViewDraft(p1.Employee, run.ID); err == nil {
				t.Fatal("withheld draft visible")
			}
			requests, err := e.Requests(p1.Observer)
			must(t, err)
			if requests[0].State != "withheld" || requests[0].SimulatedUnits != 1 {
				t.Fatal("sent history or simulated cost erased")
			}
			drain, err := e.Drain(p1.Observer)
			must(t, err)
			if !drain.Idle || drain.PendingRequests != 0 || drain.ReceiverQueueDepth != 0 {
				t.Fatal("withheld request not drained")
			}
		})
	}
}

func TestOldCitationAndDerivedDecisionAreHiddenAfterRevocation(t *testing.T) {
	e, _, run := newRun(t)
	d := produce(t, e, run)
	must(t, e.RecordDecision(p1.Employee, run.ID, "改后采用", "独特合成修订理由", false))
	var cite p1.Citation
	for _, entry := range d.Entries {
		for _, candidate := range entry.Citations {
			if candidate.Ref == a {
				cite = candidate
			}
		}
	}
	must(t, e.RevokeRead(p1.Authorizer, run.ID, a))
	if _, err := e.OpenCitation(p1.Employee, run.ID, cite); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("old link bypassed revocation")
	}
	if _, err := e.ViewDraft(p1.Employee, run.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("derived draft bypassed revocation")
	}
	rec, err := e.View(p1.Employee, run.ID)
	must(t, err)
	if rec.Decision.Reason != "" {
		t.Fatal("decision reason bypassed revocation")
	}
}

func TestScopePauseNewConfirmationAndCancellationAreDifferent(t *testing.T) {
	e, _, run := newRun(t)
	grant(t, e, run.ID, run.Spec.Materials)
	expanded := spec()
	expanded.Materials = append(expanded.Materials, c)
	change, err := e.ChangeScope(p1.Employee, run.ID, expanded)
	must(t, err)
	if _, err := e.StartRequest(p1.Employee, run.ID); !errors.Is(err, p1.ErrState) {
		t.Fatal("paused run executed")
	}
	if _, err := e.ReadMaterial(p1.Employee, run.ID, a); err != nil {
		t.Fatal("unaffected authorized read paused", err)
	}
	if ev := evidence(t, e); len(ev.Reads) != 1 || len(ev.Outbound) != 0 || len(ev.Received) != 0 {
		t.Fatal("scope proposal granted sending or extra reading")
	}
	must(t, e.CancelScopeChange(p1.Employee, change.ID))
	if _, err := e.ConfirmTask(p1.Employee, change.ID); !errors.Is(err, p1.ErrState) {
		t.Fatal("cancelled expansion confirmed")
	}
	rec, err := e.View(p1.Employee, run.ID)
	must(t, err)
	if rec.State != "confirmed" || !rec.Partial || len(rec.Spec.Materials) != 2 {
		t.Fatal("cancel expansion changed original scope")
	}
	if _, err := e.ReadMaterial(p1.Employee, run.ID, c); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("cancel expansion authorized C")
	}
	req, err := e.StartRequest(p1.Employee, run.ID)
	must(t, err)
	must(t, e.CompleteRequest(p1.Employee, req.ID))
	if err := e.RecordDecision(p1.Employee, run.ID, "采用", "部分合成草稿", true); !errors.Is(err, p1.ErrState) {
		t.Fatal("partial scope marked complete")
	}
}

func TestExpandedRunIsNewAndOldDraftIsNotRewritten(t *testing.T) {
	e, _, run := newRun(t)
	before := produce(t, e, run)
	expanded := spec()
	expanded.Materials = append(expanded.Materials, c)
	prep, err := e.ChangeScope(p1.Employee, run.ID, expanded)
	must(t, err)
	next, err := e.ConfirmTask(p1.Employee, prep.ID)
	must(t, err)
	if next.ID == run.ID || next.RelatedRunID != run.ID {
		t.Fatal("new confirmation did not create linked run")
	}
	if _, err := e.StartRequest(p1.Employee, next.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("new run inherited old grant")
	}
	after := produce(t, e, next)
	for _, entry := range after.Entries {
		if entry.ID == "alpha-retention" && entry.Category != "明确" {
			t.Fatal("approved formal replacement not used")
		}
	}
	original, err := e.ViewDraft(p1.Employee, run.ID)
	must(t, err)
	left, _ := json.Marshal(before)
	right, _ := json.Marshal(original)
	if string(left) != string(right) {
		t.Fatal("old draft was rewritten")
	}
}

func TestCancelRunRejectsLateResultAndRequiresNewRun(t *testing.T) {
	e, _, run := newRun(t)
	grant(t, e, run.ID, run.Spec.Materials)
	req, err := e.StartRequest(p1.Employee, run.ID)
	must(t, err)
	must(t, e.CancelRun(p1.Employee, run.ID))
	if err := e.CompleteRequest(p1.Employee, req.ID); !errors.Is(err, p1.ErrState) {
		t.Fatal("cancelled run accepted late response")
	}
	if _, err := e.StartRequest(p1.Employee, run.ID); !errors.Is(err, p1.ErrState) {
		t.Fatal("cancelled run resumed")
	}
	if _, err := e.ReadMaterial(p1.Employee, run.ID, a); !errors.Is(err, p1.ErrState) {
		t.Fatal("cancelled run processed material")
	}
	if _, err := e.ChangeScope(p1.Employee, run.ID, spec()); !errors.Is(err, p1.ErrState) {
		t.Fatal("scope change revived cancelled run")
	}
	prep, err := e.NewRelatedPreparation(p1.Employee, run.ID, spec())
	must(t, err)
	next, err := e.ConfirmTask(p1.Employee, prep.ID)
	must(t, err)
	if next.RelatedRunID != run.ID || next.ID == run.ID {
		t.Fatal("restart was not separate")
	}
	if _, err := e.StartRequest(p1.Employee, next.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("restart inherited authorization")
	}
	drain, err := e.Drain(p1.Observer)
	must(t, err)
	if !drain.Idle {
		t.Fatal("cancel left pending response")
	}
}

func TestSnapshotsCannotMutateAuthorityOrHistoricalContent(t *testing.T) {
	e := p1.New()
	input := spec()
	prep, err := e.CreatePreparation(p1.Employee, input)
	must(t, err)
	input.Materials[0] = c
	input.Conditions[0] = "changed"
	prep.Spec.Materials[0] = c
	run, err := e.ConfirmTask(p1.Employee, prep.ID)
	must(t, err)
	if run.Spec.Materials[0] != a || run.Spec.Conditions[0] == "changed" {
		t.Fatal("input aliases changed confirmed spec")
	}
	d := produce(t, e, run)
	d.Entries[0].Citations[0].Ref = c
	d.Entries[0].Description = "changed"
	material, err := e.ReadMaterial(p1.Employee, run.ID, a)
	must(t, err)
	original := material.Paragraphs[0].Text
	material.Paragraphs[0].Text = "changed"
	material, err = e.ReadMaterial(p1.Employee, run.ID, a)
	must(t, err)
	if material.Paragraphs[0].Text != original {
		t.Fatal("reader returned mutable storage")
	}
	draft, err := e.ViewDraft(p1.Employee, run.ID)
	must(t, err)
	if draft.Entries[0].Description == "changed" || draft.Entries[0].Citations[0].Ref == c {
		t.Fatal("draft alias changed history")
	}
	ev := evidence(t, e)
	ev.Reads[0].Materials[0] = c
	if evidence(t, e).Reads[0].Materials[0] != a {
		t.Fatal("observer mutated journal")
	}
}

func TestObserverPrivacyAndEndDrainSignal(t *testing.T) {
	e, _, run := newRun(t)
	if _, err := e.Evidence(p1.Employee); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("employee got observer access")
	}
	if _, err := e.View("synthetic-other", run.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("cross-actor record access")
	}
	grant(t, e, run.ID, run.Spec.Materials)
	_, err := e.StartRequest(p1.Employee, run.ID)
	must(t, err)
	drain, err := e.Drain(p1.Observer)
	must(t, err)
	if drain.Idle || drain.PendingRequests != 1 || drain.ReceiverQueueDepth != 1 {
		t.Fatal("pending signal inaccurate")
	}
	drain, err = e.Close(p1.Observer)
	must(t, err)
	if !drain.Closed || !drain.Idle {
		t.Fatal("close did not finish and drain")
	}
	if _, err := e.StartRequest(p1.Employee, run.ID); !errors.Is(err, p1.ErrState) {
		t.Fatal("closed engine dispatched")
	}
	blob, err := json.Marshal(evidence(t, e))
	must(t, err)
	if strings.Contains(string(blob), "合成业务代表") || strings.Contains(string(blob), "90 天") || strings.Contains(string(blob), "Text") {
		t.Fatal("body leaked to observer journal")
	}
}

func TestConcurrentRevokeBeforeDelivery(t *testing.T) {
	e, _, run := newRun(t)
	grant(t, e, run.ID, run.Spec.Materials)
	req, err := e.StartRequest(p1.Employee, run.ID)
	must(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = e.Evidence(p1.Observer) }()
	}
	must(t, e.RevokeRead(p1.Authorizer, run.ID, a))
	wg.Wait()
	if err := e.CompleteRequest(p1.Employee, req.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("revoke-before-delivery lost")
	}
}

func TestCancelScopeWithPendingOriginalAllowsExplicitNewRequest(t *testing.T) {
	for _, arrivesFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel-before-result", true: "result-before-cancel"}[arrivesFirst], func(t *testing.T) {
			e, _, run := newRun(t)
			grant(t, e, run.ID, run.Spec.Materials)
			old, err := e.StartRequest(p1.Employee, run.ID)
			must(t, err)
			expanded := spec()
			expanded.Materials = append(expanded.Materials, c)
			change, err := e.ChangeScope(p1.Employee, run.ID, expanded)
			must(t, err)
			if arrivesFirst {
				if err := e.CompleteRequest(p1.Employee, old.ID); !errors.Is(err, p1.ErrDenied) {
					t.Fatal("paused result delivered")
				}
			}
			must(t, e.CancelScopeChange(p1.Employee, change.ID))
			if err := e.CompleteRequest(p1.Employee, old.ID); !errors.Is(err, p1.ErrState) {
				t.Fatal("old pending response remained live")
			}
			rec, err := e.View(p1.Employee, run.ID)
			must(t, err)
			if rec.State != "confirmed" || !rec.Partial {
				t.Fatalf("original cannot continue: %+v", rec)
			}
			req, err := e.StartRequest(p1.Employee, run.ID)
			must(t, err)
			if req.ID == old.ID {
				t.Fatal("request history reused")
			}
			must(t, e.CompleteRequest(p1.Employee, req.ID))
			drain, err := e.Drain(p1.Observer)
			must(t, err)
			if !drain.Idle {
				t.Fatal("stale pending remains")
			}
		})
	}
}

func TestCancelWholeRunAlsoCancelsUnconfirmedExpansion(t *testing.T) {
	e, _, run := newRun(t)
	expanded := spec()
	expanded.Materials = append(expanded.Materials, c)
	proposal, err := e.ChangeScope(p1.Employee, run.ID, expanded)
	must(t, err)
	grant(t, e, proposal.ID, proposal.Spec.Materials)
	req, err := e.StartRequest(p1.Employee, proposal.ID)
	must(t, err)
	must(t, e.CancelRun(p1.Employee, run.ID))
	if err := e.CompleteRequest(p1.Employee, req.ID); !errors.Is(err, p1.ErrState) {
		t.Fatal("cancelled proposal returned")
	}
	if _, err := e.ConfirmTask(p1.Employee, proposal.ID); !errors.Is(err, p1.ErrState) {
		t.Fatal("cancelled proposal confirmed")
	}
	if _, err := e.StartRequest(p1.Employee, proposal.ID); !errors.Is(err, p1.ErrState) {
		t.Fatal("cancelled proposal resumed")
	}
	drain, err := e.Drain(p1.Observer)
	must(t, err)
	if !drain.Idle {
		t.Fatal("proposal queue not drained")
	}
}

func TestCloseSealsAllMutatingAndReadingBoundaries(t *testing.T) {
	e, _, run := newRun(t)
	draft := produce(t, e, run)
	before := evidence(t, e)
	_, err := e.Close(p1.Observer)
	must(t, err)
	if _, err := e.OpenCitation(p1.Employee, run.ID, draft.Entries[0].Citations[0]); !errors.Is(err, p1.ErrState) {
		t.Fatal("closed citation read")
	}
	if err := e.GrantRead(p1.Authorizer, run.ID, []p1.Ref{a}); !errors.Is(err, p1.ErrState) {
		t.Fatal("closed grant")
	}
	if err := e.CancelRun(p1.Employee, run.ID); !errors.Is(err, p1.ErrState) {
		t.Fatal("closed cancellation")
	}
	if err := e.RecordDecision(p1.Employee, run.ID, "采用", "closed", false); !errors.Is(err, p1.ErrState) {
		t.Fatal("closed decision")
	}
	after := evidence(t, e)
	if len(before.Reads) != len(after.Reads) || len(before.Outbound) != len(after.Outbound) || len(before.Received) != len(after.Received) {
		t.Fatal("side effect after close")
	}
}
