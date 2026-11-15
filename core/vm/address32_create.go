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
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// CreateAddress32 deploys code at keccak256(0x02 ‖ rlp([caller, nonce])).
// nonce is the caller nonce before this call increments it.
func (evm *EVM) CreateAddress32(caller common.Address32, code []byte, gas uint64, value *uint256.Int, typ OpCode) (ret []byte, contractAddr common.Address32, leftOverGas uint64, err error) {
	db, err := address32DB(evm)

	if err != nil {
		return nil, common.Address32{}, gas, err
	}

	nonce := db.Address32Nonce(caller)
	return evm.createAddress32(db, caller, crypto.CreateAddress32(caller, nonce), code, gas, value, typ)
}

// CreateAddress232 deploys code at keccak256(0x03 ‖ caller ‖ salt ‖ keccak256(initcode)).
func (evm *EVM) CreateAddress232(caller common.Address32, code []byte, gas uint64, value *uint256.Int, salt *uint256.Int, typ OpCode) (ret []byte, contractAddr common.Address32, leftOverGas uint64, err error) {
	db, err := address32DB(evm)

	if err != nil {
		return nil, common.Address32{}, gas, err
	}

	inithash := crypto.HashData(evm.hasher, code)
	address := crypto.CreateAddress232(caller, salt.Bytes32(), inithash[:])
	return evm.createAddress32(db, caller, address, code, gas, value, typ)
}

func (evm *EVM) createAddress32(db *state.StateDB, caller, address common.Address32, code []byte, gas uint64, value *uint256.Int, typ OpCode) (ret []byte, contractAddr common.Address32, leftOverGas uint64, err error) {
	if evm.Config.Tracer != nil {
		evm.captureBegin(evm.depth, typ, common.Address{}, common.Address{}, code, gas, value.ToBig())
		defer func(startGas uint64) {
			evm.captureEnd(evm.depth, startGas, leftOverGas, ret, err)
		}(gas)
	}

	// The top 12 bytes name an address20. Initcode does not run, and this
	// caller's nonce stays unchanged. The caller refunds the returned gas.
	if address.InAddress20Subspace() {
		return nil, common.Address32{}, gas, common.ErrAddress32InAddress20Subspace
	}

	if evm.depth > int(params.CallCreateDepth) {
		return nil, common.Address32{}, gas, ErrDepth
	}

	if !value.IsZero() && db.Address32Balance(caller).Cmp(value) < 0 {
		return nil, common.Address32{}, gas, ErrInsufficientBalance
	}

	nonce := db.Address32Nonce(caller)

	if nonce+1 < nonce {
		return nil, common.Address32{}, gas, ErrNonceUintOverflow
	}

	db.SetAddress32Nonce(caller, nonce+1)

	if evm.chainRules.IsEIP2929 {
		db.AddAddress32ToAccessList(address)
	}

	if address32Collision(db, address) {
		if evm.Config.Tracer != nil && evm.Config.Tracer.OnGasChange != nil {
			evm.Config.Tracer.OnGasChange(gas, 0, tracing.GasChangeCallFailedExecution)
		}

		return nil, common.Address32{}, 0, ErrContractAddressCollision
	}

	snapshot := evm.StateDB.Snapshot()
	db.SetAddress32Nonce(address, 1)

	if !value.IsZero() {
		db.SubAddress32Balance(caller, value)
		db.AddAddress32Balance(address, value)
	}

	contract := NewContract(common.Address{}, common.Address{}, value, gas, evm.jumpDests)
	contract.setAddress32(address)
	contract.setCaller32(caller)
	contract.SetCallCode(common.Hash{}, code)
	contract.IsDeployment = true

	ret, err = evm.initAddress32Contract(db, contract, address)

	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)

		if err != ErrExecutionReverted {
			contract.UseGas(contract.Gas, evm.Config.Tracer, tracing.GasChangeCallFailedExecution)
		}
	}

	return ret, address, contract.Gas, err
}

func address32Collision(db *state.StateDB, address common.Address32) bool {
	hash := db.Address32CodeHash(address)
	root := db.Address32StorageRoot(address)

	return db.Address32Nonce(address) != 0 ||
		(hash != (common.Hash{}) && hash != types.EmptyCodeHash) ||
		(root != (common.Hash{}) && root != types.EmptyRootHash)
}

func (evm *EVM) initAddress32Contract(db *state.StateDB, contract *Contract, address common.Address32) ([]byte, error) {
	ret, err := evm.Run(contract, nil, false)

	if err != nil {
		return ret, err
	}

	if len(ret) > params.MaxCodeSize && !evm.Config.NoMaxCodeSize {
		return ret, ErrMaxCodeSizeExceeded
	}

	if len(ret) >= 1 && ret[0] == 0xEF {
		return ret, ErrInvalidCode
	}

	createDataGas := uint64(len(ret)) * params.CreateDataGas

	if !contract.UseGas(createDataGas, evm.Config.Tracer, tracing.GasChangeCallCodeStorage) {
		return ret, ErrCodeStoreOutOfGas
	}

	if len(ret) > 0 {
		db.SetAddress32Code(address, ret)
	}

	if db.Error() != nil {
		return ret, db.Error()
	}

	return ret, nil
}
