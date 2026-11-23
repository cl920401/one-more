package p1

import "strings"

// SendField is a closed set of data fields, separate from the required
// routing/selection metadata in SerializedRequest.
type SendField string

const (
	FieldScopeSummary SendField = "scope_summary"
	FieldText         SendField = "text"
	FieldDate         SendField = "date"
	FieldSpeaker      SendField = "speaker"
)

type SendInput struct {
	Ref       Ref         `json:"ref"`
	Paragraph string      `json:"paragraph"`
	Fields    []SendField `json:"fields"`
}

type SendScope struct {
	Target string      `json:"target"`
	Inputs []SendInput `json:"inputs"`
}

// The envelope and selectors are a fixed metadata whitelist. Only Fields
// contains source data; no Material, purpose, conditions or history is encoded.
type SerializedInput struct {
	Ref       Ref                  `json:"ref"`
	Paragraph string               `json:"paragraph"`
	Fields    map[SendField]string `json:"fields"`
}

type SerializedRequest struct {
	RequestID   string            `json:"request_id"`
	SubjectID   string            `json:"subject_id"`
	Phase       string            `json:"phase"`
	SpecVersion string            `json:"spec_version"`
	Target      string            `json:"target"`
	Inputs      []SerializedInput `json:"inputs"`
}

func cloneScope(scope SendScope) SendScope {
	scope.Inputs = append([]SendInput(nil), scope.Inputs...)
	for i := range scope.Inputs {
		scope.Inputs[i].Fields = append([]SendField(nil), scope.Inputs[i].Fields...)
	}
	return scope
}

func validField(field SendField) bool {
	return field == FieldScopeSummary || field == FieldText || field == FieldDate || field == FieldSpeaker
}

func (e *Engine) validSendScope(s *subject, scope SendScope) bool {
	if scope.Target != SimulatedModel || len(scope.Inputs) == 0 {
		return false
	}
	seen := make(map[Citation]bool)
	for _, input := range scope.Inputs {
		key := Citation{Ref: input.Ref, Paragraph: input.Paragraph}
		if !containsRef(s.record.Spec.Materials, input.Ref) || !e.reader.hasParagraph(input.Ref, input.Paragraph) || seen[key] || len(input.Fields) == 0 {
			return false
		}
		seen[key] = true
		fields := make(map[SendField]bool)
		for _, field := range input.Fields {
			if !validField(field) || fields[field] {
				return false
			}
			fields[field] = true
		}
	}
	return true
}

func scopeAllows(granted, wanted SendScope) bool {
	if granted.Target != wanted.Target {
		return false
	}
	for _, input := range wanted.Inputs {
		found := false
		for _, approved := range granted.Inputs {
			if approved.Ref != input.Ref || approved.Paragraph != input.Paragraph {
				continue
			}
			found = true
			for _, field := range input.Fields {
				allowed := false
				for _, approvedField := range approved.Fields {
					if field == approvedField {
						allowed = true
					}
				}
				if !allowed {
					return false
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (e *Engine) sendReadable(s *subject, scope SendScope) bool {
	for _, input := range scope.Inputs {
		if !s.read[input.Ref] {
			return false
		}
	}
	return true
}

// SyntheticSendScope is an explicit demo policy for all paragraphs in the
// selected built-in versions. It supplies selectors, never document bodies.
// Callers must deliberately grant this scope; GrantSend has no default scope.
func SyntheticSendScope(refs []Ref) (SendScope, error) {
	scope := SendScope{Target: SimulatedModel}
	seen := make(map[Ref]bool)
	for _, ref := range refs {
		if seen[ref] {
			return SendScope{}, ErrInput
		}
		seen[ref] = true
		var ids []string
		switch ref {
		case Ref{ID: "S-A", Version: "v1"}:
			ids = []string{"P01", "P02"}
		case Ref{ID: "S-B", Version: "v1"}:
			ids = []string{"P01", "P02", "P03"}
		case Ref{ID: "S-C", Version: "v1"}:
			ids = []string{"P01"}
		default:
			return SendScope{}, ErrInput
		}
		for _, id := range ids {
			scope.Inputs = append(scope.Inputs, SendInput{Ref: ref, Paragraph: id, Fields: []SendField{FieldText, FieldDate, FieldSpeaker}})
		}
	}
	if len(scope.Inputs) == 0 {
		return SendScope{}, ErrInput
	}
	return scope, nil
}

// scopeSummary derives only product/stage names from this one authorized
// paragraph. It deliberately excludes numbers, speaker, date and full text.
func scopeSummary(paragraph Paragraph) string {
	scopes := []string{}
	if strings.Contains(paragraph.Text, "甲产品试点") {
		scopes = append(scopes, "甲产品试点")
	}
	if strings.Contains(paragraph.Text, "乙产品维护期") {
		scopes = append(scopes, "乙产品维护期")
	}
	if len(scopes) == 0 {
		return "本试点"
	}
	return strings.Join(scopes, "；")
}

func fieldValue(paragraph Paragraph, field SendField) string {
	switch field {
	case FieldScopeSummary:
		return scopeSummary(paragraph)
	case FieldText:
		return paragraph.Text
	case FieldDate:
		return paragraph.Date
	case FieldSpeaker:
		return paragraph.Speaker
	}
	return ""
}

func (e *Engine) project(ctx boundaryContext, scope SendScope) []SerializedInput {
	// Read each material once, restricted to the selected paragraphs.
	refs := []Ref{}
	for _, input := range scope.Inputs {
		if !containsRef(refs, input.Ref) {
			refs = append(refs, input.Ref)
		}
	}
	paragraphs := make(map[Citation]Paragraph)
	for _, ref := range refs {
		ids := []string{}
		for _, input := range scope.Inputs {
			if input.Ref == ref {
				ids = append(ids, input.Paragraph)
			}
		}
		for _, p := range e.reader.readSelected(ctx, ref, ids).Paragraphs {
			paragraphs[Citation{Ref: ref, Paragraph: p.ID}] = p
		}
	}
	inputs := make([]SerializedInput, 0, len(scope.Inputs))
	for _, input := range scope.Inputs {
		p := paragraphs[Citation{Ref: input.Ref, Paragraph: input.Paragraph}]
		wire := SerializedInput{Ref: input.Ref, Paragraph: input.Paragraph, Fields: make(map[SendField]string)}
		for _, field := range input.Fields {
			wire.Fields[field] = fieldValue(p, field)
		}
		inputs = append(inputs, wire)
	}
	return inputs
}

// StartScopedRequest accepts selectors only, never caller-supplied payload
// values. Every selector is validated before any reading or serialization.
func (e *Engine) StartScopedRequest(actor, id string, scope SendScope) (Request, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.owned(actor, id)
	if err != nil {
		return Request{}, err
	}
	return e.startScoped(s, cloneScope(scope))
}

// StartRequest uses exactly the explicitly granted projection. It cannot
// recover the old full-Material boolean-send path.
func (e *Engine) StartRequest(actor, id string) (Request, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s, err := e.owned(actor, id)
	if err != nil {
		return Request{}, err
	}
	if e.closed || !dispatchable(s) {
		return Request{}, ErrState
	}
	if s.send == nil {
		return Request{}, ErrDenied
	}
	return e.startScoped(s, cloneScope(*s.send))
}

func cloneRequest(req Request) Request {
	req.SendScope = cloneScope(req.SendScope)
	return req
}

func (e *Engine) startScoped(s *subject, scope SendScope) (Request, error) {
	req, err := e.queueScoped(s, scope)
	if err != nil {
		return Request{}, err
	}
	return e.dispatchQueued(s, e.requests[req.ID])
}

func dispatchable(s *subject) bool {
	return (s.record.Phase == "preparation" && (s.record.State == "form" || s.record.State == "ready")) || (s.record.Phase == "execution" && s.record.State == "confirmed")
}
