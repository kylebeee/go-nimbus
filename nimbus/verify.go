// Copyright (C) 2019-2026 Algorand, Inc.
// This file is part of go-algorand
//
// go-algorand is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// go-algorand is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with go-algorand.  If not, see <https://www.gnu.org/licenses/>.

package nimbus

import (
	"fmt"

	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
)

// VerifyResult holds the outcome of a receipt chain verification.
type VerifyResult struct {
	Valid      bool          `json:"valid"`
	Entries    int           `json:"entries"`
	FirstRound basics.Round  `json:"first_round"`
	LastRound  basics.Round  `json:"last_round"`
	BrokenAt   *basics.Round `json:"broken_at,omitempty"`
	Error      string        `json:"error,omitempty"`
}

// VerifyChain validates the full receipt chain for a hook.
// It checks that:
//  1. ProgramHash matches the registered program
//  2. StateHash matches the stored State bytes
//  3. PrevHash links to the previous entry's ReceiptHash
//  4. ReceiptHash is correctly computed from all components
func VerifyChain(history []HookState, program []byte) VerifyResult {
	if len(history) == 0 {
		return VerifyResult{Valid: true, Entries: 0}
	}

	programHash := crypto.Hash(program)
	result := VerifyResult{
		Entries:    len(history),
		FirstRound: history[0].Round,
		LastRound:  history[len(history)-1].Round,
	}

	for i, entry := range history {
		round := entry.Round

		// Check ProgramHash
		if entry.ProgramHash != programHash {
			result.BrokenAt = &round
			result.Error = fmt.Sprintf("round %d: program hash mismatch", round)
			return result
		}

		// Check StateHash
		expectedStateHash := crypto.Hash(entry.State)
		if entry.StateHash != expectedStateHash {
			result.BrokenAt = &round
			result.Error = fmt.Sprintf("round %d: state hash mismatch", round)
			return result
		}

		// Check PrevHash linkage
		if i > 0 {
			expectedPrev := history[i-1].ReceiptHash
			if entry.PrevHash != expectedPrev {
				result.BrokenAt = &round
				result.Error = fmt.Sprintf("round %d: prev hash does not link to previous receipt", round)
				return result
			}
		}

		// Check ReceiptHash
		receipt := HookReceipt{
			Round:       entry.Round,
			BlockHash:   entry.BlockHash,
			ProgramHash: entry.ProgramHash,
			StateHash:   entry.StateHash,
			PrevHash:    entry.PrevHash,
		}
		expectedReceipt := crypto.HashObj(receipt)
		if entry.ReceiptHash != expectedReceipt {
			result.BrokenAt = &round
			result.Error = fmt.Sprintf("round %d: receipt hash mismatch", round)
			return result
		}
	}

	result.Valid = true
	return result
}
