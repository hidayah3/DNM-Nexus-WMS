package internal

import (
	"context"
	"log"
	"time"
)

// RunInventorySyncWorker drains the durable PostgreSQL outbox. Failed sends
// are retained and retried with bounded exponential backoff by the store.
func RunInventorySyncWorker(ctx context.Context, store Store, notifier *SMSInventoryNotifier) {
	if notifier == nil {
		log.Println("SMS inventory sync is not configured; stock changes will remain queued until SMS_INVENTORY_SYNC_URL and SMS_INVENTORY_SYNC_TOKEN are set")
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := drainInventoryOutbox(ctx, store, notifier); err != nil && ctx.Err() == nil {
			log.Printf("inventory outbox drain failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func drainInventoryOutbox(ctx context.Context, store Store, notifier *SMSInventoryNotifier) error {
	events, err := store.ListInventorySyncOutbox(ctx, 25)
	if err != nil {
		return err
	}
	for _, event := range events {
		if err := notifier.Send(ctx, event); err != nil {
			if markErr := store.MarkInventorySyncFailed(ctx, event.ID, err.Error()); markErr != nil {
				log.Printf("could not schedule retry for inventory event %d: %v", event.ID, markErr)
			}
			continue
		}
		if err := store.MarkInventorySyncSent(ctx, event.ID); err != nil {
			return err
		}
	}
	return nil
}
