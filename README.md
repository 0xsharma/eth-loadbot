# Transaction Sender Bot

A Go application for sending Ethereum transactions in two modes: simple value transfers and smart contract interactions.

## Features

### Mode 1: Simple Value Transfers
- Sends simple ETH value transfers to a specified address
- Configurable transaction rate (transactions per second)
- Automatic gas price estimation with 3x multiplier

### Mode 2: Smart Contract Transactions
- Deploys a batch minting NFT contract on startup
- Sends batch mint transactions with 10 random recipient addresses per transaction
- Uses the `gosolc` library for on-the-fly Solidity compilation

## Configuration

Copy `env.example` to `.env` and configure the following variables:

```bash
# Ethereum RPC endpoint
RPC_URL=https://rpc.katana.network/<API_KEY>

# Private key (with or without 0x prefix)
PRIVATE_KEY=<PRIVATE_KEY>

# Number of transactions per second
TXS_PER_SECOND=1

# Mode: "simple" for value transfers, "contract" for smart contract transactions
MODE=simple
```

## Modes

### Simple Mode (`MODE=simple`)
- Sends simple ETH transfers to a random address
- Each transaction sends 0 ETH (just for gas consumption)

### Contract Mode (`MODE=contract`)
- Deploys a `BatchMinter` NFT contract on startup
- Sends batch mint transactions with 10 random addresses per transaction
- Each transaction mints 10 NFTs to randomly generated addresses

## Smart Contract

The `BatchMinter` contract is an ERC721 NFT contract with batch minting capabilities:

```solidity
contract BatchMinter is ERC721, Ownable {
    function batchMint(address[] calldata recipients) external onlyOwner;
}
```

## Building and Running

```bash
# Build the application
go build -o tx-sender-bot

# Run in simple mode
MODE=simple ./tx-sender-bot

# Run in contract mode
MODE=contract ./tx-sender-bot
```

## Dependencies

- `github.com/ethereum/go-ethereum` - Ethereum client
- `github.com/0xsharma/gosolc` - Solidity compiler
- `github.com/joho/godotenv` - Environment variable loading

## Contract Compilation

The application uses `gosolc` to compile the Solidity contract on-the-fly. The contract source is located in `contracts/BatchMinter.sol` and is compiled during runtime when in contract mode. 
