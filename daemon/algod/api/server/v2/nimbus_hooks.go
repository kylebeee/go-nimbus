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

package v2

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/nimbus"
	"github.com/algorand/go-algorand/protocol"
)

type nimbusHookCreateRequest struct {
	ID            string `json:"id"`
	Program       string `json:"program"`
	RequireOrigin bool   `json:"require-origin"`
	InitialState  string `json:"initial-state,omitempty"`
}

type nimbusHookInfoResponse struct {
	ID            string `json:"id"`
	ProgramHash   string `json:"program-hash"`
	RequireOrigin bool   `json:"require-origin"`
	CreatedAt     string `json:"created-at"`
}

type nimbusHookStateResponse struct {
	Round       basics.Round `json:"round"`
	State       string       `json:"state,omitempty"`
	Error       string       `json:"error,omitempty"`
	Timestamp   string       `json:"timestamp"`
	BlockHash   string       `json:"block-hash,omitempty"`
	ProgramHash string       `json:"program-hash,omitempty"`
	StateHash   string       `json:"state-hash,omitempty"`
	PrevHash    string       `json:"prev-hash,omitempty"`
	ReceiptHash string       `json:"receipt-hash,omitempty"`
}

type nimbusHooksListResponse struct {
	Hooks []nimbusHookInfoResponse `json:"hooks"`
}

type nimbusHookHistoryResponse struct {
	History []nimbusHookStateResponse `json:"history"`
}

func hookStateToResponse(state nimbus.HookState) nimbusHookStateResponse {
	return nimbusHookStateResponse{
		Round:       state.Round,
		State:       encodeOptionalBase64(state.State),
		Error:       state.Error,
		Timestamp:   state.Timestamp.UTC().Format(time.RFC3339Nano),
		BlockHash:   state.BlockHash.String(),
		ProgramHash: state.ProgramHash.String(),
		StateHash:   state.StateHash.String(),
		PrevHash:    state.PrevHash.String(),
		ReceiptHash: state.ReceiptHash.String(),
	}
}

func (v2 *Handlers) hookManager() (*nimbus.HookManager, bool) {
	managerProvider, ok := v2.Node.(interface{ Hooks() *nimbus.HookManager })
	if !ok || managerProvider.Hooks() == nil {
		return nil, false
	}
	return managerProvider.Hooks(), true
}

// ListNimbusHooks lists registered hooks.
func (v2 *Handlers) ListNimbusHooks(ctx echo.Context) error {
	manager, ok := v2.hookManager()
	if !ok {
		return notFound(ctx, errors.New("nimbus hooks disabled"), errInternalFailure, v2.Log)
	}

	hooks := manager.ListHooks()
	response := nimbusHooksListResponse{
		Hooks: make([]nimbusHookInfoResponse, 0, len(hooks)),
	}
	for _, hook := range hooks {
		response.Hooks = append(response.Hooks, nimbusHookInfoResponse{
			ID:            hook.ID,
			ProgramHash:   hook.ProgramHash.String(),
			RequireOrigin: hook.RequireOrigin,
			CreatedAt:     hook.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return ctx.JSON(http.StatusOK, response)
}

// GetNimbusHook returns metadata for a hook.
func (v2 *Handlers) GetNimbusHook(ctx echo.Context) error {
	manager, ok := v2.hookManager()
	if !ok {
		return notFound(ctx, errors.New("nimbus hooks disabled"), errInternalFailure, v2.Log)
	}
	hookID := ctx.Param("hookID")
	info, ok := manager.GetHook(hookID)
	if !ok {
		return notFound(ctx, errors.New("hook not found"), "hook not found", v2.Log)
	}
	response := nimbusHookInfoResponse{
		ID:            info.ID,
		ProgramHash:   info.ProgramHash.String(),
		RequireOrigin: info.RequireOrigin,
		CreatedAt:     info.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	return ctx.JSON(http.StatusOK, response)
}

// CreateNimbusHook registers a new hook.
func (v2 *Handlers) CreateNimbusHook(ctx echo.Context) error {
	manager, ok := v2.hookManager()
	if !ok {
		return notFound(ctx, errors.New("nimbus hooks disabled"), errInternalFailure, v2.Log)
	}

	requestBuffer := new(bytes.Buffer)
	requestBodyReader := http.MaxBytesReader(nil, ctx.Request().Body, MaxTealDryrunBytes)
	if _, err := requestBuffer.ReadFrom(requestBodyReader); err != nil {
		return badRequest(ctx, err, err.Error(), v2.Log)
	}

	var request nimbusHookCreateRequest
	if err := decode(protocol.JSONStrictHandle, requestBuffer.Bytes(), &request); err != nil {
		return badRequest(ctx, err, err.Error(), v2.Log)
	}

	program, err := base64.StdEncoding.DecodeString(request.Program)
	if err != nil {
		return badRequest(ctx, err, "program must be base64 encoded", v2.Log)
	}
	initialState, err := decodeOptionalBase64(request.InitialState)
	if err != nil {
		return badRequest(ctx, err, "initial-state must be base64 encoded", v2.Log)
	}

	err = manager.CreateHook(nimbus.HookDefinition{
		ID:            request.ID,
		Program:       program,
		RequireOrigin: request.RequireOrigin,
		InitialState:  initialState,
	})
	if err != nil {
		return badRequest(ctx, err, err.Error(), v2.Log)
	}

	return ctx.NoContent(http.StatusNoContent)
}

// DeleteNimbusHook deletes a hook.
func (v2 *Handlers) DeleteNimbusHook(ctx echo.Context) error {
	manager, ok := v2.hookManager()
	if !ok {
		return notFound(ctx, errors.New("nimbus hooks disabled"), errInternalFailure, v2.Log)
	}
	hookID := ctx.Param("hookID")
	if err := manager.DeleteHook(hookID); err != nil {
		return notFound(ctx, err, err.Error(), v2.Log)
	}
	return ctx.NoContent(http.StatusNoContent)
}

// GetNimbusHookState returns the latest hook state.
func (v2 *Handlers) GetNimbusHookState(ctx echo.Context) error {
	manager, ok := v2.hookManager()
	if !ok {
		return notFound(ctx, errors.New("nimbus hooks disabled"), errInternalFailure, v2.Log)
	}
	hookID := ctx.Param("hookID")
	state, ok := manager.LatestState(hookID)
	if !ok {
		return notFound(ctx, errors.New("hook not found"), "hook not found", v2.Log)
	}
	return ctx.JSON(http.StatusOK, hookStateToResponse(state))
}

// GetNimbusHookHistory returns hook state history.
func (v2 *Handlers) GetNimbusHookHistory(ctx echo.Context) error {
	manager, ok := v2.hookManager()
	if !ok {
		return notFound(ctx, errors.New("nimbus hooks disabled"), errInternalFailure, v2.Log)
	}
	hookID := ctx.Param("hookID")

	from, to, err := parseHistoryRange(ctx)
	if err != nil {
		return badRequest(ctx, err, err.Error(), v2.Log)
	}

	history, err := manager.History(hookID, from, to)
	if err != nil {
		return notFound(ctx, err, err.Error(), v2.Log)
	}

	response := nimbusHookHistoryResponse{
		History: make([]nimbusHookStateResponse, 0, len(history)),
	}
	for _, entry := range history {
		response.History = append(response.History, hookStateToResponse(entry))
	}
	return ctx.JSON(http.StatusOK, response)
}

// VerifyNimbusHookChain verifies the receipt chain for a hook.
func (v2 *Handlers) VerifyNimbusHookChain(ctx echo.Context) error {
	manager, ok := v2.hookManager()
	if !ok {
		return notFound(ctx, errors.New("nimbus hooks disabled"), errInternalFailure, v2.Log)
	}
	hookID := ctx.Param("hookID")

	program, ok := manager.HookProgram(hookID)
	if !ok {
		return notFound(ctx, errors.New("hook not found"), "hook not found", v2.Log)
	}

	history, err := manager.History(hookID, nil, nil)
	if err != nil {
		return internalError(ctx, err, err.Error(), v2.Log)
	}

	result := nimbus.VerifyChain(history, program)
	return ctx.JSON(http.StatusOK, result)
}

func parseHistoryRange(ctx echo.Context) (*basics.Round, *basics.Round, error) {
	var fromRound *basics.Round
	var toRound *basics.Round

	if value := ctx.QueryParam("from"); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid from round: %w", err)
		}
		r := basics.Round(parsed)
		fromRound = &r
	}
	if value := ctx.QueryParam("to"); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid to round: %w", err)
		}
		r := basics.Round(parsed)
		toRound = &r
	}
	return fromRound, toRound, nil
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
