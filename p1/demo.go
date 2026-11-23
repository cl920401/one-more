package p1

import "fmt"

type DemoCase struct {
	Name               string
	Observation        string
	Records            []Record
	Draft              *Draft
	CitationParagraphs []Paragraph
	Events             Evidence
	Authorizations     []AuthorizationEvent
	Requests           []Request
	End                DrainSignal
	Cut                *LocalCut
	OutboundCapture    []WireCapture
	ReceiverCapture    []WireCapture
}

type DemoReport struct {
	Kind             string
	Model            string
	FormalAcceptance string
	Cases            []DemoCase
}

func demoSpec() Spec {
	return Spec{Purpose: "公司内部合成需求核对", Materials: []Ref{{ID: "S-A", Version: "v1"}, {ID: "S-B", Version: "v1"}},
		Conditions: []string{"核对保存期限、报价及验收标准", "逐项回查材料版本和段落", "由员工记录采用决定及原任务完整性"}}
}

func demoGrant(e *Engine, id string, refs []Ref) error {
	if err := e.GrantRead(Authorizer, id, refs); err != nil {
		return err
	}
	scope, err := SyntheticSendScope(refs)
	if err != nil {
		return err
	}
	return e.GrantSend(Authorizer, id, scope)
}

func demoRun(e *Engine) (Record, error) {
	p, err := e.CreatePreparation(Employee, demoSpec())
	if err != nil {
		return Record{}, err
	}
	return e.ConfirmTask(Employee, p.ID)
}

func finishCase(e *Engine, c DemoCase) (DemoCase, error) {
	var err error
	c.End, err = e.Close(Observer)
	if err != nil {
		return c, err
	}
	c.Events, err = e.Evidence(Observer)
	if err != nil {
		return c, err
	}
	c.Authorizations, err = e.AuthorizationEvents(Observer)
	if err != nil {
		return c, err
	}
	c.Requests, err = e.Requests(Observer)
	return c, err
}

// RunDemo uses only hardcoded fictional inputs. decision is an explicit CLI
// employee choice; an empty value leaves adoption pending.
func RunDemo(scenario, decision string) (DemoReport, error) {
	if decision != "" && decision != "采用" && decision != "改后采用" && decision != "退回" {
		return DemoReport{}, ErrInput
	}
	report := DemoReport{Kind: "development P1 in-process simulation", Model: SimulatedModel, FormalAcceptance: "未进行独立验收"}
	names := []string{"normal", "scope", "cancel", "revoke"}
	if scenario != "all" {
		names = []string{scenario}
	}
	for _, name := range names {
		var c DemoCase
		var err error
		switch name {
		case "normal":
			c, err = demoNormal(decision)
		case "scope":
			c, err = demoScope()
		case "cancel":
			c, err = demoCancel()
		case "revoke":
			c, err = demoRevoke()
		case "reject":
			c, err = demoReject()
		case "queued-revoke":
			c, err = demoQueued(true)
		case "queued-control":
			c, err = demoQueued(false)
		default:
			return DemoReport{}, ErrInput
		}
		if err != nil {
			return DemoReport{}, fmt.Errorf("%s: %w", name, err)
		}
		report.Cases = append(report.Cases, c)
	}
	return report, nil
}

func demoNormal(decision string) (DemoCase, error) {
	e := New()
	p, err := e.CreatePreparation(Employee, demoSpec())
	if err != nil {
		return DemoCase{}, err
	}
	if _, err = e.StartRequest(Employee, p.ID); err != ErrDenied {
		return DemoCase{}, ErrBoundary
	}
	if err = demoGrant(e, p.ID, p.Spec.Materials); err != nil {
		return DemoCase{}, err
	}
	prepareRequest, err := e.StartRequest(Employee, p.ID)
	if err != nil {
		return DemoCase{}, err
	}
	if err = e.CompleteRequest(Employee, prepareRequest.ID); err != nil {
		return DemoCase{}, err
	}
	run, err := e.ConfirmTask(Employee, p.ID)
	if err != nil {
		return DemoCase{}, err
	}
	// New execution has no preparation grants.
	if _, err = e.StartRequest(Employee, run.ID); err != ErrDenied {
		return DemoCase{}, ErrBoundary
	}
	if err = e.GrantRead(Authorizer, run.ID, run.Spec.Materials); err != nil {
		return DemoCase{}, err
	}
	if _, err = e.StartRequest(Employee, run.ID); err != ErrDenied {
		return DemoCase{}, ErrBoundary
	}
	scope, err := SyntheticSendScope(run.Spec.Materials)
	if err != nil {
		return DemoCase{}, err
	}
	if err = e.GrantSend(Authorizer, run.ID, scope); err != nil {
		return DemoCase{}, err
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
	result := DemoCase{Name: "normal", Observation: "准备与执行授权分离；只读不授予发送；三类内部草稿；默认待员工决定", Draft: &draft}
	for _, entry := range draft.Entries {
		for _, cite := range entry.Citations {
			paragraph, err := e.OpenCitation(Employee, run.ID, cite)
			if err != nil {
				return DemoCase{}, err
			}
			result.CitationParagraphs = append(result.CitationParagraphs, paragraph)
		}
	}
	if decision != "" {
		if err = e.RecordDecision(Employee, run.ID, decision, "命令入口显式合成人工选择；原任务仍有待补问项", false); err != nil {
			return DemoCase{}, err
		}
	}
	p, err = e.View(Employee, p.ID)
	if err != nil {
		return DemoCase{}, err
	}
	run, err = e.View(Employee, run.ID)
	if err != nil {
		return DemoCase{}, err
	}
	result.Records = []Record{p, run}
	return finishCase(e, result)
}

func demoScope() (DemoCase, error) {
	e := New()
	run, err := demoRun(e)
	if err != nil {
		return DemoCase{}, err
	}
	if err = demoGrant(e, run.ID, run.Spec.Materials); err != nil {
		return DemoCase{}, err
	}
	expanded := demoSpec()
	expanded.Materials = append(expanded.Materials, Ref{ID: "S-C", Version: "v1"})
	change, err := e.ChangeScope(Employee, run.ID, expanded)
	if err != nil {
		return DemoCase{}, err
	}
	if _, err = e.StartRequest(Employee, run.ID); err != ErrState {
		return DemoCase{}, ErrBoundary
	}
	if err = e.CancelScopeChange(Employee, change.ID); err != nil {
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
	run, err = e.View(Employee, run.ID)
	if err != nil {
		return DemoCase{}, err
	}
	change, err = e.View(Employee, change.ID)
	if err != nil {
		return DemoCase{}, err
	}
	return finishCase(e, DemoCase{Name: "scope", Observation: "取消扩围不授权新增材料；原范围可继续并保留部分状态", Records: []Record{run, change}})
}

func demoCancel() (DemoCase, error) {
	e := New()
	run, err := demoRun(e)
	if err != nil {
		return DemoCase{}, err
	}
	if err = demoGrant(e, run.ID, run.Spec.Materials); err != nil {
		return DemoCase{}, err
	}
	req, err := e.StartRequest(Employee, run.ID)
	if err != nil {
		return DemoCase{}, err
	}
	if err = e.CancelRun(Employee, run.ID); err != nil {
		return DemoCase{}, err
	}
	if err = e.CompleteRequest(Employee, req.ID); err != ErrState {
		return DemoCase{}, ErrBoundary
	}
	if _, err = e.StartRequest(Employee, run.ID); err != ErrState {
		return DemoCase{}, ErrBoundary
	}
	p, err := e.NewRelatedPreparation(Employee, run.ID, demoSpec())
	if err != nil {
		return DemoCase{}, err
	}
	next, err := e.ConfirmTask(Employee, p.ID)
	if err != nil {
		return DemoCase{}, err
	}
	if _, err = e.StartRequest(Employee, next.ID); err != ErrDenied {
		return DemoCase{}, ErrBoundary
	}
	run, err = e.View(Employee, run.ID)
	if err != nil {
		return DemoCase{}, err
	}
	return finishCase(e, DemoCase{Name: "cancel", Observation: "整运行取消后拒收晚到结果；新关联运行须重新授权", Records: []Record{run, p, next}})
}

func demoRevoke() (DemoCase, error) {
	e := New()
	run, err := demoRun(e)
	if err != nil {
		return DemoCase{}, err
	}
	if err = demoGrant(e, run.ID, run.Spec.Materials); err != nil {
		return DemoCase{}, err
	}
	req, err := e.StartRequest(Employee, run.ID)
	if err != nil {
		return DemoCase{}, err
	}
	if err = e.RevokeRead(Authorizer, run.ID, run.Spec.Materials[0]); err != nil {
		return DemoCase{}, err
	}
	if err = e.CompleteRequest(Employee, req.ID); err != ErrDenied {
		return DemoCase{}, ErrBoundary
	}
	if _, err = e.ViewDraft(Employee, run.ID); err != ErrDenied {
		return DemoCase{}, ErrBoundary
	}
	run, err = e.View(Employee, run.ID)
	if err != nil {
		return DemoCase{}, err
	}
	return finishCase(e, DemoCase{Name: "revoke", Observation: "已发生模拟发送保留；撤权后结果不交付，不声称追回此前发送", Records: []Record{run}})
}
