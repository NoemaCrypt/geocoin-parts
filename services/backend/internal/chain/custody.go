// Custody-модель реестра (Фаза 1): передача прав по цепочке «Завод → Дилер».
//
// Дилер/склад привязывается не к точке, а к ДОПУСТИМОМУ РЕГИОНУ:
// радиус (150–300 км) вокруг склада + код страны ISO-3166. Покупатель в
// соседнем городе (Борисов, Березино) не получает ложного срабатывания;
// скан в чужом регионе — «серый импорт» или контрафакт.
//
// Состояние custody живёт в runtime реестра (backend) и включается в
// state_root чекпоинтов L1 (см. api.receiptsRoot). Переводы подписываются
// операциями завода; для Фазы 1 аутентификация — dev-режим.
package chain

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"sync"
	"time"
)

// hexID — hex-представление 32-байтного идентификатора для записей custody.
func hexID(id [32]byte) string { return hex.EncodeToString(id[:]) }

// DealerZone — допустимый регион дилера/склада.
type DealerZone struct {
	DealerID string  `json:"dealer_id"`
	Country  string  `json:"country"` // ISO-3166 alpha-2
	Name     string  `json:"name"`              // базовое имя (fallback)
	Names    map[string]string `json:"names,omitempty"` // локализованные имена: ru/en/de/es/zh
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	RadiusKm float64 `json:"radius_km"` // 150–300 по умолчанию
}

// CustodyEvent — переход прав (запись в цепочку custody).
type CustodyEvent struct {
	From  string     `json:"from"` // «factory-weissach» / dealer_id
	To    string     `json:"to"`
	Zone  DealerZone `json:"zone"` // зона нового владельца
	TS    int64      `json:"ts"`
	Label string     `json:"label,omitempty"` // партия или unit_id
}

// CustodyRegistry — текущее custody по партиям/единицам (dev: в памяти + файл).
type CustodyRegistry struct {
	mu sync.Mutex

	batches map[[32]byte]*BatchCustody // batch_id -> custody
	units   map[[32]byte]*BatchCustody // по-unit переопределение (необязательно)
	path    string                    // файл сохранения (опционально)
}

// BatchCustody — состояние custody партии и её история.
type BatchCustody struct {
	Holder     string        `json:"holder"` // «factory-weissach» или dealer_id
	Zone       *DealerZone   `json:"zone,omitempty"`
	History    []CustodyEvent `json:"history"`
	LastChange int64         `json:"last_change_ts"`
}

func NewCustodyRegistry(path string) *CustodyRegistry {
	c := &CustodyRegistry{batches: map[[32]byte]*BatchCustody{}, units: map[[32]byte]*BatchCustody{}, path: path}
	c.Load()
	return c
}

// Transfer переводит партию новому владельцу с его геозоной.
func (c *CustodyRegistry) Transfer(batchID [32]byte, from string, zone DealerZone, ts int64) (*BatchCustody, error) {
	if zone.RadiusKm <= 0 {
		zone.RadiusKm = 300 // консервативный дефолт против ложных срабатываний
	}
	if ts == 0 {
		ts = time.Now().Unix()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, ok := c.batches[batchID]
	if !ok {
		cur = &BatchCustody{Holder: "factory", LastChange: ts}
		c.batches[batchID] = cur
	}
	ev := CustodyEvent{From: cur.Holder, To: zone.DealerID, Zone: zone, TS: ts, Label: hexID(batchID)}
	if from != "" && from != cur.Holder {
		ev.From = from
	}
	cur.History = append(cur.History, ev)
	cur.Holder = zone.DealerID
	cur.Zone = &zone
	cur.LastChange = ts
	c.persistLocked()
	return cur, nil
}

// ByBatch — состояние custody партии.
func (c *CustodyRegistry) ByBatch(batchID [32]byte) (*BatchCustody, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, ok := c.batches[batchID]
	return cur, ok
}

// HolderForUnit — custody единицы (наследование от партии + unit-override).
func (c *CustodyRegistry) HolderForUnit(unitID [32]byte, batchID [32]byte) (*BatchCustody, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if u, ok := c.units[unitID]; ok {
		return u, true
	}
	cur, ok := c.batches[batchID]
	return cur, ok
}

// TransferUnit — custody отдельной единицы (override партии).
func (c *CustodyRegistry) TransferUnit(unitID [32]byte, zone DealerZone, ts int64) {
	if zone.RadiusKm <= 0 {
		zone.RadiusKm = 300
	}
	if ts == 0 {
		ts = time.Now().Unix()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, ok := c.units[unitID]
	if !ok {
		cur = &BatchCustody{Holder: "factory", LastChange: ts}
		c.units[unitID] = cur
	}
	ev := CustodyEvent{From: cur.Holder, To: zone.DealerID, Zone: zone, TS: ts, Label: hexID(unitID)}
	cur.History = append(cur.History, ev)
	cur.Holder = zone.DealerID
	cur.Zone = &zone
	cur.LastChange = ts
}

// EventsSince — все события custody (для якорения в state_root).
func (c *CustodyRegistry) EventsSince(ts int64) []CustodyEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []CustodyEvent
	for _, bc := range c.batches {
		for _, ev := range bc.History {
			if ev.TS > ts {
				out = append(out, ev)
			}
		}
	}
	for _, bc := range c.units {
		for _, ev := range bc.History {
			if ev.TS > ts {
				out = append(out, ev)
			}
		}
	}
	return out
}

// persistLocked сохраняет custody-состояние в JSON (dev; на проде — RocksDB).
func (c *CustodyRegistry) persistLocked() {
	if c.path == "" {
		return
	}
	snap := custodySnapshot{
		Batches: make(map[string]*BatchCustody, len(c.batches)),
		Units:   make(map[string]*BatchCustody, len(c.units)),
	}
	for id, bc := range c.batches {
		snap.Batches[hexID(id)] = bc
	}
	for id, bc := range c.units {
		snap.Units[hexID(id)] = bc
	}
	raw, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(c.path, raw, 0o600)
}

type custodySnapshot struct {
	Batches map[string]*BatchCustody   `json:"batches"`
	Units   map[string]*BatchCustody   `json:"units"`
}

// Load читает сохранённое custody-состояние (если есть).
func (c *CustodyRegistry) Load() {
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	var snap custodySnapshot
	if json.Unmarshal(raw, &snap) != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, bc := range snap.Batches {
		if b, err := hex.DecodeString(k); err == nil && len(b) == 32 {
			var id [32]byte
			copy(id[:], b)
			c.batches[id] = bc
		}
	}
	for k, bc := range snap.Units {
		if b, err := hex.DecodeString(k); err == nil && len(b) == 32 {
			var id [32]byte
			copy(id[:], b)
			c.units[id] = bc
		}
	}
}
