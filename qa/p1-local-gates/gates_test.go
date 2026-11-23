package localgates

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	p1 "onemore.local/p1"
)

const (
	employee   = p1.Employee
	authorizer = p1.Authorizer
	observer   = p1.Observer
)

var a = p1.Ref{ID: "S-A", Version: "v1"}
var b = p1.Ref{ID: "S-B", Version: "v1"}
var c = p1.Ref{ID: "S-C", Version: "v1"}

type rig struct {
	t   *testing.T
	e   *p1.Engine
	out *p1.OutboundProbe
	in  *p1.ControlledReceiver
}

type observation struct {
	Journal  p1.Evidence
	Outbound []p1.WireCapture
	Receiver []p1.WireCapture
}

func take[T any](v T, err error) func(*testing.T) T {
	return func(t *testing.T) T { t.Helper(); ok(t, err); return v }
}
func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func eq[T any](t *testing.T, got, wanted T) {
	t.Helper()
	if !reflect.DeepEqual(got, wanted) {
		t.Fatalf("got %+v, want %+v", got, wanted)
	}
}
func denied(t *testing.T, err, wanted error) {
	t.Helper()
	if !errors.Is(err, wanted) {
		t.Fatalf("got %v, want %v", err, wanted)
	}
}
func evidence(t *testing.T, label string, v any) {
	t.Helper()
	raw := take(json.Marshal(v))(t)
	t.Logf("%s %s", label, raw)
}
func setup(t *testing.T) *rig {
	t.Helper()
	out, in := p1.NewOutboundProbe(), p1.NewControlledReceiver()
	r := &rig{t, take(p1.NewWithLocalBoundaries(out, in))(t), out, in}
	t.Cleanup(func() {
		end := take(r.e.Close(observer))(t)
		eq(t, end, p1.DrainSignal{Closed: true, Idle: true})
		evidence(t, "closed", end)
	})
	return r
}
func spec(purpose, condition string, refs ...p1.Ref) p1.Spec {
	return p1.Spec{Purpose: purpose, Conditions: []string{condition}, Materials: refs}
}
func scope(ref p1.Ref) p1.SendScope {
	return p1.SendScope{Target: p1.SimulatedModel, Inputs: []p1.SendInput{{Ref: ref, Paragraph: "P01", Fields: []p1.SendField{p1.FieldScopeSummary}}}}
}
func (r *rig) run(s p1.Spec) p1.Record {
	r.t.Helper()
	p := take(r.e.CreatePreparation(employee, s))(r.t)
	return take(r.e.ConfirmTask(employee, p.ID))(r.t)
}
func (r *rig) grant(id string, s p1.SendScope, refs ...p1.Ref) {
	r.t.Helper()
	ok(r.t, r.e.GrantRead(authorizer, id, refs))
	ok(r.t, r.e.GrantSend(authorizer, id, s))
}
func (r *rig) observe() observation {
	r.t.Helper()
	return observation{take(r.e.Evidence(observer))(r.t), r.out.Captures(), r.in.Captures()}
}
func (r *rig) counts() [5]int {
	r.t.Helper()
	o := r.observe()
	return [5]int{len(o.Journal.Reads), len(o.Journal.Outbound), len(o.Journal.Received), len(o.Outbound), len(o.Receiver)}
}
func (r *rig) auth() []p1.AuthorizationEvent { return take(r.e.AuthorizationEvents(observer))(r.t) }
func (r *rig) view(id string) p1.Record      { return take(r.e.View(employee, id))(r.t) }
func (r *rig) queue() []p1.Request           { return take(r.e.QueueSnapshot(observer))(r.t) }
func (r *rig) requests() []p1.Request        { return take(r.e.Requests(observer))(r.t) }

func TestIndependentExplicitRefusal(t *testing.T) {
	r := setup(t)
	e := r.e
	old := r.run(spec("核验原范围验收", "识别缺失验收标准", a, b))
	valid := take(p1.SyntheticSendScope([]p1.Ref{a, b}))(t)
	r.grant(old.ID, valid, a, b)
	p := take(e.ChangeScope(employee, old.ID, spec("拟加入第三份确认", "拟核对新增范围", a, b, c)))(t)
	before := r.observe()
	history := r.auth()
	for _, actor := range []string{employee, observer, "synthetic-unassigned"} {
		denied(t, e.GrantRead(actor, p.ID, []p1.Ref{c}), p1.ErrDenied)
		denied(t, e.GrantSend(actor, p.ID, scope(c)), p1.ErrDenied)
		denied(t, e.RejectScopeChange(actor, p.ID, p1.RejectScopeNotApproved), p1.ErrDenied)
	}
	denied(t, e.RejectScopeChange(authorizer, p.ID, "not-a-supported-reason"), p1.ErrInput)
	eq(t, r.view(p.ID), p)
	eq(t, r.auth(), history)
	eq(t, r.observe(), before)
	pause := r.view(old.ID)
	ok(t, e.RejectScopeChange(authorizer, p.ID, p1.RejectScopeNotApproved))
	rejected := r.view(p.ID)
	want := &p1.ScopeRejection{ApplicationID: p.ID, OriginalRunID: old.ID, DecidedBy: authorizer, ReasonCode: p1.RejectScopeNotApproved, RequestedMaterials: []p1.Ref{a, b, c}, PurposeChanged: true, ConditionsChanged: true}
	eq(t, rejected.Rejection, want)
	eq(t, rejected.State, "rejected")
	events := r.auth()
	eq(t, len(events), len(history)+1)
	eq(t, events[:len(history)], history)
	decision := events[len(history)]
	eq(t, decision.ScopeRejection, want)
	eq(t, decision.Action, "scope-change-rejected")
	eq(t, decision.SubjectID, old.ID)
	eq(t, decision.RelatedRunID, old.ID)
	eq(t, decision.Actor, authorizer)
	eq(t, decision.Epoch, pause.AuthorizationEpoch+1)
	eq(t, decision.CancellationObjectID, "")
	eq(t, decision.CancellationObjectType, "")
	eq(t, decision.SendScope, (*p1.SendScope)(nil))
	eq(t, len(decision.Materials), 0)
	eq(t, decision.At.IsZero(), false)
	evidence(t, "refusal-audit", decision)
	_, err := e.ConfirmTask(employee, p.ID)
	denied(t, err, p1.ErrState)
	denied(t, e.GrantRead(authorizer, p.ID, []p1.Ref{c}), p1.ErrState)
	denied(t, e.GrantSend(authorizer, p.ID, scope(c)), p1.ErrState)
	denied(t, e.CancelScopeChange(employee, p.ID), p1.ErrState)
	_, err = e.ReadMaterial(employee, old.ID, c)
	denied(t, err, p1.ErrDenied)
	_, err = e.StartScopedRequest(employee, old.ID, scope(c))
	denied(t, err, p1.ErrDenied)
	eq(t, r.auth(), events)
	eq(t, r.observe(), before)
	req := take(e.StartRequest(employee, old.ID))(t)
	ok(t, e.CompleteRequest(employee, req.ID))
	captured := r.observe()
	for _, stream := range [][]p1.Event{captured.Journal.Reads, captured.Journal.Outbound, captured.Journal.Received} {
		for _, item := range stream {
			for _, ref := range item.Materials {
				if ref == c {
					t.Fatal("rejected material crossed a boundary")
				}
			}
		}
	}
	eq(t, r.counts(), [5]int{2, 1, 1, 1, 1})
	draft := take(e.ViewDraft(employee, old.ID))(t)
	var acceptance p1.Entry
	for _, item := range draft.Entries {
		if item.ID == "acceptance" {
			acceptance = item
		}
	}
	eq(t, acceptance.Category, "待补问")
	eq(t, acceptance.Citations, []p1.Citation{{Ref: b, Paragraph: "P03"}})
	eq(t, r.view(old.ID).Partial, true)
	eq(t, r.view(old.ID).Spec, old.Spec)
	denied(t, e.RecordDecision(employee, old.ID, "采用", "本地合成部分稿", true), p1.ErrState)
	ok(t, e.RecordDecision(employee, old.ID, "采用", "本地合成部分稿", false))
	eq(t, r.view(old.ID).Decision.OriginalTaskComplete, false)
	eq(t, r.view(old.ID).Decision.Version, draft.Version)
	eq(t, r.view(p.ID), rejected)
	evidence(t, "original-partial-decision", r.view(old.ID).Decision)
	evidence(t, "original-only-boundaries", r.counts())
}

func (r *rig) enterQueue(run p1.Record, s p1.SendScope) p1.Request {
	t := r.t
	t.Helper()
	zero := r.observe()
	q := take(r.e.QueueScopedRequest(employee, run.ID, s))(t)
	eq(t, q.State, "queued")
	eq(t, q.QueuedAt.IsZero(), false)
	eq(t, q.DispatchedAt.IsZero(), true)
	eq(t, q.PayloadSHA256, "")
	eq(t, r.queue(), []p1.Request{q})
	eq(t, take(r.e.Drain(observer))(t), p1.DrainSignal{QueuedRequests: 1})
	_, err := r.e.QueueSnapshot(employee)
	denied(t, err, p1.ErrDenied)
	eq(t, r.observe(), zero)
	evidence(t, "queue-observed", r.queue())
	evidence(t, "counts-at-cut", r.counts())
	return q
}
func TestDispatchObeysPreSendRevocation(t *testing.T) {
	for _, scenario := range []string{"send withdrawn", "read withdrawn", "send withdrawn then regranted"} {
		t.Run(scenario, func(t *testing.T) {
			r := setup(t)
			run := r.run(spec("合成排队切点", "获准后发送一段范围摘要", b))
			s := scope(b)
			r.grant(run.ID, s, b)
			q := r.enterQueue(run, s)
			if scenario == "read withdrawn" {
				ok(t, r.e.RevokeRead(authorizer, run.ID, b))
			} else {
				ok(t, r.e.RevokeSend(authorizer, run.ID))
				if scenario == "send withdrawn then regranted" {
					ok(t, r.e.GrantSend(authorizer, run.ID, s))
				}
			}
			blocked, err := r.e.DispatchQueuedRequest(employee, q.ID)
			denied(t, err, p1.ErrDenied)
			eq(t, blocked.ID, q.ID)
			eq(t, blocked.State, "withheld-before-send")
			eq(t, blocked.QueuedAt, q.QueuedAt)
			eq(t, blocked.DispatchedAt.IsZero(), true)
			eq(t, blocked.PayloadSHA256, "")
			eq(t, blocked.SimulatedUnits, 0)
			eq(t, len(r.queue()), 0)
			eq(t, r.counts(), [5]int{})
			eq(t, r.requests(), []p1.Request{blocked})
			denied(t, r.e.CompleteRequest(employee, q.ID), p1.ErrState)
			evidence(t, "blocked-after-dispatch-attempt", blocked)
			evidence(t, "counts-after-dispatch-attempt", r.counts())
		})
	}
}
func TestQueuedPositiveControlAndLateResult(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprintf("late=%t", late), func(t *testing.T) {
			r := setup(t)
			run := r.run(spec("合成范围", "核对受控接收字节", b))
			s := scope(b)
			r.grant(run.ID, s, b)
			q := r.enterQueue(run, s)
			pending := take(r.e.DispatchQueuedRequest(employee, q.ID))(t)
			eq(t, pending.ID, q.ID)
			eq(t, pending.State, "pending")
			eq(t, pending.DispatchedAt.Before(q.QueuedAt), false)
			eq(t, pending.SimulatedUnits, 1)
			eq(t, r.counts(), [5]int{1, 1, 1, 1, 1})
			actual := r.observe()
			a, b := actual.Outbound[0], actual.Receiver[0]
			eq(t, b.Accepted, true)
			eq(t, bytes.Equal(a.Body, b.Body), true)
			digest := fmt.Sprintf("%x", sha256.Sum256(b.Body))
			for _, s := range []string{a.SHA256, b.SHA256, pending.PayloadSHA256, actual.Journal.Outbound[0].PayloadSHA256, actual.Journal.Received[0].PayloadSHA256} {
				eq(t, s, digest)
			}
			var payload map[string]any
			ok(t, json.Unmarshal(b.Body, &payload))
			eq(t, payload, map[string]any{"request_id": q.ID, "subject_id": run.ID, "phase": "execution", "spec_version": run.SpecVersion, "target": p1.SimulatedModel,
				"inputs": []any{map[string]any{"ref": map[string]any{"id": "S-B", "version": "v1"}, "paragraph": "P01", "fields": map[string]any{"scope_summary": "甲产品试点"}}}})
			t.Logf("positive-wire sha256=%s bytes=%s", digest, b.Body)
			if late {
				ok(t, r.e.RevokeSend(authorizer, run.ID))
				ok(t, r.e.GrantSend(authorizer, run.ID, s))
				denied(t, r.e.CompleteRequest(employee, q.ID), p1.ErrDenied)
				draft, err := r.e.ViewDraft(employee, run.ID)
				denied(t, err, p1.ErrState)
				eq(t, draft, p1.Draft{})
				history := r.requests()
				eq(t, len(history), 1)
				eq(t, history[0].State, "withheld")
				eq(t, history[0].SimulatedUnits, 1)
				eq(t, history[0].PayloadSHA256, digest)
				eq(t, r.observe(), actual)
				evidence(t, "late-withheld", history[0])
			} else {
				ok(t, r.e.CompleteRequest(employee, q.ID))
				eq(t, r.requests()[0].State, "returned")
				eq(t, take(r.e.ViewDraft(employee, run.ID))(t).RunID, run.ID)
			}
		})
	}
}
