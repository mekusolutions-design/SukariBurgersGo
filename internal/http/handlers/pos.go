package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/restflow/api-go/internal/auth"
	"github.com/restflow/api-go/internal/menu"
	"github.com/restflow/api-go/internal/pos"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

type POSHandler struct {
	svc *pos.Service
}

func NewPOSHandler(svc *pos.Service) *POSHandler {
	return &POSHandler{svc: svc}
}

type createOrderRequest struct {
	ShopID               string             `json:"shop_id"`
	ShopIdCamel          string             `json:"shopId"`
	OrderType            string             `json:"order_type"`
	OrderTypeCamel       string             `json:"orderType"`
	TableNumber          string             `json:"table_number"`
	TableNumberCamel     string             `json:"tableNumber"`
	CustomerName         string             `json:"customer_name"`
	CustomerNameCamel    string             `json:"customerName"`
	CustomerPhone        string             `json:"customer_phone"`
	CustomerPhoneCamel   string             `json:"customerPhone"`
	DeliveryAddress      string             `json:"delivery_address"`
	DeliveryAddressCamel string             `json:"deliveryAddress"`
	Items                []orderLineRequest `json:"items"`
	TotalAmount          float64            `json:"total_amount"`
	TotalAmountCamel     float64            `json:"totalAmount"`
	TaxAmount            float64            `json:"tax_amount"`
	TaxAmountCamel       float64            `json:"taxAmount"`
	DiscountAmount       float64            `json:"discount_amount"`
	DiscountAmountCamel  float64            `json:"discountAmount"`
	PaymentMethod        string             `json:"payment_method"`
	PaymentMethodCamel   string             `json:"paymentMethod"`
	PaymentSplits        []pos.PaymentSplit `json:"payment_splits"`
	PaymentSplitsCamel   []pos.PaymentSplit `json:"paymentSplits"`
	MpesaReference       string             `json:"mpesa_reference"`
	MpesaReferenceCamel  string             `json:"mpesaReference"`
	PaymentStatus        string             `json:"payment_status"`
	PaymentStatusCamel   string             `json:"paymentStatus"`
	Notes                string             `json:"notes"`
	ClientIdempotencyKey string             `json:"client_idempotency_key"`
	ClientIdemKeyCamel   string             `json:"clientIdempotencyKey"`
}

type orderLineRequest struct {
	LineType          string                     `json:"line_type"`
	LineTypeCamel     string                     `json:"lineType"`
	MenuItemID        string                     `json:"menu_item_id"`
	MenuItemIDCamel   string                     `json:"menuItemId"`
	MenuID            string                     `json:"menu_id"`
	MenuIDCamel       string                     `json:"menuId"`
	ComboID           string                     `json:"combo_id"`
	ComboIDCamel      string                     `json:"comboId"`
	Name              string                     `json:"name"`
	MenuName          string                     `json:"menu_name"`
	MenuNameCamel     string                     `json:"menuName"`
	Quantity          float64                    `json:"quantity"`
	UnitPrice         float64                    `json:"unit_price"`
	UnitPriceCamel    float64                    `json:"unitPrice"`
	SellingPrice      float64                    `json:"selling_price"`
	SellingPriceCamel float64                    `json:"sellingPrice"`
	ComboSelections   []menu.ComboSelectionInput `json:"combo_selections"`
	ComboSelCamel     []menu.ComboSelectionInput `json:"comboSelections"`
	Selections        []menu.ComboSelectionInput `json:"selections"`
}

func coalesceStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func coalesceF(vals ...float64) float64 {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

func (h *POSHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid request body"))
		return
	}

	idem := r.Header.Get("Idempotency-Key")
	if idem == "" {
		idem = coalesceStr(req.ClientIdempotencyKey, req.ClientIdemKeyCamel)
	}

	shopID := auth.ShopIDFromContext(r.Context())

	items := make([]pos.OrderLineInput, 0, len(req.Items))
	for _, it := range req.Items {
		menuID := coalesceStr(it.MenuItemID, it.MenuItemIDCamel, it.MenuID, it.MenuIDCamel)
		name := coalesceStr(it.Name, it.MenuName, it.MenuNameCamel)
		unitPrice := coalesceF(it.UnitPrice, it.UnitPriceCamel, it.SellingPrice, it.SellingPriceCamel)
		comboID := coalesceStr(it.ComboID, it.ComboIDCamel)
		lineType := coalesceStr(it.LineType, it.LineTypeCamel)
		if lineType == "" {
			if comboID != "" {
				lineType = "combo"
			} else {
				lineType = "menu_item"
			}
		}
		sels := it.ComboSelections
		if len(sels) == 0 {
			sels = it.ComboSelCamel
		}
		if len(sels) == 0 {
			sels = it.Selections
		}
		items = append(items, pos.OrderLineInput{
			LineType: lineType, MenuItemID: menuID, ComboID: comboID, Name: name,
			Quantity: it.Quantity, UnitPrice: unitPrice, ComboSelections: sels,
		})
	}

	splits := req.PaymentSplits
	if len(splits) == 0 {
		splits = req.PaymentSplitsCamel
	}

	result, err := h.svc.CreateOrder(r.Context(), pos.CreateOrderInput{
		ShopID:               shopID,
		ActorUserID:          auth.UserIDFromContext(r.Context()),
		OrderType:            coalesceStr(req.OrderType, req.OrderTypeCamel),
		TableNumber:          coalesceStr(req.TableNumber, req.TableNumberCamel),
		CustomerName:         coalesceStr(req.CustomerName, req.CustomerNameCamel),
		CustomerPhone:        coalesceStr(req.CustomerPhone, req.CustomerPhoneCamel),
		DeliveryAddress:      coalesceStr(req.DeliveryAddress, req.DeliveryAddressCamel),
		Items:                items,
		TotalAmount:          coalesceF(req.TotalAmount, req.TotalAmountCamel),
		TaxAmount:            coalesceF(req.TaxAmount, req.TaxAmountCamel),
		DiscountAmount:       coalesceF(req.DiscountAmount, req.DiscountAmountCamel),
		PaymentMethod:        coalesceStr(req.PaymentMethod, req.PaymentMethodCamel),
		PaymentSplits:        splits,
		MpesaReference:       coalesceStr(req.MpesaReference, req.MpesaReferenceCamel),
		PaymentStatus:        coalesceStr(req.PaymentStatus, req.PaymentStatusCamel),
		Notes:                req.Notes,
		ClientIdempotencyKey: idem,
	})
	if err != nil {
		if ae, ok := err.(*apperrors.AppError); ok {
			apperrors.WriteJSON(w, r, ae)
		} else {
			apperrors.WriteJSON(w, r, apperrors.Wrap(500, "create order failed", err))
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *POSHandler) ListOrders(w http.ResponseWriter, r *http.Request) {
	// Match Nest's @CurrentShop behavior: query parameters are hints only and
	// must never replace the authenticated tenant scope.
	shopID := auth.ShopIDFromContext(r.Context())
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(q.Get("pageSize"))
	if limit <= 0 {
		limit, _ = strconv.Atoi(q.Get("limit"))
	}
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit
	orders, total, err := h.svc.ListOrders(r.Context(), shopID, limit, offset)
	if err != nil {
		if ae, ok := err.(*apperrors.AppError); ok {
			apperrors.WriteJSON(w, r, ae)
		} else {
			apperrors.WriteJSON(w, r, apperrors.Internal("failed to list orders"))
		}
		return
	}
	totalPages := (total + limit - 1) / limit
	if totalPages < 1 {
		totalPages = 1
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": orders, "total": total, "page": page,
		"pageSize": limit, "totalPages": totalPages,
	})
}

func (h *POSHandler) MenuAvailability(w http.ResponseWriter, r *http.Request) {
	// Nest scopes this route from the authenticated principal, not shopId query.
	shopID := auth.ShopIDFromContext(r.Context())
	if shopID == "" {
		apperrors.WriteJSON(w, r, apperrors.Forbidden("shop identity required"))
		return
	}
	items, err := h.svc.ListMenuAvailability(r.Context(), shopID)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.Internal("menu availability failed: "+err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"items":   items,
		"count":   len(items),
	})
}

func (h *POSHandler) Bootstrap(w http.ResponseWriter, r *http.Request) {
	// Nest's @CurrentShop scope is authoritative; never accept a tenant override
	// from query parameters on the POS bootstrap endpoint.
	shopID := auth.ShopIDFromContext(r.Context())
	if shopID == "" {
		apperrors.WriteJSON(w, r, apperrors.Forbidden("shop identity required"))
		return
	}
	out, err := h.svc.Bootstrap(r.Context(), shopID)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
