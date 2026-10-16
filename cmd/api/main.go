package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/restflow/api-go/internal/approvals"
	"github.com/restflow/api-go/internal/auth"
	"github.com/restflow/api-go/internal/config"
	"github.com/restflow/api-go/internal/consumption"
	"github.com/restflow/api-go/internal/costing"
	"github.com/restflow/api-go/internal/dashboard"
	"github.com/restflow/api-go/internal/eventstore"
	"github.com/restflow/api-go/internal/finishedgoods"
	"github.com/restflow/api-go/internal/http/handlers"
	"github.com/restflow/api-go/internal/http/router"
	"github.com/restflow/api-go/internal/inventory"
	"github.com/restflow/api-go/internal/kitchen"
	"github.com/restflow/api-go/internal/menu"
	"github.com/restflow/api-go/internal/notifications"
	"github.com/restflow/api-go/internal/payments"
	"github.com/restflow/api-go/internal/pos"
	"github.com/restflow/api-go/internal/production"
	"github.com/restflow/api-go/internal/projections"
	"github.com/restflow/api-go/internal/realtime"
	"github.com/restflow/api-go/internal/realtime/socketio"
	"github.com/restflow/api-go/internal/receiving"
	"github.com/restflow/api-go/internal/recipe"
	"github.com/restflow/api-go/internal/refill"
	"github.com/restflow/api-go/internal/repository"
	"github.com/restflow/api-go/internal/sync"
	"github.com/restflow/api-go/internal/trace"
	"github.com/restflow/api-go/internal/variance"
	"github.com/restflow/api-go/internal/waste"
	"github.com/restflow/api-go/internal/observability"
	"github.com/restflow/api-go/pkg/logging"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	log := logging.New(cfg.Env)
	log.Info("starting RestFlow Go API", "env", cfg.Env, "port", cfg.Port)

	flushSentry := observability.InitSentry(cfg.SentryDSN, cfg.SentryEnvironment, cfg.SentryRelease, log)
	defer flushSentry()

	ctx := context.Background()
	pool, err := repository.NewPool(ctx, cfg)
	if err != nil {
		log.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	projEngine := projections.NewEngine(log)
	eventStore := eventstore.NewStore(pool, log)
	eventStore.SetProjectionRunner(projEngine)

	resolveSvc := menu.NewResolutionService(pool)
	resolveSvc.SetEventStore(eventStore)
	authSvc := auth.NewService(pool, cfg)
	hub := realtime.NewHub(log, authSvc)
	sioSrv := socketio.NewServer(log, authSvc)
	hub.Sio = sioSrv
	projEngine.SetBroadcaster(hub) // append → project → emit (REST + WS + Socket.IO)

	r := router.New(router.Deps{
		Cfg: cfg, Log: log, AuthSvc: authSvc, Hub: hub,
		AuthH:          handlers.NewAuthHandler(authSvc),
		UsersH:         handlers.NewUsersHandler(authSvc),
		HealthH:        handlers.NewHealthHandler(pool),
		InventoryH:     handlers.NewInventoryHandler(inventory.NewService(pool)),
		ReceivedH:      handlers.NewReceivedHandler(receiving.NewService(pool, eventStore)),
		POSH:           handlers.NewPOSHandler(pos.NewService(pool, eventStore, resolveSvc)),
		KitchenH:       handlers.NewKitchenHandler(kitchen.NewService(pool, eventStore)),
		RecipeH:        handlers.NewRecipeHandler(recipe.NewService(pool)),
		WasteH:         handlers.NewWasteHandler(waste.NewService(pool, eventStore)),
		ProductionH:    handlers.NewProductionHandler(production.NewService(pool, eventStore, resolveSvc)),
		PaymentH:       handlers.NewPaymentHandler(payments.NewService(pool, eventStore, cfg, log)),
		VarianceH:      handlers.NewVarianceHandler(variance.NewService(pool, eventStore)),
		ApprovalsH:     handlers.NewApprovalsHandler(approvals.NewService(pool, eventStore)),
		SyncH:          handlers.NewSyncHandler(sync.NewService(pool, eventStore)),
		DashboardH:     handlers.NewDashboardHandler(dashboard.NewService(pool)),
		NotificationsH: handlers.NewNotificationsHandler(notifications.NewService(pool, eventStore)),
		MenuH:          handlers.NewMenuHandler(resolveSvc),
		ConsumptionH:   handlers.NewConsumptionHandler(consumption.NewService(pool)),
		CostingH:       handlers.NewCostingHandler(costing.NewService(pool)),
		FinishedGoodsH: handlers.NewFinishedGoodsHandler(finishedgoods.NewService(pool, eventStore)),
		RefillH:        handlers.NewRefillHandler(refill.NewService(pool, eventStore)),
		TraceH:         handlers.NewTraceHandler(trace.NewService(pool)),
	})

	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.Port), Handler: r,
		ReadTimeout: cfg.HTTP.ReadTimeout, WriteTimeout: cfg.HTTP.WriteTimeout, IdleTimeout: cfg.HTTP.IdleTimeout,
	}
	go func() {
		log.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Info("stopped")
}
