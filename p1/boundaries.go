package p1

import (
	"encoding/json"
	"fmt"
	"time"
)

// These journals contain only metadata and fingerprints, never raw payloads.
type Event struct {
	ID                 string
	Source             string
	At                 time.Time
	SubjectID          string
	RequestID          string
	Phase              string
	AuthorizationEpoch uint64
	Materials          []Ref
	SendScope          *SendScope
	PayloadSHA256      string
	Count              int
}

type Evidence struct {
	Reads    []Event
	Outbound []Event
	Received []Event
}

type journal struct {
	source string
	events []Event
}

func (j *journal) record(ctx boundaryContext, refs []Ref, count int) {
	var scope *SendScope
	if ctx.scope != nil {
		cloned := cloneScope(*ctx.scope)
		scope = &cloned
	}
	j.events = append(j.events, Event{
		ID: fmt.Sprintf("%s-%d", j.source, len(j.events)+1), Source: j.source, At: time.Now().UTC(),
		SubjectID: ctx.subject, RequestID: ctx.request, Phase: ctx.phase, AuthorizationEpoch: ctx.epoch,
		Materials: append([]Ref(nil), refs...), SendScope: scope, PayloadSHA256: ctx.digest, Count: count,
	})
}

func (j *journal) snapshot() []Event {
	out := append([]Event{}, j.events...)
	for i := range out {
		out[i].Materials = append([]Ref(nil), out[i].Materials...)
		if out[i].SendScope != nil {
			scope := cloneScope(*out[i].SendScope)
			out[i].SendScope = &scope
		}
	}
	return out
}

type boundaryContext struct {
	subject, request, phase string
	epoch                   uint64
	scope                   *SendScope
	digest                  string
}

type materialReader struct {
	data     map[Ref]Material
	observed journal
}

func newReader() *materialReader {
	r := &materialReader{data: make(map[Ref]Material), observed: journal{source: "reader"}}
	for _, m := range syntheticMaterials() {
		r.data[m.Ref] = cloneMaterial(m)
	}
	return r
}

func (r *materialReader) hasParagraph(ref Ref, id string) bool {
	for _, p := range r.data[ref].Paragraphs {
		if p.ID == id {
			return true
		}
	}
	return false
}

func (r *materialReader) read(ctx boundaryContext, ref Ref) Material {
	m := cloneMaterial(r.data[ref])
	r.observed.record(ctx, []Ref{ref}, 1)
	return m
}

func (r *materialReader) readSelected(ctx boundaryContext, ref Ref, ids []string) Material {
	m := Material{Ref: ref}
	for _, p := range r.data[ref].Paragraphs {
		for _, id := range ids {
			if p.ID == id {
				m.Paragraphs = append(m.Paragraphs, p)
			}
		}
	}
	r.observed.record(ctx, []Ref{ref}, 1)
	return m
}

func cloneMaterial(m Material) Material {
	m.Paragraphs = append([]Paragraph(nil), m.Paragraphs...)
	return m
}

const SimulatedModel = "in-process://synthetic-rules/v1"

type simulatedReceiver struct {
	observed   journal
	waiting    map[string]SerializedRequest
	controlled *ControlledReceiver
}

func newReceiver(controlled *ControlledReceiver) *simulatedReceiver {
	return &simulatedReceiver{observed: journal{source: "receiver"}, waiting: make(map[string]SerializedRequest), controlled: controlled}
}

func (r *simulatedReceiver) receive(ctx boundaryContext, body []byte) error {
	// Capture actual raw arrival before parsing, through a receiver owned by QA.
	if err := r.controlled.receive(SimulatedModel, body); err != nil {
		return err
	}
	var payload SerializedRequest
	if err := json.Unmarshal(body, &payload); err != nil || payload.Target != SimulatedModel {
		return ErrBoundary
	}
	refs := []Ref{}
	for _, input := range payload.Inputs {
		if !containsRef(refs, input.Ref) {
			refs = append(refs, input.Ref)
		}
	}
	ctx.digest = bodyDigest(body)
	r.observed.record(ctx, refs, 1)
	r.waiting[payload.RequestID] = payload
	return nil
}

func (r *simulatedReceiver) finish(id string) []Entry {
	payload := r.waiting[id]
	delete(r.waiting, id)
	// Reconstruct only data actually received. Missing fields remain missing:
	// selectors or fingerprints can never reload untransmitted source text.
	materials := []Material{}
	for _, input := range payload.Inputs {
		index := -1
		for i, m := range materials {
			if m.Ref == input.Ref {
				index = i
			}
		}
		if index < 0 {
			materials = append(materials, Material{Ref: input.Ref})
			index = len(materials) - 1
		}
		paragraph := Paragraph{ID: input.Paragraph, Date: input.Fields[FieldDate], Speaker: input.Fields[FieldSpeaker], Text: input.Fields[FieldText]}
		materials[index].Paragraphs = append(materials[index].Paragraphs, paragraph)
	}
	return buildEntries(materials)
}

func (r *simulatedReceiver) discard(id string) { delete(r.waiting, id) }

type simulatedOutbound struct {
	observed journal
	target   *simulatedReceiver
	probe    *OutboundProbe
}

func (o *simulatedOutbound) send(ctx boundaryContext, payload SerializedRequest) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", ErrBoundary
	}
	digest := bodyDigest(body)
	// The externally held probe captures the exact byte slice handed onwards.
	o.probe.capture(payload.Target, body)
	refs := []Ref{}
	for _, input := range payload.Inputs {
		if !containsRef(refs, input.Ref) {
			refs = append(refs, input.Ref)
		}
	}
	ctx.digest = digest
	o.observed.record(ctx, refs, 1)
	return digest, o.target.receive(ctx, body)
}
