// Package logengine does realtime log fan-out, Drain template clustering and
// deterministic anomaly rules.
package logengine

import "strings"

// Rule is an L1 deterministic ES anomaly signature.
type Rule struct {
	Name     string
	Contains string
	Severity string
}

var DefaultRules = []Rule{
	{"jvm-oom", "OutOfMemoryError", "critical"},
	{"circuit-breaker", "CircuitBreakingException", "high"},
	{"rejected-execution", "EsRejectedExecutionException", "high"},
	{"flood-stage", "flood stage disk watermark", "critical"},
	{"master-not-discovered", "master_not_discovered", "critical"},
	{"gc-overhead", "[gc][", "medium"},
}

// Match returns the rules whose signature occurs in msg.
func Match(msg string) []Rule {
	var out []Rule
	for _, r := range DefaultRules {
		if strings.Contains(msg, r.Contains) {
			out = append(out, r)
		}
	}
	return out
}

// Drain is the online log template clusterer.
// TODO: implement fixed-depth parse tree; mask IPs/numbers/index names as <*>.
type Drain struct{}

func NewDrain() *Drain { return &Drain{} }

// Add returns the template id and template text for a log message.
func (d *Drain) Add(msg string) (id int, template string, isNew bool) {
	return 0, msg, false
}
