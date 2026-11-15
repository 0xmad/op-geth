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

// wideAccount is an Address32 account tracked inside one state transition.
type wideAccount struct {
	data    types.StateAccount
	origin  *types.StateAccount
	created bool
	dirty   bool
}

func (w *wideAccount) copy() *wideAccount {
	out := &wideAccount{
		data:    *w.data.Copy(),
		created: w.created,
		dirty:   w.dirty,
	}

	if w.origin != nil {
		out.origin = w.origin.Copy()
	}

	return out
}

func (s *StateDB) liveWide(addr common.Address32) *wideAccount {
	if s.wideAccounts == nil {
		return nil
	}

	return s.wideAccounts[addr]
}

func (s *StateDB) getOrNewWideAccount(addr common.Address32) *wideAccount {
	if s.wideAccounts == nil {
		s.wideAccounts = make(map[common.Address32]*wideAccount)
	}

	if obj := s.wideAccounts[addr]; obj != nil {
		return obj
	}

	loaded, err := s.address32FromTrie(addr)

	if err != nil {
		s.setError(err)
		return &wideAccount{data: *types.NewEmptyStateAccount()}
	}

	obj := &wideAccount{data: *types.NewEmptyStateAccount()}

	if loaded != nil {
		obj.data = *loaded.Copy()

		if obj.data.Balance == nil {
			obj.data.Balance = new(uint256.Int)
		}

		obj.origin = loaded.Copy()
	} else {
		obj.created = true
		s.journal.append(wideCreateChange{account: addr})
	}

	s.wideAccounts[addr] = obj

	return obj
}

// AddAddress32Balance adds amount to the wide account.
func (s *StateDB) AddAddress32Balance(addr common.Address32, amount *uint256.Int) {
	s.changeAddress32Balance(addr, amount, true)
}

// SubAddress32Balance subtracts amount from the wide account.
func (s *StateDB) SubAddress32Balance(addr common.Address32, amount *uint256.Int) {
	s.changeAddress32Balance(addr, amount, false)
}

func (s *StateDB) changeAddress32Balance(addr common.Address32, amount *uint256.Int, add bool) {
	if amount.IsZero() {
		return
	}

	obj := s.getOrNewWideAccount(addr)

	if obj.data.Balance == nil {
		obj.data.Balance = new(uint256.Int)
	}

	s.journal.append(wideBalanceChange{account: addr, prev: obj.data.Balance.Clone()})

	if add {
		obj.data.Balance = new(uint256.Int).Add(obj.data.Balance, amount)
	} else {
		obj.data.Balance = new(uint256.Int).Sub(obj.data.Balance, amount)
	}

	obj.dirty = true
}

// SetAddress32Nonce sets the nonce of the wide account.
func (s *StateDB) SetAddress32Nonce(addr common.Address32, nonce uint64) {
	obj := s.getOrNewWideAccount(addr)

	if obj.data.Nonce == nonce {
		return
	}

	s.journal.append(wideNonceChange{account: addr, prev: obj.data.Nonce})
	obj.data.Nonce = nonce
	obj.dirty = true
}

// commitWideAccounts writes dirty Address32 accounts into the account trie.
func (s *StateDB) commitWideAccounts() {
	if len(s.wideAccounts) == 0 || s.trie == nil {
		return
	}

	tr, ok := s.trie.(*trie.StateTrie)

	if !ok {
		s.setError(fmt.Errorf("address32 accounts require a merkle patricia trie"))
		return
	}

	for addr, obj := range s.wideAccounts {
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

type wideCreateChange struct {
	account common.Address32
}

func (ch wideCreateChange) revert(s *StateDB) {
	delete(s.wideAccounts, ch.account)
}

func (ch wideCreateChange) dirtied() *common.Address { return nil }

func (ch wideCreateChange) copy() journalEntry {
	return wideCreateChange{account: ch.account}
}

type wideBalanceChange struct {
	account common.Address32
	prev    *uint256.Int
}

func (ch wideBalanceChange) revert(s *StateDB) {
	obj := s.liveWide(ch.account)
	if obj == nil {
		return
	}
	obj.data.Balance = ch.prev.Clone()
	obj.dirty = true
}

func (ch wideBalanceChange) dirtied() *common.Address { return nil }

func (ch wideBalanceChange) copy() journalEntry {
	return wideBalanceChange{account: ch.account, prev: ch.prev.Clone()}
}

type wideNonceChange struct {
	account common.Address32
	prev    uint64
}

func (ch wideNonceChange) revert(s *StateDB) {
	obj := s.liveWide(ch.account)

	if obj == nil {
		return
	}

	obj.data.Nonce = ch.prev
	obj.dirty = true
}

func (ch wideNonceChange) dirtied() *common.Address { return nil }

func (ch wideNonceChange) copy() journalEntry {
	return wideNonceChange{account: ch.account, prev: ch.prev}
}
