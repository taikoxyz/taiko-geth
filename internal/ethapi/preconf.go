package ethapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
func forward[T any](forwardURL string, method string, params []interface{}) (T, error) {
	var zeroT T

	rpcReq := rpcRequest{
		Jsonrpc: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}

	jsonData, err := json.Marshal(rpcReq)
	if err != nil {
		return zeroT, err
	}

	req, err := http.NewRequest("POST", forwardURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return zeroT, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return zeroT, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return zeroT, fmt.Errorf("failed to forward transaction, status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return zeroT, err
	}

	var rpcResp rpcResponse

	// Unmarshal the response into the struct
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return zeroT, err
	}

	// Check for errors in the response
	if rpcResp.Error != nil {
		return zeroT, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result.(T), nil
}
