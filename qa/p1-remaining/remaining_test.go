package remaining

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	p1 "onemore.local/p1"
)

var (
	a = p1.Ref{ID: "S-A", Version: "v1"}
	b = p1.Ref{ID: "S-B", Version: "v1"}
	c = p1.Ref{ID: "S-C", Version: "v1"}
)

const (
	employee   = p1.Employee
	authorizer = p1.Authorizer
	observer   = p1.Observer
)

type boundarySnapshot struct {
	Journal  p1.Evidence
	Outbound []p1.WireCapture
	Arrivals []p1.WireCapture
}

type rig struct {
	t   *testing.T
	e   *p1.Engine
	out *p1.OutboundProbe
	in  *p1.ControlledReceiver
}

func use[T any](value T, err error) func(*testing.T) T {
	return func(t *testing.T) T {
		t.Helper()
		noError(t, err)
		return value
	}
}

func noError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func same[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v; want %+v", got, want)
	}
}

func expect(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("got %v; want %v", err, want)
	}
}

func record(t *testing.T, label string, value any) {
	t.Helper()
	raw := use(json.Marshal(value))(t)
	t.Logf("%s %s", label, raw)
}

func newRig(t *testing.T) *rig {
	t.Helper()
	out, in := p1.NewOutboundProbe(), p1.NewControlledReceiver()
	r := &rig{t, use(p1.NewWithLocalBoundaries(out, in))(t), out, in}
	t.Cleanup(func() {
		end := use(r.e.Close(observer))(t)
		same(t, end, p1.DrainSignal{Closed: true, Idle: true})
		record(t, "closed", end)
	})
	return r
}

func (r *rig) snapshot() boundarySnapshot {
	r.t.Helper()
	return boundarySnapshot{use(r.e.Evidence(observer))(r.t), r.out.Captures(), r.in.Captures()}
}

func makeSpec(purpose, condition string, refs ...p1.Ref) p1.Spec {
	return p1.Spec{Purpose: purpose, Conditions: []string{condition}, Materials: refs}
}

func (r *rig) run(s p1.Spec) p1.Record {
	r.t.Helper()
	p := use(r.e.CreatePreparation(employee, s))(r.t)
	return use(r.e.ConfirmTask(employee, p.ID))(r.t)
}

func (r *rig) readGrant(run p1.Record) {
	r.t.Helper()
	noError(r.t, r.e.GrantRead(authorizer, run.ID, run.Spec.Materials))
}

func (r *rig) sendGrant(run p1.Record) {
	r.t.Helper()
	scope := use(p1.SyntheticSendScope(run.Spec.Materials))(r.t)
	noError(r.t, r.e.GrantSend(authorizer, run.ID, scope))
}

func (r *rig) view(id string) p1.Record {
	r.t.Helper()
	return use(r.e.View(employee, id))(r.t)
}

func (r *rig) draft(id string) p1.Draft {
	r.t.Helper()
	return use(r.e.ViewDraft(employee, id))(r.t)
}

func (r *rig) finish(run p1.Record) p1.Draft {
	r.t.Helper()
	req := use(r.e.StartRequest(employee, run.ID))(r.t)
	noError(r.t, r.e.CompleteRequest(employee, req.ID))
	return r.draft(run.ID)
}

func cite(ref p1.Ref, paragraph string) p1.Citation {
	return p1.Citation{Ref: ref, Paragraph: paragraph}
}

func findEntry(t *testing.T, d p1.Draft, id, category, scope string, citations ...p1.Citation) p1.Entry {
	t.Helper()
	for _, x := range d.Entries {
		if x.ID != id {
			continue
		}
		same(t, x.Category, category)
		same(t, x.Scope, scope)
		same(t, len(x.Citations), len(citations))
		used := make(map[p1.Citation]bool)
		for _, c := range x.Citations {
			used[c] = true
		}
		for _, c := range citations {
			same(t, used[c], true)
		}
		return x
	}
	t.Fatalf("missing entry %s", id)
	return p1.Entry{}
}

func contains(t *testing.T, text string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(text, part) {
			t.Fatalf("missing %q in %q", part, text)
		}
	}
}

func TestBuiltinEvidenceAndFormalReplacement(t *testing.T) {
	r := newRig(t)
	old := r.run(makeSpec("固定合成依据核查", "标明时间冲突及依据", a, b))
	r.readGrant(old)
	r.sendGrant(old)
	draft := r.finish(old)
	same(t, len(draft.Entries), 4)
	const alpha = "甲产品试点"
	const beta = "乙产品维护期"
	auth, oldTerms, disagree := cite(a, "P01"), cite(a, "P02"), cite(b, "P01")
	price, attachment, formal := cite(b, "P02"), cite(b, "P03"), cite(c, "P01")
	x := findEntry(t, draft, "alpha-retention", "冲突", alpha, auth, oldTerms, disagree)
	contains(t, x.Description, "90 天", "至少保存 180 天")
	x = findEntry(t, draft, "beta-retention", "明确", beta, auth, oldTerms)
	contains(t, x.Description, "30 天")
	x = findEntry(t, draft, "quote", "待补问", "本试点", auth, oldTerms, price)
	contains(t, x.Description, "一万元", "未提供", "报价")
	findEntry(t, draft, "acceptance", "待补问", "本试点", attachment)
	record(t, "baseline", draft)

	p := use(r.e.ChangeScope(employee, old.ID, makeSpec("经批准加入正式替代", "核查正式替代证据", a, b, c)))(t)
	next := use(r.e.ConfirmTask(employee, p.ID))(t)
	same(t, next.RelatedRunID, old.ID)
	before := r.snapshot()
	_, err := r.e.StartRequest(employee, next.ID)
	expect(t, err, p1.ErrDenied)
	_, err = r.e.ReadMaterial(employee, next.ID, c)
	expect(t, err, p1.ErrDenied)
	same(t, r.snapshot(), before)

	r.readGrant(next)
	r.sendGrant(next)
	updated := r.finish(next)
	same(t, len(updated.Entries), 4)
	x = findEntry(t, updated, "alpha-retention", "明确", alpha, auth, oldTerms, disagree, formal)
	contains(t, x.Description, "180 天", "明示替代")
	x = findEntry(t, updated, "beta-retention", "明确", beta, auth, oldTerms, formal)
	contains(t, x.Description, "30 天")
	findEntry(t, updated, "quote", "待补问", "本试点", auth, oldTerms, price)
	findEntry(t, updated, "acceptance", "待补问", "本试点", attachment)
	raw := use(r.e.OpenCitation(employee, next.ID, formal))(t)
	same(t, raw.ID, "P01")
	same(t, raw.Date, "2026-11-06")
	same(t, raw.Speaker, "合成业务代表")
	contains(t, raw.Text, "改为 180 天", "90 天")
	same(t, r.draft(old.ID), draft)
	completedOnly := r.run(makeSpec("缺原值与授权依据对照", "信息不足时待补问", c))
	r.readGrant(completedOnly)
	r.sendGrant(completedOnly)
	limited := r.finish(completedOnly)
	same(t, len(limited.Entries), 2)
	findEntry(t, limited, "alpha-retention", "待补问", alpha, formal)
	findEntry(t, limited, "beta-retention", "待补问", beta, formal)
	record(t, "approved-replacement", updated)
	record(t, "replacement-without-prerequisite-evidence", limited)
}

func TestPartiallyFulfilledGoalChange(t *testing.T) {
	r := newRig(t)
	old := r.run(makeSpec("核实验收条件", "必须确定具体验收标准", a, b))
	r.readGrant(old)
	r.sendGrant(old)
	ext := use(r.e.ChangeScope(employee, old.ID, makeSpec("申请纳入额外确认", "保留原验收标准", a, b, c)))(t)
	noError(t, r.e.CancelScopeChange(employee, ext.ID))
	partial := r.finish(old)
	findEntry(t, partial, "acceptance", "待补问", "本试点", cite(b, "P03"))
	earlier := r.view(old.ID)
	same(t, earlier.Partial, true)
	same(t, earlier.Decision, (*p1.Decision)(nil))

	nextSpec := makeSpec("整理待问清单", "列出需补问的验收问题", a, b)
	proposal := use(r.e.ChangeScope(employee, old.ID, nextSpec))(t)
	next := use(r.e.ConfirmTask(employee, proposal.ID))(t)
	same(t, next.RelatedRunID, old.ID)
	same(t, next.Spec, nextSpec)
	if next.ID == old.ID || next.SpecVersion == old.SpecVersion {
		t.Fatal("changed goal retained old run identity")
	}
	before := r.snapshot()
	_, err := r.e.StartRequest(employee, next.ID)
	expect(t, err, p1.ErrDenied)
	same(t, r.snapshot(), before)
	r.readGrant(next)
	r.sendGrant(next)
	newDraft := r.finish(next)
	same(t, newDraft.RunID, next.ID)
	saved := r.view(old.ID)
	same(t, saved.Spec, earlier.Spec)
	same(t, saved.SpecVersion, earlier.SpecVersion)
	same(t, saved.Partial, true)
	same(t, saved.Decision, earlier.Decision)
	same(t, r.draft(old.ID), partial)
	record(t, "former-goal", saved)
	record(t, "new-confirmed-goal", r.view(next.ID))
}

func TestLocalRolesRevocationAndExplicitDecision(t *testing.T) {
	r := newRig(t)
	run := r.run(makeSpec("合成授权与人工判断复核", "记录原目标是否完成", a, b))
	r.readGrant(run)
	r.sendGrant(run)
	noError(t, r.e.RevokeSend(authorizer, run.ID))
	before := r.snapshot()
	_, err := r.e.StartRequest(employee, run.ID)
	expect(t, err, p1.ErrDenied)
	same(t, r.snapshot(), before)
	same(t, use(r.e.ReadMaterial(employee, run.ID, b))(t).Ref, b)
	readOnly := r.snapshot()
	same(t, len(readOnly.Journal.Reads), len(before.Journal.Reads)+1)
	same(t, readOnly.Outbound, before.Outbound)
	same(t, readOnly.Arrivals, before.Arrivals)

	r.sendGrant(run)
	draft := r.finish(run)
	same(t, draft.CustomerConfirmed, false)
	same(t, r.view(run.ID).Decision, (*p1.Decision)(nil))
	key := cite(b, "P03")
	local := use(r.e.OpenCitation(employee, run.ID, key))(t)
	contains(t, local.Text, "见附件 B")
	safe := r.snapshot()
	for _, actor := range []string{authorizer, observer, "synthetic-unassigned"} {
		metadata, err := r.e.View(actor, run.ID)
		expect(t, err, p1.ErrDenied)
		same(t, metadata, p1.Record{})
		result, err := r.e.ViewDraft(actor, run.ID)
		expect(t, err, p1.ErrDenied)
		same(t, result, p1.Draft{})
		paragraph, err := r.e.OpenCitation(actor, run.ID, key)
		expect(t, err, p1.ErrDenied)
		same(t, paragraph, p1.Paragraph{})
		expect(t, r.e.RecordDecision(actor, run.ID, "采用", "合成未授权决定", false), p1.ErrDenied)
	}
	same(t, r.snapshot(), safe)
	_, err = r.e.Evidence(employee)
	expect(t, err, p1.ErrDenied)
	const reason = "QA_LOCAL_DECISION_REASON"
	noError(t, r.e.RecordDecision(employee, run.ID, "退回", reason, false))
	same(t, r.view(run.ID).Decision, &p1.Decision{Outcome: "退回", Reason: reason, Version: draft.Version, Actor: employee, OriginalTaskComplete: false})
	record(t, "explicit-decision", r.view(run.ID).Decision)
	noError(t, r.e.RevokeRead(authorizer, run.ID, b))
	data, err := r.e.OpenCitation(employee, run.ID, key)
	expect(t, err, p1.ErrDenied)
	same(t, data, p1.Paragraph{})
	after, err := r.e.ViewDraft(employee, run.ID)
	expect(t, err, p1.ErrDenied)
	same(t, after, p1.Draft{})
	redacted := r.view(run.ID)
	same(t, redacted.Decision.Reason, "")
	same(t, redacted.Decision.Outcome, "退回")
	same(t, r.snapshot(), safe)
	logs := use(json.Marshal(struct {
		Evidence      p1.Evidence
		Requests      []p1.Request
		Authorization []p1.AuthorizationEvent
	}{safe.Journal, use(r.e.Requests(observer))(t), use(r.e.AuthorizationEvents(observer))(t)}))(t)
	for _, secret := range []string{reason, local.Text} {
		same(t, bytes.Contains(logs, []byte(secret)), false)
	}
	record(t, "decision-after-read-revoked", redacted.Decision)
	record(t, "boundary-counts", []int{len(safe.Journal.Reads), len(safe.Outbound), len(safe.Arrivals)})
}
