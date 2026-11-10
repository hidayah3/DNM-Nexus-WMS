package internal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// POST /api/delivery-batches/awb/print with {"order_ids":[…]} returns ONE PDF
// holding the AWB labels of every selected order, in the order given, so staff
// can print a batch in one go. (Browsers allow only one new tab per click, so
// opening a tab per AWB showed just the first one.)
//
// Orders whose AWB isn't ready are left out and listed in the
// X-WMS-AWB-Missing response header (comma-separated order IDs).

const maxAWBPrint = 100

var awbHTTP = &http.Client{Timeout: 30 * time.Second}

func (h *Handler) printAWBs(w http.ResponseWriter, r *http.Request) {
	if !requireActor(w, r) {
		return
	}
	var input struct {
		OrderIDs []string `json:"order_ids"`
	}
	if !decode(w, r, &input) {
		return
	}
	ids := make([]string, 0, len(input.OrderIDs))
	seen := map[string]bool{}
	for _, id := range input.OrderIDs {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 || len(ids) > maxAWBPrint {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("select between 1 and %d booked orders", maxAWBPrint))
		return
	}

	records, err := h.store.GetShipmentRecords(r.Context(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load saved EasyParcel shipments")
		return
	}
	urls := map[string]string{}
	var needRefresh []ShipmentRecord
	for _, rec := range records {
		if rec.AWBURL != "" {
			urls[rec.OrderID] = rec.AWBURL
		} else if rec.ShipmentNumber != "" {
			needRefresh = append(needRefresh, rec)
		}
	}
	// Ask EasyParcel for labels we don't have yet; keep going if it can't answer.
	if len(needRefresh) > 0 {
		if results, err := h.parcel.AWBDetails(r.Context(), needRefresh); err == nil {
			for _, res := range results {
				if res.AWBURL == "" {
					continue
				}
				urls[res.OrderID] = res.AWBURL
				if err := h.store.SaveAWBDetails(r.Context(), res); err != nil {
					log.Printf("save AWB %s: %v", res.OrderID, err)
				}
			}
		}
	}

	var pdfs []io.ReadSeeker
	var missing []string
	for _, id := range ids {
		u, ok := urls[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		data, err := downloadAWB(r.Context(), u)
		if err != nil {
			log.Printf("download AWB %s: %v", id, err)
			missing = append(missing, id)
			continue
		}
		pdfs = append(pdfs, bytes.NewReader(data))
	}
	if len(missing) > 0 {
		w.Header().Set("X-WMS-AWB-Missing", strings.Join(missing, ","))
		w.Header().Set("Access-Control-Expose-Headers", "X-WMS-AWB-Missing")
	}
	if len(pdfs) == 0 {
		writeError(w, http.StatusConflict, "EasyParcel has not returned AWB labels for these orders yet; try again in a minute")
		return
	}

	var out bytes.Buffer
	if len(pdfs) == 1 {
		_, _ = pdfs[0].(*bytes.Reader).WriteTo(&out)
	} else if err := api.MergeRaw(pdfs, &out, false, nil); err != nil {
		log.Printf("merge AWB PDFs: %v", err)
		writeError(w, http.StatusInternalServerError, "could not combine the AWB labels into one PDF")
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=awb-%s.pdf", time.Now().Format("20060102-1504")))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out.Bytes())
}

// downloadAWB fetches one label PDF. Only https links on easyparcel.com are
// fetched, so a bad saved URL can't make the server call anything else.
func downloadAWB(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !(u.Hostname() == "easyparcel.com" || strings.HasSuffix(u.Hostname(), ".easyparcel.com")) {
		return nil, fmt.Errorf("refusing to fetch AWB from %q", raw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := awbHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		return nil, fmt.Errorf("not a PDF")
	}
	return data, nil
}
