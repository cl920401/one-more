// Package p1 is an ephemeral, synthetic-only development slice.
// Fixed actor names simulate roles; they are not production authentication.
package p1

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrDenied   = errors.New("access denied")
	ErrState    = errors.New("operation unavailable in current state")
	ErrInput    = errors.New("invalid synthetic request")
	ErrBoundary = errors.New("simulated boundary unavailable")
)

const (
	Employee   = "synthetic-employee"
	Authorizer = "synthetic-authorizer"
	Observer   = "synthetic-observer"
)

type Spec struct {
	Purpose    string
	Materials  []Ref
	Conditions []string
}

type Decision struct {
	Outcome              string
	Reason               string
	Version              string
	Actor                string
	OriginalTaskComplete bool
}

type Record struct {
	ID                 string
	Phase              string
	PreparationID      string
	RelatedRunID       string
	SpecVersion        string
	Spec               Spec
	State              string
	AuthorizationEpoch uint64
	Partial            bool
	Decision           *Decision
	Rejection          *ScopeRejection
}

type Draft struct {
	RunID             string
	Version           string
	Title             string
	CustomerConfirmed bool
	Entries           []Entry
}

type Request struct {
	ID                 string
	SubjectID          string
	Phase              string
	AuthorizationEpoch uint64
	Model              string
	State              string
	SimulatedUnits     int
	SendScope          SendScope
	PayloadSHA256      string
	QueuedAt           time.Time
	DispatchedAt       time.Time
}

type subject struct {
	record      Record
	owner       string
	read        map[Ref]bool
	send        *SendScope
	draft       *Draft
	beforePause string
}

type Engine struct {
	mu             sync.Mutex
	next           int
	reader         *materialReader
	outbound       *simulatedOutbound
	receiver       *simulatedReceiver
	subjects       map[string]*subject
	requests       map[string]*Request
	queued         map[string]struct{}
	authorizations []AuthorizationEvent
	closed         bool
}

func New() *Engine {
	e, err := NewWithLocalBoundaries(NewOutboundProbe(), NewControlledReceiver())
	if err != nil {
		panic(err)
	}
	return e
}

func (e *Engine) id(prefix string) string {
	e.next++
	return fmt.Sprintf("%s-%04d", prefix, e.next)
}

func cloneSpec(s Spec) Spec {
	s.Materials = append([]Ref(nil), s.Materials...)
	s.Conditions = append([]string(nil), s.Conditions...)
	return s
}

func cloneRecord(r Record) Record {
	r.Spec = cloneSpec(r.Spec)
	if r.Decision != nil {
		d := *r.Decision
		r.Decision = &d
	}
	r.Rejection = cloneRejection(r.Rejection)
	return r
}

func cloneEntries(entries []Entry) []Entry {
	out := append([]Entry(nil), entries...)
	for i := range out {
		out[i].Citations = append([]Citation(nil), out[i].Citations...)
	}
	return out
}

func cloneDraft(d *Draft) Draft {
	out := *d
	out.Entries = cloneEntries(d.Entries)
	return out
}

func (e *Engine) validSpec(spec Spec) bool {
	if strings.TrimSpace(spec.Purpose) == "" || len(spec.Materials) == 0 || len(spec.Conditions) == 0 {
		return false
	}
	seen := make(map[Ref]bool)
	for _, ref := range spec.Materials {
		if _, ok := e.reader.data[ref]; !ok || seen[ref] {
			return false
		}
		seen[ref] = true
	}
	for _, c := range spec.Conditions {
		if strings.TrimSpace(c) == "" {
			return false
		}
	}
	return true
}

func (e *Engine) createPreparation(spec Spec, related string) Record {
	rec := Record{ID: e.id("prep"), Phase: "preparation", RelatedRunID: related,
		Spec: cloneSpec(spec), State: "form"}
	e.subjects[rec.ID] = &subject{record: rec, owner: Employee, read: make(map[Ref]bool)}
	return cloneRecord(rec)
}

// CreatePreparation collects a fixed form and reads no document body.
func (e *Engine) CreatePreparation(actor string, spec Spec) (Record, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if actor != Employee {
		return Record{}, ErrDenied
	}
	if e.closed {
		return Record{}, ErrState
	}
	if !e.validSpec(spec) {
		return Record{}, ErrInput
	}
	return e.createPreparation(spec, ""), nil
}

func (e *Engine) owned(actor, id string) (*subject, error) {
	s, ok := e.subjects[id]
	if !ok || actor != s.owner {
		return nil, ErrDenied
	}
	return s, nil
}

func containsRef(refs []Ref, wanted Ref) bool {
	for _, r := range refs {
		if r == wanted {
			return true
		}
	}
	return false
}

func (e *Engine) grantTarget(actor, id string) (*subject, error) {
	if actor != Authorizer {
		return nil, ErrDenied
	}
	if e.closed {
		return nil, ErrState
	}
	s, ok := e.subjects[id]
	if !ok {
		return nil, ErrDenied
	}
	return s, nil
}

func (e *Engine) GrantRead(actor, id string, refs []Ref) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.grantTarget(actor, id)
	if err != nil {
		return err
	}
	if s.record.State == "cancelled" || s.record.State == "rejected" || s.record.State == "decision-recorded" || (s.record.Phase == "preparation" && s.record.State == "confirmed") {
		return ErrState
	}
	if len(refs) == 0 {
		return ErrInput
	}
	// Validate the entire grant before mutating any part.
	for _, ref := range refs {
		if !containsRef(s.record.Spec.Materials, ref) {
			return ErrDenied
		}
	}
	for _, ref := range refs {
		s.read[ref] = true
	}
	e.authorization(s, "read-granted", refs)
	return nil
}

// GrantSend requires exactly one explicit field/paragraph scope; no default.
func (e *Engine) GrantSend(actor, id string, scopes ...SendScope) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.grantTarget(actor, id)
	if err != nil {
		return err
	}
	if s.record.State == "cancelled" || s.record.State == "rejected" || s.record.State == "decision-recorded" || (s.record.Phase == "preparation" && s.record.State == "confirmed") {
		return ErrState
	}
	if len(scopes) != 1 || !e.validSendScope(s, scopes[0]) {
		return ErrInput
	}
	scope := cloneScope(scopes[0])
	s.send = &scope
	e.authorization(s, "send-granted", nil)
	event := &e.authorizations[len(e.authorizations)-1]
	snapshot := cloneScope(scope)
	event.SendScope = &snapshot
	return nil
}

func (e *Engine) RevokeRead(actor, id string, ref Ref) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.grantTarget(actor, id)
	if err != nil {
		return err
	}
	if !containsRef(s.record.Spec.Materials, ref) {
		return ErrDenied
	}
	delete(s.read, ref)
	e.authorization(s, "read-revoked", []Ref{ref})
	return nil
}

func (e *Engine) RevokeSend(actor, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.grantTarget(actor, id)
	if err != nil {
		return err
	}
	s.send = nil
	e.authorization(s, "send-revoked", s.record.Spec.Materials)
	return nil
}

func (e *Engine) allReadable(s *subject) bool {
	for _, ref := range s.record.Spec.Materials {
		if !s.read[ref] {
			return false
		}
	}
	return true
}

// ReadMaterial is a local read. It does not imply send permission.
func (e *Engine) ReadMaterial(actor, id string, ref Ref) (Material, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.owned(actor, id)
	if err != nil {
		return Material{}, err
	}
	// A scope proposal leaves this subject's original SPEC unchanged. Keep
	// current grants for its unaffected local reads; dispatch has its own state gate.
	if e.closed || s.record.State == "cancelled" || s.record.State == "rejected" || s.record.State == "closed" || (s.record.Phase == "preparation" && s.record.State == "confirmed") {
		return Material{}, ErrState
	}
	if !containsRef(s.record.Spec.Materials, ref) || !s.read[ref] {
		return Material{}, ErrDenied
	}
	ctx := boundaryContext{subject: id, request: e.id("read"), phase: s.record.Phase, epoch: s.record.AuthorizationEpoch}
	return e.reader.read(ctx, ref), nil
}

// ConfirmTask is an explicit employee action. Preparation grants never transfer.
func (e *Engine) ConfirmTask(actor, preparationID string) (Record, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p, err := e.owned(actor, preparationID)
	if err != nil {
		return Record{}, err
	}
	if e.closed {
		return Record{}, ErrState
	}
	if p.record.Phase != "preparation" || (p.record.State != "form" && p.record.State != "ready") {
		return Record{}, ErrState
	}
	rec := Record{ID: e.id("run"), Phase: "execution", PreparationID: preparationID,
		RelatedRunID: p.record.RelatedRunID, SpecVersion: e.id("spec"),
		Spec: cloneSpec(p.record.Spec), State: "confirmed"}
	e.subjects[rec.ID] = &subject{record: rec, owner: Employee, read: make(map[Ref]bool)}
	p.record.State = "confirmed"
	return cloneRecord(rec), nil
}

// ChangeScope pauses the old run and collects a new, unapproved preparation.
// No materials are read and no grants are inherited.
func (e *Engine) ChangeScope(actor, runID string, next Spec) (Record, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.owned(actor, runID)
	if err != nil {
		return Record{}, err
	}
	if s.record.Phase != "execution" || !e.validSpec(next) {
		return Record{}, ErrInput
	}
	if e.closed || (s.record.State != "confirmed" && s.record.State != "queued" && s.record.State != "pending" && s.record.State != "review" && s.record.State != "blocked") {
		return Record{}, ErrState
	}
	s.beforePause = s.record.State
	s.record.State = "paused"
	e.authorization(s, "scope-paused", nil)
	return e.createPreparation(next, runID), nil
}

// CompleteRequest deliberately separates dispatch and result delivery so tests
// and the demo can revoke while a simulated model result is pending.
func (e *Engine) CompleteRequest(actor, requestID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	req, ok := e.requests[requestID]
	if !ok {
		return ErrDenied
	}
	s, err := e.owned(actor, req.SubjectID)
	if err != nil {
		return err
	}
	if req.State != "pending" {
		return ErrState
	}
	if s.record.State != "pending" || req.AuthorizationEpoch != s.record.AuthorizationEpoch ||
		s.send == nil || !scopeAllows(*s.send, req.SendScope) || !e.sendReadable(s, req.SendScope) {
		e.receiver.discard(requestID)
		req.State = "withheld"
		if s.record.State != "paused" {
			s.record.State = "blocked"
		}
		return ErrDenied
	}
	entries := e.receiver.finish(requestID)
	req.State = "returned"
	if s.record.Phase == "preparation" {
		s.record.State = "ready"
		return nil
	}
	s.draft = &Draft{RunID: s.record.ID, Version: e.id("draft"),
		Title: "内部需求确认单草稿（合成材料）", CustomerConfirmed: false, Entries: cloneEntries(entries)}
	s.record.State = "review"
	return nil
}

func (e *Engine) View(actor, id string) (Record, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.owned(actor, id)
	if err != nil {
		return Record{}, err
	}
	result := cloneRecord(s.record)
	if result.Decision != nil && !e.allReadable(s) {
		result.Decision.Reason = ""
	}
	return result, nil
}

func (e *Engine) ViewDraft(actor, runID string) (Draft, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.owned(actor, runID)
	if err != nil {
		return Draft{}, err
	}
	if !e.allReadable(s) {
		return Draft{}, ErrDenied
	}
	if s.draft == nil {
		return Draft{}, ErrState
	}
	return cloneDraft(s.draft), nil
}

func (e *Engine) OpenCitation(actor, runID string, citation Citation) (Paragraph, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return Paragraph{}, ErrState
	}
	s, err := e.owned(actor, runID)
	if err != nil {
		return Paragraph{}, err
	}
	if !containsRef(s.record.Spec.Materials, citation.Ref) || !s.read[citation.Ref] {
		return Paragraph{}, ErrDenied
	}
	// A citation must belong to an actually produced draft, not merely a
	// guessed link to a selected material.
	found := false
	if s.draft != nil {
		for _, entry := range s.draft.Entries {
			for _, actual := range entry.Citations {
				if actual == citation {
					found = true
				}
			}
		}
	}
	if !found {
		return Paragraph{}, ErrDenied
	}
	ctx := boundaryContext{subject: runID, request: e.id("citation"), phase: "execution", epoch: s.record.AuthorizationEpoch}
	material := e.reader.read(ctx, citation.Ref)
	for _, p := range material.Paragraphs {
		if p.ID == citation.Paragraph {
			return p, nil
		}
	}
	return Paragraph{}, ErrInput
}

func (e *Engine) RecordDecision(actor, runID, outcome, reason string, complete bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.owned(actor, runID)
	if err != nil {
		return err
	}
	if e.closed || s.record.State != "review" || s.draft == nil || !e.allReadable(s) {
		return ErrState
	}
	if complete && s.record.Partial {
		return ErrState
	}
	if outcome != "采用" && outcome != "改后采用" && outcome != "退回" {
		return ErrInput
	}
	if strings.TrimSpace(reason) == "" {
		return ErrInput
	}
	s.record.Decision = &Decision{Outcome: outcome, Reason: reason, Version: s.draft.Version, Actor: actor, OriginalTaskComplete: complete}
	s.record.State = "decision-recorded"
	return nil
}

// Evidence exposes body-free snapshots of the three boundary-owned journals,
// only to the fixed synthetic observer role.
func (e *Engine) Evidence(actor string) (Evidence, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if actor != Observer {
		return Evidence{}, ErrDenied
	}
	return Evidence{Reads: e.reader.observed.snapshot(), Outbound: e.outbound.observed.snapshot(),
		Received: e.receiver.observed.snapshot()}, nil
}

func (e *Engine) Requests(actor string) ([]Request, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if actor != Observer {
		return nil, ErrDenied
	}
	// Stable order independent of Go map iteration.
	out := make([]Request, 0, len(e.requests))
	for n := 1; n <= e.next; n++ {
		id := fmt.Sprintf("request-%04d", n)
		if r, ok := e.requests[id]; ok {
			out = append(out, cloneRequest(*r))
		}
	}
	return out, nil
}

// AuthorizationEvent is body-free and correlated by SubjectID plus Epoch.
// Only fixed synthetic authorization roles can create permission events.
type AuthorizationEvent struct {
	ID          string
	SubjectID   string
	Epoch       uint64
	At          time.Time
	Actor       string
	Action      string
	Materials   []Ref
	SendScope   *SendScope
	Phase       string
	SpecVersion string
	// Cancellation fields identify the actual cancelled object separately from
	// SubjectID, whose authorization epoch this event records.
	CancellationObjectType string
	CancellationObjectID   string
	RelatedRunID           string
	ScopeRejection         *ScopeRejection
}

func (e *Engine) authorization(s *subject, action string, refs []Ref) {
	s.record.AuthorizationEpoch++
	actor := Authorizer
	if action == "scope-paused" || action == "scope-change-cancelled" || action == "run-cancelled" {
		actor = s.owner
	}
	e.authorizations = append(e.authorizations, AuthorizationEvent{
		ID:        fmt.Sprintf("auth-%s-%d", s.record.ID, s.record.AuthorizationEpoch),
		SubjectID: s.record.ID, Epoch: s.record.AuthorizationEpoch, At: time.Now().UTC(),
		Actor: actor, Action: action, Materials: append([]Ref(nil), refs...), Phase: s.record.Phase, SpecVersion: s.record.SpecVersion,
	})
}

// cancellation preserves the existing subject/epoch update and adds only
// body-free object identity. Cascaded preparation termination still names the
// original run as the cancellation object, not a separate application cancel.
func (e *Engine) cancellation(s *subject, action, objectType, objectID, runID string) {
	e.authorization(s, action, nil)
	event := &e.authorizations[len(e.authorizations)-1]
	event.CancellationObjectType = objectType
	event.CancellationObjectID = objectID
	event.RelatedRunID = runID
}

func (e *Engine) AuthorizationEvents(actor string) ([]AuthorizationEvent, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if actor != Observer {
		return nil, ErrDenied
	}
	out := append([]AuthorizationEvent{}, e.authorizations...)
	for i := range out {
		out[i].Materials = append([]Ref(nil), out[i].Materials...)
		out[i].ScopeRejection = cloneRejection(out[i].ScopeRejection)
		if out[i].SendScope != nil {
			scope := cloneScope(*out[i].SendScope)
			out[i].SendScope = &scope
		}
	}
	return out, nil
}

// CancelScopeChange cancels only the proposed expansion, not the original run.
// An already dispatched pending request has a changed epoch and remains
// ineligible for delivery and is cleared as withheld when this expansion is
// cancelled. A fresh request is an explicit action, never an automatic retry.
func (e *Engine) CancelScopeChange(actor, preparationID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	p, err := e.owned(actor, preparationID)
	if err != nil {
		return err
	}
	if e.closed || p.record.Phase != "preparation" || p.record.RelatedRunID == "" || p.record.State == "confirmed" || p.record.State == "cancelled" || p.record.State == "rejected" || p.record.State == "closed" {
		return ErrState
	}
	old, ok := e.subjects[p.record.RelatedRunID]
	if !ok || old.record.State != "paused" {
		return ErrState
	}
	e.finishScopeChange(p, old, "cancelled")
	e.cancellation(old, "scope-change-cancelled", "scope-change-application", p.record.ID, old.record.ID)
	return nil
}

func (e *Engine) cancelPending(subjectID, state string) {
	for _, request := range e.requests {
		if request.SubjectID == subjectID && (request.State == "pending" || request.State == "queued") {
			terminal := state
			if request.State == "queued" && state == "withheld" {
				terminal = "withheld-before-send"
			}
			request.State = terminal
			delete(e.queued, request.ID)
			e.receiver.discard(request.ID)
		}
	}
}

// CancelRun terminates execution. No method can revive this run.
func (e *Engine) CancelRun(actor, runID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.owned(actor, runID)
	if err != nil {
		return err
	}
	if e.closed || s.record.Phase != "execution" || s.record.State == "cancelled" || s.record.State == "rejected" || s.record.State == "decision-recorded" {
		return ErrState
	}
	s.record.State = "cancelled"
	e.cancellation(s, "run-cancelled", "run", runID, runID)
	e.cancelPending(runID, "cancelled")
	for _, proposal := range e.subjects {
		if proposal.record.Phase == "preparation" && proposal.record.RelatedRunID == runID && proposal.record.State != "confirmed" && proposal.record.State != "cancelled" && proposal.record.State != "rejected" && proposal.record.State != "closed" {
			proposal.record.State = "cancelled"
			e.cancellation(proposal, "run-cancelled", "run", runID, runID)
			e.cancelPending(proposal.record.ID, "cancelled")
		}
	}
	return nil
}

// NewRelatedPreparation starts separate work after a cancelled run.
// ConfirmTask must then create a different run, with fresh grants.
func (e *Engine) NewRelatedPreparation(actor, previousRunID string, spec Spec) (Record, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	old, err := e.owned(actor, previousRunID)
	if err != nil {
		return Record{}, err
	}
	if e.closed || old.record.Phase != "execution" || old.record.State != "cancelled" {
		return Record{}, ErrState
	}
	if !e.validSpec(spec) {
		return Record{}, ErrInput
	}
	return e.createPreparation(spec, previousRunID), nil
}

type DrainSignal struct {
	Closed             bool
	PendingRequests    int
	QueuedRequests     int
	ReceiverQueueDepth int
	Idle               bool
}

func (e *Engine) drain() DrainSignal {
	pending := 0
	for _, req := range e.requests {
		if req.State == "pending" {
			pending++
		}
	}
	queued := len(e.receiver.waiting)
	return DrainSignal{Closed: e.closed, PendingRequests: pending, QueuedRequests: len(e.queued), ReceiverQueueDepth: queued, Idle: pending == 0 && queued == 0 && len(e.queued) == 0}
}

func (e *Engine) Drain(actor string) (DrainSignal, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if actor != Observer {
		return DrainSignal{}, ErrDenied
	}
	return e.drain(), nil
}

// Close marks the engine ended and drains every in-process pending request.
// It never claims remote cancellation or deletion: no remote endpoint exists.
func (e *Engine) Close(actor string) (DrainSignal, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if actor != Observer {
		return DrainSignal{}, ErrDenied
	}
	e.closed = true
	for id, s := range e.subjects {
		e.cancelPending(id, "cancelled-on-close")
		if s.record.State == "pending" || s.record.State == "queued" {
			s.record.State = "closed"
		}
	}
	return e.drain(), nil
}
