package entpay

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResultEnvelopeIsCanonicalAndBounded(t *testing.T) {
	result, err := NewResult("Service completed", map[string]any{
		"second": 2,
		"first":  "value",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := canonicalResult(result)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"data":{"first":"value","second":2},"schema":"entpay-result-v1","summary":"Service completed"}`
	if string(encoded) != want {
		t.Fatalf("canonical result = %s, want %s", encoded, want)
	}
	decoded, err := decodeResult(encoded)
	if err != nil || decoded.Schema != ResultSchema || decoded.Summary != result.Summary {
		t.Fatalf("decoded result = %+v, %v", decoded, err)
	}
}

func TestResultEnvelopeRejectsMerchantSpecificOrAmbiguousShapes(t *testing.T) {
	tests := []Fulfillment{
		{},
		{Result: ResultEnvelope{Schema: "merchant-specific-v1", Summary: "Done", Data: json.RawMessage(`{}`)}},
		{Result: ResultEnvelope{Schema: ResultSchema, Summary: "Done\nMaybe", Data: json.RawMessage(`{}`)}},
		{Result: ResultEnvelope{Schema: ResultSchema, Summary: "Done", Data: json.RawMessage(`[]`)}},
	}
	for index, fulfillment := range tests {
		if _, err := validateFulfillment(fulfillment); err == nil {
			t.Fatalf("invalid fulfillment %d was accepted", index)
		}
	}
	if _, err := NewResult(strings.Repeat("x", 501), map[string]any{}); err == nil {
		t.Fatal("oversized result summary was accepted")
	}
}
