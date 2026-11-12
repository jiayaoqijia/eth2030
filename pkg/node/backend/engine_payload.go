package backend

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/eth2030/eth2030/bal"
	"github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/gas"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto/bls"
	"github.com/eth2030/eth2030/engine/payload"
	enginepayload "github.com/eth2030/eth2030/engine/payload"
)

// GetPayloadByID retrieves a previously requested payload by its ID.
func (b *EngineBackend) GetPayloadByID(id payload.PayloadID) (*payload.GetPayloadResponse, error) {
	p, err := b.awaitPayload(id)
	if err != nil {
		return nil, err
	}
	return b.payloadResponse(id, p)
}

func (b *EngineBackend) awaitPayload(id payload.PayloadID) (*pendingPayload, error) {
	slog.Debug("engine_getPayload", "payloadID", id)

	b.mu.Lock()
	p, ok := b.payloads[id]
	b.mu.Unlock()

	if !ok {
		slog.Warn("engine_getPayload: payload not found", "payloadID", id)
		return nil, fmt.Errorf("payload %v not found", id)
	}

	select {
	case <-p.done:
	case <-time.After(8 * time.Second):
		slog.Warn("engine_getPayload: build timed out", "payloadID", id)
		return nil, fmt.Errorf("payload %v build timed out", id)
	}

	if p.err != nil {
		return nil, p.err
	}

	return p, nil
}

func (b *EngineBackend) payloadResponse(id payload.PayloadID, p *pendingPayload) (*payload.GetPayloadResponse, error) {
	blk := p.block
	header := blk.Header()

	var blobGasUsed, excessBlobGas uint64
	if header.BlobGasUsed != nil {
		blobGasUsed = *header.BlobGasUsed
	}
	if header.ExcessBlobGas != nil {
		excessBlobGas = *header.ExcessBlobGas
	}
	execPayload := &payload.ExecutionPayloadV4{
		ExecutionPayloadV3: payload.ExecutionPayloadV3{
			ExecutionPayloadV2: payload.ExecutionPayloadV2{
				ExecutionPayloadV1: payload.ExecutionPayloadV1{
					ParentHash:    header.ParentHash,
					FeeRecipient:  header.Coinbase,
					StateRoot:     header.Root,
					ReceiptsRoot:  header.ReceiptHash,
					LogsBloom:     header.Bloom,
					PrevRandao:    header.MixDigest,
					BlockNumber:   blk.NumberU64(),
					GasLimit:      header.GasLimit,
					GasUsed:       header.GasUsed,
					Timestamp:     header.Time,
					ExtraData:     header.Extra,
					BaseFeePerGas: header.BaseFee,
					BlockHash:     blk.Hash(),
					Transactions:  encodeTxsRLP(blk.Transactions()),
				},
			},
			BlobGasUsed:   blobGasUsed,
			ExcessBlobGas: excessBlobGas,
		},
	}

	if p.result != nil {
		execPayload.ExecutionRequests = p.result.Requests.Encode()
	}
	if ws := blk.Withdrawals(); ws != nil {
		execPayload.Withdrawals = make([]*payload.Withdrawal, 0, len(ws))
		for _, w := range ws {
			execPayload.Withdrawals = append(execPayload.Withdrawals, &payload.Withdrawal{
				Index:          w.Index,
				ValidatorIndex: w.ValidatorIndex,
				Address:        w.Address,
				Amount:         w.Amount,
			})
		}
	}

	blockValue := new(big.Int)
	for _, receipt := range p.receipts {
		if receipt.EffectiveGasPrice != nil && header.BaseFee != nil {
			tip := new(big.Int).Sub(receipt.EffectiveGasPrice, header.BaseFee)
			if tip.Sign() > 0 {
				tipTotal := new(big.Int).Mul(tip, new(big.Int).SetUint64(receipt.GasUsed))
				blockValue.Add(blockValue, tipTotal)
			}
		}
	}

	slog.Debug("engine_getPayload: returning payload",
		"payloadID", id,
		"blockNumber", blk.NumberU64(),
		"blockHash", blk.Hash(),
		"txCount", len(blk.Transactions()),
		"blockValue", blockValue,
	)

	blobsBundle := &payload.BlobsBundleV1{}
	blobTxCount := 0
	for _, tx := range blk.Transactions() {
		if tx.Type() != types.BlobTxType {
			continue
		}
		blobTxCount++
		sc := tx.BlobSidecar()
		if sc == nil {
			slog.Debug("engine_getPayload: blob tx missing sidecar",
				"txHash", tx.Hash(),
				"blockNumber", blk.NumberU64(),
			)
			continue
		}
		blobsBundle.Commitments = append(blobsBundle.Commitments, sc.Commitments...)
		blobsBundle.Proofs = append(blobsBundle.Proofs, sc.Proofs...)
		blobsBundle.Blobs = append(blobsBundle.Blobs, sc.Blobs...)
	}
	if blobTxCount > 0 {
		slog.Debug("engine_getPayload: blobsBundle",
			"blobTxCount", blobTxCount,
			"blobCount", len(blobsBundle.Blobs),
			"blockNumber", blk.NumberU64(),
		)
	}

	// Cache blobs for engine_getBlobsV2 (PeerDAS).
	// When the CL calls engine_getBlobsV2, it needs to find blobs in the cache
	// because engine_newPayload doesn't receive blob data.
	if len(blobsBundle.Blobs) > 0 {
		b.blobCacheMu.Lock()
		// Clear old cache entries to avoid stale data
		b.blobCache = make(map[types.Hash][]byte)
		for _, tx := range blk.Transactions() {
			if tx.Type() != types.BlobTxType {
				continue
			}
			sc := tx.BlobSidecar()
			if sc == nil {
				slog.Warn("engine_getPayload: BlobSidecar is nil for blob tx")
				continue
			}
			blobHashes := tx.BlobHashes()
			slog.Debug("engine_getPayload: caching blob hashes",
				"blobHashes", len(blobHashes),
				"scBlobs", len(sc.Blobs),
			)
			for i, hash := range blobHashes {
				if i < len(sc.Blobs) && len(sc.Blobs[i]) > 0 {
					b.blobCache[hash] = sc.Blobs[i]
					slog.Debug("engine_getPayload: cached blob", "hash", hash.Hex(), "index", i)
				}
			}
		}
		b.blobCacheMu.Unlock()
		slog.Debug("engine_getPayload: cached blobs for getBlobsV2",
			"blobCount", len(b.blobCache),
			"blockNumber", blk.NumberU64(),
		)
	}

	return &payload.GetPayloadResponse{
		ExecutionPayload: execPayload,
		BlockValue:       blockValue,
		BlobsBundle:      blobsBundle,
		Override:         false,
	}, nil
}

// GetPayloadV4ByID retrieves a previously built payload for getPayloadV4 (Prague).
func (b *EngineBackend) GetPayloadV4ByID(id payload.PayloadID) (*payload.GetPayloadV4Response, error) {
	resp, err := b.GetPayloadByID(id)
	if err != nil {
		return nil, err
	}
	return &payload.GetPayloadV4Response{
		ExecutionPayload:  &resp.ExecutionPayload.ExecutionPayloadV3,
		BlockValue:        resp.BlockValue,
		BlobsBundle:       resp.BlobsBundle,
		ExecutionRequests: resp.ExecutionPayload.ExecutionRequests,
	}, nil
}

// GetPayloadV6ByID retrieves a previously built payload for getPayloadV6 (Amsterdam).
func (b *EngineBackend) GetPayloadV6ByID(id payload.PayloadID) (*payload.GetPayloadV6Response, error) {
	p, err := b.awaitPayload(id)
	if err != nil {
		return nil, err
	}
	resp, err := b.payloadResponse(id, p)
	if err != nil {
		return nil, err
	}
	var blobsBundleV2 *payload.BlobsBundleV2
	if b1 := resp.BlobsBundle; b1 != nil && len(b1.Blobs) > 0 {
		// Expand KZG proofs to cell proofs (128 per blob) for PeerDAS.
		cellProofs, _ := expandBlobCellProofs(b1.Blobs, "GetPayloadV6ByID: ComputeCellsAndProofs failed")
		blobsBundleV2 = &payload.BlobsBundleV2{
			Commitments: b1.Commitments,
			Proofs:      cellProofs,
			Blobs:       b1.Blobs,
		}
		// Debug: print first proof bytes to verify non-zero
		if len(cellProofs) > 0 && len(cellProofs[0]) >= 16 {
			slog.Debug("GetPayloadV6ByID: BlobsBundleV2",
				"blobCount", len(b1.Blobs),
				"cellProofs", len(cellProofs),
				"expectedProofs", len(b1.Blobs)*bls.KZGCellsPerExtBlob,
				"firstProofHex", fmt.Sprintf("%x", cellProofs[0][:16]),
			)
		} else {
			slog.Debug("GetPayloadV6ByID: BlobsBundleV2",
				"blobCount", len(b1.Blobs),
				"cellProofs", len(cellProofs),
				"expectedProofs", len(b1.Blobs)*bls.KZGCellsPerExtBlob,
			)
		}
	}
	var blockBAL *bal.BlockAccessList
	if p.result != nil {
		blockBAL = p.result.BlockAccessList
	}
	v5 := enginepayload.BlockToPayloadV5(p.block, p.block.Header().MixDigest, resp.ExecutionPayload.Withdrawals, blockBAL, p.slot)
	v5.ExecutionRequests = resp.ExecutionPayload.ExecutionRequests
	return &payload.GetPayloadV6Response{
		ExecutionPayload:  v5,
		BlockValue:        resp.BlockValue,
		BlobsBundle:       blobsBundleV2,
		ExecutionRequests: resp.ExecutionPayload.ExecutionRequests,
	}, nil
}

// GetPayloadBodiesByHash returns payload bodies for the given block hashes.
func (b *EngineBackend) GetPayloadBodiesByHash(hashes []types.Hash) ([]*payload.ExecutionPayloadBodyV2, error) {
	results := make([]*payload.ExecutionPayloadBodyV2, len(hashes))
	for i, h := range hashes {
		blk := b.node.Blockchain().GetBlock(h)
		if blk == nil {
			results[i] = nil
			continue
		}
		results[i] = enginepayload.BlockToPayloadBodyV2(blk)
	}
	return results, nil
}

// GetPayloadBodiesByRange returns payload bodies for a range of block numbers.
func (b *EngineBackend) GetPayloadBodiesByRange(start, count uint64) ([]*payload.ExecutionPayloadBodyV2, error) {
	results := make([]*payload.ExecutionPayloadBodyV2, count)
	for i := uint64(0); i < count; i++ {
		num := start + i
		blk := b.node.Blockchain().GetBlockByNumber(num)
		if blk == nil {
			results[i] = nil
			continue
		}
		results[i] = enginepayload.BlockToPayloadBodyV2(blk)
	}
	return results, nil
}

// GetBlobsByVersionedHashes returns blobs by versioned hashes.
func (b *EngineBackend) GetBlobsByVersionedHashes(hashes []types.Hash) []*payload.BlobAndProofV1 {
	if b.node.TxPool() == nil {
		return make([]*payload.BlobAndProofV1, len(hashes))
	}
	raw := b.node.TxPool().GetBlobsByVersionedHashes(hashes)
	result := make([]*payload.BlobAndProofV1, len(raw))
	for i, r := range raw {
		if r != nil {
			result[i] = &payload.BlobAndProofV1{
				Blob:       r.Blob,
				Commitment: r.Commitment,
				Proof:      r.Proof,
			}
		}
	}
	return result
}

// GetBlobsV2ByVersionedHashes returns blobs with cell proofs.
func (b *EngineBackend) GetBlobsV2ByVersionedHashes(hashes []types.Hash) []*payload.BlobAndProofV2 {
	return b.computeBlobsV2(hashes)
}

// GetBlobsV3ByVersionedHashes returns blobs with cell proofs (sparse).
func (b *EngineBackend) GetBlobsV3ByVersionedHashes(hashes []types.Hash) []*payload.BlobAndProofV2 {
	return b.computeBlobsV2(hashes)
}

func (b *EngineBackend) computeBlobsV2(hashes []types.Hash) []*payload.BlobAndProofV2 {
	result := make([]*payload.BlobAndProofV2, len(hashes))
	kzg := bls.DefaultKZGBackend()

	b.blobCacheMu.RLock()
	cachedBlobs := make([][]byte, len(hashes))
	cacheHits := 0
	// Debug: log the requested hashes and cache keys
	var requestedHashes []string
	for _, h := range hashes {
		requestedHashes = append(requestedHashes, h.Hex())
	}
	var cacheKeys []string
	for h := range b.blobCache {
		cacheKeys = append(cacheKeys, h.Hex())
	}
	slog.Debug("computeBlobsV2: hash comparison",
		"requested", requestedHashes,
		"cacheKeys", cacheKeys,
	)
	for i, h := range hashes {
		if blob, ok := b.blobCache[h]; ok {
			cachedBlobs[i] = blob
			cacheHits++
		}
	}
	cacheSize := len(b.blobCache)
	b.blobCacheMu.RUnlock()

	if len(hashes) > 0 {
		slog.Debug("computeBlobsV2: looking up blobs",
			"requested", len(hashes),
			"cacheHits", cacheHits,
			"cacheSize", cacheSize,
		)
	}

	for i, blob := range cachedBlobs {
		if len(blob) == 0 {
			continue
		}
		_, cellProofs, err := kzg.ComputeCellsAndProofs(blob)
		if err != nil {
			slog.Warn("computeBlobsV2: ComputeCellsAndProofs failed for cached blob",
				"hash", hashes[i], "err", err)
			continue
		}
		proofs := make([][]byte, len(cellProofs))
		for j, p := range cellProofs {
			cp := p
			proofs[j] = cp[:]
		}
		result[i] = &payload.BlobAndProofV2{
			Blob:   blob,
			Proofs: proofs,
		}
	}

	if b.node.TxPool() != nil {
		var pendingHashes []types.Hash
		pendingIdx := make(map[int]int)
		for i, h := range hashes {
			if result[i] == nil {
				pendingIdx[len(pendingHashes)] = i
				pendingHashes = append(pendingHashes, h)
			}
		}
		if len(pendingHashes) > 0 {
			raw := b.node.TxPool().GetBlobsByVersionedHashes(pendingHashes)
			poolHits := 0
			for j, r := range raw {
				if r == nil || len(r.Blob) == 0 {
					continue
				}
				poolHits++
				origIdx := pendingIdx[j]
				_, cellProofs, err := kzg.ComputeCellsAndProofs(r.Blob)
				if err != nil {
					slog.Warn("computeBlobsV2: ComputeCellsAndProofs failed",
						"hash", pendingHashes[j], "err", err)
					continue
				}
				proofs := make([][]byte, len(cellProofs))
				for k, p := range cellProofs {
					cp := p
					proofs[k] = cp[:]
				}
				result[origIdx] = &payload.BlobAndProofV2{
					Blob:   r.Blob,
					Proofs: proofs,
				}
			}
			slog.Debug("computeBlobsV2: txpool lookup",
				"pending", len(pendingHashes),
				"poolHits", poolHits,
			)
		}
	}

	found := 0
	for _, r := range result {
		if r != nil {
			found++
		}
	}
	if found < len(hashes) && len(hashes) > 0 {
		slog.Warn("computeBlobsV2: missing blobs",
			"requested", len(hashes),
			"found", found,
		)
	}
	return result
}

func generatePayloadID(parentHash types.Hash, attrs *block.BuildBlockAttributes) payload.PayloadID {
	var buf [8]byte
	copy(buf[:], parentHash[:8])
	var tsBytes [8]byte
	binary.BigEndian.PutUint64(tsBytes[:], attrs.Timestamp)
	for i := range buf {
		buf[i] ^= tsBytes[i]
	}
	for i := 0; i < 8; i++ {
		buf[i] ^= attrs.FeeRecipient[i]
	}
	for i := 0; i < 8; i++ {
		buf[i] ^= attrs.Random[8+i]
	}

	var id payload.PayloadID
	copy(id[:], buf[:])

	if id == (payload.PayloadID{}) {
		rand.Read(id[:])
	}
	return id
}

func encodeTxsRLP(txs []*types.Transaction) [][]byte {
	encoded := make([][]byte, 0, len(txs))
	for _, tx := range txs {
		raw, err := tx.EncodeRLP()
		if err != nil {
			continue
		}
		encoded = append(encoded, raw)
	}
	return encoded
}

// expandBlobCellProofs expands each blob's KZG proof to 128 per-cell KZG proofs.
// This is required by the Fulu/PeerDAS spec for BlobsBundleV2.
func expandBlobCellProofs(blobs [][]byte, warnLabel string) ([][]byte, int) {
	if len(blobs) == 0 {
		return nil, 0
	}

	kzg := bls.DefaultKZGBackend()
	proofs := make([][]byte, 0, len(blobs)*bls.KZGCellsPerExtBlob)
	totalProofs := 0
	for _, blob := range blobs {
		_, cellProofs, err := kzg.ComputeCellsAndProofs(blob)
		if err != nil {
			slog.Warn(warnLabel, "err", err)
			for range bls.KZGCellsPerExtBlob {
				proofs = append(proofs, make([]byte, bls.KZGBytesPerProof))
				totalProofs++
			}
			continue
		}
		for _, p := range cellProofs {
			cp := p
			proofs = append(proofs, cp[:])
			totalProofs++
		}
	}
	return proofs, totalProofs
}

// GetPayloadV7ByID retrieves a previously built payload for getPayloadV7.
// When EIP-7807 SSZ blocks are active at the payload timestamp, the response
// additionally carries the SSZ payload encoding and its hash tree root —
// the SSZ execution block hash the CL commits to.
func (b *EngineBackend) GetPayloadV7ByID(id payload.PayloadID) (*payload.GetPayloadV6Response, error) {
	resp, err := b.GetPayloadV6ByID(id)
	if err != nil {
		return nil, err
	}
	p, err := b.awaitPayload(id)
	if err != nil {
		return resp, nil
	}
	if p.block == nil || !b.node.Blockchain().Config().IsSSZBlock(p.block.Time()) {
		return resp, nil
	}
	requests := types.Requests(nil)
	balRLP := []byte(nil)
	if p.result != nil {
		requests = p.result.Requests
		if p.result.BlockAccessList != nil {
			balRLP, _ = p.result.BlockAccessList.EncodeRLP()
		}
	}
	receipts := p.receipts
	excessRegular := uint64(0)
	if p.block.Header().CalldataExcessGas != nil {
		excessRegular = *p.block.Header().CalldataExcessGas
	}
	ctx := types.ExecutionSSZContext{
		BlobGasLimit:     gas.FusakaMaxBlobGasPerBlock,
		BlobBaseFee:      gas.CalcBlobBaseFeeV2(*p.block.Header().ExcessBlobGas, p.block.Header().BaseFee),
		ExcessRegularGas: excessRegular,
		Requests:         requests,
		BlockAccessList:  balRLP,
		SlotNumber:       p.slot,
	}
	payloadSSZ, err := types.NewExecutionPayloadSSZ(p.block, receipts, ctx)
	if err != nil {
		slog.Debug("GetPayloadV7ByID: ssz build failed", "err", err, "receipts", len(receipts), "balRLP", len(balRLP))
		return resp, nil
	}
	enc, err := payloadSSZ.MarshalSSZ()
	if err != nil {
		return resp, nil
	}
	root, err := payloadSSZ.HashTreeRoot()
	if err != nil {
		return resp, nil
	}
	resp.SSZPayload = json.RawMessage(`"` + "0x" + hex.EncodeToString(enc) + `"`)
	resp.SSZBlockHash = &root
	return resp, nil
}
