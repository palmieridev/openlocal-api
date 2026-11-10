package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

func digest(body []byte) []byte {
	var value map[string]json.RawMessage
	_ = json.Unmarshal(body, &value)
	delete(value, "challenge_token")
	sum := sha256.Sum256(jsonBytes(value))
	return sum[:]
}
func (h *Service) replay(ctx context.Context, in ReplayInput, q Repository, id uuid.UUID, scope string) (WriteResult, error) {
	key := strings.TrimSpace(in.Key)
	if !idempotencyKeyPattern.MatchString(key) {
		return WriteResult{}, &Error{Kind: InvalidInput, Message: "Idempotency-Key must be 8-128 characters using letters, numbers, dot, underscore, colon, or dash"}
	}
	r, err := q.GetRequestReplay(ctx, ReplayKey{BusinessID: id, Scope: scope, Key: key})
	if errors.Is(err, ErrNotFound) {
		return WriteResult{}, nil
	}
	if err != nil {
		return WriteResult{}, err
	}
	if !bytes.Equal(r.Digest, digest(in.Body)) {
		return WriteResult{}, fail(Conflict, "version_conflict", "idempotency key was used with different input")
	}
	plain, err := h.credentials.Open(r.Response, []byte(id.String()+scope+key))
	return WriteResult{Body: plain, Outcome: r.Outcome, Replayed: err == nil}, err
}
func (h *Service) saveReplay(ctx context.Context, in ReplayInput, q Repository, id uuid.UUID, scope string, value any, outcome WriteOutcome) error {
	key := strings.TrimSpace(in.Key)
	data, err := h.credentials.Seal(jsonBytes(value), []byte(id.String()+scope+key))
	if err != nil {
		return err
	}
	return q.SaveRequestReplay(ctx, ReplayEntry{BusinessID: id, Scope: scope, Key: key, Digest: digest(in.Body), Response: data, Outcome: outcome})
}
func (h *Service) createAccess(ctx context.Context, q Repository, id, business uuid.UUID, module string, until time.Time) (string, error) {
	token, hash, err := h.credentials.Token()
	if err != nil {
		return "", err
	}
	err = q.SaveRequestAccess(ctx, AccessCredential{PublicID: id, BusinessID: business, Module: module, TokenHash: hash, ExpiresAt: until.AddDate(0, 0, 30)})
	return token, err
}
func addHistory(ctx context.Context, q Histories, business, id uuid.UUID, module, from, to, actor string) error {
	return q.AppendHistory(ctx, RecordRef{ID: id, BusinessID: business, Module: module}, HistoryChange{FromStatus: from, ToStatus: to, Actor: actor})
}
func enqueueExpiration(ctx context.Context, q ExpirationJobs, business, id uuid.UUID, module string, at time.Time) error {
	return q.ScheduleExpiration(ctx, RecordRef{ID: id, BusinessID: business, Module: module}, at)
}
func (h *Service) resolveRecord(ctx context.Context, a Actor, id uuid.UUID, module, token string) (uuid.UUID, uuid.UUID, string, error) {
	if !a.Public && !validModule(module) {
		return uuid.Nil, id, module, bad("invalid module")
	}
	if a.UserID != uuid.Nil {
		r, err := h.store.PersonalRequest(ctx, PersonalRecordQuery{UserID: nullableID(a.UserID), ID: id, Module: module})
		return r.BusinessID, r.ID, r.Module, err
	}
	if a.Public {
		if len(token) != 43 {
			return uuid.Nil, id, module, fail(NotFound, "not_found", "request not found")
		}
		hash := sha256.Sum256([]byte(token))
		r, err := h.store.GetRequestAccess(ctx, AccessProof{PublicID: id, TokenHash: hash[:]})
		if err != nil {
			return uuid.Nil, id, module, fail(NotFound, "not_found", "request not found")
		}
		return r.BusinessID, r.PublicID, r.Module, nil
	}
	return a.BusinessID, id, module, nil
}
