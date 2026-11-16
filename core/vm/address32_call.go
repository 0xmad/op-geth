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
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// CallAccount runs a call between an address20 and an address32.
// A user contract in the other namespace reverts the subcall and returns the gas.
// A precompile on the active list runs, and any value is credited to that address20.
func (evm *EVM) CallAccount(caller, target common.StateAddress, input []byte, gas uint64, value *uint256.Int, readOnly bool) ([]byte, uint64, error) {
	if value == nil {
		value = new(uint256.Int)
	}

	if readOnly && !value.IsZero() {
		return nil, gas, ErrWriteProtection
	}

	if addr, ok := target.Address20(); ok {
		return evm.callAddress20Target(caller, addr, input, gas, value, readOnly)
	}

	addr, _ := target.Address32()
	return evm.callAddress32Target(caller, addr, input, gas, value, readOnly)
}

func (evm *EVM) callAddress20Target(caller common.StateAddress, addr common.Address, input []byte, gas uint64, value *uint256.Int, readOnly bool) ([]byte, uint64, error) {
	if caller20, ok := caller.Address20(); ok {
		if readOnly {
			return evm.StaticCall(caller20, addr, input, gas)
		}

		return evm.Call(caller20, addr, input, gas, value)
	}

	if p, ok := evm.precompile(addr); ok {
		return evm.callPrecompile(caller, addr, p, input, gas, value, readOnly)
	}

	code := evm.StateDB.GetCode(addr)

	if len(code) > 0 {
		return nil, gas, ErrExecutionReverted
	}

	return evm.transferToCodeless(caller, common.Address20StateAddress(addr), gas, value)
}

func (evm *EVM) callAddress32Target(caller common.StateAddress, addr common.Address32, input []byte, gas uint64, value *uint256.Int, readOnly bool) ([]byte, uint64, error) {
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}

	db, err := address32DB(evm)

	if err != nil {
		return nil, gas, err
	}

	if !value.IsZero() && !canPayAccount(db, caller, value) {
		return nil, gas, ErrInsufficientBalance
	}

	code := db.Address32Code(addr)

	if db.Error() != nil {
		return nil, gas, db.Error()
	}

	if len(code) > 0 && caller.Kind() != common.StateAddress32 {
		return nil, gas, ErrExecutionReverted
	}

	if len(code) == 0 {
		return evm.transferToCodeless(caller, common.Address32StateAddress(addr), gas, value)
	}

	return evm.runAddress32(db, caller, addr, code, input, gas, value, readOnly)
}

func (evm *EVM) callPrecompile(caller common.StateAddress, addr common.Address, p PrecompiledContract, input []byte, gas uint64, value *uint256.Int, readOnly bool) ([]byte, uint64, error) {
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}

	db, err := address32DB(evm)

	if err != nil {
		return nil, gas, err
	}

	if !value.IsZero() && !canPayAccount(db, caller, value) {
		return nil, gas, ErrInsufficientBalance
	}

	snapshot := evm.StateDB.Snapshot()

	if readOnly {
		// Match StaticCall: touch the account, and do not create it.
		evm.StateDB.AddBalance(addr, new(uint256.Int), tracing.BalanceChangeTouchAccount)
	} else if !evm.StateDB.Exist(addr) {
		evm.StateDB.CreateAccount(addr)
	}

	if !readOnly {
		transferAccount(db, caller, common.Address20StateAddress(addr), value)
	}

	if db.Error() != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		return nil, gas, db.Error()
	}

	ret, gas, err := RunPrecompiledContract(p, input, gas, evm.Config.Tracer)

	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)

		if err != ErrExecutionReverted {
			gas = 0
		}
	}

	return ret, gas, err
}

func (evm *EVM) runAddress32(db *state.StateDB, caller common.StateAddress, addr common.Address32, code, input []byte, gas uint64, value *uint256.Int, readOnly bool) ([]byte, uint64, error) {
	snapshot := evm.StateDB.Snapshot()
	transferAccount(db, caller, common.Address32StateAddress(addr), value)

	if db.Error() != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		return nil, gas, db.Error()
	}

	contract := newAddress32Frame(caller, addr, value, gas, evm.jumpDests)
	contract.SetCallCode(db.Address32CodeHash(addr), code)
	ret, err := evm.Run(contract, input, readOnly)
	gas = contract.Gas

	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)

		if err != ErrExecutionReverted {
			gas = 0
		}
	}

	return ret, gas, err
}

// delegateCallAddress32 runs addr's code in the caller's account.
// Storage, balance, caller, and value stay with the caller. No value moves.
func (evm *EVM) delegateCallAddress32(caller *Contract, addr common.Address32, input []byte, gas uint64) ([]byte, uint64, error) {
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}

	db, err := address32DB(evm)

	if err != nil {
		return nil, gas, err
	}

	code := db.Address32Code(addr)

	if db.Error() != nil {
		return nil, gas, db.Error()
	}

	if len(code) == 0 {
		return nil, gas, nil
	}

	snapshot := evm.StateDB.Snapshot()
	contract := newDelegateFrame(caller, gas, evm.jumpDests)
	contract.SetCallCode(db.Address32CodeHash(addr), code)
	ret, err := evm.Run(contract, input, false)
	gas = contract.Gas

	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)

		if err != ErrExecutionReverted {
			gas = 0
		}
	}

	return ret, gas, err
}

func newDelegateFrame(parent *Contract, gas uint64, jumpDests JumpDestCache) *Contract {
	self := parent.stateAddress()
	var self20 common.Address

	self.Use(func(addr common.Address) {
		self20 = addr
	}, func(common.Address32) {})

	caller20 := common.Address{}

	if _, ok := parent.Caller32(); !ok {
		caller20 = parent.Caller()
	}

	contract := NewContract(caller20, self20, parent.Value(), gas, jumpDests)

	if addr, ok := parent.Address32(); ok {
		contract.setAddress32(addr)
	}

	if addr, ok := parent.Caller32(); ok {
		contract.setCaller32(addr)
	}

	return contract
}

func (evm *EVM) transferToCodeless(caller, target common.StateAddress, gas uint64, value *uint256.Int) ([]byte, uint64, error) {
	if value.IsZero() {
		return nil, gas, nil
	}

	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}

	db, err := address32DB(evm)

	if err != nil {
		return nil, gas, err
	}

	if !canPayAccount(db, caller, value) {
		return nil, gas, ErrInsufficientBalance
	}

	snapshot := evm.StateDB.Snapshot()
	transferAccount(db, caller, target, value)

	if db.Error() != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		return nil, gas, db.Error()
	}

	return nil, gas, nil
}

func newAddress32Frame(caller common.StateAddress, self common.Address32, value *uint256.Int, gas uint64, jumpDests JumpDestCache) *Contract {
	var caller20 common.Address

	caller.Use(func(addr common.Address) {
		caller20 = addr
	}, func(common.Address32) {})

	contract := NewContract(caller20, common.Address{}, value, gas, jumpDests)
	contract.setAddress32(self)

	if addr, ok := caller.Address32(); ok {
		contract.setCaller32(addr)
	}

	return contract
}

func canPayAccount(db *state.StateDB, account common.StateAddress, value *uint256.Int) bool {
	if value == nil || value.IsZero() {
		return true
	}

	return db.AccountBalance(account).Cmp(value) >= 0
}

func transferAccount(db *state.StateDB, from, to common.StateAddress, value *uint256.Int) {
	if value == nil || value.IsZero() {
		return
	}

	from.Use(func(addr common.Address) {
		db.SubBalance(addr, value, tracing.BalanceChangeTransfer)
	}, func(addr common.Address32) {
		db.SubAddress32Balance(addr, value)
	})
	to.Use(func(addr common.Address) {
		db.AddBalance(addr, value, tracing.BalanceChangeTransfer)
	}, func(addr common.Address32) {
		db.AddAddress32Balance(addr, value)
	})
}
