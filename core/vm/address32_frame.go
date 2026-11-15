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
	"github.com/holiman/uint256"
)

// address32FrameTable is the jump table for an address32 frame.
// ADDRESS, CALLER, and ORIGIN push the full word. COINBASE stays the
// address20 coinbase, which is 12 zero bytes plus the 20-byte account.
// Address20 account opcodes halt the frame.
func address32FrameTable(base *JumpTable) *JumpTable {
	jt := copyJumpTable(base)
	jt[ADDRESS].execute = opAddress32Frame
	jt[CALLER].execute = opCaller32Frame
	jt[ORIGIN].execute = opOrigin32Frame

	halt := &operation{
		execute:  opAddress20Opcode,
		minStack: minStack(0, 0),
		maxStack: maxStack(0, 0),
	}

	for _, op := range []OpCode{
		BALANCE,
		EXTCODESIZE,
		EXTCODECOPY,
		EXTCODEHASH,
		CALL,
		CALLCODE,
		DELEGATECALL,
		STATICCALL,
		CREATE,
		CREATE2,
		SELFDESTRUCT,
	} {
		jt[op] = halt
	}

	return jt
}

func opAddress32Frame(pc *uint64, evm *EVM, scope *ScopeContext) ([]byte, error) {
	scope.Stack.push(wordFromAccount(scope.Contract.stateAddress()))
	return nil, nil
}

func opCaller32Frame(pc *uint64, evm *EVM, scope *ScopeContext) ([]byte, error) {
	if caller, ok := scope.Contract.Caller32(); ok {
		scope.Stack.push(new(uint256.Int).SetBytes(caller[:]))
		return nil, nil
	}

	scope.Stack.push(new(uint256.Int).SetBytes(scope.Contract.Caller().Bytes()))
	return nil, nil
}

func opOrigin32Frame(pc *uint64, evm *EVM, scope *ScopeContext) ([]byte, error) {
	if evm.Origin32 != nil {
		scope.Stack.push(new(uint256.Int).SetBytes(evm.Origin32[:]))
		return nil, nil
	}

	scope.Stack.push(new(uint256.Int).SetBytes(evm.Origin.Bytes()))
	return nil, nil
}

// opAddress20Opcode halts an address32 frame. The caller consumes the gas
// handed to the frame because the error is not a revert.
func opAddress20Opcode(pc *uint64, evm *EVM, scope *ScopeContext) ([]byte, error) {
	return nil, &ErrInvalidOpCode{opcode: scope.Contract.GetOp(*pc)}
}

func wordFromAccount(account common.StateAddress) *uint256.Int {
	word, err := account.Word32()

	if err != nil {
		return new(uint256.Int)
	}

	return new(uint256.Int).SetBytes(word)
}
