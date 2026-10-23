package gateway

import (
	"strings"
	"testing"
)

func TestParsePricing(t *testing.T) {
	p, err := ParsePricing([]byte(`{"models":{"a":{"input_usd_per_mtok":"2.50","output_usd_per_mtok":10},"b":{"input_usd_per_mtok":0.000001,"output_usd_per_mtok":"1000"}}}`), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if p["a"] != (Price{Input: 2_500_000, Output: 10_000_000}) || p["b"] != (Price{Input: 1, Output: 1_000_000_000}) {
		t.Errorf("unexpected prices: %+v", p)
	}
}

func TestParsePricing_RejectsBadInput(t *testing.T) {
	m := func(in, out string) string {
		return `{"models":{"a":{"input_usd_per_mtok":"` + in + `","output_usd_per_mtok":"` + out + `"}}}`
	}
	bad := map[string]string{
		"not json":      `nope`,
		"unknown field": `{"models":{"a":{"input_usd_per_mtok":"1","output_usd_per_mtok":"1","extra":1}}}`,
		"unknown top":   `{"extra":1,"models":{}}`,
		"zero":          m("0", "1"),
		"negative":      m("-1", "1"),
		"exponent":      m("1e2", "1"),
		"7 decimals":    m("0.0000001", "1"),
		"over cap":      m("1001", "1"),
		"empty":         m("", "1"),
		"letters":       m("abc", "1"),
		"missing model": `{"models":{}}`,
	}
	for name, doc := range bad {
		if _, err := ParsePricing([]byte(doc), []string{"a"}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParsePricing_ErrorsDoNotEchoFile(t *testing.T) {
	_, err := ParsePricing([]byte(`{"secret-marker": 1}`), []string{"a"})
	if err == nil || strings.Contains(err.Error(), "secret-marker") {
		t.Errorf("error must not echo file content: %v", err)
	}
}

func TestPricing_CostRoundsUpAndFailsClosed(t *testing.T) {
	p := Pricing{"m": {Input: 1_000_000, Output: 3_000_000}} // 1 and 3 micro-USD per token
	if c, ok := p.Cost("m", 10, 5); !ok || c != 25 {
		t.Errorf("cost = %d,%v want 25", c, ok)
	}
	half := Pricing{"m": {Input: 1, Output: 1}} // 1e-6 micro-USD per token: any nonzero count costs at least 1
	if c, _ := half.Cost("m", 1, 0); c != 1 {
		t.Errorf("cost must round up, got %d", c)
	}
	if c, _ := half.Cost("m", 0, 0); c != 0 {
		t.Errorf("no tokens cost nothing, got %d", c)
	}
	if _, ok := p.Cost("other", 1, 1); ok {
		t.Error("an unpriced model must not be costed")
	}
	if _, ok := p.Cost("m", -1, 1); ok {
		t.Error("negative tokens must not be costed")
	}
}

func TestParseUSD(t *testing.T) {
	for in, want := range map[string]int64{"5": 5_000_000, "5.00": 5_000_000, "0.000001": 1, "12.345678": 12_345_678} {
		if got, err := ParseUSD(in); err != nil || got != want {
			t.Errorf("ParseUSD(%q) = %d,%v want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "-1", "1e3", "1.0000001", ".", "x", "99999999999999999999"} {
		if _, err := ParseUSD(in); err == nil {
			t.Errorf("ParseUSD(%q) accepted", in)
		}
	}
}
