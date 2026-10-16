package router

import (
	"net/http"

	"github.com/restflow/api-go/internal/auth"
	"github.com/restflow/api-go/internal/config"
	"github.com/restflow/api-go/internal/http/handlers"
	"github.com/restflow/api-go/internal/http/middleware"
	"github.com/restflow/api-go/internal/observability"
	"github.com/restflow/api-go/internal/realtime"
	"log/slog"
)

type Deps struct {
	Cfg            *config.Config
	Log            *slog.Logger
	AuthSvc        *auth.Service
	AuthH          *handlers.AuthHandler
	UsersH         *handlers.UsersHandler
	HealthH        *handlers.HealthHandler
	InventoryH     *handlers.InventoryHandler
	ReceivedH      *handlers.ReceivedHandler
	POSH           *handlers.POSHandler
	KitchenH       *handlers.KitchenHandler
	RecipeH        *handlers.RecipeHandler
	WasteH         *handlers.WasteHandler
	ProductionH    *handlers.ProductionHandler
	PaymentH       *handlers.PaymentHandler
	VarianceH      *handlers.VarianceHandler
	ApprovalsH     *handlers.ApprovalsHandler
	SyncH          *handlers.SyncHandler
	DashboardH     *handlers.DashboardHandler
	NotificationsH *handlers.NotificationsHandler
	MenuH          *handlers.MenuHandler
	ConsumptionH   *handlers.ConsumptionHandler
	CostingH       *handlers.CostingHandler
	FinishedGoodsH *handlers.FinishedGoodsHandler
	RefillH        *handlers.RefillHandler
	TraceH         *handlers.TraceHandler
	Hub            *realtime.Hub
}

var (
	roleAdmin   = []auth.Role{auth.RoleAdmin}
	rolePOS     = []auth.Role{auth.RolePOS, auth.RoleManager, auth.RoleAdmin}
	rolePOSKit  = []auth.Role{auth.RolePOS, auth.RoleManager, auth.RoleAdmin, auth.RoleKitchen}
	roleKitchen = []auth.Role{auth.RoleKitchen, auth.RoleManager, auth.RoleAdmin}
	roleRecv    = []auth.Role{auth.RoleKitchen, auth.RoleManager, auth.RoleAdmin}
	roleInv     = []auth.Role{auth.RoleManager, auth.RoleKitchen, auth.RolePOS, auth.RoleAdmin}
	roleMgr     = []auth.Role{auth.RoleManager, auth.RoleAdmin}
	roleAll     = []auth.Role{auth.RoleManager, auth.RoleKitchen, auth.RolePOS, auth.RoleAdmin}
	roleWaste   = []auth.Role{auth.RoleManager, auth.RoleKitchen, auth.RoleAdmin}
)

func protect(authSvc *auth.Service, roles []auth.Role, h http.HandlerFunc) http.Handler {
	var chain http.Handler = h
	if len(roles) > 0 {
		chain = middleware.RequireRoles(roles...)(chain)
	}
	return middleware.Authenticate(authSvc)(chain)
}

func New(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", d.HealthH.Liveness)
	mux.HandleFunc("GET /health/live", d.HealthH.Liveness)
	mux.HandleFunc("GET /health/ready", d.HealthH.Readiness)
	if d.Cfg != nil && d.Cfg.MetricsEnabled {
		mux.Handle("GET /metrics", observability.MetricsAuth(d.Cfg.MetricsToken, observability.MetricsHandler()))
	}
	mux.Handle("POST /auth/login", middleware.AuthRateLimit(10)(http.HandlerFunc(d.AuthH.Login)))
	mux.Handle("POST /auth/register", middleware.AuthRateLimit(5)(http.HandlerFunc(d.AuthH.Register)))
	mux.HandleFunc("POST /payment/mpesa/callback", d.PaymentH.MpesaCallback)
	mux.HandleFunc("POST /payment/stripe/webhook", d.PaymentH.StripeWebhook)
	if d.Hub != nil {
		mux.Handle("GET /ws", d.Hub)
		// Nest Socket.IO engine path (socket.io-client default)
		if d.Hub != nil && d.Hub.Sio != nil {
			mux.Handle("/socket.io/", d.Hub.Sio)
			mux.Handle("/socket.io", d.Hub.Sio)
		}
		// Nest Socket.IO namespace path parity (native WebSocket, same event JSON envelope)
		for _, ns := range []string{
			"inventory", "pos", "waste", "production", "payment",
			"refill", "finished-goods", "notifications", "menu",
		} {
			// Only under /ws/{ns} — bare /inventory would shadow REST GET /inventory
			mux.Handle("GET /ws/"+ns, d.Hub.HandleNamespace(ns))
		}
	}
	a := d.AuthSvc

	mux.Handle("GET /users/me", protect(a, roleAll, d.UsersH.Me))

	// --- Web/Mobile parity aliases ---
	mux.Handle("GET /auth/me", protect(a, roleAll, d.UsersH.AuthMe))
	mux.Handle("POST /pos/orders", protect(a, rolePOS, d.POSH.CreateOrderAlias))
	mux.Handle("GET /received", protect(a, roleRecv, d.ReceivedH.List))
	mux.Handle("PUT /recipe/{recipeId}", protect(a, roleMgr, d.RecipeH.Update))
	mux.Handle("POST /recipe/{recipeId}/outputs", protect(a, roleMgr, d.RecipeH.AddOutput))
	mux.Handle("GET /notifications", protect(a, roleAll, d.NotificationsH.List))
	mux.Handle("GET /notifications/settings", protect(a, roleAll, d.NotificationsH.Settings))
	mux.Handle("PUT /notifications/settings", protect(a, roleAll, d.NotificationsH.Settings))
	mux.Handle("POST /dashboard/rebuild-sales", protect(a, roleMgr, d.DashboardH.RebuildSales))

	mux.Handle("GET /users", protect(a, roleMgr, d.UsersH.List))
	mux.Handle("GET /users/{id}", protect(a, roleMgr, d.UsersH.Get))
	mux.Handle("PATCH /users/{id}/role", protect(a, roleAdmin, d.UsersH.UpdateRole))

	mux.Handle("GET /inventory", protect(a, roleInv, d.InventoryH.List))
	mux.Handle("GET /inventory/summary", protect(a, roleInv, d.InventoryH.Summary))
	mux.Handle("GET /inventory/alerts", protect(a, roleInv, d.InventoryH.Alerts))
	mux.Handle("GET /inventory/categories", protect(a, roleInv, d.InventoryH.Categories))
	mux.Handle("GET /inventory/{itemId}/batches", protect(a, roleInv, d.InventoryH.Batches))
	mux.Handle("GET /inventory/{itemId}", protect(a, roleInv, d.InventoryH.GetByItemID))

	mux.Handle("POST /received", protect(a, roleRecv, d.ReceivedH.Receive))

	mux.Handle("POST /payment/stripe", protect(a, rolePOS, d.PaymentH.CreateStripeIntent))
	mux.Handle("POST /pos/order", protect(a, rolePOS, d.POSH.CreateOrder))
	mux.Handle("GET /pos/menu-availability", protect(a, rolePOS, d.POSH.MenuAvailability))
	mux.Handle("GET /pos/bootstrap", protect(a, rolePOSKit, d.POSH.Bootstrap))
	mux.Handle("GET /pos/orders", protect(a, rolePOS, d.POSH.ListOrders))
	mux.Handle("GET /pos/orders/{orderId}", protect(a, rolePOS, d.POSH.GetOrder))
	mux.Handle("POST /pos/orders/{orderId}/send-to-kitchen", protect(a, rolePOS, d.POSH.SendToKitchen))
	mux.Handle("POST /pos/orders/{orderId}/payment", protect(a, rolePOS, d.POSH.UpdatePayment))

	mux.Handle("GET /kitchen/active-orders", protect(a, roleKitchen, d.KitchenH.ActiveOrders))
	mux.Handle("GET /kitchen/production-history", protect(a, roleKitchen, d.KitchenH.ProductionHistory))
	mux.Handle("GET /kitchen/production-queue", protect(a, roleKitchen, d.KitchenH.ProductionQueue))
	mux.Handle("GET /kitchen/closing-stock", protect(a, roleKitchen, d.KitchenH.ClosingStock))
	mux.Handle("POST /kitchen/closing-stock", protect(a, roleKitchen, d.KitchenH.SubmitClosingStock))
	mux.Handle("POST /kitchen/orders/{orderId}/status", protect(a, roleKitchen, d.KitchenH.UpdateStatus))
	mux.Handle("GET /kitchen/batches/{itemId}", protect(a, roleKitchen, d.KitchenH.Batches))

	// Recipe: no /recipe/{lit}/{id} next to /recipe/{id}/sub — ServeMux conflict.
	// By-item lookup: RecipeH.Get falls back to item_id/recipe_code.
	mux.Handle("GET /recipe/categories", protect(a, roleAll, d.RecipeH.Categories))
	mux.Handle("GET /recipe", protect(a, roleAll, d.RecipeH.List))
	mux.Handle("POST /recipe", protect(a, roleMgr, d.RecipeH.Create))
	mux.Handle("POST /recipe/ingredient", protect(a, roleMgr, d.RecipeH.AddIngredient))
	mux.Handle("GET /recipe/{recipeId}/availability", protect(a, roleAll, d.RecipeH.Availability))
	mux.Handle("GET /recipe/{recipeId}/cost", protect(a, roleAll, d.RecipeH.Cost))
	mux.Handle("GET /recipe/{recipeId}", protect(a, roleAll, d.RecipeH.Get))

	mux.Handle("POST /waste/record", protect(a, roleWaste, d.WasteH.Record))
	mux.Handle("POST /waste", protect(a, roleWaste, d.WasteH.Record))
	mux.Handle("POST /waste/write-off-expired", protect(a, roleMgr, d.WasteH.WriteOffExpired))
	mux.Handle("GET /waste/expired-candidates", protect(a, roleWaste, d.WasteH.ExpiredCandidates))
	mux.Handle("GET /waste/analytics", protect(a, roleMgr, d.WasteH.Analytics))
	mux.Handle("GET /waste/events", protect(a, roleWaste, d.WasteH.Events))
	mux.Handle("GET /waste/events/{eventId}", protect(a, roleWaste, d.WasteH.EventByID))

	mux.Handle("POST /production/start", protect(a, roleKitchen, d.ProductionH.Start))
	mux.Handle("POST /production/finish", protect(a, roleKitchen, d.ProductionH.Finish))
	mux.Handle("POST /production/pre-prep", protect(a, roleKitchen, d.ProductionH.PrePrep))
	mux.Handle("GET /production/history", protect(a, roleKitchen, d.ProductionH.History))

	mux.Handle("POST /payment/mpesa", protect(a, rolePOS, d.PaymentH.MpesaSTK))

	mux.Handle("GET /variance/batches", protect(a, roleMgr, d.VarianceH.ListBatches))
	mux.Handle("GET /variance/batches/{batchId}", protect(a, roleMgr, d.VarianceH.GetBatch))
	mux.Handle("POST /variance/batches/{batchId}/flag", protect(a, roleMgr, d.VarianceH.Flag))
	mux.Handle("POST /variance/batches/{batchId}/reason", protect(a, roleMgr, d.VarianceH.Reason))

	mux.Handle("GET /approvals/pending", protect(a, roleMgr, d.ApprovalsH.Pending))
	mux.Handle("GET /approvals/history", protect(a, roleMgr, d.ApprovalsH.History))
	mux.Handle("POST /approvals/{approvalId}/approve", protect(a, roleMgr, d.ApprovalsH.Approve))
	mux.Handle("POST /approvals/{approvalId}/reject", protect(a, roleMgr, d.ApprovalsH.Reject))

	mux.Handle("POST /sync/bulk", protect(a, roleAll, d.SyncH.Bulk))
	mux.Handle("GET /dashboard/summary", protect(a, roleMgr, d.DashboardH.Summary))
	mux.Handle("GET /dashboard/kpis", protect(a, roleMgr, d.DashboardH.KPIs))
	mux.Handle("GET /dashboard/alerts", protect(a, []auth.Role{auth.RoleManager, auth.RoleAdmin, auth.RoleKitchen}, d.DashboardH.Alerts))
	mux.Handle("POST /notifications", protect(a, roleMgr, d.NotificationsH.Create))

	mux.Handle("GET /menu", protect(a, roleAll, d.MenuH.ListMenus))
	mux.Handle("POST /menu", protect(a, roleAdmin, d.MenuH.CreateMenu))
	mux.Handle("POST /menu/item", protect(a, roleAdmin, d.MenuH.CreateMenu))
	mux.Handle("GET /menu/categories", protect(a, roleAll, d.MenuH.ListCategories))
	mux.Handle("POST /menu/categories", protect(a, roleAdmin, d.MenuH.CreateCategory))
	mux.Handle("GET /menu/finished-goods", protect(a, roleAll, d.MenuH.ListFinishedGoods))
	mux.Handle("GET /menu/finished-good-categories", protect(a, roleAll, d.MenuH.ListFGCategories))
	mux.Handle("POST /menu/finished-good-categories/assign", protect(a, roleAdmin, d.MenuH.AssignFGCategory))
	mux.Handle("GET /menu/{menuId}/availability", protect(a, roleAll, d.MenuH.Availability))
	mux.Handle("GET /menu/catalog-items", protect(a, roleAll, d.MenuH.ListCatalog))
	mux.Handle("POST /menu/catalog-items", protect(a, roleAdmin, d.MenuH.CreateCatalog))
	mux.Handle("GET /menu/combos", protect(a, roleAll, d.MenuH.ListCombos))
	mux.Handle("POST /menu/combos", protect(a, roleAdmin, d.MenuH.CreateCombo))
	mux.Handle("POST /menu/combos/preview", protect(a, rolePOSKit, d.MenuH.PreviewCombo))

	mux.Handle("GET /consumption/summary", protect(a, roleMgr, d.ConsumptionH.Summary))
	mux.Handle("GET /consumption/products/{productId}", protect(a, roleMgr, d.ConsumptionH.Product))
	mux.Handle("GET /consumption/menu-items/{menuItemId}", protect(a, roleMgr, d.ConsumptionH.MenuItem))
	mux.Handle("GET /consumption/alerts", protect(a, roleMgr, d.ConsumptionH.Alerts))

	mux.Handle("GET /costing/sku/{itemId}", protect(a, roleAll, d.CostingH.SKU))

	mux.Handle("GET /finished-goods", protect(a, roleAll, d.FinishedGoodsH.List))
	mux.Handle("POST /finished-goods", protect(a, roleRecv, d.FinishedGoodsH.Add))
	mux.Handle("POST /finished-goods/adjust", protect(a, roleMgr, d.FinishedGoodsH.Adjust))

	mux.Handle("GET /refill/pending", protect(a, roleKitchen, d.RefillH.Pending))
	mux.Handle("GET /refill/requests/{requestId}", protect(a, roleKitchen, d.RefillH.Get))
	mux.Handle("POST /refill/request", protect(a, roleKitchen, d.RefillH.Request))
	mux.Handle("POST /refill/issue", protect(a, roleKitchen, d.RefillH.Issue))

	mux.Handle("GET /trace/diagnostics", protect(a, roleMgr, d.TraceH.Diagnostics))
	mux.Handle("GET /trace", protect(a, roleMgr, d.TraceH.List))
	mux.Handle("GET /trace/item/{itemId}", protect(a, roleMgr, d.TraceH.ByItem))

	// Prefix handler for Nest GET /recipe/item/{itemId} (cannot register next to /recipe/{id}/availability)
	byItem := protect(a, roleAll, handlers.RecipeByItemPath(d.RecipeH))

	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && len(r.URL.Path) > len("/recipe/item/") &&
			(r.URL.Path == "/recipe/item" || (len(r.URL.Path) > 12 && r.URL.Path[:13] == "/recipe/item/")) {
			byItem.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
	h = middleware.RateLimit(180)(h)
	h = middleware.MaxBody(d.Cfg.HTTP.MaxBodyBytes)(h)
	h = middleware.CORS(d.Cfg.CORS.Origins)(h)
	h = middleware.Logger(d.Log)(h)
	if d.Cfg != nil && d.Cfg.MetricsEnabled {
		h = observability.MetricsMiddleware(h)
	}
	h = middleware.RequestID(h)
	// Sentry after RequestID so tags include request_id; Recover outermost
	h = observability.SentryMiddleware(h)
	h = middleware.Recover(d.Log)(h)
	h = middleware.SecurityHeaders(h)
	return h
}
