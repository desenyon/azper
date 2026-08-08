package artifact

import "testing"

func TestFromBytesIsContentAddressed(t *testing.T) {
	first, err := FromBytes([]byte("same content"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	second, err := FromBytes([]byte("same content"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Digest != second.Digest {
		t.Fatalf("same content produced different addresses: %q and %q", first.ID, second.ID)
	}
	if first.Algorithm != "blake3-256" || len(first.Digest) != 64 {
		t.Fatalf("unexpected artifact address: %#v", first)
	}
}

func TestVerifyRejectsTamperedContent(t *testing.T) {
	candidate, err := FromBytes([]byte("original"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	candidate.Data = []byte("tampered")
	candidate.Size = int64(len(candidate.Data))
	if err := Verify(candidate); err == nil {
		t.Fatal("expected content-address validation failure")
	}
}
