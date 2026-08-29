package chains

import "testing"

func TestResolveSupportsOnlyEthereum(t *testing.T) {
	chain, err := Resolve("ethereum")
	if err != nil || chain.ID != 1 || chain.Name != "ethereum" {
		t.Fatalf("Resolve(ethereum)=%+v err=%v", chain, err)
	}
	for _, unsupported := range []string{"base", "polygon"} {
		if _, err := Resolve(unsupported); err == nil {
			t.Fatalf("Resolve(%q) must fail", unsupported)
		}
	}
	if supported := Supported(); len(supported) != 1 || supported[0].Name != "ethereum" {
		t.Fatalf("Supported()=%+v", supported)
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
