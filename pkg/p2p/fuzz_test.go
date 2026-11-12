package p2p

import (
	"testing"

	"github.com/eth2030/eth2030/rlp"
)

// FuzzP2PMessageDecode feeds random bytes as P2P message payloads and attempts
// to decode them using DecodeMessage into various protocol types. Must not panic.
func FuzzP2PMessageDecode(f *testing.F) {
	// Seed corpus: minimal valid RLP encodings.
	f.Add([]byte{0xc0})                         // empty RLP list
	f.Add([]byte{0x80})                         // RLP empty string
	f.Add([]byte{0xc1, 0x80})                   // list with empty string
	f.Add([]byte{0xc5, 0x83, 0x63, 0x61, 0x74}) // list with "cat"
	// A minimal valid Message frame payload.
	emptyList, _ := rlp.EncodeToBytes(struct{}{})
	if emptyList != nil {
		f.Add(emptyList)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Try decoding as a Message with various codes.
		// The goal is to verify no panics on any random input.
		msg := Message{
			Code:    StatusMsg,
			Size:    uint32(len(data)),
			Payload: data,
		}

		// Try decoding into StatusData.
		var status StatusData
		_ = DecodeMessage(msg, &status)

		// Try decoding into GetBlockHeadersPacket.
		msg.Code = GetBlockHeadersMsg
		var headers GetBlockHeadersPacket
		_ = DecodeMessage(msg, &headers)

		// Try decoding into BlockHeadersPacket.
		msg.Code = BlockHeadersMsg
		var blockHeaders BlockHeadersPacket
		_ = DecodeMessage(msg, &blockHeaders)

		// Try decoding into GetBlockBodiesPacket.
		msg.Code = GetBlockBodiesMsg
		var bodies GetBlockBodiesPacket
		_ = DecodeMessage(msg, &bodies)

		// Try decoding into NewPooledTransactionHashesPacket68.
		msg.Code = NewPooledTransactionHashesMsg
		var pooledHashes NewPooledTransactionHashesPacket68
		_ = DecodeMessage(msg, &pooledHashes)

		// Try decoding into GetReceiptsPacket.
		msg.Code = GetReceiptsMsg
		var receipts GetReceiptsPacket
		_ = DecodeMessage(msg, &receipts)

		// Try decoding into GetReceiptsPacketV70 (eth/70).
		msg.Code = GetReceiptsMsg
		var receiptsV70 GetReceiptsPacketV70
		_ = DecodeMessage(msg, &receiptsV70)

		// Try decoding into GetBlockAccessListsPacket (eth/71).
		msg.Code = GetBlockAccessListsMsg
		var accessLists GetBlockAccessListsPacket
		_ = DecodeMessage(msg, &accessLists)
	})
}

// FuzzETHMessageEncodeDecode encodes valid protocol types and then feeds
// random bytes as payloads for decoding. Must not panic.
func FuzzETHMessageEncodeDecode(f *testing.F) {
	// Seed with a valid encoded StatusData.
	sd := StatusData{
		ProtocolVersion: 68,
		NetworkID:       1,
	}
	encoded, err := EncodeMessage(StatusMsg, sd)
	if err == nil {
		f.Add(uint64(StatusMsg), encoded.Payload)
	}

	// Seed with a valid GetBlockHeadersPacket.
	ghp := GetBlockHeadersPacket{
		RequestID: 1,
		Request: GetBlockHeadersRequest{
			Amount: 10,
		},
	}
	encoded, err = EncodeMessage(GetBlockHeadersMsg, ghp)
	if err == nil {
		f.Add(uint64(GetBlockHeadersMsg), encoded.Payload)
	}

	// Seed with empty payload.
	f.Add(uint64(0), []byte{0xc0})
	// Seed with garbage.
	f.Add(uint64(255), []byte{0xff, 0xfe, 0xfd})

	f.Fuzz(func(t *testing.T, code uint64, payload []byte) {
		// Cap payload size to prevent huge allocations.
		if len(payload) > 4096 {
			payload = payload[:4096]
		}

		msg := Message{
			Code:    code,
			Size:    uint32(len(payload)),
			Payload: payload,
		}

		// Validate message code: must not panic.
		_ = ValidateMessageCode(code)

		// Get message name: must not panic.
		_ = MessageName(code)

		// Attempt decode into various types based on code.
		switch code {
		case StatusMsg:
			var v StatusData
			_ = DecodeMessage(msg, &v)
		case GetBlockHeadersMsg:
			var v GetBlockHeadersPacket
			_ = DecodeMessage(msg, &v)
		case BlockHeadersMsg:
			var v BlockHeadersPacket
			_ = DecodeMessage(msg, &v)
		case GetBlockBodiesMsg:
			var v GetBlockBodiesPacket
			_ = DecodeMessage(msg, &v)
		case BlockBodiesMsg:
			var v BlockBodiesPacket
			_ = DecodeMessage(msg, &v)
		case NewPooledTransactionHashesMsg:
			var v NewPooledTransactionHashesPacket68
			_ = DecodeMessage(msg, &v)
		case GetPooledTransactionsMsg:
			var v GetPooledTransactionsPacket
			_ = DecodeMessage(msg, &v)
		case PooledTransactionsMsg:
			var v PooledTransactionsPacket
			_ = DecodeMessage(msg, &v)
		case GetReceiptsMsg:
			var v GetReceiptsPacket
			_ = DecodeMessage(msg, &v)
		case ReceiptsMsg:
			var v ReceiptsPacket
			_ = DecodeMessage(msg, &v)
		default:
			// Try decoding as a generic type anyway.
			var v StatusData
			_ = DecodeMessage(msg, &v)
		}
	})
}

// FuzzMsgPipeRoundtrip exercises the MsgPipe with random data. Must not panic.
func FuzzMsgPipeRoundtrip(f *testing.F) {
	f.Add(uint64(0), []byte{0x01, 0x02, 0x03})
	f.Add(uint64(StatusMsg), []byte{0xc0})
	f.Add(uint64(255), []byte{})

	f.Fuzz(func(t *testing.T, code uint64, payload []byte) {
		if len(payload) > 1024 {
			payload = payload[:1024]
		}

		a, b := MsgPipe()
		defer a.Close()
		defer b.Close()

		// Write from one end.
		msg := Msg{
			Code:    code,
			Size:    uint32(len(payload)),
			Payload: payload,
		}
		err := a.WriteMsg(msg)
		if err != nil {
			return
		}

		// Read from the other end. Must not panic.
		received, err := b.ReadMsg()
		if err != nil {
			return
		}

		// Verify basic fields match.
		if received.Code != code {
			t.Errorf("code mismatch: got %d, want %d", received.Code, code)
		}
		if int(received.Size) != len(payload) {
			t.Errorf("size mismatch: got %d, want %d", received.Size, len(payload))
		}
	})
}
