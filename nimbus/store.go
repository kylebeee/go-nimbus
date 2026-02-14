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
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
)

const (
	hooksFilename   = "hooks.json"
	historyDirname  = "history"
	historyFileMode = 0o600
)

type hookStore struct {
	dir string
	mu  sync.Mutex
}

type hookDefinitionJSON struct {
	ID           string `json:"id"`
	Program      string `json:"program"`
	RequireOrigin bool   `json:"require_origin"`
	CreatedAt    string `json:"created_at"`
	InitialState string `json:"initial_state,omitempty"`
}

type hookHistoryJSON struct {
	Round       basics.Round `json:"round"`
	State       string       `json:"state,omitempty"`
	Error       string       `json:"error,omitempty"`
	Timestamp   string       `json:"timestamp"`
	BlockHash   string       `json:"block_hash,omitempty"`
	ProgramHash string       `json:"program_hash,omitempty"`
	StateHash   string       `json:"state_hash,omitempty"`
	PrevHash    string       `json:"prev_hash,omitempty"`
	ReceiptHash string       `json:"receipt_hash,omitempty"`
}

func newHookStore(dir string) *hookStore {
	return &hookStore{dir: dir}
}

func (s *hookStore) ensureDirs() error {
	if err := os.MkdirAll(filepath.Join(s.dir, historyDirname), 0o700); err != nil {
		return err
	}
	return nil
}

func (s *hookStore) hooksPath() string {
	return filepath.Join(s.dir, hooksFilename)
}

func (s *hookStore) historyPath(id string) string {
	return filepath.Join(s.dir, historyDirname, fmt.Sprintf("%s.jsonl", id))
}

func (s *hookStore) loadHooks() ([]HookDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDirs(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.hooksPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var raw []hookDefinitionJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	defs := make([]HookDefinition, 0, len(raw))
	for _, item := range raw {
		program, err := base64.StdEncoding.DecodeString(item.Program)
		if err != nil {
			return nil, err
		}
		initialState, err := decodeOptionalBase64(item.InitialState)
		if err != nil {
			return nil, err
		}
		createdAt, err := parseTimestamp(item.CreatedAt)
		if err != nil {
			return nil, err
		}
		defs = append(defs, HookDefinition{
			ID:           item.ID,
			Program:      program,
			RequireOrigin: item.RequireOrigin,
			CreatedAt:    createdAt,
			InitialState: initialState,
		})
	}
	return defs, nil
}

func (s *hookStore) saveHooks(defs []HookDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDirs(); err != nil {
		return err
	}
	raw := make([]hookDefinitionJSON, 0, len(defs))
	for _, def := range defs {
		raw = append(raw, hookDefinitionJSON{
			ID:           def.ID,
			Program:      base64.StdEncoding.EncodeToString(def.Program),
			RequireOrigin: def.RequireOrigin,
			CreatedAt:    formatTimestamp(def.CreatedAt),
			InitialState: encodeOptionalBase64(def.InitialState),
		})
	}
	payload, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}

	tmp := s.hooksPath() + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.hooksPath())
}

func (s *hookStore) appendHistory(id string, entry HookState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDirs(); err != nil {
		return err
	}
	file, err := os.OpenFile(s.historyPath(id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, historyFileMode)
	if err != nil {
		return err
	}
	defer file.Close()

	record := hookHistoryJSON{
		Round:       entry.Round,
		State:       encodeOptionalBase64(entry.State),
		Error:       entry.Error,
		Timestamp:   formatTimestamp(entry.Timestamp),
		BlockHash:   encodeDigest(entry.BlockHash),
		ProgramHash: encodeDigest(entry.ProgramHash),
		StateHash:   encodeDigest(entry.StateHash),
		PrevHash:    encodeDigest(entry.PrevHash),
		ReceiptHash: encodeDigest(entry.ReceiptHash),
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *hookStore) loadLatestHistory(id string) (HookState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.historyPath(id)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return HookState{}, false, nil
		}
		return HookState{}, false, err
	}
	defer file.Close()

	var last hookHistoryJSON
	reader := bufio.NewReader(file)
	found := false
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) != 0 {
			found = true
			line = bytes.TrimSpace(line)
			if err := json.Unmarshal(line, &last); err != nil {
				return HookState{}, false, err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return HookState{}, false, err
		}
	}
	if !found {
		return HookState{}, false, nil
	}

	return decodeHistoryRecord(last)
}

func (s *hookStore) readHistory(id string, from, to *basics.Round) ([]HookState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.historyPath(id)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var entries []HookState
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) == 0 && err == io.EOF {
			break
		}
		if err != nil && err != io.EOF {
			return nil, err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			if err == io.EOF {
				break
			}
			continue
		}
		var record hookHistoryJSON
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, err
		}
		if from != nil && record.Round < *from {
			if err == io.EOF {
				break
			}
			continue
		}
		if to != nil && record.Round > *to {
			if err == io.EOF {
				break
			}
			continue
		}
		entry, _, decErr := decodeHistoryRecord(record)
		if decErr != nil {
			return nil, decErr
		}
		entries = append(entries, entry)
		if err == io.EOF {
			break
		}
	}
	return entries, nil
}

func (s *hookStore) deleteHistory(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.historyPath(id)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func decodeHistoryRecord(record hookHistoryJSON) (HookState, bool, error) {
	state, err := decodeOptionalBase64(record.State)
	if err != nil {
		return HookState{}, false, err
	}
	timestamp, err := parseTimestamp(record.Timestamp)
	if err != nil {
		return HookState{}, false, err
	}
	blockHash, err := decodeDigest(record.BlockHash)
	if err != nil {
		return HookState{}, false, err
	}
	programHash, err := decodeDigest(record.ProgramHash)
	if err != nil {
		return HookState{}, false, err
	}
	stateHash, err := decodeDigest(record.StateHash)
	if err != nil {
		return HookState{}, false, err
	}
	prevHash, err := decodeDigest(record.PrevHash)
	if err != nil {
		return HookState{}, false, err
	}
	receiptHash, err := decodeDigest(record.ReceiptHash)
	if err != nil {
		return HookState{}, false, err
	}
	return HookState{
		Round:       record.Round,
		State:       state,
		Error:       record.Error,
		Timestamp:   timestamp,
		BlockHash:   blockHash,
		ProgramHash: programHash,
		StateHash:   stateHash,
		PrevHash:    prevHash,
		ReceiptHash: receiptHash,
	}, true, nil
}

func encodeDigest(d crypto.Digest) string {
	if d.IsZero() {
		return ""
	}
	return d.String()
}

func decodeDigest(s string) (crypto.Digest, error) {
	if s == "" {
		return crypto.Digest{}, nil
	}
	return crypto.DigestFromString(s)
}

func encodeOptionalBase64(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func decodeOptionalBase64(data string) ([]byte, error) {
	if data == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(data)
}
