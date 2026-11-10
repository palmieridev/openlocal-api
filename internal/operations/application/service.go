package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
	"github.com/palmieridev/openlocal-api/internal/verification"
)

var ErrNotFound = errors.New("operation resource not found")

// Failure classifies an application error; inbound adapters choose HTTP statuses.
type Failure uint8

const (
	InvalidInput Failure = iota + 1
	Forbidden
	NotFound
	Conflict
	Unavailable
)

type Error struct {
	Kind          Failure
	Code, Message string
}

func (e *Error) Error() string { return e.Message }
func fail(kind Failure, code, message string) error {
	return &Error{Kind: kind, Code: code, Message: message}
}
func bad(message string) error { return fail(InvalidInput, "invalid_request", message) }

// Actor carries identity already authenticated and authorized by the caller.
// UserID identifies a personal customer; Public denotes guest token access.
// Business resource ownership is always checked again by the use case/store.
type Actor struct {
	BusinessID, UserID uuid.UUID
	Role, Slug         string
	Public             bool
}
type ReplayInput struct {
	Key  string
	Body []byte
}
type CreateInput struct {
	Actor       Actor
	Module      string
	Appointment AppointmentInput
	Preorder    PreorderInput
	Replay      ReplayInput
}
type MutateInput struct {
	Actor                 Actor
	ID                    uuid.UUID
	Module, Token, Method string
	Editing               bool
	Transition            TransitionInput
	Edit                  EditInput
	Replay                ReplayInput
}

type WriteOutcome uint8

const (
	Created WriteOutcome = iota + 1
	Changed
)

// WriteResult preserves the exact response bytes, including decrypted retries.
type WriteResult struct {
	Body     []byte
	Outcome  WriteOutcome
	Replayed bool
}

type Transactions interface {
	Begin(context.Context, uuid.UUID) (Transaction, error)
}

// A transaction holds the business subscription lock until commit/rollback.
// Every repository and access-policy operation uses that same transaction.
type Transaction interface {
	Repository
	Commit(context.Context) error
	Rollback(context.Context) error
}
type Credentials interface {
	Seal(plain, associated []byte) ([]byte, error)
	Open(encrypted, associated []byte) ([]byte, error)
	Token() (string, []byte, error)
}
type Service struct {
	store        Repository
	transactions Transactions
	credentials  Credentials
	verifier     verification.Verifier
}

func New(store Repository, transactions Transactions, credentials Credentials, verifier verification.Verifier) *Service {
	return &Service{store: store, transactions: transactions, credentials: credentials, verifier: verifier}
}
func (h *Service) begin(ctx context.Context, id uuid.UUID) (Transaction, Repository, access.Subscription, error) {
	tx, err := h.transactions.Begin(ctx, id)
	if err != nil {
		return nil, nil, access.Subscription{}, err
	}
	sub, err := tx.GetSubscription(ctx, id)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, sub, err
	}
	return tx, tx, sub, nil
}

func capability(ctx context.Context, q access.Store, s access.Subscription, module string, at time.Time) (access.Capability, error) {
	return access.New(q).Capability(ctx, s, module, at)
}
func usage(ctx context.Context, q access.Store, s access.Subscription, module string, at time.Time) (int32, error) {
	return access.New(q).Usage(ctx, s, module, at)
}
func accessResult(ctx context.Context, q access.Store, s access.Subscription, role string, at time.Time) (access.Result, error) {
	return access.New(q).Get(ctx, s, role, at)
}
func consume(ctx context.Context, q access.Store, s access.Subscription, module string, id uuid.UUID) error {
	err := access.New(q).Consume(ctx, s, module, id, time.Now())
	if errors.Is(err, access.ErrUnavailable) {
		return fail(Forbidden, "module_unavailable", err.Error())
	}
	if errors.Is(err, access.ErrQuotaExceeded) {
		return fail(Conflict, "quota_exceeded", err.Error())
	}
	return err
}
func (h *Service) challenge(ctx context.Context, token string) error {
	err := h.verifier.Verify(ctx, verification.Input{Token: token})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, verification.ErrRequired):
		return fail(InvalidInput, "challenge_failed", "verification is required")
	case errors.Is(err, verification.ErrRejected):
		return fail(InvalidInput, "challenge_failed", "verification failed")
	default:
		return fail(Unavailable, "challenge_failed", "verification is temporarily unavailable")
	}
}
func (h *Service) business(ctx context.Context, a Actor) (Business, error) {
	var b Business
	var err error
	if a.Slug != "" {
		b, err = h.store.ModulePublicBusiness(ctx, a.Slug)
	} else {
		b, err = h.store.ModuleBusiness(ctx, a.BusinessID)
	}
	if err != nil {
		return b, err
	}
	if a.UserID != uuid.Nil && b.Status != "active" {
		return b, fail(NotFound, "not_found", "business not found")
	}
	return b, nil
}

// AuthorizeTeam applies retained team-history policy after the HTTP membership check.
func (h *Service) AuthorizeTeam(ctx context.Context, business uuid.UUID, role, module string) error {
	if err := h.store.EnsureSubscription(ctx, business); err != nil {
		return err
	}
	sub, err := h.store.GetSubscription(ctx, business)
	if err != nil {
		return err
	}
	if role != "owner" && module != "" {
		c, err := capability(ctx, h.store, sub, module, time.Now())
		if err != nil {
			return err
		}
		if !c.TeamHistory {
			return fail(Forbidden, "module_unavailable", "team access requires Business for this module")
		}
	}
	return nil
}

// ResolveIntake lets an inbound adapter reject an unavailable business before
// decoding a command. Create repeats this check before executing the use case.
func (h *Service) ResolveIntake(ctx context.Context, a Actor) (Business, error) {
	b, err := h.business(ctx, a)
	if err != nil {
		return b, err
	}
	if a.Slug != "" {
		_, err = h.store.GetSubscription(ctx, b.ID)
		if errors.Is(err, ErrNotFound) {
			return b, fail(Forbidden, "module_unavailable", "requests unavailable")
		}
	}
	return b, err
}

// ResolveRequest supports credential/ownership checks before HTTP body decoding.
// Mutation independently rechecks ownership, including on replay.
func (h *Service) ResolveRequest(ctx context.Context, in ReadInput) (RecordRef, error) {
	business, id, module, err := h.resolveRecord(ctx, in.Actor, in.ID, in.Module, in.Token)
	return RecordRef{BusinessID: business, ID: id, Module: module}, err
}

func requireConfigurationRole(role string) error {
	if role != "owner" && role != "manager" {
		return fail(Forbidden, "module_unavailable", "configuration requires a manager")
	}
	return nil
}
