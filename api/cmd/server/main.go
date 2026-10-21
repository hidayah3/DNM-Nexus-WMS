package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"wms-module/api/internal"
)

func main() {
	addr := env("WMS_ADDR", "127.0.0.1:8080")
	dbURL := os.Getenv("DATABASE_URL")
	var store internal.Store
	var pool *pgxpool.Pool
	if dbURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		var err error
		pool, err = pgxpool.New(ctx, dbURL)
		cancel()
		if err != nil {
			log.Fatal(err)
		}
		defer pool.Close()
		store = internal.NewPostgresStore(pool)
	} else {
		log.Println("DATABASE_URL is empty; starting with an empty order store. Configure the SMS order repository before operational use.")
		store = internal.NewMemoryStore()
	}

	var parcel internal.EasyParcel = internal.EasyParcelUnavailable{}
	var oauth *internal.EasyParcelOAuth
	if pool != nil {
		clientID := os.Getenv("EASYPARCEL_CLIENT_ID")
		clientSecret := os.Getenv("EASYPARCEL_CLIENT_SECRET")
		redirectURI := os.Getenv("EASYPARCEL_REDIRECT_URI")
		tokenKey := os.Getenv("WMS_TOKEN_ENCRYPTION_KEY")
		// A callback URL and token key may be prepared before EasyParcel app
		// credentials are entered. Keep shipment endpoints disabled until the
		// client ID and secret are both present; validate the full configuration
		// only when an EasyParcel app has actually been configured.
		if clientID != "" || clientSecret != "" {
			if clientID == "" || clientSecret == "" || redirectURI == "" || tokenKey == "" {
				log.Fatal("EasyParcel OAuth setup is incomplete; set client ID, client secret, redirect URI and token encryption key")
			}
			tokenStore, err := internal.NewPostgresTokenStore(pool, tokenKey)
			if err != nil {
				log.Fatal(err)
			}
			oauth, err = internal.NewEasyParcelOAuth(internal.EasyParcelOAuthConfig{
				ClientID: clientID, ClientSecret: clientSecret, RedirectURI: redirectURI,
				CookieSecure: envBool("EASYPARCEL_COOKIE_SECURE", true),
			}, tokenStore)
			if err != nil {
				log.Fatal(err)
			}
			parcel, err = internal.NewEasyParcelClient(oauth, internal.EasyParcelConfig{
				BaseURL: os.Getenv("EASYPARCEL_API_BASE_URL"), Version: os.Getenv("EASYPARCEL_API_VERSION"),
			})
			if err != nil {
				log.Fatal(err)
			}
			log.Println("EasyParcel OAuth and API adapter configured; connect the account from the WMS before use")
		} else {
			log.Println("EasyParcel is not configured; shipment endpoints will remain disabled")
		}
	}
	smsNotifier, err := internal.NewSMSShipmentNotifier(os.Getenv("SMS_SHIPMENT_UPDATE_URL"), os.Getenv("SMS_SHIPMENT_UPDATE_TOKEN"))
	if err != nil {
		log.Printf("SMS shipment update button disabled: %v", err)
		smsNotifier = nil
	} else if smsNotifier == nil {
		log.Println("SMS shipment update callback is not configured; set SMS_SHIPMENT_UPDATE_URL and SMS_SHIPMENT_UPDATE_TOKEN to enable the portal button")
	}
	smsInventoryNotifier, err := internal.NewSMSInventoryNotifier(os.Getenv("SMS_INVENTORY_SYNC_URL"), os.Getenv("SMS_INVENTORY_SYNC_TOKEN"))
	if err != nil {
		log.Printf("SMS inventory sync disabled: %v", err)
		smsInventoryNotifier = nil
	} else if smsInventoryNotifier == nil {
		log.Println("SMS inventory sync endpoint is not configured; committed stock snapshots will stay queued")
	} else if pool == nil {
		log.Println("SMS inventory sync disabled because PostgreSQL is not configured")
		smsInventoryNotifier = nil
	} else {
		go internal.RunInventorySyncWorker(context.Background(), store, smsInventoryNotifier)
	}
	server := &http.Server{Addr: addr, Handler: internal.NewHandler(store, parcel, oauth, smsNotifier, os.Getenv("EASYPARCEL_WEBHOOK_TOKEN"), smsInventoryNotifier), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("WMS API listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes"
}
