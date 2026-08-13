package support

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type feedbackService interface {
	Create(context.Context, FeedbackRequest) (uuid.UUID, error)
}

type Handler struct {
	service feedbackService
}

func NewHandler(rt api.Runtime) Handler {
	service := NewService(rt.Q)
	return Handler{service: service}
}

func (h Handler) RegisterPublicRoutes(public fiber.Router) {
	public.Post("/public/support/feedback", h.createFeedback)
}

func (h Handler) createFeedback(c *fiber.Ctx) error {
	var req FeedbackRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	id, err := h.service.Create(c.Context(), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(FeedbackCreatedResponse{ID: id.String()})
}
