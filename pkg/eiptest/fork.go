package eiptest

import (
	"testing"

	coreconfig "github.com/eth2030/eth2030/core/config"
)

// ForkProbe drives a chain across a scheduled fork boundary and exposes the
// fork rules active at each phase. This fixes the historical gap where no
// test chain crossed a real fork activation timestamp.
type ForkProbe struct {
	s       *Sim
	fork    Fork
	seconds uint64 // phase lead/lag around the boundary, in seconds
}

// Fork returns a probe for the given fork. The sim must have scheduled it via
// ForkAt(fork, offset).
func (s *Sim) Fork(fork Fork) *ForkProbe {
	s.t.Helper()
	if _, scheduled := s.forkOffset(fork); !scheduled {
		s.t.Fatalf("eiptest: fork %q not scheduled; add eiptest.ForkAt(eiptest.Fork%s, offset) to New()", fork, fork)
	}
	return &ForkProbe{s: s, fork: fork, seconds: 12}
}

func (s *Sim) forkOffset(fork Fork) (uint64, bool) {
	if s.scheduledForks == nil {
		return 0, false
	}
	off, ok := s.scheduledForks[fork]
	return off, ok
}

// Before advances to just below the fork boundary and returns the phase view.
func (p *ForkProbe) Before() *ForkPhase {
	p.s.t.Helper()
	off, _ := p.s.forkOffset(p.fork)
	target := p.s.genesis.Time() + off - p.seconds
	p.s.AdvanceTo(target)
	return &ForkPhase{s: p.s, fork: p.fork, label: "before"}
}

// AtBoundary advances to exactly the fork activation timestamp and returns
// the phase view. The boundary block itself activates the new rules.
func (p *ForkProbe) AtBoundary() *ForkPhase {
	p.s.t.Helper()
	off, _ := p.s.forkOffset(p.fork)
	target := p.s.genesis.Time() + off
	p.s.AdvanceTo(target)
	return &ForkPhase{s: p.s, fork: p.fork, label: "boundary"}
}

// After commits n more blocks (post-boundary rules) and returns the phase view.
func (p *ForkPhase) After(blocks int) *ForkPhase {
	p.s.t.Helper()
	for i := 0; i < blocks; i++ {
		p.s.Commit()
	}
	return p
}

// ForkPhase is a view of the chain at one point relative to a fork boundary.
type ForkPhase struct {
	s     *Sim
	fork  Fork
	label string
}

// Commit builds one block at the current phase timestamp.
func (p *ForkPhase) Commit() *BlockResult { return p.s.Commit() }

// RulesActive reports whether the fork rules are active at the phase head.
func (p *ForkPhase) RulesActive() bool {
	return p.s.forkActiveAt(p.fork, p.s.bc.CurrentBlock().Header().Time)
}

// ExpectActive asserts the fork rules are active at this phase.
func (p *ForkPhase) ExpectActive(t *testing.T) {
	t.Helper()
	if !p.RulesActive() {
		t.Fatalf("fork %s: expected active at %s phase (head time %d)", p.fork, p.label, p.s.bc.CurrentBlock().Header().Time)
	}
}

// ExpectInactive asserts the fork rules are NOT active at this phase.
func (p *ForkPhase) ExpectInactive(t *testing.T) {
	t.Helper()
	if p.RulesActive() {
		t.Fatalf("fork %s: expected inactive at %s phase (head time %d)", p.fork, p.label, p.s.bc.CurrentBlock().Header().Time)
	}
}

// Rules returns the full fork rules struct at the current phase head.
func (p *ForkPhase) Rules() coreconfig.Rules {
	head := p.s.bc.CurrentBlock().Header()
	return p.s.cfg.Rules(head.Number, true, head.Time)
}

// Time returns the current phase head timestamp.
func (p *ForkPhase) Time() uint64 { return p.s.bc.CurrentBlock().Header().Time }

// HeadNumber returns the current head block number.
func (p *ForkPhase) HeadNumber() uint64 { return p.s.bc.CurrentBlock().NumberU64() }
