package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/0xsharma/gosolc"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type ContractHandler struct {
	client       *ethclient.Client
	privateKey   *ecdsa.PrivateKey
	fromAddr     common.Address
	chainID      *big.Int
	contractAddr common.Address
	contractABI  abi.ABI
	nonceMutex   sync.Mutex
	currentNonce uint64
}

func NewContractHandler(client *ethclient.Client, privateKey *ecdsa.PrivateKey, fromAddr common.Address, chainID *big.Int) *ContractHandler {
	return &ContractHandler{
		client:       client,
		privateKey:   privateKey,
		fromAddr:     fromAddr,
		chainID:      chainID,
		currentNonce: 0,
	}
}

func (ch *ContractHandler) getNextNonce() uint64 {
	ch.nonceMutex.Lock()
	defer ch.nonceMutex.Unlock()

	nonce := ch.currentNonce
	ch.currentNonce++
	return nonce
}

func (ch *ContractHandler) getGasPrice() (*big.Int, error) {
	gasPrice, err := ch.client.SuggestGasPrice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get gas price: %v", err)
	}
	// Apply 3x multiplier for higher priority
	return gasPrice.Mul(gasPrice, big.NewInt(3)), nil
}

func (ch *ContractHandler) CompileContract() ([]byte, abi.ABI, error) {
	fmt.Println("Compiling contract...")

	compiler, err := gosolc.NewDefaultCompiler("./contracts")
	if err != nil {
		return nil, abi.ABI{}, fmt.Errorf("failed to create compiler: %v", err)
	}

	compiled, err := compiler.Compile()
	if err != nil {
		return nil, abi.ABI{}, fmt.Errorf("failed to compile contract: %v", err)
	}

	// Navigate to BatchMinter contract data
	solFile, ok := compiled["BatchMinter.sol"].(map[string]interface{})
	if !ok {
		return nil, abi.ABI{}, fmt.Errorf("BatchMinter.sol not found in compilation result")
	}

	contractData, ok := solFile["BatchMinter"].(map[string]interface{})
	if !ok {
		return nil, abi.ABI{}, fmt.Errorf("BatchMinter contract not found in compilation result")
	}

	// Extract bytecode: evm.bytecode.object
	evmData, ok := contractData["evm"].(map[string]interface{})
	if !ok {
		return nil, abi.ABI{}, fmt.Errorf("evm section not found in contract data")
	}
	bytecodeData, ok := evmData["bytecode"].(map[string]interface{})
	if !ok {
		return nil, abi.ABI{}, fmt.Errorf("bytecode section not found in evm data")
	}
	bytecodeStr, ok := bytecodeData["object"].(string)
	if !ok {
		return nil, abi.ABI{}, fmt.Errorf("object bytecode not found")
	}

	// Extract ABI (already an array, not a string)
	abiData, ok := contractData["abi"].([]interface{})
	if !ok {
		return nil, abi.ABI{}, fmt.Errorf("ABI not found or in wrong format")
	}
	abiBytes, err := json.Marshal(abiData)
	if err != nil {
		return nil, abi.ABI{}, fmt.Errorf("failed to marshal ABI: %v", err)
	}

	// Strip "0x" if present
	if len(bytecodeStr) > 2 && bytecodeStr[:2] == "0x" {
		bytecodeStr = bytecodeStr[2:]
	}

	// Parse ABI
	contractABI, err := abi.JSON(strings.NewReader(string(abiBytes)))
	if err != nil {
		return nil, abi.ABI{}, fmt.Errorf("failed to parse ABI: %v", err)
	}

	// Return compiled bytecode (decoded from hex string) and parsed ABI
	bytecode, err := hex.DecodeString(bytecodeStr)
	if err != nil {
		return nil, abi.ABI{}, fmt.Errorf("failed to decode bytecode: %v", err)
	}

	return bytecode, contractABI, nil
}

func (ch *ContractHandler) DeployContract() error {
	fmt.Println("Deploying BatchMinter contract...")

	// Compile the contract
	bytecode, contractABI, err := ch.CompileContract()
	if err != nil {
		return fmt.Errorf("failed to compile contract: %v", err)
	}

	// Get current nonce
	nonce, err := ch.client.PendingNonceAt(context.Background(), ch.fromAddr)
	if err != nil {
		return fmt.Errorf("failed to get nonce: %v", err)
	}
	ch.currentNonce = nonce

	// Estimate gas for deployment
	gasLimit, err := ch.client.EstimateGas(context.Background(), ethereum.CallMsg{
		From:  ch.fromAddr,
		To:    nil, // Contract creation
		Data:  bytecode,
		Value: big.NewInt(0),
	})
	if err != nil {
		fmt.Printf("Gas estimation failed: %v\n", err)
		gasLimit = 5000000 // Use default if estimation fails
	} else {
		gasLimit = gasLimit * 12 / 10 // Add 20% buffer
	}

	// Get current gas price
	gasPrice, err := ch.getGasPrice()
	if err != nil {
		return fmt.Errorf("failed to get gas price: %v", err)
	}

	fmt.Printf("Creating deployment transaction with nonce: %d\n", nonce)
	fmt.Printf("Gas limit: %d\n", gasLimit)
	fmt.Printf("Gas price: %s\n", gasPrice.String())

	// For contract deployment, use NewContractCreation instead of NewTransaction
	// This ensures the To field is nil, which is required for contract deployment
	tx := types.NewContractCreation(
		nonce,
		big.NewInt(0), // No value
		gasLimit,      // Gas limit for deployment
		gasPrice,
		bytecode, // Contract bytecode
	)

	// Sign transaction
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(ch.chainID), ch.privateKey)
	if err != nil {
		return fmt.Errorf("failed to sign deployment transaction: %v", err)
	}

	// Send transaction
	err = ch.client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		return fmt.Errorf("failed to send deployment transaction: %v", err)
	}

	fmt.Printf("Contract deployment transaction sent: %s\n", signedTx.Hash().Hex())

	// Wait for transaction to be mined
	receipt, err := ch.waitForTransaction(signedTx.Hash())
	if err != nil {
		return fmt.Errorf("failed to wait for deployment transaction: %v", err)
	}

	if receipt.Status == 0 {
		return fmt.Errorf("contract deployment failed")
	}

	ch.contractAddr = receipt.ContractAddress
	ch.contractABI = contractABI

	fmt.Printf("Contract deployed at: %s\n", ch.contractAddr.Hex())
	return nil
}

func (ch *ContractHandler) waitForTransaction(hash common.Hash) (*types.Receipt, error) {
	for {
		receipt, err := ch.client.TransactionReceipt(context.Background(), hash)
		if err == nil && receipt != nil {
			return receipt, nil
		}
		time.Sleep(2 * time.Second)
	}
}

func (ch *ContractHandler) GenerateRandomAddresses(count int) []common.Address {
	addresses := make([]common.Address, count)
	for i := 0; i < count; i++ {
		// Generate random private key
		privateKey, err := crypto.GenerateKey()
		if err != nil {
			log.Printf("Failed to generate private key: %v", err)
			continue
		}

		// Get public key and address
		publicKey := privateKey.Public()
		publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
		if !ok {
			log.Printf("Failed to cast public key")
			continue
		}

		addresses[i] = crypto.PubkeyToAddress(*publicKeyECDSA)
	}
	return addresses
}

func (ch *ContractHandler) BatchMint(recipients []common.Address) (*types.Transaction, error) {
	// Encode the batchMint function call
	data, err := ch.contractABI.Pack("batchMint", recipients)
	if err != nil {
		return nil, fmt.Errorf("failed to pack batchMint data: %v", err)
	}

	// Estimate gas
	gasLimit, err := ch.client.EstimateGas(context.Background(), ethereum.CallMsg{
		From:  ch.fromAddr,
		To:    &ch.contractAddr,
		Data:  data,
		Value: big.NewInt(0),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to estimate gas: %v", err)
	}

	// Get current gas price
	gasPrice, err := ch.getGasPrice()
	if err != nil {
		return nil, fmt.Errorf("failed to get gas price: %v", err)
	}

	// Create transaction
	tx := types.NewTransaction(
		ch.getNextNonce(),
		ch.contractAddr,
		big.NewInt(0), // No value
		gasLimit,
		gasPrice,
		data,
	)

	// Sign transaction
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(ch.chainID), ch.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %v", err)
	}

	// Send transaction
	err = ch.client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		return nil, fmt.Errorf("failed to send transaction: %v", err)
	}

	return signedTx, nil
}

func (ch *ContractHandler) SendBatchMintTransactions(numTxs int) {
	fmt.Printf("Sending %d batch mint transactions...\n", numTxs)

	successCount := 0
	errorCount := 0

	for i := 0; i < numTxs; i++ {
		// Generate 10 random addresses for each transaction
		recipients := ch.GenerateRandomAddresses(10)

		tx, err := ch.BatchMint(recipients)
		if err != nil {
			log.Printf("Batch mint transaction %d failed: %v", i+1, err)
			errorCount++
		} else {
			fmt.Printf("Batch mint transaction %d sent: %s (minted to %d addresses)\n", i+1, tx.Hash().Hex(), len(recipients))
			successCount++
		}
	}

	fmt.Printf("Batch mint complete: %d successful, %d failed\n", successCount, errorCount)
}

func (ch *ContractHandler) StartBatchMinting(txsPerSecond int) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	fmt.Printf("Starting batch minting: %d transactions per second\n", txsPerSecond)
	fmt.Printf("From address: %s\n", ch.fromAddr.Hex())
	fmt.Printf("Contract address: %s\n", ch.contractAddr.Hex())
	fmt.Printf("Chain ID: %s\n", ch.chainID.String())
	fmt.Printf("Initial nonce: %d\n", ch.currentNonce)
	fmt.Printf("Gas price will be fetched dynamically for each transaction\n")

	for range ticker.C {
		ch.SendBatchMintTransactions(txsPerSecond)
	}
}
