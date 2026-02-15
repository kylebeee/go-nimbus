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
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/data/bookkeeping"
	"github.com/algorand/go-algorand/data/transactions"
	"github.com/algorand/go-algorand/data/txntest"
	"github.com/algorand/go-algorand/ledger/ledgercore"
	"github.com/algorand/go-algorand/ledger/simulation"
	"github.com/algorand/go-algorand/logging"
	"github.com/algorand/go-algorand/protocol"
)

const hookWorkBuffer = 128

// HookDefinition holds the configuration for a Nimbus hook.
type HookDefinition struct {
	ID            string
	Program       []byte
	RequireOrigin bool
	CreatedAt     time.Time
	InitialState  []byte
}

// HookState is a derived state entry for a hook.
type HookState struct {
	Round     basics.Round
	State     []byte
	Error     string
	Timestamp time.Time

	// Cryptographic integrity chain
	BlockHash   crypto.Digest // crypto.HashObj(blockHeader)
	ProgramHash crypto.Digest // crypto.Hash(program)
	StateHash   crypto.Digest // crypto.Hash(state.State)
	PrevHash    crypto.Digest // Previous round's ReceiptHash (zero for genesis)
	ReceiptHash crypto.Digest // Hash of this receipt (anchor for next round)
}

// HookInfo is a summarized hook metadata response.
type HookInfo struct {
	ID            string
	ProgramHash   crypto.Digest
	RequireOrigin bool
	CreatedAt     time.Time
}

// HookManager manages Nimbus hooks and their derived state.
type HookManager struct {
	log         logging.Logger
	cfg         config.Local
	ledger      *data.Ledger
	store       *hookStore
	genesisID   string
	genesisHash crypto.Digest

	mu          sync.RWMutex
	hooks       map[string]HookDefinition
	latest      map[string]HookState
	backfilling map[string]bool

	workCh chan bookkeeping.Block
	stopCh chan struct{}
}

// NewHookManager creates a hook manager and loads persisted hooks.
func NewHookManager(log logging.Logger, cfg config.Local, ledger *data.Ledger, dir string, genesisID string, genesisHash crypto.Digest) (*HookManager, error) {
	store := newHookStore(dir)
	defs, err := store.loadHooks()
	if err != nil {
		return nil, err
	}

	manager := &HookManager{
		log:         log,
		cfg:         cfg,
		ledger:      ledger,
		store:       store,
		genesisID:   genesisID,
		genesisHash: genesisHash,
		hooks:       make(map[string]HookDefinition),
		latest:      make(map[string]HookState),
		backfilling: make(map[string]bool),
		workCh:      make(chan bookkeeping.Block, hookWorkBuffer),
		stopCh:      make(chan struct{}),
	}

	for _, def := range defs {
		manager.hooks[def.ID] = def
		latest, ok, err := store.loadLatestHistory(def.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			manager.latest[def.ID] = latest
		} else {
			initial := HookState{
				Round:     0,
				State:     def.InitialState,
				Timestamp: time.Now(),
			}
			computeGenesisReceipt(&initial, def.Program)
			manager.latest[def.ID] = initial
			if err := store.appendHistory(def.ID, initial); err != nil {
				return nil, err
			}
		}
	}

	go manager.run()
	return manager, nil
}

// Stop stops hook processing.
func (m *HookManager) Stop() {
	close(m.stopCh)
}

// OnNewBlock implements ledgercore.BlockListener.
func (m *HookManager) OnNewBlock(block bookkeeping.Block, _ ledgercore.StateDelta) {
	select {
	case m.workCh <- block:
	default:
		go func() {
			m.workCh <- block
		}()
	}
}

// ListHooks returns hook metadata.
func (m *HookManager) ListHooks() []HookInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]HookInfo, 0, len(m.hooks))
	for _, def := range m.hooks {
		out = append(out, HookInfo{
			ID:            def.ID,
			ProgramHash:   crypto.Hash(def.Program),
			RequireOrigin: def.RequireOrigin,
			CreatedAt:     def.CreatedAt,
		})
	}
	return out
}

// GetHook returns hook metadata by ID.
func (m *HookManager) GetHook(id string) (HookInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	def, ok := m.hooks[id]
	if !ok {
		return HookInfo{}, false
	}
	return HookInfo{
		ID:            def.ID,
		ProgramHash:   crypto.Hash(def.Program),
		RequireOrigin: def.RequireOrigin,
		CreatedAt:     def.CreatedAt,
	}, true
}

// CreateHook registers and persists a hook.
func (m *HookManager) CreateHook(def HookDefinition) error {
	if def.ID == "" {
		return errors.New("hook id is required")
	}
	if len(def.Program) == 0 {
		return errors.New("hook program is required")
	}
	if (def.RequireOrigin || len(def.InitialState) == 0) && !m.cfg.Archival {
		return errors.New("archival mode required for hooks without initial state")
	}

	m.mu.Lock()
	if _, exists := m.hooks[def.ID]; exists {
		m.mu.Unlock()
		return fmt.Errorf("hook %s already exists", def.ID)
	}
	def.CreatedAt = time.Now()
	m.hooks[def.ID] = def
	initial := HookState{
		Round:     0,
		State:     def.InitialState,
		Timestamp: time.Now(),
	}
	computeGenesisReceipt(&initial, def.Program)
	m.latest[def.ID] = initial
	m.mu.Unlock()

	if err := m.persistHooks(); err != nil {
		return err
	}
	if err := m.store.appendHistory(def.ID, initial); err != nil {
		return err
	}

	if def.RequireOrigin || len(def.InitialState) == 0 {
		m.mu.Lock()
		m.backfilling[def.ID] = true
		m.mu.Unlock()
		go func() {
			if err := m.backfillHook(def.ID); err != nil {
				m.log.Warnf("nimbus hook %s backfill failed: %v", def.ID, err)
			}
			m.mu.Lock()
			delete(m.backfilling, def.ID)
			m.mu.Unlock()
		}()
	}
	return nil
}

// DeleteHook removes a hook and its history.
func (m *HookManager) DeleteHook(id string) error {
	m.mu.Lock()
	if _, ok := m.hooks[id]; !ok {
		m.mu.Unlock()
		return fmt.Errorf("hook %s not found", id)
	}
	delete(m.hooks, id)
	delete(m.latest, id)
	m.mu.Unlock()

	if err := m.persistHooks(); err != nil {
		return err
	}
	return m.store.deleteHistory(id)
}

// LatestState returns the latest hook state.
// During backfill this returns whatever has been processed so far.
func (m *HookManager) LatestState(id string) (HookState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.latest[id]
	if !ok {
		return HookState{}, fmt.Errorf("hook %s not found", id)
	}
	return state, nil
}

// IsBackfilling reports whether a hook is still catching up from genesis.
func (m *HookManager) IsBackfilling(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.backfilling[id]
}

// History returns hook state history.
// During backfill this returns whatever history has been processed so far.
func (m *HookManager) History(id string, from, to *basics.Round) ([]HookState, error) {
	m.mu.RLock()
	_, ok := m.hooks[id]
	if !ok {
		m.mu.RUnlock()
		return nil, fmt.Errorf("hook %s not found", id)
	}
	m.mu.RUnlock()
	return m.store.readHistory(id, from, to)
}

// HookProgram returns the raw program bytes for a hook.
func (m *HookManager) HookProgram(id string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	def, ok := m.hooks[id]
	if !ok {
		return nil, false
	}
	return def.Program, true
}

func (m *HookManager) run() {
	for {
		select {
		case <-m.stopCh:
			return
		case block := <-m.workCh:
			m.processBlock(block)
		}
	}
}

func (m *HookManager) processBlock(block bookkeeping.Block) {
	m.mu.RLock()
	hooks := make([]HookDefinition, 0, len(m.hooks))
	for _, def := range m.hooks {
		hooks = append(hooks, def)
	}
	m.mu.RUnlock()

	for _, def := range hooks {
		prev := m.getLatestState(def.ID)
		next := m.evaluateHook(def, block, prev, block.BlockHeader.Round)
		m.mu.Lock()
		m.latest[def.ID] = next
		m.mu.Unlock()
		if err := m.store.appendHistory(def.ID, next); err != nil {
			m.log.Warnf("nimbus hook %s failed to persist history: %v", def.ID, err)
		}
	}
}

func (m *HookManager) getLatestState(id string) HookState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.latest[id]
	if !ok {
		return HookState{}
	}
	return state
}

func (m *HookManager) evaluateHook(def HookDefinition, block bookkeeping.Block, prev HookState, simRound basics.Round) HookState {
	hdr := block.BlockHeader
	now := time.Now()
	state := HookState{
		Round:     hdr.Round,
		State:     prev.State,
		Timestamp: now,
	}

	proto, ok := config.Consensus[hdr.CurrentProtocol]
	if !ok {
		state.Error = fmt.Sprintf("unsupported protocol %s", hdr.CurrentProtocol)
		computeReceipt(&state, def.Program, hdr, prev.ReceiptHash)
		return state
	}

	txnTypes := encodeBlockTransactions(block)

	tx := txntest.Txn{
		Type:              protocol.ApplicationCallTx,
		Sender:            hdr.FeeSink,
		FirstValid:        simRound,
		GenesisID:         m.genesisID,
		GenesisHash:       m.genesisHash,
		ApplicationID:     0,
		OnCompletion:      transactions.NoOpOC,
		ApplicationArgs:   [][]byte{prev.State, txnTypes},
		ApprovalProgram:   def.Program,
		ClearStateProgram: def.Program,
	}
	tx.FillDefaults(proto)
	stxn := tx.SignedTxn()

	request := simulation.Request{
		Round:                simRound,
		TxnGroups:            [][]transactions.SignedTxn{{stxn}},
		AllowEmptySignatures: true,
		FixSigners:           true,
		NimbusMode:           true,
	}

	result, err := simulation.MakeSimulator(m.ledger, m.cfg.EnableDeveloperAPI).Simulate(request)
	if err != nil {
		state.Error = err.Error()
		computeReceipt(&state, def.Program, hdr, prev.ReceiptHash)
		return state
	}
	if len(result.TxnGroups) == 0 || len(result.TxnGroups[0].Txns) == 0 {
		state.Error = "hook simulation returned empty result"
		computeReceipt(&state, def.Program, hdr, prev.ReceiptHash)
		return state
	}

	logs := result.TxnGroups[0].Txns[0].Txn.ApplyData.EvalDelta.Logs
	if len(logs) == 0 {
		state.Error = "hook did not emit ARC4 ABI log output"
		computeReceipt(&state, def.Program, hdr, prev.ReceiptHash)
		return state
	}
	state.State = []byte(logs[len(logs)-1])
	computeReceipt(&state, def.Program, hdr, prev.ReceiptHash)
	return state
}

// txTypeEnum maps protocol transaction types to single-byte enum values.
// Hooks receive this in ApplicationArgs[1]: one byte per transaction in the block.
var txTypeEnum = map[protocol.TxType]byte{
	protocol.PaymentTx:         0x01,
	protocol.KeyRegistrationTx: 0x02,
	protocol.AssetConfigTx:     0x03,
	protocol.AssetTransferTx:   0x04,
	protocol.AssetFreezeTx:     0x05,
	protocol.ApplicationCallTx: 0x06,
	protocol.StateProofTx:      0x07,
	protocol.HeartbeatTx:       0x08,
}

// encodeBlockTransactions encodes the transaction types from a block as a
// packed byte array with one byte per transaction. The byte values correspond
// to txTypeEnum. Unknown types encode as 0x00.
func encodeBlockTransactions(block bookkeeping.Block) []byte {
	out := make([]byte, len(block.Payset))
	for i, txib := range block.Payset {
		if b, ok := txTypeEnum[txib.Txn.Type]; ok {
			out[i] = b
		}
	}
	return out
}

func (m *HookManager) backfillHook(id string) error {
	m.mu.RLock()
	def, ok := m.hooks[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("hook %s not found", id)
	}

	latestRound := m.ledger.Latest()
	for round := basics.Round(1); round <= latestRound; round++ {
		block, err := m.ledger.Block(round)
		if err != nil {
			return err
		}
		prev := m.getLatestState(def.ID)
		// Use the latest round for simulation so the evaluator can access
		// current ledger state. Hooks don't read on-chain state; they only
		// use the args we provide, so the simulation round doesn't affect
		// the result. The receipt still uses the historical block header.
		next := m.evaluateHook(def, block, prev, latestRound)
		m.mu.Lock()
		m.latest[def.ID] = next
		m.mu.Unlock()
		if err := m.store.appendHistory(def.ID, next); err != nil {
			return err
		}
	}
	return nil
}

func (m *HookManager) persistHooks() error {
	m.mu.RLock()
	defs := make([]HookDefinition, 0, len(m.hooks))
	for _, def := range m.hooks {
		defs = append(defs, def)
	}
	m.mu.RUnlock()
	return m.store.saveHooks(defs)
}
