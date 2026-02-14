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
	"encoding/binary"

	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/data/bookkeeping"
	"github.com/algorand/go-algorand/protocol"
)

// NimbusReceipt is the HashID domain separator for Nimbus receipts.
// Verified prefix-free against all existing HashIDs in protocol/hash.go.
const NimbusReceipt protocol.HashID = "NR"

// HookReceipt is a cryptographic receipt that binds a hook's state to a
// specific block and chains to the previous receipt, forming a tamper-evident
// hash chain.
type HookReceipt struct {
	Round       basics.Round
	BlockHash   crypto.Digest
	ProgramHash crypto.Digest
	StateHash   crypto.Digest
	PrevHash    crypto.Digest
}

// ToBeHashed implements crypto.Hashable.
// Returns a fixed-width binary encoding: 8 bytes (round) + 4 * 32 bytes (digests) = 136 bytes.
// This deterministic layout avoids any serialization ambiguity.
func (r HookReceipt) ToBeHashed() (protocol.HashID, []byte) {
	var buf [8 + 4*crypto.DigestSize]byte
	binary.BigEndian.PutUint64(buf[:8], uint64(r.Round))
	copy(buf[8:40], r.BlockHash[:])
	copy(buf[40:72], r.ProgramHash[:])
	copy(buf[72:104], r.StateHash[:])
	copy(buf[104:136], r.PrevHash[:])
	return NimbusReceipt, buf[:]
}

// computeReceipt computes and populates the cryptographic receipt fields on a
// HookState after evaluation. The receipt binds the state to the block header
// and chains to the previous receipt hash.
func computeReceipt(state *HookState, program []byte, hdr bookkeeping.BlockHeader, prevReceiptHash crypto.Digest) {
	state.BlockHash = crypto.HashObj(hdr)
	state.ProgramHash = crypto.Hash(program)
	state.StateHash = crypto.Hash(state.State)
	state.PrevHash = prevReceiptHash

	receipt := HookReceipt{
		Round:       state.Round,
		BlockHash:   state.BlockHash,
		ProgramHash: state.ProgramHash,
		StateHash:   state.StateHash,
		PrevHash:    state.PrevHash,
	}
	state.ReceiptHash = crypto.HashObj(receipt)
}

// computeGenesisReceipt computes the receipt for a hook's initial state at
// round 0. BlockHash and PrevHash are zero since there is no block or
// predecessor.
func computeGenesisReceipt(state *HookState, program []byte) {
	state.BlockHash = crypto.Digest{}
	state.ProgramHash = crypto.Hash(program)
	state.StateHash = crypto.Hash(state.State)
	state.PrevHash = crypto.Digest{}

	receipt := HookReceipt{
		Round:       state.Round,
		BlockHash:   state.BlockHash,
		ProgramHash: state.ProgramHash,
		StateHash:   state.StateHash,
		PrevHash:    state.PrevHash,
	}
	state.ReceiptHash = crypto.HashObj(receipt)
}
