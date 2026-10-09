package eventstore

import (
	"encoding/json"
	"time"

	"github.com/mekusolutions-design/SukariBurgersGo/pkg/money"
)

// Event mirrors the Prisma Event model and is the authoritative write-side record.
type Event struct {
	ID             string          `json:"id"`
	EventType      string          `json:"event_type"`
	ShopID         string          `json:"shop_id"`
	ActorUserID    *string         `json:"actor_user_id,omitempty"`
	IdempotencyKey string          `json:"idempotency_key"`
	ItemID         *string         `json:"item_id,omitempty"` // business SKU (Item.item_id)
	BatchNumber    *string         `json:"batch_number,omitempty"`
	ExpiryDate     *time.Time      `json:"expiry_date,omitempty"`
	Quantity       *float64        `json:"quantity,omitempty"`
	UnitCost       *money.Money    `json:"unit_cost,omitempty"`
	TotalCost      *money.Money    `json:"total_cost,omitempty"`
	SupplierID     *string         `json:"supplier_id,omitempty"`
	ApprovedByID   *string         `json:"approved_by_id,omitempty"`
	WasteReason    *string         `json:"waste_reason,omitempty"`
	WastePhotoURL  *string         `json:"waste_photo_url,omitempty"`
	Payload        json.RawMessage `json:"payload"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// AppendInput is the data required to append a new event.
type AppendInput struct {
	EventType      string
	ActorUserID    *string
	IdempotencyKey string
	ItemID         *string
	BatchNumber    *string
	ExpiryDate     *time.Time
	Quantity       *float64
	UnitCost       *money.Money
	TotalCost      *money.Money
	SupplierID     *string
	ApprovedByID   *string
	WasteReason    *string
	WastePhotoURL  *string
	Payload        map[string]interface{}
}

// Known event types — do not rename without a compatibility mapping.
const (
	EventPosSale                    = "pos_sale"
	EventOrderSentToKitchen         = "order_sent_to_kitchen"
	EventOrderStatusUpdated         = "order_status_updated"
	EventOrderPaymentUpdated        = "order_payment_updated"
	EventReceived                   = "received"
	EventRefillRequested            = "refill_requested"
	EventRefillIssued               = "refill_issued"
	EventProductionStarted          = "production_started"
	EventProductionFinished         = "production_finished"
	EventPrePrepCompleted           = "pre_prep_completed"
	EventWasteRecorded              = "waste_recorded"
	EventBatchExpired               = "batch_expired"
	EventVarianceBatch              = "variance_batch"
	EventVarianceFlagged            = "variance_flagged"
	EventVarianceReasonSubmitted    = "variance_reason_submitted"
	EventApprovalGranted            = "approval_granted"
	EventApprovalRejected           = "approval_rejected"
	EventRecipeCreated              = "recipe_created"
	EventRecipeUpdated              = "recipe_updated"
	EventRecipeIngredientAdded      = "recipe_ingredient_added"
	EventRecipeOutputAdded          = "recipe_output_added"
	EventMenuCreated                = "menu_created"
	EventMenuCategoryCreated        = "menu_category_created"
	EventMenuItemCreated            = "menu_item_created"
	EventMenuItemAdded              = "menu_item_added"
	EventComboCreated               = "combo_created"
	EventFinishedGoodAdded          = "finished_good_added"
	EventFinishedGoodAdjusted       = "finished_good_adjusted"
	EventStockCountCompleted        = "stock_count_completed"
	EventClosingStockCounted        = "closing_stock_counted"
	EventMpesaStkInitiated          = "mpesa_stk_initiated"
	EventMpesaCallbackProcessed     = "mpesa_callback_processed"
	EventStripePaymentIntentCreated = "stripe_payment_intent_created"
	EventStripePaymentSucceeded     = "stripe_payment_succeeded"
	EventStripePaymentFailed        = "stripe_payment_failed"
	EventNotificationCreated        = "notification_created"
	EventClientSyncAck              = "client_sync_ack"
	EventUserRoleChanged            = "user_role_changed"
)
