package main

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// parseExportStroops converts a decimal ETL amount to exact integer stroops.
// It rejects rounding, exponents, signs, zero, and values outside int64.
func parseExportStroops(s string) (int64, error) {
	if s == "" || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") || strings.ContainsAny(s, "eE") {
		return 0, fmt.Errorf("invalid decimal amount %q", s)
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, fmt.Errorf("invalid decimal amount %q", s)
	}
	for _, c := range parts[0] {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid decimal amount %q", s)
		}
	}
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
		if frac == "" || len(frac) > 7 {
			return 0, fmt.Errorf("amount must have 1 to 7 fractional digits: %q", s)
		}
		for _, c := range frac {
			if c < '0' || c > '9' {
				return 0, fmt.Errorf("invalid decimal amount %q", s)
			}
		}
	}
	frac += strings.Repeat("0", 7-len(frac))
	whole, ok := new(big.Int).SetString(parts[0], 10)
	if !ok {
		return 0, fmt.Errorf("invalid decimal amount %q", s)
	}
	whole.Mul(whole, big.NewInt(10_000_000))
	if frac != "" {
		fraction, ok := new(big.Int).SetString(frac, 10)
		if !ok {
			return 0, fmt.Errorf("invalid decimal amount %q", s)
		}
		whole.Add(whole, fraction)
	}
	if !whole.IsInt64() || whole.Sign() <= 0 {
		return 0, fmt.Errorf("amount is zero or exceeds supported stroop range: %q", s)
	}
	return whole.Int64(), nil
}

func canonicalStroops(raw string) (int64, error) {
	v, ok := new(big.Int).SetString(raw, 10)
	if !ok || !v.IsInt64() || v.Sign() <= 0 {
		return 0, fmt.Errorf("unsupported canonical raw amount %q", raw)
	}
	return v.Int64(), nil
}

func makeMovementKey(m ExportMovement) (movementKey, error) {
	if m.Type != "transfer" {
		return movementKey{}, fmt.Errorf("unsupported movement type %q", m.Type)
	}
	if m.From == "" || m.To == "" {
		return movementKey{}, errors.New("transfer is missing source or destination")
	}
	k := movementKey{assetType: m.AssetType, assetCode: m.AssetCode, issuer: m.AssetIssuer, from: m.From, to: m.To}
	if m.AssetType == "native" {
		if m.AssetCode != "" || m.AssetIssuer != "" {
			return movementKey{}, errors.New("native transfer unexpectedly has credit-asset identity")
		}
	} else if m.AssetType == "credit_alphanum4" || m.AssetType == "credit_alphanum12" {
		if m.AssetCode == "" || m.AssetIssuer == "" {
			return movementKey{}, errors.New("credit transfer is missing asset code or issuer")
		}
	} else {
		return movementKey{}, fmt.Errorf("unsupported asset type %q", m.AssetType)
	}
	amount, err := parseExportStroops(m.Amount)
	if err != nil {
		return movementKey{}, err
	}
	k.stroops = amount
	return k, nil
}

func formatStroops(amount int64) string {
	return fmt.Sprintf("%d.%07d", amount/10_000_000, amount%10_000_000)
}
