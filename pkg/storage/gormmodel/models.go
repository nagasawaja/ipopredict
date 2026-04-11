package gormmodel

import "time"

// Core
type Stock struct {
	ID uint64 `gorm:"column:id;primaryKey;autoIncrement"`

	StockCode string `gorm:"column:stock_code;uniqueIndex:uk_stocks_stock_code"`
	HkSymbol  string `gorm:"column:hk_symbol"`
	StockName string `gorm:"column:stock_name"`

	ReferenceCompany string `gorm:"column:reference_company"`

	SourceProvider      string `gorm:"column:source_provider"`
	SourceRequestSymbol string `gorm:"column:source_request_symbol"`
	SourceUrl           string `gorm:"column:source_url"`
	SourceFetchedAt     int64  `gorm:"column:source_fetched_at"`

	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (Stock) TableName() string { return "stocks" }

// 1:1 tables
type StockOffering struct {
	StockID uint64 `gorm:"column:stock_id;primaryKey"`
	Stock   Stock  `gorm:"foreignKey:StockID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`

	OfferPriceLow  float64 `gorm:"column:offer_price_low"`
	OfferPriceHigh float64 `gorm:"column:offer_price_high"`
	OfferPrice     float64 `gorm:"column:offer_price"`

	LotSize int `gorm:"column:lot_size"`

	GlobalOfferShares        int64 `gorm:"column:global_offer_shares"`
	PublicOfferShares        int64 `gorm:"column:public_offer_shares"`
	InternationalOfferShares int64 `gorm:"column:international_offer_shares"`

	ApplyStartDate *time.Time `gorm:"column:apply_start_date"`
	ApplyEndDate   *time.Time `gorm:"column:apply_end_date"`
	ListDate       *time.Time `gorm:"column:list_date"`

	AdmissionFeeHkd float64 `gorm:"column:admission_fee_hkd"`
	MarketCapHkd    float64 `gorm:"column:market_cap_hkd"`
	Pe              float64 `gorm:"column:pe"`

	ProspectusUrl string `gorm:"column:prospectus_url"`

	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (StockOffering) TableName() string { return "stock_offerings" }

type StockCompany struct {
	StockID uint64 `gorm:"column:stock_id;primaryKey"`
	Stock   Stock  `gorm:"foreignKey:StockID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`

	Address        string `gorm:"column:address"`
	Registrar      string `gorm:"column:registrar"`
	RegistrarPhone string `gorm:"column:registrar_phone"`
	Chairman       string `gorm:"column:chairman"`
	Phone          string `gorm:"column:phone"`
	Business       string `gorm:"column:business"`

	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (StockCompany) TableName() string { return "stock_companies" }

type StockGreenShoe struct {
	StockID uint64 `gorm:"column:stock_id;primaryKey"`
	Stock   Stock  `gorm:"foreignKey:StockID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`

	RatePct      float64 `gorm:"column:rate_pct"`
	AmountShares int64   `gorm:"column:amount_shares"`
	AmountText   string  `gorm:"column:amount_text"`

	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (StockGreenShoe) TableName() string { return "stock_greenshoe" }

type StockGreyMarket struct {
	StockID uint64 `gorm:"column:stock_id;primaryKey"`
	Stock   Stock  `gorm:"foreignKey:StockID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`

	GreyDate     *time.Time `gorm:"column:grey_date"`
	IncrRatePct  float64    `gorm:"column:incr_rate_pct"`
	IncrRatePct2 float64    `gorm:"column:incr_rate_pct2"`

	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (StockGreyMarket) TableName() string { return "stock_grey_market" }

type StockPerformance struct {
	StockID uint64 `gorm:"column:stock_id;primaryKey"`
	Stock   Stock  `gorm:"foreignKey:StockID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`

	FirstDayIncrRatePct float64 `gorm:"column:first_day_incr_rate_pct"`
	TotalIncrRatePct    float64 `gorm:"column:total_incr_rate_pct"`

	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (StockPerformance) TableName() string { return "stock_performance" }

type StockRaiseMoney struct {
	StockID uint64 `gorm:"column:stock_id;primaryKey"`
	Stock   Stock  `gorm:"foreignKey:StockID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`

	AmountHkd  float64 `gorm:"column:amount_hkd"`
	AmountText string  `gorm:"column:amount_text"`

	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (StockRaiseMoney) TableName() string { return "stock_fundraising" }

// Intermediaries (N:M)
type Intermediary struct {
	ID   uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	Name string `gorm:"column:name;uniqueIndex:uk_intermediaries_name"`

	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (Intermediary) TableName() string { return "intermediaries" }

type StockIntermediary struct {
	StockID        uint64       `gorm:"column:stock_id;primaryKey"`
	IntermediaryID uint64       `gorm:"column:intermediary_id;primaryKey"`
	Role           string       `gorm:"column:role;primaryKey"`
	Seq            int          `gorm:"column:seq"`
	Stock          Stock        `gorm:"foreignKey:StockID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	Intermediary   Intermediary `gorm:"foreignKey:IntermediaryID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;"`

	CreatedAt time.Time `gorm:"column:created_at"`
}

func (StockIntermediary) TableName() string { return "stock_intermediaries" }

// Arrays
type StockCompanySecretary struct {
	ID      uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	StockID uint64 `gorm:"column:stock_id;index;uniqueIndex:uk_stock_secretaries"`
	Seq     int    `gorm:"column:seq;uniqueIndex:uk_stock_secretaries"`
	Name    string `gorm:"column:name"`
	Stock   Stock  `gorm:"foreignKey:StockID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`

	CreatedAt time.Time `gorm:"column:created_at"`
}

func (StockCompanySecretary) TableName() string { return "stock_secretaries" }

type StockMajorShareholder struct {
	ID      uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	StockID uint64 `gorm:"column:stock_id;index;uniqueIndex:uk_stock_major_shareholders"`
	Seq     int    `gorm:"column:seq;uniqueIndex:uk_stock_major_shareholders"`

	Name    *string  `gorm:"column:name"`
	Pct     *float64 `gorm:"column:pct"`
	RawText string   `gorm:"column:raw_text"`
	Stock   Stock    `gorm:"foreignKey:StockID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`

	CreatedAt time.Time `gorm:"column:created_at"`
}

func (StockMajorShareholder) TableName() string { return "stock_major_shareholders" }

type StockUseOfProceeds struct {
	ID      uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	StockID uint64 `gorm:"column:stock_id;index;uniqueIndex:uk_stock_use_of_proceeds"`
	Seq     int    `gorm:"column:seq;uniqueIndex:uk_stock_use_of_proceeds"`
	Text    string `gorm:"column:text"`
	Stock   Stock  `gorm:"foreignKey:StockID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`

	CreatedAt time.Time `gorm:"column:created_at"`
}

func (StockUseOfProceeds) TableName() string { return "stock_use_of_proceeds" }

type StockManagement struct {
	ID      uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	StockID uint64 `gorm:"column:stock_id;index;uniqueIndex:uk_stock_management"`
	Seq     int    `gorm:"column:seq;uniqueIndex:uk_stock_management"`
	Name    string `gorm:"column:name"`
	Title   string `gorm:"column:title"`
	Bio     string `gorm:"column:bio"`
	Stock   Stock  `gorm:"foreignKey:StockID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`

	CreatedAt time.Time `gorm:"column:created_at"`
}

func (StockManagement) TableName() string { return "stock_management" }

// Allotment
type StockAllotmentSummary struct {
	StockID uint64 `gorm:"column:stock_id;primaryKey"`
	Stock   Stock  `gorm:"foreignKey:StockID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`

	Applicants       int64   `gorm:"column:applicants"`
	OneLotWinRatePct float64 `gorm:"column:one_lot_win_rate_pct"`
	MaxLots          int64   `gorm:"column:max_lots"`

	OfferPrice     float64 `gorm:"column:offer_price"`
	OfferPriceLow  float64 `gorm:"column:offer_price_low"`
	OfferPriceHigh float64 `gorm:"column:offer_price_high"`

	SubscriptionMultiple float64 `gorm:"column:subscription_multiple"`
	ClawbackRatePct      float64 `gorm:"column:clawback_rate_pct"`
	AnnouncementUrl      string  `gorm:"column:announcement_url"`

	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (StockAllotmentSummary) TableName() string { return "stock_allotment_summary" }

type StockAllotmentTier struct {
	ID      uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	StockID uint64 `gorm:"column:stock_id;index;uniqueIndex:uk_stock_allotment_tiers"`
	Seq     int    `gorm:"column:seq;uniqueIndex:uk_stock_allotment_tiers"`

	GroupCode  string  `gorm:"column:group_code;index"`
	Lots       int64   `gorm:"column:lots"`
	Applicants int     `gorm:"column:applicants"`
	WinLots    int64   `gorm:"column:win_lots"`
	WinRatePct float64 `gorm:"column:win_rate_pct"`
	Remark     string  `gorm:"column:remark"`
	Stock      Stock   `gorm:"foreignKey:StockID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`

	CreatedAt time.Time `gorm:"column:created_at"`
}

func (StockAllotmentTier) TableName() string { return "stock_allotment_tiers" }

// Raw fallback
type StockRawSection struct {
	ID        uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	StockID   uint64 `gorm:"column:stock_id;index;uniqueIndex:uk_stock_raw_sections"`
	SectionID string `gorm:"column:section_id;uniqueIndex:uk_stock_raw_sections"`
	Seq       *int   `gorm:"column:seq"`
	Stock     Stock  `gorm:"foreignKey:StockID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`

	CreatedAt time.Time `gorm:"column:created_at"`
}

func (StockRawSection) TableName() string { return "stock_raw_sections" }

type StockRawItem struct {
	ID           uint64          `gorm:"column:id;primaryKey;autoIncrement"`
	RawSectionID uint64          `gorm:"column:raw_section_id;index;uniqueIndex:uk_stock_raw_items"`
	Seq          int             `gorm:"column:seq;uniqueIndex:uk_stock_raw_items"`
	Label        string          `gorm:"column:label"`
	Value        string          `gorm:"column:value"`
	RawSection   StockRawSection `gorm:"foreignKey:RawSectionID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`

	CreatedAt time.Time `gorm:"column:created_at"`
}

func (StockRawItem) TableName() string { return "stock_raw_items" }
