-- MySQL 5.6 schema for hk_ipo (normalized, relationship-friendly)
-- Notes:
-- - No JSON type (MySQL 5.6)
-- - UTF8MB4 recommended
-- - Use `ipo_intermediary` + `ipo_stock_intermediary` for reverse lookup:
--   "find all stocks by sponsor/underwriter/bookrunner/global coordinator"

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

-- =========================
-- 1) Core stock table
-- =========================
CREATE TABLE IF NOT EXISTS ipo_stock (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,

  stock_code VARCHAR(16) NOT NULL,       -- e.g. "00100"
  hk_symbol  VARCHAR(32) DEFAULT NULL,   -- e.g. "00100.HK"
  -- MySQL 5.6 InnoDB + utf8mb4 indexed column length limit (767 bytes):
  -- if you need an index on this column, keep it <= 191 chars (191*4=764 bytes).
  stock_name VARCHAR(191) DEFAULT NULL,

  reference_company VARCHAR(255) DEFAULT NULL,

  source_provider VARCHAR(32) DEFAULT NULL, -- e.g. "chiefgroup"
  source_request_symbol VARCHAR(32) DEFAULT NULL,
  source_url VARCHAR(512) DEFAULT NULL,
  source_fetched_at BIGINT DEFAULT NULL, -- unix seconds

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

  PRIMARY KEY (id),
  UNIQUE KEY uk_ipo_stock_stock_code (stock_code),
  KEY idx_ipo_stock_hk_symbol (hk_symbol),
  KEY idx_ipo_stock_stock_name (stock_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =========================
-- 2) One-to-one detail tables
-- =========================

CREATE TABLE IF NOT EXISTS ipo_stock_offering (
  stock_id BIGINT UNSIGNED NOT NULL,

  offer_price_low  DECIMAL(12,4) DEFAULT NULL,
  offer_price_high DECIMAL(12,4) DEFAULT NULL,
  offer_price      DECIMAL(12,4) DEFAULT NULL,

  lot_size INT DEFAULT NULL,

  global_offer_shares        BIGINT DEFAULT NULL,
  public_offer_shares        BIGINT DEFAULT NULL,
  international_offer_shares BIGINT DEFAULT NULL,

  apply_start_date DATE DEFAULT NULL,
  apply_end_date   DATE DEFAULT NULL,
  list_date        DATE DEFAULT NULL,

  admission_fee_hkd DECIMAL(18,2) DEFAULT NULL,
  market_cap_hkd    DECIMAL(18,2) DEFAULT NULL,
  pe                DECIMAL(18,4) DEFAULT NULL,

  prospectus_url VARCHAR(512) DEFAULT NULL,
  allocation_mechanism VARCHAR(64) DEFAULT NULL,
  allocation_mechanism_confidence DECIMAL(5,4) DEFAULT NULL,
  allocation_mechanism_source VARCHAR(64) DEFAULT NULL,
  allocation_mechanism_evidence VARCHAR(512) DEFAULT NULL,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

  PRIMARY KEY (stock_id),
  CONSTRAINT fk_ipo_stock_offering_stock
    FOREIGN KEY (stock_id) REFERENCES ipo_stock(id)
    ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ipo_stock_company (
  stock_id BIGINT UNSIGNED NOT NULL,

  address VARCHAR(512) DEFAULT NULL,
  registrar VARCHAR(255) DEFAULT NULL,
  registrar_phone VARCHAR(64) DEFAULT NULL,
  chairman VARCHAR(128) DEFAULT NULL,
  phone VARCHAR(64) DEFAULT NULL,
  business TEXT,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

  PRIMARY KEY (stock_id),
  CONSTRAINT fk_ipo_stock_company_stock
    FOREIGN KEY (stock_id) REFERENCES ipo_stock(id)
    ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ipo_stock_green_shoe (
  stock_id BIGINT UNSIGNED NOT NULL,
  rate_pct DECIMAL(8,4) DEFAULT NULL,
  amount_shares BIGINT DEFAULT NULL,
  amount_text VARCHAR(255) DEFAULT NULL,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

  PRIMARY KEY (stock_id),
  CONSTRAINT fk_ipo_stock_green_shoe_stock
    FOREIGN KEY (stock_id) REFERENCES ipo_stock(id)
    ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ipo_stock_grey_market (
  stock_id BIGINT UNSIGNED NOT NULL,
  grey_date DATE DEFAULT NULL,
  incr_rate_pct  DECIMAL(8,4) DEFAULT NULL,
  incr_rate_pct2 DECIMAL(8,4) DEFAULT NULL,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

  PRIMARY KEY (stock_id),
  CONSTRAINT fk_ipo_stock_grey_market_stock
    FOREIGN KEY (stock_id) REFERENCES ipo_stock(id)
    ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ipo_stock_performance (
  stock_id BIGINT UNSIGNED NOT NULL,
  first_day_incr_rate_pct DECIMAL(8,4) DEFAULT NULL,
  total_incr_rate_pct     DECIMAL(8,4) DEFAULT NULL,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

  PRIMARY KEY (stock_id),
  CONSTRAINT fk_ipo_stock_performance_stock
    FOREIGN KEY (stock_id) REFERENCES ipo_stock(id)
    ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ipo_stock_raise_money (
  stock_id BIGINT UNSIGNED NOT NULL,
  amount_hkd  DECIMAL(18,2) DEFAULT NULL,
  amount_text VARCHAR(255) DEFAULT NULL,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

  PRIMARY KEY (stock_id),
  CONSTRAINT fk_ipo_stock_raise_money_stock
    FOREIGN KEY (stock_id) REFERENCES ipo_stock(id)
    ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =========================
-- 3) Many-to-many: Intermediaries (sponsors/underwriters/etc)
-- =========================

CREATE TABLE IF NOT EXISTS ipo_intermediary (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  -- Keep <=191 to allow UNIQUE index under MySQL 5.6 utf8mb4.
  name VARCHAR(191) NOT NULL,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

  PRIMARY KEY (id),
  UNIQUE KEY uk_ipo_intermediary_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- role examples: "sponsor" / "underwriter" / "bookrunner" / "global_coordinator"
CREATE TABLE IF NOT EXISTS ipo_stock_intermediary (
  stock_id BIGINT UNSIGNED NOT NULL,
  intermediary_id BIGINT UNSIGNED NOT NULL,
  role VARCHAR(32) NOT NULL,
  seq INT DEFAULT NULL,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (stock_id, intermediary_id, role),
  KEY idx_ipo_stock_intermediary_role (role),
  KEY idx_ipo_stock_intermediary_intermediary (intermediary_id),

  CONSTRAINT fk_ipo_stock_intermediary_stock
    FOREIGN KEY (stock_id) REFERENCES ipo_stock(id)
    ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT fk_ipo_stock_intermediary_intermediary
    FOREIGN KEY (intermediary_id) REFERENCES ipo_intermediary(id)
    ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =========================
-- 4) Company sub-items (arrays)
-- =========================

CREATE TABLE IF NOT EXISTS ipo_stock_company_secretary (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  stock_id BIGINT UNSIGNED NOT NULL,
  seq INT NOT NULL,
  name VARCHAR(255) NOT NULL,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (id),
  UNIQUE KEY uk_ipo_stock_company_secretary (stock_id, seq),
  KEY idx_ipo_stock_company_secretary_stock (stock_id),

  CONSTRAINT fk_ipo_stock_company_secretary_stock
    FOREIGN KEY (stock_id) REFERENCES ipo_stock(id)
    ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ipo_stock_major_shareholder (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  stock_id BIGINT UNSIGNED NOT NULL,
  seq INT NOT NULL,

  -- Keep both parsed + raw, because sources often embed "(xx.xx%)" in text.
  name VARCHAR(255) DEFAULT NULL,
  pct DECIMAL(6,2) DEFAULT NULL,
  raw_text VARCHAR(255) NOT NULL,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (id),
  UNIQUE KEY uk_ipo_stock_major_shareholder (stock_id, seq),
  KEY idx_ipo_stock_major_shareholder_stock (stock_id),

  CONSTRAINT fk_ipo_stock_major_shareholder_stock
    FOREIGN KEY (stock_id) REFERENCES ipo_stock(id)
    ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =========================
-- 5) Use of proceeds
-- =========================

CREATE TABLE IF NOT EXISTS ipo_stock_use_of_proceeds (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  stock_id BIGINT UNSIGNED NOT NULL,
  seq INT NOT NULL,
  text TEXT NOT NULL,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (id),
  UNIQUE KEY uk_ipo_stock_use_of_proceeds (stock_id, seq),
  KEY idx_ipo_stock_use_of_proceeds_stock (stock_id),

  CONSTRAINT fk_ipo_stock_use_of_proceeds_stock
    FOREIGN KEY (stock_id) REFERENCES ipo_stock(id)
    ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =========================
-- 6) Management
-- =========================

CREATE TABLE IF NOT EXISTS ipo_stock_management (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  stock_id BIGINT UNSIGNED NOT NULL,
  seq INT NOT NULL,
  name VARCHAR(255) NOT NULL,
  title VARCHAR(255) DEFAULT NULL,
  bio TEXT,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (id),
  UNIQUE KEY uk_ipo_stock_management (stock_id, seq),
  KEY idx_ipo_stock_management_stock (stock_id),

  CONSTRAINT fk_ipo_stock_management_stock
    FOREIGN KEY (stock_id) REFERENCES ipo_stock(id)
    ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =========================
-- 7) Allotment (summary + tiers)
-- =========================

CREATE TABLE IF NOT EXISTS ipo_stock_allotment_summary (
  stock_id BIGINT UNSIGNED NOT NULL,

  applicants BIGINT DEFAULT NULL,
  one_lot_win_rate_pct DECIMAL(8,4) DEFAULT NULL,
  max_lots BIGINT DEFAULT NULL,

  offer_price DECIMAL(12,4) DEFAULT NULL,
  offer_price_low DECIMAL(12,4) DEFAULT NULL,
  offer_price_high DECIMAL(12,4) DEFAULT NULL,

  subscription_multiple DECIMAL(18,4) DEFAULT NULL,
  clawback_rate_pct DECIMAL(8,4) DEFAULT NULL,
  announcement_url VARCHAR(512) DEFAULT NULL,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

  PRIMARY KEY (stock_id),
  CONSTRAINT fk_ipo_stock_allotment_summary_stock
    FOREIGN KEY (stock_id) REFERENCES ipo_stock(id)
    ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ipo_stock_allotment_tier (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  stock_id BIGINT UNSIGNED NOT NULL,
  seq INT NOT NULL,

  group_code VARCHAR(8) NOT NULL,  -- e.g. "A"/"B"
  lots BIGINT NOT NULL,
  applicants INT DEFAULT NULL,
  win_lots BIGINT DEFAULT NULL,
  win_rate_pct DECIMAL(8,4) DEFAULT NULL,
  remark VARCHAR(1024) DEFAULT NULL,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (id),
  UNIQUE KEY uk_ipo_stock_allotment_tier (stock_id, seq),
  KEY idx_ipo_stock_allotment_tier_stock (stock_id),
  KEY idx_ipo_stock_allotment_tier_group (group_code),

  CONSTRAINT fk_ipo_stock_allotment_tier_stock
    FOREIGN KEY (stock_id) REFERENCES ipo_stock(id)
    ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- =========================
-- 8) Raw sections (fallback, no Chinese as JSON keys; section_id is stable)
-- =========================

CREATE TABLE IF NOT EXISTS ipo_stock_raw_section (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  stock_id BIGINT UNSIGNED NOT NULL,
  section_id VARCHAR(64) NOT NULL, -- issuance/company/purpose/allotmentSummary/...
  seq INT DEFAULT NULL,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (id),
  UNIQUE KEY uk_ipo_stock_raw_section (stock_id, section_id),
  KEY idx_ipo_stock_raw_section_stock (stock_id),

  CONSTRAINT fk_ipo_stock_raw_section_stock
    FOREIGN KEY (stock_id) REFERENCES ipo_stock(id)
    ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ipo_stock_raw_item (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  raw_section_id BIGINT UNSIGNED NOT NULL,
  seq INT NOT NULL,
  label VARCHAR(255) NOT NULL,
  value TEXT NOT NULL,

  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (id),
  UNIQUE KEY uk_ipo_stock_raw_item (raw_section_id, seq),
  KEY idx_ipo_stock_raw_item_section (raw_section_id),

  CONSTRAINT fk_ipo_stock_raw_item_section
    FOREIGN KEY (raw_section_id) REFERENCES ipo_stock_raw_section(id)
    ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

SET FOREIGN_KEY_CHECKS = 1;
