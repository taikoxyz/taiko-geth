package stateless

import "testing"

func TestWitnessAddKeyDedupsAndSkipsEmpty(t *testing.T) {
	w := &Witness{}
	w.AddKey([]byte{0x01, 0x02})
	w.AddKey([]byte{0x01, 0x02}) // duplicate
	w.AddKey(nil)                // skipped
	w.AddKey([]byte{})           // skipped

	if len(w.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(w.Keys))
	}
	if _, ok := w.Keys[string([]byte{0x01, 0x02})]; !ok {
		t.Fatalf("expected key to be present")
	}
}

func TestWitnessCopyClonesKeys(t *testing.T) {
	w := &Witness{}
	w.AddKey([]byte{0xaa})
	cpy := w.Copy()
	cpy.AddKey([]byte{0xbb})

	if len(w.Keys) != 1 {
		t.Fatalf("original mutated: got %d keys", len(w.Keys))
	}
	if len(cpy.Keys) != 2 {
		t.Fatalf("copy wrong: got %d keys", len(cpy.Keys))
	}
}
