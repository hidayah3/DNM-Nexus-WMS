package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrProviderNotConfigured = errors.New("EasyParcel integration is not configured")
var ErrNoEasyParcelConnection = errors.New("EasyParcel account has not been connected")
var ErrShipmentOutcomeUnknown = errors.New("EasyParcel shipment outcome is unknown; check the EasyParcel account before retrying")

type AccessTokenSource interface {
	AccessToken(context.Context) (string, error)
}

type EasyParcel interface {
	Quotes(context.Context, ShipmentDraft) ([]CourierQuote, error)
	PushOrders(context.Context, []ShipmentSubmission) ([]PushResult, error)
	AWBDetails(context.Context, []ShipmentRecord) ([]AWBResult, error)
}

type EasyParcelConfig struct {
	BaseURL string
	Version string
}

type EasyParcelClient struct {
	tokens  AccessTokenSource
	client  *http.Client
	baseURL string
}

func NewEasyParcelClient(tokens AccessTokenSource, cfg EasyParcelConfig) (*EasyParcelClient, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = "https://api.easyparcel.com"
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("EasyParcel API base URL must be an HTTPS URL")
	}
	version := strings.Trim(strings.TrimSpace(cfg.Version), "/")
	if version == "" {
		version = "2026-09"
	}
	return &EasyParcelClient{
		tokens:  tokens,
		baseURL: base + "/open_api/" + version,
		client:  &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

type epAddressQuote struct {
	Postcode        string `json:"postcode"`
	SubdivisionCode string `json:"subdivision_code"`
	Country         string `json:"country"`
}

type epQuoteRequest struct {
	AutoApplyCoupon *bool             `json:"auto_apply_coupon,omitempty"`
	Shipment        []epQuoteShipment `json:"shipment"`
}

type epQuoteShipment struct {
	Sender      epAddressQuote `json:"sender"`
	Receiver    epAddressQuote `json:"receiver"`
	ParcelValue float64        `json:"parcel_value"`
	Weight      float64        `json:"weight"`
	Width       float64        `json:"width,omitempty"`
	Length      float64        `json:"length,omitempty"`
	Height      float64        `json:"height,omitempty"`
}

type epEnvelope struct {
	StatusCode int             `json:"status_code"`
	Message    string          `json:"message"`
	Errors     json.RawMessage `json:"errors"`
	Data       json.RawMessage `json:"data"`
}

type epQuoteResponse struct {
	epEnvelope
	Data []struct {
		Status     string `json:"status"`
		Errors     []any  `json:"errors"`
		Quotations []struct {
			Courier struct {
				ServiceID        string `json:"service_id"`
				ServiceName      string `json:"service_name"`
				CourierID        string `json:"courier_id"`
				CourierName      string `json:"courier_name"`
				DeliveryDuration any    `json:"delivery_duration"`
			} `json:"courier"`
			Pricing struct {
				Currency    string  `json:"currency"`
				TotalAmount float64 `json:"total_amount"`
			} `json:"pricing"`
		} `json:"quotations"`
	} `json:"-"`
}

func (c *EasyParcelClient) Quotes(ctx context.Context, d ShipmentDraft) ([]CourierQuote, error) {
	if err := validateDraft(d); err != nil {
		return nil, err
	}
	falseValue := false
	request := epQuoteRequest{AutoApplyCoupon: &falseValue, Shipment: []epQuoteShipment{{
		Sender:      epAddressQuote{Postcode: d.Sender.Postcode, SubdivisionCode: d.Sender.SubdivisionCode, Country: d.Sender.CountryCode},
		Receiver:    epAddressQuote{Postcode: d.Receiver.Postcode, SubdivisionCode: d.Receiver.SubdivisionCode, Country: d.Receiver.CountryCode},
		ParcelValue: d.ParcelValue, Weight: d.Weight, Width: d.Width, Length: d.Length, Height: d.Height,
	}}}
	var response struct {
		StatusCode int    `json:"status_code"`
		Message    string `json:"message"`
		Errors     []any  `json:"errors"`
		Data       []struct {
			Status     string `json:"status"`
			Errors     []any  `json:"errors"`
			Quotations []struct {
				Courier struct {
					ServiceID        string `json:"service_id"`
					ServiceName      string `json:"service_name"`
					CourierID        string `json:"courier_id"`
					CourierName      string `json:"courier_name"`
					DeliveryDuration any    `json:"delivery_duration"`
				} `json:"courier"`
				Pricing struct {
					Currency    string  `json:"currency"`
					TotalAmount float64 `json:"total_amount"`
				} `json:"pricing"`
			} `json:"quotations"`
		} `json:"data"`
	}
	if err := c.post(ctx, "/shipment/quotations", request, &response); err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, providerMessage(response.Message, response.Errors)
	}
	if len(response.Data) == 0 {
		return nil, errors.New("EasyParcel returned no quotation results")
	}
	result := response.Data[0]
	if !strings.EqualFold(result.Status, "success") {
		return nil, providerItemsError("EasyParcel could not quote this order", result.Errors)
	}
	quotes := make([]CourierQuote, 0, len(result.Quotations))
	for _, q := range result.Quotations {
		if q.Courier.ServiceID == "" {
			continue
		}
		quotes = append(quotes, CourierQuote{
			ServiceID: q.Courier.ServiceID, ServiceName: q.Courier.ServiceName,
			CourierName: q.Courier.CourierName, CourierID: q.Courier.CourierID,
			Currency: q.Pricing.Currency, TotalAmount: q.Pricing.TotalAmount,
			DeliveryDuration: printableValue(q.Courier.DeliveryDuration),
		})
	}
	return quotes, nil
}

type epSubmitRequest struct {
	AutoApplyCoupon *bool              `json:"auto_apply_coupon,omitempty"`
	Shipment        []epSubmitShipment `json:"shipment"`
}

type epSubmitShipment struct {
	Reference      string         `json:"reference"`
	ServiceID      string         `json:"service_id"`
	CollectionDate string         `json:"collection_date"`
	Weight         float64        `json:"weight"`
	Height         float64        `json:"height"`
	Length         float64        `json:"length"`
	Width          float64        `json:"width"`
	Item           []ParcelItem   `json:"item"`
	Sender         Address        `json:"sender"`
	Receiver       Address        `json:"receiver"`
	Feature        map[string]any `json:"feature"`
}

func (c *EasyParcelClient) PushOrders(ctx context.Context, submissions []ShipmentSubmission) ([]PushResult, error) {
	if len(submissions) == 0 {
		return nil, errors.New("no shipments to submit")
	}
	falseValue := false
	request := epSubmitRequest{AutoApplyCoupon: &falseValue, Shipment: make([]epSubmitShipment, 0, len(submissions))}
	for _, submission := range submissions {
		d := submission.Draft
		if err := validateDraft(d); err != nil {
			return nil, fmt.Errorf("order %s: %w", d.OrderID, err)
		}
		if strings.TrimSpace(submission.ServiceID) == "" {
			return nil, fmt.Errorf("order %s: EasyParcel service_id is required", d.OrderID)
		}
		if d.CollectionDate == "" {
			d.CollectionDate = time.Now().Format("2006-01-02")
		}
		request.Shipment = append(request.Shipment, epSubmitShipment{
			Reference: d.OrderID, ServiceID: submission.ServiceID, CollectionDate: d.CollectionDate,
			Weight: d.Weight, Height: d.Height, Length: d.Length, Width: d.Width,
			Item: d.Items, Sender: d.Sender, Receiver: d.Receiver, Feature: map[string]any{},
		})
	}
	var response struct {
		StatusCode int    `json:"status_code"`
		Message    string `json:"message"`
		Errors     []any  `json:"errors"`
		Data       []struct {
			OrderDetails struct {
				OrderNumber string `json:"order_number"`
			} `json:"order_details"`
			Shipments []struct {
				Status          string            `json:"status"`
				Reference       string            `json:"reference"`
				ReferenceTypo   string            `json:"referencce"`
				ShipmentNumber  string            `json:"shipment_number"`
				Courier         string            `json:"courier"`
				AWBNumber       string            `json:"awb_number"`
				AWBURL          string            `json:"awb_url"`
				AWBURLsByFormat map[string]string `json:"awb_urls_by_format"`
				TrackingURL     string            `json:"tracking_url"`
				Errors          []any             `json:"errors"`
				Pricing         struct {
					Currency string `json:"currency_code"`
					Total    string `json:"total_paid_amount"`
				} `json:"pricing_breakdown"`
			} `json:"shipments"`
		} `json:"data"`
	}
	if err := c.post(ctx, "/shipment/submit_orders", request, &response); err != nil {
		if errors.Is(err, ErrShipmentOutcomeUnknown) {
			return nil, err
		}
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, providerMessage(response.Message, response.Errors)
	}
	results := make([]PushResult, len(submissions))
	for i, submission := range submissions {
		results[i] = PushResult{OrderID: submission.Draft.OrderID, Status: "failed"}
		results[i].Status = "outcome_unknown"
		var data struct {
			OrderDetails struct {
				OrderNumber string `json:"order_number"`
			} `json:"order_details"`
			Shipments []struct {
				Status          string            `json:"status"`
				Reference       string            `json:"reference"`
				ReferenceTypo   string            `json:"referencce"`
				ShipmentNumber  string            `json:"shipment_number"`
				Courier         string            `json:"courier"`
				AWBNumber       string            `json:"awb_number"`
				AWBURL          string            `json:"awb_url"`
				AWBURLsByFormat map[string]string `json:"awb_urls_by_format"`
				TrackingURL     string            `json:"tracking_url"`
				Errors          []any             `json:"errors"`
				Pricing         struct {
					Currency string `json:"currency_code"`
					Total    string `json:"total_paid_amount"`
				} `json:"pricing_breakdown"`
			} `json:"shipments"`
		}
		var shipment struct {
			Status          string            `json:"status"`
			Reference       string            `json:"reference"`
			ReferenceTypo   string            `json:"referencce"`
			ShipmentNumber  string            `json:"shipment_number"`
			Courier         string            `json:"courier"`
			AWBNumber       string            `json:"awb_number"`
			AWBURL          string            `json:"awb_url"`
			AWBURLsByFormat map[string]string `json:"awb_urls_by_format"`
			TrackingURL     string            `json:"tracking_url"`
			Errors          []any             `json:"errors"`
			Pricing         struct {
				Currency string `json:"currency_code"`
				Total    string `json:"total_paid_amount"`
			} `json:"pricing_breakdown"`
		}
		found := false
		for _, candidate := range response.Data {
			for _, candidateShipment := range candidate.Shipments {
				if candidateShipment.Reference == submission.Draft.OrderID || candidateShipment.ReferenceTypo == submission.Draft.OrderID {
					data, shipment, found = candidate, candidateShipment, true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			results[i].Error = "EasyParcel did not return a conclusive result for this order; reconcile it before retrying."
			continue
		}
		if !strings.EqualFold(shipment.Status, "success") || shipment.ShipmentNumber == "" {
			results[i].Status = "failed"
			results[i].Error = safeProviderError(strings.Join(anyStrings(shipment.Errors), "; "), "EasyParcel rejected this shipment")
			continue
		}
		awbURL := shipment.AWBURL
		if awbURL == "" {
			awbURL = shipment.AWBURLsByFormat["A4"]
		}
		results[i] = PushResult{
			OrderID: submission.Draft.OrderID, Status: "created", CourierName: shipment.Courier, ProviderOrderNumber: data.OrderDetails.OrderNumber, ShipmentNumber: shipment.ShipmentNumber,
			TrackingNumber: shipment.AWBNumber, TrackingURL: shipment.TrackingURL, AWBNumber: shipment.AWBNumber,
			AWBURL: awbURL, Currency: shipment.Pricing.Currency,
			Amount: parseAmount(shipment.Pricing.Total),
		}
	}
	return results, nil
}

func (c *EasyParcelClient) AWBDetails(ctx context.Context, records []ShipmentRecord) ([]AWBResult, error) {
	results := make([]AWBResult, 0, len(records))
	for _, record := range records {
		out := AWBResult{OrderID: record.OrderID, ShipmentNumber: record.ShipmentNumber}
		if record.ShipmentNumber == "" {
			out.Error = "No EasyParcel shipment number has been saved for this order."
			results = append(results, out)
			continue
		}
		var response struct {
			StatusCode int    `json:"status_code"`
			Message    string `json:"message"`
			Data       []struct {
				ShipmentNumber  string `json:"shipment_number"`
				ShipmentDetails struct {
					AWBNumber   string `json:"awb_number"`
					AWBURL      string `json:"awb_url"`
					TrackingURL string `json:"tracking_url"`
				} `json:"shipment_details"`
			} `json:"data"`
		}
		if err := c.post(ctx, "/shipment/details", map[string]string{"shipment_number": record.ShipmentNumber}, &response); err != nil {
			out.Error = safeProviderError(err.Error(), "Could not retrieve the Air Waybill yet.")
			results = append(results, out)
			continue
		}
		if response.StatusCode != http.StatusOK || len(response.Data) == 0 {
			out.Error = safeProviderError(response.Message, "EasyParcel has not returned shipment details yet.")
			results = append(results, out)
			continue
		}
		details := response.Data[0].ShipmentDetails
		out.TrackingNumber, out.TrackingURL = details.AWBNumber, details.TrackingURL
		out.AWBNumber, out.AWBURL = details.AWBNumber, details.AWBURL
		if out.AWBURL == "" {
			out.Error = "EasyParcel has not generated an AWB for this shipment yet."
		}
		results = append(results, out)
	}
	return results, nil
}

func (c *EasyParcelClient) post(ctx context.Context, path string, input any, output any) error {
	if c.tokens == nil {
		return ErrNoEasyParcelConnection
	}
	token, err := c.tokens.AccessToken(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrShipmentOutcomeUnknown, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		if path == "/shipment/submit_orders" {
			return fmt.Errorf("%w: could not read EasyParcel response", ErrShipmentOutcomeUnknown)
		}
		return errors.New("could not read EasyParcel response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if path == "/shipment/submit_orders" && resp.StatusCode >= 500 {
			return fmt.Errorf("%w: EasyParcel returned HTTP %d", ErrShipmentOutcomeUnknown, resp.StatusCode)
		}
		var envelope epEnvelope
		_ = json.Unmarshal(data, &envelope)
		return providerMessage(envelope.Message, envelope.Errors)
	}
	if err := json.Unmarshal(data, output); err != nil {
		if path == "/shipment/submit_orders" {
			return fmt.Errorf("%w: EasyParcel returned an invalid response", ErrShipmentOutcomeUnknown)
		}
		return errors.New("EasyParcel returned an invalid response")
	}
	return nil
}

func validateDraft(d ShipmentDraft) error {
	if strings.TrimSpace(d.OrderID) == "" {
		return errors.New("order reference is required")
	}
	if !validAddress(d.Sender) {
		return errors.New("sender name, phone, address, postcode, city, subdivision and country are required")
	}
	if !validAddress(d.Receiver) {
		return errors.New("receiver name, phone, address, postcode, city, subdivision and country are required")
	}
	if d.Weight <= 0 || d.Width <= 0 || d.Length <= 0 || d.Height <= 0 {
		return errors.New("parcel weight and dimensions must be greater than zero")
	}
	if len(d.Items) == 0 {
		return errors.New("at least one parcel content item is required")
	}
	for _, item := range d.Items {
		if strings.TrimSpace(item.Content) == "" || item.Weight <= 0 || item.Quantity <= 0 || item.CurrencyCode == "" {
			return errors.New("each parcel item needs content, positive weight and quantity, and a currency code")
		}
	}
	return nil
}

func validAddress(a Address) bool {
	return strings.TrimSpace(a.Name) != "" && strings.TrimSpace(a.PhoneNumber) != "" &&
		strings.TrimSpace(a.Address1) != "" && strings.TrimSpace(a.Postcode) != "" &&
		strings.TrimSpace(a.City) != "" && strings.TrimSpace(a.SubdivisionCode) != "" &&
		strings.TrimSpace(a.CountryCode) != "" && strings.TrimSpace(a.PhoneNumberCountryCode) != ""
}

func providerMessage(message string, details any) error {
	clean := safeProviderError(message, "EasyParcel request was not accepted")
	if details != nil {
		if parts := anyStrings(details); len(parts) > 0 {
			clean += ": " + strings.Join(parts, "; ")
		}
	}
	return errors.New(clean)
}

func providerItemsError(message string, details []any) error {
	return providerMessage(message, details)
}

func anyStrings(v any) []string {
	var raw []any
	switch x := v.(type) {
	case []any:
		raw = x
	default:
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func safeProviderError(message, fallback string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return fallback
	}
	if len(message) > 350 {
		message = message[:350]
	}
	return message
}

func printableValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return fmt.Sprintf("%.0f", x)
	default:
		return ""
	}
}

func parseAmount(s string) float64 {
	var amount float64
	_, _ = fmt.Sscanf(s, "%f", &amount)
	return amount
}

type EasyParcelUnavailable struct{}

func (EasyParcelUnavailable) Quotes(context.Context, ShipmentDraft) ([]CourierQuote, error) {
	return nil, ErrProviderNotConfigured
}
func (EasyParcelUnavailable) PushOrders(context.Context, []ShipmentSubmission) ([]PushResult, error) {
	return nil, ErrProviderNotConfigured
}
func (EasyParcelUnavailable) AWBDetails(context.Context, []ShipmentRecord) ([]AWBResult, error) {
	return nil, ErrProviderNotConfigured
}
