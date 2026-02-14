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

package node

import (
	"path/filepath"

	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/data/bookkeeping"
	"github.com/algorand/go-algorand/ledger/ledgercore"
	"github.com/algorand/go-algorand/ledger/simulation"
	"github.com/algorand/go-algorand/logging"
	"github.com/algorand/go-algorand/nimbus"
)

// AlgorandNimbusNode extends follower mode with Nimbus features.
type AlgorandNimbusNode struct {
	*AlgorandFollowerNode
	hooks *nimbus.HookManager
}

// MakeNimbus sets up an Algorand Nimbus node.
func MakeNimbus(log logging.Logger, rootDir string, cfg config.Local, phonebookAddresses []string, genesis bookkeeping.Genesis) (*AlgorandNimbusNode, error) {
	follower, err := MakeFollower(log, rootDir, cfg, phonebookAddresses, genesis)
	if err != nil {
		return nil, err
	}

	hooksDir := filepath.Join(follower.genesisDirs.HotGenesisDir, "nimbus")
	hooks, err := nimbus.NewHookManager(log, cfg, follower.ledger, hooksDir, follower.genesisID, follower.genesisHash)
	if err != nil {
		return nil, err
	}
	follower.ledger.RegisterBlockListeners([]ledgercore.BlockListener{hooks})

	return &AlgorandNimbusNode{
		AlgorandFollowerNode: follower,
		hooks:                hooks,
	}, nil
}

// Simulate speculatively runs a transaction group against the current blockchain state in Nimbus mode.
func (node *AlgorandNimbusNode) Simulate(request simulation.Request) (result simulation.Result, err error) {
	request.NimbusMode = true
	simulator := simulation.MakeSimulator(node.ledger, node.config.EnableDeveloperAPI)
	return simulator.Simulate(request)
}

// Hooks returns the hook manager.
func (node *AlgorandNimbusNode) Hooks() *nimbus.HookManager {
	return node.hooks
}

// Stop stops running the node and Nimbus services.
func (node *AlgorandNimbusNode) Stop() {
	if node.hooks != nil {
		node.hooks.Stop()
	}
	node.AlgorandFollowerNode.Stop()
}
