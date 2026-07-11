package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const signatureTolerance = 5 * time.Minute

type Verifier struct {
	secret []byte
	now    func() time.Time
}

func NewVerifier(secret string) (Verifier, error) {
	secret = strings.TrimSpace(secret)
	secret = strings.TrimPrefix(secret, "whsec_")
	if secret == "" {
		return Verifier{}, errors.New("webhook signing secret is required")
	}
	decoded, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return Verifier{}, fmt.Errorf("decode webhook signing secret: %w", err)
	}
	return Verifier{secret: decoded, now: time.Now}, nil
}

func (v Verifier) Verify(id, timestamp, signature string, body []byte) error {
	if id == "" || timestamp == "" || signature == "" {
		return errors.New("missing webhook signature headers")
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errors.New("invalid webhook timestamp")
	}
	signedAt := time.Unix(ts, 0)
	now := v.now()
	if signedAt.Before(now.Add(-signatureTolerance)) || signedAt.After(now.Add(signatureTolerance)) {
		return errors.New("webhook timestamp is outside tolerance")
	}
	expected := sign(v.secret, id, timestamp, body)
	for _, candidate := range strings.Split(signature, " ") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "v1,")
		if candidate == "" {
			continue
		}
		actual, err := base64.StdEncoding.DecodeString(candidate)
		if err != nil {
			continue
		}
		if hmac.Equal(actual, expected) {
			return nil
		}
	}
	return errors.New("webhook signature mismatch")
}

func sign(secret []byte, id, timestamp string, body []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(id))
	mac.Write([]byte("."))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return mac.Sum(nil)
}
