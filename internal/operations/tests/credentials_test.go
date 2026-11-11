package operations_test

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/palmieridev/openlocal-api/internal/operations/credentials"
)

func TestReplayEncryptionAuthenticatesBusinessScopeAndKey(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	c := credentials.New(key)
	plain := []byte(`{"management_token":"private-customer-secret"}`)
	aad := []byte("business-scope-key")
	encrypted, err := c.Seal(plain, aad)
	must(t, err)
	if bytes.Contains(encrypted, []byte("private-customer-secret")) {
		t.Fatal("unencrypted management credentials")
	}
	out, err := c.Open(encrypted, aad)
	must(t, err)
	if !bytes.Equal(out, plain) {
		t.Fatal("replay did not roundtrip")
	}
	tampered := append([]byte(nil), encrypted...)
	tampered[len(tampered)-1] ^= 1
	for _, test := range []struct {
		name      string
		body, aad []byte
	}{
		{"wrong business", encrypted, []byte("other-business-scope-key")}, {"wrong scope", encrypted, []byte("business-other-scope-key")}, {"wrong key", encrypted, []byte("business-scope-other-key")}, {"tampered", tampered, aad}, {"truncated", encrypted[:3], aad},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := c.Open(test.body, test.aad); err == nil {
				t.Fatal("unauthenticated replay decrypted")
			}
		})
	}
	a, hash, err := c.Token()
	must(t, err)
	b, _, err := c.Token()
	must(t, err)
	if len(a) != 43 || len(hash) != 32 || a == b {
		t.Fatal("invalid or reused management token")
	}
}
