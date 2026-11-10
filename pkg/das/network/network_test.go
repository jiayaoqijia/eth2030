package network

import (
	"bytes"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/das/dastypes"
)

// --- DASNetworkConfig ---

func TestDefaultDASNetworkConfig(t *testing.T) {
	cfg := DefaultDASNetworkConfig()
	if cfg.NumSubnets != dastypes.DataColumnSidecarSubnetCount {
		t.Fatalf("NumSubnets = %d, want %d", cfg.NumSubnets, dastypes.DataColumnSidecarSubnetCount)
	}
	if cfg.SamplesPerSlot != dastypes.SamplesPerSlot {
		t.Fatalf("SamplesPerSlot = %d, want %d", cfg.SamplesPerSlot, dastypes.SamplesPerSlot)
	}
	if cfg.MinCustodySubnets != dastypes.CustodyRequirement {
		t.Fatalf("MinCustodySubnets = %d, want %d", cfg.MinCustodySubnets, dastypes.CustodyRequirement)
	}
	if cfg.ColumnSize != dastypes.BytesPerCell {
		t.Fatalf("ColumnSize = %d, want %d", cfg.ColumnSize, dastypes.BytesPerCell)
	}
}

// --- DASNetwork lifecycle ---

func TestDASNetworkStartStop(t *testing.T) {
	dn := NewDASNetwork(DefaultDASNetworkConfig())
	if dn.isStarted() {
		t.Fatal("should not be started initially")
	}
	dn.Start()
	if !dn.isStarted() {
		t.Fatal("should be started after Start()")
	}
	dn.Stop()
	if dn.isStarted() {
		t.Fatal("should not be started after Stop()")
	}
}

func TestDASNetworkNotStarted(t *testing.T) {
	dn := NewDASNetwork(DefaultDASNetworkConfig())

	_, err := dn.RequestSamples(0, []uint64{0})
	if err != ErrDASNotStarted {
		t.Fatalf("RequestSamples: got %v, want ErrDASNotStarted", err)
	}
	_, err = dn.ServeSample(0, 0)
	if err != ErrDASNotStarted {
		t.Fatalf("ServeSample: got %v, want ErrDASNotStarted", err)
	}
}

// --- StoreSample ---

func TestStoreSample(t *testing.T) {
	dn := NewDASNetwork(DefaultDASNetworkConfig())
	dn.Start()

	sample := &SampleResponse{
		BlobIndex: 0,
		CellIndex: 5,
		Data:      []byte{0x01, 0x02, 0x03},
		Proof:     []byte{0xaa},
	}
	if err := dn.StoreSample(sample); err != nil {
		t.Fatalf("StoreSample: %v", err)
	}
}

func TestStoreSampleErrors(t *testing.T) {
	dn := NewDASNetwork(DefaultDASNetworkConfig())

	// Nil sample.
	if err := dn.StoreSample(nil); err != ErrInvalidSampleData {
		t.Fatalf("nil: got %v, want ErrInvalidSampleData", err)
	}

	// Empty data.
	if err := dn.StoreSample(&SampleResponse{Data: nil}); err != ErrInvalidSampleData {
		t.Fatalf("empty data: got %v, want ErrInvalidSampleData", err)
	}

	// Invalid blob index.
	if err := dn.StoreSample(&SampleResponse{
		BlobIndex: dastypes.MaxBlobCommitmentsPerBlock,
		CellIndex: 0,
		Data:      []byte{1},
	}); err == nil {
		t.Fatal("expected error for invalid blob index")
	}

	// Invalid cell index.
	if err := dn.StoreSample(&SampleResponse{
		BlobIndex: 0,
		CellIndex: dastypes.NumberOfColumns,
		Data:      []byte{1},
	}); err == nil {
		t.Fatal("expected error for invalid cell index")
	}
}

// --- RequestSamples ---

func TestRequestSamples(t *testing.T) {
	dn := NewDASNetwork(DefaultDASNetworkConfig())
	dn.Start()

	// Store some samples.
	for i := uint64(0); i < 5; i++ {
		dn.StoreSample(&SampleResponse{
			BlobIndex: 0,
			CellIndex: i,
			Data:      []byte{byte(i)},
			Proof:     []byte{byte(i + 100)},
		})
	}

	// Request existing samples.
	results, err := dn.RequestSamples(0, []uint64{0, 2, 4})
	if err != nil {
		t.Fatalf("RequestSamples: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	// Request mix of existing and missing.
	results, err = dn.RequestSamples(0, []uint64{0, 10, 20})
	if err != nil {
		t.Fatalf("RequestSamples: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1 (only index 0 exists)", len(results))
	}
}

func TestRequestSamplesInvalidBlobIndex(t *testing.T) {
	dn := NewDASNetwork(DefaultDASNetworkConfig())
	dn.Start()

	_, err := dn.RequestSamples(dastypes.MaxBlobCommitmentsPerBlock, []uint64{0})
	if err == nil {
		t.Fatal("expected error for invalid blob index")
	}
}

func TestRequestSamplesInvalidCellIndex(t *testing.T) {
	dn := NewDASNetwork(DefaultDASNetworkConfig())
	dn.Start()

	_, err := dn.RequestSamples(0, []uint64{dastypes.NumberOfColumns})
	if err == nil {
		t.Fatal("expected error for invalid cell index")
	}
}

func TestRequestSamplesEmptyIndices(t *testing.T) {
	dn := NewDASNetwork(DefaultDASNetworkConfig())
	dn.Start()

	results, err := dn.RequestSamples(0, nil)
	if err != nil {
		t.Fatalf("RequestSamples (empty): %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("got %d results, want 0", len(results))
	}
}

// --- ServeSample ---

func TestServeSample(t *testing.T) {
	dn := NewDASNetwork(DefaultDASNetworkConfig())
	dn.Start()

	data := []byte{0xde, 0xad}
	dn.StoreSample(&SampleResponse{
		BlobIndex: 1,
		CellIndex: 10,
		Data:      data,
		Proof:     []byte{0xff},
	})

	sample, err := dn.ServeSample(1, 10)
	if err != nil {
		t.Fatalf("ServeSample: %v", err)
	}
	if !bytes.Equal(sample.Data, data) {
		t.Fatal("data mismatch")
	}
}

func TestServeSampleNotAvailable(t *testing.T) {
	dn := NewDASNetwork(DefaultDASNetworkConfig())
	dn.Start()

	_, err := dn.ServeSample(0, 0)
	if err != ErrSampleNotAvailable {
		t.Fatalf("got %v, want ErrSampleNotAvailable", err)
	}
}

func TestServeSampleInvalidIndices(t *testing.T) {
	dn := NewDASNetwork(DefaultDASNetworkConfig())
	dn.Start()

	if _, err := dn.ServeSample(dastypes.MaxBlobCommitmentsPerBlock, 0); err == nil {
		t.Fatal("expected error for invalid blob index")
	}
	if _, err := dn.ServeSample(0, dastypes.NumberOfColumns); err == nil {
		t.Fatal("expected error for invalid cell index")
	}
}

// --- VerifySample ---

func TestVerifySampleValid(t *testing.T) {
	commitment := []byte("test commitment")
	data := []byte{0x01, 0x02, 0x03}
	prf := ComputeSampleProof(commitment, 0, 5, data)

	sample := &SampleResponse{
		BlobIndex: 0,
		CellIndex: 5,
		Data:      data,
		Proof:     prf,
	}

	if !VerifySample(sample, commitment) {
		t.Fatal("valid sample should verify")
	}
}

func TestVerifySampleInvalid(t *testing.T) {
	commitment := []byte("test commitment")
	data := []byte{0x01, 0x02, 0x03}
	prf := ComputeSampleProof(commitment, 0, 5, data)

	// Wrong data.
	badSample := &SampleResponse{
		BlobIndex: 0,
		CellIndex: 5,
		Data:      []byte{0xff},
		Proof:     prf,
	}
	if VerifySample(badSample, commitment) {
		t.Fatal("tampered data should not verify")
	}

	// Wrong commitment.
	goodSample := &SampleResponse{
		BlobIndex: 0,
		CellIndex: 5,
		Data:      data,
		Proof:     prf,
	}
	if VerifySample(goodSample, []byte("wrong commitment")) {
		t.Fatal("wrong commitment should not verify")
	}

	// Wrong cell index (proof was computed for cellIndex=5).
	wrongIdx := &SampleResponse{
		BlobIndex: 0,
		CellIndex: 99,
		Data:      data,
		Proof:     prf,
	}
	if VerifySample(wrongIdx, commitment) {
		t.Fatal("wrong cell index should not verify")
	}
}

func TestVerifySampleEdgeCases(t *testing.T) {
	// Nil sample.
	if VerifySample(nil, []byte("c")) {
		t.Fatal("nil sample should not verify")
	}
	// Empty data.
	if VerifySample(&SampleResponse{Proof: []byte{1}}, []byte("c")) {
		t.Fatal("empty data should not verify")
	}
	// Empty commitment.
	if VerifySample(&SampleResponse{Data: []byte{1}, Proof: []byte{1}}, nil) {
		t.Fatal("empty commitment should not verify")
	}
	// Empty proof.
	if VerifySample(&SampleResponse{Data: []byte{1}}, []byte("c")) {
		t.Fatal("empty proof should not verify")
	}
}

// --- CustodySubnet ---

func TestAssignCustody(t *testing.T) {
	nodeID := types.HexToHash("0xabcdef01")
	custody := AssignCustody(nodeID, dastypes.CustodyRequirement)

	if custody.NodeID != nodeID {
		t.Fatal("node ID mismatch")
	}
	if len(custody.SubnetIDs) != int(dastypes.CustodyRequirement) {
		t.Fatalf("got %d subnets, want %d", len(custody.SubnetIDs), dastypes.CustodyRequirement)
	}
	if custody.NumSubnets != dastypes.DataColumnSidecarSubnetCount {
		t.Fatalf("NumSubnets = %d, want %d", custody.NumSubnets, dastypes.DataColumnSidecarSubnetCount)
	}

	// All subnets in valid range.
	for _, s := range custody.SubnetIDs {
		if s >= dastypes.DataColumnSidecarSubnetCount {
			t.Fatalf("subnet %d out of range", s)
		}
	}

	// No duplicates.
	seen := make(map[uint64]bool)
	for _, s := range custody.SubnetIDs {
		if seen[s] {
			t.Fatalf("duplicate subnet %d", s)
		}
		seen[s] = true
	}
}

func TestAssignCustodyDeterministic(t *testing.T) {
	nodeID := types.HexToHash("0x1234")
	c1 := AssignCustody(nodeID, dastypes.CustodyRequirement)
	c2 := AssignCustody(nodeID, dastypes.CustodyRequirement)

	if len(c1.SubnetIDs) != len(c2.SubnetIDs) {
		t.Fatal("non-deterministic lengths")
	}
	for i := range c1.SubnetIDs {
		if c1.SubnetIDs[i] != c2.SubnetIDs[i] {
			t.Fatalf("non-deterministic: c1[%d]=%d != c2[%d]=%d",
				i, c1.SubnetIDs[i], i, c2.SubnetIDs[i])
		}
	}
}

func TestAssignCustodyDifferentNodes(t *testing.T) {
	id1 := types.HexToHash("0xaa")
	id2 := types.HexToHash("0xbb")

	c1 := AssignCustody(id1, dastypes.CustodyRequirement)
	c2 := AssignCustody(id2, dastypes.CustodyRequirement)

	allSame := true
	for i := range c1.SubnetIDs {
		if c1.SubnetIDs[i] != c2.SubnetIDs[i] {
			allSame = false
			break
		}
	}
	if allSame {
		t.Error("fixed node IDs produced identical subnets")
	}
}

func TestAssignCustodyMinSubnets(t *testing.T) {
	nodeID := types.HexToHash("0x42")
	// Request fewer than CustodyRequirement; should be clamped.
	custody := AssignCustody(nodeID, 1)
	if len(custody.SubnetIDs) < int(dastypes.CustodyRequirement) {
		t.Fatalf("got %d subnets, min should be %d", len(custody.SubnetIDs), dastypes.CustodyRequirement)
	}
}

func TestAssignCustodyMaxSubnets(t *testing.T) {
	nodeID := types.HexToHash("0x99")
	// Request more than the total; should be clamped.
	custody := AssignCustody(nodeID, 1000)
	if uint64(len(custody.SubnetIDs)) > dastypes.DataColumnSidecarSubnetCount {
		t.Fatalf("got %d subnets, max should be %d", len(custody.SubnetIDs), dastypes.DataColumnSidecarSubnetCount)
	}
}

func TestCustodySubnetContains(t *testing.T) {
	cs := &CustodySubnet{
		SubnetIDs:  []uint64{3, 10, 42},
		NumSubnets: 64,
	}
	if !cs.Contains(3) {
		t.Fatal("should contain subnet 3")
	}
	if !cs.Contains(42) {
		t.Fatal("should contain subnet 42")
	}
	if cs.Contains(5) {
		t.Fatal("should not contain subnet 5")
	}
}

// --- IsCustodian ---

func TestIsCustodian(t *testing.T) {
	nodeID := types.HexToHash("0xdeadbeef")
	custody := AssignCustody(nodeID, dastypes.CustodyRequirement)

	// A subnet in the custody set should return true.
	if len(custody.SubnetIDs) == 0 {
		t.Fatal("no subnets assigned")
	}
	assignedSubnet := custody.SubnetIDs[0]
	if !IsCustodian(nodeID, assignedSubnet) {
		t.Fatalf("node should be custodian for subnet %d", assignedSubnet)
	}

	// Find a subnet NOT in the custody set.
	assigned := make(map[uint64]bool)
	for _, s := range custody.SubnetIDs {
		assigned[s] = true
	}
	for i := uint64(0); i < dastypes.DataColumnSidecarSubnetCount; i++ {
		if !assigned[i] {
			if IsCustodian(nodeID, i) {
				t.Fatalf("node should NOT be custodian for subnet %d", i)
			}
			break
		}
	}
}

// --- ColumnReconstructor ---

func TestColumnReconstructorBasic(t *testing.T) {
	cr := NewColumnReconstructor(3) // need 3 fragments

	// Add fragments; first two should not be ready.
	if cr.AddFragment(0, []byte{0x01, 0x02}) {
		t.Fatal("should not be ready with 1 fragment")
	}
	if cr.AddFragment(1, []byte{0x03, 0x04}) {
		t.Fatal("should not be ready with 2 fragments")
	}
	if cr.FragmentCount() != 2 {
		t.Fatalf("fragment count = %d, want 2", cr.FragmentCount())
	}

	// Third fragment completes.
	if !cr.AddFragment(2, []byte{0x05, 0x06}) {
		t.Fatal("should be ready with 3 fragments")
	}
	if !cr.CanReconstruct() {
		t.Fatal("should be able to reconstruct")
	}

	data, err := cr.Reconstruct()
	if err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}
	// Expect 3 fragments * 2 bytes each = 6 bytes.
	if len(data) != 6 {
		t.Fatalf("reconstructed len = %d, want 6", len(data))
	}

	expected := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	if !bytes.Equal(data, expected) {
		t.Fatalf("reconstructed data = %x, want %x", data, expected)
	}
}

func TestColumnReconstructorNotReady(t *testing.T) {
	cr := NewColumnReconstructor(5)
	cr.AddFragment(0, []byte{0x01})

	_, err := cr.Reconstruct()
	if err == nil {
		t.Fatal("expected error when not enough fragments")
	}
}

func TestColumnReconstructorAlreadyComplete(t *testing.T) {
	cr := NewColumnReconstructor(1)
	cr.AddFragment(0, []byte{0x01})

	_, err := cr.Reconstruct()
	if err != nil {
		t.Fatalf("first Reconstruct: %v", err)
	}

	_, err = cr.Reconstruct()
	if err != ErrReconstructDone {
		t.Fatalf("second Reconstruct: got %v, want ErrReconstructDone", err)
	}
}

func TestColumnReconstructorReset(t *testing.T) {
	cr := NewColumnReconstructor(1)
	cr.AddFragment(0, []byte{0xaa})
	cr.Reconstruct()

	cr.Reset()
	if cr.FragmentCount() != 0 {
		t.Fatal("fragment count should be 0 after reset")
	}
	if cr.CanReconstruct() {
		t.Fatal("should not be able to reconstruct after reset")
	}

	// Should work again after reset.
	cr.AddFragment(0, []byte{0xbb})
	data, err := cr.Reconstruct()
	if err != nil {
		t.Fatalf("Reconstruct after reset: %v", err)
	}
	if !bytes.Equal(data, []byte{0xbb}) {
		t.Fatalf("data = %x, want bb", data)
	}
}

func TestColumnReconstructorOverwriteFragment(t *testing.T) {
	cr := NewColumnReconstructor(2)
	cr.AddFragment(0, []byte{0x01})
	cr.AddFragment(0, []byte{0x02}) // overwrite
	if cr.FragmentCount() != 1 {
		t.Fatalf("fragment count = %d, want 1 (overwrite)", cr.FragmentCount())
	}

	cr.AddFragment(1, []byte{0x03})
	data, err := cr.Reconstruct()
	if err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}
	// Fragment 0 was overwritten with 0x02.
	if data[0] != 0x02 {
		t.Fatalf("fragment 0 = 0x%02x, want 0x02 (overwritten)", data[0])
	}
}

func TestColumnReconstructorSparseIndices(t *testing.T) {
	cr := NewColumnReconstructor(2)
	cr.AddFragment(0, []byte{0xaa})
	cr.AddFragment(5, []byte{0xbb})

	data, err := cr.Reconstruct()
	if err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}

	// Output should span indices 0..5, each fragment is 1 byte.
	if len(data) != 6 {
		t.Fatalf("len = %d, want 6", len(data))
	}
	if data[0] != 0xaa {
		t.Fatalf("data[0] = 0x%02x, want 0xaa", data[0])
	}
	if data[5] != 0xbb {
		t.Fatalf("data[5] = 0x%02x, want 0xbb", data[5])
	}
	// Gaps should be zero.
	for i := 1; i < 5; i++ {
		if data[i] != 0 {
			t.Fatalf("data[%d] = 0x%02x, want 0x00", i, data[i])
		}
	}
}

// --- End-to-end: store, request, verify ---

func TestStoreAndRequestSamples(t *testing.T) {
	dn := NewDASNetwork(DefaultDASNetworkConfig())
	dn.Start()
	defer dn.Stop()

	commitment := []byte("blob commitment")
	for i := uint64(0); i < 3; i++ {
		data := []byte{byte(i * 10), byte(i*10 + 1)}
		prf := ComputeSampleProof(commitment, 0, i, data)
		dn.StoreSample(&SampleResponse{
			BlobIndex: 0,
			CellIndex: i,
			Data:      data,
			Proof:     prf,
		})
	}

	results, err := dn.RequestSamples(0, []uint64{0, 1, 2})
	if err != nil {
		t.Fatalf("RequestSamples: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	// Verify each sample.
	for _, sample := range results {
		if !VerifySample(sample, commitment) {
			t.Fatalf("sample (blob=%d, cell=%d) failed verification",
				sample.BlobIndex, sample.CellIndex)
		}
	}
}

// --- Concurrency safety ---

func TestDASNetworkConcurrentAccess(t *testing.T) {
	dn := NewDASNetwork(DefaultDASNetworkConfig())
	dn.Start()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cellIdx := uint64(idx % int(dastypes.NumberOfColumns))
			_ = dn.StoreSample(&SampleResponse{
				BlobIndex: 0,
				CellIndex: cellIdx,
				Data:      []byte{byte(idx)},
				Proof:     []byte{byte(idx)},
			})
			_, _ = dn.ServeSample(0, cellIdx)
			_, _ = dn.RequestSamples(0, []uint64{cellIdx})
		}(i)
	}
	wg.Wait()
}

func TestColumnReconstructorConcurrentAdd(t *testing.T) {
	cr := NewColumnReconstructor(50)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cr.AddFragment(uint64(idx), []byte{byte(idx)})
		}(i)
	}
	wg.Wait()

	if !cr.CanReconstruct() {
		t.Fatal("should be able to reconstruct with 100 fragments (threshold 50)")
	}
}

func TestCustodyConcurrentAssign(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			nodeID := types.BytesToHash([]byte{byte(idx)})
			custody := AssignCustody(nodeID, dastypes.CustodyRequirement)
			if len(custody.SubnetIDs) < int(dastypes.CustodyRequirement) {
				t.Errorf("node %d got %d subnets", idx, len(custody.SubnetIDs))
			}
		}(i)
	}
	wg.Wait()
}

// --- Config accessor ---

func TestDASNetworkConfig(t *testing.T) {
	cfg := DASNetworkConfig{
		NumSubnets:        32,
		SamplesPerSlot:    16,
		MinCustodySubnets: 8,
		ColumnSize:        4096,
	}
	dn := NewDASNetwork(cfg)
	got := dn.Config()
	if got.NumSubnets != 32 || got.SamplesPerSlot != 16 ||
		got.MinCustodySubnets != 8 || got.ColumnSize != 4096 {
		t.Fatal("Config() does not match constructor input")
	}
}
