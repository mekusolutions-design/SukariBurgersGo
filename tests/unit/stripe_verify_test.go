package unit_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/restflow/api-go/internal/payments"
)

func TestVerifyStripeSignatureValid(t *testing.T) {
	secret := "whsec_test_secret"
	body := []byte(`{"id":"evt_1","type":"payment_intent.succeeded"}`)
	ts := time.Now().Unix()
	signed := fmt.Sprintf("%d.%s", ts, string(body))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signed))
	sig := hex.EncodeToString(mac.Sum(nil))
	header := fmt.Sprintf("t=%d,v1=%s", ts, sig)
	if err := payments.VerifyStripeSignature(body, header, secret, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyStripeSignatureRejectsBad(t *testing.T) {
	err := payments.VerifyStripeSignature([]byte(`{}`), "t=1,v1=deadbeef", "secret", time.Minute)
	if err == nil {
		t.Fatal("expected error")
	}
}
