package core

import "sort"

// Signal is a fact about what surrounds a changed unit but is not in the
// diff. Every related node carries the provenance of the relationship; a
// signal never judges whether the change is correct.
type Signal struct {
	Kind    string           `json:"kind"`
	Detail  string           `json:"detail"`
	Related []Recommendation `json:"related"`
}

// SignalCount summarizes one kind of signal across a review.
type SignalCount struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
	Units  int    `json:"units"`
}

// Signal kinds.
const (
	SignalUnchangedCallers = "unchanged-callers"
	SignalTestsChanged     = "tests-changed"
	SignalTestsUnchanged   = "tests-unchanged"
	SignalNoKnownTest      = "no-known-test"
)

const maxSignalNodes = 12

var signalSummaries = map[string]string{
	SignalUnchangedCallers: "with callers that were not changed",
	SignalTestsChanged:     "with a directly related test changed alongside",
	SignalTestsUnchanged:   "with directly related tests that were not changed",
	SignalNoKnownTest:      "with no test calling directly",
}

// attachSignals derives caller and test facts for changed Go units. Other
// languages have no call relationships, so they get no signals at all
// rather than a misleading "no callers".
func attachSignals(changes []UnitChange, oldGraph, newGraph *Graph) ([]SignalCount, []string) {
	// A unit listed only because a relationship moved has not itself changed.
	changed := map[string]bool{}
	for _, change := range changes {
		changed[change.Node.ID] = change.BeforeSource != change.AfterSource
	}
	counts, other := map[string]int{}, false
	for i := range changes {
		change := &changes[i]
		change.Signals = []Signal{}
		if change.Node.UnitID == "" || !changed[change.Node.ID] {
			continue
		}
		if change.Node.Language != "go" {
			other = true
			continue
		}
		graph := newGraph
		if change.Status == "removed" {
			graph = oldGraph
		}
		change.Signals = unitSignals(graph, change.Node, change.Status, changed)
		for _, signal := range change.Signals {
			counts[signal.Kind]++
		}
	}
	summary := make([]SignalCount, 0, len(counts))
	for kind, units := range counts {
		summary = append(summary, SignalCount{Kind: kind, Units: units, Detail: plural(units, "changed unit") + " " + signalSummaries[kind]})
	}
	sort.Slice(summary, func(i, j int) bool { return summary[i].Kind < summary[j].Kind })
	limitations := []string{"Caller and test signals come from direct, statically resolved Go calls: interface dispatch, reflection and indirect coverage are not seen."}
	if other {
		limitations = append(limitations, "Changed units in other languages have no caller or test signals because no call relationships are analysed for them.")
	}
	return summary, limitations
}

func unitSignals(graph *Graph, node Node, status string, changed map[string]bool) []Signal {
	signals := []Signal{}
	callers := []Recommendation{}
	for _, edge := range graph.Incoming(node.ID) {
		caller, ok := graph.Node(edge.From)
		if edge.Kind == "calls" && ok && !changed[caller.ID] && !hasRole(caller, "test support") {
			callers = append(callers, Recommendation{Node: caller, Reason: "Calls this unit and was not changed", Relationship: "called-by", Provenance: edge.Provenance})
		}
	}
	// An added unit can only be called by code that changed to call it.
	if len(callers) > 0 && status != "added" {
		detail := plural(len(callers), "caller") + " not changed in this review"
		if status == "removed" {
			detail = plural(len(callers), "caller") + " in the base not changed in this review"
		}
		signals = append(signals, newSignal(SignalUnchangedCallers, detail, callers))
	}
	if hasRole(node, "test support") || status == "removed" {
		return signals
	}
	return append(signals, testSignals(graph, node, changed)...)
}

func testSignals(graph *Graph, node Node, changed map[string]bool) []Signal {
	with, without := []Recommendation{}, []Recommendation{}
	for _, edge := range graph.Outgoing(node.ID) {
		test, ok := graph.Node(edge.To)
		if edge.Kind != "tested-by" || !ok {
			continue
		}
		if changed[test.ID] {
			with = append(with, Recommendation{Node: test, Reason: "Test calls this unit and changed with it", Relationship: "tested-by", Provenance: edge.Provenance})
		} else {
			without = append(without, Recommendation{Node: test, Reason: "Test calls this unit and was not changed", Relationship: "tested-by", Provenance: edge.Provenance})
		}
	}
	signals := []Signal{}
	if len(with) > 0 {
		signals = append(signals, newSignal(SignalTestsChanged, plural(len(with), "directly related test")+" changed in this review", with))
	}
	if len(without) > 0 {
		signals = append(signals, newSignal(SignalTestsUnchanged, plural(len(without), "directly related test")+" not changed in this review", without))
	}
	if len(signals) == 0 {
		signals = append(signals, newSignal(SignalNoKnownTest, "No test calls this unit directly", nil))
	}
	return signals
}

func newSignal(kind, detail string, related []Recommendation) Signal {
	if related == nil {
		related = []Recommendation{}
	}
	sort.Slice(related, func(i, j int) bool { return related[i].Node.ID < related[j].Node.ID })
	if len(related) > maxSignalNodes {
		related = related[:maxSignalNodes]
	}
	return Signal{Kind: kind, Detail: detail, Related: related}
}
