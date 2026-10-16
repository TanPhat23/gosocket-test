package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// maxPriceMicro caps a single price at 1000 USD per million tokens, so a typo cannot overflow cost math.
const maxPriceMicro = 1_000_000_000

// Price is a model's price in micro-USD per million tokens (USD per million tokens times 1e6).
type Price struct{ Input, Output int64 }

// Pricing maps a model name to its operator-supplied price. There is deliberately no built-in table:
// prices change and a stale compiled-in value would silently mis-bill.
type Pricing map[string]Price

// Cost returns the cost in micro-USD of in input and out output tokens, rounded up so that cost is
// never under-estimated. ok is false for a model with no price (callers must fail closed).
func (p Pricing) Cost(model string, in, out int64) (int64, bool) {
	pr, ok := p[model]
	if !ok || in < 0 || out < 0 {
		return 0, false
	}
	return ceilDiv(in, pr.Input) + ceilDiv(out, pr.Output), true
}

// ceilDiv is ceil(tokens*priceMicro / 1e6) without overflow for tokens up to ~9e9 at the price cap.
func ceilDiv(tokens, priceMicro int64) int64 {
	return (tokens*priceMicro + 999_999) / 1_000_000
}

type decimal string

func (d *decimal) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	s = strings.Trim(s, `"`)
	if s == "" || s == "null" {
		return errors.New("empty")
	}
	*d = decimal(s)
	return nil
}

// ParsePricing reads {"models":{"<model>":{"input_usd_per_mtok":"2.50","output_usd_per_mtok":"10"}}}.
// Prices may be JSON numbers or strings, have at most 6 decimals, and must be positive. Every model in
// required must have a price (an unpriced model cannot be bounded, so the gateway refuses to start),
// and unknown fields are rejected. Errors never echo file content.
func ParsePricing(raw []byte, required []string) (Pricing, error) {
	var doc struct {
		Models map[string]struct {
			Input  decimal `json:"input_usd_per_mtok"`
			Output decimal `json:"output_usd_per_mtok"`
		} `json:"models"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, errors.New("gateway: price file is not valid JSON of the expected shape")
	}
	out := Pricing{}
	for name, m := range doc.Models {
		in, err := parseMicro(string(m.Input), maxPriceMicro)
		if err != nil {
			return nil, fmt.Errorf("gateway: price file: model %q input price: %w", name, err)
		}
		o, err := parseMicro(string(m.Output), maxPriceMicro)
		if err != nil {
			return nil, fmt.Errorf("gateway: price file: model %q output price: %w", name, err)
		}
		out[name] = Price{Input: in, Output: o}
	}
	for _, name := range required {
		if _, ok := out[name]; !ok {
			return nil, fmt.Errorf("gateway: price file has no price for allowed model %q", name)
		}
	}
	return out, nil
}

// ParseUSD parses a positive dollar amount with up to 6 decimals into micro-USD.
func ParseUSD(s string) (int64, error) { return parseMicro(s, 1<<53) }

// parseMicro parses a positive decimal with at most 6 fractional digits into millionths, at most max.
func parseMicro(s string, max int64) (int64, error) {
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" || len(frac) > 6 || strings.ContainsAny(s, "+-eE ") {
		return 0, errors.New("must be a positive decimal with at most 6 decimals")
	}
	frac += strings.Repeat("0", 6-len(frac))
	var n int64
	for _, c := range whole + frac {
		if c < '0' || c > '9' {
			return 0, errors.New("must be a positive decimal with at most 6 decimals")
		}
		n = n*10 + int64(c-'0')
		if n > max {
			return 0, errors.New("is too large")
		}
	}
	if n <= 0 {
		return 0, errors.New("must be positive")
	}
	return n, nil
}
