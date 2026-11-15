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

package filters

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestAddress32LogFilterMatchesLogger(t *testing.T) {
	var logger common.Address32
	logger[0] = 0xab
	logger[31] = 0x11
	address20 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	logs := []*types.Log{
		{Address32: &logger, Data: []byte{1}},
		{Address: address20, Data: []byte{2}},
	}
	matched := filterLogs(logs, nil, nil, types.Loggers{Addresses32: []common.Address32{logger}}, nil)

	if len(matched) != 1 || matched[0].Address32 == nil || *matched[0].Address32 != logger {
		t.Fatalf("address32 matches = %+v", matched)
	}

	address20Matches := filterLogs(logs, nil, nil, types.Loggers{Addresses: []common.Address{address20}}, nil)

	if len(address20Matches) != 1 || address20Matches[0].Address != address20 {
		t.Fatalf("address20 matches = %+v", address20Matches)
	}

	var bloom types.Bloom
	bloom.Add(logger[:])

	if !bloomFilter(bloom, types.Loggers{Addresses32: []common.Address32{logger}}, nil) {
		t.Fatal("bloom missed the address32 logger")
	}

	if bloomFilter(bloom, types.Loggers{Addresses: []common.Address{address20}}, nil) {
		t.Fatal("address32 bloom matched an address20")
	}
}
