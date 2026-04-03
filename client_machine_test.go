package main

import (
	"encoding/json"
	"testing"
)

func TestMachineInfoUnmarshalAcceptsNumericDifficulty(t *testing.T) {
	payload := []byte(`{
		"id": 123,
		"name": "DevArea",
		"difficulty": 48,
		"difficultyText": "Medium"
	}`)

	var info machineInfo
	if err := json.Unmarshal(payload, &info); err != nil {
		t.Fatalf("unmarshal machine info: %v", err)
	}

	if got, want := string(info.Difficulty), "48"; got != want {
		t.Fatalf("unexpected difficulty value: got %q want %q", got, want)
	}

	machine := normalizeMachine(rawMachine{Info: info})
	if got, want := machine.Difficulty, "Medium"; got != want {
		t.Fatalf("unexpected normalized difficulty: got %q want %q", got, want)
	}
}
