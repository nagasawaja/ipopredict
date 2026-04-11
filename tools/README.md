# Tools Overview

This directory contains standalone command-line tools for model validation and data extraction.

## 1) `backtest_predict`

Path: `tools/backtest_predict/main.go`

Purpose:
- Backtest one-lot win-rate prediction on historical stocks.
- Compares:
  - Actual: `stock_allotment_summary.one_lot_win_rate_pct`
  - Predicted: one-lot `PerLotRate` from `ipo_predict.Predict`

Usage:

```bash
GOCACHE=/tmp/go-build go run ./tools/backtest_predict -db sql/hk_ipo.db -top 20
```

Flags:
- `-db` (default: `sql/hk_ipo.db`): sqlite database path
- `-top` (default: `20`): number of largest-error stocks to print

Output:
- `samples`
- overall `mae`
- top-N absolute errors

## 2) `backtest_tier_predict`

Path: `tools/backtest_tier_predict/main.go`

Purpose:
- Tier-by-tier backtest for latest stocks (or latest N).
- Uses real tier lots (`stock_allotment_tiers.lots`) as buckets, then compares:
  - Actual tier win rate: `stock_allotment_tiers.win_rate_pct`
  - Predicted tier rate: `PerLotRate * 100`

Usage:

```bash
GOCACHE=/tmp/go-build go run ./tools/backtest_tier_predict -db sql/hk_ipo.db -latest 15 -detail=true
```

Flags:
- `-db` (default: `sql/hk_ipo.db`): sqlite database path
- `-latest` (default: `15`): latest N stocks ordered by `list_date`
- `-detail` (default: `true`): whether to print every tier row

Output:
- overall tier MAE
- per-stock MAE
- optional full tier-level compare rows

## 3) `test_predict`

Path: `tools/test_predict/main.go`

Purpose:
- Quick local sanity runner for `ipo_predict.Predict`.
- Uses an embedded JSON request and prints all predicted tiers and allocations.

Usage:

```bash
GOCACHE=/tmp/go-build go run ./tools/test_predict
```

Notes:
- Input request is hardcoded in source file.
- Useful for fast debugging of distribution behavior.

## 4) `backtest_clean_predict`

Path: `tools/backtest_clean_predict/main.go`

Purpose:
- Filter "normal" stocks for backtesting:
  - core fields complete
  - announcement tiers complete and include one-lot row
  - prospectus tier table can be extracted
  - prospectus lots exactly match announcement lots
- On the clean sample, compare one-lot prediction accuracy:
  - `default buckets`
  - `prospectus buckets`
- Also compute tier-level MAE on the exact-match clean sample.

Usage:

```bash
GOCACHE=/tmp/go-build go run ./tools/backtest_clean_predict -db sql/hk_ipo.db -top 20 -cache-dir /tmp/hkipo_prospectus_cache
```

Flags:
- `-db`: sqlite database path
- `-top`: number of largest clean-sample errors to print
- `-limit`: optional stock limit for debugging
- `-cache-dir`: prospectus PDF cache dir
- `-codes`: print exact-match clean stock codes

## 5) `pdfreader`

Path: `tools/pdfreader/main.go`

Purpose:
- Experimental helper around PDF table extraction.
- Includes sample code to:
  - initialize ORM
  - locate a prospectus URL
  - download PDF
  - parse anchored table data

Usage:

```bash
GOCACHE=/tmp/go-build go run ./tools/pdfreader
```

Notes:
- Contains hardcoded local sample paths and test logic.
- Intended for development/testing, not production batch use.

## 6) `tune_one_lot`

Path: `tools/tune_one_lot/main.go`

Purpose:
- Use the exact-match clean sample to test lightweight one-lot calibration ideas.
- Current implementation compares:
  - current model MAE
  - oversub-band scaling CV MAE
  - log-linear refit CV MAE
- Prints the fitted coefficients for further manual review.

Usage:

```bash
GOCACHE=/tmp/go-build go run ./tools/tune_one_lot -db sql/hk_ipo.db -cache-dir /tmp/hkipo_prospectus_cache -folds 5
```

## 7) `analyze_current_model`

Path: `tools/analyze_current_model/main.go`

Purpose:
- Run a field-level review of the current `ipo_predict` model on the exact-match clean sample.
- Reports accuracy for:
  - one-lot win rate
  - tier win rate
  - total applicants
  - tier applicants
- Also prints structural diagnostics, including how far actual B-group applicant ratio and A-group one-lot fraction drift from the model constants.

Usage:

```bash
GOCACHE=/tmp/go-build go run ./tools/analyze_current_model -db sql/hk_ipo.db -cache-dir /tmp/hkipo_prospectus_cache -top 10
```
