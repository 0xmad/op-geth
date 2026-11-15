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

var address32Coinbase = common.HexToAddress("0x0101010101010101010101010101010101010101")

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
	statedb, tdb := openAddress32State(t, cfg, types.GenesisAlloc{
		legacySender:      {Balance: big.NewInt(params.Ether)},
		types.L1BlockAddr: address32L1Account(),
	}, map[common.Address32]types.Account{
		wideSender: {Balance: big.NewInt(params.Ether)},
	})

	legacyToWide := signAddress32Tx(t, cfg, key, types.SenderKindLegacy, freshWide, 0, 1000)

	if _, err := applyAddress32Tx(cfg, statedb, legacyToWide); err != nil {
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

	if _, err := applyAddress32Tx(cfg, statedb, wideToLegacy); err != nil {
		t.Fatal(err)
	}

	if got := statedb.GetBalance(freshLegacy); got.Cmp(uint256.NewInt(1000)) != 0 {
		t.Fatalf("legacy recipient balance = %s, want 1000", got)
	}

	if statedb.Address32Nonce(wideSender) != 1 {
		t.Fatalf("wide sender nonce = %d, want 1", statedb.Address32Nonce(wideSender))
	}

	wideToWide := signAddress32Tx(t, cfg, key, types.SenderKindWide, otherWide, 1, 1000)

	if _, err := applyAddress32Tx(cfg, statedb, wideToWide); err != nil {
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
	statedb := address32State(t, &cfg, types.GenesisAlloc{
		crypto.PubkeyToAddress(key.PublicKey): {Balance: big.NewInt(params.Ether)},
	}, nil)
	_, err := applyAddress32Tx(&cfg, statedb, tx)

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

	statedb := address32State(t, cfg, types.GenesisAlloc{
		crypto.PubkeyToAddress(key.PublicKey): {Balance: big.NewInt(params.Ether)},
	}, nil)

	if _, err := applyAddress32Tx(cfg, statedb, signed); err == nil {
		t.Fatal("contract creation was applied")
	}
}

func TestWideSenderCallToLegacyCodeReverts(t *testing.T) {
	cfg := address32Config()
	key := anvilKey(t)
	wideSender, err := crypto.PubkeyToAddress32(key.PublicKey)

	if err != nil {
		t.Fatal(err)
	}

	callee := common.HexToAddress("0x2222222222222222222222222222222222222222")
	code, slot, stored, account := codedCallee()
	statedb := address32State(t, cfg, types.GenesisAlloc{
		crypto.PubkeyToAddress(key.PublicKey): {Balance: big.NewInt(params.Ether)},
		callee:                                account,
		types.L1BlockAddr:                     address32L1Account(),
	}, map[common.Address32]types.Account{
		wideSender: {Balance: big.NewInt(params.Ether)},
	})
	tx := signAddress32Tx(t, cfg, key, types.SenderKindWide, legacyWord(callee), 0, 1000)
	recovered, err := types.SenderAccount(types.LatestSigner(cfg), tx)

	if err != nil {
		t.Fatal(err)
	}

	got, ok := recovered.Address32()

	if !ok || got != wideSender {
		t.Fatalf("recovered sender = %v, want %s", recovered, wideSender.Hex())
	}

	sender := wideAccountReader(statedb, wideSender)
	before := sender.balance().Clone()
	receipt, err := applyAddress32Tx(cfg, statedb, tx)

	if err != nil {
		t.Fatal(err)
	}

	assertIncludedRevert(t, statedb, receipt, before, sender, legacyAccountReader(statedb, callee), code, slot, stored)
}

func TestLegacySenderCallToWideCodeReverts(t *testing.T) {
	cfg := address32Config()
	key := anvilKey(t)
	legacySender := crypto.PubkeyToAddress(key.PublicKey)
	var callee common.Address32
	callee[0] = 0x01
	callee[31] = 0x42
	code, slot, stored, account := codedCallee()
	statedb := address32State(t, cfg, types.GenesisAlloc{
		legacySender:      {Balance: big.NewInt(params.Ether)},
		types.L1BlockAddr: address32L1Account(),
	}, map[common.Address32]types.Account{
		callee: account,
	})
	tx := signAddress32Tx(t, cfg, key, types.SenderKindLegacy, callee, 0, 1000)
	recovered, err := types.SenderAccount(types.LatestSigner(cfg), tx)

	if err != nil {
		t.Fatal(err)
	}

	got, ok := recovered.Legacy()

	if !ok || got != legacySender {
		t.Fatalf("recovered sender = %v, want %s", recovered, legacySender.Hex())
	}

	sender := legacyAccountReader(statedb, legacySender)
	before := sender.balance().Clone()
	receipt, err := applyAddress32Tx(cfg, statedb, tx)

	if err != nil {
		t.Fatal(err)
	}

	assertIncludedRevert(t, statedb, receipt, before, sender, wideAccountReader(statedb, callee), code, slot, stored)
}

func TestDelegationCountsAsCode(t *testing.T) {
	cfg := address32Config()
	key := anvilKey(t)
	wideSender, err := crypto.PubkeyToAddress32(key.PublicKey)

	if err != nil {
		t.Fatal(err)
	}

	callee := common.HexToAddress("0x3333333333333333333333333333333333333333")
	delegated := common.HexToAddress("0x4444444444444444444444444444444444444444")
	code := types.AddressToDelegation(delegated)
	_, slot, stored, account := codedCallee()
	account.Code = code
	statedb := address32State(t, cfg, types.GenesisAlloc{
		crypto.PubkeyToAddress(key.PublicKey): {Balance: big.NewInt(params.Ether)},
		callee:                                account,
		types.L1BlockAddr:                     address32L1Account(),
	}, map[common.Address32]types.Account{
		wideSender: {Balance: big.NewInt(params.Ether)},
	})
	tx := signAddress32Tx(t, cfg, key, types.SenderKindWide, legacyWord(callee), 0, 1000)
	sender := wideAccountReader(statedb, wideSender)
	before := sender.balance().Clone()
	receipt, err := applyAddress32Tx(cfg, statedb, tx)

	if err != nil {
		t.Fatal(err)
	}

	assertIncludedRevert(t, statedb, receipt, before, sender, legacyAccountReader(statedb, callee), code, slot, stored)
}

func TestAddress32TargetCodeDoesNotAffectSignature(t *testing.T) {
	cfg := address32Config()
	key := anvilKey(t)
	wideSender, err := crypto.PubkeyToAddress32(key.PublicKey)

	if err != nil {
		t.Fatal(err)
	}

	coded := common.HexToAddress("0x5555555555555555555555555555555555555555")
	codeless := common.HexToAddress("0x6666666666666666666666666666666666666666")
	signer := types.LatestSigner(cfg)
	toCode := signAddress32Tx(t, cfg, key, types.SenderKindWide, legacyWord(coded), 0, 1000)
	toEmpty := signAddress32Tx(t, cfg, key, types.SenderKindWide, legacyWord(codeless), 1, 1000)

	for _, tx := range []*types.Transaction{toCode, toEmpty} {
		recovered, err := types.SenderAccount(signer, tx)

		if err != nil {
			t.Fatal(err)
		}

		got, ok := recovered.Address32()

		if !ok || got != wideSender {
			t.Fatalf("recovered sender = %v, want %s", recovered, wideSender.Hex())
		}
	}

	statedb := address32State(t, cfg, types.GenesisAlloc{
		coded:             {Balance: big.NewInt(77), Code: []byte{0xfe}},
		types.L1BlockAddr: address32L1Account(),
	}, map[common.Address32]types.Account{
		wideSender: {Balance: big.NewInt(params.Ether)},
	})
	receipt, err := applyAddress32Tx(cfg, statedb, toCode)

	if err != nil {
		t.Fatal(err)
	}

	if receipt.Status != types.ReceiptStatusFailed {
		t.Fatalf("coded target status = %d, want failed", receipt.Status)
	}

	if got := statedb.GetBalance(coded); got.Cmp(uint256.NewInt(77)) != 0 {
		t.Fatalf("coded target balance = %s, want 77", got)
	}

	codelessReceipt, err := applyAddress32Tx(cfg, statedb, toEmpty)

	if err != nil {
		t.Fatal(err)
	}

	if codelessReceipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("codeless target status = %d, want success", codelessReceipt.Status)
	}

	if got := statedb.GetBalance(codeless); got.Cmp(uint256.NewInt(1000)) != 0 {
		t.Fatalf("codeless target balance = %s, want 1000", got)
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

func address32L1Account() types.Account {
	return types.Account{Storage: map[common.Hash]common.Hash{
		types.L1FeeScalarsSlot:      {19: 1},
		types.L1BaseFeeSlot:         common.BigToHash(big.NewInt(1_000_000)),
		types.OperatorFeeParamsSlot: {31: 10},
	}}
}

func codedCallee() (code []byte, slot, stored common.Hash, account types.Account) {
	code = []byte{0x63, 0xde, 0xad, 0xbe, 0xef}
	slot[31] = 0x11
	stored[31] = 0xab
	account = types.Account{
		Balance: big.NewInt(77),
		Nonce:   7,
		Code:    code,
		Storage: map[common.Hash]common.Hash{slot: stored},
	}
	return
}

// accountReader reads one legacy account or one Address32 after a transaction.
type accountReader struct {
	balance func() *uint256.Int
	nonce   func() uint64
	code    func() []byte
	storage func(common.Hash) common.Hash
}

func legacyAccountReader(statedb *state.StateDB, addr common.Address) accountReader {
	return accountReader{
		balance: func() *uint256.Int { return statedb.GetBalance(addr) },
		nonce:   func() uint64 { return statedb.GetNonce(addr) },
		code:    func() []byte { return statedb.GetCode(addr) },
		storage: func(key common.Hash) common.Hash { return statedb.GetState(addr, key) },
	}
}

func wideAccountReader(statedb *state.StateDB, addr common.Address32) accountReader {
	return accountReader{
		balance: func() *uint256.Int { return statedb.Address32Balance(addr) },
		nonce:   func() uint64 { return statedb.Address32Nonce(addr) },
		code:    func() []byte { return statedb.Address32Code(addr) },
		storage: func(key common.Hash) common.Hash { return statedb.Address32State(addr, key) },
	}
}

func assertIncludedRevert(t *testing.T, statedb *state.StateDB, receipt *types.Receipt, senderBefore *uint256.Int, sender, callee accountReader, code []byte, slot, stored common.Hash) {
	t.Helper()

	if receipt.Status != types.ReceiptStatusFailed {
		t.Fatalf("status = %d, want failed", receipt.Status)
	}

	if sender.nonce() != 1 {
		t.Fatalf("sender nonce = %d, want 1", sender.nonce())
	}

	if got := callee.balance(); got.Cmp(uint256.NewInt(77)) != 0 {
		t.Fatalf("callee balance = %s, want 77", got)
	}

	if callee.nonce() != 7 {
		t.Fatalf("callee nonce = %d, want 7", callee.nonce())
	}

	if !bytes.Equal(callee.code(), code) {
		t.Fatalf("callee code = %x, want %x", callee.code(), code)
	}

	if callee.storage(slot) != stored {
		t.Fatalf("callee storage = %s, want %s", callee.storage(slot), stored)
	}

	spent := new(uint256.Int).Sub(senderBefore, sender.balance())
	assertAddress32Fees(t, statedb, spent)
}

func address32State(t *testing.T, cfg *params.ChainConfig, alloc types.GenesisAlloc, wide map[common.Address32]types.Account) *state.StateDB {
	t.Helper()
	statedb, _ := openAddress32State(t, cfg, alloc, wide)
	return statedb
}

func openAddress32State(t *testing.T, cfg *params.ChainConfig, alloc types.GenesisAlloc, wide map[common.Address32]types.Account) (*state.StateDB, *triedb.Database) {
	t.Helper()
	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, triedb.HashDefaults)
	block, err := (&Genesis{
		Config:     cfg,
		Difficulty: big.NewInt(0),
		GasLimit:   30_000_000,
		BaseFee:    big.NewInt(1),
		Alloc:      alloc,
		WideAlloc:  wide,
	}).Commit(db, tdb)

	if err != nil {
		t.Fatal(err)
	}

	statedb, err := state.New(block.Root(), state.NewDatabase(tdb, nil))

	if err != nil {
		t.Fatal(err)
	}

	return statedb, tdb
}

func assertAddress32Fees(t *testing.T, statedb *state.StateDB, spent *uint256.Int) {
	t.Helper()
	base := statedb.GetBalance(params.OptimismBaseFeeRecipient).Clone()
	l1 := statedb.GetBalance(params.OptimismL1FeeRecipient).Clone()
	operator := statedb.GetBalance(params.OptimismOperatorFeeRecipient).Clone()
	coinbase := statedb.GetBalance(address32Coinbase).Clone()

	if base.Sign() == 0 || l1.Sign() == 0 || operator.Sign() == 0 || coinbase.Sign() == 0 {
		t.Fatalf("fees base %s l1 %s operator %s coinbase %s", base, l1, operator, coinbase)
	}

	credited := new(uint256.Int).Add(base, l1)
	credited.Add(credited, operator)
	credited.Add(credited, coinbase)

	if spent.Cmp(credited) != 0 {
		t.Fatalf("sender spent %s, fee recipients received %s", spent, credited)
	}
}

func applyAddress32Tx(cfg *params.ChainConfig, statedb *state.StateDB, tx *types.Transaction) (*types.Receipt, error) {
	header := &types.Header{
		Number:     big.NewInt(1),
		Time:       1,
		BaseFee:    big.NewInt(1),
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(0),
		Coinbase:   address32Coinbase,
	}
	coinbase := header.Coinbase
	evm := vm.NewEVM(NewEVMBlockContext(header, nil, &coinbase, cfg, statedb), statedb, cfg, vm.Config{})
	gp := new(GasPool).AddGas(header.GasLimit)
	return ApplyTransaction(evm, gp, statedb, header, tx, new(uint64))
}
