package mission

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type HTTPHandler struct{ service *Service }

func NewHTTPHandler(service *Service) *HTTPHandler { return &HTTPHandler{service: service} }

func (h *HTTPHandler) Register(router fiber.Router) {
	router.Post("/missions", h.create)
	router.Get("/missions/:id", h.get)
	router.Put("/missions/:id/assets/:shot", h.upload)
	router.Post("/missions/:id/draft", h.draft)
	router.Post("/missions/:id/export", h.export)
	router.Post("/missions/:id/posted", h.posted)
}

type createRequest struct {
	UserID  string  `json:"userId"`
	Product Product `json:"product"`
}

func (h *HTTPHandler) create(c *fiber.Ctx) error {
	var request createRequest
	if err := c.BodyParser(&request); err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid_json", "request must be valid JSON")
	}
	result, err := h.service.Create(c.UserContext(), request.UserID, request.Product)
	if err != nil {
		return mapServiceError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(result)
}

func (h *HTTPHandler) get(c *fiber.Ctx) error {
	result, err := h.service.Get(c.UserContext(), c.Params("id"))
	if err != nil {
		return mapServiceError(c, err)
	}
	return c.JSON(result)
}

func (h *HTTPHandler) upload(c *fiber.Ctx) error {
	shot, err := strconv.Atoi(c.Params("shot"))
	if err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid_shot", "shot must be 1, 2, or 3")
	}
	contentType := strings.TrimSpace(strings.Split(c.Get(fiber.HeaderContentType), ";")[0])
	result, err := h.service.Upload(c.UserContext(), c.Params("id"), shot, contentType, c.Body())
	if err != nil {
		return mapServiceError(c, err)
	}
	return c.JSON(result)
}

func (h *HTTPHandler) draft(c *fiber.Ctx) error {
	result, err := h.service.GenerateDraft(c.UserContext(), c.Params("id"))
	if err != nil {
		return mapServiceError(c, err)
	}
	return c.JSON(result)
}
func (h *HTTPHandler) export(c *fiber.Ctx) error {
	result, err := h.service.Export(c.UserContext(), c.Params("id"))
	if err != nil {
		return mapServiceError(c, err)
	}
	return c.JSON(result)
}

type postedRequest struct {
	Platform string `json:"platform"`
	PostURL  string `json:"postUrl"`
}

func (h *HTTPHandler) posted(c *fiber.Ctx) error {
	var request postedRequest
	if err := c.BodyParser(&request); err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid_json", "request must be valid JSON")
	}
	result, err := h.service.MarkPosted(c.UserContext(), c.Params("id"), request.Platform, request.PostURL)
	if err != nil {
		return mapServiceError(c, err)
	}
	return c.JSON(result)
}

func mapServiceError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return apiError(c, fiber.StatusNotFound, "mission_not_found", "mission was not found")
	case IsConflict(err):
		return apiError(c, fiber.StatusConflict, "invalid_state", "mission cannot perform this action in its current state")
	default:
		return apiError(c, fiber.StatusBadRequest, "invalid_request", err.Error())
	}
}

func apiError(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{"error": fiber.Map{"code": code, "message": message, "requestId": c.Get(requestIDHeaderForMission)}})
}

const requestIDHeaderForMission = "X-Request-ID"
