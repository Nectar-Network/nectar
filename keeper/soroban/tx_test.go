package soroban

import (
	"testing"

	"github.com/stellar/go/xdr"
)

func TestScvU64(t *testing.T) {
	cases := []uint64{0, 1, 42, 1<<32 - 1, 1<<63 - 1}
	for _, n := range cases {
		v := ScvU64(n)
		if v.Type != xdr.ScValTypeScvU64 {
			t.Fatalf("wrong type for %d", n)
		}
		if v.U64 == nil || uint64(*v.U64) != n {
			t.Fatalf("wrong value for %d", n)
		}
	}
}

func TestScvI128_Positive(t *testing.T) {
	v := ScvI128(100)
	if v.I128 == nil {
		t.Fatal("nil I128")
	}
	if int64(v.I128.Hi) != 0 {
		t.Fatalf("positive value should have hi=0, got %d", v.I128.Hi)
	}
	if uint64(v.I128.Lo) != 100 {
		t.Fatalf("expected lo=100, got %d", v.I128.Lo)
	}
}

func TestScvI128_Negative(t *testing.T) {
	v := ScvI128(-1)
	if v.I128 == nil {
		t.Fatal("nil I128")
	}
	// sign-extended: hi should be -1 (all ones)
	if int64(v.I128.Hi) != -1 {
		t.Fatalf("negative value should have hi=-1, got %d", v.I128.Hi)
	}
	if uint64(v.I128.Lo) != ^uint64(0) {
		t.Fatalf("lo should be all-ones for -1, got %d", v.I128.Lo)
	}
}

func TestScvI128_Zero(t *testing.T) {
	v := ScvI128(0)
	if int64(v.I128.Hi) != 0 || uint64(v.I128.Lo) != 0 {
		t.Fatalf("zero should have both parts zero")
	}
}

func TestScvSymbol(t *testing.T) {
	v := ScvSymbol("hello")
	if v.Type != xdr.ScValTypeScvSymbol {
		t.Fatal("wrong type")
	}
	if v.Sym == nil || string(*v.Sym) != "hello" {
		t.Fatal("wrong symbol value")
	}
}

func TestScvString(t *testing.T) {
	v := ScvString("world")
	if v.Type != xdr.ScValTypeScvString {
		t.Fatal("wrong type")
	}
	if v.Str == nil || string(*v.Str) != "world" {
		t.Fatal("wrong string value")
	}
}

func TestScvAddress_Account(t *testing.T) {
	// valid testnet public key
	addr := "GCCYJT7KHQZ235LCND5DKNRBNGZ4DRDPP24R3M5TMWUPJRRQDRVZDEMF"
	v, err := ScvAddress(addr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Type != xdr.ScValTypeScvAddress {
		t.Fatal("wrong type")
	}
	if v.Address.Type != xdr.ScAddressTypeScAddressTypeAccount {
		t.Fatalf("expected Account type, got %v", v.Address.Type)
	}
}

func TestScvAddress_Contract(t *testing.T) {
	// valid C-strkey (32 zero bytes encoded)
	addr := "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM"
	v, err := ScvAddress(addr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Address.Type != xdr.ScAddressTypeScAddressTypeContract {
		t.Fatalf("expected Contract type, got %v", v.Address.Type)
	}
}

func TestParseAddress_RoundTrip(t *testing.T) {
	original := "GCCYJT7KHQZ235LCND5DKNRBNGZ4DRDPP24R3M5TMWUPJRRQDRVZDEMF"
	v, err := ScvAddress(original)
	if err != nil {
		t.Fatalf("ScvAddress: %v", err)
	}
	got, err := ParseAddress(*v.Address)
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	if got != original {
		t.Fatalf("round-trip mismatch: got %q, want %q", got, original)
	}
}

func TestI128ToInt64_Range(t *testing.T) {
	if n, ok := I128ToInt64(ScvI128(1_000_000_000)); !ok || n != 1_000_000_000 {
		t.Fatalf("positive in-range: got (%d,%v)", n, ok)
	}
	if n, ok := I128ToInt64(ScvI128(-42)); !ok || n != -42 {
		t.Fatalf("negative in-range: got (%d,%v)", n, ok)
	}
	// Hi=1 → value >= 2^64, does not fit int64.
	big := xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{Hi: 1, Lo: 0}}
	if _, ok := I128ToInt64(big); ok {
		t.Fatal("out-of-range i128 must report !ok")
	}
	// Non-i128 types must report !ok.
	if _, ok := I128ToInt64(ScvU64(7)); ok {
		t.Fatal("non-i128 must report !ok")
	}
}
