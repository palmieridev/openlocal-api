package webhooks

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestVerifierAcceptsValidSvixSignature(t *testing.T) {
	secretBytes := []byte("test-signing-secret")
	secret := "whsec_" + base64.StdEncoding.EncodeToString(secretBytes)
	body := []byte(`{"type":"user.created","data":{"id":"user_123"}}`)
	id := "msg_123"
	timestamp := "1700000000"

	verifier, err := NewVerifier(secret)
	if err != nil {
		t.Fatal(err)
	}
	verifier.now = func() time.Time { return time.Unix(1700000000, 0) }
	signature := "v1," + base64.StdEncoding.EncodeToString(sign(secretBytes, id, timestamp, body))

	if err := verifier.Verify(id, timestamp, signature, body); err != nil {
		t.Fatalf("expected signature to verify: %v", err)
	}
}

func TestVerifierRejectsInvalidSignature(t *testing.T) {
	secret := "whsec_" + base64.StdEncoding.EncodeToString([]byte("test-signing-secret"))
	verifier, err := NewVerifier(secret)
	if err != nil {
		t.Fatal(err)
	}
	verifier.now = func() time.Time { return time.Unix(1700000000, 0) }

	err = verifier.Verify("msg_123", "1700000000", "v1,bm90LWEtcmVhbC1zaWduYXR1cmU=", []byte(`{}`))
	if err == nil {
		t.Fatal("expected invalid signature to fail")
	}
}

func TestVerifierRejectsOldTimestamp(t *testing.T) {
	secretBytes := []byte("test-signing-secret")
	secret := "whsec_" + base64.StdEncoding.EncodeToString(secretBytes)
	body := []byte(`{}`)
	id := "msg_123"
	timestamp := "1700000000"
	verifier, err := NewVerifier(secret)
	if err != nil {
		t.Fatal(err)
	}
	verifier.now = func() time.Time { return time.Unix(1700001000, 0) }
	signature := "v1," + base64.StdEncoding.EncodeToString(sign(secretBytes, id, timestamp, body))

	if err := verifier.Verify(id, timestamp, signature, body); err == nil {
		t.Fatal("expected old timestamp to fail")
	}
}
