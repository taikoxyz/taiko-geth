package ethapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type rpcRequest struct {
	Jsonrpc string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type rpcResponse struct {
	Jsonrpc string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Result  interface{} `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    string `json:"data"`
	} `json:"error,omitempty"`
}

// change(taiko)
func forwardRawTransaction(forwardURL string, input hexutil.Bytes) (common.Hash, error) {
	rpcReq := rpcRequest{
		Jsonrpc: "2.0",
		Method:  "eth_sendRawTransaction",
		Params:  []interface{}{input.String()},
		ID:      1,
	}

	jsonData, err := json.Marshal(rpcReq)
	if err != nil {
		return common.Hash{}, err
	}

	req, err := http.NewRequest("POST", forwardURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return common.Hash{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return common.Hash{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return common.Hash{}, fmt.Errorf("failed to forward transaction, status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return common.Hash{}, err
	}

	var rpcResp rpcResponse

	// Unmarshal the response into the struct
	if err := json.Unmarshal(body, &resp); err != nil {
		return common.Hash{}, err
	}

	// Check for errors in the response
	if rpcResp.Error != nil {
		return common.Hash{}, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return common.HexToHash(rpcResp.Result.(string)), nil
}

func forwardGetTransactionReceipt(forwardURL string, hash common.Hash) (map[string]interface{}, error) {
	rpcReq := rpcRequest{
		Jsonrpc: "2.0",
		Method:  "eth_getTransactionReceipt",
		Params:  []interface{}{hash.Hex()},
		ID:      1,
	}

	jsonData, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", forwardURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to forward transaction, status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rpcResp rpcResponse
	// Unmarshal the response into the struct
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, err
	}

	// Check for errors in the response
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result.(map[string]interface{}), nil
}
