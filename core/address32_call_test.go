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
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/holiman/uint256"
)

func TestAddress32FramePushesFullWords(t *testing.T) {
	cfg := address32Config()
	caller := address32Word(0x11)
	contract := address32Word(0x22)
	code := bytes.Join([][]byte{
		{byte(vm.ADDRESS), byte(vm.PUSH1), 0, byte(vm.MSTORE)},
		{byte(vm.CALLER), byte(vm.PUSH1), 32, byte(vm.MSTORE)},
		{byte(vm.ORIGIN), byte(vm.PUSH1), 64, byte(vm.MSTORE)},
		{byte(vm.COINBASE), byte(vm.PUSH1), 96, byte(vm.MSTORE)},
		{byte(vm.PUSH1), 128, byte(vm.PUSH1), 0, byte(vm.RETURN)},
	}, nil)
	run := runAddress32(t, cfg, caller, contract, code, nil)

	if run.err != nil {
		t.Fatal(run.err)
	}

	if len(run.ret) != 128 {
		t.Fatalf("return length = %d, want 128", len(run.ret))
	}

	wantCoinbase := address20Word(address32Coinbase)

	if !bytes.Equal(run.ret[:32], contract[:]) || !bytes.Equal(run.ret[32:64], caller[:]) || !bytes.Equal(run.ret[64:96], caller[:]) || !bytes.Equal(run.ret[96:], wantCoinbase[:]) {
		t.Fatalf("words = %x", run.ret)
	}
}

func TestAddress20OpcodesHaltAddress32Frame(t *testing.T) {
	cfg := address32Config()
	caller := address32Word(0x31)
	contract := address32Word(0x32)
	ops := []vm.OpCode{
		vm.BALANCE, vm.EXTCODESIZE, vm.EXTCODECOPY, vm.EXTCODEHASH,
		vm.CALL, vm.CALLCODE, vm.DELEGATECALL, vm.STATICCALL,
		vm.CREATE, vm.CREATE2, vm.SELFDESTRUCT,
	}

	for _, op := range ops {
		t.Run(op.String(), func(t *testing.T) {
			run := runAddress32(t, cfg, caller, contract, []byte{byte(op)}, nil)
			var invalid *vm.ErrInvalidOpCode

			if !errors.As(run.err, &invalid) || !strings.Contains(invalid.Error(), op.String()) {
				t.Fatalf("err = %v, want invalid %s", run.err, op)
			}

			if run.left != 0 {
				t.Fatalf("gas left = %d, want 0", run.left)
			}
		})
	}
}

func TestNoCallCode32(t *testing.T) {
	if vm.StringToOp("CALLCODE32") != 0 {
		t.Fatal("CALLCODE32 is assigned")
	}

	run := runOpcode(t, address32Config(), []byte{0xaf}, nil, nil)

	if _, ok := errors.AsType[*vm.ErrInvalidOpCode](run.err); !ok {
		t.Fatalf("err = %v, want invalid opcode", run.err)
	}
}

func TestAddress32Call32RunsAddress32Contract(t *testing.T) {
	cfg := address32Config()
	caller := address32Word(0x41)
	callee := address32Word(0x42)
	parent := address32Word(0x40)
	calleeCode := []byte{byte(vm.CALLER), byte(vm.PUSH1), 0, byte(vm.MSTORE), byte(vm.PUSH1), 32, byte(vm.PUSH1), 0, byte(vm.RETURN)}
	run := runAddress32(t, cfg, caller, parent, callReturning(vm.CALL32, callee[:], 0), map[common.Address32]types.Account{
		callee: {Code: calleeCode},
	})

	if run.err != nil {
		t.Fatal(run.err)
	}

	success, ret := callResult(t, run.ret)

	if success.Cmp(uint256.NewInt(1)) != 0 || ret.Cmp(new(uint256.Int).SetBytes(parent[:])) != 0 {
		t.Fatalf("success %s return %s, want caller %x", success, ret, parent[:])
	}
}

func TestAddress32StaticCall32RunsAddress32Contract(t *testing.T) {
	cfg := address32Config()
	caller := address32Word(0x51)
	callee := address32Word(0x52)
	calleeCode := []byte{byte(vm.PUSH1), 0x2a, byte(vm.PUSH1), 0, byte(vm.MSTORE), byte(vm.PUSH1), 32, byte(vm.PUSH1), 0, byte(vm.RETURN)}
	run := runAddress32(t, cfg, caller, address32Word(0x50), staticCallReturning(callee[:]), map[common.Address32]types.Account{
		callee: {Code: calleeCode},
	})

	if run.err != nil {
		t.Fatal(run.err)
	}

	success, ret := callResult(t, run.ret)

	if success.Cmp(uint256.NewInt(1)) != 0 || ret.Cmp(uint256.NewInt(0x2a)) != 0 {
		t.Fatalf("success %s return %s", success, ret)
	}
}

func TestAddress32DelegateCall32UsesCallerAccount(t *testing.T) {
	cfg := address32Config()
	caller := address32Word(0xa1)
	parent := address32Word(0xa2)
	callee := address32Word(0xa3)
	slot := common.Hash{}
	stored := common.Hash{}
	stored[31] = 0x42
	target := delegateTargetCode()
	run := runAddress32Paying(t, cfg, caller, parent, delegateContextReturning(callee[:]), uint256.NewInt(9), map[common.Address32]types.Account{
		caller: {Balance: big.NewInt(9)},
		parent: {Balance: big.NewInt(40)},
		callee: {Balance: big.NewInt(3), Code: target},
	})

	if run.err != nil {
		t.Fatal(run.err)
	}

	if len(run.ret) != 128 {
		t.Fatalf("return length = %d, want 128", len(run.ret))
	}

	if new(uint256.Int).SetBytes(run.ret[:32]).Cmp(uint256.NewInt(1)) != 0 || !bytes.Equal(run.ret[32:64], parent[:]) || !bytes.Equal(run.ret[64:96], caller[:]) || new(uint256.Int).SetBytes(run.ret[96:]).Cmp(uint256.NewInt(9)) != 0 {
		t.Fatalf("success %x address %x caller %x value %x", run.ret[:32], run.ret[32:64], run.ret[64:96], run.ret[96:])
	}

	if run.statedb.Address32State(parent, slot) != stored {
		t.Fatalf("caller storage = %s, want %s", run.statedb.Address32State(parent, slot), stored)
	}

	if run.statedb.Address32State(callee, slot) != (common.Hash{}) {
		t.Fatalf("code account storage = %s, want empty", run.statedb.Address32State(callee, slot))
	}

	if got := run.statedb.Address32Balance(callee); got.Cmp(uint256.NewInt(3)) != 0 {
		t.Fatalf("code account balance = %s, want 3", got)
	}

	run.statedb.Finalise(false)

	if run.statedb.Address32CommittedState(parent, slot) != stored {
		t.Fatalf("committed storage = %s, want %s", run.statedb.Address32CommittedState(parent, slot), stored)
	}

	root, err := run.statedb.Commit(1, false, false)

	if err != nil {
		t.Fatal(err)
	}

	reopened, err := state.New(root, run.statedb.Database())

	if err != nil {
		t.Fatal(err)
	}

	if reopened.Address32State(parent, slot) != stored {
		t.Fatalf("committed caller storage = %s, want %s", reopened.Address32State(parent, slot), stored)
	}

	if reopened.Address32State(callee, slot) != (common.Hash{}) {
		t.Fatal("committed code account storage")
	}

	next := common.Hash{}
	next[31] = 1
	updated := common.Hash{}
	updated[31] = 0x7
	reopened.SetAddress32State(parent, next, updated)
	root, err = reopened.Commit(2, false, false)

	if err != nil {
		t.Fatal(err)
	}

	again, err := state.New(root, reopened.Database())

	if err != nil {
		t.Fatal(err)
	}

	if again.Address32State(parent, slot) != stored || again.Address32State(parent, next) != updated {
		t.Fatalf("slot0 %s slot1 %s", again.Address32State(parent, slot), again.Address32State(parent, next))
	}

	called := runAddress32(t, cfg, caller, parent, callContextReturning(callee[:]), map[common.Address32]types.Account{
		callee: {Code: target},
	})

	if called.err != nil {
		t.Fatal(called.err)
	}

	if new(uint256.Int).SetBytes(called.ret[:32]).Cmp(uint256.NewInt(1)) != 0 || !bytes.Equal(called.ret[32:64], callee[:]) || !bytes.Equal(called.ret[64:96], parent[:]) {
		t.Fatalf("call success %x address %x caller %x", called.ret[:32], called.ret[32:64], called.ret[64:96])
	}

	if called.statedb.Address32State(callee, slot) != stored || called.statedb.Address32State(parent, slot) != (common.Hash{}) {
		t.Fatal("CALL32 wrote the caller's storage")
	}
}

func TestAddress32DelegateCall32RevertUndoesCallerStorage(t *testing.T) {
	cfg := address32Config()
	caller := address32Word(0xa4)
	parent := address32Word(0xa5)
	callee := address32Word(0xa6)
	target := []byte{
		byte(vm.PUSH1), 0x42, byte(vm.PUSH1), 0, byte(vm.SSTORE),
		byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.REVERT),
	}
	run := runAddress32(t, cfg, caller, parent, delegateReturning(vm.DELEGATECALL32, callee[:]), map[common.Address32]types.Account{
		callee: {Code: target},
	})

	if run.err != nil {
		t.Fatal(run.err)
	}

	success, ret := callResult(t, run.ret)

	if success.Sign() != 0 || ret.Sign() != 0 {
		t.Fatalf("success %s return %s, want failed delegatecall", success, ret)
	}

	if run.statedb.Address32State(parent, common.Hash{}) != (common.Hash{}) {
		t.Fatal("reverted delegatecall kept the caller's storage")
	}
}

func TestAddress32DelegateCall32ToAddress20Fails(t *testing.T) {
	cfg := address32Config()
	caller := address32Word(0xb1)
	parent := address32Word(0xb2)
	user := common.HexToAddress("0x7777777777777777777777777777777777777777")
	identity := common.BytesToAddress([]byte{4})
	userCode := []byte{byte(vm.PUSH1), 0x2a, byte(vm.PUSH1), 0, byte(vm.MSTORE), byte(vm.PUSH1), 32, byte(vm.PUSH1), 0, byte(vm.RETURN)}
	statedb := address32State(t, cfg, types.GenesisAlloc{
		user: {Balance: big.NewInt(3), Code: userCode},
	}, map[common.Address32]types.Account{
		parent: {Balance: big.NewInt(20)},
	})
	userWord := address20Word(user)
	run := runAddress32State(t, cfg, caller, parent, delegateReturning(vm.DELEGATECALL32, userWord[:]), statedb, nil)

	if run.err != nil {
		t.Fatal(run.err)
	}

	success, ret := callResult(t, run.ret)

	if success.Sign() != 0 || ret.Sign() != 0 {
		t.Fatalf("success %s return %s, want failed delegatecall", success, ret)
	}

	if got := run.statedb.GetBalance(user); got.Cmp(uint256.NewInt(3)) != 0 {
		t.Fatalf("address20 balance = %s, want 3", got)
	}

	precompile := runAddress32(t, cfg, caller, parent, delegateCallWithInput(address20Word(identity), []byte{1, 2, 3, 4}), nil)

	if precompile.err != nil {
		t.Fatal(precompile.err)
	}

	success, ret = callResult(t, precompile.ret)

	if success.Sign() != 0 || ret.Sign() != 0 {
		t.Fatalf("precompile success %s return %s", success, ret)
	}

	if got := precompile.statedb.GetBalance(identity); got.Sign() != 0 {
		t.Fatalf("precompile balance = %s, want 0", got)
	}
}

func TestAddress32Selfdestruct32SendsBalanceAndKeepsExistingCode(t *testing.T) {
	cfg := address32Config()
	caller := address32Word(0xc1)
	contract := address32Word(0xc2)
	address32Beneficiary := address32Word(0xc3)
	address20Beneficiary := common.HexToAddress("0x8888888888888888888888888888888888888888")
	code := append(push32(address32Beneficiary[:]), byte(vm.SELFDESTRUCT32))
	run := runAddress32(t, cfg, caller, contract, code, map[common.Address32]types.Account{
		contract: {Balance: big.NewInt(8)},
	})

	if run.err != nil {
		t.Fatal(run.err)
	}

	if got := run.statedb.Address32Balance(address32Beneficiary); got.Cmp(uint256.NewInt(8)) != 0 {
		t.Fatalf("address32 beneficiary = %s, want 8", got)
	}

	if got := run.statedb.Address32Balance(contract); got.Sign() != 0 {
		t.Fatalf("contract balance = %s, want 0", got)
	}

	if !bytes.Equal(run.statedb.Address32Code(contract), code) {
		t.Fatal("existing address32 lost its code")
	}

	if run.statedb.Address32SelfDestructed(contract) {
		t.Fatal("existing address32 was deleted")
	}

	beneficiaryWord := address20Word(address20Beneficiary)
	address20Code := append(push32(beneficiaryWord[:]), byte(vm.SELFDESTRUCT32))
	address20Run := runAddress32(t, cfg, caller, contract, address20Code, map[common.Address32]types.Account{
		contract: {Balance: big.NewInt(4)},
	})

	if address20Run.err != nil {
		t.Fatal(address20Run.err)
	}

	if got := address20Run.statedb.GetBalance(address20Beneficiary); got.Cmp(uint256.NewInt(4)) != 0 {
		t.Fatalf("address20 beneficiary = %s, want 4", got)
	}

	if address20Run.statedb.Address32SelfDestructed(contract) {
		t.Fatal("existing address32 was deleted for an address20 beneficiary")
	}
}

func TestAddress32Selfdestruct32InStaticCallIsWriteProtected(t *testing.T) {
	cfg := address32Config()
	caller := address32Word(0xe1)
	parent := address32Word(0xe2)
	callee := address32Word(0xe3)
	beneficiary := address32Word(0xe4)
	code := append(push32(beneficiary[:]), byte(vm.SELFDESTRUCT32))
	run := runAddress32(t, cfg, caller, parent, staticCallReturning(callee[:]), map[common.Address32]types.Account{
		callee: {Balance: big.NewInt(6), Code: code},
	})

	if run.err != nil {
		t.Fatal(run.err)
	}

	success, ret := callResult(t, run.ret)

	if success.Sign() != 0 || ret.Sign() != 0 {
		t.Fatalf("success %s return %s, want failed static call", success, ret)
	}

	if got := run.statedb.Address32Balance(callee); got.Cmp(uint256.NewInt(6)) != 0 {
		t.Fatalf("callee balance = %s, want 6", got)
	}

	if got := run.statedb.Address32Balance(beneficiary); got.Sign() != 0 {
		t.Fatalf("beneficiary balance = %s, want 0", got)
	}
}

func TestAddress32Selfdestruct32DeletesContractCreatedInThisTransaction(t *testing.T) {
	cfg := address32Config()
	caller := address32Word(0xd1)
	parent := address32Word(0xd2)
	beneficiary := address32Word(0xd3)
	init := append(push32(beneficiary[:]), byte(vm.SELFDESTRUCT32))
	prefix := []byte{
		byte(vm.PUSH1), byte(len(init)),
		byte(vm.PUSH1), 15,
		byte(vm.PUSH1), 0,
		byte(vm.CODECOPY),
		byte(vm.PUSH1), byte(len(init)),
		byte(vm.PUSH1), 0,
		byte(vm.PUSH1), 10,
		byte(vm.CREATE32),
		byte(vm.STOP),
	}

	if len(prefix) != 15 {
		t.Fatalf("initcode offset = %d, want 15", len(prefix))
	}

	run := runAddress32(t, cfg, caller, parent, append(prefix, init...), map[common.Address32]types.Account{
		parent: {Balance: big.NewInt(10)},
	})

	if run.err != nil {
		t.Fatal(run.err)
	}

	child := crypto.CreateAddress32(parent, 0)

	if got := run.statedb.Address32Balance(beneficiary); got.Cmp(uint256.NewInt(10)) != 0 {
		t.Fatalf("beneficiary = %s, want 10", got)
	}

	if got := run.statedb.Address32Balance(parent); got.Sign() != 0 {
		t.Fatalf("parent balance = %s, want 0", got)
	}

	if !run.statedb.Address32SelfDestructed(child) {
		t.Fatal("contract created in this transaction was not deleted")
	}

	root, err := run.statedb.Commit(1, false, false)

	if err != nil {
		t.Fatal(err)
	}

	reopened, err := state.New(root, run.statedb.Database())

	if err != nil {
		t.Fatal(err)
	}

	if reopened.Address32Exist(child) || reopened.Address32Nonce(child) != 0 || len(reopened.Address32Code(child)) != 0 {
		t.Fatalf("committed child nonce %d code %x", reopened.Address32Nonce(child), reopened.Address32Code(child))
	}

	if got := reopened.Address32Balance(beneficiary); got.Cmp(uint256.NewInt(10)) != 0 {
		t.Fatalf("committed beneficiary = %s, want 10", got)
	}
}

func TestAddress32CallIntoOtherNamespaceReverts(t *testing.T) {
	cfg := address32Config()
	caller := address32Word(0x61)
	contract := address32Word(0x60)
	user := common.HexToAddress("0x7777777777777777777777777777777777777777")
	word := address20Word(user)
	userCode := []byte{byte(vm.PUSH1), 0x2a, byte(vm.PUSH1), 0, byte(vm.MSTORE), byte(vm.PUSH1), 32, byte(vm.PUSH1), 0, byte(vm.RETURN)}
	statedb := address32State(t, cfg, types.GenesisAlloc{
		user: {Balance: big.NewInt(3), Code: userCode},
	}, map[common.Address32]types.Account{
		contract: {Balance: big.NewInt(100)},
	})
	run := runAddress32State(t, cfg, caller, contract, callReturning(vm.CALL32, word[:], 5), statedb, nil)

	if run.err != nil {
		t.Fatal(run.err)
	}

	success, ret := callResult(t, run.ret)

	if success.Sign() != 0 || ret.Sign() != 0 {
		t.Fatalf("success %s return %s, want failed call", success, ret)
	}

	if got := run.statedb.GetBalance(user); got.Cmp(uint256.NewInt(3)) != 0 {
		t.Fatalf("address20 balance = %s, want 3", got)
	}

	if got := run.statedb.Address32Balance(contract); got.Cmp(uint256.NewInt(100)) != 0 {
		t.Fatalf("contract balance = %s, want 100", got)
	}
}

func TestAddress32StaticCall32RunsPrecompile(t *testing.T) {
	cfg := address32Config()
	caller := address32Word(0x81)
	contract := address32Word(0x80)
	identity := common.BytesToAddress([]byte{4})
	word := address20Word(identity)
	code := staticCallWithInput(word, []byte{1, 2, 3, 4})
	run := runAddress32(t, cfg, caller, contract, code, nil)

	if run.err != nil {
		t.Fatal(run.err)
	}

	if !bytes.Equal(run.ret, []byte{1, 2, 3, 4}) {
		t.Fatalf("identity return = %x", run.ret)
	}

	if got := run.statedb.GetBalance(identity); got.Sign() != 0 {
		t.Fatalf("precompile balance = %s, want 0", got)
	}
}

func TestAddress32Call32RunsPrecompile(t *testing.T) {
	cfg := address32Config()
	caller := address32Word(0x71)
	contract := address32Word(0x70)
	identity := common.BytesToAddress([]byte{4})
	p256 := common.BytesToAddress([]byte{1, 0})
	rules := cfg.Rules(big.NewInt(1), true, 1)
	active := vm.ActivePrecompiles(rules)

	if !rules.IsOptimismJovian || len(active) != len(vm.PrecompiledAddressesJovian) {
		t.Fatalf("active precompiles = %d, jovian = %d", len(active), len(vm.PrecompiledAddressesJovian))
	}

	for _, addr := range vm.PrecompiledAddressesJovian {
		found := false

		for _, got := range active {
			if got == addr {
				found = true
			}
		}

		if !found {
			t.Fatalf("jovian precompile %s is not active", addr.Hex())
		}
	}

	code := callWithInput(vm.CALL32, address20Word(identity), 9, []byte{1, 2, 3, 4})
	run := runAddress32(t, cfg, caller, contract, code, map[common.Address32]types.Account{
		contract: {Balance: big.NewInt(100)},
	})

	if run.err != nil {
		t.Fatal(run.err)
	}

	if !bytes.Equal(run.ret, []byte{1, 2, 3, 4}) {
		t.Fatalf("identity return = %x", run.ret)
	}

	if got := run.statedb.GetBalance(identity); got.Cmp(uint256.NewInt(9)) != 0 {
		t.Fatalf("precompile balance = %s, want 9", got)
	}

	plain := address20Word(common.HexToAddress("0x2020202020202020202020202020202020202020"))
	p256Word := address20Word(p256)
	precompileRun := runAddress32(t, cfg, caller, contract, callReturning(vm.CALL32, p256Word[:], 0), nil)
	plainRun := runAddress32(t, cfg, caller, contract, callReturning(vm.CALL32, plain[:], 0), nil)

	if precompileRun.err != nil || plainRun.err != nil {
		t.Fatalf("p256 %v plain %v", precompileRun.err, plainRun.err)
	}

	if precompileRun.gas-plainRun.gas != params.P256VerifyGasFjord {
		t.Fatalf("p256 gas difference = %d, want %d", precompileRun.gas-plainRun.gas, params.P256VerifyGasFjord)
	}
}

func TestAddress32TopLevelPrecompileAndCall(t *testing.T) {
	cfg := address32Config()
	key := anvilKey(t)
	sender, err := crypto.PubkeyToAddress32(key.PublicKey)

	if err != nil {
		t.Fatal(err)
	}

	contract := address32Word(0x81)
	code := []byte{
		byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.LOG0),
		byte(vm.PUSH1), 0x2a, byte(vm.PUSH1), 0, byte(vm.MSTORE),
		byte(vm.PUSH1), 32, byte(vm.PUSH1), 0, byte(vm.RETURN),
	}
	statedb := address32State(t, cfg, types.GenesisAlloc{
		crypto.PubkeyToAddress(key.PublicKey): {Balance: big.NewInt(params.Ether)},
		types.L1BlockAddr:                     address32L1Account(),
	}, map[common.Address32]types.Account{
		sender:   {Balance: big.NewInt(params.Ether)},
		contract: {Code: code},
	})
	signed := signAddress32Data(t, cfg, key, contract, 0, 100_000, 0, nil)
	statedb.SetTxContext(signed.Hash(), 0)
	receipt, err := applyAddress32Tx(cfg, statedb, signed)

	if err != nil {
		t.Fatal(err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("status = %d", receipt.Status)
	}

	if len(receipt.Logs) != 1 || receipt.Logs[0].Address32 == nil || *receipt.Logs[0].Address32 != contract {
		t.Fatalf("log = %+v", receipt.Logs)
	}

	var want types.Bloom
	want.Add(contract[:])

	for _, topic := range receipt.Logs[0].Topics {
		want.Add(topic[:])
	}

	if receipt.Bloom != want {
		t.Fatal("address32 logger bloom does not use the 32-byte word")
	}

	address20 := common.HexToAddress("0x9999999999999999999999999999999999999999")
	address20Bloom := types.CreateBloom(&types.Receipt{Logs: []*types.Log{{Address: address20}}})
	var address20Want types.Bloom
	address20Want.Add(address20.Bytes())
	paddedWord := address20Word(address20)
	var padded types.Bloom
	padded.Add(paddedWord[:])

	if address20Bloom != address20Want || address20Bloom == padded {
		t.Fatal("address20 logger bloom changed")
	}

	identity := common.BytesToAddress([]byte{4})
	precompile := signAddress32Data(t, cfg, key, address20Word(identity), 1, 100_000, 7, []byte{1, 2, 3, 4})
	result, err := applyAddress32Result(cfg, statedb, precompile)

	if err != nil {
		t.Fatal(err)
	}

	if result.Err != nil {
		t.Fatal(result.Err)
	}

	if !bytes.Equal(result.ReturnData, []byte{1, 2, 3, 4}) {
		t.Fatalf("precompile return = %x", result.ReturnData)
	}

	if got := statedb.GetBalance(identity); got.Cmp(uint256.NewInt(7)) != 0 {
		t.Fatalf("precompile balance = %s, want 7", got)
	}
}

func TestAddress32LogRoundTrip(t *testing.T) {
	logger := address32Word(0x91)
	log := &types.Log{
		Address32: &logger,
		Topics:    []common.Hash{{1}},
		Data:      []byte{9},
		TxHash:    common.HexToHash("0x11"),
	}
	raw, err := rlp.EncodeToBytes(log)

	if err != nil {
		t.Fatal(err)
	}

	decoded := new(types.Log)

	if err := rlp.DecodeBytes(raw, decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Address32 == nil || *decoded.Address32 != logger || len(decoded.Topics) != 1 || decoded.Topics[0] != log.Topics[0] || !bytes.Equal(decoded.Data, log.Data) {
		t.Fatalf("decoded = %+v", decoded)
	}

	address20 := &types.Log{Address: common.HexToAddress("0x2222222222222222222222222222222222222222"), Data: []byte{1}, TxHash: log.TxHash}
	address20Raw, err := rlp.EncodeToBytes(address20)

	if err != nil {
		t.Fatal(err)
	}

	address20Decoded := new(types.Log)

	if err := rlp.DecodeBytes(address20Raw, address20Decoded); err != nil {
		t.Fatal(err)
	}

	if address20Decoded.Address32 != nil || address20Decoded.Address != address20.Address {
		t.Fatalf("address20 log = %+v", address20Decoded)
	}

	jsonRaw, err := log.MarshalJSON()

	if err != nil {
		t.Fatal(err)
	}

	fromJSON := new(types.Log)

	if err := fromJSON.UnmarshalJSON(jsonRaw); err != nil {
		t.Fatal(err)
	}

	if fromJSON.Address32 == nil || *fromJSON.Address32 != logger {
		t.Fatalf("json logger = %v", fromJSON.Address32)
	}
}

type address32Run struct {
	ret     []byte
	gas     uint64
	left    uint64
	err     error
	statedb *state.StateDB
}

func runAddress32(t *testing.T, cfg *params.ChainConfig, caller, contract common.Address32, code []byte, accounts map[common.Address32]types.Account) address32Run {
	t.Helper()
	return runAddress32Paying(t, cfg, caller, contract, code, uint256.NewInt(0), accounts)
}

func runAddress32Paying(t *testing.T, cfg *params.ChainConfig, caller, contract common.Address32, code []byte, value *uint256.Int, accounts map[common.Address32]types.Account) address32Run {
	t.Helper()
	statedb := address32State(t, cfg, nil, nil)
	return runAddress32StateValue(t, cfg, caller, contract, code, statedb, accounts, value)
}

func runAddress32State(t *testing.T, cfg *params.ChainConfig, caller, contract common.Address32, code []byte, statedb *state.StateDB, accounts map[common.Address32]types.Account) address32Run {
	t.Helper()
	return runAddress32StateValue(t, cfg, caller, contract, code, statedb, accounts, uint256.NewInt(0))
}

func runAddress32StateValue(t *testing.T, cfg *params.ChainConfig, caller, contract common.Address32, code []byte, statedb *state.StateDB, accounts map[common.Address32]types.Account, value *uint256.Int) address32Run {
	t.Helper()

	for addr, account := range accounts {
		if account.Code != nil {
			statedb.SetAddress32Code(addr, account.Code)
		}

		if account.Balance != nil {
			statedb.AddAddress32Balance(addr, uint256.MustFromBig(account.Balance))
		}
	}

	statedb.SetAddress32Code(contract, code)
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
	origin := caller
	evm.SetTxContext(vm.TxContext{Origin32: &origin, GasPrice: big.NewInt(1)})
	ret, left, err := evm.CallAccount(common.Address32StateAddress(caller), common.Address32StateAddress(contract), nil, opcodeCallGas, value, false)
	return address32Run{ret: ret, gas: opcodeCallGas - left, left: left, err: err, statedb: statedb}
}

func delegateTargetCode() []byte {
	return []byte{
		byte(vm.PUSH1), 0x42,
		byte(vm.PUSH1), 0,
		byte(vm.SSTORE),
		byte(vm.ADDRESS),
		byte(vm.PUSH1), 0,
		byte(vm.MSTORE),
		byte(vm.CALLER),
		byte(vm.PUSH1), 32,
		byte(vm.MSTORE),
		byte(vm.CALLVALUE),
		byte(vm.PUSH1), 64,
		byte(vm.MSTORE),
		byte(vm.PUSH1), 96,
		byte(vm.PUSH1), 0,
		byte(vm.RETURN),
	}
}

func delegateContextReturning(word []byte) []byte {
	code := []byte{
		byte(vm.PUSH1), 96,
		byte(vm.PUSH1), 32,
		byte(vm.PUSH1), 0,
		byte(vm.PUSH1), 0,
	}
	code = append(code, push32(word)...)
	code = append(code, push32Uint(500_000)...)
	return append(code, byte(vm.DELEGATECALL32), byte(vm.PUSH1), 0, byte(vm.MSTORE), byte(vm.PUSH1), 128, byte(vm.PUSH1), 0, byte(vm.RETURN))
}

func callContextReturning(word []byte) []byte {
	code := []byte{
		byte(vm.PUSH1), 96,
		byte(vm.PUSH1), 32,
		byte(vm.PUSH1), 0,
		byte(vm.PUSH1), 0,
	}
	code = append(code, push32Uint(0)...)
	code = append(code, push32(word)...)
	code = append(code, push32Uint(500_000)...)
	return append(code, byte(vm.CALL32), byte(vm.PUSH1), 0, byte(vm.MSTORE), byte(vm.PUSH1), 128, byte(vm.PUSH1), 0, byte(vm.RETURN))
}

func delegateReturning(op vm.OpCode, word []byte) []byte {
	code := []byte{
		byte(vm.PUSH1), 32,
		byte(vm.PUSH1), 32,
		byte(vm.PUSH1), 0,
		byte(vm.PUSH1), 0,
	}
	code = append(code, push32(word)...)
	code = append(code, push32Uint(500_000)...)
	return append(code, byte(op), byte(vm.PUSH1), 0, byte(vm.MSTORE), byte(vm.PUSH1), 64, byte(vm.PUSH1), 0, byte(vm.RETURN))
}

func delegateCallWithInput(word common.Address32, input []byte) []byte {
	var padded [32]byte
	copy(padded[32-len(input):], input)
	offset := byte(32 - len(input))
	code := append(push32(padded[:]), byte(vm.PUSH1), 0, byte(vm.MSTORE))
	code = append(code, byte(vm.PUSH1), 32, byte(vm.PUSH1), 32)
	code = append(code, byte(vm.PUSH1), byte(len(input)), byte(vm.PUSH1), offset)
	code = append(code, push32(word[:])...)
	code = append(code, push32Uint(500_000)...)
	return append(code, byte(vm.DELEGATECALL32), byte(vm.PUSH1), 0, byte(vm.MSTORE), byte(vm.PUSH1), 64, byte(vm.PUSH1), 0, byte(vm.RETURN))
}

func staticCallReturning(word []byte) []byte {
	code := []byte{byte(vm.PUSH1), 32, byte(vm.PUSH1), 32, byte(vm.PUSH1), 0, byte(vm.PUSH1), 0}
	code = append(code, push32(word)...)
	code = append(code, push32Uint(500_000)...)
	return append(code, byte(vm.STATICCALL32), byte(vm.PUSH1), 0, byte(vm.MSTORE), byte(vm.PUSH1), 64, byte(vm.PUSH1), 0, byte(vm.RETURN))
}

func staticCallWithInput(word common.Address32, input []byte) []byte {
	var padded [32]byte
	copy(padded[32-len(input):], input)
	offset := byte(32 - len(input))
	code := append(push32(padded[:]), byte(vm.PUSH1), 0, byte(vm.MSTORE))
	code = append(code, byte(vm.PUSH1), byte(len(input)), byte(vm.PUSH1), 32)
	code = append(code, byte(vm.PUSH1), byte(len(input)), byte(vm.PUSH1), offset)
	code = append(code, push32(word[:])...)
	code = append(code, push32Uint(500_000)...)
	return append(code, byte(vm.STATICCALL32), byte(vm.PUSH1), byte(len(input)), byte(vm.PUSH1), 32, byte(vm.RETURN))
}

func callWithInput(op vm.OpCode, word common.Address32, value uint64, input []byte) []byte {
	var padded [32]byte
	copy(padded[32-len(input):], input)
	offset := byte(32 - len(input))
	code := append(push32(padded[:]), byte(vm.PUSH1), 0, byte(vm.MSTORE))
	code = append(code, byte(vm.PUSH1), byte(len(input)), byte(vm.PUSH1), 32)
	code = append(code, byte(vm.PUSH1), byte(len(input)), byte(vm.PUSH1), offset)
	code = append(code, push32Uint(value)...)
	code = append(code, push32(word[:])...)
	code = append(code, push32Uint(500_000)...)
	return append(code, byte(op), byte(vm.PUSH1), byte(len(input)), byte(vm.PUSH1), 32, byte(vm.RETURN))
}

func signAddress32Data(t *testing.T, cfg *params.ChainConfig, key *ecdsa.PrivateKey, to common.Address32, nonce, gas uint64, value int64, data []byte) *types.Transaction {
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
		Gas:        gas,
		To:         word,
		Value:      big.NewInt(value),
		Data:       data,
		SenderKind: types.SenderKindAddress32,
	}), types.LatestSigner(cfg), key)

	if err != nil {
		t.Fatal(err)
	}

	return signed
}

func applyAddress32Result(cfg *params.ChainConfig, statedb *state.StateDB, tx *types.Transaction) (*ExecutionResult, error) {
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
	msg, err := TransactionToMessage(tx, types.MakeSigner(cfg, header.Number, header.Time), header.BaseFee)

	if err != nil {
		return nil, err
	}

	gp := new(GasPool).AddGas(header.GasLimit)
	statedb.SetTxContext(tx.Hash(), 1)
	return ApplyMessage(evm, msg, gp)
}
