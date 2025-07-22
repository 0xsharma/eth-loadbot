package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/joho/godotenv"
)

type TxSender struct {
	client       *ethclient.Client
	privateKey   *ecdsa.PrivateKey
	fromAddr     common.Address
	toAddr       common.Address
	chainID      *big.Int
	gasLimit     uint64
	nonceMutex   sync.Mutex
	currentNonce uint64
}

func NewTxSender(rpcURL, privateKeyHex, toAddress string) (*TxSender, error) {
	// Connect to Ethereum client
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum client: %v", err)
	}

	// Parse private key
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %v", err)
	}

	// Get public key and address
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("error casting public key to ECDSA")
	}
	fromAddr := crypto.PubkeyToAddress(*publicKeyECDSA)

	// Parse destination address
	toAddr := common.HexToAddress(toAddress)

	// Get chain ID
	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID: %v", err)
	}

	// Get current nonce
	nonce, err := client.PendingNonceAt(context.Background(), fromAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get nonce: %v", err)
	}

	return &TxSender{
		client:       client,
		privateKey:   privateKey,
		fromAddr:     fromAddr,
		toAddr:       toAddr,
		chainID:      chainID,
		gasLimit:     21_000, // Standard gas limit for simple transfers
		currentNonce: nonce,
	}, nil
}

func (ts *TxSender) getNextNonce() uint64 {
	ts.nonceMutex.Lock()
	defer ts.nonceMutex.Unlock()

	nonce := ts.currentNonce
	ts.currentNonce++
	return nonce
}

func (ts *TxSender) getGasPrice() (*big.Int, error) {
	gasPrice, err := ts.client.SuggestGasPrice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get gas price: %v", err)
	}
	// Apply 3x multiplier for higher priority
	return gasPrice.Mul(gasPrice, big.NewInt(3)), nil
}

func (ts *TxSender) generateRandomAddress() (common.Address, error) {
	// Generate random private key
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to generate private key: %v", err)
	}

	// Get public key and address
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return common.Address{}, fmt.Errorf("failed to cast public key")
	}

	return crypto.PubkeyToAddress(*publicKeyECDSA), nil
}

func (ts *TxSender) sendTransaction() (*types.Transaction, error) {
	nonce := ts.getNextNonce()

	// Get current gas price
	gasPrice, err := ts.getGasPrice()
	if err != nil {
		return nil, fmt.Errorf("failed to get gas price: %v", err)
	}

	// Generate random address for each transaction
	randomAddr, err := ts.generateRandomAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to generate random address: %v", err)
	}

	// Create transaction
	tx := types.NewTransaction(
		nonce,
		randomAddr,
		big.NewInt(0), // 0 value
		ts.gasLimit,
		gasPrice,
		nil, // no data
	)

	// Sign transaction
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(ts.chainID), ts.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %v", err)
	}

	// Send transaction
	err = ts.client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		return nil, fmt.Errorf("failed to send transaction: %v", err)
	}

	return signedTx, nil
}

func (ts *TxSender) SendBatch(numTxs int) {
	fmt.Printf("Sending %d transactions...\n", numTxs)

	successCount := 0
	errorCount := 0

	for i := 0; i < numTxs; i++ {
		tx, err := ts.sendTransaction()
		if err != nil {
			log.Printf("Transaction %d failed: %v", i+1, err)
			errorCount++
		} else {
			fmt.Printf("Transaction %d sent: %s (to: %s)\n", i+1, tx.Hash().Hex(), tx.To().Hex())
			successCount++
		}
	}

	fmt.Printf("Batch complete: %d successful, %d failed\n", successCount, errorCount)
}

func (ts *TxSender) StartSending(txsPerSecond int) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	fmt.Printf("Starting transaction sender: %d transactions per second\n", txsPerSecond)
	fmt.Printf("From address: %s\n", ts.fromAddr.Hex())
	fmt.Printf("To address: Random addresses generated for each transaction\n")
	fmt.Printf("Chain ID: %s\n", ts.chainID.String())
	fmt.Printf("Initial nonce: %d\n", ts.currentNonce)
	fmt.Printf("Gas price will be fetched dynamically for each transaction\n")

	for range ticker.C {
		ts.SendBatch(txsPerSecond)
	}
}

func main() {
	fmt.Println("Starting transaction sender...")

	// Load .env file
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Some error occurred. Err: %s", err)
	}

	// Configuration from environment variables or defaults
	rpcURL := getEnvOrDefault("RPC_URL", "")
	privateKeyHex := getEnvOrDefault("PRIVATE_KEY", "")
	txsPerSecondStr := getEnvOrDefault("TXS_PER_SECOND", "")
	mode := getEnvOrDefault("MODE", "simple") // "simple" or "contract"

	fmt.Println("RPC_URL:", rpcURL)
	fmt.Println("PRIVATE_KEY:", privateKeyHex)
	fmt.Println("TXS_PER_SECOND:", txsPerSecondStr)
	fmt.Println("MODE:", mode)

	if privateKeyHex == "" {
		log.Fatal("PRIVATE_KEY environment variable is required")
	}

	if rpcURL == "" {
		log.Fatal("RPC_URL environment variable is required")
	}

	// Remove 0x prefix if present
	if len(privateKeyHex) > 2 && privateKeyHex[:2] == "0x" {
		privateKeyHex = privateKeyHex[2:]
	}

	txsPerSecond, err := strconv.Atoi(txsPerSecondStr)
	if err != nil {
		log.Fatalf("Invalid TXS_PER_SECOND value: %v", err)
	}

	if txsPerSecond <= 0 {
		log.Fatal("TXS_PER_SECOND must be greater than 0")
	}

	// Connect to Ethereum client
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		log.Fatalf("Failed to connect to Ethereum client: %v", err)
	}

	// Parse private key
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		log.Fatalf("Failed to parse private key: %v", err)
	}

	// Get public key and address
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		log.Fatalf("Error casting public key to ECDSA")
	}
	fromAddr := crypto.PubkeyToAddress(*publicKeyECDSA)

	// Get chain ID
	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		log.Fatalf("Failed to get chain ID: %v", err)
	}

	// Gas price will be fetched dynamically for each transaction
	// No need to fetch it here anymore

	switch mode {
	case "simple":
		// Create simple transaction sender
		sender, err := NewTxSender(rpcURL, privateKeyHex, "")
		if err != nil {
			log.Fatalf("Failed to create transaction sender: %v", err)
		}

		// Start sending simple transactions
		sender.StartSending(txsPerSecond)

	case "contract":
		// Create contract handler
		contractHandler := NewContractHandler(client, privateKey, fromAddr, chainID)

		// Deploy the contract first
		err = contractHandler.DeployContract()
		if err != nil {
			log.Fatalf("Failed to deploy contract: %v", err)
		}

		// Start batch minting
		contractHandler.StartBatchMinting(txsPerSecond)

	default:
		log.Fatalf("Invalid MODE: %s. Must be 'simple' or 'contract'", mode)
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
