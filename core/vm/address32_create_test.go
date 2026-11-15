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
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

func TestCreateAddress32LeavesAddress20SubspaceUntouched(t *testing.T) {
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())

	if err != nil {
		t.Fatal(err)
	}

	var caller common.Address32
	caller[0] = 0x11
	statedb.SetAddress32Nonce(caller, 4)

	var address common.Address32
	address[31] = 0x01
	evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), Time: 1}, statedb, params.AllEthashProtocolChanges, Config{})
	ret, got, left, err := evm.createAddress32(statedb, caller, address, []byte{0x60, 0x00, 0x60, 0x00, 0xfd}, 1000, uint256.NewInt(0), CREATE32)

	if !errors.Is(err, common.ErrAddress32InAddress20Subspace) {
		t.Fatalf("err = %v, want ErrAddress32InAddress20Subspace", err)
	}

	if statedb.Address32Nonce(caller) != 4 {
		t.Fatalf("caller nonce = %d, want 4", statedb.Address32Nonce(caller))
	}

	if statedb.Address32Nonce(address) != 0 {
		t.Fatalf("address nonce = %d, want 0", statedb.Address32Nonce(address))
	}

	if statedb.Address32Code(address) != nil {
		t.Fatal("initcode stored code")
	}

	if left != 1000 || ret != nil || got != (common.Address32{}) {
		t.Fatalf("ret = %x, address = %s, gas = %d", ret, got, left)
	}
}
