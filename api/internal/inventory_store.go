package internal

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) ListInventory(ctx context.Context) ([]InventoryProduct, error) {
	rows, err := s.pool.Query(ctx, `SELECT sku,name,quantity FROM wms_inventory_product ORDER BY name,sku`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InventoryProduct{}
	for rows.Next() {
		var p InventoryProduct
		if err := rows.Scan(&p.SKU, &p.Name, &p.Quantity); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ListInventoryLots(ctx context.Context) ([]InventoryLot, error) {
	rows, err := s.pool.Query(ctx, `SELECT l.id,l.sku,p.name,l.batch_number,
		COALESCE(to_char(l.expiry_date,'YYYY-MM-DD'),''),l.quantity,l.created_by,l.created_name,l.created_at
		FROM wms_inventory_lot l JOIN wms_inventory_product p USING(sku)
		ORDER BY p.name,l.expiry_date NULLS LAST,l.batch_number,l.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InventoryLot{}
	for rows.Next() {
		var lot InventoryLot
		var expiry string
		if err := rows.Scan(&lot.ID, &lot.SKU, &lot.ProductName, &lot.BatchNumber, &expiry, &lot.Quantity, &lot.CreatedBy, &lot.CreatedName, &lot.CreatedAt); err != nil {
			return nil, err
		}
		if expiry != "" {
			d, err := time.Parse("2006-01-02", expiry)
			if err != nil {
				return nil, err
			}
			lot.ExpiryDate = &d
		}
		out = append(out, lot)
	}
	return out, rows.Err()
}

// CreateInventoryProduct is retained for catalog-only imports. Warehouse
// receipts should use ReceiveInventoryStock so stock, lot, audit and SMS
// outbox are committed together.
func (s *PostgresStore) CreateInventoryProduct(ctx context.Context, p InventoryProduct) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO wms_inventory_product(sku,name,quantity) VALUES($1,$2,0)`, strings.TrimSpace(p.SKU), strings.TrimSpace(p.Name))
	return err
}

func (s *PostgresStore) ReceiveInventoryStock(ctx context.Context, receipt InventoryReceipt, actor AuthenticatedActor) (InventoryProduct, InventoryLot, StockMovement, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return InventoryProduct{}, InventoryLot{}, StockMovement{}, err
	}
	defer tx.Rollback(ctx)

	var p InventoryProduct
	err = tx.QueryRow(ctx, `INSERT INTO wms_inventory_product(sku,name,quantity) VALUES($1,$2,0)
		ON CONFLICT(sku) DO UPDATE SET name=wms_inventory_product.name
		WHERE lower(wms_inventory_product.name)=lower(EXCLUDED.name)
		RETURNING sku,name,quantity`, strings.TrimSpace(receipt.SKU), strings.TrimSpace(receipt.Name)).Scan(&p.SKU, &p.Name, &p.Quantity)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, InventoryLot{}, StockMovement{}, UserError("this SKU already belongs to a different product name")
	}
	if err != nil {
		return p, InventoryLot{}, StockMovement{}, err
	}

	var lot InventoryLot
	var expiry string
	err = tx.QueryRow(ctx, `INSERT INTO wms_inventory_lot(sku,batch_number,expiry_date,quantity,created_by,created_name)
		VALUES($1,$2,$3::date,$4,$5,$6)
		ON CONFLICT(sku,batch_number) DO UPDATE SET expiry_date=EXCLUDED.expiry_date,
		quantity=wms_inventory_lot.quantity+EXCLUDED.quantity,updated_at=now()
		WHERE wms_inventory_lot.expiry_date=EXCLUDED.expiry_date
		RETURNING id,sku,batch_number,COALESCE(to_char(expiry_date,'YYYY-MM-DD'),''),quantity,created_by,created_name,created_at`,
		p.SKU, strings.TrimSpace(receipt.BatchNumber), receipt.ExpiryDate, receipt.Quantity, actor.ID, actor.Name).
		Scan(&lot.ID, &lot.SKU, &lot.BatchNumber, &expiry, &lot.Quantity, &lot.CreatedBy, &lot.CreatedName, &lot.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, lot, StockMovement{}, UserError("this batch number already exists with a different expiry date")
	}
	if err != nil {
		return p, lot, StockMovement{}, err
	}
	if expiry != "" {
		d, parseErr := time.Parse("2006-01-02", expiry)
		if parseErr != nil {
			return p, lot, StockMovement{}, parseErr
		}
		lot.ExpiryDate = &d
	}
	lot.ProductName = p.Name

	if err = tx.QueryRow(ctx, `UPDATE wms_inventory_product SET quantity=quantity+$2,updated_at=now() WHERE sku=$1 RETURNING quantity`, p.SKU, receipt.Quantity).Scan(&p.Quantity); err != nil {
		return p, lot, StockMovement{}, err
	}
	m, err := insertInventoryMovement(ctx, tx, p, lot, "add", receipt.Quantity, strings.TrimSpace(receipt.Reason), actor)
	if err != nil {
		return p, lot, m, err
	}
	if err = enqueueInventorySnapshot(ctx, tx, p); err != nil {
		return p, lot, m, err
	}
	if err = tx.Commit(ctx); err != nil {
		return p, lot, m, err
	}
	return p, lot, m, nil
}

func (s *PostgresStore) AdjustStock(ctx context.Context, a StockAdjustment, actor AuthenticatedActor) (InventoryProduct, InventoryLot, StockMovement, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return InventoryProduct{}, InventoryLot{}, StockMovement{}, err
	}
	defer tx.Rollback(ctx)

	var p InventoryProduct
	var lot InventoryLot
	var expiry string
	err = tx.QueryRow(ctx, `SELECT p.sku,p.name,p.quantity,l.id,l.batch_number,
		COALESCE(to_char(l.expiry_date,'YYYY-MM-DD'),''),l.quantity,l.created_by,l.created_name,l.created_at
		FROM wms_inventory_lot l JOIN wms_inventory_product p USING(sku) WHERE l.id=$1 FOR UPDATE OF l,p`, a.LotID).
		Scan(&p.SKU, &p.Name, &p.Quantity, &lot.ID, &lot.BatchNumber, &expiry, &lot.Quantity, &lot.CreatedBy, &lot.CreatedName, &lot.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, lot, StockMovement{}, ErrNotFound
	}
	if err != nil {
		return p, lot, StockMovement{}, err
	}
	lot.SKU, lot.ProductName = p.SKU, p.Name
	if expiry != "" {
		d, parseErr := time.Parse("2006-01-02", expiry)
		if parseErr != nil {
			return p, lot, StockMovement{}, parseErr
		}
		lot.ExpiryDate = &d
	}
	if a.Type == "reduce" && lot.Quantity < a.Quantity {
		return p, lot, StockMovement{}, UserError("reduction exceeds the selected batch quantity")
	}
	if a.Type == "add" {
		lot.Quantity += a.Quantity
		p.Quantity += a.Quantity
	} else {
		lot.Quantity -= a.Quantity
		p.Quantity -= a.Quantity
	}
	if _, err = tx.Exec(ctx, `UPDATE wms_inventory_lot SET quantity=$2,updated_at=now() WHERE id=$1`, lot.ID, lot.Quantity); err != nil {
		return p, lot, StockMovement{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE wms_inventory_product SET quantity=$2,updated_at=now() WHERE sku=$1`, p.SKU, p.Quantity); err != nil {
		return p, lot, StockMovement{}, err
	}
	m, err := insertInventoryMovement(ctx, tx, p, lot, a.Type, a.Quantity, strings.TrimSpace(a.Reason), actor)
	if err != nil {
		return p, lot, m, err
	}
	if err = enqueueInventorySnapshot(ctx, tx, p); err != nil {
		return p, lot, m, err
	}
	if err = tx.Commit(ctx); err != nil {
		return p, lot, m, err
	}
	return p, lot, m, nil
}

func insertInventoryMovement(ctx context.Context, tx pgx.Tx, p InventoryProduct, lot InventoryLot, kind string, quantity int64, reason string, actor AuthenticatedActor) (StockMovement, error) {
	var m StockMovement
	var expiry string
	err := tx.QueryRow(ctx, `INSERT INTO wms_inventory_movement(sku,lot_id,batch_number,expiry_date,movement_type,quantity,stock_after,reason,actor_id,actor_name)
		VALUES($1,$2,$3,$4::date,$5,$6,$7,$8,$9,$10)
		RETURNING id,sku,movement_type,quantity,stock_after,reason,actor_id,actor_name,created_at,COALESCE(to_char(expiry_date,'YYYY-MM-DD'),'')`,
		p.SKU, lot.ID, lot.BatchNumber, lot.ExpiryDate, kind, quantity, p.Quantity, reason, actor.ID, actor.Name).
		Scan(&m.ID, &m.SKU, &m.Type, &m.Quantity, &m.StockAfter, &m.Reason, &m.ActorID, &m.ActorName, &m.CreatedAt, &expiry)
	m.ProductName, m.BatchNumber, m.ExpiryDate = p.Name, lot.BatchNumber, expiry
	return m, err
}

func enqueueInventorySnapshot(ctx context.Context, tx pgx.Tx, product InventoryProduct) error {
	_, err := tx.Exec(ctx, `INSERT INTO wms_inventory_sync_outbox(sku,product_name,quantity)
		VALUES($1,$2,$3)`, product.SKU, product.Name, product.Quantity)
	return err
}

func (s *PostgresStore) ListStockMovements(ctx context.Context, from, to time.Time) ([]StockMovement, error) {
	rows, err := s.pool.Query(ctx, `SELECT m.id,m.sku,p.name,COALESCE(m.batch_number,''),COALESCE(to_char(m.expiry_date,'YYYY-MM-DD'),''),
		m.movement_type,m.quantity,m.stock_after,m.reason,m.actor_id,m.actor_name,m.created_at
		FROM wms_inventory_movement m JOIN wms_inventory_product p USING(sku) WHERE m.created_at >= $1 AND m.created_at < $2 ORDER BY m.created_at DESC,m.id DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StockMovement{}
	for rows.Next() {
		var m StockMovement
		if err := rows.Scan(&m.ID, &m.SKU, &m.ProductName, &m.BatchNumber, &m.ExpiryDate, &m.Type, &m.Quantity, &m.StockAfter, &m.Reason, &m.ActorID, &m.ActorName, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *PostgresStore) InventoryWorkbookRows(ctx context.Context) ([]InventoryLot, error) {
	return s.ListInventoryLots(ctx)
}

func (s *PostgresStore) ListInventorySyncOutbox(ctx context.Context, limit int) ([]InventorySyncEvent, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,sku,product_name,quantity,created_at FROM wms_inventory_sync_outbox
		WHERE sent_at IS NULL AND next_attempt_at<=now() ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InventorySyncEvent{}
	for rows.Next() {
		var event InventorySyncEvent
		if err := rows.Scan(&event.ID, &event.SKU, &event.Name, &event.Quantity, &event.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *PostgresStore) MarkInventorySyncSent(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE wms_inventory_sync_outbox SET sent_at=now(),last_error=NULL WHERE id=$1`, id)
	return err
}

func (s *PostgresStore) MarkInventorySyncFailed(ctx context.Context, id int64, message string) error {
	_, err := s.pool.Exec(ctx, `UPDATE wms_inventory_sync_outbox SET attempts=attempts+1,last_error=$2,
		next_attempt_at=now()+LEAST(interval '1 hour',interval '10 seconds'*power(2,LEAST(attempts,8))) WHERE id=$1`, id, message)
	return err
}

func (s *PostgresStore) ListShippingRates(ctx context.Context) ([]ShippingRate, error) {
	rows, err := s.pool.Query(ctx, `SELECT region,weight_kg,courier_charge,courier_tax,packaging_cost,COALESCE(updated_by,''),updated_at FROM wms_shipping_rate ORDER BY region,weight_kg`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShippingRate{}
	for rows.Next() {
		var r ShippingRate
		if err := rows.Scan(&r.Region, &r.WeightKG, &r.CourierCharge, &r.CourierTax, &r.PackagingCost, &r.UpdatedBy, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PostgresStore) SaveShippingRate(ctx context.Context, r ShippingRate, actor AuthenticatedActor) (ShippingRate, error) {
	err := s.pool.QueryRow(ctx, `INSERT INTO wms_shipping_rate(region,weight_kg,courier_charge,courier_tax,packaging_cost,updated_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,now()) ON CONFLICT(region,weight_kg) DO UPDATE SET courier_charge=EXCLUDED.courier_charge,courier_tax=EXCLUDED.courier_tax,packaging_cost=EXCLUDED.packaging_cost,updated_by=EXCLUDED.updated_by,updated_at=now() RETURNING region,weight_kg,courier_charge,courier_tax,packaging_cost,updated_by,updated_at`, r.Region, r.WeightKG, r.CourierCharge, r.CourierTax, r.PackagingCost, actor.ID).Scan(&r.Region, &r.WeightKG, &r.CourierCharge, &r.CourierTax, &r.PackagingCost, &r.UpdatedBy, &r.UpdatedAt)
	return r, err
}
