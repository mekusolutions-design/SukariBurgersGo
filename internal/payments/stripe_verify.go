package payments

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	apperrors "github.com/restflow/api-go/pkg/errors"
)

// VerifyStripeSignature validates Stripe-Signature header against raw body.
// Format: t=timestamp,v1=signature[,v1=...]
func VerifyStripeSignature(payload []byte, sigHeader, secret string, tolerance time.Duration) error {
	if secret == "" {
		return apperrors.Internal("Stripe webhook secret not configured")
	}
	if sigHeader == "" {
		return apperrors.Unauthorized("Missing Stripe-Signature header")
	}
	if tolerance <= 0 {
		tolerance = 5 * time.Minute
	}

	var timestamp int64
	var signatures []string
	for _, part := range strings.Split(sigHeader, ",") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			t, err := strconv.ParseInt(kv[1], 10, 64)
			if err != nil {
				return apperrors.Unauthorized("Invalid Stripe signature timestamp")
			}
			timestamp = t
		case "v1":
			signatures = append(signatures, kv[1])
		}
	}
	if timestamp == 0 || len(signatures) == 0 {
		return apperrors.Unauthorized("Invalid Stripe-Signature header")
	}

	now := time.Now().Unix()
	if abs64(now-timestamp) > int64(tolerance.Seconds()) {
		return apperrors.Unauthorized("Stripe signature timestamp outside tolerance")
	}

	signed := fmt.Sprintf("%d.%s", timestamp, string(payload))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signed))
	expected := hex.EncodeToString(mac.Sum(nil))

	for _, sig := range signatures {
		if hmac.Equal([]byte(expected), []byte(sig)) {
			return nil
		}
	}
	return apperrors.Unauthorized("Stripe signature mismatch")
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
