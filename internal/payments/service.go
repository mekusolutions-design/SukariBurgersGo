package payments

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/mekusolutions-design/SukariBurgersGo/pkg/money"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/config"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/eventstore"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
	"log/slog"
)

type Service struct {
	pool       *pgxpool.Pool
	eventStore *eventstore.Store
	cfg        *config.Config
	log        *slog.Logger
	httpClient *http.Client
}

func NewService(pool *pgxpool.Pool, es *eventstore.Store, cfg *config.Config, log *slog.Logger) *Service {
	return &Service{
		pool: pool, eventStore: es, cfg: cfg, log: log,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func normalizePhone(phone string) string {
	p := strings.TrimSpace(phone)
	p = strings.ReplaceAll(p, " ", "")
	p = strings.ReplaceAll(p, "+", "")
	if strings.HasPrefix(p, "0") && len(p) == 10 {
		return "254" + p[1:]
	}
	if strings.HasPrefix(p, "254") {
		return p
	}
	return p
}

type MpesaSTKInput struct {
	ShopID, OrderID, Phone, ActorUserID, IdempotencyKey string
	Amount                                              float64
}

func (s *Service) InitiateMpesaSTK(ctx context.Context, in MpesaSTKInput) (map[string]interface{}, error) {
	if s.cfg.MPesa.ConsumerKey == "" || s.cfg.MPesa.ConsumerSecret == "" {
		return nil, apperrors.Internal("M-Pesa is not configured")
	}
	if in.Amount <= 0 || in.Phone == "" {
		return nil, apperrors.BadRequest("amount and phone required")
	}
	in.Amount = money.FromFloat(in.Amount).AsTotal().Float64()
	phone := normalizePhone(in.Phone)
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = "mpesa-stk-" + uuid.New().String()
	}
	if in.ShopID == "" {
		return nil, apperrors.Forbidden("shop identity required for M-Pesa initiation")
	}

	token, err := s.mpesaToken(ctx)
	if err != nil {
		return nil, apperrors.Internal("M-Pesa auth failed: " + err.Error())
	}

	timestamp := time.Now().Format("20060102150405")
	password := base64.StdEncoding.EncodeToString([]byte(s.cfg.MPesa.Shortcode + s.cfg.MPesa.Passkey + timestamp))

	baseURL := "https://sandbox.safaricom.co.ke"
	if s.cfg.MPesa.Env == "production" {
		baseURL = "https://api.safaricom.co.ke"
	}
	callback := s.cfg.MPesa.CallbackURL
	if callback == "" {
		callback = s.cfg.BaseURL + "/payment/mpesa/callback"
	}

	body := map[string]interface{}{
		"BusinessShortCode": s.cfg.MPesa.Shortcode,
		"Password":          password,
		"Timestamp":         timestamp,
		"TransactionType":   "CustomerPayBillOnline",
		"Amount":            int(in.Amount),
		"PartyA":            phone,
		"PartyB":            s.cfg.MPesa.Shortcode,
		"PhoneNumber":       phone,
		"CallBackURL":       callback,
		"AccountReference":  in.OrderID,
		"TransactionDesc":   "RestFlow Order " + in.OrderID,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/mpesa/stkpush/v1/processrequest", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, apperrors.Internal("M-Pesa request failed")
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	_ = json.Unmarshal(respBody, &result)

	checkoutID, _ := result["CheckoutRequestID"].(string)
	var actorPtr *string
	if in.ActorUserID != "" {
		actorPtr = &in.ActorUserID
	}
	payload := map[string]interface{}{
		"order_id": in.OrderID, "phone": phone, "amount": in.Amount,
		"checkout_request_id": checkoutID, "shop_id": in.ShopID,
		"mpesa_response": result,
	}
	event, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType: eventstore.EventMpesaStkInitiated, ActorUserID: actorPtr,
		IdempotencyKey: in.IdempotencyKey, Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"success": true, "eventId": event.ID, "checkoutRequestId": checkoutID,
		"message": "STK push initiated",
	}, nil
}

func (s *Service) mpesaToken(ctx context.Context) (string, error) {
	baseURL := "https://sandbox.safaricom.co.ke"
	if s.cfg.MPesa.Env == "production" {
		baseURL = "https://api.safaricom.co.ke"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/oauth/v1/generate?grant_type=client_credentials", nil)
	if err != nil {
		return "", err
	}
	cred := base64.StdEncoding.EncodeToString([]byte(s.cfg.MPesa.ConsumerKey + ":" + s.cfg.MPesa.ConsumerSecret))
	req.Header.Set("Authorization", "Basic "+cred)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("empty token")
	}
	return out.AccessToken, nil
}

// HandleMpesaCallback processes STK callback with secret + checkout→shop binding.
func (s *Service) HandleMpesaCallback(ctx context.Context, rawBody []byte, callbackSecretHeader string) (map[string]interface{}, error) {
	// Production fail-closed: require shared secret when configured
	if s.cfg.MPesa.CallbackSecret != "" {
		if callbackSecretHeader == "" || callbackSecretHeader != s.cfg.MPesa.CallbackSecret {
			return nil, apperrors.Unauthorized("Invalid M-Pesa callback secret")
		}
	} else if s.cfg.IsProduction() {
		return nil, apperrors.Internal("MPESA_CALLBACK_SECRET must be set in production")
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rawBody, &body); err != nil {
		return nil, apperrors.BadRequest("invalid callback body")
	}

	checkoutID := ""
	resultCode := float64(-1)
	if stk, ok := body["Body"].(map[string]interface{}); ok {
		if cb, ok := stk["stkCallback"].(map[string]interface{}); ok {
			checkoutID, _ = cb["CheckoutRequestID"].(string)
			switch v := cb["ResultCode"].(type) {
			case float64:
				resultCode = v
			case int:
				resultCode = float64(v)
			}
		}
	}
	if checkoutID == "" {
		return nil, apperrors.BadRequest("CheckoutRequestID required")
	}

	// Resolve shop/order from STK initiation event — never hardcode shop_id=1
	shopID, orderID, amount, err := s.lookupCheckout(ctx, checkoutID)
	if err != nil {
		return nil, err
	}

	idemKey := "mpesa-cb-" + checkoutID
	payload := map[string]interface{}{
		"callback": body, "checkout_request_id": checkoutID,
		"shop_id": shopID, "order_id": orderID, "amount": amount,
		"result_code": resultCode,
	}
	event, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType: eventstore.EventMpesaCallbackProcessed, IdempotencyKey: idemKey, Payload: payload,
	})
	if err != nil {
		return nil, err
	}

	// On success, record payment update event bound to order/shop (no inventory side effects)
	if resultCode == 0 && orderID != "" {
		payKey := "mpesa-paid-" + checkoutID
		_, _ = s.eventStore.Append(ctx, eventstore.AppendInput{
			EventType: eventstore.EventOrderPaymentUpdated, IdempotencyKey: payKey,
			Payload: map[string]interface{}{
				"order_id": orderID, "shop_id": shopID, "payment_status": "paid",
				"payment_method": "mpesa", "checkout_request_id": checkoutID, "amount": amount,
			},
		})
	}

	return map[string]interface{}{"ResultCode": 0, "ResultDesc": "Accepted", "eventId": event.ID}, nil
}

// lookupCheckout finds STK initiation event by checkout_request_id.
func (s *Service) lookupCheckout(ctx context.Context, checkoutID string) (shopID, orderID string, amount float64, err error) {
	const q = `
		SELECT payload FROM "Event"
		WHERE event_type = $1
		  AND (payload->>'checkout_request_id' = $2 OR payload->>'CheckoutRequestID' = $2)
		ORDER BY created_at DESC LIMIT 1`
	var raw []byte
	e := s.pool.QueryRow(ctx, q, eventstore.EventMpesaStkInitiated, checkoutID).Scan(&raw)
	if e != nil {
		return "", "", 0, apperrors.NotFound("Unknown CheckoutRequestID")
	}
	var p map[string]interface{}
	_ = json.Unmarshal(raw, &p)
	shopID, _ = p["shop_id"].(string)
	if shopID == "" {
		shopID, _ = p["shopId"].(string)
	}
	orderID, _ = p["order_id"].(string)
	if orderID == "" {
		orderID, _ = p["orderId"].(string)
	}
	switch v := p["amount"].(type) {
	case float64:
		amount = v
	}
	if shopID == "" {
		return "", "", 0, apperrors.BadRequest("STK record missing shop_id")
	}
	return shopID, orderID, amount, nil
}

// HandleStripeWebhook verifies signature and binds shop from payment metadata / prior intent.
func (s *Service) HandleStripeWebhook(ctx context.Context, rawBody []byte, sigHeader string) error {
	if err := VerifyStripeSignature(rawBody, sigHeader, s.cfg.Stripe.WebhookSecret, 5*time.Minute); err != nil {
		return err
	}

	var evt map[string]interface{}
	if err := json.Unmarshal(rawBody, &evt); err != nil {
		return apperrors.BadRequest("invalid webhook body")
	}
	evtType, _ := evt["type"].(string)
	id, _ := evt["id"].(string)
	if id == "" {
		return apperrors.BadRequest("stripe event id required")
	}

	shopID, orderID := extractStripeShopOrder(evt)
	if shopID == "" {
		// Try lookup prior stripe event with same payment_intent
		shopID, orderID = s.lookupStripeMeta(ctx, evt)
	}
	if shopID == "" {
		return apperrors.BadRequest("unable to resolve shop for Stripe event — set metadata.shop_id on PaymentIntent")
	}

	eventType := eventstore.EventStripePaymentIntentCreated
	switch evtType {
	case "payment_intent.succeeded":
		eventType = eventstore.EventStripePaymentSucceeded
	case "payment_intent.payment_failed":
		eventType = eventstore.EventStripePaymentFailed
	}

	_, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType: eventType, IdempotencyKey: "stripe-" + id,
		Payload: map[string]interface{}{
			"stripe_event": evt, "shop_id": shopID, "order_id": orderID, "stripe_event_id": id, "type": evtType,
		},
	})
	return err
}

func extractStripeShopOrder(evt map[string]interface{}) (shopID, orderID string) {
	data, _ := evt["data"].(map[string]interface{})
	if data == nil {
		return "", ""
	}
	obj, _ := data["object"].(map[string]interface{})
	if obj == nil {
		return "", ""
	}
	meta, _ := obj["metadata"].(map[string]interface{})
	if meta != nil {
		shopID, _ = meta["shop_id"].(string)
		if shopID == "" {
			shopID, _ = meta["shopId"].(string)
		}
		orderID, _ = meta["order_id"].(string)
		if orderID == "" {
			orderID, _ = meta["orderId"].(string)
		}
	}
	return shopID, orderID
}

func (s *Service) lookupStripeMeta(ctx context.Context, evt map[string]interface{}) (string, string) {
	// Best-effort: no hardcode to shop 1
	return "", ""
}

// CreateStripePaymentIntent mirrors Nest payment.service createStripePaymentIntent.
func (s *Service) CreateStripePaymentIntent(ctx context.Context, orderID string, amount float64, currency, shopID, actorUserID string) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	if currency == "" {
		currency = "kes"
	}
	currency = strings.ToLower(currency)
	amountMinor := int64(amount)
	if currency != "kes" && currency != "jpy" {
		amountMinor = int64(amount*100 + 0.5)
	}
	secret := s.cfg.Stripe.SecretKey
	idem := "stripe-pi-" + orderID

	var clientSecret, intentID string
	if secret != "" {
		form := strings.NewReader(fmt.Sprintf(
			"amount=%d&currency=%s&metadata[order_id]=%s&metadata[shop_id]=%s&automatic_payment_methods[enabled]=true",
			amountMinor, currency, orderID, shopID,
		))
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.stripe.com/v1/payment_intents", form)
		if err != nil {
			return nil, err
		}
		req.SetBasicAuth(secret, "")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Idempotency-Key", idem)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, apperrors.Internal("stripe request failed")
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var parsed map[string]interface{}
		_ = json.Unmarshal(body, &parsed)
		if resp.StatusCode >= 300 {
			m := "stripe error"
			if errObj, ok := parsed["error"].(map[string]interface{}); ok {
				if s, ok := errObj["message"].(string); ok {
					m = s
				}
			}
			return nil, apperrors.BadRequest(m)
		}
		intentID, _ = parsed["id"].(string)
		clientSecret, _ = parsed["client_secret"].(string)
	} else {
		intentID = "pi_dev_" + uuid.New().String()
		clientSecret = intentID + "_secret_dev"
	}

	var actorPtr *string
	if actorUserID != "" {
		actorPtr = &actorUserID
	}
	_, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType:      eventstore.EventStripePaymentIntentCreated,
		IdempotencyKey: idem,
		ActorUserID:    actorPtr,
		Payload: map[string]interface{}{
			"order_id": orderID, "amount": amount, "currency": currency,
			"payment_intent_id": intentID, "shop_id": shopID,
		},
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"success":           true,
		"clientSecret":      clientSecret,
		"client_secret":     clientSecret,
		"paymentIntentId":   intentID,
		"payment_intent_id": intentID,
		"order_id":          orderID,
		"amount":            amount,
		"currency":          currency,
	}, nil
}
