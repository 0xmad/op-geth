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
	address32AllocKey = "1111111111111111111111111111111111111111111111111111111111111111"
	address20AllocKey = "2222222222222222222222222222222222222222"
	// keccak256 of the 32-byte address32AllocKey.
	address32TrieKey = "b569321de72d0af89c2fb48a484de3fc9343f31600ae1f3e13d633cb48cbf816"
	// keccak256 of the 20-byte address20AllocKey.
	address20TrieKey = "2ab0a4443bbea3fbe4d0e1503d11ff1367842fb0c8b28a5c8550f27599a40751"
	// keccak256 of address20AllocKey left-padded to 32 bytes. That is not the address20 trie key.
	paddedTrieKey = "761d1673706b12d7996f463b3ee41f89445e41739a7b2688d9e00d8e45cf27f3"
)

func TestGenesisJSONRejectsAddress32AccountWhenUnset(t *testing.T) {
	raw := `{
		"config": {"chainId": 1},
		"gasLimit": "0x1",
		"difficulty": "0x0",
		"alloc": {"` + address32AllocKey + `": {"balance": "0x1"}}
	}`
	var genesis Genesis

	if err := json.Unmarshal([]byte(raw), &genesis); err == nil {
		t.Fatal("address32 genesis account was accepted with Address32 unset")
	} else if !errors.Is(err, errAddress32GenesisRequiresFork) {
		t.Fatalf("Unmarshal() error = %v, want errAddress32GenesisRequiresFork", err)
	}
}

func TestGenesisJSONAcceptsAddress32AccountWhenActive(t *testing.T) {
	raw := `{
		"config": {"chainId": 1, "address32Time": 0},
		"gasLimit": "0x1",
		"difficulty": "0x0",
		"alloc": {
			"` + address32AllocKey + `": {"balance": "0x2a", "nonce": "0x4"},
			"` + address20AllocKey + `": {"balance": "0x7"}
		}
	}`

	var genesis Genesis

	if err := json.Unmarshal([]byte(raw), &genesis); err != nil {
		t.Fatal(err)
	}

	address32, err := common.NewAddress32(common.FromHex(address32AllocKey))

	if err != nil {
		t.Fatal(err)
	}

	if genesis.Address32Alloc[address32].Balance.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("address32 balance = %v, want 42", genesis.Address32Alloc[address32].Balance)
	}

	address20 := common.HexToAddress(address20AllocKey)

	if genesis.Alloc[address20].Balance.Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("address20 balance = %v, want 7", genesis.Alloc[address20].Balance)
	}

	blob, err := json.Marshal(genesis)

	if err != nil {
		t.Fatal(err)
	}

	var again Genesis

	if err := json.Unmarshal(blob, &again); err != nil {
		t.Fatal(err)
	}

	if again.Address32Alloc[address32].Balance.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("round-trip address32 balance = %v, want 42", again.Address32Alloc[address32].Balance)
	}

	if again.Alloc[address20].Balance.Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("round-trip address20 balance = %v, want 7", again.Alloc[address20].Balance)
	}
}

func TestCommitRejectsAddress32AccountWhenUnset(t *testing.T) {
	address32, err := common.NewAddress32(common.FromHex(address32AllocKey))

	if err != nil {
		t.Fatal(err)
	}

	genesis := &Genesis{
		Config:     params.TestChainConfig,
		Difficulty: big.NewInt(0),
		GasLimit:   1,
		Address32Alloc: map[common.Address32]types.Account{
			address32: {Balance: big.NewInt(1)},
		},
	}
	db := rawdb.NewMemoryDatabase()
	_, err = genesis.Commit(db, triedb.NewDatabase(db, triedb.HashDefaults))

	if !errors.Is(err, errAddress32GenesisRequiresFork) {
		t.Fatalf("Commit() error = %v, want errAddress32GenesisRequiresFork", err)
	}
}

func TestAddress32GenesisAccountTrieKey(t *testing.T) {
	address32, err := common.NewAddress32(common.FromHex(address32AllocKey))

	if err != nil {
		t.Fatal(err)
	}

	address20 := common.HexToAddress(address20AllocKey)
	ts := uint64(0)
	cfg := *params.TestChainConfig
	cfg.Address32Time = &ts
	genesis := &Genesis{
		Config:     &cfg,
		Difficulty: big.NewInt(0),
		GasLimit:   1,
		Alloc: types.GenesisAlloc{
			address20: {Balance: big.NewInt(7)},
		},
		Address32Alloc: map[common.Address32]types.Account{
			address32: {Balance: big.NewInt(42), Nonce: 4},
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

	address32Acc, err := tr.GetAccountByHash(common.HexToHash(address32TrieKey))

	if err != nil {
		t.Fatal(err)
	}

	if address32Acc == nil || address32Acc.Balance.Cmp(uint256.NewInt(42)) != 0 || address32Acc.Nonce != 4 {
		t.Fatalf("address32 = %+v, want balance 42 nonce 4", address32Acc)
	}

	address20Acc, err := tr.GetAccountByHash(common.HexToHash(address20TrieKey))

	if err != nil {
		t.Fatal(err)
	}

	if address20Acc == nil || address20Acc.Balance.Cmp(uint256.NewInt(7)) != 0 {
		t.Fatalf("address20 = %+v, want balance 7", address20Acc)
	}

	padded, err := tr.GetAccountByHash(common.HexToHash(paddedTrieKey))

	if err != nil {
		t.Fatal(err)
	}

	if padded != nil {
		t.Fatal("address20 was stored under the zero-padded 32-byte word")
	}
}
