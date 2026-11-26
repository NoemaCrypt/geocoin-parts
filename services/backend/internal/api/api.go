// Package api — HTTP-интерфейс verification backend (Фаза 0, mock).
//
//   - GET  /api/unit/{id}        — статус, счётчик, risk (то видит покупатель)
//   - POST /api/scan             — фиксация скана {unit_id, lat, lon, ts?, source?}
//   - GET  /api/unit/{id}/proof  — Merkle-прут против batch_root
//   - POST /api/redeem           — погашение {unit_id, claim_code, vin?}
//   - GET  /                     — web-checker (PWA-каркас)
package api

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"geocoin.parts/backend/internal/chain"
	"geocoin.parts/backend/internal/risk"
)

//go:embed web/index.html
var webCheckerFS embed.FS

var webChecker, _ = webCheckerFS.ReadFile("web/index.html")

type Server struct {
	Chain   *chain.Chain
	ScanLog string // путь к JSONL-логу сканов
	Demux   bool   // демо-режим: повышенные пороги не нужны, лог общий
}

func (s *Server) Router() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/unit/{id}", s.handleUnit)
	mux.HandleFunc("GET /api/unit/{id}/proof", s.handleProof)
	mux.HandleFunc("POST /api/scan", s.handleScan)
	mux.HandleFunc("POST /api/redeem", s.handleRedeem)
	mux.HandleFunc("GET /", s.handleWeb)
	return mux
}

// unitState — ответ для покупателя (тот же JSON, что рендерит web-checker).
type unitState struct {
	UnitID     string   `json:"unit_id"`
	Found      bool     `json:"found"`
	Status     string   `json:"status"` // issued | redeemed | unknown
	RedeemedAt int64    `json:"redeemed_at,omitempty"`
	BatchID    string   `json:"batch_id,omitempty"`
	BatchRoot  string   `json:"batch_root,omitempty"`
	ScansTotal int      `json:"scans_total"`
	Scans24h   int      `json:"scans_24h"`
	RiskScore  int      `json:"risk_score"`
	Flags      []string `json:"flags"`
	Message    string   `json:"message"`
}

func (s *Server) unitState(id string, now int64) unitState {
	st := unitState{UnitID: id, Flags: []string{}}
	var uid [32]byte
	b, err := hex.DecodeString(id)
	if err != nil || len(b) != 32 {
		st.Message = "Некорректный unit_id"
		return st
	}
	copy(uid[:], b)
	spent, redeemedAt, batchID, found := s.Chain.Status(uid)
	if !found {
		st.Message = "Единица не найдена в якоренных батчах (подделка или другая сеть)"
		return st
	}
	st.Found = true
	st.BatchID = batchID
	st.BatchRoot = s.Chain.BatchRootHex(batchID)
	if spent {
		st.Status = "redeemed"
		st.RedeemedAt = redeemedAt
		st.RiskScore = 100
		st.Flags = append(st.Flags, "already_redeemed")
		st.Message = "Код уже был погашен. Высокий риск подделки (клонированная этикетка)."
		return st
	}
	st.Status = "issued"

	// риск по логу сканов
	recs := []chain.ScanRecord{}
	_ = chain.ScanLines(s.ScanLog, func(r chain.ScanRecord) error {
		if r.UnitID == id {
			recs = append(recs, r)
		}
		return nil
	})
	st.ScansTotal = len(recs)
	dayAgo := now - 24*3600
	seen := map[string]bool{}
	for _, r := range recs {
		if r.TS >= dayAgo && r.Source != "dealer" {
			key := fmt.Sprintf("%.1f:%.1f", r.Lat, r.Lon)
			if !seen[key] {
				seen[key] = true
				st.Scans24h++
			}
		}
	}
	st.RiskScore, st.Flags = risk.Evaluate(recs, now)
	switch {
	case contains(st.Flags, "geo_velocity"):
		st.Message = "ВНИМАНИЕ: сканы из географически невозможных точек. Код, вероятно, клонирован."
	case contains(st.Flags, "scan_count_red", "scan_count_yellow"):
		st.Message = "Подозрительная активность: этот код проверяли " + strconv.Itoa(st.Scans24h) + " раз за сутки."
	default:
		st.Message = "Оригинал. Выпущено заводом, ни разу не устанавливалось."
	}
	return st
}

func contains(list []string, vals ...string) bool {
	for _, v := range vals {
		for _, x := range list {
			if x == v {
				return true
			}
		}
	}
	return false
}

func (s *Server) handleUnit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	writeJSON(w, s.unitState(strings.ToLower(id), time.Now().Unix()))
}

func (s *Server) handleProof(w http.ResponseWriter, r *http.Request) {
	var uid [32]byte
	b, err := hex.DecodeString(r.PathValue("id"))
	if err != nil || len(b) != 32 {
		http.Error(w, "bad unit id", 400)
		return
	}
	copy(uid[:], b)
	proof, batchID, ok := s.Chain.Proof(uid)
	if !ok {
		http.Error(w, "unit not found", 404)
		return
	}
	ph := make([]string, 0, len(proof))
	for _, p := range proof {
		ph = append(ph, hex.EncodeToString(p[:]))
	}
	writeJSON(w, map[string]any{"batch_id": batchID, "batch_root": s.Chain.BatchRootHex(batchID), "proof": ph})
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UnitID string  `json:"unit_id"`
		Lat    float64 `json:"lat"`
		Lon    float64 `json:"lon"`
		TS     int64   `json:"ts,omitempty"`
		Source string  `json:"source,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	req.UnitID = strings.ToLower(req.UnitID)
	now := req.TS
	if now == 0 {
		now = time.Now().Unix()
	}
	if req.Source == "" {
		req.Source = "public"
	}
	b, err := hex.DecodeString(req.UnitID)
	if err != nil || len(b) != 32 {
		http.Error(w, "bad unit_id", 400)
		return
	}
	rec := chain.ScanRecord{
		UnitID: req.UnitID, TS: now, Lat: req.Lat, Lon: req.Lon, Source: req.Source,
	}
	if err := chain.AppendScan(s.ScanLog, rec); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, s.unitState(req.UnitID, now))
}

func (s *Server) handleRedeem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UnitID    string `json:"unit_id"`
		ClaimCode string `json:"claim_code"`
		VIN       string `json:"vin,omitempty"` // VIN или его хеш; хранится только хеш с off-chain солью
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	req.UnitID = strings.ToLower(req.UnitID)
	b, err := hex.DecodeString(req.UnitID)
	if err != nil || len(b) != 32 {
		http.Error(w, "bad unit_id", 400)
		return
	}
	var uid [32]byte
	copy(uid[:], b)

	// claim_code: каноническая форма — Base58 без дефисов (регистр сохраняется!)
	code := strings.NewReplacer("-", "", " ", "").Replace(req.ClaimCode)

	var vinHash [32]byte
	if req.VIN != "" {
		// mock: соль в демо фиксированная; на проде соль off-chain у backend завода
		vinHash = sha256.Sum256(append([]byte("geocoin:parts:vinsalt:v0:"), []byte(req.VIN)...))
	}

	now := time.Now().Unix()
	err = s.Chain.Redeem(uid, code, vinHash, now)
	switch {
	case errors.Is(err, chain.ErrAlreadySpent):
		writeJSON(w, httpResp{OK: false, Error: "UNIT_ALREADY_SPENT", Message: "Код уже был погашен. Высокий риск подделки."}, 409)
	case errors.Is(err, chain.ErrBadClaimCode):
		writeJSON(w, httpResp{OK: false, Error: "BAD_CLAIM_CODE", Message: "Код не совпадает с заявленным хешем."}, 403)
	case errors.Is(err, chain.ErrBadProof):
		writeJSON(w, httpResp{OK: false, Error: "BAD_PROOF", Message: "Merkle-прут не подтверждает принадлежность батчу."}, 403)
	case errors.Is(err, chain.ErrUnitNotFound):
		writeJSON(w, httpResp{OK: false, Error: "UNIT_NOT_FOUND", Message: "Единица не найдена."}, 404)
	case err != nil:
		log.Printf("redeem: %v", err)
		http.Error(w, "internal", 500)
	default:
		st := s.unitState(req.UnitID, now)
		writeJSON(w, httpResp{OK: true, Message: "Деталь погашена и привязана к автомобилю. Жизненный цикл закрыт.", Unit: st})
	}
}

type httpResp struct {
	OK      bool      `json:"ok"`
	Error   string    `json:"error,omitempty"`
	Message string    `json:"message,omitempty"`
	Unit    unitState `json:"unit,omitempty"`
}

func (s *Server) handleWeb(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(webChecker)
}

func writeJSON(w http.ResponseWriter, v any, code ...int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if len(code) > 0 {
		w.WriteHeader(code[0])
	}
	_ = json.NewEncoder(w).Encode(v)
}
