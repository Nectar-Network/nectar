package soroban

import (
	"encoding/json"
	"testing"
)

// A JSON-RPC "error" field arrives in many shapes across nodes/proxies; only a
// real, non-empty error should surface (an empty-string error must not fail an
// otherwise-successful call — the live "cannot unmarshal string into .error" bug).
func TestRPCErrorMessage(t *testing.T) {
	cases := []struct{ raw, want string }{
		{`""`, ""},
		{`null`, ""},
		{``, ""},
		{`{}`, ""},
		{`"  "`, ""},
		{`{"code":-32601,"message":"method not found"}`, "method not found (code -32601)"},
		{`{"message":"boom"}`, "boom"},
		{`"plain string error"`, "plain string error"},
	}
	for _, c := range cases {
		if got := rpcErrorMessage(json.RawMessage(c.raw)); got != c.want {
			t.Errorf("rpcErrorMessage(%s) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestParseContractCode(t *testing.T) {
	cases := []struct {
		in   string
		want uint32
		ok   bool
	}{
		{"HostError: Error(Contract, #4)", 4, true},
		{"submit sim: Error(Contract, #10) trapped", 10, true},
		{"Error(Contract, #5)", 5, true},
		{"value #7 standalone", 7, true},
		{"no contract code present", 0, false},
		{"tx 1a2b3c4d failed: AAAABQ== resultXdr", 0, false}, // base64/hash, no #N token
	}
	for _, c := range cases {
		got, ok := ParseContractCode(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("ParseContractCode(%q) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
