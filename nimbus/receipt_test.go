package nimbus

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/data/bookkeeping"
)

func TestHookReceiptDeterminism(t *testing.T) {
	program := []byte{0x06, 0x81, 0x01}
	state := &HookState{
		Round: basics.Round(10),
		State: []byte("hello"),
	}
	hdr := bookkeeping.BlockHeader{Round: basics.Round(10)}
	prevHash := crypto.Hash([]byte("prev"))

	computeReceipt(state, program, hdr, prevHash)
	first := state.ReceiptHash

	// Compute again with identical inputs
	state2 := &HookState{
		Round: basics.Round(10),
		State: []byte("hello"),
	}
	computeReceipt(state2, program, hdr, prevHash)

	require.Equal(t, first, state2.ReceiptHash, "identical inputs must produce identical receipt hashes")
}

func TestHookReceiptDifferentInputs(t *testing.T) {
	program := []byte{0x06, 0x81, 0x01}
	hdr := bookkeeping.BlockHeader{Round: basics.Round(10)}
	prevHash := crypto.Digest{}

	// Different state bytes
	s1 := &HookState{Round: 10, State: []byte("aaa")}
	s2 := &HookState{Round: 10, State: []byte("bbb")}
	computeReceipt(s1, program, hdr, prevHash)
	computeReceipt(s2, program, hdr, prevHash)
	require.NotEqual(t, s1.ReceiptHash, s2.ReceiptHash, "different state should produce different receipts")

	// Different program
	s3 := &HookState{Round: 10, State: []byte("aaa")}
	computeReceipt(s3, []byte{0x06, 0x81, 0x02}, hdr, prevHash)
	require.NotEqual(t, s1.ReceiptHash, s3.ReceiptHash, "different program should produce different receipts")

	// Different round
	s4 := &HookState{Round: 11, State: []byte("aaa")}
	hdr2 := bookkeeping.BlockHeader{Round: basics.Round(11)}
	computeReceipt(s4, program, hdr2, prevHash)
	require.NotEqual(t, s1.ReceiptHash, s4.ReceiptHash, "different round should produce different receipts")

	// Different prevHash
	s5 := &HookState{Round: 10, State: []byte("aaa")}
	computeReceipt(s5, program, hdr, crypto.Hash([]byte("other")))
	require.NotEqual(t, s1.ReceiptHash, s5.ReceiptHash, "different prevHash should produce different receipts")
}

func TestHookReceiptChainLinkage(t *testing.T) {
	program := []byte{0x06, 0x81, 0x01}

	// Genesis
	genesis := &HookState{Round: 0, State: []byte("init")}
	computeGenesisReceipt(genesis, program)
	require.False(t, genesis.ReceiptHash.IsZero(), "genesis receipt should not be zero")
	require.True(t, genesis.BlockHash.IsZero(), "genesis block hash should be zero")
	require.True(t, genesis.PrevHash.IsZero(), "genesis prev hash should be zero")

	// Round 1
	hdr1 := bookkeeping.BlockHeader{Round: basics.Round(1)}
	r1 := &HookState{Round: 1, State: []byte("state1")}
	computeReceipt(r1, program, hdr1, genesis.ReceiptHash)
	require.Equal(t, genesis.ReceiptHash, r1.PrevHash, "round 1 prev should link to genesis receipt")

	// Round 2
	hdr2 := bookkeeping.BlockHeader{Round: basics.Round(2)}
	r2 := &HookState{Round: 2, State: []byte("state2")}
	computeReceipt(r2, program, hdr2, r1.ReceiptHash)
	require.Equal(t, r1.ReceiptHash, r2.PrevHash, "round 2 prev should link to round 1 receipt")

	// Round 3
	hdr3 := bookkeeping.BlockHeader{Round: basics.Round(3)}
	r3 := &HookState{Round: 3, State: []byte("state3")}
	computeReceipt(r3, program, hdr3, r2.ReceiptHash)
	require.Equal(t, r2.ReceiptHash, r3.PrevHash, "round 3 prev should link to round 2 receipt")

	// All receipts are unique
	hashes := []crypto.Digest{genesis.ReceiptHash, r1.ReceiptHash, r2.ReceiptHash, r3.ReceiptHash}
	for i := 0; i < len(hashes); i++ {
		for j := i + 1; j < len(hashes); j++ {
			require.NotEqual(t, hashes[i], hashes[j], "all receipt hashes should be unique")
		}
	}
}

func TestGenesisReceiptFields(t *testing.T) {
	program := []byte{0x06, 0x81, 0x01}
	state := &HookState{
		Round: 0,
		State: []byte("seed"),
	}
	computeGenesisReceipt(state, program)

	require.Equal(t, crypto.Hash(program), state.ProgramHash)
	require.Equal(t, crypto.Hash([]byte("seed")), state.StateHash)
	require.Equal(t, crypto.Digest{}, state.BlockHash)
	require.Equal(t, crypto.Digest{}, state.PrevHash)
	require.False(t, state.ReceiptHash.IsZero())
}

func TestToBeHashedEncoding(t *testing.T) {
	receipt := HookReceipt{
		Round:       42,
		BlockHash:   crypto.Hash([]byte("block")),
		ProgramHash: crypto.Hash([]byte("program")),
		StateHash:   crypto.Hash([]byte("state")),
		PrevHash:    crypto.Hash([]byte("prev")),
	}

	hashID, data := receipt.ToBeHashed()
	require.Equal(t, NimbusReceipt, hashID)
	require.Len(t, data, 8+4*crypto.DigestSize, "encoding should be exactly 136 bytes")
}
