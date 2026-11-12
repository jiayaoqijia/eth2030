package beacon

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

type executionTestChain struct {
	mu     sync.Mutex
	blocks map[types.Hash]*types.Block
}

func (c *executionTestChain) GetBlock(hash types.Hash) *types.Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.blocks[hash]
}
func (c *executionTestChain) InsertBlock(block *types.Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blocks[block.Hash()] = block
	return nil
}

type executionTestFetcher func(context.Context, types.Hash) (*types.Block, error)

func (f executionTestFetcher) FetchBlock(ctx context.Context, hash types.Hash) (*types.Block, error) {
	return f(ctx, hash)
}

func TestExecutionSyncReplacesTargetAndJoinsCancellation(t *testing.T) {
	genesis := types.NewBlock(&types.Header{Number: big.NewInt(0)}, nil)
	first := types.NewBlock(&types.Header{ParentHash: genesis.Hash(), Number: big.NewInt(1), Time: 1}, nil)
	second := types.NewBlock(&types.Header{ParentHash: genesis.Hash(), Number: big.NewInt(1), Time: 2}, nil)
	chain := &executionTestChain{blocks: map[types.Hash]*types.Block{genesis.Hash(): genesis}}
	started := make(chan struct{})
	cancelled := make(chan struct{})
	applied := make(chan ExecutionTarget, 2)
	fetch := executionTestFetcher(func(ctx context.Context, hash types.Hash) (*types.Block, error) {
		if hash == first.Hash() {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		}
		return second, nil
	})
	syncer := NewExecutionSyncer(chain, fetch, func(target ExecutionTarget) error { applied <- target; return nil })
	syncer.Start()
	defer syncer.Stop()
	syncer.Request(ExecutionTarget{Head: first.Hash()})
	<-started
	syncer.Request(ExecutionTarget{Head: second.Hash()})
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("old download not cancelled")
	}
	select {
	case target := <-applied:
		if target.Head != second.Hash() {
			t.Fatal("superseded target was published")
		}
	case <-time.After(time.Second):
		t.Fatal("new target not processed")
	}
	if chain.GetBlock(first.Hash()) != nil {
		t.Fatal("cancelled target inserted")
	}
	syncer.Stop()
}

func TestExecutionSyncFinishesPivotWhileHeadAdvances(t *testing.T) {
	genesis := types.NewBlock(&types.Header{Number: big.NewInt(0)}, nil)
	pivot := types.NewBlock(&types.Header{ParentHash: genesis.Hash(), Number: big.NewInt(1)}, nil)
	head := types.NewBlock(&types.Header{ParentHash: pivot.Hash(), Number: big.NewInt(2)}, nil)
	chain := &executionTestChain{blocks: map[types.Hash]*types.Block{genesis.Hash(): genesis}}
	started, release := make(chan struct{}), make(chan struct{})
	applied := make(chan ExecutionTarget, 1)
	syncer := NewExecutionSyncer(chain, executionTestFetcher(func(_ context.Context, hash types.Hash) (*types.Block, error) {
		if hash == pivot.Hash() {
			return pivot, nil
		}
		return head, nil
	}), func(target ExecutionTarget) error { applied <- target; return nil })
	syncer.SetPivotSync(func(ctx context.Context, block *types.Block) (bool, error) {
		close(started)
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-release:
		}
		return true, chain.InsertBlock(block)
	})
	syncer.Start()
	defer syncer.Stop()
	syncer.Request(ExecutionTarget{Head: pivot.Hash(), Finalized: pivot.Hash()})
	<-started
	syncer.Request(ExecutionTarget{Head: head.Hash(), Finalized: pivot.Hash()})
	close(release)
	select {
	case target := <-applied:
		if target.Head != head.Hash() {
			t.Fatal("stale head applied")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("head update restarted finalized snapshot download")
	}
}

func TestExecutionSyncRejectsWrongHash(t *testing.T) {
	genesis := types.NewBlock(&types.Header{Number: big.NewInt(0)}, nil)
	chain := &executionTestChain{blocks: map[types.Hash]*types.Block{genesis.Hash(): genesis}}
	syncer := NewExecutionSyncer(chain, executionTestFetcher(func(context.Context, types.Hash) (*types.Block, error) { return genesis, nil }), nil)
	if err := syncer.synchronize(context.Background(), ExecutionTarget{Head: types.BytesToHash([]byte{1})}); err == nil {
		t.Fatal("wrong hash response accepted")
	}
}

func TestAdoptFinalizedAfterFinalityAdvance(t *testing.T) {
	genesis := types.NewBlock(&types.Header{Number: big.NewInt(0)}, nil)
	pivot := types.NewBlock(&types.Header{ParentHash: genesis.Hash(), Number: big.NewInt(1)}, nil)
	next := types.NewBlock(&types.Header{ParentHash: pivot.Hash(), Number: big.NewInt(2)}, nil)
	other := types.NewBlock(&types.Header{ParentHash: genesis.Hash(), Number: big.NewInt(1), Time: 2}, nil)
	chain := &executionTestChain{blocks: map[types.Hash]*types.Block{genesis.Hash(): genesis}}
	fetch := executionTestFetcher(func(_ context.Context, hash types.Hash) (*types.Block, error) {
		switch hash {
		case next.Hash():
			return next, nil
		case other.Hash():
			return other, nil
		default:
			return nil, nil
		}
	})
	syncer := NewExecutionSyncer(chain, fetch, nil)
	syncer.Request(ExecutionTarget{Head: next.Hash(), Finalized: next.Hash()})
	adopted := false
	if err := syncer.AdoptFinalized(context.Background(), pivot, func() error { adopted = true; return nil }); err != nil || !adopted {
		t.Fatalf("valid finalized advance not adopted: %v", err)
	}
	syncer.Request(ExecutionTarget{Head: other.Hash(), Finalized: other.Hash()})
	adopted = false
	if err := syncer.AdoptFinalized(context.Background(), pivot, func() error { adopted = true; return nil }); err == nil || adopted {
		t.Fatal("conflicting finalized chain adopted")
	}
}

func TestAdoptFinalizedRechecksTargetDuringAncestryFetch(t *testing.T) {
	genesis := types.NewBlock(&types.Header{Number: big.NewInt(0)}, nil)
	pivot := types.NewBlock(&types.Header{ParentHash: genesis.Hash(), Number: big.NewInt(1)}, nil)
	next := types.NewBlock(&types.Header{ParentHash: pivot.Hash(), Number: big.NewInt(2)}, nil)
	other := types.NewBlock(&types.Header{ParentHash: genesis.Hash(), Number: big.NewInt(1), Time: 2}, nil)
	chain := &executionTestChain{blocks: map[types.Hash]*types.Block{genesis.Hash(): genesis}}
	var syncer *ExecutionSyncer
	fetch := executionTestFetcher(func(_ context.Context, hash types.Hash) (*types.Block, error) {
		if hash == next.Hash() {
			syncer.Request(ExecutionTarget{Head: other.Hash(), Finalized: other.Hash()})
			return next, nil
		}
		if hash == other.Hash() {
			return other, nil
		}
		return nil, nil
	})
	syncer = NewExecutionSyncer(chain, fetch, nil)
	syncer.Request(ExecutionTarget{Head: next.Hash(), Finalized: next.Hash()})
	adopted := false
	if err := syncer.AdoptFinalized(context.Background(), pivot, func() error { adopted = true; return nil }); err == nil || adopted {
		t.Fatal("adopted snapshot after finalized target changed to conflicting chain")
	}
}

func TestAdoptFinalizedRejectsNoncontiguousAncestry(t *testing.T) {
	pivot := types.NewBlock(&types.Header{Number: big.NewInt(4)}, nil)
	bad := types.NewBlock(&types.Header{ParentHash: pivot.Hash(), Number: big.NewInt(7)}, nil)
	chain := &executionTestChain{blocks: map[types.Hash]*types.Block{bad.Hash(): bad}}
	syncer := NewExecutionSyncer(chain, nil, nil)
	syncer.Request(ExecutionTarget{Head: bad.Hash(), Finalized: bad.Hash()})
	adopted := false
	if err := syncer.AdoptFinalized(context.Background(), pivot, func() error { adopted = true; return nil }); err == nil || adopted {
		t.Fatal("noncontiguous finalized ancestry adopted")
	}
}
