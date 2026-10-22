package operations

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/entitlements"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type Handler struct {
	RT     api.Runtime
	Config config.Config
	Client *http.Client
}

func NewHandler(rt api.Runtime, cfg config.Config) *Handler {
	return &Handler{rt, cfg, &http.Client{Timeout: 10 * time.Second}}
}
func TS(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()} }
func bad(msg string) error              { return api.Fail(400, "invalid_request", msg) }
func parseID(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, bad("invalid identifier")
	}
	return id, nil
}
func nullableID(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: id != uuid.Nil} }
func validModule(m string) bool             { return m == "appointments" || m == "preorders" }

func (h *Handler) authorize(c *fiber.Ctx, configure bool) (uuid.UUID, string, error) {
	id, err := parseID(c.Params("id"))
	if err != nil {
		return id, "", err
	}
	roles := []string{"owner", "manager", "staff"}
	if configure {
		roles = []string{"owner", "manager"}
	}
	a, _, err := h.RT.RequireBusinessRole(c, id, roles...)
	if err != nil {
		return id, "", err
	}
	if err = h.RT.Q.EnsureSubscription(c.Context(), id); err != nil {
		return id, "", err
	}
	s, err := h.RT.Q.GetSubscription(c.Context(), id)
	if err != nil {
		return id, "", err
	}
	// Check the local role as well, not just the role claimed by the token.
	role, err := h.RT.Q.GetBusinessMemberRole(c.Context(), db.GetBusinessMemberRoleParams{BusinessID: id, UserID: a.UserID, ClerkOrgID: a.ClerkOrgID})
	if err != nil {
		return id, "", err
	}
	if role != "owner" && s.Tier != "business" {
		return id, "", api.Fail(403, "module_unavailable", "team access requires Business")
	}
	return id, role, nil
}
func (h *Handler) begin(ctx context.Context, id uuid.UUID) (pgx.Tx, *db.Queries, db.BusinessSubscription, error) {
	if err := h.RT.Q.EnsureSubscription(ctx, id); err != nil {
		return nil, nil, db.BusinessSubscription{}, err
	}
	tx, err := h.RT.Pool.Begin(ctx)
	if err != nil {
		return nil, nil, db.BusinessSubscription{}, err
	}
	q := h.RT.Q.WithTx(tx)
	s, err := q.LockSubscription(ctx, id)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, s, err
	}
	return tx, q, s, nil
}
func settings(ctx context.Context, q *db.Queries, id uuid.UUID, module string) (Settings, error) {
	s := defaults()
	r, err := q.GetModule(ctx, db.GetModuleParams{BusinessID: id, Module: module})
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(r.Settings, &s); err != nil {
		return s, err
	}
	s.Enabled = r.Enabled
	return s, nil
}
func requirePaid(s db.BusinessSubscription, module string) error {
	if !entitlements.Paid(s, time.Now()) || !slices.Contains(s.Modules, module) {
		return api.Fail(403, "module_unavailable", "module is not subscribed")
	}
	return nil
}
func requireIntake(ctx context.Context, q *db.Queries, s db.BusinessSubscription, module string) (Settings, error) {
	cfg, err := settings(ctx, q, s.BusinessID, module)
	if err != nil {
		return cfg, err
	}
	if err = requirePaid(s, module); err != nil {
		return cfg, err
	}
	if !cfg.Enabled || len(cfg.Hours) == 0 {
		return cfg, api.Fail(403, "module_unavailable", "requests are currently unavailable")
	}
	used, err := q.ModuleUsage(ctx, db.ModuleUsageParams{BusinessID: s.BusinessID, Module: module, PeriodStart: s.PeriodStart})
	if err != nil {
		return cfg, err
	}
	limit, _ := entitlements.Limits(s.Tier)
	if used >= limit {
		return cfg, api.Fail(409, "quota_exceeded", "requests are currently unavailable")
	}
	return cfg, nil
}
func publicUnavailable(err error) error {
	var e *api.DomainError
	if errors.As(err, &e) && (e.Code == "quota_exceeded" || e.Code == "module_unavailable") {
		return api.Fail(403, "module_unavailable", "requests are currently unavailable; contact the business")
	}
	return err
}

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

func customer(in *CustomerInput) error {
	in.CustomerName = v.Clean(in.CustomerName)
	in.CustomerPhone = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(in.CustomerPhone))
	in.Notes = strings.TrimSpace(in.Notes)
	if !strings.HasPrefix(in.CustomerPhone, "+") {
		in.CustomerPhone = "+" + in.CustomerPhone
	}
	if len([]rune(in.CustomerName)) < 2 || len([]rune(in.CustomerName)) > 120 || !phonePattern.MatchString(in.CustomerPhone) || len([]rune(in.Notes)) > 2000 {
		return bad("name, international phone, or notes is invalid")
	}
	return nil
}
func (h *Handler) challenge(ctx context.Context, token string) error {
	if h.Config.ModulesAllowTestChallenge && h.Config.AppEnv != "production" && h.Config.AppEnv != "prod" {
		return nil
	}
	if token == "" || len(token) > 2048 || h.Config.TurnstileSecret == "" {
		return api.Fail(400, "challenge_failed", "verification is required")
	}
	form := url.Values{"secret": {h.Config.TurnstileSecret}, "response": {token}}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://challenges.cloudflare.com/turnstile/v0/siteverify", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := h.Client.Do(req)
	if err != nil {
		return api.Fail(503, "challenge_failed", "verification is temporarily unavailable")
	}
	defer res.Body.Close()
	var result struct {
		Success  bool   `json:"success"`
		Hostname string `json:"hostname"`
		Action   string `json:"action"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&result) != nil || !result.Success || !slices.Contains(h.Config.TurnstileHostnames, result.Hostname) || result.Action != "request" {
		return api.Fail(400, "challenge_failed", "verification failed")
	}
	return nil
}
func (h *Handler) aead() (cipher.AEAD, error) {
	key, err := base64.StdEncoding.DecodeString(h.Config.ModulesTokenKey)
	if err != nil || len(key) != 32 {
		return nil, api.Fail(503, "module_unavailable", "request credentials are not configured")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func digest(body []byte) []byte {
	var value map[string]json.RawMessage
	_ = json.Unmarshal(body, &value)
	delete(value, "challenge_token")
	sum := sha256.Sum256(jsonBytes(value))
	return sum[:]
}
func (h *Handler) replay(c *fiber.Ctx, q *db.Queries, id uuid.UUID, scope string) (bool, error) {
	key, err := v.IdempotencyKey(c.Get("Idempotency-Key"))
	if err != nil {
		return false, err
	}
	r, err := q.GetRequestReplay(c.Context(), db.GetRequestReplayParams{BusinessID: id, Scope: scope, Key: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !bytes.Equal(r.Digest, digest(c.Body())) {
		return false, api.Fail(409, "version_conflict", "idempotency key was used with different input")
	}
	a, err := h.aead()
	if err != nil {
		return false, err
	}
	if len(r.Response) < a.NonceSize() {
		return false, errors.New("invalid encrypted replay")
	}
	plain, err := a.Open(nil, r.Response[:a.NonceSize()], r.Response[a.NonceSize():], []byte(id.String()+scope+key))
	if err != nil {
		return false, err
	}
	c.Set("Idempotent-Replayed", "true")
	c.Set("Content-Type", "application/json")
	return true, c.Status(int(r.StatusCode)).Send(plain)
}
func (h *Handler) saveReplay(c *fiber.Ctx, q *db.Queries, id uuid.UUID, scope string, value any, status int) error {
	a, err := h.aead()
	if err != nil {
		return err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	key := c.Get("Idempotency-Key")
	data := a.Seal(nonce, nonce, jsonBytes(value), []byte(id.String()+scope+key))
	return q.SaveRequestReplay(c.Context(), db.SaveRequestReplayParams{BusinessID: id, Scope: scope, Key: key, Digest: digest(c.Body()), Response: data, StatusCode: int32(status)})
}
func createAccess(ctx context.Context, q *db.Queries, id, business uuid.UUID, module string, until time.Time) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	err := q.SaveRequestAccess(ctx, db.SaveRequestAccessParams{PublicID: id, BusinessID: business, Module: module, TokenHash: hash[:], ExpiresAt: TS(until.AddDate(0, 0, 30))})
	return token, err
}
func addHistory(ctx context.Context, q *db.Queries, business, id uuid.UUID, module, from, to, actor string) error {
	p := db.AddRequestHistoryParams{BusinessID: business, FromStatus: from, ToStatus: to, Actor: actor}
	if module == "appointments" {
		p.AppointmentID = nullableID(id)
	} else {
		p.PreorderID = nullableID(id)
	}
	return q.AddRequestHistory(ctx, p)
}
func enqueueExpiration(ctx context.Context, q *db.Queries, business, id uuid.UUID, module string, at time.Time) error {
	seconds := max(0, int(time.Until(at).Seconds())+1)
	_, err := q.EnqueueJob(ctx, db.EnqueueJobParams{Payload: jsonBytes(map[string]any{"type": "expire_request", "business_id": business, "record_id": id, "module": module}), DelaySeconds: int32(seconds)})
	return err
}
func pendingExpiration(now, start time.Time, minutes int) time.Time {
	e := now.Add(time.Duration(minutes) * time.Minute)
	if start.Before(e) {
		return start
	}
	return e
}
