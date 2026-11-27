// Package node — RPC-клиент изолированной ноды GeoCoin (degree-load) и
// якорение состояния реестра в L1 через учётный сайдчейн.
//
// Поток якорения (проверен вручную на degree-load):
//  1. createsidechaingenesis  — детерминированный genesis_hash
//  2. LOCK_DEPOSIT            — обеспечение сайдчейна (подпись владельца + witness)
//  3. REGISTER_ACCOUNTING_SIDECHAIN — реестр (namespace + schema_hash, policy 3/1)
//  4. createaccountingcheckpointproofv1 (SCA1) + BATCH_SIDECHAIN_CHECKPOINT
//     — чекпоинт state_root с подписью комитета (m=1)
//
// Подписи выполняет tools/falcon-signer (собран из статических библиотек
// основного репозитория, исходники L1 не модифицируются).
package node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Client — JSON-RPC 2.0 over HTTP клиент ноды.
type Client struct {
	URL     string
	User    string
	Pass    string
	HTTP    *http.Client
	Signer  string // путь к falcon-signer
	PrivKey string // hex32 — ключ владельца/комитета (только dev-режим)
}

func NewClient(url, user, pass, signer, privKey string) *Client {
	return &Client{
		URL:     strings.TrimSuffix(url, "/") + "/",
		User:    user,
		Pass:    pass,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
		Signer:  signer,
		PrivKey: privKey,
	}
}

// Call выполняет JSON-RPC запрос; params — []any (позиционные) или map.
func (c *Client) Call(method string, params any, result any) error {
	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", c.URL, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.User, c.Pass)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var parsed struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("%s: bad rpc body %s", method, string(body[:min(200, len(body))]))
	}
	if parsed.Error != nil {
		return fmt.Errorf("%s: rpc error %d: %s", method, parsed.Error.Code, parsed.Error.Message)
	}
	if result != nil {
		return json.Unmarshal(parsed.Result, result)
	}
	return nil
}

// SignPayload подписывает payload через falcon-signer (recoverable message).
func (c *Client) SignPayload(payloadHex string) (string, error) {
	cmd := exec.Command(c.Signer, "payload", c.PrivKey, payloadHex)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("falcon-signer payload: %v: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// SignTx подписывает транзакцию (sidechain_signature + witness входов).
func (c *Client) SignTx(txHex string) (string, error) {
	cmd := exec.Command(c.Signer, "tx", c.PrivKey, txHex)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("falcon-signer tx: %v: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// SidechainGenesis — ответ getsidechaingenesis.
type SidechainGenesis struct {
	GenesisHash string `json:"genesis_hash"`
	Status      string `json:"status"`
	SidechainID string `json:"sidechain_id"`
}

// Anchorer владеет жизненным циклом учётного сайдчейна «parts-units».
type Anchorer struct {
	Client      *Client
	SchemaHash  string
	Namespace   string
	ChainName   string
	DepositGC   float64 // обеспечение в GC
	CheckFee    uint64  // fee чекпоинта, angles
	NextHeight  uint64  // внутренняя высота сайдчейна (1,2,3...)
	GenesisHash string
	StatePath   string // файл якорного состояния (genesis/высота)

	logf func(format string, args ...any)

	// UniqueNamespace — добавлять уникальный суффикс к namespace (dev-прогоны).
	// false = использовать namespace как есть (mainnet: имя глобально уникально само).
	UniqueNamespace bool

	// Registered — REGISTER_ACCOUNTING_SIDECHAIN отправлен (чекпоинты можно строить).
	Registered bool

	// LastAnchorL1Height — высота L1 последнего успешного чекпоинта (интервал ≥5).
	LastAnchorL1Height uint64

	// LockTxID — LOCK_DEPOSIT отправлен (state переживает рестарт).
	LockTxID string
}

func LoadAnchorer(c *Client, schemaHash, namespace, chainName, statePath string, depositGC float64, checkFee uint64, uniqueNamespace bool, logf func(string, ...any)) (*Anchorer, error) {
	a := &Anchorer{
		Client: c, SchemaHash: schemaHash, Namespace: namespace, ChainName: chainName,
		DepositGC: depositGC, CheckFee: checkFee, NextHeight: 1, StatePath: statePath, UniqueNamespace: uniqueNamespace, logf: logf,
	}
	// восстановление состояния
	if raw, err := os.ReadFile(statePath); err == nil {
		var st struct {
			GenesisHash  string `json:"genesis_hash"`
			NextHeight   uint64 `json:"next_height"`
			LastL1Height uint64 `json:"last_l1_height"`
			LockTxID     string `json:"lock_txid"`
		}
		if json.Unmarshal(raw, &st) == nil && st.GenesisHash != "" {
			a.GenesisHash = st.GenesisHash
			if st.NextHeight > a.NextHeight {
				a.NextHeight = st.NextHeight
			}
			a.LastAnchorL1Height = st.LastL1Height
			a.LockTxID = st.LockTxID
		}
	}
	return a, nil
}

func (a *Anchorer) saveState() {
	raw, _ := json.MarshalIndent(map[string]any{
		"genesis_hash":   a.GenesisHash,
		"next_height":    a.NextHeight,
		"last_l1_height": a.LastAnchorL1Height,
	}, "", "  ")
	_ = os.MkdirAll(filepath.Dir(a.StatePath), 0o700)
	_ = os.WriteFile(a.StatePath, raw, 0o600)
}

// EnsureRegistered идемпотентно доводит сайдчейн до статуса active.
// Если genesis уже отправлен (есть в state) — НЕ создаём заново: повторяем
// только REGISTER, пока депозит не подтвердится (mainnet-блоки 6-10 мин).
func (a *Anchorer) EnsureRegistered() error {
	if a.GenesisHash != "" {
		var g SidechainGenesis
		if err := a.Client.Call("getsidechaingenesis", []any{a.GenesisHash}, &g); err == nil && g.Status == "active" {
			a.Registered = true
			a.logf("сайдчейн %s активен (высота %d)", a.GenesisHash[:12], a.NextHeight)
			return nil
		}
		// genesis уже отправлен; лока ещё нет — отправляем LOCK_DEPOSIT
		if a.LockTxID == "" {
			owner, err := a.ownerAddress()
			if err != nil {
				return err
			}
			unlockHeight, err := a.height()
			if err != nil {
				return err
			}
			lockTx, err := a.lockDeposit(owner, unlockHeight+200)
			if err != nil {
				return fmt.Errorf("lock: %w", err)
			}
			a.LockTxID = lockTx
			a.logf("LOCK_DEPOSIT отправлен: %s", lockTx)
			a.saveState()
		}
		// регистрация: депозит должен подтвердиться (mainnet-блоки 6-10 мин)
		owner, err := a.ownerAddress()
		if err != nil {
			return err
		}
		var regTx string
		for attempt := 1; attempt <= 360; attempt++ { // до 1 часа при паузе 10 c
			regTx, err = a.register(owner)
			if err == nil {
				break
			}
			if attempt%3 == 1 {
				a.logf("register: попытка %d: %v", attempt, err)
			}
			if !strings.Contains(err.Error(), "Депозит не найден") {
				return fmt.Errorf("register: %w", err)
			}
			time.Sleep(10 * time.Second)
		}
		if regTx == "" {
			return fmt.Errorf("register: депозит не подтвердился за отведённое время")
		}
		a.logf("REGISTER_ACCOUNTING_SIDECHAIN отправлен: %s", regTx)
		a.Registered = true
		a.saveState()
		return nil
	}

	// namespace глобально уникален в сети; для dev-прогонов добавляем суффикс
	if a.UniqueNamespace {
		a.Namespace = fmt.Sprintf("%s-%x", a.Namespace, time.Now().Unix()%1000000)
	}

	// 1) genesis
	var gen struct {
		GenesisHash string `json:"genesis_hash"`
	}
	genReq := map[string]any{
		"version": 2, "timestamp": time.Now().Unix(), "nonce": time.Now().Unix() % 100000,
		"chain_name": a.ChainName, "meta": "GeoCoin Parts registry",
		"sidechain_kind": "accounting", "schema_hash": a.SchemaHash,
	}
	if err := a.Client.Call("createsidechaingenesis", map[string]any{"request": genReq}, &gen); err != nil {
		return fmt.Errorf("genesis: %w", err)
	}
	a.GenesisHash = gen.GenesisHash
	a.logf("genesis сайдчейна: %s", a.GenesisHash)

	// сохраняем genesis сразу: рестарт должен продолжить, а не повторить
	a.saveState()

	owner, err := a.ownerAddress()
	if err != nil {
		return err
	}

	// 2) LOCK_DEPOSIT
	unlockHeight, err := a.height()
	if err != nil {
		return err
	}
	unlockHeight += 200
	lockTx, err := a.lockDeposit(owner, unlockHeight)
	if err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	a.LockTxID = lockTx
	a.saveState()
	a.logf("LOCK_DEPOSIT отправлен: %s", lockTx)

	// 3) REGISTER_ACCOUNTING_SIDECHAIN — депозит должен подтвердиться в цепи
	var regTx string
	for attempt := 1; attempt <= 360; attempt++ {
		regTx, err = a.register(owner)
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "Депозит не найден") {
			return fmt.Errorf("register: %w", err)
		}
		time.Sleep(10 * time.Second)
	}
	if regTx == "" {
		return fmt.Errorf("register: депозит не подтвердился за отведённое время")
	}
	a.logf("REGISTER_ACCOUNTING_SIDECHAIN отправлен: %s", regTx)

	a.Registered = true
	a.saveState()
	return nil
}

func (a *Anchorer) height() (uint64, error) {
	var h uint64
	err := a.Client.Call("getblockcount", map[string]any{}, &h)
	return h, err
}

// ownerAddress восстанавливает адрес из приватного ключа через falcon-signer
// addr-режим (без импорта ключа в кошелёк).
func (a *Anchorer) ownerAddress() (string, error) {
	cmd := exec.Command(a.Client.Signer, "addr", a.Client.PrivKey, "-")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("falcon-signer addr: %v: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// rpcTxRef — UTXO для входов транзакций якорения.
type rpcTxRef struct {
	TxID      string  `json:"txid"`
	Vout      int     `json:"vout"`
	Address   string  `json:"address,omitempty"`
	Amount    float64 `json:"amount"`
	Spendable bool    `json:"spendable"`
	Coinbase  bool    `json:"coinbase"`
	Height    uint64  `json:"height,omitempty"`
}

// selectUTXO — обычный (не coinbase) UTXO владельца на needAngles.
// Предпочтение getaddressutxos (точный адрес); fallback — listunspent.
func (a *Anchorer) selectUTXO(owner string, needAngles uint64) (*rpcTxRef, error) {
	var candidates []rpcTxRef
	if err := a.Client.Call("getaddressutxos", []any{owner}, &candidates); err != nil {
		var wide []rpcTxRef
		if err2 := a.Client.Call("listunspent", map[string]any{}, &wide); err2 != nil {
			return nil, fmt.Errorf("нет источника UTXO: %v / %v", err, err2)
		}
		for i := range wide {
			if wide[i].Address == owner {
				candidates = append(candidates, wide[i])
			}
		}
	}
	var fallbackCB *rpcTxRef
	for i := range candidates {
		u := &candidates[i]
		if u.Amount*1e8 < float64(needAngles) {
			continue
		}
		if !u.Coinbase {
			return u, nil // обычный UTXO — предпочтителен (нет coinbase-maturity)
		}
		if fallbackCB == nil {
			fallbackCB = u
		}
	}
	if fallbackCB != nil {
		return fallbackCB, nil
	}
	return nil, fmt.Errorf("нет UTXO %s на %d angles (пополните owner-адрес)", owner, needAngles)
}

// lockDeposit — LOCK_DEPOSIT: payload → falcon-подпись (sidechain_signature,
// затем witness входов) → preflight → sendrawtransaction.
func (a *Anchorer) lockDeposit(owner string, unlockHeight uint64) (string, error) {
	needAngles := uint64(a.DepositGC*1e8) + 60000 // + network fee
	utxo, err := a.selectUTXO(owner, needAngles)
	if err != nil {
		return "", err
	}
	inputAngles := uint64(utxo.Amount * 1e8)
	change := int64(inputAngles) - int64(needAngles)
	if change < 1000 {
		change = 1000
	}
	var lock struct {
		TxHex         string `json:"tx_hex"`
		PayloadToSign string `json:"payload_to_sign_hex"`
	}
	req := map[string]any{
		"genesis_hash":  a.GenesisHash,
		"owner_address": owner,
		"amount":        uint64(a.DepositGC * 1e8),
		"inputs":        []map[string]any{{"txid": utxo.TxID, "vout": utxo.Vout}},
		"outputs":       []map[string]any{{"address": owner, "value": change}},
		"unlock_height": unlockHeight,
		"validate":      true,
	}
	if err := a.Client.Call("createsidechainlockpayload", []any{req}, &lock); err != nil {
		return "", fmt.Errorf("createsidechainlockpayload: %w", err)
	}
	signed, err := a.Client.SignTx(lock.TxHex)
	if err != nil {
		return "", err
	}
	// предфлайт: reject должен быть виден до отправки в сеть
	var pf []struct {
		Allowed      bool   `json:"allowed"`
		RejectReason string `json:"reject_reason"`
		TxID         string `json:"txid"`
	}
	if err := a.Client.Call("testmempoolaccept", []any{[]string{signed}}, &pf); err != nil {
		return "", fmt.Errorf("lock preflight: %w", err)
	}
	if len(pf) != 1 || !pf[0].Allowed {
		return "", fmt.Errorf("lock preflight: allowed=false reject_reason=%q txid=%q", pf[0].RejectReason, pf[0].TxID)
	}
	var txid string
	if err := a.Client.Call("sendrawtransaction", []any{signed}, &txid); err != nil {
		return "", err
	}
	return txid, nil
}

// register — REGISTER_ACCOUNTING_SIDECHAIN (namespace + schema_hash, policy 3/1):
// payload подписывает владелец, finalize в RPC, далее sendrawtransaction.
func (a *Anchorer) register(owner string) (string, error) {
	var created struct {
		TxHex         string `json:"tx_hex"`
		PayloadToSign string `json:"payload_to_sign_hex"`
	}
	req := map[string]any{
		"genesis_hash":      a.GenesisHash,
		"owner_address":     owner,
		"namespace":         a.Namespace,
		"schema_hash":       a.SchemaHash,
		"committee_m":       1,
		"committee_members": []string{owner},
		"validate":          true,
	}
	if err := a.Client.Call("createaccountingsidechainpayload", []any{req}, &created); err != nil {
		// Депозит ещё не подтвердился — ретрай наверху.
		return "", err
	}
	sig, err := a.Client.SignPayload(created.PayloadToSign)
	if err != nil {
		return "", err
	}
	var fin struct {
		TxHex string `json:"tx_hex"`
	}
	if err := a.Client.Call("finalizeaccountingsidechain", []any{created.TxHex, sig}, &fin); err != nil {
		return "", err
	}
	var txid string
	if err := a.Client.Call("sendrawtransaction", []any{fin.TxHex}, &txid); err != nil {
		return "", err
	}
	return txid, nil
}

// AnchorState — чекпоинт state_root в L1: SCA1-доказательство (schema_hash,
// accounting v1) + batch checkpoint (fee из депозита), подпись комитета m=1.
// Консенсус требует интервал ≥5 L1-блоков между чекпоинтами.
func (a *Anchorer) AnchorState(root string) (string, error) {
	cur, err := a.height()
	if err != nil {
		return "", err
	}
	if a.LastAnchorL1Height > 0 && cur < a.LastAnchorL1Height+5 {
		return "", fmt.Errorf("anchor deferred: интервал ≥5 L1-блоков (последний на %d, сейчас %d)",
			a.LastAnchorL1Height, cur)
	}

	var proof struct {
		Checkpoint       string `json:"checkpoint"`
		CheckpointHeight uint64 `json:"checkpoint_height"`
		ProofHex         string `json:"proof_hex"`
	}
	pfReq := map[string]any{
		"sidechain_id":      a.GenesisHash,
		"state_root":        root,
		"checkpoint_height": a.NextHeight,
		"schema_hash":       a.SchemaHash,
	}
	if err := a.Client.Call("createaccountingcheckpointproofv1", []any{pfReq}, &proof); err != nil {
		return "", fmt.Errorf("createaccountingcheckpointproofv1: %w", err)
	}
	item := map[string]any{
		"sidechain_id":      a.GenesisHash,
		"checkpoint":        proof.Checkpoint,
		"checkpoint_height": proof.CheckpointHeight,
		"fee":               a.CheckFee,
		"proof_hex":         proof.ProofHex,
	}
	var created struct {
		TxHex    string `json:"tx_hex"`
		Payloads []struct {
			PayloadToSign string `json:"payload_to_sign_hex"`
		} `json:"payloads"`
	}
	if err := a.Client.Call("createbatchsidechaincheckpointpayload", []any{map[string]any{"items": []any{item}}}, &created); err != nil {
		return "", fmt.Errorf("createbatchsidechaincheckpointpayload: %w", err)
	}
	if len(created.Payloads) != 1 {
		return "", fmt.Errorf("checkpoint: неожиданное число payload: %d", len(created.Payloads))
	}
	sig, err := a.Client.SignPayload(created.Payloads[0].PayloadToSign)
	if err != nil {
		return "", err
	}
	var fin struct {
		TxHex string `json:"tx_hex"`
	}
	if err := a.Client.Call("finalizebatchsidechaincheckpoint", []any{created.TxHex, []string{sig}}, &fin); err != nil {
		return "", fmt.Errorf("finalizebatchsidechaincheckpoint: %w", err)
	}
	var pf []struct {
		Allowed      bool   `json:"allowed"`
		RejectReason string `json:"reject_reason"`
		TxID         string `json:"txid"`
	}
	if err := a.Client.Call("testmempoolaccept", []any{[]string{fin.TxHex}}, &pf); err != nil {
		return "", fmt.Errorf("checkpoint preflight: %w", err)
	}
	if len(pf) != 1 || !pf[0].Allowed {
		return "", fmt.Errorf("checkpoint preflight: allowed=false reject_reason=%q", pf[0].RejectReason)
	}
	var txid string
	if err := a.Client.Call("sendrawtransaction", []any{fin.TxHex}, &txid); err != nil {
		return "", err
	}
	a.LastAnchorL1Height = cur
	a.NextHeight++
	a.saveState()
	return txid, nil
}
