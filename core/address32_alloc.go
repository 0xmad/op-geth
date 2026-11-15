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
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
)

// insertAddress32Alloc writes Address32 genesis accounts into the Merkle Patricia
// trie at parent. The trie key is keccak256 of the 32-byte word. Address20
// accounts are left where they are. An empty address32 set returns parent.
func insertAddress32Alloc(parent common.Hash, tdb *triedb.Database, address32 map[common.Address32]types.Account) (common.Hash, error) {
	if len(address32) == 0 {
		return parent, nil
	}

	if tdb == nil {
		return common.Hash{}, errors.New("address32 genesis account requires a trie database")
	}

	accTrie, err := trie.NewStateTrie(trie.StateTrieID(parent), tdb)

	if err != nil {
		return common.Hash{}, err
	}

	merged := trienode.NewMergedNodeSet()
	codeBatch := tdb.Disk().NewBatch()
	wroteCode := false

	for addr, account := range address32 {
		addrHash := crypto.Keccak256Hash(addr[:])
		storageRoot := types.EmptyRootHash

		if len(account.Storage) > 0 {
			root, nodes, err := writeAddress32Storage(tdb, parent, addrHash, account.Storage)

			if err != nil {
				return common.Hash{}, err
			}

			storageRoot = root

			if nodes != nil {
				if err := merged.Merge(nodes); err != nil {
					return common.Hash{}, err
				}
			}
		}

		codeHash := types.EmptyCodeHash

		if len(account.Code) > 0 {
			codeHash = crypto.Keccak256Hash(account.Code)
			rawdb.WriteCode(codeBatch, codeHash, account.Code)
			wroteCode = true
		}

		balance := uint256.NewInt(0)

		if account.Balance != nil {
			balance = uint256.MustFromBig(account.Balance)
		}

		acc := &types.StateAccount{
			Nonce:    account.Nonce,
			Balance:  balance,
			Root:     storageRoot,
			CodeHash: codeHash.Bytes(),
		}

		if err := accTrie.UpdateAccountPreimage(addr[:], acc); err != nil {
			return common.Hash{}, err
		}
	}

	if wroteCode {
		if err := codeBatch.Write(); err != nil {
			return common.Hash{}, err
		}
	}

	newRoot, nodes := accTrie.Commit(true)

	if nodes == nil {
		return common.Hash{}, errors.New("address32 genesis account trie commit produced no nodes")
	}

	if err := merged.Merge(nodes); err != nil {
		return common.Hash{}, err
	}

	if err := tdb.Update(newRoot, parent, 0, merged, triedb.NewStateSet()); err != nil {
		return common.Hash{}, err
	}

	return newRoot, nil
}

func writeAddress32Storage(tdb *triedb.Database, parent, addrHash common.Hash, storage map[common.Hash]common.Hash) (common.Hash, *trienode.NodeSet, error) {
	st, err := trie.NewStateTrie(trie.StorageTrieID(parent, addrHash, types.EmptyRootHash), tdb)

	if err != nil {
		return common.Hash{}, nil, err
	}

	wrote := false
	for key, value := range storage {
		if value == (common.Hash{}) {
			continue
		}

		if err := st.UpdateStorage(common.Address{}, key[:], common.TrimLeftZeroes(value[:])); err != nil {
			return common.Hash{}, nil, err
		}

		wrote = true
	}

	if !wrote {
		return types.EmptyRootHash, nil, nil
	}

	root, nodes := st.Commit(false)
	return root, nodes, nil
}
