// Copyright 2023 The go-ethereum Authors
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

	"github.com/ethereum/go-ethereum/params"
)

// LookupInstructionSet returns the instruction set for the fork configured by
// the rules.
func LookupInstructionSet(rules params.Rules) (JumpTable, error) {
	var (
		jt  JumpTable
		err error
	)
	switch {
	case rules.IsVerkle:
		jt, err = newCancunInstructionSet(), errors.New("verkle-fork not defined yet")
	case rules.IsOsaka:
		jt = newOsakaInstructionSet()
	case rules.IsPrague:
		jt = newPragueInstructionSet()
	case rules.IsCancun:
		jt = newCancunInstructionSet()
	case rules.IsShanghai:
		jt = newShanghaiInstructionSet()
	case rules.IsMerge:
		jt = newMergeInstructionSet()
	case rules.IsLondon:
		jt = newLondonInstructionSet()
	case rules.IsBerlin:
		jt = newBerlinInstructionSet()
	case rules.IsIstanbul:
		jt = newIstanbulInstructionSet()
	case rules.IsConstantinople:
		jt = newConstantinopleInstructionSet()
	case rules.IsByzantium:
		jt = newByzantiumInstructionSet()
	case rules.IsEIP158:
		jt = newSpuriousDragonInstructionSet()
	case rules.IsEIP150:
		jt = newTangerineWhistleInstructionSet()
	case rules.IsHomestead:
		jt = newHomesteadInstructionSet()
	default:
		jt = newFrontierInstructionSet()
	}

	jt = *applyAddress32(rules, &jt)

	return jt, err
}

// Stack returns the minimum and maximum stack requirements.
func (op *operation) Stack() (int, int) {
	return op.minStack, op.maxStack
}

// HasCost returns true if the opcode has a cost. Opcodes which do _not_ have
// a cost assigned are one of two things:
// - undefined, a.k.a invalid opcodes,
// - the STOP opcode.
// This method can thus be used to check if an opcode is "Invalid (or STOP)".
func (op *operation) HasCost() bool {
	// Ideally, we'd check this:
	//	return op.execute == opUndefined
	// However, go-lang does now allow that. So we'll just check some other
	// 'indicators' that this is an invalid op. Alas, STOP is impossible to
	// filter out
	return op.dynamicGas != nil || op.constantGas != 0
}
