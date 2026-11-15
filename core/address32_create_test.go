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

package core

import (
	"bytes"
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

func TestAddress32CreateRejectsAddress20Subspace(t *testing.T) {
	var address20 common.Address32
	address20[31] = 0x01

	if !address20.InAddress20Subspace() {
		t.Fatal("an address20 subspace word was accepted")
	}

	address32 := address32Word(0x11)

	if address32.InAddress20Subspace() {
		t.Fatal("an address32 was rejected")
	}
}

func TestAddress32DeploysAddress32Contract(t *testing.T) {
	cfg := address32Config()
	key := anvilKey(t)
	sender, err := crypto.PubkeyToAddress32(key.PublicKey)

	if err != nil {
		t.Fatal(err)
	}

	initcode := deployByteInitcode(0x42)
	statedb, tdb := openAddress32State(t, cfg, types.GenesisAlloc{
		types.L1BlockAddr: address32L1Account(),
	}, map[common.Address32]types.Account{
		sender: {Balance: big.NewInt(params.Ether)},
	})
	tx := signAddress32Create(t, cfg, key, types.SenderKindAddress32, 0, 500_000, initcode)
	receipt, err := applyAddress32Tx(cfg, statedb, tx)

	if err != nil {
		t.Fatal(err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("status = %d, want success", receipt.Status)
	}

	want := crypto.CreateAddress32(sender, 0)

	if receipt.ContractAddress32 == nil || *receipt.ContractAddress32 != want {
		t.Fatalf("contract address = %s, want %s", receipt.ContractAddress32, want)
	}

	if receipt.ContractAddress != (common.Address{}) {
		t.Fatalf("address20 contract address = %s", receipt.ContractAddress)
	}

	if !bytes.Equal(statedb.Address32Code(want), []byte{0x42}) {
		t.Fatalf("code = %x, want 42", statedb.Address32Code(want))
	}

	if statedb.Address32Nonce(sender) != 1 {
		t.Fatalf("sender nonce = %d, want 1", statedb.Address32Nonce(sender))
	}

	if statedb.Address32Nonce(want) != 1 {
		t.Fatalf("contract nonce = %d, want 1", statedb.Address32Nonce(want))
	}

	root, err := statedb.Commit(1, false, false)

	if err != nil {
		t.Fatal(err)
	}

	reopened, err := state.New(root, state.NewDatabase(tdb, nil))

	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(reopened.Address32Code(want), []byte{0x42}) {
		t.Fatalf("committed code = %x, want 42", reopened.Address32Code(want))
	}
}

func TestCreate32FromAddress32CodeUsesCreatorNonce(t *testing.T) {
	cfg := address32Config()
	key := anvilKey(t)
	sender, err := crypto.PubkeyToAddress32(key.PublicKey)

	if err != nil {
		t.Fatal(err)
	}

	child := deployByteInitcode(0x42)
	initcode := create32Initcode(child)
	statedb := address32State(t, cfg, types.GenesisAlloc{
		types.L1BlockAddr: address32L1Account(),
	}, map[common.Address32]types.Account{
		sender: {Balance: big.NewInt(params.Ether)},
	})
	tx := signAddress32Create(t, cfg, key, types.SenderKindAddress32, 0, 500_000, initcode)
	receipt, err := applyAddress32Tx(cfg, statedb, tx)

	if err != nil {
		t.Fatal(err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("status = %d, want success", receipt.Status)
	}

	outer := crypto.CreateAddress32(sender, 0)
	childAddr := crypto.CreateAddress32(outer, 1)

	if receipt.ContractAddress32 == nil || *receipt.ContractAddress32 != outer {
		t.Fatalf("receipt address = %s, want %s", receipt.ContractAddress32, outer)
	}

	if !bytes.Equal(statedb.Address32Code(outer), childAddr[:]) {
		t.Fatalf("outer code = %x, want child address %x", statedb.Address32Code(outer), childAddr[:])
	}

	if !bytes.Equal(statedb.Address32Code(childAddr), []byte{0x42}) {
		t.Fatalf("child code = %x, want 42", statedb.Address32Code(childAddr))
	}

	if statedb.Address32Nonce(outer) != 2 {
		t.Fatalf("outer nonce = %d, want 2", statedb.Address32Nonce(outer))
	}

	if statedb.Address32Nonce(sender) != 1 {
		t.Fatalf("sender nonce = %d, want 1", statedb.Address32Nonce(sender))
	}
}

func TestCreate232FromAddress32Code(t *testing.T) {
	cfg := address32Config()
	key := anvilKey(t)
	sender, err := crypto.PubkeyToAddress32(key.PublicKey)

	if err != nil {
		t.Fatal(err)
	}

	var salt [32]byte
	salt[31] = 0x07
	child := deployByteInitcode(0x42)
	initcode := create232Initcode(child, salt)
	statedb := address32State(t, cfg, types.GenesisAlloc{
		types.L1BlockAddr: address32L1Account(),
	}, map[common.Address32]types.Account{
		sender: {Balance: big.NewInt(params.Ether)},
	})
	tx := signAddress32Create(t, cfg, key, types.SenderKindAddress32, 0, 500_000, initcode)
	receipt, err := applyAddress32Tx(cfg, statedb, tx)

	if err != nil {
		t.Fatal(err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("status = %d, want success", receipt.Status)
	}

	outer := crypto.CreateAddress32(sender, 0)
	childAddr := crypto.CreateAddress232(outer, salt, crypto.Keccak256(child))

	if !bytes.Equal(statedb.Address32Code(childAddr), []byte{0x42}) {
		t.Fatalf("child code = %x, want 42", statedb.Address32Code(childAddr))
	}

	if !bytes.Equal(statedb.Address32Code(outer), childAddr[:]) {
		t.Fatalf("outer code = %x, want child address %x", statedb.Address32Code(outer), childAddr[:])
	}
}

func TestAddress20SenderUsesAddress20Create(t *testing.T) {
	cfg := address32Config()
	key := anvilKey(t)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	statedb := address32State(t, cfg, types.GenesisAlloc{
		sender:            {Balance: big.NewInt(params.Ether)},
		types.L1BlockAddr: address32L1Account(),
	}, nil)
	tx := signAddress32Create(t, cfg, key, types.SenderKindAddress20, 0, 500_000, deployByteInitcode(0x42))
	receipt, err := applyAddress32Tx(cfg, statedb, tx)

	if err != nil {
		t.Fatal(err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("status = %d, want success", receipt.Status)
	}

	want := crypto.CreateAddress(sender, 0)

	if receipt.ContractAddress != want {
		t.Fatalf("contract address = %s, want %s", receipt.ContractAddress, want)
	}

	if receipt.ContractAddress32 != nil {
		t.Fatalf("address32 contract address = %s", receipt.ContractAddress32)
	}

	if !bytes.Equal(statedb.GetCode(want), []byte{0x42}) {
		t.Fatalf("code = %x, want 42", statedb.GetCode(want))
	}

	if statedb.GetNonce(sender) != 1 {
		t.Fatalf("sender nonce = %d, want 1", statedb.GetNonce(sender))
	}
}

func TestAddress32CreateCodeLimit(t *testing.T) {
	cfg := address32Config()
	key := anvilKey(t)
	sender, err := crypto.PubkeyToAddress32(key.PublicKey)

	if err != nil {
		t.Fatal(err)
	}

	statedb := address32State(t, cfg, types.GenesisAlloc{
		types.L1BlockAddr: address32L1Account(),
	}, map[common.Address32]types.Account{
		sender: {Balance: big.NewInt(params.Ether)},
	})
	fit := signAddress32Create(t, cfg, key, types.SenderKindAddress32, 0, 8_000_000, returnZeros(params.MaxCodeSize))
	receipt, err := applyAddress32Tx(cfg, statedb, fit)

	if err != nil {
		t.Fatal(err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("max code status = %d, want success", receipt.Status)
	}

	if len(statedb.Address32Code(*receipt.ContractAddress32)) != params.MaxCodeSize {
		t.Fatalf("code length = %d, want %d", len(statedb.Address32Code(*receipt.ContractAddress32)), params.MaxCodeSize)
	}

	over := signAddress32Create(t, cfg, key, types.SenderKindAddress32, 1, 8_000_000, returnZeros(params.MaxCodeSize+1))
	receipt, err = applyAddress32Tx(cfg, statedb, over)

	if err != nil {
		t.Fatal(err)
	}

	if receipt.Status != types.ReceiptStatusFailed {
		t.Fatalf("oversize status = %d, want failed", receipt.Status)
	}

	if statedb.Address32Code(*receipt.ContractAddress32) != nil {
		t.Fatalf("oversize code = %x", statedb.Address32Code(*receipt.ContractAddress32))
	}

	if statedb.Address32Nonce(sender) != 2 {
		t.Fatalf("sender nonce = %d, want 2", statedb.Address32Nonce(sender))
	}

	invalid := deployByteInitcode(0xef)
	rejected := signAddress32Create(t, cfg, key, types.SenderKindAddress32, 2, 500_000, invalid)
	receipt, err = applyAddress32Tx(cfg, statedb, rejected)

	if err != nil {
		t.Fatal(err)
	}

	if receipt.Status != types.ReceiptStatusFailed {
		t.Fatalf("0xef status = %d, want failed", receipt.Status)
	}

	if statedb.Address32Code(*receipt.ContractAddress32) != nil {
		t.Fatal("0xef code was stored")
	}
}

func TestAddress32CreateInitcodeLimit(t *testing.T) {
	cfg := address32Config()
	key := anvilKey(t)
	sender, err := crypto.PubkeyToAddress32(key.PublicKey)

	if err != nil {
		t.Fatal(err)
	}

	statedb := address32State(t, cfg, types.GenesisAlloc{
		types.L1BlockAddr: address32L1Account(),
	}, map[common.Address32]types.Account{
		sender: {Balance: big.NewInt(params.Ether)},
	})
	data := make([]byte, params.MaxInitCodeSize+1)
	tx := signAddress32Create(t, cfg, key, types.SenderKindAddress32, 0, 30_000_000, data)
	_, err = applyAddress32Tx(cfg, statedb, tx)

	if err == nil {
		t.Fatal("oversized initcode was included")
	}

	if statedb.Address32Nonce(sender) != 0 {
		t.Fatalf("sender nonce = %d, want 0", statedb.Address32Nonce(sender))
	}
}

func TestAddress32CreateRevertKeepsNonceAndDropsCode(t *testing.T) {
	cfg := address32Config()
	key := anvilKey(t)
	sender, err := crypto.PubkeyToAddress32(key.PublicKey)

	if err != nil {
		t.Fatal(err)
	}

	statedb := address32State(t, cfg, types.GenesisAlloc{
		types.L1BlockAddr: address32L1Account(),
	}, map[common.Address32]types.Account{
		sender: {Balance: big.NewInt(params.Ether)},
	})
	tx := signAddress32Create(t, cfg, key, types.SenderKindAddress32, 0, 500_000, []byte{0x60, 0x00, 0x60, 0x00, 0xfd})
	receipt, err := applyAddress32Tx(cfg, statedb, tx)

	if err != nil {
		t.Fatal(err)
	}

	if receipt.Status != types.ReceiptStatusFailed {
		t.Fatalf("status = %d, want failed", receipt.Status)
	}

	want := crypto.CreateAddress32(sender, 0)

	if receipt.ContractAddress32 == nil || *receipt.ContractAddress32 != want {
		t.Fatalf("contract address = %s, want %s", receipt.ContractAddress32, want)
	}

	if statedb.Address32Code(want) != nil {
		t.Fatal("reverted creation stored code")
	}

	if statedb.Address32Nonce(sender) != 1 {
		t.Fatalf("sender nonce = %d, want 1", statedb.Address32Nonce(sender))
	}

	if statedb.Address32Nonce(want) != 0 {
		t.Fatalf("contract nonce = %d, want 0", statedb.Address32Nonce(want))
	}
}

func signAddress32Create(t *testing.T, cfg *params.ChainConfig, key *ecdsa.PrivateKey, kind uint8, nonce, gas uint64, data []byte) *types.Transaction {
	t.Helper()
	signed, err := types.SignTx(types.NewTx(&types.Address32Tx{
		ChainID:    cfg.ChainID,
		Nonce:      nonce,
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(2),
		Gas:        gas,
		Value:      big.NewInt(0),
		Data:       data,
		SenderKind: kind,
	}), types.LatestSigner(cfg), key)

	if err != nil {
		t.Fatal(err)
	}

	return signed
}

func deployByteInitcode(b byte) []byte {
	return []byte{0x60, b, 0x60, 0x00, 0x52, 0x60, 0x01, 0x60, 0x1f, 0xf3}
}

func returnZeros(n int) []byte {
	return []byte{byte(vm.PUSH2), byte(n >> 8), byte(n), byte(vm.PUSH1), 0x00, byte(vm.RETURN)}
}

func create32Initcode(child []byte) []byte {
	offset := byte(22)
	size := byte(len(child))
	code := []byte{
		byte(vm.PUSH1), size,
		byte(vm.PUSH1), offset,
		byte(vm.PUSH1), 0,
		byte(vm.CODECOPY),
		byte(vm.PUSH1), size,
		byte(vm.PUSH1), 0,
		byte(vm.PUSH1), 0,
		byte(vm.CREATE32),
		byte(vm.PUSH1), 0,
		byte(vm.MSTORE),
		byte(vm.PUSH1), 32,
		byte(vm.PUSH1), 0,
		byte(vm.RETURN),
	}

	return append(code, child...)
}

func create232Initcode(child []byte, salt [32]byte) []byte {
	offset := byte(55)
	size := byte(len(child))
	code := []byte{
		byte(vm.PUSH1), size,
		byte(vm.PUSH1), offset,
		byte(vm.PUSH1), 0,
		byte(vm.CODECOPY),
		byte(vm.PUSH32),
	}
	code = append(code, salt[:]...)
	code = append(code,
		byte(vm.PUSH1), size,
		byte(vm.PUSH1), 0,
		byte(vm.PUSH1), 0,
		byte(vm.CREATE232),
		byte(vm.PUSH1), 0,
		byte(vm.MSTORE),
		byte(vm.PUSH1), 32,
		byte(vm.PUSH1), 0,
		byte(vm.RETURN),
	)

	return append(code, child...)
}
