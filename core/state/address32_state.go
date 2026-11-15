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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
)

// address32Account is an Address32 account tracked inside one state transition.
type address32Account struct {
	data    types.StateAccount
	origin  *types.StateAccount
	created bool
	dirty   bool
}

func (w *address32Account) copy() *address32Account {
	out := &address32Account{
		data:    *w.data.Copy(),
		created: w.created,
		dirty:   w.dirty,
	}

	if w.origin != nil {
		out.origin = w.origin.Copy()
	}

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

// commitAddress32Accounts writes dirty Address32 accounts into the account trie.
func (s *StateDB) commitAddress32Accounts() {
	if len(s.address32Accounts) == 0 || s.trie == nil {
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

		if err := tr.UpdateAccountPreimage(addr[:], obj.data.Copy()); err != nil {
			s.setError(fmt.Errorf("update address32 %s: %w", addr.Hex(), err))
			return
		}

		obj.dirty = false
	}
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
