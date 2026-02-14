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

package test

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/algorand/go-algorand/config"
	v2 "github.com/algorand/go-algorand/daemon/algod/api/server/v2"
	"github.com/algorand/go-algorand/logging"
	"github.com/algorand/go-algorand/nimbus"
	"github.com/algorand/go-algorand/node"
	"github.com/algorand/go-algorand/test/partitiontest"
)

type hookNode struct {
	*mockNode
	hooks *nimbus.HookManager
}

func (n hookNode) Hooks() *nimbus.HookManager { return n.hooks }

func setupNimbusHooks(t *testing.T) (v2.Handlers, *nimbus.HookManager, func()) {
	mockLedger, _, _, _, releasefunc := testingenv(t, 1, 1, true)
	cfg := config.GetDefaultLocal()
	cfg.EnableNimbusMode = true
	manager, err := nimbus.NewHookManager(logging.Base(), cfg, mockLedger, t.TempDir(), t.Name(), mockLedger.GenesisHash())
	require.NoError(t, err)

	mockNode := makeMockNode(mockLedger, t.Name(), nil, node.StatusReport{}, false)
	handler := v2.Handlers{
		Node:     hookNode{mockNode: mockNode, hooks: manager},
		Log:      logging.Base(),
		Shutdown: make(chan struct{}),
	}
	return handler, manager, releasefunc
}

func TestNimbusHooksCreateListStateHistory(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()

	handler, _, releasefunc := setupNimbusHooks(t)
	defer releasefunc()

	program := base64.StdEncoding.EncodeToString([]byte{0x01})
	initialState := base64.StdEncoding.EncodeToString([]byte("seed"))
	body := []byte(`{"id":"hook-1","program":"` + program + `","require-origin":false,"initial-state":"` + initialState + `"}`)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/v2/nimbus/hooks", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	err := handler.CreateNimbusHook(ctx)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, rec.Code)

	reqList := httptest.NewRequest(http.MethodGet, "/v2/nimbus/hooks", nil)
	recList := httptest.NewRecorder()
	ctxList := e.NewContext(reqList, recList)
	err = handler.ListNimbusHooks(ctxList)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recList.Code)

	reqState := httptest.NewRequest(http.MethodGet, "/v2/nimbus/hooks/hook-1/state", nil)
	recState := httptest.NewRecorder()
	ctxState := e.NewContext(reqState, recState)
	ctxState.SetParamNames("hookID")
	ctxState.SetParamValues("hook-1")
	err = handler.GetNimbusHookState(ctxState)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recState.Code)

	reqHistory := httptest.NewRequest(http.MethodGet, "/v2/nimbus/hooks/hook-1/history", nil)
	recHistory := httptest.NewRecorder()
	ctxHistory := e.NewContext(reqHistory, recHistory)
	ctxHistory.SetParamNames("hookID")
	ctxHistory.SetParamValues("hook-1")
	err = handler.GetNimbusHookHistory(ctxHistory)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recHistory.Code)
}
