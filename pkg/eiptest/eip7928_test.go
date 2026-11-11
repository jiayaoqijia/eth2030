package eiptest_test

import (
	"strings"
	"testing"

	"github.com/eth2030/eth2030/bal"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/engine"
)

func TestEIP7928_BlockAccessList(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
	tx := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), 0, eiptest.ToPtr(s.Bob().Address), eiptest.Wei(11), nil, 21_000, bigFeeCap, bigTipCap)
	r := s.Commit(tx)
	eiptest.ExpectSuccess(t, r, 0)
	eiptest.ExpectBAL(t, r)
	changes := make(map[types.Address]bool)
	for _, entry := range r.BAL.Entries {
		if entry.AccessIndex == 1 && entry.BalanceChange != nil {
			changes[entry.Address] = true
		}
	}
	if !changes[s.Alice().Address] || !changes[s.Bob().Address] {
		t.Fatal("BAL omitted a transfer balance change")
	}
	empty := s.Commit()
	eiptest.ExpectBAL(t, empty)
	if len(empty.BAL.Entries) == 0 {
		t.Fatal("empty block omitted system-contract accesses")
	}
}

func TestEIP7928_OrderingInvariant(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
	c := s.STF().Deploy(s.Alice(), []byte{0x60, 1, 0x60, 2, 0x55, 0x60, 2, 0x60, 0, 0x55, 0x00})
	tx := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), s.Nonce(s.Alice().Address),
		eiptest.ToPtr(c), nil, nil, 100_000, bigFeeCap, bigTipCap)
	r := s.Commit(tx)
	eiptest.ExpectSuccess(t, r, 0)
	eiptest.ExpectBAL(t, r)
	if err := bal.ValidateBALOrdering(r.BAL); err != nil {
		t.Fatal(err)
	}
	for _, entry := range r.BAL.Entries {
		if entry.Address == c && len(entry.StorageChanges) == 2 {
			if entry.StorageChanges[0].Slot != (types.Hash{}) || entry.StorageChanges[1].Slot != eiptest.Uint64ToHash(2) {
				t.Fatal("BAL did not sort written storage slots")
			}
			return
		}
	}
	t.Fatal("BAL omitted the two written storage slots")
}

func TestEIP7928_EngineRejectsInconsistentBAL(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
	p := s.Engine()
	head := p.HeadHash()
	id, err := p.FCU()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.GetPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	altered := *resp.ExecutionPayload
	altered.BlockAccessList = []byte(`"0xc0"`)
	status, err := p.Process(&altered)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != engine.StatusInvalid ||
		status.ValidationError == nil || !strings.Contains(*status.ValidationError, "invalid block access list hash") {
		t.Fatalf("modified BAL was not rejected by execution validation: %+v", status)
	}
	if status.LatestValidHash == nil || *status.LatestValidHash != head || p.HeadHash() != head {
		t.Fatalf("invalid BAL changed the receiver's validated head: %s", p.HeadHash().Hex())
	}
	status, err = p.Process(resp.ExecutionPayload)
	if err != nil {
		t.Fatal(err)
	}
	p.ExpectStatusValid(t, status)
}
