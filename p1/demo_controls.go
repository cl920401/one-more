package p1

// LocalCut is a developer snapshot at a caller-controlled local boundary.
// No worker can dispatch between Queue and the caller's explicit Dispatch.
type LocalCut struct {
	Queue    []Request
	Drain    DrainSignal
	Events   Evidence
	Outbound []WireCapture
	Received []WireCapture
}

func localCut(e *Engine, out *OutboundProbe, receiver *ControlledReceiver) (*LocalCut, error) {
	queued, err := e.QueueSnapshot(Observer)
	if err != nil {
		return nil, err
	}
	drain, err := e.Drain(Observer)
	if err != nil {
		return nil, err
	}
	events, err := e.Evidence(Observer)
	if err != nil {
		return nil, err
	}
	return &LocalCut{Queue: queued, Drain: drain, Events: events, Outbound: out.Captures(), Received: receiver.Captures()}, nil
}

func demoLocalEngine() (*Engine, *OutboundProbe, *ControlledReceiver, error) {
	out, receiver := NewOutboundProbe(), NewControlledReceiver()
	e, err := NewWithLocalBoundaries(out, receiver)
	return e, out, receiver, err
}

func demoReject() (DemoCase, error) {
	e, out, receiver, err := demoLocalEngine()
	if err != nil {
		return DemoCase{}, err
	}
	run, err := demoRun(e)
	if err != nil {
		return DemoCase{}, err
	}
	if err = demoGrant(e, run.ID, run.Spec.Materials); err != nil {
		return DemoCase{}, err
	}
	expanded := demoSpec()
	expanded.Materials = append(expanded.Materials, Ref{ID: "S-C", Version: "v1"})
	application, err := e.ChangeScope(Employee, run.ID, expanded)
	if err != nil {
		return DemoCase{}, err
	}
	if err = e.RejectScopeChange(Authorizer, application.ID, RejectScopeNotApproved); err != nil {
		return DemoCase{}, err
	}
	cut, err := localCut(e, out, receiver)
	if err != nil {
		return DemoCase{}, err
	}
	if _, err = e.ReadMaterial(Employee, run.ID, Ref{ID: "S-C", Version: "v1"}); err != ErrDenied {
		return DemoCase{}, ErrBoundary
	}
	req, err := e.StartRequest(Employee, run.ID)
	if err != nil {
		return DemoCase{}, err
	}
	if err = e.CompleteRequest(Employee, req.ID); err != nil {
		return DemoCase{}, err
	}
	draft, err := e.ViewDraft(Employee, run.ID)
	if err != nil {
		return DemoCase{}, err
	}
	run, err = e.View(Employee, run.ID)
	if err != nil {
		return DemoCase{}, err
	}
	application, err = e.View(Employee, application.ID)
	if err != nil {
		return DemoCase{}, err
	}
	return finishCase(e, DemoCase{Name: "reject", Observation: "有权者明确拒绝扩围；原授权显式续行并保持部分稿，无新增材料权限",
		Records: []Record{run, application}, Draft: &draft, Cut: cut, OutboundCapture: out.Captures(), ReceiverCapture: receiver.Captures()})
}

func demoQueued(revoke bool) (DemoCase, error) {
	e, out, receiver, err := demoLocalEngine()
	if err != nil {
		return DemoCase{}, err
	}
	run, err := demoRun(e)
	if err != nil {
		return DemoCase{}, err
	}
	scope := SendScope{Target: SimulatedModel, Inputs: []SendInput{{Ref: Ref{ID: "S-A", Version: "v1"}, Paragraph: "P02", Fields: []SendField{FieldScopeSummary}}}}
	if err = e.GrantRead(Authorizer, run.ID, run.Spec.Materials); err != nil {
		return DemoCase{}, err
	}
	if err = e.GrantSend(Authorizer, run.ID, scope); err != nil {
		return DemoCase{}, err
	}
	queued, err := e.QueueScopedRequest(Employee, run.ID, scope)
	if err != nil {
		return DemoCase{}, err
	}
	cut, err := localCut(e, out, receiver)
	if err != nil {
		return DemoCase{}, err
	}
	name, observation := "queued-control", "已排队且无正文读取；显式派发正例实际到达，用于本地调度观察"
	if revoke {
		name, observation = "queued-revoke", "已排队、实际投影和序列化前撤发送权；无读取、出站或接收，不冒充已发送晚到"
		if err = e.RevokeSend(Authorizer, run.ID); err != nil {
			return DemoCase{}, err
		}
		stopped, err := e.DispatchQueuedRequest(Employee, queued.ID)
		if err != ErrDenied || stopped.State != "withheld-before-send" {
			return DemoCase{}, ErrBoundary
		}
	} else {
		sent, err := e.DispatchQueuedRequest(Employee, queued.ID)
		if err != nil {
			return DemoCase{}, err
		}
		if err = e.CompleteRequest(Employee, sent.ID); err != nil {
			return DemoCase{}, err
		}
	}
	run, err = e.View(Employee, run.ID)
	if err != nil {
		return DemoCase{}, err
	}
	return finishCase(e, DemoCase{Name: name, Observation: observation, Records: []Record{run}, Cut: cut,
		OutboundCapture: out.Captures(), ReceiverCapture: receiver.Captures()})
}
