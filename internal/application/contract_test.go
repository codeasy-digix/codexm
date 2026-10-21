package application

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestPublishedContractMatchesGo(t *testing.T) {
	b, err := os.ReadFile("../../contracts/methods-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Version int      `json:"protocolVersion"`
		Methods []Method `json:"methods"`
	}
	if err = json.Unmarshal(b, &contract); err != nil {
		t.Fatal(err)
	}
	if contract.Version != ProtocolVersion || !reflect.DeepEqual(contract.Methods, Methods()) {
		t.Fatal("regenerate contracts with go run ./cmd/webcontract")
	}
}
