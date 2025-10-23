# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

This is **taiko-geth**, a fork of go-ethereum v1.15.5 customized for the Taiko L2 rollup. All Taiko-specific changes are marked with `"CHANGE(taiko): ...."` comments, and new files follow the `taiko_*.go` naming convention.

## Build and Development Commands

### Building
```bash
# Build geth executable
make geth

# Build all packages and executables
make all

# Clean build artifacts
make clean
```

### Testing
```bash
# Run all tests
make test

# Run tests for a specific package
go test ./core/...
go test ./consensus/taiko/...
```

### Code Quality
```bash
# Run linters
make lint

# Format code
make fmt
gofmt -s -w .
```

### Development Tools
```bash
# Install required developer tools
make devtools
```

## Architecture Overview

### Core Components

1. **Consensus Engine (`consensus/taiko/`)**
   - Custom L2 rollup consensus implementation
   - Handles anchor transactions and L1 origin verification
   - Key files: `consensus.go` - main consensus logic

2. **Taiko-Specific Core Modifications**
   - `core/taiko_genesis.go` - Genesis configuration for Taiko networks
   - `core/rawdb/taiko_l1_origin.go` - L1 origin tracking
   - `core/types/taiko_transaction.go` - Transaction type extensions

3. **Mining/Block Building (`miner/`)**
   - `taiko_miner.go` - Custom miner implementation
   - `taiko_worker.go` - Block production worker
   - `taiko_payload_building.go` - Payload construction for L2 blocks

4. **API Extensions**
   - `eth/taiko_api_backend.go` - Backend API extensions
   - `ethclient/taiko_api.go` - Client-side API implementations
   - Custom JSON-RPC methods documented at https://taikoxyz.github.io/taiko-geth/taiko-geth-json-rpc/

5. **Configuration**
   - `params/taiko_config.go` - Chain configuration parameters
   - `cmd/utils/taiko_flags.go` - Command-line flags

### Key Taiko Concepts

- **Anchor Transactions**: Special transactions that link L2 blocks to L1
- **L1 Origin**: Tracking mechanism for L1 block references
- **Golden Touch Account**: Special account at `0x0000777735367b36bC9B61C50022d9D0700dB4Ec`
- **TaikoL2 Contract**: Dynamically generated address based on chain ID

### Network Configurations

Pre-configured genesis files in `core/taiko_genesis/`:
- `mainnet.json` - Taiko mainnet
- `taiko_hoodi.json` - Taiko Hoodi testnet

## Development Workflow

### Making Changes

1. All Taiko-specific changes must include `CHANGE(taiko):` comment
2. New files should follow `taiko_*.go` naming convention
3. Maintain compatibility with upstream go-ethereum where possible

### Testing Locally

```bash
# Run taiko consensus tests
go test ./consensus/taiko/...

# Run miner tests including Taiko modifications
go test ./miner/...

# Run with verbose output
go test -v ./core/rawdb/...
```

### Common Development Tasks

```bash
# Generate code from templates
go generate ./...

# Update dependencies
go mod tidy

# Verify module dependencies
go mod verify
```

## Important Notes

- This is a fork of go-ethereum v1.15.5 - check upstream documentation for base functionality
- Requires Go 1.23+ and a C compiler
- All Taiko modifications are clearly marked for easy identification
- The codebase maintains standard go-ethereum structure with Taiko additions