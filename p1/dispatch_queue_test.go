package p1_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	p1 "onemore.local/p1"
)

func assertNoQueueWire(t *testing.T, e *p1.Engine, out *p1.OutboundProbe, receiver *p1.ControlledReceiver) {
	t.Helper()
	ev := evidence(t, e)
	if len(ev.Reads)+len(ev.Outbound)+len(ev.Received)+len(out.Captures())+len(receiver.Captures()) != 0 {
		t.Fatal("queue/revocation crossed a material or serialized transport boundary")
	}
}

func TestQueuedRequestRevocationStopsBeforeSerializationInBothPhases(t *testing.T) {
	for _, phase := range []string{"preparation", "execution"} {
		for _, kind := range []string{"read", "send", "read-regrant", "send-regrant", "scope-replaced"} {
			t.Run(phase+"/"+kind, func(t *testing.T) {
				out, receiver := p1.NewOutboundProbe(), p1.NewControlledReceiver()
				e, err := p1.NewWithLocalBoundaries(out, receiver)
				must(t, err)
				subject, err := e.CreatePreparation(p1.Employee, spec())
				must(t, err)
				if phase == "execution" {
					subject, err = e.ConfirmTask(p1.Employee, subject.ID)
					must(t, err)
				}
				must(t, e.GrantRead(p1.Authorizer, subject.ID, subject.Spec.Materials))
				must(t, e.GrantSend(p1.Authorizer, subject.ID, summaryScope()))
				queued, err := e.QueueScopedRequest(p1.Employee, subject.ID, summaryScope())
				must(t, err)
				if queued.State != "queued" || queued.SimulatedUnits != 0 || queued.PayloadSHA256 != "" || queued.QueuedAt.IsZero() || !queued.DispatchedAt.IsZero() {
					t.Fatal("queue was treated as a send")
				}
				snapshot, err := e.QueueSnapshot(p1.Observer)
				must(t, err)
				if len(snapshot) != 1 || snapshot[0].ID != queued.ID {
					t.Fatal("request not in actual dispatch queue")
				}
				drain, err := e.Drain(p1.Observer)
				must(t, err)
				if drain.Idle || drain.QueuedRequests != 1 || drain.PendingRequests != 0 || drain.ReceiverQueueDepth != 0 {
					t.Fatal("queue falsely reported idle")
				}
				if err := e.CompleteRequest(p1.Employee, queued.ID); !errors.Is(err, p1.ErrState) {
					t.Fatal("unsent request completed")
				}
				assertNoQueueWire(t, e, out, receiver)
				switch kind {
				case "read", "read-regrant":
					must(t, e.RevokeRead(p1.Authorizer, subject.ID, a))
					if kind == "read-regrant" {
						must(t, e.GrantRead(p1.Authorizer, subject.ID, []p1.Ref{a}))
					}
				case "send", "send-regrant":
					must(t, e.RevokeSend(p1.Authorizer, subject.ID))
					if kind == "send-regrant" {
						must(t, e.GrantSend(p1.Authorizer, subject.ID, summaryScope()))
					}
				case "scope-replaced":
					full, err := p1.SyntheticSendScope(subject.Spec.Materials)
					must(t, err)
					must(t, e.GrantSend(p1.Authorizer, subject.ID, full))
				}
				blocked, err := e.DispatchQueuedRequest(p1.Employee, queued.ID)
				if !errors.Is(err, p1.ErrDenied) || blocked.ID != queued.ID || blocked.State != "withheld-before-send" || blocked.SimulatedUnits != 0 || blocked.PayloadSHA256 != "" || !blocked.DispatchedAt.IsZero() {
					t.Fatalf("stale queue dispatched: %+v %v", blocked, err)
				}
				assertNoQueueWire(t, e, out, receiver)
				snapshot, err = e.QueueSnapshot(p1.Observer)
				must(t, err)
				if len(snapshot) != 0 {
					t.Fatal("blocked queue not removed")
				}
				drain, err = e.Drain(p1.Observer)
				must(t, err)
				if !drain.Idle || drain.QueuedRequests != 0 {
					t.Fatal("blocked queue not drained")
				}
				if _, err := e.DispatchQueuedRequest(p1.Employee, queued.ID); !errors.Is(err, p1.ErrState) {
					t.Fatal("stale queue revived")
				}
				end, err := e.Close(p1.Observer)
				must(t, err)
				if !end.Closed || !end.Idle {
					t.Fatal("end not observable")
				}
			})
		}
	}
}

func TestQueuedPositiveControlUsesFrozenSelectorsAndDispatchesOnce(t *testing.T) {
	e, out, receiver, run := observedRun(t)
	approved := summaryScope()
	must(t, e.GrantSend(p1.Authorizer, run.ID, approved))
	queued, err := e.QueueScopedRequest(p1.Employee, run.ID, approved)
	must(t, err)
	approved.Inputs[0].Fields[0] = p1.FieldText
	queued.SendScope.Inputs[0].Fields[0] = p1.FieldText
	snapshot, err := e.QueueSnapshot(p1.Observer)
	must(t, err)
	snapshot[0].SendScope.Inputs[0].Ref = c
	requests, err := e.Requests(p1.Observer)
	must(t, err)
	requests[0].SendScope.Inputs[0].Fields[0] = p1.FieldDate
	if _, err := e.QueueSnapshot(p1.Employee); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("employee enumerated queue")
	}
	if _, err := e.DispatchQueuedRequest(p1.Authorizer, queued.ID); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("wrong actor dispatched")
	}
	if _, err := e.QueueScopedRequest(p1.Employee, run.ID, summaryScope()); !errors.Is(err, p1.ErrState) {
		t.Fatal("duplicate queue accepted")
	}
	if _, err := e.StartRequest(p1.Employee, run.ID); !errors.Is(err, p1.ErrState) {
		t.Fatal("immediate entry bypassed queue")
	}
	assertNoQueueWire(t, e, out, receiver)
	sent, err := e.DispatchQueuedRequest(p1.Employee, queued.ID)
	must(t, err)
	if sent.State != "pending" || sent.SimulatedUnits != 1 || sent.PayloadSHA256 == "" || sent.DispatchedAt.IsZero() {
		t.Fatal("positive dispatch not observed")
	}
	var wire p1.SerializedRequest
	must(t, json.Unmarshal(receiver.Captures()[0].Body, &wire))
	if len(wire.Inputs) != 1 || wire.Inputs[0].Ref != a || len(wire.Inputs[0].Fields) != 1 || wire.Inputs[0].Fields[p1.FieldScopeSummary] == "" {
		t.Fatal("queue selector aliases enlarged wire")
	}
	if _, err := e.DispatchQueuedRequest(p1.Employee, queued.ID); !errors.Is(err, p1.ErrState) {
		t.Fatal("request dispatched twice")
	}
	if len(out.Captures()) != 1 || len(receiver.Captures()) != 1 {
		t.Fatal("duplicate attempt counted")
	}
	must(t, e.CompleteRequest(p1.Employee, queued.ID))
	drain, err := e.Drain(p1.Observer)
	must(t, err)
	if !drain.Idle || drain.QueuedRequests != 0 || drain.PendingRequests != 0 || drain.ReceiverQueueDepth != 0 {
		t.Fatal("positive queue not drained")
	}
}

func TestPreSendAndPostSendRevocationHaveSeparateHistory(t *testing.T) {
	for _, beforeSend := range []bool{true, false} {
		t.Run(map[bool]string{true: "queued-before-send", false: "sent-waiting-result"}[beforeSend], func(t *testing.T) {
			e, out, receiver, run := observedRun(t)
			must(t, e.GrantSend(p1.Authorizer, run.ID, summaryScope()))
			var request p1.Request
			var err error
			if beforeSend {
				request, err = e.QueueScopedRequest(p1.Employee, run.ID, summaryScope())
			} else {
				request, err = e.StartScopedRequest(p1.Employee, run.ID, summaryScope())
			}
			must(t, err)
			must(t, e.RevokeSend(p1.Authorizer, run.ID))
			if beforeSend {
				blocked, err := e.DispatchQueuedRequest(p1.Employee, request.ID)
				if !errors.Is(err, p1.ErrDenied) || blocked.State != "withheld-before-send" || blocked.SimulatedUnits != 0 || blocked.PayloadSHA256 != "" {
					t.Fatal("unsent request misclassified")
				}
				assertNoQueueWire(t, e, out, receiver)
			} else {
				if err := e.CompleteRequest(p1.Employee, request.ID); !errors.Is(err, p1.ErrDenied) {
					t.Fatal("late result accepted")
				}
				history, err := e.Requests(p1.Observer)
				must(t, err)
				if history[0].State != "withheld" || history[0].SimulatedUnits != 1 || history[0].PayloadSHA256 == "" || len(out.Captures()) != 1 || len(receiver.Captures()) != 1 {
					t.Fatal("previously sent history erased or confused with queue")
				}
			}
		})
	}
}

func TestScopeResolutionDrainsQueuedOriginalAndApplication(t *testing.T) {
	for _, resolve := range []string{"cancel", "reject"} {
		for _, dispatchFirst := range []bool{false, true} {
			t.Run(resolve+map[bool]string{false: "-resolve-first", true: "-dispatch-while-paused"}[dispatchFirst], func(t *testing.T) {
				e, out, receiver, run := observedRun(t)
				must(t, e.GrantSend(p1.Authorizer, run.ID, summaryScope()))
				queued, err := e.QueueScopedRequest(p1.Employee, run.ID, summaryScope())
				must(t, err)
				expanded := spec()
				expanded.Materials = append(expanded.Materials, c)
				app, err := e.ChangeScope(p1.Employee, run.ID, expanded)
				must(t, err)
				must(t, e.GrantRead(p1.Authorizer, app.ID, []p1.Ref{a}))
				must(t, e.GrantSend(p1.Authorizer, app.ID, summaryScope()))
				appRequest, err := e.QueueScopedRequest(p1.Employee, app.ID, summaryScope())
				must(t, err)
				drain, err := e.Drain(p1.Observer)
				must(t, err)
				if drain.QueuedRequests != 2 {
					t.Fatal("test did not freeze both queues")
				}
				if dispatchFirst {
					if _, err := e.DispatchQueuedRequest(p1.Employee, queued.ID); !errors.Is(err, p1.ErrDenied) {
						t.Fatal("paused run dispatched")
					}
				}
				if resolve == "reject" {
					must(t, e.RejectScopeChange(p1.Authorizer, app.ID, p1.RejectScopeNotApproved))
				} else {
					must(t, e.CancelScopeChange(p1.Employee, app.ID))
				}
				assertNoQueueWire(t, e, out, receiver)
				drain, err = e.Drain(p1.Observer)
				must(t, err)
				if !drain.Idle || drain.QueuedRequests != 0 {
					t.Fatal("resolution left ghost queue")
				}
				for _, id := range []string{queued.ID, appRequest.ID} {
					if _, err := e.DispatchQueuedRequest(p1.Employee, id); !errors.Is(err, p1.ErrState) {
						t.Fatal("resolved queue dispatched late")
					}
				}
				current, err := e.View(p1.Employee, run.ID)
				must(t, err)
				if current.State != "confirmed" || !current.Partial || !reflect.DeepEqual(current.Spec, run.Spec) {
					t.Fatal("original scope not preserved")
				}
				// An explicit new request uses only the old, still-valid grant.
				next, err := e.StartRequest(p1.Employee, run.ID)
				must(t, err)
				if next.ID == queued.ID || !reflect.DeepEqual(next.SendScope, summaryScope()) {
					t.Fatal("resolution retried or enlarged old request")
				}
				must(t, e.CompleteRequest(p1.Employee, next.ID))
			})
		}
	}
}

func TestCloseAndRunCancellationAccountForQueuedAndSentRequests(t *testing.T) {
	for _, closeEngine := range []bool{true, false} {
		t.Run(map[bool]string{true: "close", false: "cancel-separate-runs"}[closeEngine], func(t *testing.T) {
			e, out, receiver, first := observedRun(t)
			must(t, e.GrantSend(p1.Authorizer, first.ID, summaryScope()))
			queued, err := e.QueueScopedRequest(p1.Employee, first.ID, summaryScope())
			must(t, err)
			prep, err := e.CreatePreparation(p1.Employee, spec())
			must(t, err)
			second, err := e.ConfirmTask(p1.Employee, prep.ID)
			must(t, err)
			must(t, e.GrantRead(p1.Authorizer, second.ID, second.Spec.Materials))
			must(t, e.GrantSend(p1.Authorizer, second.ID, summaryScope()))
			sent, err := e.StartRequest(p1.Employee, second.ID)
			must(t, err)
			drain, err := e.Drain(p1.Observer)
			must(t, err)
			if drain.QueuedRequests != 1 || drain.PendingRequests != 1 || drain.ReceiverQueueDepth != 1 {
				t.Fatal("test did not establish separate cut points")
			}
			if closeEngine {
				_, err = e.Close(p1.Observer)
				must(t, err)
			} else {
				must(t, e.CancelRun(p1.Employee, first.ID))
				remaining, err := e.Drain(p1.Observer)
				must(t, err)
				if remaining.Idle || remaining.PendingRequests != 1 || remaining.QueuedRequests != 0 {
					t.Fatal("cancelling one run affected unrelated sent work")
				}
				must(t, e.CancelRun(p1.Employee, second.ID))
			}
			drain, err = e.Drain(p1.Observer)
			must(t, err)
			if !drain.Idle || drain.QueuedRequests != 0 || drain.PendingRequests != 0 || drain.ReceiverQueueDepth != 0 {
				t.Fatal("terminal queues not drained")
			}
			history, err := e.Requests(p1.Observer)
			must(t, err)
			if history[0].ID != queued.ID || history[0].SimulatedUnits != 0 || history[0].PayloadSHA256 != "" || history[1].ID != sent.ID || history[1].SimulatedUnits != 1 || history[1].PayloadSHA256 == "" {
				t.Fatal("queue cleanup changed dispatch accounting")
			}
			if _, err := e.DispatchQueuedRequest(p1.Employee, queued.ID); !errors.Is(err, p1.ErrState) {
				t.Fatal("terminal queue revived")
			}
			if err := e.CompleteRequest(p1.Employee, sent.ID); !errors.Is(err, p1.ErrState) {
				t.Fatal("terminal sent result revived")
			}
			if len(out.Captures()) != 1 || len(receiver.Captures()) != 1 {
				t.Fatal("terminal cleanup changed actual attempts")
			}
		})
	}
}

func TestQueueWithoutMatchingAuthorizationHasNoEntryOrEffects(t *testing.T) {
	e, out, receiver, run := observedRun(t)
	if _, err := e.QueueScopedRequest(p1.Employee, run.ID, summaryScope()); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("scope selector granted sending")
	}
	if _, err := e.QueueScopedRequest(p1.Observer, run.ID, summaryScope()); !errors.Is(err, p1.ErrDenied) {
		t.Fatal("observer queued work")
	}
	snapshot, err := e.QueueSnapshot(p1.Observer)
	must(t, err)
	if len(snapshot) != 0 {
		t.Fatal("denied attempt enqueued")
	}
	drain, err := e.Drain(p1.Observer)
	must(t, err)
	if !drain.Idle {
		t.Fatal("denied queue changed idle state")
	}
	assertNoQueueWire(t, e, out, receiver)
}
