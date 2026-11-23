package p1_test

import (
	"encoding/json"
	"testing"

	p1 "onemore.local/p1"
)

func TestDemoCompletesAllSyntheticCasesWithObservableDrain(t *testing.T) {
	report, err := p1.RunDemo("all", "")
	must(t, err)
	if len(report.Cases) != 4 {
		t.Fatal("missing scenario")
	}
	for _, c := range report.Cases {
		if !c.End.Closed || !c.End.Idle || c.End.PendingRequests != 0 || c.End.ReceiverQueueDepth != 0 {
			t.Fatalf("not ended and drained: %s", c.Name)
		}
		if len(c.Events.Reads) == 0 || len(c.Events.Outbound) == 0 || len(c.Events.Received) == 0 {
			t.Fatalf("missing positive boundary control: %s", c.Name)
		}
		if c.Draft != nil && c.Draft.CustomerConfirmed {
			t.Fatal("customer confirmation invented")
		}
		for _, record := range c.Records {
			if record.Decision != nil {
				t.Fatal("default demo adopted automatically")
			}
		}
	}
	blob, err := json.Marshal(report)
	must(t, err)
	if len(blob) == 0 {
		t.Fatal("no reviewable demo output")
	}
}

func TestExplicitDemoDecisionIsStillSeparateFromOriginalCompletion(t *testing.T) {
	report, err := p1.RunDemo("normal", "采用")
	must(t, err)
	record := report.Cases[0].Records[1]
	if record.Decision == nil || record.Decision.Outcome != "采用" || record.Decision.OriginalTaskComplete {
		t.Fatal("explicit adoption/completion split lost")
	}
}

func TestOptionalRejectAndQueueDemonstrationsExposeTheirCutPoints(t *testing.T) {
	for _, name := range []string{"reject", "queued-revoke", "queued-control"} {
		t.Run(name, func(t *testing.T) {
			report, err := p1.RunDemo(name, "")
			must(t, err)
			if len(report.Cases) != 1 {
				t.Fatal("optional scenario missing")
			}
			c := report.Cases[0]
			if c.Cut == nil || !c.End.Closed || !c.End.Idle || c.End.QueuedRequests != 0 || c.End.PendingRequests != 0 || c.End.ReceiverQueueDepth != 0 {
				t.Fatal("cut or end signal missing")
			}
			if len(c.Cut.Events.Reads)+len(c.Cut.Events.Outbound)+len(c.Cut.Events.Received)+len(c.Cut.Outbound)+len(c.Cut.Received) != 0 {
				t.Fatal("cut already crossed material or transport boundary")
			}
			switch name {
			case "reject":
				if c.Records[1].State != "rejected" || c.Records[1].Rejection == nil || !c.Records[0].Partial || len(c.OutboundCapture) != 1 || len(c.ReceiverCapture) != 1 {
					t.Fatal("rejection did not preserve explicit original continuation")
				}
			case "queued-revoke":
				if c.Cut.Drain.QueuedRequests != 1 || c.Cut.Drain.Idle || len(c.Cut.Queue) != 1 || c.Requests[0].State != "withheld-before-send" || c.Requests[0].SimulatedUnits != 0 || len(c.OutboundCapture)+len(c.ReceiverCapture) != 0 {
					t.Fatal("pre-send revoke not isolated")
				}
			case "queued-control":
				if c.Cut.Drain.QueuedRequests != 1 || c.Requests[0].State != "returned" || c.Requests[0].SimulatedUnits != 1 || len(c.OutboundCapture) != 1 || len(c.ReceiverCapture) != 1 {
					t.Fatal("queue positive control missing")
				}
			}
		})
	}
}
