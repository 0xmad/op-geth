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

package txpool

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

func TestValidateAddress32SenderState(t *testing.T) {
	key, err := crypto.HexToECDSA("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")

	if err != nil {
		t.Fatal(err)
	}

	cfg := *params.OptimismTestConfig
	ts := uint64(0)
	cfg.Address32Time = &ts
	signer := types.LatestSigner(&cfg)
	address32, err := crypto.PubkeyToAddress32(key.PublicKey)

	if err != nil {
		t.Fatal(err)
	}

	var recipient common.Address32
	recipient[0] = 1
	word, err := types.Address32WordFromBytes(recipient[:])

	if err != nil {
		t.Fatal(err)
	}

	signed, err := types.SignTx(types.NewTx(&types.Address32Tx{
		ChainID:    cfg.ChainID,
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(2),
		Gas:        params.TxGas,
		To:         word,
		Value:      big.NewInt(1),
		SenderKind: types.SenderKindAddress32,
	}), signer, key)

	if err != nil {
		t.Fatal(err)
	}

	funded := newAddress32State(t)
	funded.AddAddress32Balance(address32, uint256.MustFromBig(big.NewInt(params.Ether)))
	opts := &ValidationOptionsWithState{
		State:               funded,
		ExistingExpenditure: func(common.Address) *big.Int { return new(big.Int) },
		ExistingCost:        func(common.Address, uint64) *big.Int { return nil },
	}

	if err := ValidateTransactionWithState(signed, signer, opts); err != nil {
		t.Fatal(err)
	}

	opts.State = newAddress32State(t)

	if err := ValidateTransactionWithState(signed, signer, opts); !errors.Is(err, core.ErrInsufficientFunds) {
		t.Fatalf("empty address32 sender error = %v", err)
	}
}

func newAddress32State(t *testing.T) *state.StateDB {
	t.Helper()
	db := rawdb.NewMemoryDatabase()
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabase(triedb.NewDatabase(db, triedb.HashDefaults), nil))

	if err != nil {
		t.Fatal(err)
	}

	return statedb
}
