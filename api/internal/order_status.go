package internal

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	OrderStatusCancelled = "Cancelled"
	OrderStatusComplete  = "Complete"
)

var ErrOrderClosed = errors.New("this order is already cancelled or complete")
var ErrCannotComplete = errors.New("only booked delivery orders or self-collection orders can be marked complete")

// UpdateOrderStatus moves an order to Cancelled or Complete.
//   - Cancelled: needs a remark, and is refused when an EasyParcel shipment exists.
//   - Complete: allowed for orders with a shipment number, or for self-collection orders.
func (s *PostgresStore) UpdateOrderStatus(ctx context.Context, id, status, remark string, actor AuthenticatedActor) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var current string
	err = tx.QueryRow(ctx, `SELECT status FROM wms_order WHERE id=$1 FOR UPDATE`, id).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if strings.EqualFold(current, OrderStatusCancelled) || strings.EqualFold(current, OrderStatusComplete) {
		return ErrOrderClosed
	}

	var method string
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT method FROM order_fulfilment WHERE order_id=$1),'delivery')`, id).Scan(&method); err != nil {
		return err
	}

	var shipmentStatus, shipmentNumber string
	hasShipment := true
	err = tx.QueryRow(ctx, `SELECT status, COALESCE(shipment_number,'') FROM shipment WHERE order_id=$1`, id).Scan(&shipmentStatus, &shipmentNumber)
	if errors.Is(err, pgx.ErrNoRows) {
		hasShipment = false
	} else if err != nil {
		return err
	}
	// A shipment row that only recorded a failed attempt did not book anything.
	booked := hasShipment && shipmentStatus != "failed"

	switch status {
	case OrderStatusCancelled:
		if booked {
			return ErrShipmentAlreadySubmitted
		}
		_, err = tx.Exec(ctx, `UPDATE wms_order SET status=$2, cancel_remark=$3, cancelled_at=now(), cancelled_by=$4, cancelled_name=$5, updated_at=now() WHERE id=$1`,
			id, OrderStatusCancelled, remark, actor.ID, actor.Name)
	case OrderStatusComplete:
		if shipmentNumber == "" && method != "self_collection" {
			return ErrCannotComplete
		}
		_, err = tx.Exec(ctx, `UPDATE wms_order SET status=$2, completed_at=now(), completed_by=$3, updated_at=now() WHERE id=$1`,
			id, OrderStatusComplete, actor.ID)
	default:
		return errors.New("unsupported order status")
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *MemoryStore) UpdateOrderStatus(_ context.Context, id, status, remark string, _ AuthenticatedActor) error {
	for i := range s.orders {
		if s.orders[i].ID == id {
			s.orders[i].Status = status
			s.orders[i].CancelRemark = remark
			return nil
		}
	}
	return ErrNotFound
}

// PATCH /api/orders/{id}/status with {"status":"cancelled","remark":"..."} or {"status":"complete"}
func (h *Handler) updateOrderStatus(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var input struct {
		Status string `json:"status"`
		Remark string `json:"remark"`
	}
	if !decode(w, r, &input) {
		return
	}
	remark := strings.TrimSpace(input.Remark)
	var status string
	switch strings.ToLower(strings.TrimSpace(input.Status)) {
	case "cancelled":
		status = OrderStatusCancelled
		if n := len([]rune(remark)); n < 3 || n > 500 {
			writeError(w, http.StatusBadRequest, "a cancellation reason of 3 to 500 characters is required")
			return
		}
	case "complete":
		status = OrderStatusComplete
		remark = ""
	default:
		writeError(w, http.StatusBadRequest, "status must be cancelled or complete")
		return
	}
	if err := h.store.UpdateOrderStatus(r.Context(), id, status, remark, actorFromRequest(r)); err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "order not found")
		case errors.Is(err, ErrOrderClosed), errors.Is(err, ErrCannotComplete):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrShipmentAlreadySubmitted):
			writeError(w, http.StatusConflict, "this order already has an EasyParcel shipment; cancel the shipment in EasyParcel first")
		default:
			log.Printf("update order status %s: %v", id, err)
			writeError(w, http.StatusInternalServerError, "could not update the order status")
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"order_id": id, "status": status})
}