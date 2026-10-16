package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/restflow/api-go/internal/auth"
	"github.com/restflow/api-go/internal/payments"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

type PaymentHandler struct {
	svc *payments.Service
}

func NewPaymentHandler(svc *payments.Service) *PaymentHandler {
	return &PaymentHandler{svc: svc}
}

func (h *PaymentHandler) MpesaSTK(w http.ResponseWriter, r *http.Request) {
	m, err := decodeMap(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	idem := r.Header.Get("Idempotency-Key")
	if idem == "" {
		idem = mapString(m, "idempotency_key", "idempotencyKey")
	}
	result, err := h.svc.InitiateMpesaSTK(r.Context(), payments.MpesaSTKInput{
		ShopID: auth.ShopIDFromContext(r.Context()),
		OrderID: mapString(m, "order_id", "orderId"),
		Phone: mapString(m, "phone", "phoneNumber", "phone_number"),
		Amount: mapFloat(m, "amount"),
		ActorUserID: auth.UserIDFromContext(r.Context()),
		IdempotencyKey: idem,
	})
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *PaymentHandler) MpesaCallback(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("cannot read body"))
		return
	}
	secret := r.Header.Get("X-Callback-Secret")
	if secret == "" {
		secret = r.Header.Get("X-Mpesa-Callback-Secret")
	}
	if secret == "" {
		secret = r.URL.Query().Get("secret")
	}
	result, err := h.svc.HandleMpesaCallback(r.Context(), body, secret)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *PaymentHandler) StripeWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("cannot read body"))
		return
	}
	if err := h.svc.HandleStripeWebhook(r.Context(), body, r.Header.Get("Stripe-Signature")); err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"received": "true"})
}

// CreateStripeIntent POST /payment/stripe — Nest createStripePaymentIntent parity.
func (h *PaymentHandler) CreateStripeIntent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrderID  string  `json:"order_id"`
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
		ShopID   string  `json:"shop_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	if body.OrderID == "" || body.Amount <= 0 {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("order_id and amount required"))
		return
	}
	if body.Currency == "" {
		body.Currency = "kes"
	}
	if body.ShopID == "" {
		body.ShopID = auth.ShopIDFromContext(r.Context())
	}
	out, err := h.svc.CreateStripePaymentIntent(r.Context(), body.OrderID, body.Amount, body.Currency, body.ShopID, auth.UserIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
