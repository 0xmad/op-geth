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
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

const (
	wideAllocKey   = "1111111111111111111111111111111111111111111111111111111111111111"
	legacyAllocKey = "2222222222222222222222222222222222222222"
	// keccak256 of the 32-byte wideAllocKey.
	wideTrieKey = "b569321de72d0af89c2fb48a484de3fc9343f31600ae1f3e13d633cb48cbf816"
	// keccak256 of the 20-byte legacyAllocKey.
	legacyTrieKey = "2ab0a4443bbea3fbe4d0e1503d11ff1367842fb0c8b28a5c8550f27599a40751"
	// keccak256 of legacyAllocKey left-padded to 32 bytes. That is not the legacy trie key.
	paddedTrieKey = "761d1673706b12d7996f463b3ee41f89445e41739a7b2688d9e00d8e45cf27f3"
)

func TestGenesisJSONRejectsWideAccountWhenAddress32Unset(t *testing.T) {
	raw := `{
		"config": {"chainId": 1},
		"gasLimit": "0x1",
		"difficulty": "0x0",
		"alloc": {"` + wideAllocKey + `": {"balance": "0x1"}}
	}`
	var genesis Genesis

	if err := json.Unmarshal([]byte(raw), &genesis); err == nil {
		t.Fatal("wide genesis account was accepted with Address32 unset")
	} else if !errors.Is(err, errWideGenesisRequiresAddress32) {
		t.Fatalf("Unmarshal() error = %v, want errWideGenesisRequiresAddress32", err)
	}
}

func TestGenesisJSONAcceptsWideAccountWhenAddress32Active(t *testing.T) {
	raw := `{
		"config": {"chainId": 1, "address32Time": 0},
		"gasLimit": "0x1",
		"difficulty": "0x0",
		"alloc": {
			"` + wideAllocKey + `": {"balance": "0x2a", "nonce": "0x4"},
			"` + legacyAllocKey + `": {"balance": "0x7"}
		}
	}`

	var genesis Genesis

	if err := json.Unmarshal([]byte(raw), &genesis); err != nil {
		t.Fatal(err)
	}

	wide, err := common.NewAddress32(common.FromHex(wideAllocKey))

	if err != nil {
		t.Fatal(err)
	}

	if genesis.WideAlloc[wide].Balance.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("wide balance = %v, want 42", genesis.WideAlloc[wide].Balance)
	}

	legacy := common.HexToAddress(legacyAllocKey)

	if genesis.Alloc[legacy].Balance.Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("legacy balance = %v, want 7", genesis.Alloc[legacy].Balance)
	}

	blob, err := json.Marshal(genesis)

	if err != nil {
		t.Fatal(err)
	}

	var again Genesis

	if err := json.Unmarshal(blob, &again); err != nil {
		t.Fatal(err)
	}

	if again.WideAlloc[wide].Balance.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("round-trip wide balance = %v, want 42", again.WideAlloc[wide].Balance)
	}

	if again.Alloc[legacy].Balance.Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("round-trip legacy balance = %v, want 7", again.Alloc[legacy].Balance)
	}
}

func TestCommitRejectsWideAccountWhenAddress32Unset(t *testing.T) {
	wide, err := common.NewAddress32(common.FromHex(wideAllocKey))

	if err != nil {
		t.Fatal(err)
	}

	genesis := &Genesis{
		Config:     params.TestChainConfig,
		Difficulty: big.NewInt(0),
		GasLimit:   1,
		WideAlloc: map[common.Address32]types.Account{
			wide: {Balance: big.NewInt(1)},
		},
	}
	db := rawdb.NewMemoryDatabase()
	_, err = genesis.Commit(db, triedb.NewDatabase(db, triedb.HashDefaults))

	if !errors.Is(err, errWideGenesisRequiresAddress32) {
		t.Fatalf("Commit() error = %v, want errWideGenesisRequiresAddress32", err)
	}
}

func TestWideGenesisAccountTrieKey(t *testing.T) {
	wide, err := common.NewAddress32(common.FromHex(wideAllocKey))

	if err != nil {
		t.Fatal(err)
	}

	legacy := common.HexToAddress(legacyAllocKey)
	ts := uint64(0)
	cfg := *params.TestChainConfig
	cfg.Address32Time = &ts
	genesis := &Genesis{
		Config:     &cfg,
		Difficulty: big.NewInt(0),
		GasLimit:   1,
		Alloc: types.GenesisAlloc{
			legacy: {Balance: big.NewInt(7)},
		},
		WideAlloc: map[common.Address32]types.Account{
			wide: {Balance: big.NewInt(42), Nonce: 4},
		},
	}
	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, triedb.HashDefaults)
	block, err := genesis.Commit(db, tdb)

	if err != nil {
		t.Fatal(err)
	}

	tr, err := trie.NewStateTrie(trie.StateTrieID(block.Root()), tdb)

	if err != nil {
		t.Fatal(err)
	}

	wideAcc, err := tr.GetAccountByHash(common.HexToHash(wideTrieKey))

	if err != nil {
		t.Fatal(err)
	}

	if wideAcc == nil || wideAcc.Balance.Cmp(uint256.NewInt(42)) != 0 || wideAcc.Nonce != 4 {
		t.Fatalf("wide account = %+v, want balance 42 nonce 4", wideAcc)
	}

	legacyAcc, err := tr.GetAccountByHash(common.HexToHash(legacyTrieKey))

	if err != nil {
		t.Fatal(err)
	}

	if legacyAcc == nil || legacyAcc.Balance.Cmp(uint256.NewInt(7)) != 0 {
		t.Fatalf("legacy account = %+v, want balance 7", legacyAcc)
	}

	padded, err := tr.GetAccountByHash(common.HexToHash(paddedTrieKey))

	if err != nil {
		t.Fatal(err)
	}

	if padded != nil {
		t.Fatal("legacy account was stored under the zero-padded 32-byte word")
	}
}
