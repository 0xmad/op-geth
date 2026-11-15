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

package vm

import (
	"errors"
	gomath "math"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// applyAddress32 copies jt and installs the address32 opcodes when the fork is active.
func applyAddress32(rules params.Rules, jt *JumpTable) *JumpTable {
	if jt == nil || !rules.IsAddress32 {
		return jt
	}

	copied := copyJumpTable(jt)
	enableAddress32(copied)

	return copied
}

// enableAddress32 installs the address32 opcodes onto jt.
// Each opcode copies the gas schedule of the mirrored opcode on this fork.
// A mirrored opcode that is not on this fork stays unassigned.
func enableAddress32(jt *JumpTable) {
	enableMirrored(jt, BALANCE32, jt[BALANCE], opBalance32, gasAccountCheck32)
	enableMirrored(jt, EXTCODESIZE32, jt[EXTCODESIZE], opExtCodeSize32, gasAccountCheck32)
	enableMirrored(jt, EXTCODEHASH32, jt[EXTCODEHASH], opExtCodeHash32, gasAccountCheck32)
	enableMirrored(jt, EXTCODECOPY32, jt[EXTCODECOPY], opExtCodeCopy32, gasExtCodeCopy32)
	enableMirrored(jt, CALL32, jt[CALL], opCall32, gasCall32)
	enableMirrored(jt, CREATE32, jt[CREATE], opCreate32, nil)
	enableMirrored(jt, CREATE232, jt[CREATE2], opCreate232, nil)
}

func enableMirrored(jt *JumpTable, op OpCode, mirrored *operation, execute executionFunc, wrap func(gasFunc) gasFunc) {
	if mirrored == nil || mirrored.undefined {
		return
	}

	copied := *mirrored
	copied.execute = execute
	copied.undefined = false

	if mirrored.dynamicGas != nil && wrap != nil {
		copied.dynamicGas = wrap(mirrored.dynamicGas)
	}

	jt[op] = &copied
}

func accountFromSlot(slot *uint256.Int) (common.StateAddress, error) {
	word := slot.Bytes32()
	return common.StateAddressFromWord(word[:])
}

func address32DB(evm *EVM) (*state.StateDB, error) {
	db := state.AsStateDB(evm.StateDB)

	if db == nil {
		return nil, errors.New("address32 account requires a state database")
	}

	return db, nil
}

// withAccount runs address20 or address32 for the stack word.
// Twelve leading zero bytes select the address20.
func withAccount[T any](slot *uint256.Int, evm *EVM, address20 func(common.Address) (T, error), address32 func(*state.StateDB, common.Address32) (T, error)) (T, error) {
	var zero T
	account, err := accountFromSlot(slot)

	if err != nil {
		return zero, err
	}

	if addr, ok := account.Address20(); ok {
		return address20(addr)
	}

	addr, ok := account.Address32()

	if !ok {
		return zero, errors.New("state address is not an address32")
	}

	db, err := address32DB(evm)

	if err != nil {
		return zero, err
	}

	return address32(db, addr)
}

// address32ColdSurcharge warms addr and returns the cold-minus-warm surcharge.
// A warm account, or a fork without EIP-2929, returns 0.
func address32ColdSurcharge(evm *EVM, db *state.StateDB, addr common.Address32) uint64 {
	if !evm.chainRules.IsEIP2929 || db.Address32InAccessList(addr) {
		return 0
	}

	db.AddAddress32ToAccessList(addr)

	return params.ColdAccountAccessCostEIP2929 - params.WarmStorageReadCostEIP2929
}

// address32DelegationCost charges EIP-7702 resolution of an address32 designator.
// The delegated address20 is warmed the same way CALL warms it.
func address32DelegationCost(evm *EVM, db *state.StateDB, addr common.Address32) (uint64, error) {
	if !evm.chainRules.IsPrague || evm.chainRules.IsEIP4762 {
		return 0, nil
	}

	code := db.Address32Code(addr)

	if err := db.Error(); err != nil {
		return 0, err
	}

	target, ok := types.ParseDelegation(code)

	if !ok {
		return 0, nil
	}

	if evm.StateDB.AddressInAccessList(target) {
		return params.WarmStorageReadCostEIP2929, nil
	}

	evm.StateDB.AddAddressToAccessList(target)

	return params.ColdAccountAccessCostEIP2929, nil
}

func useCallSurcharge(evm *EVM, contract *Contract, cost uint64) error {
	if cost == 0 {
		return nil
	}

	if !contract.UseGas(cost, evm.Config.Tracer, tracing.GasChangeCallStorageColdAccess) {
		return ErrOutOfGas
	}

	return nil
}

// gasAccountCheck32 charges the BALANCE / EXTCODESIZE / EXTCODEHASH cold surcharge.
// An address20 word uses the mirrored gas function.
func gasAccountCheck32(fallback gasFunc) gasFunc {
	return func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
		return withAccount(stack.peek(), evm,
			func(common.Address) (uint64, error) {
				return fallback(evm, contract, stack, mem, memorySize)
			},
			func(db *state.StateDB, addr common.Address32) (uint64, error) {
				return address32ColdSurcharge(evm, db, addr), nil
			},
		)
	}
}

// gasExtCodeCopy32 charges code-copy gas plus the cold-account surcharge.
func gasExtCodeCopy32(fallback gasFunc) gasFunc {
	return func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
		return withAccount(stack.peek(), evm,
			func(common.Address) (uint64, error) {
				return fallback(evm, contract, stack, mem, memorySize)
			},
			func(db *state.StateDB, addr common.Address32) (uint64, error) {
				gas, err := gasExtCodeCopy(evm, contract, stack, mem, memorySize)

				if err != nil {
					return 0, err
				}

				var overflow bool

				if gas, overflow = math.SafeAdd(gas, address32ColdSurcharge(evm, db, addr)); overflow {
					return 0, ErrGasUintOverflow
				}

				return gas, nil
			},
		)
	}
}

// gasCall32 charges CALL gas. An address20 word uses the mirrored calculator.
func gasCall32(fallback gasFunc) gasFunc {
	return func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
		return withAccount(stack.Back(1), evm,
			func(common.Address) (uint64, error) {
				return fallback(evm, contract, stack, mem, memorySize)
			},
			func(db *state.StateDB, addr common.Address32) (uint64, error) {
				return gasCallAddress32(evm, contract, stack, mem, memorySize, db, addr)
			},
		)
	}
}

func gasCallAddress32(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64, db *state.StateDB, addr common.Address32) (uint64, error) {
	cold := address32ColdSurcharge(evm, db, addr)

	if err := useCallSurcharge(evm, contract, cold); err != nil {
		return 0, err
	}

	delegation, err := address32DelegationCost(evm, db, addr)

	if err != nil {
		return 0, err
	}

	if err := useCallSurcharge(evm, contract, delegation); err != nil {
		return 0, err
	}

	gas, err := gasCallMeter(evm, contract, stack, mem, memorySize, db.Address32Empty(addr), db.Address32Exist(addr))

	return addChargedCallGas(contract, gas, err, cold, delegation)
}

func addChargedCallGas(contract *Contract, gas uint64, err error, charged, delegation uint64) (uint64, error) {
	total := charged + delegation

	if total == 0 || err != nil {
		return gas, err
	}

	contract.Gas += total
	sum, overflow := math.SafeAdd(gas, total)

	if overflow {
		return 0, ErrGasUintOverflow
	}

	return sum, nil
}

func opBalance32(pc *uint64, evm *EVM, scope *ScopeContext) ([]byte, error) {
	return withAccount(scope.Stack.peek(), evm,
		func(common.Address) ([]byte, error) {
			return opBalance(pc, evm, scope)
		},
		func(db *state.StateDB, addr common.Address32) ([]byte, error) {
			scope.Stack.peek().Set(db.Address32Balance(addr))
			return nil, nil
		},
	)
}

func opExtCodeSize32(pc *uint64, evm *EVM, scope *ScopeContext) ([]byte, error) {
	return withAccount(scope.Stack.peek(), evm,
		func(common.Address) ([]byte, error) {
			return opExtCodeSize(pc, evm, scope)
		},
		func(db *state.StateDB, addr common.Address32) ([]byte, error) {
			scope.Stack.peek().SetUint64(uint64(len(db.Address32Code(addr))))
			return nil, nil
		},
	)
}

func opExtCodeCopy32(pc *uint64, evm *EVM, scope *ScopeContext) ([]byte, error) {
	return withAccount(scope.Stack.peek(), evm,
		func(common.Address) ([]byte, error) {
			return opExtCodeCopy(pc, evm, scope)
		},
		func(db *state.StateDB, addr common.Address32) ([]byte, error) {
			stack := scope.Stack
			stack.pop()
			memOffset := stack.pop()
			codeOffset := stack.pop()
			length := stack.pop()
			uint64CodeOffset, overflow := codeOffset.Uint64WithOverflow()

			if overflow {
				uint64CodeOffset = gomath.MaxUint64
			}

			codeCopy := getData(db.Address32Code(addr), uint64CodeOffset, length.Uint64())
			scope.Memory.Set(memOffset.Uint64(), length.Uint64(), codeCopy)

			return nil, nil
		},
	)
}

func opExtCodeHash32(pc *uint64, evm *EVM, scope *ScopeContext) ([]byte, error) {
	slot := scope.Stack.peek()

	return withAccount(slot, evm,
		func(common.Address) ([]byte, error) {
			return opExtCodeHash(pc, evm, scope)
		},
		func(db *state.StateDB, addr common.Address32) ([]byte, error) {
			if db.Address32Empty(addr) {
				slot.Clear()
			} else {
				slot.SetBytes(db.Address32CodeHash(addr).Bytes())
			}

			return nil, nil
		},
	)
}

type callOutcome struct {
	ret []byte
	gas uint64
}

func opCall32(pc *uint64, evm *EVM, scope *ScopeContext) ([]byte, error) {
	stack := scope.Stack
	temp := stack.pop()
	gas := evm.callGasTemp
	addrWord, value, inOffset, inSize, retOffset, retSize := stack.pop(), stack.pop(), stack.pop(), stack.pop(), stack.pop(), stack.pop()
	args := scope.Memory.GetPtr(inOffset.Uint64(), inSize.Uint64())

	if evm.readOnly && !value.IsZero() {
		return nil, ErrWriteProtection
	}

	if !value.IsZero() {
		gas += params.CallStipend
	}

	outcome, err := withAccount(&addrWord, evm,
		func(addr common.Address) (callOutcome, error) {
			ret, returnGas, err := evm.Call(scope.Contract.Address(), addr, args, gas, &value)
			return callOutcome{ret: ret, gas: returnGas}, err
		},
		func(db *state.StateDB, addr common.Address32) (callOutcome, error) {
			ret, returnGas, err := evm.callAddress32(scope.Contract.Address(), addr, gas, &value)

			if db.Error() != nil {
				return callOutcome{}, db.Error()
			}

			return callOutcome{ret: ret, gas: returnGas}, err
		},
	)

	if err != nil {
		temp.Clear()
	} else {
		temp.SetOne()
	}

	stack.push(&temp)

	if err == nil || err == ErrExecutionReverted {
		scope.Memory.Set(retOffset.Uint64(), retSize.Uint64(), outcome.ret)
	}

	scope.Contract.RefundGas(outcome.gas, evm.Config.Tracer, tracing.GasChangeCallLeftOverRefunded)
	evm.returnData = outcome.ret

	return outcome.ret, nil
}

// callAddress32 transfers value to a codeless address32.
// An address32 with code reverts the subcall and does not run that code.
func (evm *EVM) callAddress32(caller common.Address, addr common.Address32, gas uint64, value *uint256.Int) ([]byte, uint64, error) {
	caller = evm.maybeOverrideCaller(caller)

	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}

	if !value.IsZero() && !evm.Context.CanTransfer(evm.StateDB, caller, value) {
		return nil, gas, ErrInsufficientBalance
	}

	db, err := address32DB(evm)

	if err != nil {
		return nil, gas, err
	}

	code := db.Address32Code(addr)

	if db.Error() != nil {
		return nil, gas, db.Error()
	}

	if len(code) > 0 {
		return nil, gas, ErrExecutionReverted
	}

	if value.IsZero() {
		return nil, gas, nil
	}

	snapshot := evm.StateDB.Snapshot()
	evm.StateDB.SubBalance(caller, value, tracing.BalanceChangeTransfer)
	db.AddAddress32Balance(addr, value)

	if db.Error() != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		return nil, gas, db.Error()
	}

	return nil, gas, nil
}

// opCreate32 deploys an address32 contract from an address32 frame.
// An address20 creator pushes 0 and continues. The child stipend is not taken.
// The fixed CREATE gas was already charged.
func opCreate32(pc *uint64, evm *EVM, scope *ScopeContext) ([]byte, error) {
	if evm.readOnly {
		return nil, ErrWriteProtection
	}

	var (
		value        = scope.Stack.pop()
		offset, size = scope.Stack.pop(), scope.Stack.pop()
		input        = scope.Memory.GetCopy(offset.Uint64(), size.Uint64())
		gas          = scope.Contract.Gas
		stackvalue   = size
	)

	gas -= gas / 64

	if _, err := address32DB(evm); err != nil {
		return nil, err
	}

	caller, ok := scope.Contract.Address32()

	if !ok {
		return pushCreateFailure(evm, scope, stackvalue)
	}

	scope.Contract.UseGas(gas, evm.Config.Tracer, tracing.GasChangeCallContractCreation)
	res, addr, returnGas, suberr := evm.CreateAddress32(caller, input, gas, &value, CREATE32)
	return finishCreate32(evm, scope, stackvalue, res, addr, returnGas, suberr)
}

// opCreate232 deploys an address32 contract at a salted address.
// An address20 creator pushes 0 and continues, and the child stipend is returned.
func opCreate232(pc *uint64, evm *EVM, scope *ScopeContext) ([]byte, error) {
	if evm.readOnly {
		return nil, ErrWriteProtection
	}

	var (
		endowment    = scope.Stack.pop()
		offset, size = scope.Stack.pop(), scope.Stack.pop()
		salt         = scope.Stack.pop()
		input        = scope.Memory.GetCopy(offset.Uint64(), size.Uint64())
		gas          = scope.Contract.Gas
		stackvalue   = size
	)

	gas -= gas / 64

	if _, err := address32DB(evm); err != nil {
		return nil, err
	}

	caller, ok := scope.Contract.Address32()

	if !ok {
		return pushCreateFailure(evm, scope, stackvalue)
	}

	scope.Contract.UseGas(gas, evm.Config.Tracer, tracing.GasChangeCallContractCreation2)
	res, addr, returnGas, suberr := evm.CreateAddress232(caller, input, gas, &endowment, &salt, CREATE232)
	return finishCreate32(evm, scope, stackvalue, res, addr, returnGas, suberr)
}

func pushCreateFailure(evm *EVM, scope *ScopeContext, stackvalue uint256.Int) ([]byte, error) {
	stackvalue.Clear()
	scope.Stack.push(&stackvalue)
	evm.returnData = nil
	return nil, nil
}

func finishCreate32(evm *EVM, scope *ScopeContext, stackvalue uint256.Int, res []byte, addr common.Address32, returnGas uint64, suberr error) ([]byte, error) {
	if suberr != nil {
		stackvalue.Clear()
	} else {
		stackvalue.SetBytes(addr[:])
	}

	scope.Stack.push(&stackvalue)
	scope.Contract.RefundGas(returnGas, evm.Config.Tracer, tracing.GasChangeCallLeftOverRefunded)

	if suberr == ErrExecutionReverted {
		evm.returnData = res
		return res, nil
	}

	evm.returnData = nil
	return nil, nil
}
