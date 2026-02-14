package nimbus

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/data/bookkeeping"
)

func buildTestChain(program []byte, rounds int) []HookState {
	chain := make([]HookState, 0, rounds+1)

	genesis := HookState{Round: 0, State: []byte("init")}
	computeGenesisReceipt(&genesis, program)
	chain = append(chain, genesis)

	for i := 1; i <= rounds; i++ {
		hdr := bookkeeping.BlockHeader{Round: basics.Round(i)}
		prev := chain[len(chain)-1]
		state := HookState{
			Round: basics.Round(i),
			State: []byte("state"),
		}
		computeReceipt(&state, program, hdr, prev.ReceiptHash)
		chain = append(chain, state)
	}
	return chain
}

func TestVerifyChainValid(t *testing.T) {
	program := []byte{0x06, 0x81, 0x01}
	chain := buildTestChain(program, 5)

	result := VerifyChain(chain, program)
	require.True(t, result.Valid)
	require.Equal(t, 6, result.Entries) // genesis + 5 rounds
	require.Equal(t, basics.Round(0), result.FirstRound)
	require.Equal(t, basics.Round(5), result.LastRound)
	require.Nil(t, result.BrokenAt)
	require.Empty(t, result.Error)
}

func TestVerifyChainEmpty(t *testing.T) {
	program := []byte{0x06, 0x81, 0x01}
	result := VerifyChain(nil, program)
	require.True(t, result.Valid)
	require.Equal(t, 0, result.Entries)
}

func TestVerifyChainTamperedState(t *testing.T) {
	program := []byte{0x06, 0x81, 0x01}
	chain := buildTestChain(program, 3)

	// Tamper with state at round 2
	chain[2].State = []byte("tampered")

	result := VerifyChain(chain, program)
	require.False(t, result.Valid)
	require.NotNil(t, result.BrokenAt)
	require.Equal(t, basics.Round(2), *result.BrokenAt)
	require.Contains(t, result.Error, "state hash mismatch")
}

func TestVerifyChainTamperedReceipt(t *testing.T) {
	program := []byte{0x06, 0x81, 0x01}
	chain := buildTestChain(program, 3)

	// Tamper with receipt hash at round 1
	chain[1].ReceiptHash = crypto.Hash([]byte("bad"))

	result := VerifyChain(chain, program)
	require.False(t, result.Valid)
	require.NotNil(t, result.BrokenAt)
	require.Equal(t, basics.Round(1), *result.BrokenAt)
	require.Contains(t, result.Error, "receipt hash mismatch")
}

func TestVerifyChainBrokenLink(t *testing.T) {
	program := []byte{0x06, 0x81, 0x01}
	chain := buildTestChain(program, 3)

	// Break the PrevHash link at round 2
	chain[2].PrevHash = crypto.Hash([]byte("wrong"))
	// Recompute receipt hash so the receipt itself is valid but the link is broken
	receipt := HookReceipt{
		Round:       chain[2].Round,
		BlockHash:   chain[2].BlockHash,
		ProgramHash: chain[2].ProgramHash,
		StateHash:   chain[2].StateHash,
		PrevHash:    chain[2].PrevHash,
	}
	chain[2].ReceiptHash = crypto.HashObj(receipt)

	result := VerifyChain(chain, program)
	require.False(t, result.Valid)
	require.NotNil(t, result.BrokenAt)
	require.Equal(t, basics.Round(2), *result.BrokenAt)
	require.Contains(t, result.Error, "prev hash does not link")
}

func TestVerifyChainWrongProgram(t *testing.T) {
	program := []byte{0x06, 0x81, 0x01}
	chain := buildTestChain(program, 2)

	// Verify with wrong program
	result := VerifyChain(chain, []byte{0x06, 0x81, 0x02})
	require.False(t, result.Valid)
	require.NotNil(t, result.BrokenAt)
	require.Equal(t, basics.Round(0), *result.BrokenAt)
	require.Contains(t, result.Error, "program hash mismatch")
}
