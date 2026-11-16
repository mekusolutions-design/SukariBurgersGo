package router

import (
	"net/http"

	"github.com/restflow/api-go/internal/auth"
	"github.com/restflow/api-go/internal/config"
	"github.com/restflow/api-go/internal/http/handlers"
	"github.com/restflow/api-go/internal/http/middleware"
	"github.com/restflow/api-go/internal/observability"
	"github.com/restflow/api-go/internal/realtime"
	"github.com/restflow/api-go/internal/tenant"
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
	TenantResolver *tenant.Resolver
	TenantStrict   bool
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

func protect(d Deps, roles []auth.Role, h http.HandlerFunc) http.Handler {
	var chain http.Handler = h
	if len(roles) > 0 {
		chain = middleware.RequireRoles(roles...)(chain)
	}
	if d.TenantResolver != nil {
		chain = middleware.AttachTenant(d.TenantResolver, d.TenantStrict)(chain)
	}
	return middleware.Authenticate(d.AuthSvc)(chain)
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


	mux.Handle("GET /users/me", protect(d, roleAll, d.UsersH.Me))

	// --- Web/Mobile parity aliases ---
	mux.Handle("GET /auth/me", protect(d, roleAll, d.UsersH.AuthMe))
	mux.Handle("POST /pos/orders", protect(d, rolePOS, d.POSH.CreateOrderAlias))
	mux.Handle("GET /received", protect(d, roleRecv, d.ReceivedH.List))
	mux.Handle("PUT /recipe/{recipeId}", protect(d, roleMgr, d.RecipeH.Update))
	mux.Handle("POST /recipe/{recipeId}/outputs", protect(d, roleMgr, d.RecipeH.AddOutput))
	mux.Handle("GET /notifications", protect(d, roleAll, d.NotificationsH.List))
	mux.Handle("GET /notifications/settings", protect(d, roleAll, d.NotificationsH.Settings))
	mux.Handle("PUT /notifications/settings", protect(d, roleAll, d.NotificationsH.Settings))
	mux.Handle("POST /dashboard/rebuild-sales", protect(d, roleMgr, d.DashboardH.RebuildSales))

	mux.Handle("GET /users", protect(d, roleMgr, d.UsersH.List))
	mux.Handle("GET /users/{id}", protect(d, roleMgr, d.UsersH.Get))
	mux.Handle("PATCH /users/{id}/role", protect(d, roleAdmin, d.UsersH.UpdateRole))

	mux.Handle("GET /inventory", protect(d, roleInv, d.InventoryH.List))
	mux.Handle("GET /inventory/summary", protect(d, roleInv, d.InventoryH.Summary))
	mux.Handle("GET /inventory/alerts", protect(d, roleInv, d.InventoryH.Alerts))
	mux.Handle("GET /inventory/categories", protect(d, roleInv, d.InventoryH.Categories))
	mux.Handle("GET /inventory/{itemId}/batches", protect(d, roleInv, d.InventoryH.Batches))
	mux.Handle("GET /inventory/{itemId}", protect(d, roleInv, d.InventoryH.GetByItemID))

	mux.Handle("POST /received", protect(d, roleRecv, d.ReceivedH.Receive))

	mux.Handle("POST /payment/stripe", protect(d, rolePOS, d.PaymentH.CreateStripeIntent))
	mux.Handle("POST /pos/order", protect(d, rolePOS, d.POSH.CreateOrder))
	mux.Handle("GET /pos/menu-availability", protect(d, rolePOS, d.POSH.MenuAvailability))
	mux.Handle("GET /pos/bootstrap", protect(d, rolePOSKit, d.POSH.Bootstrap))
	mux.Handle("GET /pos/orders", protect(d, rolePOS, d.POSH.ListOrders))
	mux.Handle("GET /pos/orders/{orderId}", protect(d, rolePOS, d.POSH.GetOrder))
	mux.Handle("POST /pos/orders/{orderId}/send-to-kitchen", protect(d, rolePOS, d.POSH.SendToKitchen))
	mux.Handle("POST /pos/orders/{orderId}/payment", protect(d, rolePOS, d.POSH.UpdatePayment))

	mux.Handle("GET /kitchen/active-orders", protect(d, roleKitchen, d.KitchenH.ActiveOrders))
	mux.Handle("GET /kitchen/production-history", protect(d, roleKitchen, d.KitchenH.ProductionHistory))
	mux.Handle("GET /kitchen/production-queue", protect(d, roleKitchen, d.KitchenH.ProductionQueue))
	mux.Handle("GET /kitchen/closing-stock", protect(d, roleKitchen, d.KitchenH.ClosingStock))
	mux.Handle("POST /kitchen/closing-stock", protect(d, roleKitchen, d.KitchenH.SubmitClosingStock))
	mux.Handle("POST /kitchen/orders/{orderId}/status", protect(d, roleKitchen, d.KitchenH.UpdateStatus))
	mux.Handle("GET /kitchen/batches/{itemId}", protect(d, roleKitchen, d.KitchenH.Batches))

	// Recipe: no /recipe/{lit}/{id} next to /recipe/{id}/sub — ServeMux conflict.
	// By-item lookup: RecipeH.Get falls back to item_id/recipe_code.
	mux.Handle("GET /recipe/categories", protect(d, roleAll, d.RecipeH.Categories))
	mux.Handle("GET /recipe", protect(d, roleAll, d.RecipeH.List))
	mux.Handle("POST /recipe", protect(d, roleMgr, d.RecipeH.Create))
	mux.Handle("POST /recipe/ingredient", protect(d, roleMgr, d.RecipeH.AddIngredient))
	mux.Handle("GET /recipe/{recipeId}/availability", protect(d, roleAll, d.RecipeH.Availability))
	mux.Handle("GET /recipe/{recipeId}/cost", protect(d, roleAll, d.RecipeH.Cost))
	mux.Handle("GET /recipe/{recipeId}", protect(d, roleAll, d.RecipeH.Get))

	mux.Handle("POST /waste/record", protect(d, roleWaste, d.WasteH.Record))
	mux.Handle("POST /waste", protect(d, roleWaste, d.WasteH.Record))
	mux.Handle("POST /waste/write-off-expired", protect(d, roleMgr, d.WasteH.WriteOffExpired))
	mux.Handle("GET /waste/expired-candidates", protect(d, roleWaste, d.WasteH.ExpiredCandidates))
	mux.Handle("GET /waste/analytics", protect(d, roleMgr, d.WasteH.Analytics))
	mux.Handle("GET /waste/events", protect(d, roleWaste, d.WasteH.Events))
	mux.Handle("GET /waste/events/{eventId}", protect(d, roleWaste, d.WasteH.EventByID))

	mux.Handle("POST /production/start", protect(d, roleKitchen, d.ProductionH.Start))
	mux.Handle("POST /production/finish", protect(d, roleKitchen, d.ProductionH.Finish))
	mux.Handle("POST /production/pre-prep", protect(d, roleKitchen, d.ProductionH.PrePrep))
	mux.Handle("GET /production/history", protect(d, roleKitchen, d.ProductionH.History))

	mux.Handle("POST /payment/mpesa", protect(d, rolePOS, d.PaymentH.MpesaSTK))

	mux.Handle("GET /variance/batches", protect(d, roleMgr, d.VarianceH.ListBatches))
	mux.Handle("GET /variance/batches/{batchId}", protect(d, roleMgr, d.VarianceH.GetBatch))
	mux.Handle("POST /variance/batches/{batchId}/flag", protect(d, roleMgr, d.VarianceH.Flag))
	mux.Handle("POST /variance/batches/{batchId}/reason", protect(d, roleMgr, d.VarianceH.Reason))

	mux.Handle("GET /approvals/pending", protect(d, roleMgr, d.ApprovalsH.Pending))
	mux.Handle("GET /approvals/history", protect(d, roleMgr, d.ApprovalsH.History))
	mux.Handle("POST /approvals/{approvalId}/approve", protect(d, roleMgr, d.ApprovalsH.Approve))
	mux.Handle("POST /approvals/{approvalId}/reject", protect(d, roleMgr, d.ApprovalsH.Reject))

	mux.Handle("POST /sync/bulk", protect(d, roleAll, d.SyncH.Bulk))
	mux.Handle("GET /dashboard/summary", protect(d, roleMgr, d.DashboardH.Summary))
	mux.Handle("GET /dashboard/kpis", protect(d, roleMgr, d.DashboardH.KPIs))
	mux.Handle("GET /dashboard/alerts", protect(d, []auth.Role{auth.RoleManager, auth.RoleAdmin, auth.RoleKitchen}, d.DashboardH.Alerts))
	mux.Handle("POST /notifications", protect(d, roleMgr, d.NotificationsH.Create))

	mux.Handle("GET /menu", protect(d, roleAll, d.MenuH.ListMenus))
	mux.Handle("POST /menu", protect(d, roleAdmin, d.MenuH.CreateMenu))
	mux.Handle("POST /menu/item", protect(d, roleAdmin, d.MenuH.CreateMenu))
	mux.Handle("GET /menu/categories", protect(d, roleAll, d.MenuH.ListCategories))
	mux.Handle("POST /menu/categories", protect(d, roleAdmin, d.MenuH.CreateCategory))
	mux.Handle("GET /menu/finished-goods", protect(d, roleAll, d.MenuH.ListFinishedGoods))
	mux.Handle("GET /menu/finished-good-categories", protect(d, roleAll, d.MenuH.ListFGCategories))
	mux.Handle("POST /menu/finished-good-categories/assign", protect(d, roleAdmin, d.MenuH.AssignFGCategory))
	mux.Handle("GET /menu/{menuId}/availability", protect(d, roleAll, d.MenuH.Availability))
	mux.Handle("GET /menu/catalog-items", protect(d, roleAll, d.MenuH.ListCatalog))
	mux.Handle("POST /menu/catalog-items", protect(d, roleAdmin, d.MenuH.CreateCatalog))
	mux.Handle("GET /menu/combos", protect(d, roleAll, d.MenuH.ListCombos))
	mux.Handle("POST /menu/combos", protect(d, roleAdmin, d.MenuH.CreateCombo))
	mux.Handle("POST /menu/combos/preview", protect(d, rolePOSKit, d.MenuH.PreviewCombo))

	mux.Handle("GET /consumption/summary", protect(d, roleMgr, d.ConsumptionH.Summary))
	mux.Handle("GET /consumption/products/{productId}", protect(d, roleMgr, d.ConsumptionH.Product))
	mux.Handle("GET /consumption/menu-items/{menuItemId}", protect(d, roleMgr, d.ConsumptionH.MenuItem))
	mux.Handle("GET /consumption/alerts", protect(d, roleMgr, d.ConsumptionH.Alerts))

	mux.Handle("GET /costing/sku/{itemId}", protect(d, roleAll, d.CostingH.SKU))

	mux.Handle("GET /finished-goods", protect(d, roleAll, d.FinishedGoodsH.List))
	mux.Handle("POST /finished-goods", protect(d, roleRecv, d.FinishedGoodsH.Add))
	mux.Handle("POST /finished-goods/adjust", protect(d, roleMgr, d.FinishedGoodsH.Adjust))

	mux.Handle("GET /refill/pending", protect(d, roleKitchen, d.RefillH.Pending))
	mux.Handle("GET /refill/requests/{requestId}", protect(d, roleKitchen, d.RefillH.Get))
	mux.Handle("POST /refill/request", protect(d, roleKitchen, d.RefillH.Request))
	mux.Handle("POST /refill/issue", protect(d, roleKitchen, d.RefillH.Issue))

	mux.Handle("GET /trace/diagnostics", protect(d, roleMgr, d.TraceH.Diagnostics))
	mux.Handle("GET /trace", protect(d, roleMgr, d.TraceH.List))
	mux.Handle("GET /trace/item/{itemId}", protect(d, roleMgr, d.TraceH.ByItem))

	// Prefix handler for Nest GET /recipe/item/{itemId} (cannot register next to /recipe/{id}/availability)
	byItem := protect(d, roleAll, handlers.RecipeByItemPath(d.RecipeH))

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
