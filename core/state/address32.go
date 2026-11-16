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

package state

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
)

// AddAddress20AccessPreimage warms the address20 named by preimage.
// A 20-byte entry and the same account left-padded to 32 bytes warm one account.
// An Address32 is rejected here; it is not cropped into an address20.
func (s *StateDB) AddAddress20AccessPreimage(preimage []byte) error {
	addr, ok := common.Address20Of(preimage)

	if !ok {
		return fmt.Errorf("access-list entry %x is not an address20", preimage)
	}

	s.AddAddressToAccessList(addr)
	return nil
}

// Address32 loads the address32 stored under keccak256 of its 32-byte word.
// A pending write in this state is returned ahead of the committed trie.
// A missing account returns a nil account and a nil error.
func (s *StateDB) Address32(addr common.Address32) (*types.StateAccount, error) {
	if s.address32Destroyed(addr) {
		return nil, nil
	}

	if obj := s.liveAddress32(addr); obj != nil {
		return obj.data.Copy(), nil
	}

	return s.address32FromTrie(addr)
}

func (s *StateDB) address32FromTrie(addr common.Address32) (*types.StateAccount, error) {
	tr, err := s.db.OpenTrie(s.originalRoot)

	if err != nil {
		return nil, err
	}

	stateTrie, ok := tr.(*trie.StateTrie)

	if !ok {
		return nil, errors.New("address32 accounts require a merkle patricia trie")
	}

	return stateTrie.GetAccountByHash(crypto.Keccak256Hash(addr[:]))
}

// address32Account loads an address32 and records a read error on StateDB.
// A missing account returns nil.
func (s *StateDB) address32Account(addr common.Address32) *types.StateAccount {
	acc, err := s.Address32(addr)

	if err != nil {
		s.setError(err)
		return nil
	}

	return acc
}

// Address32Empty reports whether the address32 is missing or empty under
// EIP-161 (nonce, balance, and code are all zero).
func (s *StateDB) Address32Empty(addr common.Address32) bool {
	acc := s.address32Account(addr)

	if acc == nil {
		return true
	}

	balance := acc.Balance

	if balance == nil {
		balance = common.U2560
	}

	return acc.Nonce == 0 && balance.IsZero() && bytes.Equal(acc.CodeHash, types.EmptyCodeHash.Bytes())
}

// Address32Exist reports whether the address32 is present in state.
func (s *StateDB) Address32Exist(addr common.Address32) bool {
	return s.address32Account(addr) != nil
}

// Address32Balance returns the address32 balance, or 0 when it is absent.
func (s *StateDB) Address32Balance(addr common.Address32) *uint256.Int {
	acc := s.address32Account(addr)

	if acc == nil || acc.Balance == nil {
		return common.U2560.Clone()
	}

	return acc.Balance.Clone()
}

// Address32Nonce returns the address32 nonce, or 0 when it is absent.
func (s *StateDB) Address32Nonce(addr common.Address32) uint64 {
	acc := s.address32Account(addr)

	if acc == nil {
		return 0
	}

	return acc.Nonce
}

// Address32Code returns the address32 code, or nil when it is absent or empty.
func (s *StateDB) Address32Code(addr common.Address32) []byte {
	if obj := s.liveAddress32(addr); obj != nil && obj.code != nil {
		return common.CopyBytes(obj.code)
	}

	acc := s.address32Account(addr)

	if acc == nil || len(acc.CodeHash) == 0 || bytes.Equal(acc.CodeHash, types.EmptyCodeHash.Bytes()) {
		return nil
	}

	return rawdb.ReadCode(s.db.TrieDB().Disk(), common.BytesToHash(acc.CodeHash))
}

// Address32CodeHash returns the address32 code hash.
// A missing account returns the zero hash, matching GetCodeHash.
func (s *StateDB) Address32CodeHash(addr common.Address32) common.Hash {
	acc := s.address32Account(addr)

	if acc == nil || len(acc.CodeHash) == 0 {
		return common.Hash{}
	}

	return common.BytesToHash(acc.CodeHash)
}

// Address32StorageRoot returns the address32 storage root.
// A missing account returns the zero hash, matching GetStorageRoot.
func (s *StateDB) Address32StorageRoot(addr common.Address32) common.Hash {
	acc := s.address32Account(addr)

	if acc == nil {
		return common.Hash{}
	}

	return acc.Root
}

// Address32State returns the address32 storage value at key, or the zero
// hash when the account or the slot is absent.
func (s *StateDB) Address32State(addr common.Address32, key common.Hash) common.Hash {
	if s.address32Destroyed(addr) {
		return common.Hash{}
	}

	if obj := s.liveAddress32(addr); obj != nil {
		if val, ok := obj.storage[key]; ok {
			return val
		}
	}

	return s.address32TrieState(addr, key)
}

// Address32CommittedState returns the storage value from before this transition.
func (s *StateDB) Address32CommittedState(addr common.Address32, key common.Hash) common.Hash {
	if obj := s.liveAddress32(addr); obj != nil {
		if val, ok := obj.originStorage[key]; ok {
			return val
		}
	}

	val := s.address32TrieState(addr, key)

	if obj := s.liveAddress32(addr); obj != nil {
		if obj.originStorage == nil {
			obj.originStorage = make(map[common.Hash]common.Hash)
		}

		obj.originStorage[key] = val
	}

	return val
}

// SetAddress32State writes key on the address32 for this transition.
// The returned hash is the value replaced.
func (s *StateDB) SetAddress32State(addr common.Address32, key, value common.Hash) common.Hash {
	prev := s.Address32State(addr, key)

	if prev == value {
		return prev
	}

	obj := s.getOrNewAddress32Account(addr)

	if obj.originStorage == nil {
		obj.originStorage = make(map[common.Hash]common.Hash)
	}

	if _, ok := obj.originStorage[key]; !ok {
		obj.originStorage[key] = s.address32TrieState(addr, key)
	}

	if obj.storage == nil {
		obj.storage = make(map[common.Hash]common.Hash)
	}

	old, had := obj.storage[key]
	s.journal.append(address32StorageChange{account: addr, key: key, prev: old, had: had})
	obj.storage[key] = value
	obj.dirty = true

	return prev
}

func (s *StateDB) address32TrieState(addr common.Address32, key common.Hash) common.Hash {
	acc := s.address32Account(addr)

	if acc == nil || acc.Root == (common.Hash{}) || acc.Root == types.EmptyRootHash {
		return common.Hash{}
	}

	st, err := trie.NewStateTrie(trie.StorageTrieID(s.originalRoot, crypto.Keccak256Hash(addr[:]), acc.Root), s.db.TrieDB())

	if err != nil {
		s.setError(err)
		return common.Hash{}
	}

	val, err := st.GetStorage(common.Address{}, key.Bytes())

	if err != nil {
		s.setError(err)
		return common.Hash{}
	}

	return common.BytesToHash(val)
}

// readAccount dispatches on the account kind. A new kind is a case here.
func readAccount[T any](s *StateDB, address common.StateAddress, address20 func(common.Address) T, address32 func(common.Address32) T, zero T) T {
	switch address.Kind() {
	case common.StateAddress20:
		account, _ := address.Address20()
		return address20(account)
	case common.StateAddress32:
		account, _ := address.Address32()
		return address32(account)
	default:
		s.setError(fmt.Errorf("unsupported state address kind %d", address.Kind()))
		return zero
	}
}

// AccountTrieKey is keccak256 of the account preimage. An Address32 hashes
// the 32-byte word. An address20 hashes the 20-byte address.
func AccountTrieKey(address common.StateAddress) (common.Hash, error) {
	preimage, err := address.Preimage()

	if err != nil {
		return common.Hash{}, err
	}

	return crypto.Keccak256Hash(preimage), nil
}

// AccountBalance returns the balance of an address20 or an Address32.
func (s *StateDB) AccountBalance(address common.StateAddress) *uint256.Int {
	return readAccount(s, address, s.GetBalance, s.Address32Balance, common.U2560.Clone())
}

// AccountNonce returns the nonce of an address20 or an Address32.
func (s *StateDB) AccountNonce(address common.StateAddress) uint64 {
	return readAccount(s, address, s.GetNonce, s.Address32Nonce, 0)
}

// AccountCode returns the code of an address20 or an Address32.
func (s *StateDB) AccountCode(address common.StateAddress) []byte {
	return readAccount(s, address, s.GetCode, s.Address32Code, nil)
}

// AccountCodeHash returns the code hash of an address20 or an Address32.
func (s *StateDB) AccountCodeHash(address common.StateAddress) common.Hash {
	return readAccount(s, address, s.GetCodeHash, s.Address32CodeHash, common.Hash{})
}

// AccountStorageRoot returns the storage root of an address20 or an Address32.
func (s *StateDB) AccountStorageRoot(address common.StateAddress) common.Hash {
	return readAccount(s, address, s.GetStorageRoot, s.Address32StorageRoot, common.Hash{})
}

// AccountState returns the storage value of an address20 or an Address32.
func (s *StateDB) AccountState(address common.StateAddress, key common.Hash) common.Hash {
	return readAccount(s, address,
		func(addr common.Address) common.Hash { return s.GetState(addr, key) },
		func(addr common.Address32) common.Hash { return s.Address32State(addr, key) },
		common.Hash{},
	)
}
