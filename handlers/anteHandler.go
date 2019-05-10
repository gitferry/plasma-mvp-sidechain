package handlers

import (
	"bytes"
	"fmt"
	"github.com/FourthState/plasma-mvp-sidechain/msgs"
	"github.com/FourthState/plasma-mvp-sidechain/plasma"
	"github.com/FourthState/plasma-mvp-sidechain/store"
	"github.com/FourthState/plasma-mvp-sidechain/utils"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/crypto"
	"math/big"
)

// the reason for an interface is to allow the connection object
// to be cooked when testing the ante handler
type plasmaConn interface {
	GetDeposit(*big.Int, *big.Int) (plasma.Deposit, *big.Int, bool)
	HasTxBeenExited(*big.Int, plasma.Position) bool
}

func NewAnteHandler(txStore store.TxStore, depositStore store.DepositStore, blockStore store.BlockStore, client plasmaConn) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (newCtx sdk.Context, res sdk.Result, abort bool) {
		msg := tx.GetMsgs()[0] // tx should only have one msg
		switch mtype := msg.Type(); mtype {
		case "include_deposit":
			depositMsg := msg.(msgs.IncludeDepositMsg)
			return includeDepositAnteHandler(ctx, depositStore, blockStore, depositMsg, client)
		case "spend_utxo":
			spendMsg := msg.(msgs.SpendMsg)
			return spendMsgAnteHandler(ctx, spendMsg, txStore, depositStore, blockStore, client)
		default:
			return ctx, ErrInvalidTransaction(DefaultCodespace, "msg is not of type SpendMsg or IncludeDepositMsg").Result(), true
		}
	}
}

func spendMsgAnteHandler(ctx sdk.Context, spendMsg msgs.SpendMsg, txStore store.TxStore, depositStore store.DepositStore, blockStore store.BlockStore, client plasmaConn) (newCtx sdk.Context, res sdk.Result, abort bool) {
	var totalInputAmt, totalOutputAmt *big.Int

	// attempt to recover signers
	signers := spendMsg.GetSigners()
	if len(signers) == 0 {
		return ctx, ErrInvalidTransaction(DefaultCodespace, "failed recovering signers").Result(), true
	}

	if len(signers) != len(spendMsg.Inputs) {
		return ctx, ErrInvalidSignature(DefaultCodespace, "number of signers does not equal number of signatures").Result(), true
	}

	/* validate inputs */
	for i, input := range spendMsg.Inputs {
		amt, res := validateInput(ctx, input, txStore, blockStore, client)
		if !res.IsOK() {
			return ctx, res, true
		}

		// first input must cover the fee
		if i == 0 && amt.Cmp(spendMsg.Fee) < 0 {
			return ctx, ErrInsufficientFee(DefaultCodespace, "first input has an insufficient amount to pay the fee").Result(), true
		}

		totalInputAmt = totalInputAmt.Add(totalInputAmt, amt)
	}

	// input0 + input1 (totalInputAmt) == output0 + output1 + Fee (totalOutputAmt)
	for _, output := range spendMsg.Outputs {
		totalOutputAmt = new(big.Int).Add(totalOutputAmt, output.Amount)
	}
	totalOutputAmt = new(big.Int).Add(totalOutputAmt, spendMsg.Fee)

	if totalInputAmt.Cmp(totalOutputAmt) != 0 {
		return ctx, ErrInvalidTransaction(DefaultCodespace, "inputs do not equal Outputs (+ fee)").Result(), true
	}

	return ctx, sdk.Result{}, false
}

// validates the inputs against the utxo store and returns the amount of the respective input
func validateInput(ctx sdk.Context, input plasma.Input, txStore store.TxStore, blockStore store.BlockStore, client plasmaConn) (*big.Int, sdk.Result) {
	var amt *big.Int

	// inputUTXO must be owned by the signer due to the prefix so we do not need to
	// check the owner of the position
	inputUTXO, ok := txStore.GetUTXO(ctx, input.Position)
	if !ok {
		return nil, ErrInvalidInput(DefaultCodespace, "input, %v, does not exist", input.Position).Result()
	}
	if inputUTXO.Spent {
		return nil, ErrInvalidInput(DefaultCodespace, "input, %v, already spent", input.Position).Result()
	}
	if client.HasTxBeenExited(blockStore.CurrentPlasmaBlockNum(ctx), input.Position) {
		return nil, ErrExitedInput(DefaultCodespace, "input, %v, utxo has exitted", input.Position).Result()
	}

	tx, ok := txStore.GetTxWithPosition(ctx, input.Position)
	if !ok {
		return nil, sdk.ErrInternal(fmt.Sprintf("failed to retrieve the transaction that input with position %s belongs to", input.Position)).Result()
	}

	// validate confirm signatures if not a fee utxo or deposit utxo
	if input.TxIndex < 1<<16-1 && input.DepositNonce.Sign() == 0 {
		res := validateConfirmSignatures(ctx, input, tx, txStore)
		if !res.IsOK() {
			return nil, res
		}
	}

	// check if the parent utxo has exited
	for _, in := range tx.Transaction.Inputs {
		if client.HasTxBeenExited(blockStore.CurrentPlasmaBlockNum(ctx), in.Position) {
			return nil, sdk.ErrUnauthorized(fmt.Sprintf("a parent of the input has exited. Position: %v", in.Position)).Result()
		}
	}

	amt = inputUTXO.Output.Amount

	return amt, sdk.Result{}
}

// validates the input's confirm signatures
func validateConfirmSignatures(ctx sdk.Context, input plasma.Input, inputTx store.Transaction, txStore store.TxStore) sdk.Result {
	if len(input.ConfirmSignatures) > 0 && len(inputTx.Transaction.Inputs) != len(input.ConfirmSignatures) {
		return ErrInvalidTransaction(DefaultCodespace, "incorrect number of confirm signatures").Result()
	}

	confirmationHash := utils.ToEthSignedMessageHash(inputTx.ConfirmationHash[:])[:]
	for i, in := range inputTx.Transaction.Inputs {
		utxo, ok := txStore.GetUTXO(ctx, in.Position)
		if !ok {
			return sdk.ErrInternal(fmt.Sprintf("failed to retrieve input utxo with position %s", in.Position)).Result()
		}

		pubKey, _ := crypto.SigToPub(confirmationHash, input.ConfirmSignatures[i][:])
		signer := crypto.PubkeyToAddress(*pubKey)
		if !bytes.Equal(signer[:], utxo.Output.Owner[:]) {
			return sdk.ErrUnauthorized(fmt.Sprintf("confirm signature not signed by the correct address. Got: %x. Expected %x", signer, utxo.Output.Owner)).Result()
		}
	}

	return sdk.Result{}
}

func includeDepositAnteHandler(ctx sdk.Context, depositStore store.DepositStore, blockStore store.BlockStore, msg msgs.IncludeDepositMsg, client plasmaConn) (newCtx sdk.Context, res sdk.Result, abort bool) {
	if depositStore.HasDeposit(ctx, msg.DepositNonce) {
		return ctx, ErrInvalidTransaction(DefaultCodespace, "deposit, %s, already exists in store", msg.DepositNonce.String()).Result(), true
	}
	deposit, threshold, ok := client.GetDeposit(blockStore.CurrentPlasmaBlockNum(ctx), msg.DepositNonce)
	if !ok && threshold == nil {
		return ctx, ErrInvalidTransaction(DefaultCodespace, "deposit, %s, does not exist.", msg.DepositNonce.String()).Result(), true
	}
	if !ok {
		return ctx, ErrInvalidTransaction(DefaultCodespace, "deposit, %s, has not finalized yet. Please wait at least %d blocks before resubmitting", msg.DepositNonce.String(), threshold.Int64()).Result(), true
	}
	if !bytes.Equal(deposit.Owner[:], msg.Owner[:]) {
		return ctx, ErrInvalidTransaction(DefaultCodespace, "deposit, %s, does not equal the owner specified in the include-deposit Msg", msg.DepositNonce).Result(), true
	}

	depositPosition := plasma.NewPosition(big.NewInt(0), 0, 0, msg.DepositNonce)
	exited := client.HasTxBeenExited(blockStore.CurrentPlasmaBlockNum(ctx), depositPosition)
	if exited {
		return ctx, ErrInvalidTransaction(DefaultCodespace, "deposit, %s, has already exitted from rootchain", msg.DepositNonce.String()).Result(), true
	}
	return ctx, sdk.Result{}, false
}
