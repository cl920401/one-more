package auditrepair

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	p1 "onemore.local/p1"
)

const (
	employee   = p1.Employee
	authorizer = p1.Authorizer
	observer   = p1.Observer
)

var (
	a = p1.Ref{ID: "S-A", Version: "v1"}
	b = p1.Ref{ID: "S-B", Version: "v1"}
	c = p1.Ref{ID: "S-C", Version: "v1"}
)

type rig struct {
	t   *testing.T
	e   *p1.Engine
	out *p1.OutboundProbe
	in  *p1.ControlledReceiver
}

type boundaries struct {
	Read, Outbound, Received int
	Sent, Arrived            int
}

func take[T any](v T, err error) func(*testing.T) T {
	return func(t *testing.T) T {
		t.Helper()
		must(t, err)
		return v
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func wantErr(t *testing.T, err, expected error) {
	t.Helper()
	if !errors.Is(err, expected) {
		t.Fatalf("error %v, expected %v", err, expected)
	}
}

func want[T any](t *testing.T, got, expected T) {
	t.Helper()
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("got %+v; expected %+v", got, expected)
	}
}

func setup(t *testing.T) *rig {
	t.Helper()
	out, in := p1.NewOutboundProbe(), p1.NewControlledReceiver()
	r := &rig{t, take(p1.NewWithLocalBoundaries(out, in))(t), out, in}
	t.Cleanup(func() {
		end := take(r.e.Close(observer))(t)
		want(t, end, p1.DrainSignal{Closed: true, Idle: true})
		t.Logf("end=%+v boundaries=%+v", end, r.boundaries())
	})
	return r
}

func spec(purpose string, refs ...p1.Ref) p1.Spec {
	return p1.Spec{Purpose: purpose, Materials: refs, Conditions: []string{"QA_AUDIT_CONDITION"}}
}

func summary(ref p1.Ref, paragraph string) p1.SendScope {
	return p1.SendScope{Target: p1.SimulatedModel, Inputs: []p1.SendInput{{Ref: ref, Paragraph: paragraph, Fields: []p1.SendField{p1.FieldScopeSummary}}}}
}

func (r *rig) run(s p1.Spec) p1.Record {
	r.t.Helper()
	p := take(r.e.CreatePreparation(employee, s))(r.t)
	return take(r.e.ConfirmTask(employee, p.ID))(r.t)
}

func (r *rig) grant(id string, scope p1.SendScope, refs ...p1.Ref) {
	r.t.Helper()
	must(r.t, r.e.GrantRead(authorizer, id, refs))
	must(r.t, r.e.GrantSend(authorizer, id, scope))
}

func (r *rig) boundaries() boundaries {
	r.t.Helper()
	ev := take(r.e.Evidence(observer))(r.t)
	return boundaries{len(ev.Reads), len(ev.Outbound), len(ev.Received), len(r.out.Captures()), len(r.in.Captures())}
}

func (r *rig) events() []p1.AuthorizationEvent {
	r.t.Helper()
	return take(r.e.AuthorizationEvents(observer))(r.t)
}

func (r *rig) view(id string) p1.Record {
	r.t.Helper()
	return take(r.e.View(employee, id))(r.t)
}

type cancelExpectation struct {
	Action, Subject, Kind, Object, Run string
	Epoch                              uint64
}

func (r *rig) logNewCancels(before []p1.AuthorizationEvent, expected ...cancelExpectation) {
	r.t.Helper()
	after := r.events()
	want(r.t, len(after), len(before)+len(expected))
	want(r.t, after[:len(before)], before)
	var got []cancelExpectation
	for _, item := range after[len(before):] {
		want(r.t, item.Actor, employee)
		if item.ID == "" || item.At.IsZero() || len(item.Materials) != 0 || item.SendScope != nil {
			r.t.Fatalf("cancellation event lacks identity/time or includes a grant: %+v", item)
		}
		got = append(got, cancelExpectation{
			item.Action, item.SubjectID, item.CancellationObjectType, item.CancellationObjectID, item.RelatedRunID, item.Epoch,
		})
		raw := take(json.Marshal(item))(r.t)
		r.t.Logf("audit=%s", raw)
	}
	for _, match := range expected {
		found := false
		for _, item := range got {
			found = found || item == match
		}
		if !found {
			r.t.Fatalf("cancellation %+v missing in %+v", match, got)
		}
	}
}

func (r *rig) requestState(id, expected string) {
	r.t.Helper()
	for _, request := range take(r.e.Requests(observer))(r.t) {
		if request.ID == id {
			want(r.t, request.State, expected)
			want(r.t, request.SimulatedUnits, 1)
			return
		}
	}
	r.t.Fatalf("original request %s missing", id)
}

func TestTwoDistinctApplicationsUnderSameRun(t *testing.T) {
	r := setup(t)
	old := r.run(spec("原范围", a, b))
	scope := summary(a, "P02")
	r.grant(old.ID, scope, a, b)
	var applicationIDs []string
	for turn, purpose := range []string{"申请核对新增确认", "取消后另提不同用途"} {
		pending := take(r.e.StartScopedRequest(employee, old.ID, scope))(t)
		p := take(r.e.ChangeScope(employee, old.ID, spec(purpose, a, b, c)))(t)
		applicationIDs = append(applicationIDs, p.ID)
		base := r.boundaries()
		m := take(r.e.ReadMaterial(employee, old.ID, a))(t)
		want(t, m.Ref, a)
		base.Read++
		want(t, r.boundaries(), base)
		_, err := r.e.ReadMaterial(employee, old.ID, c)
		wantErr(t, err, p1.ErrDenied)
		_, err = r.e.ReadMaterial(employee, p.ID, c)
		wantErr(t, err, p1.ErrDenied)
		_, err = r.e.StartScopedRequest(employee, p.ID, summary(c, "P01"))
		wantErr(t, err, p1.ErrDenied)
		_, err = r.e.StartRequest(employee, old.ID)
		wantErr(t, err, p1.ErrState)
		if turn == 1 {
			must(t, r.e.RevokeRead(authorizer, old.ID, b))
			_, err = r.e.ReadMaterial(employee, old.ID, b)
			wantErr(t, err, p1.ErrDenied)
		}
		want(t, r.boundaries(), base)
		previousEpoch := r.view(old.ID).AuthorizationEpoch
		history := r.events()
		must(t, r.e.CancelScopeChange(employee, p.ID))
		r.logNewCancels(history, cancelExpectation{"scope-change-cancelled", old.ID, "scope-change-application", p.ID, old.ID, previousEpoch + 1})
		want(t, r.view(p.ID).State, "cancelled")
		reopened := r.view(old.ID)
		want(t, reopened.State, "confirmed")
		want(t, reopened.Partial, true)
		want(t, reopened.Spec, old.Spec)
		want(t, reopened.AuthorizationEpoch, previousEpoch+1)
		wantErr(t, r.e.CompleteRequest(employee, pending.ID), p1.ErrState)
		draft, err := r.e.ViewDraft(employee, old.ID)
		if turn == 1 {
			wantErr(t, err, p1.ErrDenied)
		} else {
			wantErr(t, err, p1.ErrState)
		}
		want(t, draft, p1.Draft{})
		r.requestState(pending.ID, "withheld")
		_, err = r.e.ReadMaterial(employee, old.ID, c)
		wantErr(t, err, p1.ErrDenied)
		_, err = r.e.StartScopedRequest(employee, old.ID, summary(c, "P01"))
		wantErr(t, err, p1.ErrDenied)
		history = r.events()
		wantErr(t, r.e.CancelScopeChange(employee, p.ID), p1.ErrState)
		want(t, r.events(), history)
		want(t, r.boundaries(), base)
	}
	if applicationIDs[0] == applicationIDs[1] {
		t.Fatal("applications reused an ID")
	}
	continued := take(r.e.StartRequest(employee, old.ID))(t)
	must(t, r.e.CompleteRequest(employee, continued.ID))
	r.requestState(continued.ID, "returned")
	want(t, r.view(old.ID).Partial, true)
}

func TestWholeRunAndRelatedRunCascade(t *testing.T) {
	r := setup(t)
	root := r.run(spec("原运行", a, b))
	scope := summary(a, "P02")
	r.grant(root.ID, scope, a, b)
	rootReq := take(r.e.StartRequest(employee, root.ID))(t)
	childPrep := take(r.e.ChangeScope(employee, root.ID, spec("确认新运行", a, b, c)))(t)
	child := take(r.e.ConfirmTask(employee, childPrep.ID))(t)
	want(t, child.RelatedRunID, root.ID)
	_, err := r.e.StartRequest(employee, child.ID)
	wantErr(t, err, p1.ErrDenied)
	r.grant(child.ID, scope, a)
	childReq := take(r.e.StartRequest(employee, child.ID))(t)
	leafPrep := take(r.e.ChangeScope(employee, child.ID, spec("子运行的待确认申请", a, c)))(t)
	leafScope := summary(c, "P01")
	r.grant(leafPrep.ID, leafScope, c)
	leafReq := take(r.e.StartScopedRequest(employee, leafPrep.ID, leafScope))(t)
	base := r.boundaries()

	history := r.events()
	rootEpoch := r.view(root.ID).AuthorizationEpoch
	must(t, r.e.CancelRun(employee, root.ID))
	r.logNewCancels(history, cancelExpectation{"run-cancelled", root.ID, "run", root.ID, root.ID, rootEpoch + 1})
	want(t, r.view(child.ID).State, "paused")
	want(t, r.view(leafPrep.ID).State, "pending")
	r.requestState(rootReq.ID, "cancelled")
	r.requestState(childReq.ID, "pending")
	wantErr(t, r.e.CompleteRequest(employee, rootReq.ID), p1.ErrState)
	want(t, r.boundaries(), base)

	history = r.events()
	childEpoch := r.view(child.ID).AuthorizationEpoch
	leafEpoch := r.view(leafPrep.ID).AuthorizationEpoch
	must(t, r.e.CancelRun(employee, child.ID))
	r.logNewCancels(history,
		cancelExpectation{"run-cancelled", child.ID, "run", child.ID, child.ID, childEpoch + 1},
		cancelExpectation{"run-cancelled", leafPrep.ID, "run", child.ID, child.ID, leafEpoch + 1})
	for _, id := range []string{child.ID, leafPrep.ID} {
		want(t, r.view(id).State, "cancelled")
		_, err = r.e.StartRequest(employee, id)
		wantErr(t, err, p1.ErrState)
	}
	for _, id := range []string{childReq.ID, leafReq.ID} {
		r.requestState(id, "cancelled")
		wantErr(t, r.e.CompleteRequest(employee, id), p1.ErrState)
	}
	draft, err := r.e.ViewDraft(employee, child.ID)
	wantErr(t, err, p1.ErrDenied)
	want(t, draft, p1.Draft{})
	wantErr(t, r.e.GrantSend(authorizer, child.ID, scope), p1.ErrState)
	want(t, r.boundaries(), base)
	want(t, take(r.e.Drain(observer))(t), p1.DrainSignal{Idle: true})

	newPrep := take(r.e.NewRelatedPreparation(employee, child.ID, spec("取消后重新发起", a)))(t)
	next := take(r.e.ConfirmTask(employee, newPrep.ID))(t)
	want(t, next.RelatedRunID, child.ID)
	_, err = r.e.StartScopedRequest(employee, next.ID, scope)
	wantErr(t, err, p1.ErrDenied)
	want(t, r.boundaries(), base)
	r.grant(next.ID, scope, a)
	nextReq := take(r.e.StartScopedRequest(employee, next.ID, scope))(t)
	must(t, r.e.CompleteRequest(employee, nextReq.ID))
	r.requestState(nextReq.ID, "returned")
	r.requestState(childReq.ID, "cancelled")
}

func TestWhitelistWireSpotCheck(t *testing.T) {
	r := setup(t)
	subject := r.run(spec("QA_AUDIT_PRIVATE_PURPOSE", a, b))
	s := summary(b, "P01")
	must(t, r.e.GrantRead(authorizer, subject.ID, []p1.Ref{a, b}))
	_ = take(r.e.ReadMaterial(employee, subject.ID, b))(t)
	start := r.boundaries()
	want(t, start, boundaries{Read: 1})
	wantErr(t, r.e.GrantSend(authorizer, subject.ID), p1.ErrInput)
	_, err := r.e.StartRequest(employee, subject.ID)
	wantErr(t, err, p1.ErrDenied)
	must(t, r.e.GrantSend(authorizer, subject.ID, s))
	_, err = r.e.StartScopedRequest(employee, subject.ID, summary(b, "P02"))
	wantErr(t, err, p1.ErrDenied)
	widened := summary(b, "P01")
	widened.Inputs[0].Fields = append(widened.Inputs[0].Fields, p1.FieldText)
	_, err = r.e.StartScopedRequest(employee, subject.ID, widened)
	wantErr(t, err, p1.ErrDenied)
	want(t, r.boundaries(), start)
	req := take(r.e.StartRequest(employee, subject.ID))(t)
	want(t, r.boundaries(), boundaries{2, 1, 1, 1, 1})
	out, in := r.out.Captures()[0], r.in.Captures()[0]
	want(t, in.Accepted, true)
	want(t, bytes.Equal(out.Body, in.Body), true)
	sum := sha256.Sum256(out.Body)
	digest := hex.EncodeToString(sum[:])
	for _, value := range []string{out.SHA256, in.SHA256, req.PayloadSHA256} {
		want(t, value, digest)
	}
	var decoded map[string]any
	must(t, json.Unmarshal(in.Body, &decoded))
	want(t, decoded, map[string]any{
		"request_id": req.ID, "subject_id": subject.ID, "phase": "execution", "spec_version": subject.SpecVersion,
		"target": p1.SimulatedModel, "inputs": []any{map[string]any{
			"ref": map[string]any{"id": "S-B", "version": "v1"}, "paragraph": "P01", "fields": map[string]any{"scope_summary": "甲产品试点"},
		}},
	})
	ev := take(r.e.Evidence(observer))(t)
	for _, row := range []p1.Event{ev.Reads[1], ev.Outbound[0], ev.Received[0]} {
		want(t, row.RequestID, req.ID)
		want(t, row.SubjectID, subject.ID)
		want(t, row.AuthorizationEpoch, req.AuthorizationEpoch)
		want(t, row.Materials, []p1.Ref{b})
	}
	t.Logf("wire SHA256=%s JSON=%s", digest, out.Body)
	must(t, r.e.CompleteRequest(employee, req.ID))
	want(t, len(take(r.e.ViewDraft(employee, subject.ID))(t).Entries), 0)
}
