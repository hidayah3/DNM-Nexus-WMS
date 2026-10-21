package internal

import "time"

type Order struct {
	ID                 string    `json:"id"`
	Date               time.Time `json:"date"`
	Customer           string    `json:"customer"`
	Items              string    `json:"items"`
	Status             string    `json:"status"`
	Method             string    `json:"method"`
	Courier            string    `json:"courier"`
	CourierCode        string    `json:"courier_code"`
	Tracking           string    `json:"tracking"`
	ShipmentNo         string    `json:"shipment_number,omitempty"`
	ProviderStatus     string    `json:"provider_status,omitempty"`
	TrackingStatus     string    `json:"tracking_status,omitempty"`
	TrackingStatusCode *int      `json:"tracking_status_code,omitempty"`
	AWBURL             string    `json:"awb_url,omitempty"`
}

// IncomingOrder is the server-to-server snapshot SMS sends to WMS.
type IncomingOrder struct {
	ID       string        `json:"id"`
	Date     time.Time     `json:"date"`
	Customer string        `json:"customer"`
	Items    string        `json:"items"`
	Status   string        `json:"status"`
	Shipment ShipmentDraft `json:"shipment"`
}

type Fulfilment struct {
	Method      string `json:"method"`
	CourierCode string `json:"courier_code"`
}

type InventoryProduct struct {
	SKU      string `json:"sku"`
	Name     string `json:"name"`
	Quantity int64  `json:"quantity"`
}

// InventoryLot is an auditable batch of stock with its own expiry date.
type InventoryLot struct {
	ID          int64      `json:"id"`
	SKU         string     `json:"sku"`
	ProductName string     `json:"product_name"`
	BatchNumber string     `json:"batch_number"`
	ExpiryDate  *time.Time `json:"expiry_date,omitempty"`
	Quantity    int64      `json:"quantity"`
	CreatedBy   string     `json:"created_by"`
	CreatedName string     `json:"created_name"`
	CreatedAt   time.Time  `json:"created_at"`
}

type InventoryReceipt struct {
	SKU         string `json:"sku"`
	Name        string `json:"name"`
	BatchNumber string `json:"batch_number"`
	ExpiryDate  string `json:"expiry_date"`
	Quantity    int64  `json:"quantity"`
	Reason      string `json:"reason"`
}

type InventorySyncEvent struct {
	ID        int64     `json:"id"`
	SKU       string    `json:"sku"`
	Name      string    `json:"product_name"`
	Quantity  int64     `json:"quantity"`
	CreatedAt time.Time `json:"created_at"`
}

type ShippingRate struct {
	Region        string    `json:"region"`
	WeightKG      int       `json:"weight_kg"`
	CourierCharge float64   `json:"courier_charge"`
	CourierTax    float64   `json:"courier_tax"`
	PackagingCost float64   `json:"packaging_cost"`
	UpdatedBy     string    `json:"updated_by,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

type StockMovement struct {
	ID          int64     `json:"id"`
	SKU         string    `json:"sku"`
	ProductName string    `json:"product_name"`
	BatchNumber string    `json:"batch_number"`
	ExpiryDate  string    `json:"expiry_date,omitempty"`
	Type        string    `json:"type"`
	Quantity    int64     `json:"quantity"`
	StockAfter  int64     `json:"stock_after"`
	Reason      string    `json:"reason"`
	ActorID     string    `json:"actor_id"`
	ActorName   string    `json:"actor_name"`
	CreatedAt   time.Time `json:"created_at"`
}

type StockAdjustment struct {
	LotID    int64  `json:"lot_id"`
	Type     string `json:"type"`
	Quantity int64  `json:"quantity"`
	Reason   string `json:"reason"`
}

type AuthenticatedActor struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type Address struct {
	Name                            string `json:"name"`
	Company                         string `json:"company,omitempty"`
	PhoneNumberCountryCode          string `json:"phone_number_country_code"`
	PhoneNumber                     string `json:"phone_number"`
	AlternatePhoneNumberCountryCode string `json:"alternate_phone_number_country_code,omitempty"`
	AlternatePhoneNumber            string `json:"alternate_phone_number,omitempty"`
	Email                           string `json:"email,omitempty"`
	Address1                        string `json:"address_1"`
	Address2                        string `json:"address_2,omitempty"`
	Address3                        string `json:"address_3,omitempty"`
	Address4                        string `json:"address_4,omitempty"`
	Postcode                        string `json:"postcode"`
	City                            string `json:"city"`
	SubdivisionCode                 string `json:"subdivision_code"`
	CountryCode                     string `json:"country_code"`
}

type ParcelItem struct {
	Content      string  `json:"content"`
	Weight       float64 `json:"weight"`
	Height       float64 `json:"height"`
	Length       float64 `json:"length"`
	Width        float64 `json:"width"`
	CurrencyCode string  `json:"currency_code"`
	Value        float64 `json:"value"`
	Quantity     int     `json:"quantity"`
}

type ShipmentDraft struct {
	OrderID        string       `json:"order_id"`
	Reference      string       `json:"reference"`
	CollectionDate string       `json:"collection_date"`
	Sender         Address      `json:"sender"`
	Receiver       Address      `json:"receiver"`
	Weight         float64      `json:"weight"`
	Height         float64      `json:"height"`
	Length         float64      `json:"length"`
	Width          float64      `json:"width"`
	ParcelValue    float64      `json:"parcel_value"`
	CurrencyCode   string       `json:"currency_code"`
	Items          []ParcelItem `json:"item"`
}

type CourierQuote struct {
	ServiceID        string  `json:"service_id"`
	ServiceName      string  `json:"service_name"`
	CourierName      string  `json:"courier_name"`
	CourierID        string  `json:"courier_id"`
	Currency         string  `json:"currency"`
	TotalAmount      float64 `json:"total_amount"`
	DeliveryDuration string  `json:"delivery_duration,omitempty"`
}

type PushOrder struct {
	OrderID   string `json:"order_id"`
	ServiceID string `json:"service_id"`
}

type ShipmentSubmission struct {
	Draft     ShipmentDraft
	ServiceID string
}

type ShipmentRecord struct {
	OrderID            string     `json:"order_id"`
	ServiceID          string     `json:"service_id"`
	Status             string     `json:"status"`
	ProviderStatus     string     `json:"provider_status,omitempty"`
	ProviderStatusCode *int       `json:"provider_status_code,omitempty"`
	TrackingStatus     string     `json:"tracking_status,omitempty"`
	TrackingStatusCode *int       `json:"tracking_status_code,omitempty"`
	ProviderEventAt    *time.Time `json:"provider_event_at,omitempty"`
	ShipmentNumber     string     `json:"shipment_number"`
	OrderNumber        string     `json:"order_number,omitempty"`
	TrackingNumber     string     `json:"tracking_number,omitempty"`
	TrackingURL        string     `json:"tracking_url,omitempty"`
	AWBNumber          string     `json:"awb_number,omitempty"`
	AWBURL             string     `json:"awb_url,omitempty"`
	Currency           string     `json:"currency,omitempty"`
	Amount             float64    `json:"amount,omitempty"`
	Error              string     `json:"error,omitempty"`
}

type PushResult struct {
	OrderID             string  `json:"order_id"`
	Status              string  `json:"status"`
	CourierName         string  `json:"courier_name,omitempty"`
	ProviderOrderNumber string  `json:"provider_order_number,omitempty"`
	ShipmentNumber      string  `json:"shipment_number,omitempty"`
	TrackingNumber      string  `json:"tracking_number,omitempty"`
	TrackingURL         string  `json:"tracking_url,omitempty"`
	AWBNumber           string  `json:"awb_number,omitempty"`
	AWBURL              string  `json:"awb_url,omitempty"`
	Currency            string  `json:"currency,omitempty"`
	Amount              float64 `json:"amount,omitempty"`
	Error               string  `json:"error,omitempty"`
}

type AWBResult struct {
	OrderID        string `json:"order_id"`
	ShipmentNumber string `json:"shipment_number"`
	TrackingNumber string `json:"tracking_number,omitempty"`
	TrackingURL    string `json:"tracking_url,omitempty"`
	AWBNumber      string `json:"awb_number,omitempty"`
	AWBURL         string `json:"awb_url,omitempty"`
	Error          string `json:"error,omitempty"`
}

// EasyParcelWebhook is normalized from EasyParcel's shipment webhook topics.
// The raw JSON payload is retained in PostgreSQL for audit and future remapping.
type EasyParcelWebhook struct {
	EventKey           string
	Topic              string
	ShipmentNumber     string
	ProviderStatus     string
	ProviderStatusCode *int
	TrackingStatus     string
	TrackingStatusCode *int
	TrackingNumber     string
	TrackingURL        string
	AWBNumber          string
	AWBURL             string
	OccurredAt         time.Time
}
