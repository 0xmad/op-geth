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

package ethapi

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
)

const (
	address32Word = "1111111111111111111111111111111111111111111111111111111111111111"
	address20Word = "2222222222222222222222222222222222222222"
	// keccak256 of address32Word.
	address32TrieKey = "b569321de72d0af89c2fb48a484de3fc9343f31600ae1f3e13d633cb48cbf816"
	// keccak256 of address20Word.
	address20TrieKey = "2ab0a4443bbea3fbe4d0e1503d11ff1367842fb0c8b28a5c8550f27599a40751"
)

func TestAddress32GenesisAccountRPC(t *testing.T) {
	address32, err := common.NewAddress32(common.FromHex(address32Word))

	if err != nil {
		t.Fatal(err)
	}

	address20 := common.HexToAddress(address20Word)
	code := []byte{0x60, 0x01}
	slot := common.HexToHash("0x01")
	ts := uint64(0)
	cfg := *params.TestChainConfig
	cfg.Address32Time = &ts
	genesis := &core.Genesis{
		Config: &cfg,
		Alloc: types.GenesisAlloc{
			address20: {Balance: big.NewInt(7)},
		},
		Address32Alloc: map[common.Address32]types.Account{
			address32: {
				Balance: big.NewInt(42),
				Nonce:   4,
				Code:    code,
				Storage: map[common.Hash]common.Hash{slot: common.HexToHash("0x02")},
			},
		},
	}
	backend := newTestBackend(t, 0, genesis, ethash.NewFaker(), nil)
	defer backend.chain.Stop()

	server := rpc.NewServer()
	defer server.Stop()

	if err := server.RegisterName("eth", NewBlockChainAPI(backend)); err != nil {
		t.Fatal(err)
	}

	if err := server.RegisterName("eth", NewTransactionAPI(backend, new(AddrLocker))); err != nil {
		t.Fatal(err)
	}

	client := rpc.DialInProc(server)
	defer client.Close()

	address32Hex := "0x" + address32Word
	var balance hexutil.Big

	if err := client.Call(&balance, "eth_getBalance", address32Hex, "latest"); err != nil {
		t.Fatal(err)
	}

	if balance.ToInt().Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("balance = %s, want 42", balance.ToInt())
	}

	var nonce hexutil.Uint64

	if err := client.Call(&nonce, "eth_getTransactionCount", address32Hex, "latest"); err != nil {
		t.Fatal(err)
	}

	if nonce != 4 {
		t.Fatalf("nonce = %d, want 4", nonce)
	}

	var gotCode hexutil.Bytes

	if err := client.Call(&gotCode, "eth_getCode", address32Hex, "latest"); err != nil {
		t.Fatal(err)
	}

	if string(gotCode) != string(code) {
		t.Fatalf("code = %x, want %x", gotCode, code)
	}

	var stored hexutil.Bytes

	if err := client.Call(&stored, "eth_getStorageAt", address32Hex, "0x01", "latest"); err != nil {
		t.Fatal(err)
	}

	if common.BytesToHash(stored) != common.HexToHash("0x02") {
		t.Fatalf("storage = %x, want 2", stored)
	}

	var proof AccountResult

	if err := client.Call(&proof, "eth_getProof", address32Hex, []string{"0x01"}, "latest"); err != nil {
		t.Fatal(err)
	}

	got, ok := proof.Address.Address32()

	if !ok || got != address32 {
		t.Fatalf("proof address = %s, want %s", got, address32)
	}

	if proof.Balance.ToInt().Cmp(big.NewInt(42)) != 0 || proof.Nonce != 4 {
		t.Fatalf("proof account balance=%s nonce=%d", proof.Balance.ToInt(), proof.Nonce)
	}

	if len(proof.StorageProof) != 1 || proof.StorageProof[0].Value.ToInt().Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("storage proof = %+v", proof.StorageProof)
	}

	account := mustVerifyAccountProof(t, backend.chain.CurrentBlock().Root, common.HexToHash(address32TrieKey), proof.AccountProof)

	if account.Balance.Cmp(uint256.NewInt(42)) != 0 || account.Nonce != 4 {
		t.Fatalf("proved account = %+v", account)
	}

	address20Hex := "0x" + address20Word
	var address20Balance hexutil.Big

	if err := client.Call(&address20Balance, "eth_getBalance", address20Hex, "latest"); err != nil {
		t.Fatal(err)
	}

	if address20Balance.ToInt().Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("address20 balance = %s, want 7", address20Balance.ToInt())
	}

	padded := "0x000000000000000000000000" + address20Word
	var paddedBalance hexutil.Big

	if err := client.Call(&paddedBalance, "eth_getBalance", padded, "latest"); err != nil {
		t.Fatal(err)
	}

	if paddedBalance.ToInt().Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("padded word balance = %s, want the address20 balance 7", paddedBalance.ToInt())
	}

	var address20Proof AccountResult

	if err := client.Call(&address20Proof, "eth_getProof", address20Hex, nil, "latest"); err != nil {
		t.Fatal(err)
	}

	address20Account := mustVerifyAccountProof(t, backend.chain.CurrentBlock().Root, common.HexToHash(address20TrieKey), address20Proof.AccountProof)

	if address20Account.Balance.Cmp(uint256.NewInt(7)) != 0 {
		t.Fatalf("proved address20 balance = %s, want 7", address20Account.Balance)
	}
}

func mustVerifyAccountProof(t *testing.T, root, trieKey common.Hash, nodes []string) *types.StateAccount {
	t.Helper()
	db := rawdb.NewMemoryDatabase()

	for _, node := range nodes {
		blob := common.FromHex(node)
		if err := db.Put(crypto.Keccak256(blob), blob); err != nil {
			t.Fatal(err)
		}
	}

	val, err := trie.VerifyProof(root, trieKey.Bytes(), db)

	if err != nil {
		t.Fatal(err)
	}

	if len(val) == 0 {
		t.Fatal("account proof missed the trie key")
	}

	var account types.StateAccount

	if err := rlp.DecodeBytes(val, &account); err != nil {
		t.Fatal(err)
	}

	return &account
}
