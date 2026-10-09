package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/mekusolutions-design/SukariBurgersGo/internal/approvals"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/auth"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/config"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/dashboard"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/eventstore"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/http/handlers"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/http/router"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/inventory"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/kitchen"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/menu"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/notifications"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/payments"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/pos"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/production"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/projections"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/realtime"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/receiving"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/recipe"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/repository"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/sync"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/variance"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/waste"
	"github.com/mekusolutions-design/SukariBurgersGo/pkg/logging"
)

func TestFullFlowAgainstDB(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	os.Setenv("JWT_SECRET", "integration-test-secret-not-for-prod")
	os.Setenv("APP_ENV", "test")
	os.Setenv("NODE_ENV", "test")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabaseURL = dbURL
	cfg.JWT.Secret = "integration-test-secret-not-for-prod"
	log := logging.New("test")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := repository.NewPool(ctx, cfg)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer pool.Close()

	proj := projections.NewEngine(log)
	es := eventstore.NewStore(pool, log)
	es.SetProjectionRunner(proj)
	resolve := menu.NewResolutionService(pool)
	authSvc := auth.NewService(pool, cfg)
	hub := realtime.NewHub(log, authSvc)

	h := router.New(router.Deps{
		Cfg: cfg, Log: log, AuthSvc: authSvc, Hub: hub,
		AuthH:          handlers.NewAuthHandler(authSvc),
		UsersH:         handlers.NewUsersHandler(authSvc),
		HealthH:        handlers.NewHealthHandler(pool),
		InventoryH:     handlers.NewInventoryHandler(inventory.NewService(pool)),
		ReceivedH:      handlers.NewReceivedHandler(receiving.NewService(pool, es)),
		POSH:           handlers.NewPOSHandler(pos.NewService(pool, es, resolve)),
		KitchenH:       handlers.NewKitchenHandler(kitchen.NewService(pool, es)),
		RecipeH:        handlers.NewRecipeHandler(recipe.NewService(pool)),
		WasteH:         handlers.NewWasteHandler(waste.NewService(pool, es)),
		ProductionH:    handlers.NewProductionHandler(production.NewService(pool, es, resolve)),
		PaymentH:       handlers.NewPaymentHandler(payments.NewService(pool, es, cfg, log)),
		VarianceH:      handlers.NewVarianceHandler(variance.NewService(pool, es)),
		ApprovalsH:     handlers.NewApprovalsHandler(approvals.NewService(pool, es)),
		SyncH:          handlers.NewSyncHandler(sync.NewService(pool, es)),
		DashboardH:     handlers.NewDashboardHandler(dashboard.NewService(pool)),
		NotificationsH: handlers.NewNotificationsHandler(notifications.NewService(pool, es)),
		MenuH:          handlers.NewMenuHandler(resolve),
	})

	// Health
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("ready: %d %s", rec.Code, rec.Body.String())
	}

	// Register unique user
	email := "integ_" + time.Now().Format("150405.000") + "@test.local"
	body, _ := json.Marshal(map[string]string{
		"email": email, "password": "testpass123", "name": "Integration User",
	})
	req = httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 201 && rec.Code != 200 {
		// may fail if users table constraints differ — try login path only
		t.Logf("register status %d: %s", rec.Code, rec.Body.String())
	}

	var reg map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &reg)
	token, _ := reg["access_token"].(string)
	if token == "" {
		// login if register returned existing
		body, _ = json.Marshal(map[string]string{"email": email, "password": "testpass123"})
		req = httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		_ = json.Unmarshal(rec.Body.Bytes(), &reg)
		token, _ = reg["access_token"].(string)
	}
	if token == "" {
		t.Fatal("no access token — ensure users table exists and register/login works")
	}

	req = httptest.NewRequest(http.MethodGet, "/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("me: %d %s", rec.Code, rec.Body.String())
	}

	// Receive goods
	recvBody, _ := json.Marshal(map[string]interface{}{
		"item_id": "INTEG-SKU-1", "item_name": "Integration Item", "units": "pcs",
		"quantity": 100, "unit_cost": 10.5, "category": "Test",
	})
	req = httptest.NewRequest(http.MethodPost, "/received", bytes.NewReader(recvBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "integ-recv-"+email)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("received: %d %s", rec.Code, rec.Body.String())
	}

	// Idempotent replay
	req = httptest.NewRequest(http.MethodPost, "/received", bytes.NewReader(recvBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "integ-recv-"+email)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("received replay: %d %s", rec.Code, rec.Body.String())
	}

	// Inventory list
	req = httptest.NewRequest(http.MethodGet, "/inventory", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("inventory: %d %s", rec.Code, rec.Body.String())
	}

	// Dashboard
	req = httptest.NewRequest(http.MethodGet, "/dashboard/summary", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("dashboard: %d %s", rec.Code, rec.Body.String())
	}

	t.Log("integration flow OK: health → register/login → me → received → inventory → dashboard")
}
