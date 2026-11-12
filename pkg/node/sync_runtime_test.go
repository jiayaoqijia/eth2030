package node

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreblock "github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/engine/payload"
	nodebackend "github.com/eth2030/eth2030/node/backend"
	"github.com/eth2030/eth2030/rpc"
	gethcommon "github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	gethrlp "github.com/ethereum/go-ethereum/rlp"
	gethtrie "github.com/ethereum/go-ethereum/trie"
	"github.com/golang-jwt/jwt/v4"
)

func connectSyncNodes(t *testing.T, a, b *Node) {
	t.Helper()
	_, port, err := net.SplitHostPort(a.p2pServer.ListenAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.p2pServer.AddPeer("enode://" + a.p2pServer.LocalID() + "@" + net.JoinHostPort("127.0.0.1", port)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if a.ethHandler.Peers().Len() > 0 && b.ethHandler.Peers().Len() > 0 && len(a.snapHandler.Clients()) > 0 && len(b.snapHandler.Clients()) > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("eth and snap protocols did not connect over TCP")
}

func TestNodeCLDrivenSyncAndSnapRestart(t *testing.T) {
	for _, mode := range []string{"full", "snap"} {
		t.Run(mode, func(t *testing.T) {
			var genesis map[string]any
			if err := json.Unmarshal(minimalGenesisJSON(1337), &genesis); err != nil {
				t.Fatal(err)
			}
			account := types.HexToAddress("0x0000000000000000000000000000000000001234")
			genesis["alloc"].(map[string]any)[account.Hex()] = map[string]any{"balance": "1000", "code": "0x600160005500", "storage": map[string]string{"0x00": "0x80"}}
			data, _ := json.Marshal(genesis)
			path := filepath.Join(t.TempDir(), "genesis.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			sourceCfg := makeTestConfig(t)
			sourceCfg.GenesisPath = path
			source := newTestNode(t, &sourceCfg)
			targetCfg := makeTestConfig(t)
			targetCfg.GenesisPath = path
			targetCfg.SyncMode = mode
			target := newTestNode(t, &targetCfg)
			blocks := []*types.Block{source.blockchain.Genesis()}
			for i := 1; i <= 4; i++ {
				builder := coreblock.NewBlockBuilder(source.blockchain.Config(), source.blockchain, nil)
				beaconRoot := types.Hash{}
				block, _, err := builder.BuildBlock(blocks[len(blocks)-1].Header(), &coreblock.BuildBlockAttributes{Timestamp: uint64(i * 12), GasLimit: 30_000_000, BeaconRoot: &beaconRoot, Withdrawals: []*types.Withdrawal{{Index: uint64(i), Address: account, Amount: 1}}})
				if err != nil {
					t.Fatal(err)
				}
				if err := source.blockchain.InsertBlock(block); err != nil {
					t.Fatal(err)
				}
				blocks = append(blocks, block)
			}
			if err := source.Start(); err != nil {
				t.Fatal(err)
			}
			if err := target.Start(); err != nil {
				t.Fatal(err)
			}
			fc := payload.ForkchoiceStateV1{HeadBlockHash: blocks[4].Hash(), SafeBlockHash: blocks[3].Hash(), FinalizedBlockHash: blocks[2].Hash()}
			api := httptest.NewServer(target.engineServer.Handler())
			defer api.Close()
			body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "engine_forkchoiceUpdatedV3", "params": []any{fc, nil}})
			secretText, err := os.ReadFile(targetCfg.JWTSecretPath())
			if err != nil {
				t.Fatal(err)
			}
			secret, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(string(secretText)), "0x"))
			if err != nil {
				t.Fatal(err)
			}
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{IssuedAt: jwt.NewNumericDate(time.Now())}).SignedString(secret)
			if err != nil {
				t.Fatal(err)
			}
			request, err := http.NewRequest(http.MethodPost, api.URL, bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+token)
			response, err := api.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				Result payload.ForkchoiceUpdatedResult
				Error  any
			}
			err = json.NewDecoder(response.Body).Decode(&envelope)
			response.Body.Close()
			if err != nil || envelope.Error != nil || envelope.Result.PayloadStatus.Status != "SYNCING" {
				t.Fatalf("unknown head HTTP response: %+v %v", envelope, err)
			}
			if !nodeReportsSyncing(t, target) {
				t.Fatal("RPC reported synced while the CL target was unavailable")
			}
			connectSyncNodes(t, source, target)
			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				_, complete, _ := target.executionSync.Status()
				if complete {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			_, complete, syncErr := target.executionSync.Status()
			if !complete {
				t.Fatalf("sync failed: %v", syncErr)
			}
			if nodeReportsSyncing(t, target) {
				t.Fatal("RPC remained syncing after target publication")
			}
			got := target.blockchain.State()
			if target.blockchain.CurrentBlock().Hash() != blocks[4].Hash() || got.GetRoot() != source.blockchain.State().GetRoot() {
				t.Fatal("synced head/state differs")
			}
			if got.GetState(account, types.Hash{}) != types.BytesToHash([]byte{128}) || got.GetCodeSize(account) != 6 {
				t.Fatal("storage/code not available after sync")
			}
			proof, err := nodebackend.NewRPCBackend(toNodeDeps(target), target.engBackend).GetProof(account, []types.Hash{{}}, rpc.LatestBlockNumber)
			if err != nil {
				t.Fatal(err)
			}
			proofDB := memorydb.New()
			for _, node := range proof.AccountProof {
				_ = proofDB.Put(crypto.Keccak256(node), node)
			}
			accountBody, err := gethtrie.VerifyProof(gethcommon.Hash(blocks[4].Header().Root), crypto.Keccak256(account[:]), proofDB)
			if err != nil || len(accountBody) == 0 {
				t.Fatalf("independent account proof failed: %v", err)
			}
			var provedAccount gethtypes.StateAccount
			if err := gethrlp.DecodeBytes(accountBody, &provedAccount); err != nil {
				t.Fatal(err)
			}
			if provedAccount.Root != gethcommon.Hash(proof.StorageHash) {
				t.Fatal("storage proof is not bound to account proof")
			}

			proofDB = memorydb.New()
			for _, node := range proof.StorageProof[0].Proof {
				_ = proofDB.Put(crypto.Keccak256(node), node)
			}
			var slot types.Hash
			storageBody, err := gethtrie.VerifyProof(gethcommon.Hash(proof.StorageHash), crypto.Keccak256(slot[:]), proofDB)
			if err != nil {
				t.Fatal(err)
			}
			var integer []byte
			if err := gethrlp.DecodeBytes(storageBody, &integer); err != nil {
				t.Fatal(err)
			}
			if len(integer) != 1 || integer[0] != 128 || proof.StorageProof[0].Value.Uint64() != 128 {
				t.Fatal("RPC storage proof value is not the decoded slot integer")
			}
			if got.GetBalance(account).Cmp(source.blockchain.State().GetBalance(account)) != 0 {
				t.Fatal("post-pivot execution lost withdrawal balances")
			}
			if target.blockchain.CurrentFinalBlock().Hash() != blocks[2].Hash() || target.blockchain.CurrentSafeBlock().Hash() != blocks[3].Hash() {
				t.Fatal("CL checkpoints not applied")
			}
			if mode == "snap" {
				if _, ok := got.(*state.SnapStateDB); !ok {
					t.Fatalf("snap mode replayed genesis instead of adopting snapshot: %T", got)
				}
			}
			if err := target.Stop(); err != nil {
				t.Fatal(err)
			}
			reopened := newTestNode(t, &targetCfg)
			if reopened.blockchain.CurrentBlock().Hash() != blocks[4].Hash() || reopened.blockchain.State().GetRoot() != got.GetRoot() {
				t.Fatal("restart did not restore and replay head")
			}
		})
	}
}

func nodeReportsSyncing(t *testing.T, node *Node) bool {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_syncing","params":[]}`))
	response := httptest.NewRecorder()
	node.rpcHandler.Handler().ServeHTTP(response, request)
	var body struct {
		Result json.RawMessage
		Error  any
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error != nil {
		t.Fatalf("eth_syncing response: %s", response.Body.String())
	}
	return string(body.Result) != "false"
}

func TestDownloadedBlockWaitsForValidForkchoice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "genesis.json")
	if err := os.WriteFile(path, minimalGenesisJSON(1337), 0600); err != nil {
		t.Fatal(err)
	}
	sourceCfg := makeTestConfig(t)
	sourceCfg.GenesisPath = path
	source := newTestNode(t, &sourceCfg)
	targetCfg := makeTestConfig(t)
	targetCfg.GenesisPath = path
	target := newTestNode(t, &targetCfg)
	genesis := source.blockchain.Genesis()
	beaconRoot := types.Hash{}
	builder := coreblock.NewBlockBuilder(source.blockchain.Config(), source.blockchain, nil)
	block, _, err := builder.BuildBlock(genesis.Header(), &coreblock.BuildBlockAttributes{Timestamp: 12, GasLimit: 30_000_000, BeaconRoot: &beaconRoot, Withdrawals: []*types.Withdrawal{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := target.blockchain.InsertSyncedBlock(block); err != nil {
		t.Fatal(err)
	}
	if target.blockchain.CurrentBlock().Hash() != genesis.Hash() {
		t.Fatal("download published an optimistic head")
	}
	if err := target.blockchain.ApplyForkchoice(block.Hash(), types.BytesToHash([]byte{0xff}), genesis.Hash()); err == nil {
		t.Fatal("unknown safe checkpoint accepted")
	}
	if target.blockchain.CurrentBlock().Hash() != genesis.Hash() {
		t.Fatal("invalid forkchoice partially changed head")
	}
	if err := target.blockchain.ApplyForkchoice(block.Hash(), block.Hash(), genesis.Hash()); err != nil {
		t.Fatal(err)
	}
	if target.blockchain.CurrentBlock().Hash() != block.Hash() || target.blockchain.State().GetRoot() != block.Header().Root {
		t.Fatal("validated target was not published")
	}
}
