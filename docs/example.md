# Using the Sidechain Example #

The following assumes you have already deployed the rootchain contract to either ganache or a testnet.
See our rootchain deployment [example](https://github.com/FourthState/plasma-mvp-sidechain/blob/develop/docs/example_rootchain_deployment.md)

Plasmacli: the command-line interface for interacting with the sidechain and rootchain. 

Plasmad: runs a sidechain full-node

## Setting up a full-node ##

Install the latest version of plasmad: 

```
cd server/plasmad/
go install
```

Run `plasmad init` to initalize a validator. cd into `~/.plasmad/config`. 
Open genesis.json and add an ethereum address to `fee_address`. 
See our example [genesis.json](https://github.com/FourthState/plasma-mvp-sidechain/blob/develop/docs/testnet-setup/example_genesis.json)

Open config.toml and add any configurations you would like to add for your validator, such as a moniker. TODO: add section on seeds

Open plasma.toml, set `is_operator` to true if you are running a validator. 
Set `ethereum_operator_privatekey` to be the unencrypted private key that will be used to submit blocks to the rootchain.
It must contain sufficient eth to pay gas costs for every submitted plasma block.
Set `ethereum_plasma_contract_address` to be the contract address of the deployed rootchain. 
Set `plasma_block_commitment_rate` to be the rate at which you want plasma blocks to be submitted to the rootchain. 
Set `ethereum_nodeurl` to be the url which contains your ethereum full node. 
Set `ethereum_finality` to be the number of ethereum blocks until a submitted header is presumed to be final.

See our example [plasma.toml](https://github.com/FourthState/plasma-mvp-sidechain/blob/develop/docs/testnet-setup/example_plasma.toml)

Run `plasmad unsafe-reset-all` followed by `plasmad start`

You should be successfully producing empty blocks

Things to keep in mind: 
- You can change `timeout_commit` in config.toml to slow down block time. 
- go install `plasmacli` and `plasmad` when updating to newer versions
- Using `plasmad unsafe-reset-all` will erase all chain history. You will need to redeploy the rootchain contract. 

## Setting up the client ##

You will need to run a full eth node to interact with the rootchain contract.
See the install [script](https://github.com/FourthState/plasma-mvp-sidechain/blob/develop/scripts/plasma_install.sh) for an example of setting up a full eth node.

Install the latest version of plasmacli:

```
cd client/plasmacli/
go install
```

cd into `~/.plasmacli/`. Open plasma.toml.
Set `ethereum_plasma_contract_address` to be the contract address of the deployed rootchain. 
Set `ethereum_nodeurl` to be the url which contains your ethereum full node. 
Set `ethereum_finality` to be the number of ethereum blocks until a submitted header is presumed to be final.

Things to keep in mind:
- plasmacli can be used without a full node, but certain features will be disabled such as interacting with the rootchain
- Using the `-h` will provide short documentation and example usage for each command 

See [keys documentation](https://github.com/FourthState/plasma-mvp-sidechain/blob/develop/docs/keys.md) for examples on how to use the keys subcommand.

See [eth documentation](https://github.com/FourthState/plasma-mvp-sidechain/blob/develop/docs/eth.md) for examples on how to use the eth subcommand.

## Spending Deposits ## 

In order to spend a deposit on the sidechain, first a user must deposit on the rootchain and then send an include-deposit transaction (after presumed finality).
A user can deposit using the eth subcommand. See this [example](https://github.com/FourthState/plasma-mvp-sidechain/blob/develop/docs/eth.md#depositing)

Sending an include-deposit transaction: 
```
plasmacli include-deposit 1 acc1
```

You can also use the --sync flag
```
plasmacli include-deposit 1 acc1 --sync
Error: broadcast_tx_commit: Response error: RPC error -32603 - Internal error: Error on broadcastTxCommit: Tx already exists in cache
```

The above error simply means that the above transaction has been sent but not yet included in a block.

If you query your account balance you should see your deposit:
```
plasmacli query balance acc1
Position: (0.0.0.1) , Amount: 1000
Total: 1000
```

**Note:** The include-deposit transaction will fail if presumed finality specified by the validator has not yet been reached.

Spending the deposit:
First argument is address being sent to, followed by amounts to send (first output, second output), followed by the account to send from. 
Position flag of inputs to be spent must be specified. 

```
 plasmacli spend 0x5475b99e01ac3bb08b24fd754e2868dbb829bc3a 1000,0 acc1 --position "(0.0.0.1)"
```

The above address being sent to corresponds to acc2

```
plasmacli query balance acc2
Position: (2.0.0.0) , Amount: 1000
Total: 1000
```

Deposits and Fees do not need a confirmation signature to be spent. 

## Spending Fees ##

Fees can be spent in the same manner as deposits

```

```
## Spending UTXOS ##

In order to spend a utxo, a user must have the confirmation signatures for it. 
Confirmation signatures can be generated using the sign command:

```
plasmacli sign acc1 --owner 0x5475b99e01ac3bb08b24fd754e2868dbb829bc3a --position "(4.0.0.0)"

UTXO
Position: (4.0.0.0)
Owner: 0x5475b99e01ac3bb08b24fd754e2868dbb829bc3a
Value: 1000
> Would you like to finalize this transaction? [Y/n]
Y
Enter passphrase:
Confirmation Signature for output with position: (4.0.0.0)
0x1ff4bc8fe08e14a480ff4744e802c8ff05a9ca5e17f1d72be7a718dc8869feaa58e07359cae70aa210a77a065d14495c79ca369cfcb23d94af921eaf16ec103701
```

Spending the utxo:

```
plasmacli spend 0xec36ead9c897b609a4ffa5820e1b2b137d454343 1000 acc2 --position "(4.0.0.0)"
Enter passphrase:
Committed at block 2891. Hash 0x33434538413842383733393530443230314242324539303539324235343646364544323535333943304236423838353943413335363733454533363245463534
```

If you cannot use the sign command to generate the confirmation signature because another user sent the transaction, use the "Input0ConfirmSigs" flag or "-0" for confirmation signatures for the first input and "Input1ConfirmSigs" flag or "-1" for confirmation signatures for the second input.

A user can also make spends using generated inputs by not specifying the positions to use.

```
plasmacli query balance acc2
Position: (2.0.0.0) , Amount: 1000
Position: (22.0.0.0) , Amount: 9000
Position: (0.0.0.7) , Amount: 10000

plasmacli spend 0xec36ead9c897b609a4ffa5820e1b2b137d454343 15000 acc2 --fee 1000
Enter passphrase:
Enter passphrase:
```

The above transaction was committed in plasma block 24.

The following transaction generated would spend positions (22.0.0.0) and (0.0.0.7), send 15000 to the specified address, use 1000 for a fee and send the remaining 3000 to acc2.

```
plasmacli query balance acc2
Position: (2.0.0.0) , Amount: 1000
Position: (24.0.1.0) , Amount: 3000
Total: 4000
```

```
plasmacli query balance acc1
Position: (10.0.0.0) , Amount: 10000
Position: (22.0.1.0) , Amount: 2000
Position: (24.0.0.0) , Amount: 15000
Position: (0.0.0.3) , Amount: 1000
Position: (0.0.0.6) , Amount: 10000
Position: (24.65535.0.0) , Amount: 1000
Total: 39000
```

