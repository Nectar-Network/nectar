package soroban

import (
	"testing"

	"github.com/stellar/go/xdr"
)

func TestScvVec_BuildsVector(t *testing.T) {
	a, err := ScvAddress("CCJUD55AG6W5HAI5LRVNKAE5WDP5XGZBUDS5WNTIVDU7O264UZZE7BRD")
	if err != nil {
		t.Fatalf("ScvAddress: %v", err)
	}
	b, err := ScvAddress("GCCYJT7KHQZ235LCND5DKNRBNGZ4DRDPP24R3M5TMWUPJRRQDRVZDEMF")
	if err != nil {
		t.Fatalf("ScvAddress: %v", err)
	}
	v := ScvVec(a, b)
	if v.Type != xdr.ScValTypeScvVec {
		t.Fatalf("expected vec type, got %v", v.Type)
	}
	if v.Vec == nil || *v.Vec == nil {
		t.Fatal("vec pointer not set")
	}
	if got := len(**v.Vec); got != 2 {
		t.Fatalf("expected 2 elements, got %d", got)
	}
	if _, err := xdr.MarshalBase64(v); err != nil {
		t.Fatalf("marshal: %v", err)
	}
}

func TestScvVec_Empty(t *testing.T) {
	v := ScvVec()
	if v.Type != xdr.ScValTypeScvVec {
		t.Fatalf("expected vec type, got %v", v.Type)
	}
	if v.Vec == nil || *v.Vec == nil {
		t.Fatal("vec pointer not set")
	}
	if got := len(**v.Vec); got != 0 {
		t.Fatalf("expected empty vec, got %d", got)
	}
}

func TestScvVoid(t *testing.T) {
	v := ScvVoid()
	if v.Type != xdr.ScValTypeScvVoid {
		t.Fatalf("expected void type, got %v", v.Type)
	}
	if _, err := xdr.MarshalBase64(v); err != nil {
		t.Fatalf("marshal: %v", err)
	}
}
