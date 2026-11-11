package eiptest

import (
	"fmt"
	"testing"

	"github.com/eth2030/eth2030/core/types"

	coreconfig "github.com/eth2030/eth2030/core/config"
)

// ForkProbe drives a chain across a scheduled fork boundary and exposes the
// fork rules active at each phase. This fixes the historical gap where no
// test chain crossed a real fork activation timestamp.
type ForkProbe struct {
	s          *Sim
	fork       Fork
	activation uint64
}

// Fork returns a probe for the given fork. The sim must have scheduled it via
// ForkAt(fork, offset).
func (s *Sim) Fork(fork Fork) *ForkProbe {
	s.helper()
	offset, scheduled := s.scheduledForks[fork]
	if !scheduled || offset == Never {
		s.fail(fmt.Errorf("eiptest: fork %s has no activation timestamp", fork))
	}
	return &ForkProbe{s: s, fork: fork, activation: s.Genesis().Time() + offset}
}

// Before materializes the last timestamp before activation. If genesis already
// occupies that timestamp, it returns genesis and requires an empty tx list.
func (p *ForkProbe) Before(txs ...*types.Transaction) *ForkPhase {
	p.s.helper()
	if p.activation <= p.s.Genesis().Time() {
		p.s.fail(fmt.Errorf("eiptest: fork %s is active at genesis", p.fork))
	}
	return p.at(p.activation-1, "before", txs)
}

// AtBoundary materializes a block with exactly the fork activation timestamp.
func (p *ForkProbe) AtBoundary(txs ...*types.Transaction) *ForkPhase {
	p.s.helper()
	return p.at(p.activation, "boundary", txs)
}

func (p *ForkProbe) at(timestamp uint64, label string, txs []*types.Transaction) *ForkPhase {
	head := p.s.CurrentBlock()
	phase := &ForkPhase{s: p.s, fork: p.fork, label: label, block: head}
	switch {
	case head.Time() == timestamp && len(txs) == 0:
		return phase
	case head.Time() >= timestamp:
		p.s.fail(fmt.Errorf("eiptest: fork %s %s timestamp %d cannot follow head time %d", p.fork, label, timestamp, head.Time()))
	}
	attrs := p.s.attributes()
	attrs.Timestamp = timestamp
	r, err := p.s.commit(&slicePool{txs: txs}, attrs, nil)
	if err != nil {
		p.s.fail(err)
	}
	phase.block, phase.result = r.Block, r
	return phase
}

// After commits blocks from the current head and returns a new phase view.
func (p *ForkPhase) After(blocks int) *ForkPhase {
	p.s.helper()
	phase := *p
	for i := 0; i < blocks; i++ {
		phase.result = p.s.Commit()
		phase.block = phase.result.Block
	}
	phase.label = "after"
	return &phase
}

// ForkPhase is a snapshot of a block relative to a fork boundary.
type ForkPhase struct {
	s      *Sim
	fork   Fork
	label  string
	block  *types.Block
	result *BlockResult
}

// Result returns the execution result of the phase's new block, if it built one.
func (p *ForkPhase) Result() *BlockResult { return p.result }

// Header returns the header of the phase block.
func (p *ForkPhase) Header() *types.Header { return p.block.Header() }

// RulesActive reports whether the fork was active at the phase block.
func (p *ForkPhase) RulesActive() bool { return p.s.forkActiveAt(p.fork, p.block.Time()) }

// ExpectActive asserts the fork rules are active at this phase.
func (p *ForkPhase) ExpectActive(t *testing.T) {
	t.Helper()
	if !p.RulesActive() {
		t.Fatalf("fork %s: expected active at %s phase (head time %d)", p.fork, p.label, p.block.Time())
	}
}

// ExpectInactive asserts the fork rules are NOT active at this phase.
func (p *ForkPhase) ExpectInactive(t *testing.T) {
	t.Helper()
	if p.RulesActive() {
		t.Fatalf("fork %s: expected inactive at %s phase (head time %d)", p.fork, p.label, p.block.Time())
	}
}

// Rules returns the full fork rules struct at the current phase head.
func (p *ForkPhase) Rules() coreconfig.Rules {
	head := p.block.Header()
	return p.s.cfg.Rules(head.Number, p.s.cfg.IsMerge(), head.Time)
}

// Time returns the current phase head timestamp.
func (p *ForkPhase) Time() uint64 { return p.block.Time() }

// HeadNumber returns the current head block number.
func (p *ForkPhase) HeadNumber() uint64 { return p.block.NumberU64() }
