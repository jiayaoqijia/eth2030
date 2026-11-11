package consensus

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// testRandaoReveal returns a non-zero 96-byte RANDAO reveal for testing.
func testRandaoReveal() [96]byte {
	var r [96]byte
	for i := range r {
		r[i] = byte(i + 1)
	}
	return r
}

// testParentRoot returns a non-zero parent root for testing.
func testParentRoot() types.Hash {
	var h types.Hash
	h[0] = 0xAA
	h[31] = 0xBB
	return h
}

func TestBlockProducerBasic(t *testing.T) {
	bp := NewBlockProducer(nil, nil)
	if bp == nil {
		t.Fatal("NewBlockProducer returned nil")
	}

	block, err := bp.ProduceBlock(1, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if block.Slot != 1 {
		t.Fatalf("expected slot 1, got %d", block.Slot)
	}
	if block.ProposerIndex != 0 {
		t.Fatalf("expected proposer 0, got %d", block.ProposerIndex)
	}
	if block.ParentRoot != testParentRoot() {
		t.Fatal("parent root mismatch")
	}
	if block.Body == nil {
		t.Fatal("block body is nil")
	}
	if block.Body.SyncAggregate == nil {
		t.Fatal("sync aggregate is nil")
	}
	if block.Body.SignedExecutionPayloadBid == nil {
		t.Fatal("signed execution payload bid is nil")
	}
	if block.Body.SignedExecutionPayloadBid.Message.BuilderIndex != BuilderIndexSelfBuild {
		t.Fatalf("expected self-build bid, got builder index %d",
			block.Body.SignedExecutionPayloadBid.Message.BuilderIndex)
	}
}

func TestBlockProducerEmptyRandao(t *testing.T) {
	bp := NewBlockProducer(nil, nil)
	_, err := bp.ProduceBlock(1, 0, testParentRoot(), types.Hash{}, [96]byte{})
	if err != ErrBPEmptyRandao {
		t.Fatalf("expected ErrBPEmptyRandao, got %v", err)
	}
}

func TestBlockProducerInvalidSlot(t *testing.T) {
	bp := NewBlockProducer(nil, nil)
	_, err := bp.ProduceBlock(0, 0, testParentRoot(), types.Hash{}, testRandaoReveal())
	if err != ErrBPInvalidBlockSlot {
		t.Fatalf("expected ErrBPInvalidBlockSlot, got %v", err)
	}
}

func TestBlockProducerInvalidParent(t *testing.T) {
	bp := NewBlockProducer(nil, nil)
	_, err := bp.ProduceBlock(1, 0, types.Hash{}, types.Hash{}, testRandaoReveal())
	if err != ErrBPInvalidParent {
		t.Fatalf("expected ErrBPInvalidParent, got %v", err)
	}
}

func TestBlockProducerWithGraffiti(t *testing.T) {
	graffiti := [GraffitiLength]byte{}
	copy(graffiti[:], "ETH2030-test-graffiti")
	cfg := &BlockProducerConfig{Graffiti: graffiti}
	bp := NewBlockProducer(cfg, nil)

	block, err := bp.ProduceBlock(5, 2, testParentRoot(), types.Hash{0x02}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if block.Body.Graffiti != graffiti {
		t.Fatal("graffiti mismatch")
	}
}

func TestBlockProducerSetGraffiti(t *testing.T) {
	bp := NewBlockProducer(nil, nil)
	newGraffiti := [GraffitiLength]byte{}
	copy(newGraffiti[:], "updated-graffiti")
	bp.SetGraffiti(newGraffiti)

	block, err := bp.ProduceBlock(1, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if block.Body.Graffiti != newGraffiti {
		t.Fatal("graffiti not updated")
	}
}

func TestBlockProducerWithAttestations(t *testing.T) {
	pool := NewAttestationPool(nil)
	pool.SetCurrentSlot(10)

	// Add an attestation for slot 8 (satisfies min inclusion delay of 1).
	att := &PoolAttestation{
		Slot:            8,
		CommitteeIndex:  0,
		AggregationBits: []byte{0xFF},
		BeaconBlockRoot: types.Hash{0x01},
		Source:          Checkpoint{Epoch: 0},
		Target:          Checkpoint{Epoch: 0},
	}
	if err := pool.Add(att); err != nil {
		t.Fatalf("pool.Add failed: %v", err)
	}

	pools := &OperationPools{Attestations: pool}
	bp := NewBlockProducer(nil, pools)

	block, err := bp.ProduceBlock(10, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if len(block.Body.Attestations) == 0 {
		t.Fatal("expected attestations in block")
	}
}

func TestBlockProducerWithSlashings(t *testing.T) {
	slashings := make([]ProposerSlashing, 3)
	for i := range slashings {
		slashings[i] = ProposerSlashing{ProposerIndex: ValidatorIndex(i)}
	}

	pools := &OperationPools{ProposerSlashings: slashings}
	bp := NewBlockProducer(nil, pools)

	block, err := bp.ProduceBlock(1, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if len(block.Body.ProposerSlashings) != 3 {
		t.Fatalf("expected 3 proposer slashings, got %d", len(block.Body.ProposerSlashings))
	}
}

func TestBlockProducerSlashingsMaxCap(t *testing.T) {
	// Create more than MaxProposerSlashings.
	slashings := make([]ProposerSlashing, MaxProposerSlashings+5)
	for i := range slashings {
		slashings[i] = ProposerSlashing{ProposerIndex: ValidatorIndex(i)}
	}

	pools := &OperationPools{ProposerSlashings: slashings}
	bp := NewBlockProducer(nil, pools)

	block, err := bp.ProduceBlock(1, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if len(block.Body.ProposerSlashings) != MaxProposerSlashings {
		t.Fatalf("expected %d proposer slashings, got %d", MaxProposerSlashings, len(block.Body.ProposerSlashings))
	}
}

func TestBlockProducerWithDeposits(t *testing.T) {
	deposits := []Deposit{
		{Amount: 32_000_000_000},
		{Amount: 64_000_000_000},
	}
	pools := &OperationPools{Deposits: deposits}
	bp := NewBlockProducer(nil, pools)

	block, err := bp.ProduceBlock(1, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if len(block.Body.Deposits) != 2 {
		t.Fatalf("expected 2 deposits, got %d", len(block.Body.Deposits))
	}
}

func TestBlockProducerWithVoluntaryExits(t *testing.T) {
	exits := []VoluntaryExit{
		{Epoch: 10, ValidatorIndex: 5},
	}
	pools := &OperationPools{VoluntaryExits: exits}
	bp := NewBlockProducer(nil, pools)

	block, err := bp.ProduceBlock(1, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if len(block.Body.VoluntaryExits) != 1 {
		t.Fatalf("expected 1 voluntary exit, got %d", len(block.Body.VoluntaryExits))
	}
}

func TestBlockProducerWithSyncAggregate(t *testing.T) {
	agg := &SyncAggregate{
		SyncCommitteeBits: [SyncAggregateBitfieldSize]byte{0xFF},
	}
	pools := &OperationPools{SyncAggregate: agg}
	bp := NewBlockProducer(nil, pools)

	block, err := bp.ProduceBlock(1, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if block.Body.SyncAggregate.SyncCommitteeBits[0] != 0xFF {
		t.Fatal("sync aggregate bits not preserved")
	}
}

func TestBlockProducerWithExecutionBid(t *testing.T) {
	bid := &SignedExecutionPayloadBid{
		Message: ExecutionPayloadBid{
			BlockHash: types.Hash{0xDE, 0xAD},
			GasLimit:  30_000_000,
			Slot:      1,
		},
	}
	cfg := &BlockProducerConfig{DefaultBid: bid}
	bp := NewBlockProducer(cfg, nil)

	block, err := bp.ProduceBlock(1, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if block.Body.SignedExecutionPayloadBid == nil {
		t.Fatal("signed execution payload bid is nil")
	}
	if block.Body.SignedExecutionPayloadBid.Message.GasLimit != 30_000_000 {
		t.Fatalf("expected gas limit 30M, got %d",
			block.Body.SignedExecutionPayloadBid.Message.GasLimit)
	}
	if block.Body.SignedExecutionPayloadBid.Message.BlockHash != (types.Hash{0xDE, 0xAD}) {
		t.Fatal("block hash mismatch")
	}
}

func TestBlockProducerPayloadAttestationsCap(t *testing.T) {
	atts := make([]*PayloadAttestation, MaxPayloadAttestations+3)
	for i := range atts {
		atts[i] = &PayloadAttestation{Data: PayloadAttestationData{Slot: uint64(i)}}
	}
	pools := &OperationPools{PayloadAttestations: atts}
	bp := NewBlockProducer(nil, pools)

	block, err := bp.ProduceBlock(1, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if len(block.Body.PayloadAttestations) != MaxPayloadAttestations {
		t.Fatalf("expected %d payload attestations, got %d",
			MaxPayloadAttestations, len(block.Body.PayloadAttestations))
	}
}

func TestBlockRoot2Deterministic(t *testing.T) {
	bp := NewBlockProducer(nil, nil)
	block1, err := bp.ProduceBlock(5, 3, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	block2, err := bp.ProduceBlock(5, 3, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}

	root1 := BlockRoot2(block1)
	root2 := BlockRoot2(block2)
	if root1 != root2 {
		t.Fatal("BlockRoot2 is not deterministic for identical blocks")
	}

	// Different slot should yield different root.
	block3, err := bp.ProduceBlock(6, 3, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	root3 := BlockRoot2(block3)
	if root1 == root3 {
		t.Fatal("different slots should produce different roots")
	}
}

func TestBlockProducerEth1Data(t *testing.T) {
	eth1 := Eth1Data{
		DepositRoot:  types.Hash{0x11},
		DepositCount: 42,
		BlockHash:    types.Hash{0x22},
	}
	cfg := &BlockProducerConfig{Eth1Data: eth1}
	bp := NewBlockProducer(cfg, nil)

	block, err := bp.ProduceBlock(1, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if block.Body.Eth1Data.DepositCount != 42 {
		t.Fatalf("expected deposit count 42, got %d", block.Body.Eth1Data.DepositCount)
	}
	if block.Body.Eth1Data.DepositRoot != eth1.DepositRoot {
		t.Fatal("deposit root mismatch")
	}
}

func TestBlockProducerThreadSafety(t *testing.T) {
	bp := NewBlockProducer(nil, nil)
	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(slot Slot) {
			defer wg.Done()
			_, err := bp.ProduceBlock(slot, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
			if err != nil {
				errs <- err
			}
		}(Slot(i + 1))
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent ProduceBlock failed: %v", err)
	}
}

func TestBlockProducerSetPools(t *testing.T) {
	bp := NewBlockProducer(nil, nil)

	// Initially no attestations.
	block, err := bp.ProduceBlock(10, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if len(block.Body.Attestations) != 0 {
		t.Fatal("expected no attestations before setting pools")
	}

	// Set pools with attestations.
	pool := NewAttestationPool(nil)
	pool.SetCurrentSlot(10)
	att := &PoolAttestation{
		Slot:            8,
		AggregationBits: []byte{0x01},
		Source:          Checkpoint{Epoch: 0},
		Target:          Checkpoint{Epoch: 0},
	}
	_ = pool.Add(att)

	bp.SetPools(&OperationPools{Attestations: pool})

	block, err = bp.ProduceBlock(10, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if len(block.Body.Attestations) == 0 {
		t.Fatal("expected attestations after setting pools")
	}
}

func TestBlockProducerAttesterSlashingsCap(t *testing.T) {
	slashings := make([]AttesterSlashing, MaxAttesterSlashings+3)
	pools := &OperationPools{AttesterSlashings: slashings}
	bp := NewBlockProducer(nil, pools)

	block, err := bp.ProduceBlock(1, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if len(block.Body.AttesterSlashings) != MaxAttesterSlashings {
		t.Fatalf("expected %d attester slashings, got %d", MaxAttesterSlashings, len(block.Body.AttesterSlashings))
	}
}

func TestBlockProducerDepositsCap(t *testing.T) {
	deposits := make([]Deposit, MaxDepositsPerBlock+5)
	pools := &OperationPools{Deposits: deposits}
	bp := NewBlockProducer(nil, pools)

	block, err := bp.ProduceBlock(1, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if len(block.Body.Deposits) != MaxDepositsPerBlock {
		t.Fatalf("expected %d deposits, got %d", MaxDepositsPerBlock, len(block.Body.Deposits))
	}
}

func TestBlockProducerVoluntaryExitsCap(t *testing.T) {
	exits := make([]VoluntaryExit, MaxVoluntaryExits+5)
	pools := &OperationPools{VoluntaryExits: exits}
	bp := NewBlockProducer(nil, pools)

	block, err := bp.ProduceBlock(1, 0, testParentRoot(), types.Hash{0x01}, testRandaoReveal())
	if err != nil {
		t.Fatalf("ProduceBlock failed: %v", err)
	}
	if len(block.Body.VoluntaryExits) != MaxVoluntaryExits {
		t.Fatalf("expected %d voluntary exits, got %d", MaxVoluntaryExits, len(block.Body.VoluntaryExits))
	}
}

func TestValidateBlock(t *testing.T) {
	// Valid block.
	block := &BeaconBlockV2{
		Slot:       1,
		ParentRoot: testParentRoot(),
		Body:       &BeaconBlockBody{},
	}
	if err := validateBlock(block); err != nil {
		t.Fatalf("validateBlock failed for valid block: %v", err)
	}

	// Invalid: zero slot.
	badSlot := &BeaconBlockV2{
		Slot:       0,
		ParentRoot: testParentRoot(),
		Body:       &BeaconBlockBody{},
	}
	if err := validateBlock(badSlot); err != ErrBPInvalidBlockSlot {
		t.Fatalf("expected ErrBPInvalidBlockSlot, got %v", err)
	}

	// Invalid: empty parent.
	badParent := &BeaconBlockV2{
		Slot: 1,
		Body: &BeaconBlockBody{},
	}
	if err := validateBlock(badParent); err != ErrBPInvalidParent {
		t.Fatalf("expected ErrBPInvalidParent, got %v", err)
	}

	// Invalid: nil body.
	noBody := &BeaconBlockV2{
		Slot:       1,
		ParentRoot: testParentRoot(),
	}
	if err := validateBlock(noBody); err != ErrBPInvalidBody {
		t.Fatalf("expected ErrBPInvalidBody, got %v", err)
	}
}

func TestComputeBodyRoot(t *testing.T) {
	// Nil body returns zero hash.
	root := computeBodyRoot(nil)
	if root != (types.Hash{}) {
		t.Fatal("expected zero hash for nil body")
	}

	// Non-nil body returns non-zero hash.
	body := &BeaconBlockBody{
		RandaoReveal: testRandaoReveal(),
	}
	root = computeBodyRoot(body)
	if root == (types.Hash{}) {
		t.Fatal("expected non-zero hash for non-nil body")
	}

	// Deterministic.
	root2 := computeBodyRoot(body)
	if root != root2 {
		t.Fatal("computeBodyRoot not deterministic")
	}
}
