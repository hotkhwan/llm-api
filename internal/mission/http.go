package mission

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type IdentityVerifier interface {
	Verify(context.Context, string) (string, error)
}

type HTTPHandler struct {
	service            *Service
	identity           IdentityVerifier
	allowTrustedHeader bool
}

func NewHTTPHandler(service *Service) *HTTPHandler {
	return &HTTPHandler{service: service, allowTrustedHeader: true}
}
func NewSecureHTTPHandler(service *Service, identity IdentityVerifier, allowTrustedHeader bool) *HTTPHandler {
	return &HTTPHandler{service: service, identity: identity, allowTrustedHeader: allowTrustedHeader}
}

func (h *HTTPHandler) Register(router fiber.Router) {
	router.Use(h.authenticate)
	router.Post("/missions", h.create)
	router.Get("/missions/:id", h.get)
	router.Put("/missions/:id/assets/:shot", h.upload)
	router.Put("/missions/:id/product-references/:index", h.uploadProductReference)
	router.Post("/missions/:id/draft", h.draft)
	router.Post("/missions/:id/export", h.export)
	router.Post("/missions/:id/posted", h.posted)
	router.Put("/missions/:id/outcome", h.outcome)
	router.Post("/missions/:id/visual-qc", h.visualQC)
	router.Put("/missions/:id/visual-qc/override", h.visualQCOverride)
}

func (h *HTTPHandler) visualQC(c *fiber.Ctx) error {
	result, err := h.service.EnqueueVisualQC(c.UserContext(), c.Params("id"))
	if err != nil {
		return mapServiceError(c, err)
	}
	return c.JSON(result)
}

type visualQCOverrideRequest struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func (h *HTTPHandler) visualQCOverride(c *fiber.Ctx) error {
	var request visualQCOverrideRequest
	if err := c.BodyParser(&request); err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid_json", "request must be valid JSON")
	}
	userID, _ := c.Locals("authenticatedUserID").(string)
	result, err := h.service.OverrideVisualQC(c.UserContext(), c.Params("id"), userID, request.Decision, request.Reason)
	if err != nil {
		return mapServiceError(c, err)
	}
	return c.JSON(result)
}

func (h *HTTPHandler) authenticate(c *fiber.Ctx) error {
	userID := ""
	if authorization := strings.TrimSpace(c.Get(fiber.HeaderAuthorization)); strings.HasPrefix(authorization, "Bearer ") && h.identity != nil {
		verified, err := h.identity.Verify(c.UserContext(), strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")))
		if err == nil {
			userID = strings.TrimSpace(verified)
		}
	}
	if userID == "" && h.allowTrustedHeader {
		userID = strings.TrimSpace(c.Get("X-Authenticated-User-ID"))
	}
	if userID == "" {
		return apiError(c, fiber.StatusUnauthorized, "authentication_required", "authenticated user identity is required")
	}
	c.Locals("authenticatedUserID", userID)
	missionID := missionIDFromPath(c.Path())
	if missionID != "" {
		value, err := h.service.Get(c.UserContext(), missionID)
		if err != nil {
			return mapServiceError(c, err)
		}
		if value.UserID != userID {
			// Do not reveal the existence of another user's mission.
			return apiError(c, fiber.StatusNotFound, "mission_not_found", "mission was not found")
		}
	}
	return c.Next()
}

func missionIDFromPath(value string) string {
	marker := "/missions/"
	position := strings.Index(value, marker)
	if position < 0 {
		return ""
	}
	remainder := strings.TrimPrefix(value[position:], marker)
	if separator := strings.IndexByte(remainder, '/'); separator >= 0 {
		remainder = remainder[:separator]
	}
	return strings.TrimSpace(remainder)
}

func (h *HTTPHandler) uploadProductReference(c *fiber.Ctx) error {
	index, err := strconv.Atoi(c.Params("index"))
	if err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid_product_reference", "index must be between 1 and 5")
	}
	contentType := strings.TrimSpace(strings.Split(c.Get(fiber.HeaderContentType), ";")[0])
	result, err := h.service.UploadProductReference(c.UserContext(), c.Params("id"), index, contentType, c.Body())
	if err != nil {
		return mapServiceError(c, err)
	}
	return c.JSON(result)
}

type createRequest struct {
	Product              Product `json:"product"`
	ConsentAccepted      bool    `json:"consentAccepted"`
	PrivacyNoticeVersion string  `json:"privacyNoticeVersion"`
	Locale               Locale  `json:"locale"`
}

func (h *HTTPHandler) create(c *fiber.Ctx) error {
	var request createRequest
	if err := c.BodyParser(&request); err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid_json", "request must be valid JSON")
	}
	userID, _ := c.Locals("authenticatedUserID").(string)
	if !request.ConsentAccepted {
		return apiError(c, fiber.StatusBadRequest, "consent_required", "privacy notice consent is required")
	}
	result, err := h.service.CreateWithConsentAndLocale(c.UserContext(), userID, request.Product, request.PrivacyNoticeVersion, request.Locale)
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

type outcomeRequest struct {
	Views  int64 `json:"views"`
	Clicks int64 `json:"clicks"`
	Sales  int64 `json:"sales"`
}

func (h *HTTPHandler) outcome(c *fiber.Ctx) error {
	var request outcomeRequest
	if err := c.BodyParser(&request); err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid_json", "request must be valid JSON")
	}
	result, err := h.service.RecordOutcome(c.UserContext(), c.Params("id"), request.Views, request.Clicks, request.Sales)
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
