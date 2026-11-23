package p1

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// WireCapture is a copy of the exact bytes observed, not a re-encoded object.
// Synthetic test bodies stay only in these opt-in in-memory probes.
type WireCapture struct {
	Source   string
	At       time.Time
	Target   string
	Body     []byte
	SHA256   string
	Accepted bool
}

func bodyDigest(body []byte) string { return fmt.Sprintf("%x", sha256.Sum256(body)) }

func cloneCaptures(captures []WireCapture) []WireCapture {
	out := append([]WireCapture{}, captures...)
	for i := range out {
		out[i].Body = append([]byte(nil), out[i].Body...)
	}
	return out
}

// OutboundProbe can be held by QA separately from Engine.Evidence.
// It is concrete: no custom callback/adapter or network transport is accepted.
type OutboundProbe struct {
	mu       sync.Mutex
	bound    bool
	captures []WireCapture
}

func NewOutboundProbe() *OutboundProbe { return &OutboundProbe{} }

func (p *OutboundProbe) capture(target string, body []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.captures = append(p.captures, WireCapture{Source: "outbound-bytes", At: time.Now().UTC(), Target: target,
		Body: append([]byte(nil), body...), SHA256: bodyDigest(body)})
}

func (p *OutboundProbe) Captures() []WireCapture {
	p.mu.Lock()
	defer p.mu.Unlock()
	return cloneCaptures(p.captures)
}

// ControlledReceiver records exact arrivals before decoding. QA may reject
// arrivals, but cannot provide a network adapter, URL or executable callback.
type ControlledReceiver struct {
	mu       sync.Mutex
	bound    bool
	reject   bool
	captures []WireCapture
}

func NewControlledReceiver() *ControlledReceiver { return &ControlledReceiver{} }

func (r *ControlledReceiver) RejectArrivals(reject bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reject = reject
}

func (r *ControlledReceiver) receive(target string, body []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.captures = append(r.captures, WireCapture{Source: "receiver-bytes", At: time.Now().UTC(), Target: target,
		Body: append([]byte(nil), body...), SHA256: bodyDigest(body), Accepted: !r.reject})
	if r.reject {
		return ErrBoundary
	}
	return nil
}

func (r *ControlledReceiver) Captures() []WireCapture {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneCaptures(r.captures)
}

// NewWithLocalBoundaries binds each concrete observer once to prevent request
// ID collisions between engines. Nil or reused probes fail closed.
func NewWithLocalBoundaries(probe *OutboundProbe, target *ControlledReceiver) (*Engine, error) {
	if probe == nil || target == nil {
		return nil, ErrInput
	}
	probe.mu.Lock()
	defer probe.mu.Unlock()
	target.mu.Lock()
	defer target.mu.Unlock()
	if probe.bound || target.bound {
		return nil, ErrInput
	}
	probe.bound, target.bound = true, true
	receiver := newReceiver(target)
	return &Engine{reader: newReader(), receiver: receiver,
		outbound: &simulatedOutbound{observed: journal{source: "outbound"}, target: receiver, probe: probe},
		subjects: make(map[string]*subject), requests: make(map[string]*Request), queued: make(map[string]struct{})}, nil
}
