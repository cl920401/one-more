package p1

import (
	"fmt"
	"time"
)

// QueueScopedRequest freezes authorized selectors in an actual in-memory
// queue. It does not read bodies, construct a payload or serialize/send data.
// Dispatch is caller-controlled: no worker or timer can cross this cut point.
func (e *Engine) QueueScopedRequest(actor, id string, scope SendScope) (Request, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.owned(actor, id)
	if err != nil {
		return Request{}, err
	}
	return e.queueScoped(s, cloneScope(scope))
}

func (e *Engine) queueScoped(s *subject, scope SendScope) (Request, error) {
	if e.closed || !dispatchable(s) {
		return Request{}, ErrState
	}
	if s.send == nil || !e.validSendScope(s, scope) || !scopeAllows(*s.send, scope) || !e.sendReadable(s, scope) {
		return Request{}, ErrDenied
	}
	req := &Request{ID: e.id("request"), SubjectID: s.record.ID, Phase: s.record.Phase,
		AuthorizationEpoch: s.record.AuthorizationEpoch, Model: SimulatedModel, State: "queued",
		SendScope: cloneScope(scope), QueuedAt: time.Now().UTC()}
	e.requests[req.ID] = req
	e.queued[req.ID] = struct{}{}
	s.record.State = "queued"
	return cloneRequest(*req), nil
}

// DispatchQueuedRequest rechecks the current boundary under the same lock
// used by revocation, immediately before reading/projection/serialization.
// A revoke that returned before this call cannot be bypassed by a frozen grant.
func (e *Engine) DispatchQueuedRequest(actor, requestID string) (Request, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	req, ok := e.requests[requestID]
	if !ok {
		return Request{}, ErrDenied
	}
	s, err := e.owned(actor, req.SubjectID)
	if err != nil {
		return Request{}, err
	}
	return e.dispatchQueued(s, req)
}

func (e *Engine) dispatchQueued(s *subject, req *Request) (Request, error) {
	_, present := e.queued[req.ID]
	if e.closed || req.State != "queued" || !present {
		return cloneRequest(*req), ErrState
	}
	if s.record.State != "queued" || req.Phase != s.record.Phase ||
		req.AuthorizationEpoch != s.record.AuthorizationEpoch || s.send == nil ||
		!e.validSendScope(s, req.SendScope) || !scopeAllows(*s.send, req.SendScope) || !e.sendReadable(s, req.SendScope) {
		delete(e.queued, req.ID)
		req.State = "withheld-before-send"
		if s.record.State == "queued" {
			s.record.State = "blocked"
		}
		return cloneRequest(*req), ErrDenied
	}
	delete(e.queued, req.ID)
	req.DispatchedAt = time.Now().UTC()
	ctx := boundaryContext{subject: s.record.ID, request: req.ID, phase: req.Phase, epoch: req.AuthorizationEpoch, scope: &req.SendScope}
	payload := SerializedRequest{RequestID: req.ID, SubjectID: s.record.ID, Phase: req.Phase,
		SpecVersion: s.record.SpecVersion, Target: req.SendScope.Target, Inputs: e.project(ctx, req.SendScope)}
	req.SimulatedUnits = 1 // Existing counter: one actual local dispatch attempt.
	digest, err := e.outbound.send(ctx, payload)
	req.PayloadSHA256 = digest
	if err != nil {
		req.State = "failed"
		s.record.State = "blocked"
		return cloneRequest(*req), err
	}
	req.State = "pending"
	s.record.State = "pending"
	return cloneRequest(*req), nil
}

// QueueSnapshot observes the real queued IDs independently of record.State.
// It exposes metadata copies only, never prebuilt payloads or source bodies.
func (e *Engine) QueueSnapshot(actor string) ([]Request, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if actor != Observer {
		return nil, ErrDenied
	}
	out := []Request{}
	for n := 1; n <= e.next; n++ {
		id := fmt.Sprintf("request-%04d", n)
		if _, queued := e.queued[id]; queued {
			out = append(out, cloneRequest(*e.requests[id]))
		}
	}
	return out, nil
}
