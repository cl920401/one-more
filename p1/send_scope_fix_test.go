package p1_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	p1 "onemore.local/p1"
)

func summaryScope() p1.SendScope {
	return p1.SendScope{Target: p1.SimulatedModel, Inputs: []p1.SendInput{
		{Ref: a, Paragraph: "P02", Fields: []p1.SendField{p1.FieldScopeSummary}},
	}}
}

func observedRun(t *testing.T) (*p1.Engine, *p1.OutboundProbe, *p1.ControlledReceiver, p1.Record) {
	t.Helper()
	out, receiver := p1.NewOutboundProbe(), p1.NewControlledReceiver()
	e, err := p1.NewWithLocalBoundaries(out, receiver)
	must(t, err)
	prep, err := e.CreatePreparation(p1.Employee, spec())
	must(t, err)
	run, err := e.ConfirmTask(p1.Employee, prep.ID)
	must(t, err)
	must(t, e.GrantRead(p1.Authorizer, run.ID, run.Spec.Materials))
	return e, out, receiver, run
}

func TestSendRequiresExplicitParagraphFieldGrantEvenThroughOldEntry(t *testing.T) {
	e, out, receiver, run := observedRun(t)
	if err := e.GrantSend(p1.Authorizer, run.ID); !errors.Is(err, p1.ErrInput) {
		t.Fatal("implicit send policy accepted")
	}
	if _, err := e.StartRequest(p1.Employee, run.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("old entry bypassed missing grant")
	}
	if _, err := e.StartScopedRequest(p1.Employee, run.ID, summaryScope()); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("selector acted as authorization")
	}
	if len(out.Captures())+len(receiver.Captures())+len(evidence(t, e).Reads) != 0 {
		t.Fatal("denial caused side effect")
	}
	must(t, e.GrantSend(p1.Authorizer, run.ID, summaryScope()))
	// The legacy dispatch entry shares the exact same projection.
	req, err := e.StartRequest(p1.Employee, run.ID)
	must(t, err)
	if req.SendScope.Inputs[0].Fields[0] != p1.FieldScopeSummary {
		t.Fatal("old entry broadened grant")
	}
	captures := out.Captures()
	if len(captures) != 1 || bytes.Contains(captures[0].Body, []byte(`"text":`)) {
		t.Fatal("old entry transmitted full material")
	}
}

func TestSummaryOnlyBytesAreIndependentlyCapturedAndNeverBackfilled(t *testing.T) {
	e, out, receiver, run := observedRun(t)
	must(t, e.GrantSend(p1.Authorizer, run.ID, summaryScope()))
	req, err := e.StartScopedRequest(p1.Employee, run.ID, summaryScope())
	must(t, err)
	sent, arrived := out.Captures(), receiver.Captures()
	if len(sent) != 1 || len(arrived) != 1 || !arrived[0].Accepted {
		t.Fatal("missing raw positive control")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(sent[0].Body))
	if !bytes.Equal(sent[0].Body, arrived[0].Body) || sent[0].SHA256 != digest || arrived[0].SHA256 != digest || req.PayloadSHA256 != digest {
		t.Fatal("wire bytes or fingerprints mismatch")
	}
	var wire p1.SerializedRequest
	must(t, json.Unmarshal(sent[0].Body, &wire))
	if wire.RequestID != req.ID || wire.SubjectID != run.ID || wire.Phase != "execution" || wire.SpecVersion != run.SpecVersion || wire.Target != p1.SimulatedModel {
		t.Fatal("metadata not bound to actual subject")
	}
	if len(wire.Inputs) != 1 || wire.Inputs[0].Ref != a || wire.Inputs[0].Paragraph != "P02" || len(wire.Inputs[0].Fields) != 1 || wire.Inputs[0].Fields[p1.FieldScopeSummary] == "" {
		t.Fatalf("wrong projection: %+v", wire)
	}
	for _, forbidden := range []string{`"text":`, `"date":`, `"speaker":`, "90 天", "一万元", "合成业务代表"} {
		if bytes.Contains(sent[0].Body, []byte(forbidden)) {
			t.Fatalf("unapproved field/value %q in wire", forbidden)
		}
	}
	events := evidence(t, e)
	if len(events.Reads) != 1 || len(events.Outbound) != 1 || len(events.Received) != 1 || events.Outbound[0].PayloadSHA256 != digest || events.Received[0].PayloadSHA256 != digest {
		t.Fatal("boundary counters/digests disagree")
	}
	must(t, e.CompleteRequest(p1.Employee, req.ID))
	draft, err := e.ViewDraft(p1.Employee, run.ID)
	must(t, err)
	if len(draft.Entries) != 0 || draft.CustomerConfirmed {
		t.Fatal("receiver reloaded source facts absent from projection")
	}
	end, err := e.Close(p1.Observer)
	must(t, err)
	if !end.Closed || !end.Idle {
		t.Fatal("raw observers changed drain lifecycle")
	}
}

func TestUnapprovedSelectorOrFieldDeniesWholeRequestBeforeReading(t *testing.T) {
	for _, kind := range []string{"paragraph", "version", "material", "extra-text", "unknown-field", "wildcard", "empty-fields", "target", "mixed"} {
		t.Run(kind, func(t *testing.T) {
			e, out, receiver, run := observedRun(t)
			must(t, e.GrantSend(p1.Authorizer, run.ID, summaryScope()))
			wanted := summaryScope()
			switch kind {
			case "paragraph":
				wanted.Inputs[0].Paragraph = "P01"
			case "version":
				wanted.Inputs[0].Ref.Version = "v2"
			case "material":
				wanted.Inputs[0].Ref = b
			case "extra-text":
				wanted.Inputs[0].Fields = append(wanted.Inputs[0].Fields, p1.FieldText)
			case "unknown-field":
				wanted.Inputs[0].Fields = []p1.SendField{"private_notes"}
			case "wildcard":
				wanted.Inputs[0].Fields = []p1.SendField{"*"}
			case "empty-fields":
				wanted.Inputs[0].Fields = nil
			case "target":
				wanted.Target = "https://unapproved.invalid"
			case "mixed":
				wanted.Inputs = append(wanted.Inputs, p1.SendInput{Ref: b, Paragraph: "P01", Fields: []p1.SendField{p1.FieldText}})
			}
			if _, err := e.StartScopedRequest(p1.Employee, run.ID, wanted); !errors.Is(err, p1.ErrDenied) {
				t.Fatalf("unexpected result: %v", err)
			}
			ev := evidence(t, e)
			if len(ev.Reads)+len(ev.Outbound)+len(ev.Received)+len(out.Captures())+len(receiver.Captures()) != 0 {
				t.Fatal("invalid mixed request partially dispatched")
			}
			if _, err := e.StartScopedRequest(p1.Employee, run.ID, summaryScope()); err != nil {
				t.Fatal("denial damaged valid grant")
			}
		})
	}
}

func TestGrantAndRequestSnapshotsCannotBroadenAllowedWire(t *testing.T) {
	e, out, receiver, run := observedRun(t)
	approved := summaryScope()
	must(t, e.GrantSend(p1.Authorizer, run.ID, approved))
	approved.Inputs[0].Fields[0] = p1.FieldText
	if _, err := e.StartScopedRequest(p1.Employee, run.ID, approved); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("grant aliases expanded field")
	}
	req, err := e.StartScopedRequest(p1.Employee, run.ID, summaryScope())
	must(t, err)
	req.SendScope.Inputs[0].Fields[0] = p1.FieldText
	audit, err := e.AuthorizationEvents(p1.Observer)
	must(t, err)
	audit[len(audit)-1].SendScope.Inputs[0].Fields[0] = p1.FieldText
	events := evidence(t, e)
	events.Outbound[0].SendScope.Inputs[0].Fields[0] = p1.FieldText
	captures := receiver.Captures()
	captures[0].Body[0] = 'X'
	sent := out.Captures()
	sent[0].Body[0] = 'Y'
	if receiver.Captures()[0].Body[0] != '{' || out.Captures()[0].Body[0] != '{' {
		t.Fatal("raw byte snapshot aliases storage")
	}
	requests, err := e.Requests(p1.Observer)
	must(t, err)
	if requests[0].SendScope.Inputs[0].Fields[0] != p1.FieldScopeSummary {
		t.Fatal("request scope mutated")
	}
	must(t, e.CompleteRequest(p1.Employee, req.ID))
}

func TestParagraphGrantCannotBeReusedAcrossSubjectsOrAfterRevocation(t *testing.T) {
	e, out, receiver, run := observedRun(t)
	must(t, e.GrantSend(p1.Authorizer, run.ID, summaryScope()))
	must(t, e.RevokeRead(p1.Authorizer, run.ID, a))
	if _, err := e.StartRequest(p1.Employee, run.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("revoked read reached serialization")
	}
	if len(out.Captures())+len(receiver.Captures()) != 0 {
		t.Fatal("pre-send revoke produced wire attempt")
	}
	must(t, e.GrantRead(p1.Authorizer, run.ID, []p1.Ref{a}))
	req, err := e.StartRequest(p1.Employee, run.ID)
	must(t, err)
	must(t, e.RevokeSend(p1.Authorizer, run.ID))
	if err := e.CompleteRequest(p1.Employee, req.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("revoked send delivered result")
	}
	if len(receiver.Captures()) != 1 {
		t.Fatal("previous receipt history erased")
	}
	prep, err := e.CreatePreparation(p1.Employee, spec())
	must(t, err)
	other, err := e.ConfirmTask(p1.Employee, prep.ID)
	must(t, err)
	must(t, e.GrantRead(p1.Authorizer, other.ID, other.Spec.Materials))
	if _, err := e.StartScopedRequest(p1.Employee, other.ID, summaryScope()); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("send scope inherited")
	}
}

func TestReceiverControlRecordsRejectedRawAttemptAndPositiveControl(t *testing.T) {
	e, out, receiver, run := observedRun(t)
	receiver.RejectArrivals(true)
	must(t, e.GrantSend(p1.Authorizer, run.ID, summaryScope()))
	req, err := e.StartRequest(p1.Employee, run.ID)
	if !errors.Is(err, p1.ErrBoundary) || req.State != "failed" || req.SimulatedUnits != 1 {
		t.Fatalf("controlled rejection missing: %+v %v", req, err)
	}
	if len(out.Captures()) != 1 || len(receiver.Captures()) != 1 || receiver.Captures()[0].Accepted {
		t.Fatal("raw rejection not independently recorded")
	}
	ev := evidence(t, e)
	if len(ev.Outbound) != 1 || len(ev.Received) != 0 {
		t.Fatal("rejected arrival recorded as accepted")
	}
	receiver.RejectArrivals(false)
	prep, err := e.CreatePreparation(p1.Employee, spec())
	must(t, err)
	next, err := e.ConfirmTask(p1.Employee, prep.ID)
	must(t, err)
	must(t, e.GrantRead(p1.Authorizer, next.ID, next.Spec.Materials))
	must(t, e.GrantSend(p1.Authorizer, next.ID, summaryScope()))
	valid, err := e.StartRequest(p1.Employee, next.ID)
	must(t, err)
	must(t, e.CompleteRequest(p1.Employee, valid.ID))
	if len(receiver.Captures()) != 2 || !receiver.Captures()[1].Accepted {
		t.Fatal("positive receipt control missing")
	}
}

func TestLocalObserversRejectNilReuseAndPermitConcurrentSnapshots(t *testing.T) {
	if _, err := p1.NewWithLocalBoundaries(nil, p1.NewControlledReceiver()); !errors.Is(err, p1.ErrInput) {
		t.Fatal("nil probe silently replaced")
	}
	if _, err := p1.NewWithLocalBoundaries(p1.NewOutboundProbe(), nil); !errors.Is(err, p1.ErrInput) {
		t.Fatal("nil receiver silently replaced")
	}
	e, out, receiver, run := observedRun(t)
	if _, err := p1.NewWithLocalBoundaries(out, p1.NewControlledReceiver()); !errors.Is(err, p1.ErrInput) {
		t.Fatal("probe reused across engines")
	}
	if _, err := p1.NewWithLocalBoundaries(p1.NewOutboundProbe(), receiver); !errors.Is(err, p1.ErrInput) {
		t.Fatal("receiver reused across engines")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = out.Captures()
				_ = receiver.Captures()
			}
		}()
	}
	must(t, e.GrantSend(p1.Authorizer, run.ID, summaryScope()))
	_, err := e.StartRequest(p1.Employee, run.ID)
	must(t, err)
	wg.Wait()
}

func TestNewGrantsRejectTerminalSubjectsButRevocationStillWorks(t *testing.T) {
	e, prep, run := newRun(t)
	if err := e.GrantRead(p1.Authorizer, prep.ID, prep.Spec.Materials); !errors.Is(err, p1.ErrState) {
		t.Fatal("confirmed preparation received new grant")
	}
	grant(t, e, run.ID, run.Spec.Materials)
	must(t, e.CancelRun(p1.Employee, run.ID))
	if err := e.GrantRead(p1.Authorizer, run.ID, run.Spec.Materials); !errors.Is(err, p1.ErrState) {
		t.Fatal("cancelled run received new read grant")
	}
	if err := e.GrantSend(p1.Authorizer, run.ID, summaryScope()); !errors.Is(err, p1.ErrState) {
		t.Fatal("cancelled run received new send grant")
	}
	must(t, e.RevokeRead(p1.Authorizer, run.ID, a))
	must(t, e.RevokeSend(p1.Authorizer, run.ID))
}

func TestInvalidMixedGrantDoesNotReplaceValidProjection(t *testing.T) {
	e, out, receiver, run := observedRun(t)
	must(t, e.GrantSend(p1.Authorizer, run.ID, summaryScope()))
	before, err := e.View(p1.Employee, run.ID)
	must(t, err)
	invalid := summaryScope()
	invalid.Inputs = append(invalid.Inputs, p1.SendInput{Ref: c, Paragraph: "P01", Fields: []p1.SendField{p1.FieldText}})
	if err := e.GrantSend(p1.Authorizer, run.ID, invalid); !errors.Is(err, p1.ErrInput) {
		t.Fatal("out-of-task mixed grant accepted")
	}
	after, err := e.View(p1.Employee, run.ID)
	must(t, err)
	if after.AuthorizationEpoch != before.AuthorizationEpoch {
		t.Fatal("rejected grant mutated authorization")
	}
	req, err := e.StartRequest(p1.Employee, run.ID)
	must(t, err)
	if len(req.SendScope.Inputs) != 1 || len(out.Captures()) != 1 || len(receiver.Captures()) != 1 {
		t.Fatal("original grant not preserved")
	}
}
