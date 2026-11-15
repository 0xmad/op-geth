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
	"crypto/ecdsa"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

const anvil0Key = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

func TestIntrinsicGasAddress32MatchesType2(t *testing.T) {
	empty, err := IntrinsicGasAddress32(nil, nil, false, true)

	if err != nil {
		t.Fatal(err)
	}

	if empty != params.TxGas {
		t.Fatalf("empty transfer gas = %d, want %d", empty, params.TxGas)
	}

	nonzero, err := IntrinsicGasAddress32([]byte{0x01}, nil, false, true)

	if err != nil {
		t.Fatal(err)
	}

	if nonzero != params.TxGas+params.TxDataNonZeroGasEIP2028 {
		t.Fatalf("nonzero byte gas = %d, want %d", nonzero, params.TxGas+params.TxDataNonZeroGasEIP2028)
	}

	zero, err := IntrinsicGasAddress32([]byte{0x00}, nil, false, true)

	if err != nil {
		t.Fatal(err)
	}

	if zero != params.TxGas+params.TxDataZeroGas {
		t.Fatalf("zero byte gas = %d, want %d", zero, params.TxGas+params.TxDataZeroGas)
	}

	var word types.Address32Word
	word[0] = 1
	var slot common.Hash
	slot[31] = 1
	listed, err := IntrinsicGasAddress32(nil, types.Address32AccessList{{
		Address:     word,
		StorageKeys: []common.Hash{slot},
	}}, false, true)

	if err != nil {
		t.Fatal(err)
	}

	want := params.TxGas + params.TxAccessListAddressGas + params.TxAccessListStorageKeyGas

	if listed != want {
		t.Fatalf("access list gas = %d, want %d", listed, want)
	}
}

func TestAddress32TransferMovesValue(t *testing.T) {
	cfg := address32Config()
	key := anvilKey(t)
	legacySender := crypto.PubkeyToAddress(key.PublicKey)
	wideSender, err := crypto.PubkeyToAddress32(key.PublicKey)

	if err != nil {
		t.Fatal(err)
	}

	freshLegacy := common.HexToAddress("0x1111111111111111111111111111111111111111")
	var freshWide common.Address32
	freshWide[0] = 0x01
	freshWide[31] = 0x42
	var otherWide common.Address32
	otherWide[0] = 0x02
	otherWide[31] = 0x43

	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, triedb.HashDefaults)
	genesis := &Genesis{
		Config:     cfg,
		Difficulty: big.NewInt(0),
		GasLimit:   30_000_000,
		BaseFee:    big.NewInt(1),
		Alloc: types.GenesisAlloc{
			legacySender: {Balance: big.NewInt(params.Ether)},
			types.L1BlockAddr: {Storage: map[common.Hash]common.Hash{
				types.L1FeeScalarsSlot:      {19: 1},
				types.L1BaseFeeSlot:         common.BigToHash(big.NewInt(1_000_000)),
				types.OperatorFeeParamsSlot: {31: 10},
			}},
		},
		WideAlloc: map[common.Address32]types.Account{
			wideSender: {Balance: big.NewInt(params.Ether)},
		},
	}
	block, err := genesis.Commit(db, tdb)

	if err != nil {
		t.Fatal(err)
	}

	statedb, err := state.New(block.Root(), state.NewDatabase(tdb, nil))

	if err != nil {
		t.Fatal(err)
	}

	legacyToWide := signAddress32Tx(t, cfg, key, types.SenderKindLegacy, freshWide, 0, 1000)

	if err := applyAddress32Tx(cfg, statedb, legacyToWide); err != nil {
		t.Fatal(err)
	}

	if got := statedb.Address32Balance(freshWide); got.Cmp(uint256.NewInt(1000)) != 0 {
		t.Fatalf("wide recipient balance = %s, want 1000", got)
	}

	if statedb.GetNonce(legacySender) != 1 {
		t.Fatalf("legacy sender nonce = %d, want 1", statedb.GetNonce(legacySender))
	}

	if statedb.GetBalance(params.OptimismBaseFeeRecipient).Sign() == 0 {
		t.Fatal("base fee recipient was not credited")
	}

	if statedb.GetBalance(params.OptimismL1FeeRecipient).Sign() == 0 {
		t.Fatal("l1 fee recipient was not credited")
	}
	if statedb.GetBalance(params.OptimismOperatorFeeRecipient).Sign() == 0 {
		t.Fatal("operator fee recipient was not credited")
	}

	wideToLegacy := signAddress32Tx(t, cfg, key, types.SenderKindWide, legacyWord(freshLegacy), 0, 1000)

	if err := applyAddress32Tx(cfg, statedb, wideToLegacy); err != nil {
		t.Fatal(err)
	}

	if got := statedb.GetBalance(freshLegacy); got.Cmp(uint256.NewInt(1000)) != 0 {
		t.Fatalf("legacy recipient balance = %s, want 1000", got)
	}

	if statedb.Address32Nonce(wideSender) != 1 {
		t.Fatalf("wide sender nonce = %d, want 1", statedb.Address32Nonce(wideSender))
	}

	wideToWide := signAddress32Tx(t, cfg, key, types.SenderKindWide, otherWide, 1, 1000)

	if err := applyAddress32Tx(cfg, statedb, wideToWide); err != nil {
		t.Fatal(err)
	}

	if got := statedb.Address32Balance(otherWide); got.Cmp(uint256.NewInt(1000)) != 0 {
		t.Fatalf("second wide recipient balance = %s, want 1000", got)
	}

	root, err := statedb.Commit(1, false, false)

	if err != nil {
		t.Fatal(err)
	}

	reopened, err := state.New(root, state.NewDatabase(tdb, nil))

	if err != nil {
		t.Fatal(err)
	}

	if got := reopened.Address32Balance(freshWide); got.Cmp(uint256.NewInt(1000)) != 0 {
		t.Fatalf("committed wide balance = %s, want 1000", got)
	}

	if got := reopened.GetBalance(freshLegacy); got.Cmp(uint256.NewInt(1000)) != 0 {
		t.Fatalf("committed legacy balance = %s, want 1000", got)
	}

	if reopened.GetNonce(legacySender) != 1 || reopened.Address32Nonce(wideSender) != 2 {
		t.Fatalf("committed nonces legacy %d wide %d", reopened.GetNonce(legacySender), reopened.Address32Nonce(wideSender))
	}
}

func TestAddress32TxRejectedBeforeFork(t *testing.T) {
	cfg := *params.OptimismTestConfig
	key := anvilKey(t)
	var to common.Address32
	to[0] = 1
	tx := signAddress32Tx(t, address32Config(), key, types.SenderKindLegacy, to, 0, 1)
	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, triedb.HashDefaults)
	block, err := (&Genesis{
		Config:     &cfg,
		Difficulty: big.NewInt(0),
		GasLimit:   30_000_000,
		BaseFee:    big.NewInt(1),
		Alloc:      types.GenesisAlloc{crypto.PubkeyToAddress(key.PublicKey): {Balance: big.NewInt(params.Ether)}},
	}).Commit(db, tdb)

	if err != nil {
		t.Fatal(err)
	}

	statedb, err := state.New(block.Root(), state.NewDatabase(tdb, nil))

	if err != nil {
		t.Fatal(err)
	}

	err = applyAddress32Tx(&cfg, statedb, tx)

	if !errors.Is(err, ErrTxTypeNotSupported) {
		t.Fatalf("pre-fork error = %v", err)
	}
}

func TestAddress32CreationIsRejected(t *testing.T) {
	cfg := address32Config()
	key := anvilKey(t)
	signer := types.LatestSigner(cfg)
	signed, err := types.SignTx(types.NewTx(&types.Address32Tx{
		ChainID:    cfg.ChainID,
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(2),
		Gas:        params.TxGas,
		Value:      big.NewInt(1),
		SenderKind: types.SenderKindLegacy,
	}), signer, key)
	if err != nil {
		t.Fatal(err)
	}
	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, triedb.HashDefaults)
	block, err := (&Genesis{
		Config:     cfg,
		Difficulty: big.NewInt(0),
		GasLimit:   30_000_000,
		BaseFee:    big.NewInt(1),
		Alloc:      types.GenesisAlloc{crypto.PubkeyToAddress(key.PublicKey): {Balance: big.NewInt(params.Ether)}},
	}).Commit(db, tdb)

	if err != nil {
		t.Fatal(err)
	}

	statedb, err := state.New(block.Root(), state.NewDatabase(tdb, nil))

	if err != nil {
		t.Fatal(err)
	}

	if err := applyAddress32Tx(cfg, statedb, signed); err == nil {
		t.Fatal("contract creation was applied")
	}
}

func address32Config() *params.ChainConfig {
	cfg := *params.OptimismTestConfig
	ts := uint64(0)
	cfg.Address32Time = &ts
	return &cfg
}

func anvilKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.HexToECDSA(anvil0Key)

	if err != nil {
		t.Fatal(err)
	}

	return key
}

func legacyWord(addr common.Address) common.Address32 {
	var word common.Address32
	copy(word[12:], addr[:])
	return word
}

func signAddress32Tx(t *testing.T, cfg *params.ChainConfig, key *ecdsa.PrivateKey, kind uint8, to common.Address32, nonce uint64, value int64) *types.Transaction {
	t.Helper()
	word, err := types.Address32WordFromBytes(to[:])

	if err != nil {
		t.Fatal(err)
	}

	signed, err := types.SignTx(types.NewTx(&types.Address32Tx{
		ChainID:    cfg.ChainID,
		Nonce:      nonce,
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(2),
		Gas:        params.TxGas,
		To:         word,
		Value:      big.NewInt(value),
		SenderKind: kind,
	}), types.LatestSigner(cfg), key)

	if err != nil {
		t.Fatal(err)
	}

	return signed
}

func applyAddress32Tx(cfg *params.ChainConfig, statedb *state.StateDB, tx *types.Transaction) error {
	header := &types.Header{
		Number:     big.NewInt(1),
		Time:       1,
		BaseFee:    big.NewInt(1),
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(0),
		Coinbase:   common.HexToAddress("0x0101010101010101010101010101010101010101"),
	}
	coinbase := header.Coinbase
	evm := vm.NewEVM(NewEVMBlockContext(header, nil, &coinbase, cfg, statedb), statedb, cfg, vm.Config{})
	gp := new(GasPool).AddGas(header.GasLimit)
	_, err := ApplyTransaction(evm, gp, statedb, header, tx, new(uint64))
	return err
}
