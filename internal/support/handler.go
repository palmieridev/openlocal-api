package support

import (
	"errors"
	"github.com/palmieridev/openlocal-api/internal/support/feedback"
	"github.com/palmieridev/openlocal-api/internal/support/feedbackpostgres"

	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type Handler struct{ service feedback.Service }

func NewHandler(rt api.Runtime) Handler {
	return Handler{service: feedback.New(feedbackpostgres.New(rt.Q))}
}

func (h Handler) createFeedback(c *fiber.Ctx) error {
	var req FeedbackRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	id, err := h.service.Create(c.Context(), feedback.Input{DocID: req.DocID, Locale: req.Locale, Verdict: req.Verdict, Comment: req.Comment, Path: req.Path})
	if err != nil {
		var validation *feedback.ValidationError
		if errors.As(err, &validation) {
			return fiber.NewError(400, validation.Message)
		}
		if errors.Is(err, feedback.ErrUnavailable) {
			return fiber.NewError(503, err.Error())
		}
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(FeedbackCreatedResponse{ID: id.String()})
}
