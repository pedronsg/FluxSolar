// Package history persists per-minute averages of the live power readings
// (solar, load, grid, battery) in a small SQLite database, and serves
// aggregated time series for the dashboard charts.
//
// All timestamps stored in the DB are UTC unix epoch in *minutes*.
// Aggregation to local hour / local day for chart rendering is performed at
// query time using the server's current local timezone offset.
package history

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/OrbitOS-org/orbit-os-sdk-go/v26/logger"
	_ "modernc.org/sqlite"
)

const logTag = "history"

// Sample is one live power reading written into the minute accumulator.
// Any field set to NaN is treated as "missing" and skipped.
type Sample struct {
	SolarW   float64
	LoadW    float64
	GridW    float64
	BatteryW float64
}

// Bucket is one point of an aggregated time series returned to the frontend.
// Missing values are reported as null pointers so the chart can render gaps.
type Bucket struct {
	Ts       time.Time `json:"ts"`
	SolarW   *float64  `json:"solar_w"`
	LoadW    *float64  `json:"load_w"`
	GridW    *float64  `json:"grid_w"`
	BatteryW *float64  `json:"battery_w"`
}

// Granularity selects the bucket size for a query.
type Granularity string

const (
	GranMinute Granularity = "minute"
	GranHour   Granularity = "hour"
	GranDay    Granularity = "day"
)

// Store wraps the sqlite database and the in-memory minute accumulator.
type Store struct {
	db *sql.DB

	mu  sync.Mutex
	cur *minuteAccum
}

type minuteAccum struct {
	minute       int64
	solarSum     float64
	solarCount   int64
	loadSum      float64
	loadCount    int64
	gridSum      float64
	gridCount    int64
	batterySum   float64
	batteryCount int64
}

type minuteRow struct {
	minute int64
	solar  sql.NullFloat64
	load   sql.NullFloat64
	grid   sql.NullFloat64
	batt   sql.NullFloat64
	count  int64
}

// Open opens (or creates) the SQLite database at path and ensures the schema.
func Open(path string) (*Store, error) {
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open history db: %w", err)
	}
	db.SetMaxOpenConns(1)

	if err := ensureSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	logger.Infof(logTag, "history db ready at %s", path)
	return &Store{db: db}, nil
}

func ensureSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS power_samples_1m (
			ts_minute     INTEGER PRIMARY KEY,
			solar_w       REAL,
			load_w        REAL,
			grid_w        REAL,
			battery_w     REAL,
			sample_count  INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_power_samples_1m_ts ON power_samples_1m(ts_minute)`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}
	}
	return nil
}

// Close flushes the current minute (best effort) and closes the DB.
func (s *Store) Close() error {
	s.mu.Lock()
	if s.cur != nil {
		_ = s.flushLocked(s.cur)
		s.cur = nil
	}
	s.mu.Unlock()
	return s.db.Close()
}

// AddSample feeds a new live reading into the current minute accumulator.
// On a minute rollover the previous accumulator is flushed (upserted).
// The current minute is also upserted on every call so a sudden crash does
// not lose more than the last sample.
func (s *Store) AddSample(ts time.Time, sample Sample) {
	minute := ts.UTC().Unix() / 60

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cur == nil || s.cur.minute != minute {
		if s.cur != nil {
			if err := s.flushLocked(s.cur); err != nil {
				logger.Warnf(logTag, "flush previous minute %d: %v", s.cur.minute, err)
			}
		}
		s.cur = &minuteAccum{minute: minute}
	}

	addIfFinite(&s.cur.solarSum, &s.cur.solarCount, sample.SolarW)
	addIfFinite(&s.cur.loadSum, &s.cur.loadCount, sample.LoadW)
	addIfFinite(&s.cur.gridSum, &s.cur.gridCount, sample.GridW)
	addIfFinite(&s.cur.batterySum, &s.cur.batteryCount, sample.BatteryW)

	if err := s.flushLocked(s.cur); err != nil {
		logger.Warnf(logTag, "incremental flush minute %d: %v", s.cur.minute, err)
	}
}

func addIfFinite(sum *float64, count *int64, v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	*sum += v
	*count++
}

func (s *Store) flushLocked(a *minuteAccum) error {
	if a == nil {
		return nil
	}
	totalCount := a.solarCount + a.loadCount + a.gridCount + a.batteryCount
	if totalCount == 0 {
		return nil
	}

	solar := avgOrNil(a.solarSum, a.solarCount)
	load := avgOrNil(a.loadSum, a.loadCount)
	grid := avgOrNil(a.gridSum, a.gridCount)
	batt := avgOrNil(a.batterySum, a.batteryCount)

	maxCount := a.solarCount
	for _, v := range []int64{a.loadCount, a.gridCount, a.batteryCount} {
		if v > maxCount {
			maxCount = v
		}
	}

	const q = `
		INSERT INTO power_samples_1m (ts_minute, solar_w, load_w, grid_w, battery_w, sample_count)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(ts_minute) DO UPDATE SET
			solar_w      = excluded.solar_w,
			load_w       = excluded.load_w,
			grid_w       = excluded.grid_w,
			battery_w    = excluded.battery_w,
			sample_count = excluded.sample_count
	`
	_, err := s.db.Exec(q, a.minute, solar, load, grid, batt, maxCount)
	return err
}

func avgOrNil(sum float64, count int64) interface{} {
	if count <= 0 {
		return nil
	}
	return sum / float64(count)
}

// Query returns aggregated time-series buckets over the half-open range
// [start, end). The granularity controls the bucket size:
//
//   - GranMinute: rows are returned 1:1 from the DB (1 bucket per minute).
//   - GranHour:   minute rows are grouped by *local* hour (loc).
//   - GranDay:    minute rows are grouped by *local* day (loc).
//
// Missing buckets inside the range are filled with nil values so the frontend
// can render time-aware charts with gaps.
func (s *Store) Query(ctx context.Context, start, end time.Time, gran Granularity, loc *time.Location) ([]Bucket, error) {
	if loc == nil {
		loc = time.Local
	}
	if !start.Before(end) {
		return []Bucket{}, nil
	}

	startMin := start.UTC().Unix() / 60
	endMin := (end.UTC().Unix() + 59) / 60

	rows, err := s.db.QueryContext(ctx, `
		SELECT ts_minute, solar_w, load_w, grid_w, battery_w, sample_count
		FROM power_samples_1m
		WHERE ts_minute >= ? AND ts_minute < ?
		ORDER BY ts_minute
	`, startMin, endMin)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()

	var fetched []minuteRow
	for rows.Next() {
		var m minuteRow
		if err := rows.Scan(&m.minute, &m.solar, &m.load, &m.grid, &m.batt, &m.count); err != nil {
			return nil, fmt.Errorf("scan history row: %w", err)
		}
		fetched = append(fetched, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate history rows: %w", err)
	}

	switch gran {
	case GranMinute:
		return buildMinuteSeries(start, end, fetched, loc), nil
	case GranHour:
		return buildBucketedSeries(start, end, fetched, loc, time.Hour), nil
	case GranDay:
		return buildBucketedSeries(start, end, fetched, loc, 24*time.Hour), nil
	default:
		return nil, fmt.Errorf("unsupported granularity %q", gran)
	}
}

// LatestSampleTime returns the timestamp (local) of the most recent stored
// minute, or zero if the DB is empty.
func (s *Store) LatestSampleTime(ctx context.Context, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	var minute sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(ts_minute) FROM power_samples_1m`).Scan(&minute)
	if err != nil {
		return time.Time{}, err
	}
	if !minute.Valid {
		return time.Time{}, nil
	}
	return time.Unix(minute.Int64*60, 0).In(loc), nil
}

func buildMinuteSeries(start, end time.Time, rows []minuteRow, loc *time.Location) []Bucket {
	startMin := start.UTC().Unix() / 60
	endMin := end.UTC().Unix() / 60
	if endMin <= startMin {
		return []Bucket{}
	}
	totalMinutes := endMin - startMin

	const maxMinutes = 24 * 60 * 7
	if totalMinutes > maxMinutes {
		totalMinutes = maxMinutes
	}

	out := make([]Bucket, 0, totalMinutes)
	rowIdx := 0
	for i := int64(0); i < totalMinutes; i++ {
		min := startMin + i
		ts := time.Unix(min*60, 0).In(loc)
		b := Bucket{Ts: ts}
		if rowIdx < len(rows) && rows[rowIdx].minute == min {
			r := rows[rowIdx]
			b.SolarW = nullToPtr(r.solar)
			b.LoadW = nullToPtr(r.load)
			b.GridW = nullToPtr(r.grid)
			b.BatteryW = nullToPtr(r.batt)
			rowIdx++
		}
		out = append(out, b)
	}
	return out
}

func buildBucketedSeries(start, end time.Time, rows []minuteRow, loc *time.Location, bucketSize time.Duration) []Bucket {
	startLocal := start.In(loc)
	endLocal := end.In(loc)

	var alignStart time.Time
	switch bucketSize {
	case time.Hour:
		alignStart = time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), startLocal.Hour(), 0, 0, 0, loc)
	case 24 * time.Hour:
		alignStart = time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, loc)
	default:
		alignStart = startLocal
	}

	var bucketCount int
	switch bucketSize {
	case time.Hour:
		diff := endLocal.Sub(alignStart)
		bucketCount = int((diff + time.Hour - 1) / time.Hour)
	case 24 * time.Hour:
		y1, m1, d1 := alignStart.Date()
		y2, m2, d2 := endLocal.Date()
		t1 := time.Date(y1, m1, d1, 0, 0, 0, 0, loc)
		t2 := time.Date(y2, m2, d2, 0, 0, 0, 0, loc)
		bucketCount = int(t2.Sub(t1) / (24 * time.Hour))
	default:
		bucketCount = int(endLocal.Sub(alignStart) / bucketSize)
	}
	if bucketCount <= 0 {
		return []Bucket{}
	}
	const maxBuckets = 24*31*2 + 366
	if bucketCount > maxBuckets {
		bucketCount = maxBuckets
	}

	type acc struct {
		solarSum, loadSum, gridSum, battSum         float64
		solarCount, loadCount, gridCount, battCount int64
	}
	accs := make([]acc, bucketCount)

	bucketIdx := func(ts time.Time) int {
		switch bucketSize {
		case time.Hour:
			diff := ts.In(loc).Sub(alignStart)
			return int(diff / time.Hour)
		case 24 * time.Hour:
			localTs := ts.In(loc)
			y1, m1, d1 := alignStart.Date()
			y2, m2, d2 := localTs.Date()
			t1 := time.Date(y1, m1, d1, 0, 0, 0, 0, loc)
			t2 := time.Date(y2, m2, d2, 0, 0, 0, 0, loc)
			return int(t2.Sub(t1) / (24 * time.Hour))
		}
		return 0
	}

	for _, r := range rows {
		ts := time.Unix(r.minute*60, 0).In(loc)
		idx := bucketIdx(ts)
		if idx < 0 || idx >= bucketCount {
			continue
		}
		a := &accs[idx]
		if r.solar.Valid {
			a.solarSum += r.solar.Float64
			a.solarCount++
		}
		if r.load.Valid {
			a.loadSum += r.load.Float64
			a.loadCount++
		}
		if r.grid.Valid {
			a.gridSum += r.grid.Float64
			a.gridCount++
		}
		if r.batt.Valid {
			a.battSum += r.batt.Float64
			a.battCount++
		}
	}

	out := make([]Bucket, 0, bucketCount)
	for i := 0; i < bucketCount; i++ {
		var ts time.Time
		switch bucketSize {
		case time.Hour:
			ts = alignStart.Add(time.Duration(i) * time.Hour)
		case 24 * time.Hour:
			ts = alignStart.AddDate(0, 0, i)
		default:
			ts = alignStart.Add(time.Duration(i) * bucketSize)
		}
		a := accs[i]
		b := Bucket{Ts: ts}
		if a.solarCount > 0 {
			v := a.solarSum / float64(a.solarCount)
			b.SolarW = &v
		}
		if a.loadCount > 0 {
			v := a.loadSum / float64(a.loadCount)
			b.LoadW = &v
		}
		if a.gridCount > 0 {
			v := a.gridSum / float64(a.gridCount)
			b.GridW = &v
		}
		if a.battCount > 0 {
			v := a.battSum / float64(a.battCount)
			b.BatteryW = &v
		}
		out = append(out, b)
	}
	return out
}

// Totals holds aggregate energy totals derived from the history database.
type Totals struct {
	SolarKWh       float64 `json:"solar_kwh"`
	LoadKWh        float64 `json:"load_kwh"`
	ImportKWh      float64 `json:"import_kwh"`
	ExportKWh      float64 `json:"export_kwh"`
	BChargedKWh    float64 `json:"bcharged_kwh"`
	BDischargedKWh float64 `json:"bdischarged_kwh"`
}

// QueryTotals returns aggregate energy totals over [start, end).
// Pass zero-value times to query all available data.
func (s *Store) QueryTotals(ctx context.Context, start, end time.Time) (Totals, error) {
	const baseQ = `
		SELECT
			COALESCE(SUM(CASE WHEN solar_w   > 0 THEN solar_w   ELSE 0 END), 0) / 60000.0,
			COALESCE(SUM(CASE WHEN load_w    > 0 THEN load_w    ELSE 0 END), 0) / 60000.0,
			COALESCE(SUM(CASE WHEN grid_w    > 0 THEN grid_w    ELSE 0 END), 0) / 60000.0,
			COALESCE(SUM(CASE WHEN grid_w    < 0 THEN -grid_w   ELSE 0 END), 0) / 60000.0,
			COALESCE(SUM(CASE WHEN battery_w < 0 THEN -battery_w ELSE 0 END), 0) / 60000.0,
			COALESCE(SUM(CASE WHEN battery_w > 0 THEN battery_w  ELSE 0 END), 0) / 60000.0
		FROM power_samples_1m`

	var (
		t    Totals
		err  error
		row  *sql.Row
	)
	if start.IsZero() || end.IsZero() {
		row = s.db.QueryRowContext(ctx, baseQ)
	} else {
		row = s.db.QueryRowContext(ctx, baseQ+" WHERE ts_minute >= ? AND ts_minute < ?",
			start.UTC().Unix()/60, end.UTC().Unix()/60)
	}
	err = row.Scan(&t.SolarKWh, &t.LoadKWh, &t.ImportKWh, &t.ExportKWh, &t.BChargedKWh, &t.BDischargedKWh)
	if err != nil {
		return Totals{}, fmt.Errorf("query totals: %w", err)
	}
	return t, nil
}

func nullToPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}
