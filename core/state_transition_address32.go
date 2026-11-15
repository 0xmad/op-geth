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
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

func address32TransactionToMessage(tx *types.Transaction, signer types.Signer, baseFee *big.Int) (*Message, error) {
	inner := tx.Address32Tx()

	if inner == nil {
		return nil, fmt.Errorf("%w: type %d", ErrTxTypeNotSupported, tx.Type())
	}

	from, err := types.SenderAccount(signer, tx)

	if err != nil {
		return nil, err
	}

	var to *common.StateAddress

	if word, _ := inner.ToWord(); word != nil {
		recipient, err := common.StateAddressFromWord(word)

		if err != nil {
			return nil, err
		}

		to = &recipient
	}

	msg := &Message{
		Nonce:          tx.Nonce(),
		GasLimit:       tx.Gas(),
		GasPrice:       new(big.Int).Set(tx.GasPrice()),
		GasFeeCap:      new(big.Int).Set(tx.GasFeeCap()),
		GasTipCap:      new(big.Int).Set(tx.GasTipCap()),
		Value:          tx.Value(),
		Data:           tx.Data(),
		RollupCostData: tx.RollupCostData(),
		Address32: &Address32Call{
			SenderKind: inner.SenderKind,
			From:       from,
			To:         to,
			AccessList: inner.AccessList,
		},
	}

	if address20, ok := from.Address20(); ok {
		msg.From = address20
	}

	if address20To := tx.To(); address20To != nil {
		msg.To = address20To
	}

	if baseFee != nil {
		msg.GasPrice = new(big.Int).Add(msg.GasTipCap, baseFee)

		if msg.GasPrice.Cmp(msg.GasFeeCap) > 0 {
			msg.GasPrice = new(big.Int).Set(msg.GasFeeCap)
		}
	}

	return msg, nil
}

func applyAddress32Message(evm *vm.EVM, msg *Message, gp *GasPool) (*ExecutionResult, error) {
	if !evm.ChainConfig().IsAddress32(evm.Context.Time) {
		return nil, fmt.Errorf("%w: type %d rejected, Address32 is not active", ErrTxTypeNotSupported, types.Address32TxType)
	}

	statedb := state.AsStateDB(evm.StateDB)

	if statedb == nil {
		return nil, fmt.Errorf("address32 transfer requires a state database")
	}

	call := msg.Address32
	sender := accountBalance{statedb, call.From}
	creating := call.To == nil

	var recipient accountBalance

	if !creating {
		recipient = accountBalance{statedb, *call.To}
	}

	chargeFees := true

	if !msg.SkipNonceChecks {
		if err := sender.checkNonce(msg.Nonce); err != nil {
			return nil, err
		}
	}

	if !msg.SkipTransactionChecks {
		if len(sender.code()) > 0 {
			return nil, fmt.Errorf("%w: address %s", ErrSenderNoEOA, sender.label())
		}
	} else if msg.SkipNonceChecks {
		// eth_call and eth_estimateGas do not buy rollup fees.
		chargeFees = false
	}

	if evm.ChainConfig().IsLondon(evm.Context.BlockNumber) {
		skip := evm.Config.NoBaseFee && msg.GasFeeCap.BitLen() == 0 && msg.GasTipCap.BitLen() == 0

		if !skip {
			if msg.GasFeeCap.Cmp(msg.GasTipCap) < 0 {
				return nil, fmt.Errorf("%w: address %s", ErrTipAboveFeeCap, sender.label())
			}

			if msg.GasFeeCap.Cmp(evm.Context.BaseFee) < 0 {
				return nil, fmt.Errorf("%w: address %s, maxFeePerGas: %s, baseFee: %s", ErrFeeCapTooLow, sender.label(), msg.GasFeeCap, evm.Context.BaseFee)
			}
		}
	}

	rules := evm.ChainConfig().Rules(evm.Context.BlockNumber, evm.Context.Random != nil, evm.Context.Time)
	intrinsic, err := IntrinsicGasAddress32(msg.Data, call.AccessList, creating, rules.IsShanghai)

	if err != nil {
		return nil, err
	}

	if msg.GasLimit < intrinsic {
		return nil, fmt.Errorf("%w: have %d, want %d", ErrIntrinsicGas, msg.GasLimit, intrinsic)
	}

	floor := intrinsic

	if rules.IsPrague {
		floorData, err := FloorDataGas(msg.Data)

		if err != nil {
			return nil, err
		}

		if msg.GasLimit < floorData {
			return nil, fmt.Errorf("%w: have %d, want %d", ErrFloorDataGas, msg.GasLimit, floorData)
		}

		if floor < floorData {
			floor = floorData
		}
	}

	if creating && rules.IsShanghai && len(msg.Data) > params.MaxInitCodeSize {
		return nil, fmt.Errorf("%w: code size %v limit %v", ErrMaxInitCodeSizeExceeded, len(msg.Data), params.MaxInitCodeSize)
	}

	// A user contract in the other namespace is not executed. The transaction is
	// included, the nonce is spent, and fees are paid. The callee is left as it was.
	// A 7702 delegation is code. A codeless target still receives the value.
	// A precompile has no account code and runs on the active list.
	var crossNamespaceRevert bool

	if !creating {
		code := recipient.code()
		crossNamespaceRevert = len(code) > 0 && call.From.Kind() != call.To.Kind()
	}

	var l1Cost *big.Int
	var operatorLimit *uint256.Int

	if chargeFees {
		if evm.Context.L1CostFunc != nil {
			l1Cost = evm.Context.L1CostFunc(msg.RollupCostData, evm.Context.Time)
		}

		if evm.Context.OperatorCostFunc != nil {
			operatorLimit = evm.Context.OperatorCostFunc(msg.GasLimit, evm.Context.Time)
		}
	}

	if err := sender.canPay(msg, l1Cost, operatorLimit); err != nil {
		return nil, err
	}

	if err := gp.SubGas(msg.GasLimit); err != nil {
		return nil, err
	}

	bought := new(big.Int).SetUint64(msg.GasLimit)
	bought.Mul(bought, msg.GasPrice)

	if l1Cost != nil {
		bought.Add(bought, l1Cost)
	}

	if operatorLimit != nil {
		bought.Add(bought, operatorLimit.ToBig())
	}

	if err := sender.sub(bought, tracing.BalanceDecreaseGasBuy); err != nil {
		return nil, err
	}

	fees := address32Fees{
		evm:           evm,
		statedb:       statedb,
		msg:           msg,
		gp:            gp,
		sender:        sender,
		rules:         rules,
		intrinsic:     intrinsic,
		floor:         floor,
		l1Cost:        l1Cost,
		operatorLimit: operatorLimit,
		chargeFees:    chargeFees,
	}

	if creating {
		return fees.applyCreation()
	}

	if crossNamespaceRevert {
		sender.setNonce(sender.nonce() + 1)
		gasUsed := min(floor, msg.GasLimit)

		fees.refundUnspent(msg.GasLimit - gasUsed)

		if err := fees.settle(gasUsed); err != nil {
			return nil, err
		}

		return &ExecutionResult{
			UsedGas:    gasUsed,
			MaxUsedGas: gasUsed,
			Err:        vm.ErrExecutionReverted,
		}, nil
	}

	return fees.applyCall()
}

// address32Fees is the fee context shared by an address32 call and an address32 creation.
type address32Fees struct {
	evm           *vm.EVM
	statedb       *state.StateDB
	msg           *Message
	gp            *GasPool
	sender        accountBalance
	rules         params.Rules
	intrinsic     uint64
	floor         uint64
	l1Cost        *big.Int
	operatorLimit *uint256.Int
	chargeFees    bool
}

func (f address32Fees) refundUnspent(gasRemaining uint64) {
	if gasRemaining == 0 {
		return
	}

	refunded := new(big.Int).SetUint64(gasRemaining)
	refunded.Mul(refunded, f.msg.GasPrice)
	f.sender.add(uint256.MustFromBig(refunded), tracing.BalanceIncreaseGasReturn)
	f.gp.AddGas(gasRemaining)
}

func (f address32Fees) settle(gasUsed uint64) error {
	if f.chargeFees && f.evm.Context.OperatorCostFunc != nil && f.rules.IsOptimismIsthmus {
		usedCost := f.evm.Context.OperatorCostFunc(gasUsed, f.evm.Context.Time)

		if f.operatorLimit != nil && f.operatorLimit.Cmp(usedCost) > 0 {
			f.sender.add(new(uint256.Int).Sub(f.operatorLimit, usedCost), tracing.BalanceIncreaseGasReturn)
		}
	}

	payFees := !(f.evm.Config.NoBaseFee && f.msg.GasFeeCap.Sign() == 0 && f.msg.GasTipCap.Sign() == 0)

	if payFees && f.chargeFees {
		if err := payAddress32Fees(f.evm, f.statedb, f.msg, f.rules, gasUsed, f.l1Cost); err != nil {
			return err
		}
	}

	return f.statedb.Error()
}

func (f address32Fees) applyCreation() (*ExecutionResult, error) {
	gasRemaining := f.msg.GasLimit - f.intrinsic
	value, overflow := uint256.FromBig(f.msg.Value)

	if overflow {
		return nil, fmt.Errorf("%w: address %s", ErrInsufficientFundsForTransfer, f.sender.label())
	}

	var (
		ret   []byte
		vmerr error
	)

	f.evm.SetTxContext(NewEVMTxContext(f.msg))
	f.sender.addr.Use(func(addr common.Address) {
		ret, _, gasRemaining, vmerr = f.evm.Create(addr, f.msg.Data, gasRemaining, value)
	}, func(addr common.Address32) {
		ret, _, gasRemaining, vmerr = f.evm.CreateAddress32(addr, f.msg.Data, gasRemaining, value, vm.CREATE32)
	})

	// Creation returns before touching the nonce when the formula address is
	// an address20. The transaction is included, so the sender nonce is spent
	// and the initcode does not run.
	if errors.Is(vmerr, common.ErrAddress32InAddress20Subspace) {
		f.sender.setNonce(f.sender.nonce() + 1)
	}

	return f.finish(gasRemaining, ret, vmerr)
}

func (f address32Fees) applyCall() (*ExecutionResult, error) {
	gasRemaining := f.msg.GasLimit - f.intrinsic
	value, overflow := uint256.FromBig(f.msg.Value)

	if overflow {
		return nil, fmt.Errorf("%w: address %s", ErrInsufficientFundsForTransfer, f.sender.label())
	}

	f.sender.setNonce(f.sender.nonce() + 1)
	f.evm.SetTxContext(NewEVMTxContext(f.msg))
	ret, gasRemaining, vmerr := f.evm.CallAccount(f.msg.Address32.From, *f.msg.Address32.To, f.msg.Data, gasRemaining, value, false)

	if vmerr == nil {
		vmerr = f.statedb.Error()
	}

	return f.finish(gasRemaining, ret, vmerr)
}

func (f address32Fees) finish(gasRemaining uint64, ret []byte, vmerr error) (*ExecutionResult, error) {
	refund := f.statedb.GetRefund()
	gasUsed := f.msg.GasLimit - gasRemaining
	cap := gasUsed / params.RefundQuotient

	if f.rules.IsLondon {
		cap = gasUsed / params.RefundQuotientEIP3529
	}

	if refund > cap {
		refund = cap
	}

	gasRemaining += refund

	if f.rules.IsPrague && f.msg.GasLimit-gasRemaining < f.floor {
		gasRemaining = f.msg.GasLimit - f.floor
	}

	gasUsed = f.msg.GasLimit - gasRemaining
	f.refundUnspent(gasRemaining)

	if err := f.settle(gasUsed); err != nil {
		return nil, err
	}

	return &ExecutionResult{
		UsedGas:    gasUsed,
		MaxUsedGas: gasUsed,
		Err:        vmerr,
		ReturnData: ret,
	}, nil
}

func payAddress32Fees(evm *vm.EVM, statedb *state.StateDB, msg *Message, rules params.Rules, gasUsed uint64, l1Cost *big.Int) error {
	effectiveTip := new(big.Int).Set(msg.GasPrice)

	if rules.IsLondon {
		effectiveTip.Sub(effectiveTip, evm.Context.BaseFee)
	}

	if effectiveTip.Sign() > 0 {
		tip := new(uint256.Int).SetUint64(gasUsed)
		tip.Mul(tip, uint256.MustFromBig(effectiveTip))
		statedb.AddBalance(evm.Context.Coinbase, tip, tracing.BalanceIncreaseRewardTransactionFee)
	}

	if evm.ChainConfig().Optimism == nil || !rules.IsOptimismBedrock {
		return nil
	}

	gasCost := new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), evm.Context.BaseFee)
	amt, overflow := uint256.FromBig(gasCost)

	if overflow {
		return fmt.Errorf("optimism gas cost overflows U256: %d", gasCost)
	}

	statedb.AddBalance(params.OptimismBaseFeeRecipient, amt, tracing.BalanceIncreaseRewardTransactionFee)

	if l1Cost != nil {
		amt, overflow = uint256.FromBig(l1Cost)

		if overflow {
			return fmt.Errorf("optimism l1 cost overflows U256: %d", l1Cost)
		}

		statedb.AddBalance(params.OptimismL1FeeRecipient, amt, tracing.BalanceIncreaseRewardTransactionFee)
	}

	if rules.IsOptimismIsthmus && evm.Context.OperatorCostFunc != nil {
		operatorFee := evm.Context.OperatorCostFunc(gasUsed, evm.Context.Time)
		statedb.AddBalance(params.OptimismOperatorFeeRecipient, operatorFee, tracing.BalanceIncreaseRewardTransactionFee)
	}

	return nil
}

// accountBalance reads and writes one address20 or one Address32.
type accountBalance struct {
	db   *state.StateDB
	addr common.StateAddress
}

func (a accountBalance) balance() *uint256.Int {
	return a.db.AccountBalance(a.addr)
}

func (a accountBalance) nonce() uint64 {
	return a.db.AccountNonce(a.addr)
}

func (a accountBalance) code() []byte {
	return a.db.AccountCode(a.addr)
}

func (a accountBalance) label() string {
	if address20, ok := a.addr.Address20(); ok {
		return address20.Hex()
	}

	address32, _ := a.addr.Address32()
	return address32.Hex()
}

func (a accountBalance) checkNonce(nonce uint64) error {
	have := a.nonce()

	if have < nonce {
		return fmt.Errorf("%w: address %s, tx: %d state: %d", ErrNonceTooHigh, a.label(), nonce, have)
	}

	if have > nonce {
		return fmt.Errorf("%w: address %s, tx: %d state: %d", ErrNonceTooLow, a.label(), nonce, have)
	}

	return nil
}

func (a accountBalance) setNonce(nonce uint64) {
	a.addr.Use(func(addr common.Address) {
		a.db.SetNonce(addr, nonce, tracing.NonceChangeEoACall)
	}, func(addr common.Address32) {
		a.db.SetAddress32Nonce(addr, nonce)
	})
}

func (a accountBalance) add(amount *uint256.Int, reason tracing.BalanceChangeReason) {
	if amount.IsZero() {
		return
	}

	a.addr.Use(func(addr common.Address) {
		a.db.AddBalance(addr, amount, reason)
	}, func(addr common.Address32) {
		a.db.AddAddress32Balance(addr, amount)
	})
}

func (a accountBalance) sub(amount *big.Int, reason tracing.BalanceChangeReason) error {
	if amount.Sign() == 0 {
		return nil
	}

	cost, overflow := uint256.FromBig(amount)

	if overflow {
		return fmt.Errorf("%w: address %s", ErrInsufficientFunds, a.label())
	}

	if a.balance().Cmp(cost) < 0 {
		return fmt.Errorf("%w: address %s have %v want %v", ErrInsufficientFunds, a.label(), a.balance(), cost)
	}

	a.addr.Use(func(addr common.Address) {
		a.db.SubBalance(addr, cost, reason)
	}, func(addr common.Address32) {
		a.db.SubAddress32Balance(addr, cost)
	})

	return nil
}

func (a accountBalance) canPay(msg *Message, l1Cost *big.Int, operator *uint256.Int) error {
	need := new(big.Int).SetUint64(msg.GasLimit)
	price := msg.GasFeeCap

	if price == nil || price.BitLen() == 0 {
		price = msg.GasPrice
	}

	need.Mul(need, price)

	if l1Cost != nil {
		need.Add(need, l1Cost)
	}

	if operator != nil {
		need.Add(need, operator.ToBig())
	}

	if msg.Value != nil {
		need.Add(need, msg.Value)
	}

	have := a.balance().ToBig()

	if have.Cmp(need) < 0 {
		return fmt.Errorf("%w: address %s have %v want %v", ErrInsufficientFunds, a.label(), have, need)
	}

	return nil
}
