// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package ethapi

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/misc/eip1559"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

const anvil0Key = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

func TestAddress32TransactionRPC(t *testing.T) {
	key, err := crypto.HexToECDSA(anvil0Key)

	if err != nil {
		t.Fatal(err)
	}

	sender := crypto.PubkeyToAddress(key.PublicKey)
	var recipient common.Address32
	recipient[0] = 0x01
	recipient[31] = 0x42
	cfg := *params.OptimismTestConfig
	ts := uint64(0)
	cfg.Address32Time = &ts
	cfg.JovianTime = nil
	word, err := types.Address32WordFromBytes(recipient[:])

	if err != nil {
		t.Fatal(err)
	}

	signed, err := types.SignTx(types.NewTx(&types.Address32Tx{
		ChainID:    cfg.ChainID,
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(2),
		Gas:        params.TxGas,
		To:         word,
		Value:      big.NewInt(1),
		SenderKind: types.SenderKindAddress20,
	}), types.LatestSigner(&cfg), key)

	if err != nil {
		t.Fatal(err)
	}

	extra := eip1559.EncodeHoloceneExtraData(250, 6)
	genesis := &core.Genesis{
		Config:     &cfg,
		Difficulty: big.NewInt(0),
		ExtraData:  extra,
		GasLimit:   30_000_000,
		BaseFee:    big.NewInt(1),
		Alloc: types.GenesisAlloc{
			sender: {Balance: big.NewInt(params.Ether)},
			types.L1BlockAddr: {Storage: map[common.Hash]common.Hash{
				types.L1FeeScalarsSlot:      {19: 1},
				types.L1BaseFeeSlot:         common.BigToHash(big.NewInt(1_000_000)),
				types.OperatorFeeParamsSlot: {31: 10},
			}},
		},
	}
	backend := newTestBackend(t, 1, genesis, beacon.New(ethash.NewFaker()), func(i int, b *core.BlockGen) {
		b.SetExtra(extra)
		if i == 0 {
			b.AddTx(signed)
		}
	})
	defer backend.chain.Stop()

	server := rpc.NewServer()
	defer server.Stop()

	if err := server.RegisterName("eth", NewBlockChainAPI(backend)); err != nil {
		t.Fatal(err)
	}

	if err := server.RegisterName("eth", NewTransactionAPI(backend, new(AddrLocker))); err != nil {
		t.Fatal(err)
	}

	client := rpc.DialInProc(server)
	defer client.Close()

	var got map[string]any

	if err := client.Call(&got, "eth_getTransactionByHash", signed.Hash()); err != nil {
		t.Fatal(err)
	}

	from, _ := got["from"].(string)
	to, _ := got["to"].(string)
	kind, _ := got["senderKind"].(string)
	var padded common.Address32
	copy(padded[12:], sender[:])

	if from != "0x"+hex.EncodeToString(padded[:]) {
		t.Fatalf("from = %s", from)
	}

	if to != "0x"+hex.EncodeToString(recipient[:]) {
		t.Fatalf("to = %s", to)
	}

	if kind != "0x0" {
		t.Fatalf("senderKind = %s", kind)
	}

	call := map[string]any{
		"from":       "0x" + hex.EncodeToString(padded[:]),
		"to":         "0x" + hex.EncodeToString(recipient[:]),
		"senderKind": "0x0",
		"value":      "0x0",
		"gas":        "0x5208",
	}
	var ret string

	if err := client.Call(&ret, "eth_call", call, "latest"); err != nil {
		t.Fatal(err)
	}

	if ret != "0x" {
		t.Fatalf("eth_call = %s", ret)
	}

	var estimate string

	if err := client.Call(&estimate, "eth_estimateGas", call, "latest"); err != nil {
		t.Fatal(err)
	}

	if estimate != "0x5208" {
		t.Fatalf("eth_estimateGas = %s, want 0x5208", estimate)
	}

	var receipt map[string]any

	if err := client.Call(&receipt, "eth_getTransactionReceipt", signed.Hash()); err != nil {
		t.Fatal(err)
	}

	if receipt["from"] != from || receipt["to"] != to || receipt["senderKind"] != kind {
		t.Fatalf("receipt from %v to %v senderKind %v", receipt["from"], receipt["to"], receipt["senderKind"])
	}

	var slot common.Hash
	slot[31] = 1
	listed := map[string]any{
		"from":       call["from"],
		"to":         call["to"],
		"senderKind": "0x0",
		"value":      "0x0",
		"gas":        "0x62d3",
		"accessList": []map[string]any{{
			"address":     "0x" + hex.EncodeToString(recipient[:]),
			"storageKeys": []string{slot.Hex()},
		}},
	}
	// 21000 + 2400 address + 1900 storage key = 25300 (0x62d4). One less gas is rejected.
	if err := client.Call(&ret, "eth_call", listed, "latest"); err == nil {
		t.Fatal("access-list call succeeded below intrinsic gas")
	}

	listed["gas"] = "0x62d4"

	if err := client.Call(&ret, "eth_call", listed, "latest"); err != nil {
		t.Fatal(err)
	}

	delete(listed, "gas")

	if err := client.Call(&estimate, "eth_estimateGas", listed, "latest"); err != nil {
		t.Fatal(err)
	}

	gotGas, ok := new(big.Int).SetString(estimate, 0)

	if !ok || gotGas.Cmp(big.NewInt(25300)) < 0 {
		t.Fatalf("access-list estimate = %s, want at least 25300", estimate)
	}
}

func TestAddress32CreateReceiptAndCodeRPC(t *testing.T) {
	key, err := crypto.HexToECDSA(anvil0Key)

	if err != nil {
		t.Fatal(err)
	}

	sender, err := crypto.PubkeyToAddress32(key.PublicKey)

	if err != nil {
		t.Fatal(err)
	}

	cfg := *params.OptimismTestConfig
	ts := uint64(0)
	cfg.Address32Time = &ts
	cfg.JovianTime = nil
	initcode := []byte{0x60, 0x42, 0x60, 0x00, 0x52, 0x60, 0x01, 0x60, 0x1f, 0xf3}
	signed, err := types.SignTx(types.NewTx(&types.Address32Tx{
		ChainID:    cfg.ChainID,
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(2),
		Gas:        500_000,
		Value:      big.NewInt(0),
		Data:       initcode,
		SenderKind: types.SenderKindAddress32,
	}), types.LatestSigner(&cfg), key)

	if err != nil {
		t.Fatal(err)
	}

	extra := eip1559.EncodeHoloceneExtraData(250, 6)
	genesis := &core.Genesis{
		Config:     &cfg,
		Difficulty: big.NewInt(0),
		ExtraData:  extra,
		GasLimit:   30_000_000,
		BaseFee:    big.NewInt(1),
		Alloc: types.GenesisAlloc{
			types.L1BlockAddr: {Storage: map[common.Hash]common.Hash{
				types.L1FeeScalarsSlot:      {19: 1},
				types.L1BaseFeeSlot:         common.BigToHash(big.NewInt(1_000_000)),
				types.OperatorFeeParamsSlot: {31: 10},
			}},
		},
		Address32Alloc: map[common.Address32]types.Account{
			sender: {Balance: big.NewInt(params.Ether)},
		},
	}
	backend := newTestBackend(t, 1, genesis, beacon.New(ethash.NewFaker()), func(i int, b *core.BlockGen) {
		b.SetExtra(extra)
		if i == 0 {
			b.AddTx(signed)
		}
	})
	defer backend.chain.Stop()

	server := rpc.NewServer()
	defer server.Stop()

	if err := server.RegisterName("eth", NewBlockChainAPI(backend)); err != nil {
		t.Fatal(err)
	}

	if err := server.RegisterName("eth", NewTransactionAPI(backend, new(AddrLocker))); err != nil {
		t.Fatal(err)
	}

	client := rpc.DialInProc(server)
	defer client.Close()

	want := crypto.CreateAddress32(sender, 0)
	var receipt map[string]any

	if err := client.Call(&receipt, "eth_getTransactionReceipt", signed.Hash()); err != nil {
		t.Fatal(err)
	}

	address, _ := receipt["contractAddress"].(string)

	if address != "0x"+hex.EncodeToString(want[:]) {
		t.Fatalf("contractAddress = %s, want 0x%x", address, want[:])
	}

	if len(address) != 66 {
		t.Fatalf("contractAddress length = %d, want 66", len(address))
	}

	var code string

	if err := client.Call(&code, "eth_getCode", address, "latest"); err != nil {
		t.Fatal(err)
	}

	if code != "0x42" {
		t.Fatalf("code = %s, want 0x42", code)
	}
}
