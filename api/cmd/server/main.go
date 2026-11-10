package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"wms-module/api/internal"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "user" {
		os.Exit(userCommand(os.Args[2:]))
	}
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
	orderSyncer, err := internal.NewOrderSyncer(internal.OrderSyncConfig{
		URL:            os.Getenv("ORDER_SYNC_URL"),
		Authorization:  os.Getenv("ORDER_SYNC_AUTHORIZATION"),
		ImportStatuses: strings.Split(env("ORDER_SYNC_IMPORT_STATUSES", "PAID"), ","),
		Stockist:       os.Getenv("ORDER_SYNC_STOCKIST"),
		Sender: internal.Address{
			Name:                   os.Getenv("WAREHOUSE_SENDER_NAME"),
			PhoneNumberCountryCode: env("WAREHOUSE_SENDER_PHONE_COUNTRY", "60"),
			PhoneNumber:            os.Getenv("WAREHOUSE_SENDER_PHONE"),
			Email:                  os.Getenv("WAREHOUSE_SENDER_EMAIL"),
			Address1:               os.Getenv("WAREHOUSE_SENDER_ADDRESS_1"),
			Address2:               os.Getenv("WAREHOUSE_SENDER_ADDRESS_2"),
			Postcode:               os.Getenv("WAREHOUSE_SENDER_POSTCODE"),
			City:                   os.Getenv("WAREHOUSE_SENDER_CITY"),
			SubdivisionCode:        os.Getenv("WAREHOUSE_SENDER_STATE"),
			CountryCode:            "MY",
		},
	}, store)
	if err != nil {
		log.Printf("order sync disabled: %v", err)
		orderSyncer = nil
	} else if orderSyncer == nil {
		log.Println("order sync is not configured; set ORDER_SYNC_URL and ORDER_SYNC_AUTHORIZATION to pull orders from the order API")
	} else {
		interval, err := time.ParseDuration(env("ORDER_SYNC_INTERVAL", "5m"))
		if err != nil {
			log.Printf("ORDER_SYNC_INTERVAL is invalid (%v); using 5m", err)
			interval = 5 * time.Minute
		}
		log.Printf("order sync enabled; pulling orders every %s", interval)
		go internal.RunOrderSyncWorker(context.Background(), orderSyncer, interval)
	}
	server := &http.Server{Addr: addr, Handler: internal.NewHandlerWithOrderSync(store, parcel, oauth, smsNotifier, os.Getenv("EASYPARCEL_WEBHOOK_TOKEN"), smsInventoryNotifier, orderSyncer), ReadHeaderTimeout: 5 * time.Second}
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

// userCommand adds a staff account or changes its password:
//
//	wms-api user add <username> [display name]
//
// The password is read from standard input (first line), so it never appears
// in the command line or shell history.
func userCommand(args []string) int {
	if len(args) < 2 || args[0] != "add" {
		fmt.Fprintln(os.Stderr, "usage: wms-api user add <username> [display name]   (password on standard input)")
		return 2
	}
	username := strings.TrimSpace(args[1])
	display := username
	if len(args) > 2 {
		display = strings.TrimSpace(strings.Join(args[2:], " "))
	}
	if username == "" || strings.ContainsAny(username, " \t:") {
		fmt.Fprintln(os.Stderr, "the username must not be empty or contain spaces or colons")
		return 2
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(os.Stderr, "no password received on standard input")
		return 1
	}
	hash, err := internal.HashPassword(strings.TrimRight(line, "\r\n"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is not set")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer pool.Close()
	store := internal.NewPostgresStore(pool)
	if err := store.SaveStaffUser(ctx, internal.StaffUser{Username: username, DisplayName: display, PasswordHash: hash, Role: "warehouse_admin"}); err != nil {
		fmt.Fprintln(os.Stderr, "could not save the account (is migration 007 applied?):", err)
		return 1
	}
	fmt.Printf("Saved account %q. Existing sessions for it were signed out.\n", username)
	return 0
}
