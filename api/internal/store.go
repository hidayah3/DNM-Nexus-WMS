package internal

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("order not found")

// userError is a business-rule message that is safe to show to staff as it is.
// Any other error (database, network) is logged and replaced by a generic message.
type userError struct{ msg string }

func (e userError) Error() string { return e.msg }

// UserError makes an error whose text can be shown to staff.
func UserError(msg string) error { return userError{msg} }

var ErrDuplicateOrder = errors.New("order ID already exists")
var ErrShipmentAlreadySubmitted = errors.New("shipment already exists or has an unresolved EasyParcel submission; reconcile it before retrying")

type Store interface {
	GetStaffUser(context.Context, string) (StaffUser, error)
	SaveStaffUser(context.Context, StaffUser) error
	CreateSession(context.Context, string, string, time.Time) error
	SessionUser(context.Context, string) (StaffUser, error)
	DeleteSession(context.Context, string) error
	UpsertIncomingOrder(context.Context, IncomingOrder) error
	UpsertSyncedOrder(context.Context, SyncedOrder) (SyncOutcome, error)
	StartOrderSyncRun(context.Context, []byte) (int64, error)
	FinishOrderSyncRun(context.Context, int64, OrderSyncResult) error
	SaveShipmentDetails(context.Context, string, ShipmentDraft) error
	ListOrders(context.Context, time.Time, time.Time) ([]Order, error)
	SaveFulfilment(context.Context, string, Fulfilment) error
	UpdateOrderStatus(context.Context, string, string, string, AuthenticatedActor) error
	DeleteOrder(context.Context, string) error
	GetShipmentDraft(context.Context, string) (ShipmentDraft, error)
	ReserveShipmentPush(context.Context, []PushOrder) error
	SaveShipmentResult(context.Context, PushResult, string) error
	MarkShipmentUnknown(context.Context, []string) error
	GetShipmentRecords(context.Context, []string) ([]ShipmentRecord, error)
	SaveAWBDetails(context.Context, AWBResult) error
	SaveEasyParcelWebhook(context.Context, EasyParcelWebhook, []byte) (bool, error)
	ListInventory(context.Context) ([]InventoryProduct, error)
	ListInventoryLots(context.Context) ([]InventoryLot, error)
	CreateInventoryProduct(context.Context, InventoryProduct) error
	ReceiveInventoryStock(context.Context, InventoryReceipt, AuthenticatedActor) (InventoryProduct, InventoryLot, StockMovement, error)
	AdjustStock(context.Context, StockAdjustment, AuthenticatedActor) (InventoryProduct, InventoryLot, StockMovement, error)
	ListStockMovements(context.Context, time.Time, time.Time) ([]StockMovement, error)
	InventoryWorkbookRows(context.Context) ([]InventoryLot, error)
	ListInventorySyncOutbox(context.Context, int) ([]InventorySyncEvent, error)
	MarkInventorySyncSent(context.Context, int64) error
	MarkInventorySyncFailed(context.Context, int64, string) error
	ListShippingRates(context.Context) ([]ShippingRate, error)
	SaveShippingRate(context.Context, ShippingRate, AuthenticatedActor) (ShippingRate, error)
}

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

// Orders are snapshots received from SMS through the authenticated integration endpoint.
func (s *PostgresStore) ListOrders(ctx context.Context, from, to time.Time) ([]Order, error) {
	rows, err := s.pool.Query(ctx, `SELECT o.id::text, o.order_date, o.customer_name, o.items_summary, o.status,
		COALESCE(f.method,'delivery'), COALESCE(sh.courier_name,f.courier_name,''), COALESCE(f.courier_code,''), COALESCE(sh.tracking_number,''),
		COALESCE(sh.shipment_number,''), COALESCE(sh.provider_status,''), COALESCE(sh.tracking_status,''), sh.tracking_status_code, COALESCE(sh.awb_url,''),
		COALESCE(o.cancel_remark,''), COALESCE(o.cancelled_name,''), o.cancelled_at, o.completed_at,
		NOT (COALESCE(o.shipment_draft->'receiver'->>'address_1','')<>'' AND COALESCE(o.shipment_draft->'receiver'->>'phone_number','')<>''
			AND COALESCE(o.shipment_draft->'sender'->>'address_1','')<>'' AND COALESCE(o.shipment_draft->>'weight','0') NOT IN ('0',''))
		FROM wms_order o LEFT JOIN order_fulfilment f ON f.order_id=o.id
		LEFT JOIN shipment sh ON sh.order_id=o.id::text
		WHERE ($1::date IS NULL OR o.order_date::date >= $1::date) AND ($2::date IS NULL OR o.order_date::date <= $2::date)
		ORDER BY o.order_date DESC, o.id DESC`, nullableDate(from), nullableDate(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	orders := []Order{}
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.Date, &o.Customer, &o.Items, &o.Status, &o.Method, &o.Courier, &o.CourierCode, &o.Tracking, &o.ShipmentNo, &o.ProviderStatus, &o.TrackingStatus, &o.TrackingStatusCode, &o.AWBURL,
			&o.CancelRemark, &o.CancelledName, &o.CancelledAt, &o.CompletedAt, &o.NeedsDetails); err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}

func (s *PostgresStore) UpsertIncomingOrder(ctx context.Context, incoming IncomingOrder) error {
	draft := incoming.Shipment
	draft.OrderID = incoming.ID
	if draft.Reference == "" {
		draft.Reference = incoming.ID
	}
	payload, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO wms_order(id,order_date,customer_name,items_summary,status,shipment_draft,source,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,'sms',now()) ON CONFLICT(id) DO UPDATE SET order_date=EXCLUDED.order_date,customer_name=EXCLUDED.customer_name,
		items_summary=EXCLUDED.items_summary,status=CASE WHEN wms_order.status IN ('Cancelled','Complete') THEN wms_order.status ELSE EXCLUDED.status END,shipment_draft=EXCLUDED.shipment_draft,source='sms',updated_at=now()`,
		incoming.ID, incoming.Date, incoming.Customer, incoming.Items, incoming.Status, payload)
	return err
}

func (s *PostgresStore) SaveFulfilment(ctx context.Context, id string, f Fulfilment) error {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wms_order WHERE id=$1)`, id).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	// When no name is sent (e.g. from the booking step), keep the saved name if the courier is unchanged.
	_, err = s.pool.Exec(ctx, `INSERT INTO order_fulfilment(order_id,method,courier_code,courier_name,updated_at)
		VALUES($1,$2,NULLIF($3,''),NULLIF($4,''),now()) ON CONFLICT(order_id) DO UPDATE SET method=EXCLUDED.method,courier_code=EXCLUDED.courier_code,
		courier_name=COALESCE(EXCLUDED.courier_name, CASE WHEN order_fulfilment.courier_code IS NOT DISTINCT FROM EXCLUDED.courier_code THEN order_fulfilment.courier_name END),
		updated_at=now()`, id, f.Method, f.CourierCode, f.CourierName)
	return err
}

func (s *PostgresStore) DeleteOrder(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var locked string
	err = tx.QueryRow(ctx, `SELECT id::text FROM wms_order WHERE id=$1 FOR UPDATE`, id).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM shipment WHERE order_id=$1 FOR UPDATE`, id).Scan(&status)
	switch {
	case err == nil:
		if status != "failed" {
			return ErrShipmentAlreadySubmitted
		}
		if _, err := tx.Exec(ctx, `DELETE FROM shipment WHERE order_id=$1`, id); err != nil {
			return err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM order_fulfilment WHERE order_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM wms_order WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Shipment inputs are supplied by the SMS order adapter into wms_shipment_details.
func (s *PostgresStore) GetShipmentDraft(ctx context.Context, id string) (ShipmentDraft, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wms_order WHERE id=$1)`, id).Scan(&exists); err != nil {
		return ShipmentDraft{}, err
	}
	if !exists {
		return ShipmentDraft{}, ErrNotFound
	}
	var payload []byte
	err := s.pool.QueryRow(ctx, `SELECT shipment_draft FROM wms_order WHERE id=$1`, id).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return ShipmentDraft{}, errors.New("shipping address and parcel details are not mapped for this order")
	}
	if err != nil {
		return ShipmentDraft{}, err
	}
	var draft ShipmentDraft
	if err := json.Unmarshal(payload, &draft); err != nil {
		return ShipmentDraft{}, errors.New("saved shipping details are invalid")
	}
	draft.OrderID = id
	if draft.Reference == "" {
		draft.Reference = id
	}
	return draft, nil
}

func (s *PostgresStore) ReserveShipmentPush(ctx context.Context, requests []PushOrder) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, request := range requests {
		tag, err := tx.Exec(ctx, `INSERT INTO shipment(order_id,provider,status,service_id,quoted_amount,quoted_currency,updated_at)
			VALUES($1,'easyparcel','submitting',$2,NULL,NULL,now())
			ON CONFLICT(order_id) DO UPDATE SET provider='easyparcel',status='submitting',service_id=EXCLUDED.service_id,
			quoted_amount=NULL,quoted_currency=NULL,updated_at=now()
			WHERE shipment.status='failed'`, request.OrderID, request.ServiceID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrShipmentAlreadySubmitted
		}
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) SaveShipmentResult(ctx context.Context, result PushResult, serviceID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE shipment SET status=$2,service_id=$3,courier_name=NULLIF($4,''),provider_order_id=NULLIF($5,''),shipment_number=NULLIF($6,''),
		tracking_number=NULLIF($7,''),tracking_url=NULLIF($8,''),awb_number=NULLIF($9,''),awb_url=NULLIF($10,''),
		quoted_amount=$11,quoted_currency=NULLIF($12,''),last_error=NULLIF($13,''),updated_at=now() WHERE order_id=$1`,
		result.OrderID, result.Status, serviceID, result.CourierName, result.ProviderOrderNumber, result.ShipmentNumber, result.TrackingNumber, result.TrackingURL,
		result.AWBNumber, result.AWBURL, result.Amount, result.Currency, result.Error)
	if err != nil {
		return err
	}
	if result.ShipmentNumber != "" {
		return s.applyPendingEasyParcelWebhooks(ctx, result.ShipmentNumber)
	}
	return nil
}

func (s *PostgresStore) MarkShipmentUnknown(ctx context.Context, orderIDs []string) error {
	_, err := s.pool.Exec(ctx, `UPDATE shipment SET status='outcome_unknown',last_error='EasyParcel response was not conclusive; reconcile before retrying',updated_at=now()
		WHERE order_id=ANY($1) AND status='submitting'`, orderIDs)
	return err
}

func (s *PostgresStore) GetShipmentRecords(ctx context.Context, orderIDs []string) ([]ShipmentRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT order_id,COALESCE(service_id,''),status,COALESCE(provider_status,''),provider_status_code,COALESCE(tracking_status,''),tracking_status_code,provider_event_at,COALESCE(shipment_number,''),COALESCE(provider_order_id,''),
		COALESCE(tracking_number,''),COALESCE(tracking_url,''),COALESCE(awb_number,''),COALESCE(awb_url,''),
		COALESCE(quoted_currency,''),COALESCE(quoted_amount,0),COALESCE(last_error,'')
		FROM shipment WHERE order_id=ANY($1)`, orderIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []ShipmentRecord{}
	for rows.Next() {
		var r ShipmentRecord
		if err := rows.Scan(&r.OrderID, &r.ServiceID, &r.Status, &r.ProviderStatus, &r.ProviderStatusCode, &r.TrackingStatus, &r.TrackingStatusCode, &r.ProviderEventAt, &r.ShipmentNumber, &r.OrderNumber, &r.TrackingNumber, &r.TrackingURL, &r.AWBNumber, &r.AWBURL, &r.Currency, &r.Amount, &r.Error); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (s *PostgresStore) SaveAWBDetails(ctx context.Context, result AWBResult) error {
	_, err := s.pool.Exec(ctx, `UPDATE shipment SET awb_number=COALESCE(NULLIF($2,''),awb_number),awb_url=COALESCE(NULLIF($3,''),awb_url),
		tracking_number=COALESCE(NULLIF($4,''),tracking_number),tracking_url=COALESCE(NULLIF($5,''),tracking_url),updated_at=now()
		WHERE order_id=$1 AND shipment_number=$6`, result.OrderID, result.AWBNumber, result.AWBURL, result.TrackingNumber, result.TrackingURL, result.ShipmentNumber)
	return err
}

func (s *PostgresStore) SaveEasyParcelWebhook(ctx context.Context, event EasyParcelWebhook, raw []byte) (bool, error) {
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `INSERT INTO wms_easyparcel_webhook_event
		(event_key,topic,shipment_number,occurred_at,provider_status,provider_status_code,tracking_status,tracking_status_code,tracking_number,tracking_url,awb_number,awb_url,payload)
		VALUES($1,$2,$3,$4,NULLIF($5,''),$6,NULLIF($7,''),$8,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),$13::jsonb)
		ON CONFLICT(event_key) DO NOTHING`, event.EventKey, event.Topic, event.ShipmentNumber, event.OccurredAt,
		event.ProviderStatus, event.ProviderStatusCode, event.TrackingStatus, event.TrackingStatusCode,
		event.TrackingNumber, event.TrackingURL, event.AWBNumber, event.AWBURL, string(raw))
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
		return true, nil
	}
	var orderID string
	err = tx.QueryRow(ctx, `SELECT order_id FROM shipment WHERE shipment_number=$1 FOR UPDATE`, event.ShipmentNumber).Scan(&orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Shipment creation can race its webhook; retain the event and apply it
		// from SaveShipmentResult once the API response has been persisted.
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := applyEasyParcelWebhookTx(ctx, tx, orderID, event); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE wms_easyparcel_webhook_event SET applied=true WHERE event_key=$1`, event.EventKey); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *PostgresStore) applyPendingEasyParcelWebhooks(ctx context.Context, shipmentNumber string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var orderID string
	if err := tx.QueryRow(ctx, `SELECT order_id FROM shipment WHERE shipment_number=$1 FOR UPDATE`, shipmentNumber).Scan(&orderID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT event_key,topic,shipment_number,provider_status,provider_status_code,tracking_status,tracking_status_code,tracking_number,tracking_url,awb_number,awb_url,occurred_at
		FROM wms_easyparcel_webhook_event WHERE shipment_number=$1 AND applied=false ORDER BY occurred_at,received_at FOR UPDATE`, shipmentNumber)
	if err != nil {
		return err
	}
	type pendingEvent struct {
		key, topic, shipmentNumber, providerStatus, trackingStatus, trackingNumber, trackingURL, awbNumber, awbURL string
		providerCode, trackingCode                                                                                 *int
		occurred                                                                                                   time.Time
	}
	pending := []pendingEvent{}
	for rows.Next() {
		var event pendingEvent
		if err := rows.Scan(&event.key, &event.topic, &event.shipmentNumber, &event.providerStatus, &event.providerCode,
			&event.trackingStatus, &event.trackingCode, &event.trackingNumber, &event.trackingURL, &event.awbNumber, &event.awbURL, &event.occurred); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, event)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range pending {
		event := EasyParcelWebhook{EventKey: item.key, Topic: item.topic, ShipmentNumber: item.shipmentNumber,
			ProviderStatus: item.providerStatus, ProviderStatusCode: item.providerCode, TrackingStatus: item.trackingStatus,
			TrackingStatusCode: item.trackingCode, TrackingNumber: item.trackingNumber, TrackingURL: item.trackingURL, AWBNumber: item.awbNumber,
			AWBURL: item.awbURL, OccurredAt: item.occurred}
		if err := applyEasyParcelWebhookTx(ctx, tx, orderID, event); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE wms_easyparcel_webhook_event SET applied=true WHERE event_key=$1`, item.key); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func applyEasyParcelWebhookTx(ctx context.Context, tx pgx.Tx, orderID string, event EasyParcelWebhook) error {
	_, err := tx.Exec(ctx, `UPDATE shipment SET
		provider_status=CASE WHEN $10 >= COALESCE(provider_event_at,'-infinity'::timestamptz) THEN COALESCE(NULLIF($2,''),provider_status) ELSE provider_status END,
		provider_status_code=CASE WHEN $10 >= COALESCE(provider_event_at,'-infinity'::timestamptz) THEN COALESCE($3,provider_status_code) ELSE provider_status_code END,
		tracking_status=CASE WHEN $10 >= COALESCE(provider_event_at,'-infinity'::timestamptz) THEN COALESCE(NULLIF($4,''),tracking_status) ELSE tracking_status END,
		tracking_status_code=CASE WHEN $10 >= COALESCE(provider_event_at,'-infinity'::timestamptz) THEN COALESCE($5,tracking_status_code) ELSE tracking_status_code END,
		tracking_number=CASE WHEN $10 >= COALESCE(provider_event_at,'-infinity'::timestamptz) THEN COALESCE(NULLIF($6,''),tracking_number) ELSE tracking_number END,
		tracking_url=CASE WHEN $10 >= COALESCE(provider_event_at,'-infinity'::timestamptz) THEN COALESCE(NULLIF($7,''),tracking_url) ELSE tracking_url END,
		awb_number=CASE WHEN $10 >= COALESCE(provider_event_at,'-infinity'::timestamptz) THEN COALESCE(NULLIF($8,''),awb_number) ELSE awb_number END,
		awb_url=CASE WHEN $10 >= COALESCE(provider_event_at,'-infinity'::timestamptz) THEN COALESCE(NULLIF($9,''),awb_url) ELSE awb_url END,
		provider_event_at=GREATEST(COALESCE(provider_event_at,'-infinity'::timestamptz),$10),
		updated_at=now()
		WHERE order_id=$1`, orderID, event.ProviderStatus, event.ProviderStatusCode, event.TrackingStatus,
		event.TrackingStatusCode, event.TrackingNumber, event.TrackingURL, event.AWBNumber, event.AWBURL, event.OccurredAt)
	return err
}

type MemoryStore struct{ orders []Order }

func NewMemoryStore() *MemoryStore { return &MemoryStore{orders: []Order{}} }
func (s *MemoryStore) UpsertIncomingOrder(_ context.Context, incoming IncomingOrder) error {
	for i := range s.orders {
		if s.orders[i].ID == incoming.ID {
			s.orders[i] = Order{ID: incoming.ID, Date: incoming.Date, Customer: incoming.Customer, Items: incoming.Items, Status: incoming.Status, Method: "delivery"}
			return nil
		}
	}
	s.orders = append(s.orders, Order{ID: incoming.ID, Date: incoming.Date, Customer: incoming.Customer, Items: incoming.Items, Status: incoming.Status, Method: "delivery"})
	return nil
}
func (s *MemoryStore) ListOrders(_ context.Context, from, to time.Time) ([]Order, error) {
	out := []Order{}
	for _, o := range s.orders {
		if !from.IsZero() && o.Date.Before(from) {
			continue
		}
		if !to.IsZero() && o.Date.After(to) {
			continue
		}
		out = append(out, o)
	}
	return out, nil
}
func (s *MemoryStore) SaveFulfilment(_ context.Context, id string, f Fulfilment) error {
	for i := range s.orders {
		if s.orders[i].ID == id {
			s.orders[i].Method = f.Method
			s.orders[i].CourierCode = f.CourierCode
			s.orders[i].Courier = f.CourierName
			return nil
		}
	}
	return ErrNotFound
}
func (s *MemoryStore) DeleteOrder(_ context.Context, id string) error {
	for i := range s.orders {
		if s.orders[i].ID == id {
			s.orders = append(s.orders[:i], s.orders[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}
func (s *MemoryStore) GetShipmentDraft(_ context.Context, id string) (ShipmentDraft, error) {
	return ShipmentDraft{}, errors.New("order has no stored shipping address or parcel measurements")
}
func (s *MemoryStore) ReserveShipmentPush(context.Context, []PushOrder) error {
	return errors.New("shipment persistence is unavailable in sample mode")
}
func (s *MemoryStore) SaveShipmentResult(context.Context, PushResult, string) error {
	return errors.New("shipment persistence is unavailable in sample mode")
}
func (s *MemoryStore) MarkShipmentUnknown(context.Context, []string) error {
	return errors.New("shipment persistence is unavailable in sample mode")
}
func (s *MemoryStore) GetShipmentRecords(context.Context, []string) ([]ShipmentRecord, error) {
	return nil, errors.New("shipment persistence is unavailable in sample mode")
}
func (s *MemoryStore) SaveAWBDetails(context.Context, AWBResult) error {
	return errors.New("shipment persistence is unavailable in sample mode")
}
func (s *MemoryStore) SaveEasyParcelWebhook(context.Context, EasyParcelWebhook, []byte) (bool, error) {
	return false, errors.New("shipment persistence is unavailable in sample mode")
}
func (s *MemoryStore) ListInventory(context.Context) ([]InventoryProduct, error) {
	return nil, errors.New("inventory database is not configured")
}
func (s *MemoryStore) ListInventoryLots(context.Context) ([]InventoryLot, error) {
	return nil, errors.New("inventory database is not configured")
}
func (s *MemoryStore) CreateInventoryProduct(context.Context, InventoryProduct) error {
	return errors.New("inventory database is not configured")
}
func (s *MemoryStore) ReceiveInventoryStock(context.Context, InventoryReceipt, AuthenticatedActor) (InventoryProduct, InventoryLot, StockMovement, error) {
	return InventoryProduct{}, InventoryLot{}, StockMovement{}, errors.New("inventory database is not configured")
}
func (s *MemoryStore) AdjustStock(context.Context, StockAdjustment, AuthenticatedActor) (InventoryProduct, InventoryLot, StockMovement, error) {
	return InventoryProduct{}, InventoryLot{}, StockMovement{}, errors.New("inventory database is not configured")
}
func (s *MemoryStore) ListStockMovements(context.Context, time.Time, time.Time) ([]StockMovement, error) {
	return nil, errors.New("inventory database is not configured")
}
func (s *MemoryStore) InventoryWorkbookRows(context.Context) ([]InventoryLot, error) {
	return nil, errors.New("inventory database is not configured")
}
func (s *MemoryStore) ListInventorySyncOutbox(context.Context, int) ([]InventorySyncEvent, error) {
	return nil, errors.New("inventory database is not configured")
}
func (s *MemoryStore) MarkInventorySyncSent(context.Context, int64) error {
	return errors.New("inventory database is not configured")
}
func (s *MemoryStore) MarkInventorySyncFailed(context.Context, int64, string) error {
	return errors.New("inventory database is not configured")
}
func (s *MemoryStore) ListShippingRates(context.Context) ([]ShippingRate, error) {
	return nil, errors.New("shipping rate database is not configured")
}
func (s *MemoryStore) SaveShippingRate(context.Context, ShippingRate, AuthenticatedActor) (ShippingRate, error) {
	return ShippingRate{}, errors.New("shipping rate database is not configured")
}

func nullableDate(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Format("2006-01-02")
}
