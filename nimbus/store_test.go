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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
)

func TestHookStoreRoundTrip(t *testing.T) {
	store := newHookStore(t.TempDir())
	defs := []HookDefinition{
		{
			ID:           "hook-a",
			Program:      []byte{0x01, 0x02},
			RequireOrigin: false,
			CreatedAt:    time.Now().UTC(),
			InitialState: []byte("seed"),
		},
	}
	require.NoError(t, store.saveHooks(defs))

	loaded, err := store.loadHooks()
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, defs[0].ID, loaded[0].ID)
	require.Equal(t, defs[0].Program, loaded[0].Program)
	require.Equal(t, defs[0].InitialState, loaded[0].InitialState)

	entry := HookState{
		Round:     basics.Round(7),
		State:     []byte("state"),
		Error:     "",
		Timestamp: time.Now().UTC(),
	}
	require.NoError(t, store.appendHistory(defs[0].ID, entry))

	latest, ok, err := store.loadLatestHistory(defs[0].ID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, entry.Round, latest.Round)
	require.Equal(t, entry.State, latest.State)

	from := basics.Round(5)
	to := basics.Round(9)
	history, err := store.readHistory(defs[0].ID, &from, &to)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, entry.Round, history[0].Round)
}

func TestHookStoreCryptoFieldsRoundTrip(t *testing.T) {
	store := newHookStore(t.TempDir())

	program := []byte{0x06, 0x81, 0x01}

	// Create an entry with crypto fields populated
	entry := HookState{
		Round:       basics.Round(5),
		State:       []byte("data"),
		Timestamp:   time.Now().UTC(),
		BlockHash:   crypto.Hash([]byte("block")),
		ProgramHash: crypto.Hash(program),
		StateHash:   crypto.Hash([]byte("data")),
		PrevHash:    crypto.Hash([]byte("prev")),
		ReceiptHash: crypto.Hash([]byte("receipt")),
	}

	require.NoError(t, store.appendHistory("hook-crypto", entry))

	// Load back and verify
	latest, ok, err := store.loadLatestHistory("hook-crypto")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, entry.Round, latest.Round)
	require.Equal(t, entry.State, latest.State)
	require.Equal(t, entry.BlockHash, latest.BlockHash)
	require.Equal(t, entry.ProgramHash, latest.ProgramHash)
	require.Equal(t, entry.StateHash, latest.StateHash)
	require.Equal(t, entry.PrevHash, latest.PrevHash)
	require.Equal(t, entry.ReceiptHash, latest.ReceiptHash)

	// Also verify via readHistory
	history, err := store.readHistory("hook-crypto", nil, nil)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, entry.BlockHash, history[0].BlockHash)
	require.Equal(t, entry.ReceiptHash, history[0].ReceiptHash)
}

func TestHookStoreBackwardCompatOldFormat(t *testing.T) {
	store := newHookStore(t.TempDir())

	// Write an entry without crypto fields (old format)
	entry := HookState{
		Round:     basics.Round(3),
		State:     []byte("old"),
		Timestamp: time.Now().UTC(),
		// All crypto fields are zero-valued
	}

	require.NoError(t, store.appendHistory("hook-old", entry))

	latest, ok, err := store.loadLatestHistory("hook-old")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, entry.Round, latest.Round)
	require.Equal(t, entry.State, latest.State)
	require.True(t, latest.BlockHash.IsZero(), "zero digest should survive round-trip")
	require.True(t, latest.ProgramHash.IsZero())
	require.True(t, latest.StateHash.IsZero())
	require.True(t, latest.PrevHash.IsZero())
	require.True(t, latest.ReceiptHash.IsZero())
}

func TestDigestEncoding(t *testing.T) {
	d := crypto.Hash([]byte("test"))
	encoded := encodeDigest(d)
	require.NotEmpty(t, encoded)

	decoded, err := decodeDigest(encoded)
	require.NoError(t, err)
	require.Equal(t, d, decoded)

	// Zero digest
	zero := crypto.Digest{}
	require.Equal(t, "", encodeDigest(zero))

	decoded, err = decodeDigest("")
	require.NoError(t, err)
	require.Equal(t, crypto.Digest{}, decoded)
}
