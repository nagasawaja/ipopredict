package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const vbkrBaseURL = "https://web-api.vbkr.com"

var errVBrokerMarginNotFound = errors.New("vbroker margin data not found")

type vBrokerMarginClient struct {
	BaseURL string
	HTTP    *http.Client
}

func newVBrokerMarginClient(httpClient *http.Client) *vBrokerMarginClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 8 * time.Second}
	}
	return &vBrokerMarginClient{
		BaseURL: vbkrBaseURL,
		HTTP:    httpClient,
	}
}

type vBrokerMarginData struct {
	IPOID            int
	SecurityCode     string
	SecurityName     string
	SecurityNameTC   string
	ApplyRate        float64
	TotalMarginMoney float64
	RaisingMoney     float64
	LastUpdateTime   string
	Brokers          []vBrokerMarginBroker
}

type vBrokerMarginBroker struct {
	BrokerName  string
	MarginMoney float64
}

type vbkrStockListResponse struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Data    struct {
		Applying  []vbkrIPORecord `json:"applying"`
		StockList []vbkrIPORecord `json:"stockList"`
	} `json:"data"`
}

type vbkrIPORecord struct {
	IPOInfo vbkrIPOInfo `json:"ipoInfo"`
}

type vbkrIPOInfo struct {
	IPOID          int    `json:"ipoId"`
	SecurityCode   string `json:"securityCode"`
	SecurityName   string `json:"securityName"`
	SecurityNameTC string `json:"securityNameTc"`
}

type vbkrMarginResponse struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Data    struct {
		TotalMarginMoney flexibleFloat      `json:"totalMarginMoney"`
		ApplyRate        flexibleFloat      `json:"applyRate"`
		RaisingMoney     flexibleFloat      `json:"raisingMoney"`
		LastUpdateTime   string             `json:"lastUpdateTime"`
		BrokerList       []vbkrMarginBroker `json:"brokerList"`
	} `json:"data"`
}

type vbkrMarginBroker struct {
	MarginMoney flexibleFloat `json:"marginMoney"`
	BrokerName  string        `json:"brokerName"`
}

type flexibleFloat float64

func (f *flexibleFloat) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		unquoted, err := strconv.Unquote(s)
		if err != nil {
			return err
		}
		s = strings.TrimSpace(unquoted)
		if s == "" {
			*f = 0
			return nil
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = flexibleFloat(v)
	return nil
}

func (c *vBrokerMarginClient) fetchByStockCode(ctx context.Context, stockCode string) (*vBrokerMarginData, error) {
	if c == nil || c.HTTP == nil {
		return nil, fmt.Errorf("vbroker margin client is nil")
	}
	wantCode := normalizeHKStockCode(stockCode)
	if wantCode == "" {
		return nil, errVBrokerMarginNotFound
	}

	ipo, err := c.findIPOInfo(ctx, wantCode)
	if err != nil {
		return nil, err
	}

	margin, err := c.fetchMarginByIPOID(ctx, ipo.IPOID)
	if err != nil {
		return nil, err
	}
	margin.IPOID = ipo.IPOID
	margin.SecurityCode = ipo.SecurityCode
	margin.SecurityName = ipo.SecurityName
	margin.SecurityNameTC = ipo.SecurityNameTC
	return margin, nil
}

func (c *vBrokerMarginClient) findIPOInfo(ctx context.Context, stockCode string) (vbkrIPOInfo, error) {
	for _, endpoint := range []string{"/ipo/hk-stock/query-applying", "/ipo/hk-stock/query-prepare-listed"} {
		records, err := c.fetchIPORecords(ctx, endpoint)
		if err != nil {
			return vbkrIPOInfo{}, err
		}
		for _, rec := range records {
			ipo := rec.IPOInfo
			if ipo.IPOID <= 0 {
				continue
			}
			if normalizeHKStockCode(ipo.SecurityCode) == stockCode {
				return ipo, nil
			}
		}
	}
	return vbkrIPOInfo{}, errVBrokerMarginNotFound
}

func (c *vBrokerMarginClient) fetchIPORecords(ctx context.Context, endpoint string) ([]vbkrIPORecord, error) {
	var out vbkrStockListResponse
	if err := c.getJSON(ctx, endpoint, nil, &out); err != nil {
		return nil, err
	}
	if !out.Success {
		return nil, fmt.Errorf("vbroker %s failed: code=%s", endpoint, out.Code)
	}
	records := out.Data.Applying
	if len(records) == 0 {
		records = out.Data.StockList
	}
	return records, nil
}

func (c *vBrokerMarginClient) fetchMarginByIPOID(ctx context.Context, ipoID int) (*vBrokerMarginData, error) {
	if ipoID <= 0 {
		return nil, errVBrokerMarginNotFound
	}
	q := url.Values{}
	q.Set("ipoId", strconv.Itoa(ipoID))
	var out vbkrMarginResponse
	if err := c.getJSON(ctx, "/ipo/hk-stock/query-margin-brokers", q, &out); err != nil {
		return nil, err
	}
	if !out.Success {
		return nil, fmt.Errorf("vbroker query-margin-brokers failed: code=%s", out.Code)
	}
	data := &vBrokerMarginData{
		ApplyRate:        float64(out.Data.ApplyRate),
		TotalMarginMoney: float64(out.Data.TotalMarginMoney),
		RaisingMoney:     float64(out.Data.RaisingMoney),
		LastUpdateTime:   out.Data.LastUpdateTime,
		Brokers:          make([]vBrokerMarginBroker, 0, len(out.Data.BrokerList)),
	}
	for _, b := range out.Data.BrokerList {
		if strings.TrimSpace(b.BrokerName) == "" {
			continue
		}
		data.Brokers = append(data.Brokers, vBrokerMarginBroker{
			BrokerName:  strings.TrimSpace(b.BrokerName),
			MarginMoney: float64(b.MarginMoney),
		})
	}
	if data.ApplyRate <= 0 && data.TotalMarginMoney <= 0 && len(data.Brokers) == 0 {
		return nil, errVBrokerMarginNotFound
	}
	return data, nil
}

func (c *vBrokerMarginClient) getJSON(ctx context.Context, endpoint string, q url.Values, out any) error {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("parse vbroker base url: %w", err)
	}
	u.Path = endpoint
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("new vbroker request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; hk_ipo/1.0)")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("vbroker http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("vbroker http status: %s", resp.Status)
	}
	dec := json.NewDecoder(resp.Body)
	return dec.Decode(out)
}

func normalizeHKStockCode(s string) string {
	s = strings.TrimSpace(strings.ToUpper(s))
	s = strings.TrimSuffix(s, ".HK")
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	code := b.String()
	if code == "" {
		return ""
	}
	if len(code) < 5 {
		code = strings.Repeat("0", 5-len(code)) + code
	}
	return code
}
