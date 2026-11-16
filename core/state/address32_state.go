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
	"fmt"
	"maps"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/holiman/uint256"
)

// address32Account is an Address32 account tracked inside one state transition.
type address32Account struct {
	data                 types.StateAccount
	origin               *types.StateAccount
	code                 []byte
	storage              map[common.Hash]common.Hash
	originStorage        map[common.Hash]common.Hash
	storageTrie          *trie.StateTrie
	created              bool // absent from the trie when first seen; not the EIP-6780 flag
	dirty                bool
	createdInTransaction bool // EIP-6780: set only by CreateAddress32Contract
	selfDestructed       bool
}

func (w *address32Account) copy() *address32Account {
	out := &address32Account{
		data:                 *w.data.Copy(),
		code:                 common.CopyBytes(w.code),
		storage:              copyHashMap(w.storage),
		originStorage:        copyHashMap(w.originStorage),
		created:              w.created,
		dirty:                w.dirty,
		createdInTransaction: w.createdInTransaction,
		selfDestructed:       w.selfDestructed,
	}

	if w.storageTrie != nil {
		out.storageTrie = w.storageTrie.Copy()
	}

	if w.origin != nil {
		out.origin = w.origin.Copy()
	}

	return out
}

func copyAddress32Slots(in map[common.Address32]map[common.Hash]struct{}) map[common.Address32]map[common.Hash]struct{} {
	out := make(map[common.Address32]map[common.Hash]struct{}, len(in))

	for addr, slots := range in {
		copied := make(map[common.Hash]struct{}, len(slots))

		for slot := range slots {
			copied[slot] = struct{}{}
		}

		out[addr] = copied
	}

	return out
}

func copyHashMap(in map[common.Hash]common.Hash) map[common.Hash]common.Hash {
	if in == nil {
		return nil
	}

	out := make(map[common.Hash]common.Hash, len(in))

	maps.Copy(out, in)

	return out
}

func (s *StateDB) liveAddress32(addr common.Address32) *address32Account {
	if s.address32Accounts == nil {
		return nil
	}

	return s.address32Accounts[addr]
}

func (s *StateDB) getOrNewAddress32Account(addr common.Address32) *address32Account {
	if s.address32Accounts == nil {
		s.address32Accounts = make(map[common.Address32]*address32Account)
	}

	if obj := s.address32Accounts[addr]; obj != nil {
		return obj
	}

	loaded, err := s.address32FromTrie(addr)

	if err != nil {
		s.setError(err)
		return &address32Account{data: *types.NewEmptyStateAccount()}
	}

	obj := &address32Account{data: *types.NewEmptyStateAccount()}

	if loaded != nil {
		obj.data = *loaded.Copy()

		if obj.data.Balance == nil {
			obj.data.Balance = new(uint256.Int)
		}

		obj.origin = loaded.Copy()
	} else {
		obj.created = true
		s.journal.append(address32CreateChange{account: addr})
	}

	s.address32Accounts[addr] = obj

	return obj
}

// Address32InAccessList reports whether the address32 is warm.
func (s *StateDB) Address32InAccessList(addr common.Address32) bool {
	if s.address32Access == nil {
		return false
	}

	_, ok := s.address32Access[addr]
	return ok
}

// AddAddress32ToAccessList warms the address32.
// A word in the address20 subspace is not warmed here.
func (s *StateDB) AddAddress32ToAccessList(addr common.Address32) {
	if s.address32Access == nil {
		s.address32Access = make(map[common.Address32]struct{})
	}

	if _, ok := s.address32Access[addr]; ok {
		return
	}

	s.address32Access[addr] = struct{}{}
	s.journal.append(address32AccessListAddChange{account: addr})
}

// AddAddress32Balance adds amount to the address32.
func (s *StateDB) AddAddress32Balance(addr common.Address32, amount *uint256.Int) {
	s.changeAddress32Balance(addr, amount, true)
}

// SubAddress32Balance subtracts amount from the address32.
func (s *StateDB) SubAddress32Balance(addr common.Address32, amount *uint256.Int) {
	s.changeAddress32Balance(addr, amount, false)
}

func (s *StateDB) changeAddress32Balance(addr common.Address32, amount *uint256.Int, add bool) {
	if amount.IsZero() {
		return
	}

	obj := s.getOrNewAddress32Account(addr)

	if obj.data.Balance == nil {
		obj.data.Balance = new(uint256.Int)
	}

	s.journal.append(address32BalanceChange{account: addr, prev: obj.data.Balance.Clone()})

	if add {
		obj.data.Balance = new(uint256.Int).Add(obj.data.Balance, amount)
	} else {
		obj.data.Balance = new(uint256.Int).Sub(obj.data.Balance, amount)
	}

	obj.dirty = true
}

// SetAddress32Nonce sets the nonce of the address32.
func (s *StateDB) SetAddress32Nonce(addr common.Address32, nonce uint64) {
	obj := s.getOrNewAddress32Account(addr)

	if obj.data.Nonce == nonce {
		return
	}

	s.journal.append(address32NonceChange{account: addr, prev: obj.data.Nonce})
	obj.data.Nonce = nonce
	obj.dirty = true
}

// SetAddress32Code stores code on the address32.
// The blob stays on the live account until commit writes it to disk.
func (s *StateDB) SetAddress32Code(addr common.Address32, code []byte) {
	obj := s.getOrNewAddress32Account(addr)
	s.journal.append(address32CodeChange{
		account:  addr,
		prev:     common.CopyBytes(obj.code),
		prevHash: common.CopyBytes(obj.data.CodeHash),
	})
	obj.code = common.CopyBytes(code)
	obj.data.CodeHash = crypto.Keccak256Hash(code).Bytes()
	obj.dirty = true
}

// address32CodeBlobs returns code written during this state.
// Dirty is not required: an earlier root hash may already have cleared it.
func (s *StateDB) address32CodeBlobs() []contractCode {
	if len(s.address32Accounts) == 0 {
		return nil
	}

	var codes []contractCode

	for _, obj := range s.address32Accounts {
		if obj == nil || len(obj.code) == 0 {
			continue
		}

		codes = append(codes, contractCode{
			hash: crypto.Keccak256Hash(obj.code),
			blob: common.CopyBytes(obj.code),
		})
	}

	return codes
}

// commitAddress32Accounts writes dirty Address32 accounts into the account trie.
func (s *StateDB) commitAddress32Accounts() {
	if s.trie == nil || (len(s.address32Accounts) == 0 && len(s.address32Destruct) == 0) {
		return
	}

	tr, ok := s.trie.(*trie.StateTrie)

	if !ok {
		s.setError(fmt.Errorf("address32 accounts require a merkle patricia trie"))
		return
	}

	for addr, obj := range s.address32Accounts {
		if obj == nil || !obj.dirty {
			continue
		}

		if err := s.updateAddress32Storage(addr, obj); err != nil {
			s.setError(fmt.Errorf("update address32 storage %s: %w", addr.Hex(), err))
			return
		}

		if err := tr.UpdateAccountPreimage(addr[:], obj.data.Copy()); err != nil {
			s.setError(fmt.Errorf("update address32 %s: %w", addr.Hex(), err))
			return
		}

		obj.dirty = false
	}

	for addr := range s.address32Destruct {
		tr.MustDelete(addr[:])
	}
}

// updateAddress32Storage folds the storage overlay into a trie opened at the
// account's root from when it was loaded. The trie stays on the account until
// the block commit collects its nodes. The account root is updated first so
// the account trie records the storage root.
func (s *StateDB) updateAddress32Storage(addr common.Address32, obj *address32Account) error {
	if len(obj.storage) == 0 {
		return nil
	}

	base := types.EmptyRootHash

	if obj.origin != nil && obj.origin.Root != (common.Hash{}) {
		base = obj.origin.Root
	}

	st, err := trie.NewStateTrie(trie.StorageTrieID(s.originalRoot, crypto.Keccak256Hash(addr[:]), base), s.db.TrieDB())

	if err != nil {
		return err
	}

	for key, value := range obj.storage {
		var writeErr error

		if value == (common.Hash{}) {
			writeErr = st.DeleteStorage(common.Address{}, key[:])
		} else {
			writeErr = st.UpdateStorage(common.Address{}, key[:], common.TrimLeftZeroes(value[:]))
		}

		if writeErr != nil {
			return writeErr
		}
	}

	obj.data.Root = st.Hash()
	obj.storageTrie = st
	return nil
}

// commitAddress32Storage returns the dirty storage-trie nodes for the block commit.
func (s *StateDB) commitAddress32Storage() []*trienode.NodeSet {
	var sets []*trienode.NodeSet

	for _, obj := range s.address32Accounts {
		if obj == nil || obj.storageTrie == nil {
			continue
		}

		_, nodes := obj.storageTrie.Commit(false)
		obj.storageTrie = nil

		if nodes != nil {
			sets = append(sets, nodes)
		}
	}

	return sets
}

// CreateAddress32Contract marks addr as created in this transaction.
// SELFDESTRUCT32 deletes only that kind of account.
func (s *StateDB) CreateAddress32Contract(addr common.Address32) {
	obj := s.getOrNewAddress32Account(addr)

	if obj.createdInTransaction {
		return
	}

	obj.createdInTransaction = true
	s.journal.append(address32CreatedInTransactionChange{account: addr})
}

// SelfDestructAddress32 registers addr for deletion when it was created
// in this transaction. The balance transfer is done by the opcode.
func (s *StateDB) SelfDestructAddress32(addr common.Address32) {
	obj := s.liveAddress32(addr)

	if obj == nil || !obj.createdInTransaction || obj.selfDestructed {
		return
	}

	obj.selfDestructed = true
	s.journal.append(address32SelfDestructChange{account: addr})
}

// Address32SelfDestructed reports whether addr is registered for deletion.
func (s *StateDB) Address32SelfDestructed(addr common.Address32) bool {
	if obj := s.liveAddress32(addr); obj != nil && obj.selfDestructed {
		return true
	}

	return s.address32Destroyed(addr)
}

func (s *StateDB) address32Destroyed(addr common.Address32) bool {
	if len(s.address32Destruct) == 0 {
		return false
	}

	_, ok := s.address32Destruct[addr]
	return ok
}

// finaliseAddress32 ends the transaction for address32 accounts.
// A contract created and selfdestructed in this transaction leaves the live set.
// createdInTransaction does not carry into the next transaction.
// The storage overlay is copied into originStorage so the next transaction's
// SSTORE original is the value at this boundary.
func (s *StateDB) finaliseAddress32() {
	for addr, obj := range s.address32Accounts {
		if obj == nil {
			continue
		}

		obj.createdInTransaction = false

		// The next transaction's original storage is the value at this boundary.
		for key, value := range obj.storage {
			if obj.originStorage == nil {
				obj.originStorage = make(map[common.Hash]common.Hash)
			}

			obj.originStorage[key] = value
		}

		if !obj.selfDestructed {
			continue
		}

		if s.address32Destruct == nil {
			s.address32Destruct = make(map[common.Address32]*address32Account)
		}

		if _, ok := s.address32Destruct[addr]; !ok {
			s.address32Destruct[addr] = obj
		}

		delete(s.address32Accounts, addr)
	}
}

// Address32SlotWarm reports whether slot was touched in this transaction.
func (s *StateDB) Address32SlotWarm(addr common.Address32, slot common.Hash) bool {
	slots := s.address32Slots[addr]

	if slots == nil {
		return false
	}

	_, ok := slots[slot]
	return ok
}

// WarmAddress32Slot marks slot warm. It does not create the account.
func (s *StateDB) WarmAddress32Slot(addr common.Address32, slot common.Hash) {
	if s.Address32SlotWarm(addr, slot) {
		return
	}

	if s.address32Slots == nil {
		s.address32Slots = make(map[common.Address32]map[common.Hash]struct{})
	}

	if s.address32Slots[addr] == nil {
		s.address32Slots[addr] = make(map[common.Hash]struct{})
	}

	s.address32Slots[addr][slot] = struct{}{}
	s.journal.append(address32SlotWarmChange{account: addr, slot: slot})
}

// AsStateDB returns the state database behind db.
// db may be a StateDB or the tracing wrapper around one.
func AsStateDB(db any) *StateDB {
	switch db := db.(type) {
	case *StateDB:
		return db
	case *hookedStateDB:
		return db.inner
	default:
		return nil
	}
}

type address32AccessListAddChange struct {
	account common.Address32
}

func (ch address32AccessListAddChange) revert(s *StateDB) {
	delete(s.address32Access, ch.account)
}

func (ch address32AccessListAddChange) dirtied() *common.Address { return nil }

func (ch address32AccessListAddChange) copy() journalEntry {
	return address32AccessListAddChange{account: ch.account}
}

type address32CreateChange struct {
	account common.Address32
}

func (ch address32CreateChange) revert(s *StateDB) {
	delete(s.address32Accounts, ch.account)
}

func (ch address32CreateChange) dirtied() *common.Address { return nil }

func (ch address32CreateChange) copy() journalEntry {
	return address32CreateChange{account: ch.account}
}

type address32BalanceChange struct {
	account common.Address32
	prev    *uint256.Int
}

func (ch address32BalanceChange) revert(s *StateDB) {
	obj := s.liveAddress32(ch.account)
	if obj == nil {
		return
	}
	obj.data.Balance = ch.prev.Clone()
	obj.dirty = true
}

func (ch address32BalanceChange) dirtied() *common.Address { return nil }

func (ch address32BalanceChange) copy() journalEntry {
	return address32BalanceChange{account: ch.account, prev: ch.prev.Clone()}
}

type address32NonceChange struct {
	account common.Address32
	prev    uint64
}

func (ch address32NonceChange) revert(s *StateDB) {
	obj := s.liveAddress32(ch.account)

	if obj == nil {
		return
	}

	obj.data.Nonce = ch.prev
	obj.dirty = true
}

func (ch address32NonceChange) dirtied() *common.Address { return nil }

func (ch address32NonceChange) copy() journalEntry {
	return address32NonceChange{account: ch.account, prev: ch.prev}
}

type address32CodeChange struct {
	account  common.Address32
	prev     []byte
	prevHash []byte
}

func (ch address32CodeChange) revert(s *StateDB) {
	obj := s.liveAddress32(ch.account)

	if obj == nil {
		return
	}

	obj.code = common.CopyBytes(ch.prev)
	obj.data.CodeHash = common.CopyBytes(ch.prevHash)
	obj.dirty = true
}

func (ch address32CodeChange) dirtied() *common.Address { return nil }

func (ch address32CodeChange) copy() journalEntry {
	return address32CodeChange{
		account:  ch.account,
		prev:     common.CopyBytes(ch.prev),
		prevHash: common.CopyBytes(ch.prevHash),
	}
}

type address32CreatedInTransactionChange struct {
	account common.Address32
}

func (ch address32CreatedInTransactionChange) revert(s *StateDB) {
	obj := s.liveAddress32(ch.account)

	if obj == nil {
		return
	}

	obj.createdInTransaction = false
}

func (ch address32CreatedInTransactionChange) dirtied() *common.Address { return nil }

func (ch address32CreatedInTransactionChange) copy() journalEntry {
	return address32CreatedInTransactionChange{account: ch.account}
}

type address32SelfDestructChange struct {
	account common.Address32
}

func (ch address32SelfDestructChange) revert(s *StateDB) {
	obj := s.liveAddress32(ch.account)

	if obj == nil {
		return
	}

	obj.selfDestructed = false
}

func (ch address32SelfDestructChange) dirtied() *common.Address { return nil }

func (ch address32SelfDestructChange) copy() journalEntry {
	return address32SelfDestructChange{account: ch.account}
}

type address32StorageChange struct {
	account common.Address32
	key     common.Hash
	prev    common.Hash
	had     bool
}

func (ch address32StorageChange) revert(s *StateDB) {
	obj := s.liveAddress32(ch.account)

	if obj == nil || obj.storage == nil {
		return
	}

	if ch.had {
		obj.storage[ch.key] = ch.prev
		return
	}

	delete(obj.storage, ch.key)
}

func (ch address32StorageChange) dirtied() *common.Address { return nil }

func (ch address32StorageChange) copy() journalEntry {
	return address32StorageChange{account: ch.account, key: ch.key, prev: ch.prev, had: ch.had}
}

type address32SlotWarmChange struct {
	account common.Address32
	slot    common.Hash
}

func (ch address32SlotWarmChange) revert(s *StateDB) {
	slots := s.address32Slots[ch.account]

	if slots == nil {
		return
	}

	delete(slots, ch.slot)
}

func (ch address32SlotWarmChange) dirtied() *common.Address { return nil }

func (ch address32SlotWarmChange) copy() journalEntry {
	return address32SlotWarmChange{account: ch.account, slot: ch.slot}
}
