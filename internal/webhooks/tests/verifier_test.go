package webhooks_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"testing"
	"time"

	"github.com/palmieridev/openlocal-api/internal/webhooks"
)

// signedFixture produces provider-side Svix headers for the public verifier.
func signedFixture(key []byte, id string, at time.Time, body []byte) (string, string) {
	timestamp := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(id + "." + timestamp + "."))
	_, _ = mac.Write(body)
	return timestamp, "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestVerifierAcceptsValidSvixSignature(t *testing.T) {
	key := []byte("test-signing-secret")
	verifier, err := webhooks.NewVerifier("whsec_" + base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"type":"user.created","data":{"id":"user_123"}}`)
	timestamp, signature := signedFixture(key, "msg_fixture", time.Now(), body)
	if err := verifier.Verify("msg_fixture", timestamp, signature, body); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
}

func TestVerifierRejectsInvalidSvixRequests(t *testing.T) {
	key := []byte("test-signing-secret")
	verifier, err := webhooks.NewVerifier("whsec_" + base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"forged", "tampered body", "missing id", "missing timestamp", "missing signature", "invalid timestamp", "expired", "future"} {
		t.Run(name, func(t *testing.T) {
			id := "msg_fixture"
			body := []byte(`{"type":"user.created","data":{"id":"user_123"}}`)
			at := time.Now()
			if name == "expired" {
				at = at.Add(-time.Hour)
			}
			if name == "future" {
				at = at.Add(time.Hour)
			}
			timestamp, signature := signedFixture(key, id, at, body)
			switch name {
			case "forged":
				signature = "v1,bm90LWEtcmVhbC1zaWduYXR1cmU="
			case "tampered body":
				body = []byte(`{"type":"user.deleted","data":{"id":"user_123"}}`)
			case "missing id":
				id = ""
			case "missing timestamp":
				timestamp = ""
			case "missing signature":
				signature = ""
			case "invalid timestamp":
				timestamp = "invalid"
			}
			if err := verifier.Verify(id, timestamp, signature, body); err == nil {
				t.Fatal("invalid Svix request accepted")
			}
		})
	}
}
