package artifacts

import "testing"

func TestDigests(t *testing.T) {
	data := []byte(`{"data":{"artifact":"sha256:abc","artifacts":["sha256:def","notadigest"]}}`)
	got := Digests(data)
	if len(got) != 2 || got[0] != "sha256:abc" || got[1] != "sha256:def" {
		t.Fatalf("unexpected digests: %#v", got)
	}
}
