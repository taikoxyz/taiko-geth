package ethapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ethereum/go-ethereum/common/hexutil"
)

type InclusionPreconfirmationRequest struct {
	Tx        []byte `json:"tx"`
	Slot      uint64 `json:"slot"`
	Signature string `json:"signature"`
}

type InclusionPreconfirmationResponse struct {
	Request           InclusionPreconfirmationRequest `json:"request"`
	ProposerSignature string                          `json:"proposerSignature"`
}

// change(taiko)
func forwardRawTransaction(forwardURL string, input hexutil.Bytes, slot uint64, signature string) (*InclusionPreconfirmationResponse, error) {
	// Prepare the request
	request := InclusionPreconfirmationRequest{
		Tx:        input,
		Slot:      slot,      // Set the appropriate slot value
		Signature: signature, // Set the appropriate signature value (a keccak256 hash)
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	// Send the request
	req, err := http.NewRequest("POST", fmt.Sprintf("%v/%v", forwardURL, "commitments/v1/request_preconf"), bytes.NewBuffer(jsonData))
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

	// Parse the response
	var response InclusionPreconfirmationResponse
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return nil, err
	}

	return &response, nil
}
