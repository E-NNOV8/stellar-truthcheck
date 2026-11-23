package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
)

// IndexedMovementAdapter parses newline-delimited JSON (JSONL) movement exports
// produced by independent Stellar indexers (e.g. Galexie/Horizon ETL pipelines)
// into canonical ExportMovement slices for offline verification.
func ParseJSONLMovements(jsonl string) ([]ExportMovement, error) {
	var out []ExportMovement
	scanner := bufio.NewScanner(strings.NewReader(jsonl))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var m ExportMovement
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			return nil, fmt.Errorf("jsonl line %d decode error: %w", lineNum, err)
		}
		out = append(out, m)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ClassifySacEventType normalizes supported SAC event topic names (transfer, mint, burn, clawback).
func ClassifySacEventType(topic string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(topic)) {
	case "transfer":
		return "transfer", nil
	case "mint":
		return "mint", nil
	case "burn":
		return "burn", nil
	case "clawback":
		return "clawback", nil
	default:
		return "", fmt.Errorf("unsupported SAC event topic %q", topic)
	}
}
