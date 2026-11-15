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
	"encoding/binary"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

const opcodeCallGas = 4_000_000

var (
	opcodeCaller   = common.HexToAddress("0x1000000000000000000000000000000000000001")
	opcodeContract = common.HexToAddress("0x2000000000000000000000000000000000000002")
)

func TestAddress32OpcodeBytes(t *testing.T) {
	if vm.BALANCE32 != 0xa5 || vm.EXTCODESIZE32 != 0xa6 || vm.EXTCODECOPY32 != 0xa7 || vm.EXTCODEHASH32 != 0xa8 || vm.CALL32 != 0xa9 || vm.STATICCALL32 != 0xab {
		t.Fatalf("opcodes = %#x %#x %#x %#x %#x %#x", vm.BALANCE32, vm.EXTCODESIZE32, vm.EXTCODECOPY32, vm.EXTCODEHASH32, vm.CALL32, vm.STATICCALL32)
	}

	if vm.CREATE32 != 0xac || vm.CREATE232 != 0xad {
		t.Fatalf("create opcodes = %#x %#x", vm.CREATE32, vm.CREATE232)
	}
}

func TestAddress20ContractReadsAddress32(t *testing.T) {
	code := []byte{0x60, 0x2a, 0x60, 0x00, 0x52}
	coded := address32Word(0x11)
	codeless := address32Word(0x22)
	missing := address32Word(0x33)
	address20 := common.HexToAddress("0x3333333333333333333333333333333333333333")
	address20Padded := address20Word(address20)

	alloc := types.GenesisAlloc{
		address20: {Balance: big.NewInt(4242), Code: code},
	}
	address32Accounts := map[common.Address32]types.Account{
		coded:    {Balance: big.NewInt(123456), Code: code},
		codeless: {Balance: big.NewInt(9)},
	}
	codeHash := crypto.Keccak256Hash(code)

	cases := []struct {
		name string
		code []byte
		want *uint256.Int
	}{
		{"BALANCE32", readWord(vm.BALANCE32, coded[:]), uint256.NewInt(123456)},
		{"EXTCODESIZE32", readWord(vm.EXTCODESIZE32, coded[:]), uint256.NewInt(uint64(len(code)))},
		{"EXTCODEHASH32", readWord(vm.EXTCODEHASH32, coded[:]), new(uint256.Int).SetBytes(codeHash.Bytes())},
		{"EXTCODEHASH32 codeless", readWord(vm.EXTCODEHASH32, codeless[:]), new(uint256.Int).SetBytes(types.EmptyCodeHash.Bytes())},
		{"EXTCODESIZE32 codeless", readWord(vm.EXTCODESIZE32, codeless[:]), uint256.NewInt(0)},
		{"BALANCE32 missing", readWord(vm.BALANCE32, missing[:]), uint256.NewInt(0)},
		{"EXTCODEHASH32 missing", readWord(vm.EXTCODEHASH32, missing[:]), uint256.NewInt(0)},
		{"BALANCE32 address20 word", readWord(vm.BALANCE32, address20Padded[:]), uint256.NewInt(4242)},
		{"EXTCODESIZE32 address20 word", readWord(vm.EXTCODESIZE32, address20Padded[:]), uint256.NewInt(uint64(len(code)))},
		{"EXTCODEHASH32 address20 word", readWord(vm.EXTCODEHASH32, address20Padded[:]), new(uint256.Int).SetBytes(codeHash.Bytes())},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runOpcode(t, address32Config(), tc.code, alloc, address32Accounts)

			if got.err != nil {
				t.Fatal(got.err)
			}

			if wordResult(t, got.ret).Cmp(tc.want) != 0 {
				t.Fatalf("result = %s, want %s", wordResult(t, got.ret), tc.want)
			}
		})
	}

	copied := runOpcode(t, address32Config(), extCodeCopy(vm.EXTCODECOPY32, coded[:], 4), alloc, address32Accounts)

	if copied.err != nil {
		t.Fatal(copied.err)
	}

	if !bytes.Equal(copied.ret, code[:4]) {
		t.Fatalf("EXTCODECOPY32 = %x, want %x", copied.ret, code[:4])
	}

	address20Copy := runOpcode(t, address32Config(), extCodeCopy(vm.EXTCODECOPY32, address20Padded[:], 4), alloc, address32Accounts)

	if address20Copy.err != nil {
		t.Fatal(address20Copy.err)
	}

	if !bytes.Equal(address20Copy.ret, code[:4]) {
		t.Fatalf("EXTCODECOPY32 address20 word = %x, want %x", address20Copy.ret, code[:4])
	}
}

func TestAddress32OpcodeGasMatchesMirroredOpcodes(t *testing.T) {
	code := []byte{0x60, 0x2a, 0x60, 0x00, 0x52}
	address20 := common.HexToAddress("0x3333333333333333333333333333333333333333")
	address32 := address32Word(0x11)
	address20Padded := address20Word(address20)
	callAddress20 := common.HexToAddress("0x4444444444444444444444444444444444444444")
	callAddress32 := address32Word(0x22)
	callAddress20Padded := address20Word(callAddress20)
	missingAddress20 := common.HexToAddress("0x5555555555555555555555555555555555555555")
	missingAddress32 := address32Word(0x33)
	missingAddress20Padded := address20Word(missingAddress20)

	alloc := types.GenesisAlloc{
		address20:     {Balance: big.NewInt(50), Code: code},
		callAddress20: {Balance: big.NewInt(50)},
	}
	address32Alloc := map[common.Address32]types.Account{
		address32:     {Balance: big.NewInt(50), Code: code},
		callAddress32: {Balance: big.NewInt(50)},
	}

	cases := []struct {
		name      string
		address20 []byte
		address32 []byte
	}{
		{"BALANCE cold", readWord(vm.BALANCE, address20Padded[:]), readWord(vm.BALANCE32, address32[:])},
		{"BALANCE cold and warm", readWordTwice(vm.BALANCE, address20Padded[:]), readWordTwice(vm.BALANCE32, address32[:])},
		{"EXTCODESIZE cold", readWord(vm.EXTCODESIZE, address20Padded[:]), readWord(vm.EXTCODESIZE32, address32[:])},
		{"EXTCODESIZE cold and warm", readWordTwice(vm.EXTCODESIZE, address20Padded[:]), readWordTwice(vm.EXTCODESIZE32, address32[:])},
		{"EXTCODEHASH cold", readWord(vm.EXTCODEHASH, address20Padded[:]), readWord(vm.EXTCODEHASH32, address32[:])},
		{"EXTCODEHASH cold and warm", readWordTwice(vm.EXTCODEHASH, address20Padded[:]), readWordTwice(vm.EXTCODEHASH32, address32[:])},
		{"EXTCODECOPY cold", extCodeCopy(vm.EXTCODECOPY, address20Padded[:], 4), extCodeCopy(vm.EXTCODECOPY32, address32[:], 4)},
		{"EXTCODECOPY cold and warm", extCodeCopyTwice(vm.EXTCODECOPY, address20Padded[:], 4), extCodeCopyTwice(vm.EXTCODECOPY32, address32[:], 4)},
		{"CALL cold", callOnce(vm.CALL, callAddress20Padded[:], 0), callOnce(vm.CALL32, callAddress32[:], 0)},
		{"CALL cold and warm", callTwice(vm.CALL, callAddress20Padded[:], 0), callTwice(vm.CALL32, callAddress32[:], 0)},
		{"CALL value", callOnce(vm.CALL, callAddress20Padded[:], 1), callOnce(vm.CALL32, callAddress32[:], 1)},
		{"CALL new account", callOnce(vm.CALL, missingAddress20Padded[:], 1), callOnce(vm.CALL32, missingAddress32[:], 1)},
	}

	cfg := address32Config()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			address20Run := runOpcode(t, cfg, tc.address20, alloc, address32Alloc)
			address32Run := runOpcode(t, cfg, tc.address32, alloc, address32Alloc)

			if address20Run.err != nil {
				t.Fatal(address20Run.err)
			}

			if address32Run.err != nil {
				t.Fatal(address32Run.err)
			}

			if address20Run.gas != address32Run.gas {
				t.Fatalf("gas = %d, mirrored = %d", address32Run.gas, address20Run.gas)
			}
		})
	}
}

func TestAddress32WarmthIsNotAddress20Warmth(t *testing.T) {
	address32 := address32Word(0x11)
	cropped := common.BytesToAddress(address32[12:])
	croppedWord := address20Word(cropped)
	cfg := address32Config()
	alloc := types.GenesisAlloc{
		cropped: {Balance: big.NewInt(1)},
	}
	address32Alloc := map[common.Address32]types.Account{
		address32: {Balance: big.NewInt(2)},
	}

	separate := runOpcode(t, cfg, readTwo(vm.BALANCE, croppedWord[:], vm.BALANCE32, address32[:]), alloc, address32Alloc)
	same := runOpcode(t, cfg, readWordTwice(vm.BALANCE32, address32[:]), alloc, address32Alloc)

	if separate.err != nil {
		t.Fatal(separate.err)
	}

	if same.err != nil {
		t.Fatal(same.err)
	}

	if separate.gas < same.gas {
		t.Fatalf("separate gas %d < same-account gas %d", separate.gas, same.gas)
	}

	want := params.ColdAccountAccessCostEIP2929 - params.WarmStorageReadCostEIP2929

	if separate.gas-same.gas != want {
		t.Fatalf("cold minus warm = %d, want %d", separate.gas-same.gas, want)
	}
}

func TestCall32PaysCodelessAddress32(t *testing.T) {
	address32 := address32Word(0x44)
	run := runOpcode(t, address32Config(), callReturning(vm.CALL32, address32[:], 1000), nil, map[common.Address32]types.Account{
		address32: {Balance: big.NewInt(50)},
	})

	if run.err != nil {
		t.Fatal(run.err)
	}

	success, ret := callResult(t, run.ret)

	if success.Cmp(uint256.NewInt(1)) != 0 || ret.Sign() != 0 {
		t.Fatalf("success %s return %s", success, ret)
	}

	if got := run.statedb.Address32Balance(address32); got.Cmp(uint256.NewInt(1050)) != 0 {
		t.Fatalf("address32 balance = %s, want 1050", got)
	}

	left := uint256.NewInt(params.Ether)
	left.Sub(left, uint256.NewInt(1000))

	if got := run.statedb.GetBalance(opcodeContract); got.Cmp(left) != 0 {
		t.Fatalf("caller balance = %s, want %s", got, left)
	}

	cropped := common.BytesToAddress(address32[12:])

	if got := run.statedb.GetBalance(cropped); !got.IsZero() {
		t.Fatalf("cropped address20 balance = %s", got)
	}
}

func TestCall32ToMissingAddress32(t *testing.T) {
	missing := address32Word(0x46)
	paid := runOpcode(t, address32Config(), callReturning(vm.CALL32, missing[:], 1000), nil, nil)

	if paid.err != nil {
		t.Fatal(paid.err)
	}

	success, _ := callResult(t, paid.ret)

	if success.Cmp(uint256.NewInt(1)) != 0 {
		t.Fatalf("success = %s, want 1", success)
	}

	if got := paid.statedb.Address32Balance(missing); got.Cmp(uint256.NewInt(1000)) != 0 {
		t.Fatalf("missing address32 balance = %s, want 1000", got)
	}

	untouched := address32Word(0x47)
	zero := runOpcode(t, address32Config(), callReturning(vm.CALL32, untouched[:], 0), nil, nil)

	if zero.err != nil {
		t.Fatal(zero.err)
	}

	success, _ = callResult(t, zero.ret)

	if success.Cmp(uint256.NewInt(1)) != 0 {
		t.Fatalf("zero-value success = %s, want 1", success)
	}

	if zero.statedb.Address32Exist(untouched) {
		t.Fatal("zero-value call created an address32 account")
	}
}

func TestCall32ToAddress32CodeRevertsSubcall(t *testing.T) {
	address32 := address32Word(0x55)
	code := []byte{byte(vm.PUSH1), 0x2a, byte(vm.PUSH1), 0, byte(vm.MSTORE), byte(vm.PUSH1), 32, byte(vm.PUSH1), 0, byte(vm.RETURN)}
	run := runOpcode(t, address32Config(), callReturning(vm.CALL32, address32[:], 1000), nil, map[common.Address32]types.Account{
		address32: {Balance: big.NewInt(77), Code: code},
	})

	if run.err != nil {
		t.Fatal(run.err)
	}

	success, ret := callResult(t, run.ret)

	if success.Sign() != 0 || ret.Sign() != 0 {
		t.Fatalf("success %s return %s, want failed call and no return data", success, ret)
	}

	if got := run.statedb.Address32Balance(address32); got.Cmp(uint256.NewInt(77)) != 0 {
		t.Fatalf("address32 balance = %s, want 77", got)
	}

	if got := run.statedb.GetBalance(opcodeContract); got.Cmp(uint256.NewInt(params.Ether)) != 0 {
		t.Fatalf("caller balance = %s, want 1 ether", got)
	}
}

func TestCall32DelegationResolutionGas(t *testing.T) {
	target := common.HexToAddress("0x8888888888888888888888888888888888888888")
	designator := address32Word(0x88)
	plain := address32Word(0x89)
	accounts := map[common.Address32]types.Account{
		designator: {Code: types.AddressToDelegation(target)},
		plain:      {Code: make([]byte, 23)},
	}
	alloc := types.GenesisAlloc{
		target: {Balance: big.NewInt(1)},
	}
	cfg := address32Config()

	coldDesignator := runOpcode(t, cfg, callReturning(vm.CALL32, designator[:], 0), alloc, accounts)
	coldPlain := runOpcode(t, cfg, callReturning(vm.CALL32, plain[:], 0), alloc, accounts)
	assertDelegationCall(t, coldDesignator, coldPlain, params.ColdAccountAccessCostEIP2929)

	if !coldDesignator.statedb.AddressInAccessList(target) {
		t.Fatal("delegation target stayed cold")
	}

	if coldPlain.statedb.AddressInAccessList(target) {
		t.Fatal("plain code warmed the delegation target")
	}

	warmTarget := address20Word(target)
	warmDesignator := runOpcode(t, cfg, balanceThenCall(vm.CALL32, warmTarget[:], designator[:]), alloc, accounts)
	warmPlain := runOpcode(t, cfg, balanceThenCall(vm.CALL32, warmTarget[:], plain[:]), alloc, accounts)
	assertDelegationCall(t, warmDesignator, warmPlain, params.WarmStorageReadCostEIP2929)
}

func assertDelegationCall(t *testing.T, designator, plain opcodeRun, want uint64) {
	t.Helper()

	if designator.err != nil {
		t.Fatal(designator.err)
	}

	if plain.err != nil {
		t.Fatal(plain.err)
	}

	designatorSuccess, _ := callResult(t, designator.ret)
	plainSuccess, _ := callResult(t, plain.ret)

	if designatorSuccess.Sign() != 0 || plainSuccess.Sign() != 0 {
		t.Fatalf("success designator %s plain %s, want 0", designatorSuccess, plainSuccess)
	}

	if designator.gas < plain.gas || designator.gas-plain.gas != want {
		t.Fatalf("gas difference = %d, want %d", designator.gas-plain.gas, want)
	}
}

func TestCall32ToAddress20WordHitsAddress20(t *testing.T) {
	address20 := common.HexToAddress("0x6666666666666666666666666666666666666666")
	word := address20Word(address20)
	code := []byte{byte(vm.PUSH1), 0x2a, byte(vm.PUSH1), 0, byte(vm.MSTORE), byte(vm.PUSH1), 32, byte(vm.PUSH1), 0, byte(vm.RETURN)}
	cfg := address32Config()

	executed := runOpcode(t, cfg, callReturning(vm.CALL32, word[:], 0), types.GenesisAlloc{
		address20: {Balance: big.NewInt(7), Code: code},
	}, nil)

	if executed.err != nil {
		t.Fatal(executed.err)
	}

	success, ret := callResult(t, executed.ret)

	if success.Cmp(uint256.NewInt(1)) != 0 || ret.Cmp(uint256.NewInt(0x2a)) != 0 {
		t.Fatalf("success %s return %s, want 1 and 42", success, ret)
	}

	paid := runOpcode(t, cfg, callReturning(vm.CALL32, word[:], 5), types.GenesisAlloc{
		address20: {Balance: big.NewInt(7)},
	}, nil)

	if paid.err != nil {
		t.Fatal(paid.err)
	}

	success, _ = callResult(t, paid.ret)

	if success.Cmp(uint256.NewInt(1)) != 0 {
		t.Fatalf("success = %s, want 1", success)
	}

	if got := paid.statedb.GetBalance(address20); got.Cmp(uint256.NewInt(12)) != 0 {
		t.Fatalf("address20 balance = %s, want 12", got)
	}
}

func TestCall32RevertUndoesPaymentAndWarmth(t *testing.T) {
	address32 := address32Word(0x66)
	code := append(callOnce(vm.CALL32, address32[:], 1000), byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.REVERT))
	run := runOpcode(t, address32Config(), code, nil, map[common.Address32]types.Account{
		address32: {Balance: big.NewInt(50)},
	})

	if !errors.Is(run.err, vm.ErrExecutionReverted) {
		t.Fatalf("err = %v, want revert", run.err)
	}

	if got := run.statedb.Address32Balance(address32); got.Cmp(uint256.NewInt(50)) != 0 {
		t.Fatalf("address32 balance = %s, want 50", got)
	}

	if run.statedb.Address32InAccessList(address32) {
		t.Fatal("reverted call left the address32 account warm")
	}
}

func TestCall32WithValueInStaticCallIsWriteProtected(t *testing.T) {
	address32 := address32Word(0x77)
	run := runOpcodeStatic(t, address32Config(), callOnce(vm.CALL32, address32[:], 1), nil, map[common.Address32]types.Account{
		address32: {Balance: big.NewInt(50)},
	})

	if !errors.Is(run.err, vm.ErrWriteProtection) {
		t.Fatalf("err = %v, want write protection", run.err)
	}

	if got := run.statedb.Address32Balance(address32); got.Cmp(uint256.NewInt(50)) != 0 {
		t.Fatalf("address32 balance = %s, want 50", got)
	}
}

func TestAddress32OpcodesUnassignedBeforeFork(t *testing.T) {
	cfg := optimismConfig()

	for _, op := range []byte{0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xab} {
		run := runOpcode(t, cfg, []byte{op}, nil, nil)
		var invalid *vm.ErrInvalidOpCode

		if !errors.As(run.err, &invalid) || invalid.Error() == "" {
			t.Fatalf("opcode %#x err = %v, want invalid opcode", op, run.err)
		}
	}
}

func TestAddress20BalanceAndCallUnchanged(t *testing.T) {
	address20 := common.HexToAddress("0x3333333333333333333333333333333333333333")
	word := address20Word(address20)
	address32 := address32Word(0x11)
	copy(address32[12:], address20[:])
	alloc := types.GenesisAlloc{
		address20: {Balance: big.NewInt(4242)},
	}
	onAlloc := map[common.Address32]types.Account{
		address32: {Balance: big.NewInt(999999)},
	}
	balanceCode := readWord(vm.BALANCE, word[:])
	on := runOpcode(t, address32Config(), balanceCode, alloc, onAlloc)
	off := runOpcode(t, optimismConfig(), balanceCode, alloc, nil)

	if on.err != nil || off.err != nil {
		t.Fatalf("on %v off %v", on.err, off.err)
	}

	if !bytes.Equal(on.ret, off.ret) || wordResult(t, on.ret).Cmp(uint256.NewInt(4242)) != 0 {
		t.Fatalf("BALANCE on %x off %x", on.ret, off.ret)
	}

	if on.gas != off.gas {
		t.Fatalf("BALANCE gas on %d off %d", on.gas, off.gas)
	}

	payCode := callReturning(vm.CALL, word[:], 5)
	payOn := runOpcode(t, address32Config(), payCode, alloc, onAlloc)
	payOff := runOpcode(t, optimismConfig(), payCode, alloc, nil)

	if payOn.err != nil || payOff.err != nil {
		t.Fatalf("CALL on %v off %v", payOn.err, payOff.err)
	}

	if payOn.gas != payOff.gas {
		t.Fatalf("CALL gas on %d off %d", payOn.gas, payOff.gas)
	}

	if got := payOn.statedb.GetBalance(address20); got.Cmp(uint256.NewInt(4247)) != 0 {
		t.Fatalf("address20 balance = %s, want 4247", got)
	}

	if got := payOn.statedb.Address32Balance(address32); got.Cmp(uint256.NewInt(999999)) != 0 {
		t.Fatalf("address32 balance = %s, want 999999", got)
	}
}

type opcodeRun struct {
	ret     []byte
	gas     uint64
	err     error
	statedb *state.StateDB
}

func optimismConfig() *params.ChainConfig {
	cfg := *params.OptimismTestConfig
	return &cfg
}

func address32Word(fill byte) common.Address32 {
	var addr common.Address32

	for i := range addr {
		addr[i] = fill
	}

	return addr
}

func runOpcode(t *testing.T, cfg *params.ChainConfig, code []byte, alloc types.GenesisAlloc, address32Accounts map[common.Address32]types.Account) opcodeRun {
	t.Helper()
	return runOpcodeCall(t, cfg, code, alloc, address32Accounts, false)
}

func runOpcodeStatic(t *testing.T, cfg *params.ChainConfig, code []byte, alloc types.GenesisAlloc, address32Accounts map[common.Address32]types.Account) opcodeRun {
	t.Helper()
	return runOpcodeCall(t, cfg, code, alloc, address32Accounts, true)
}

func runOpcodeCall(t *testing.T, cfg *params.ChainConfig, code []byte, alloc types.GenesisAlloc, address32Accounts map[common.Address32]types.Account, static bool) opcodeRun {
	t.Helper()
	merged := types.GenesisAlloc{}

	for addr, account := range alloc {
		merged[addr] = account
	}

	merged[opcodeContract] = types.Account{Balance: big.NewInt(params.Ether), Code: code}
	statedb := address32State(t, cfg, merged, address32Accounts)
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
	var (
		ret  []byte
		left uint64
		err  error
	)

	if static {
		ret, left, err = evm.StaticCall(opcodeCaller, opcodeContract, nil, opcodeCallGas)
	} else {
		ret, left, err = evm.Call(opcodeCaller, opcodeContract, nil, opcodeCallGas, uint256.NewInt(0))
	}

	return opcodeRun{ret: ret, gas: opcodeCallGas - left, err: err, statedb: statedb}
}

func wordResult(t *testing.T, ret []byte) *uint256.Int {
	t.Helper()

	if len(ret) != 32 {
		t.Fatalf("return length = %d, want 32", len(ret))
	}

	return new(uint256.Int).SetBytes(ret)
}

func callResult(t *testing.T, ret []byte) (*uint256.Int, *uint256.Int) {
	t.Helper()

	if len(ret) != 64 {
		t.Fatalf("return length = %d, want 64", len(ret))
	}

	return new(uint256.Int).SetBytes(ret[:32]), new(uint256.Int).SetBytes(ret[32:])
}

func push32(word []byte) []byte {
	out := make([]byte, 33)
	out[0] = byte(vm.PUSH32)
	copy(out[1:], word)
	return out
}

func push32Uint(n uint64) []byte {
	var word [32]byte
	binary.BigEndian.PutUint64(word[24:], n)
	return push32(word[:])
}

func readWord(op vm.OpCode, word []byte) []byte {
	code := append(push32(word), byte(op))
	return append(code, byte(vm.PUSH1), 0, byte(vm.MSTORE), byte(vm.PUSH1), 32, byte(vm.PUSH1), 0, byte(vm.RETURN))
}

func readWordTwice(op vm.OpCode, word []byte) []byte {
	return readTwo(op, word, op, word)
}

func readTwo(first vm.OpCode, firstWord []byte, second vm.OpCode, secondWord []byte) []byte {
	code := append(push32(firstWord), byte(first), byte(vm.POP))
	code = append(code, push32(secondWord)...)
	code = append(code, byte(second))
	return append(code, byte(vm.PUSH1), 0, byte(vm.MSTORE), byte(vm.PUSH1), 32, byte(vm.PUSH1), 0, byte(vm.RETURN))
}

func extCodeCopy(op vm.OpCode, word []byte, length byte) []byte {
	code := []byte{byte(vm.PUSH1), length, byte(vm.PUSH1), 0, byte(vm.PUSH1), 0}
	code = append(code, push32(word)...)
	code = append(code, byte(op), byte(vm.PUSH1), length, byte(vm.PUSH1), 0, byte(vm.RETURN))
	return code
}

func extCodeCopyTwice(op vm.OpCode, word []byte, length byte) []byte {
	once := []byte{byte(vm.PUSH1), length, byte(vm.PUSH1), 0, byte(vm.PUSH1), 0}
	once = append(once, push32(word)...)
	once = append(once, byte(op))
	code := append(append([]byte{}, once...), once...)
	return append(code, byte(vm.PUSH1), length, byte(vm.PUSH1), 0, byte(vm.RETURN))
}

func callOnce(op vm.OpCode, word []byte, value uint64) []byte {
	code := []byte{
		byte(vm.PUSH1), 0,
		byte(vm.PUSH1), 0,
		byte(vm.PUSH1), 0,
		byte(vm.PUSH1), 0,
	}
	code = append(code, push32Uint(value)...)
	code = append(code, push32(word)...)
	code = append(code, push32Uint(500_000)...)
	return append(code, byte(op))
}

func callTwice(op vm.OpCode, word []byte, value uint64) []byte {
	once := callOnce(op, word, value)
	code := append(append([]byte{}, once...), byte(vm.POP))
	code = append(code, once...)
	return append(code, byte(vm.PUSH1), 0, byte(vm.MSTORE), byte(vm.PUSH1), 32, byte(vm.PUSH1), 0, byte(vm.RETURN))
}

func balanceThenCall(op vm.OpCode, target []byte, callee []byte) []byte {
	code := append(push32(target), byte(vm.BALANCE), byte(vm.POP))
	return append(code, callReturning(op, callee, 0)...)
}

func callReturning(op vm.OpCode, word []byte, value uint64) []byte {
	code := []byte{
		byte(vm.PUSH1), 32,
		byte(vm.PUSH1), 32,
		byte(vm.PUSH1), 0,
		byte(vm.PUSH1), 0,
	}
	code = append(code, push32Uint(value)...)
	code = append(code, push32(word)...)
	code = append(code, push32Uint(500_000)...)
	code = append(code, byte(op), byte(vm.PUSH1), 0, byte(vm.MSTORE), byte(vm.PUSH1), 64, byte(vm.PUSH1), 0, byte(vm.RETURN))
	return code
}

func TestCreate32FromAddress20CreatorPushesZero(t *testing.T) {
	cfg := address32Config()

	for _, op := range []vm.OpCode{vm.CREATE32, vm.CREATE232} {
		t.Run(op.String(), func(t *testing.T) {
			run := runOpcode(t, cfg, address20CreateReturning(op), nil, nil)

			if run.err != nil {
				t.Fatal(run.err)
			}

			if wordResult(t, run.ret).Sign() != 0 {
				t.Fatalf("result = %s, want 0", wordResult(t, run.ret))
			}

			if run.statedb.GetNonce(opcodeContract) != 0 {
				t.Fatalf("creator nonce = %d, want 0", run.statedb.GetNonce(opcodeContract))
			}

			// Three PUSH1s cost 9. The word store and return cost 15.
			// CREATE232 also pushes a salt, which costs 3.
			// The child stipend is not spent. The fixed CREATE gas is.
			want := params.CreateGas + 24

			if op == vm.CREATE232 {
				want = params.Create2Gas + 27
			}

			if run.gas != want {
				t.Fatalf("gas = %d, want %d", run.gas, want)
			}
		})
	}
}

func TestCreate32InitcodeLimitMatchesCreate(t *testing.T) {
	size := byte(0)
	// 49153 does not fit in one byte. Push it as a 3-byte value.
	tooBig := uint64(params.MaxInitCodeSize + 1)
	code := append(push32Uint(tooBig), byte(vm.PUSH1), size, byte(vm.PUSH1), size, byte(vm.CREATE32))
	mirrored := append(push32Uint(tooBig), byte(vm.PUSH1), size, byte(vm.PUSH1), size, byte(vm.CREATE))
	cfg := address32Config()
	address32Run := runOpcode(t, cfg, code, nil, nil)
	address20Run := runOpcode(t, cfg, mirrored, nil, nil)

	if address32Run.err == nil || address20Run.err == nil {
		t.Fatalf("create32 err = %v, create err = %v", address32Run.err, address20Run.err)
	}

	if address32Run.err.Error() != address20Run.err.Error() {
		t.Fatalf("create32 err = %v, create err = %v", address32Run.err, address20Run.err)
	}
}

// address20CreateReturning is a create with empty initcode that returns the result word.
// Three PUSH1s cost 9, the return sequence costs 15, and one word of memory costs 3.
func address20CreateReturning(op vm.OpCode) []byte {
	code := []byte{
		byte(vm.PUSH1), 0,
		byte(vm.PUSH1), 0,
		byte(vm.PUSH1), 0,
	}

	if op == vm.CREATE232 {
		code = append(push32(make([]byte, 32)), code...)
	}

	return append(code,
		byte(op),
		byte(vm.PUSH1), 0,
		byte(vm.MSTORE),
		byte(vm.PUSH1), 32,
		byte(vm.PUSH1), 0,
		byte(vm.RETURN),
	)
}
