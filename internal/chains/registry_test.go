package chains

import "testing"

func TestResolveSupportedChains(t *testing.T) {
	for _, test := range []struct {
		name string
		id   int64
	}{{"ethereum", 1}, {"base", 8453}} {
		chain, err := Resolve(test.name)
		if err != nil || chain.ID != test.id || chain.Name != test.name {
			t.Fatalf("Resolve(%q)=%+v err=%v", test.name, chain, err)
		}
	}
	if _, err := Resolve("polygon"); err == nil {
		t.Fatal("unsupported chain must fail")
	}
}

func TestDefaultAndCanonicalName(t *testing.T) {
	chain, err := Resolve(" ETHEREUM ")
	if err != nil || chain.Name != "ethereum" {
		t.Fatalf("chain=%+v err=%v", chain, err)
	}
	chain, err = Resolve("")
	if err != nil || chain.ID != 1 {
		t.Fatalf("default=%+v err=%v", chain, err)
	}
}
