package internal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) StartOrderSyncRun(ctx context.Context, raw []byte) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO order_sync_run(raw_response) VALUES($1) RETURNING id`, string(raw)).Scan(&id)
	return id, err
}

func (s *PostgresStore) FinishOrderSyncRun(ctx context.Context, id int64, r OrderSyncResult) error {
	_, err := s.pool.Exec(ctx, `UPDATE order_sync_run SET finished_at=now(), order_count=$2, created_count=$3,
		updated_count=$4, skipped_count=$5, failed_count=$6, error=NULLIF($7,'') WHERE id=$1`,
		id, r.Received, r.Created, r.Updated, r.Skipped, r.Failed, r.Error)
	return err
}

// UpsertSyncedOrder saves one order from the order API.
//   - New order: created with the items and value from the API, plus the warehouse
//     sender address copied from the most recent order that has one. The customer
//     address stays empty until staff fill it in.
//   - Existing order: name, items, status and raw data are refreshed. The delivery
//     details are left alone so addresses staff typed in are never erased.
func (s *PostgresStore) UpsertSyncedOrder(ctx context.Context, o SyncedOrder) (SyncOutcome, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var current string
	var booked bool
	err = tx.QueryRow(ctx, `SELECT o.status, EXISTS(SELECT 1 FROM shipment sh WHERE sh.order_id=o.id AND sh.status<>'failed')
		FROM wms_order o WHERE o.id=$1 FOR UPDATE OF o`, o.ID).Scan(&current, &booked)
	if errors.Is(err, pgx.ErrNoRows) {
		if !o.AllowCreate {
			return SyncSkipped, nil
		}
		if err := s.insertSyncedOrder(ctx, tx, o); err != nil {
			return "", err
		}
		return SyncCreated, tx.Commit(ctx)
	}
	if err != nil {
		return "", err
	}

	status := o.Status
	switch {
	case strings.EqualFold(current, OrderStatusCancelled) || strings.EqualFold(current, OrderStatusComplete):
		// Closed in the WMS: keep it closed.
		status = current
	case status == OrderStatusCancelled && booked:
		// Cancelled at source after a courier was booked. Keep the order open so the
		// booking stays visible; staff must cancel the shipment in EasyParcel.
		status = current
	}
	_, err = tx.Exec(ctx, `UPDATE wms_order SET customer_name=$2, items_summary=$3, status=$4, source_payload=$5,
		synced_at=now(), updated_at=now(),
		cancel_remark=CASE WHEN $4='Cancelled' AND status<>'Cancelled' THEN 'Cancelled in the order system' ELSE cancel_remark END,
		cancelled_at=CASE WHEN $4='Cancelled' AND status<>'Cancelled' THEN now() ELSE cancelled_at END,
		cancelled_name=CASE WHEN $4='Cancelled' AND status<>'Cancelled' THEN 'Order system' ELSE cancelled_name END
		WHERE id=$1`, o.ID, o.Customer, o.Items, status, []byte(o.Raw))
	if err != nil {
		return "", err
	}
	return SyncUpdated, tx.Commit(ctx)
}

func (s *PostgresStore) insertSyncedOrder(ctx context.Context, tx pgx.Tx, o SyncedOrder) error {
	draft := ShipmentDraft{
		OrderID:      o.ID,
		Reference:    o.ID,
		Receiver:     Address{Name: o.Customer, PhoneNumberCountryCode: "60", CountryCode: "MY"},
		ParcelValue:  o.ParcelValue,
		CurrencyCode: o.Currency,
		Items:        o.Lines,
	}
	var sender []byte
	err := tx.QueryRow(ctx, `SELECT shipment_draft->'sender' FROM wms_order
		WHERE COALESCE(shipment_draft->'sender'->>'address_1','')<>'' ORDER BY updated_at DESC LIMIT 1`).Scan(&sender)
	if err == nil {
		_ = json.Unmarshal(sender, &draft.Sender)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	payload, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO wms_order(id,order_date,customer_name,items_summary,status,shipment_draft,source,source_payload,synced_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,'sms',$7,now(),now())`,
		o.ID, o.Date, o.Customer, o.Items, o.Status, payload, []byte(o.Raw))
	return err
}

// SaveShipmentDetails replaces an order's delivery address and parcel details.
// It is refused once a courier is booked or the order is closed.
func (s *PostgresStore) SaveShipmentDetails(ctx context.Context, id string, draft ShipmentDraft) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	var booked bool
	err = tx.QueryRow(ctx, `SELECT o.status, EXISTS(SELECT 1 FROM shipment sh WHERE sh.order_id=o.id AND sh.status<>'failed')
		FROM wms_order o WHERE o.id=$1 FOR UPDATE OF o`, id).Scan(&status, &booked)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if strings.EqualFold(status, OrderStatusCancelled) || strings.EqualFold(status, OrderStatusComplete) {
		return ErrOrderClosed
	}
	if booked {
		return ErrShipmentAlreadyBooked
	}
	payload, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE wms_order SET shipment_draft=$2, updated_at=now() WHERE id=$1`, id, payload); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// In-memory versions, used only when DATABASE_URL is not set.

func (s *MemoryStore) StartOrderSyncRun(context.Context, []byte) (int64, error) { return 0, nil }
func (s *MemoryStore) FinishOrderSyncRun(context.Context, int64, OrderSyncResult) error {
	return nil
}

func (s *MemoryStore) UpsertSyncedOrder(_ context.Context, o SyncedOrder) (SyncOutcome, error) {
	for i := range s.orders {
		if s.orders[i].ID == o.ID {
			if !strings.EqualFold(s.orders[i].Status, OrderStatusCancelled) && !strings.EqualFold(s.orders[i].Status, OrderStatusComplete) {
				s.orders[i].Status = o.Status
			}
			s.orders[i].Customer, s.orders[i].Items = o.Customer, o.Items
			return SyncUpdated, nil
		}
	}
	if !o.AllowCreate {
		return SyncSkipped, nil
	}
	s.orders = append(s.orders, Order{ID: o.ID, Date: o.Date, Customer: o.Customer, Items: o.Items, Status: o.Status, Method: "delivery", NeedsDetails: true})
	return SyncCreated, nil
}

func (s *MemoryStore) SaveShipmentDetails(_ context.Context, id string, _ ShipmentDraft) error {
	for i := range s.orders {
		if s.orders[i].ID == id {
			s.orders[i].NeedsDetails = false
			return nil
		}
	}
	return ErrNotFound
}
