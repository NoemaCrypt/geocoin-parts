// Package risk — счётчик сканов и гео-скор (Фаза 0, mock).
//
// Правила пилота:
//   - счётчик уникальных проверок за 24 ч (сканы дилера с авторизацией
//     помечаются source="dealer" и не завышают риск);
//   - гео-скор: дистанция/Δt между последовательными сканами > ~900 км/ч
//     (MVP — ортодромия на сфере; на проде GeographicLib WGS84 из L1-репо);
//   - выход: risk_score 0–100 + человекочитаемые флаги.
package risk

import (
	"fmt"
	"math"
	"sort"

	"geocoin.parts/backend/internal/chain"
)

const (
	// MaxCivilSpeed км/ч — порог impossible travel (самолёт + запас).
	MaxCivilSpeed = 900.0
	// YellowScans24h — порог жёлтого баннера.
	YellowScans24h = 10
	// RedScans24h — порог красного («клонированная этикетка»).
	RedScans24h = 45
)

// Evaluate считает риск по истории сканов одного unit_id.
// zone — допустимый регион текущего custodian (nil = custody не известна):
// скан за пределами радиуса — custody_mismatch («серый импорт»), совмещённый
// с гео-скоростью/аномальным счётчиком — custody_critical (красный +
// рекомендация freeze).
func Evaluate(recs []chain.ScanRecord, now int64, zone *chain.DealerZone) (score int, flags []string) {
	sort.Slice(recs, func(i, j int) bool { return recs[i].TS < recs[j].TS })

	// 1. счётчик за 24 ч (уникальные пары lat/lon; dealer-сканы не считаем)
	dayAgo := now - 24*3600
	seen := make(map[string]bool)
	scans24 := 0
	for _, r := range recs {
		if r.TS < dayAgo || r.Source == "dealer" {
			continue
		}
		key := coordKey(r.Lat, r.Lon)
		if !seen[key] {
			seen[key] = true
			scans24++
		}
	}
	switch {
	case scans24 > RedScans24h:
		score += 50
		flags = append(flags, "scan_count_red")
	case scans24 > YellowScans24h:
		score += 20
		flags = append(flags, "scan_count_yellow")
	}

	// 2. гео-скор по последовательным сканам
	geoVelocity := false
	for i := 1; i < len(recs); i++ {
		a, b := recs[i-1], recs[i]
		dt := b.TS - a.TS
		if dt <= 0 {
			continue
		}
		d := GreatCircleKm(a.Lat, a.Lon, b.Lat, b.Lon)
		speed := d / (float64(dt) / 3600.0)
		if speed > MaxCivilSpeed && d > 50 { // >50 км отсекает шум GPS-дрейфа
			score += 50
			flags = append(flags, "geo_velocity")
			geoVelocity = true
			break
		}
	}

	// 3. custody-mismatch: скан вне допустимого региона текущего custodian
	custodyMismatch := false
	if zone != nil && zone.RadiusKm > 0 {
		for _, r := range recs {
			if r.Source == "dealer" { // сканы дилера со своим складом не проверяем
				continue
			}
			if GreatCircleKm(r.Lat, r.Lon, zone.Lat, zone.Lon) > zone.RadiusKm {
				custodyMismatch = true
				break
			}
		}
	}
	switch {
	case custodyMismatch && (geoVelocity || scans24 > RedScans24h):
		score += 60
		flags = append(flags, "custody_critical")
	case custodyMismatch:
		score += 30
		flags = append(flags, "custody_mismatch")
	}

	if score > 100 {
		score = 100
	}
	if score == 0 {
		flags = []string{}
	}
	return score, flags
}

// GreatCircleKm — ортодромия (MVP; на проде заменить на GeographicLib WGS84).
func GreatCircleKm(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0088 // средний радиус WGS84
	p1, p2 := rad(lat1), rad(lat2)
	dp := p2 - p1
	dl := rad(lon2 - lon1)
	h := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(h)))
}

func rad(d float64) float64 { return d * math.Pi / 180 }

func coordKey(lat, lon float64) string {
	// грубая ячейка ~1 км: сканы из одной точки не дублируют счётчик
	return fmt.Sprintf("%d:%d", int(lat*90), int(lon*90))
}
