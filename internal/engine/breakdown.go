package engine

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
)

// The breakdown shows the transaction properties relay policy cares about. It only
// states what the transaction reveals: a Taproot key-path spend, for example, hides
// whether one key or several signed, and the breakdown says so instead of guessing.

type Breakdown struct {
	Weight       int          `json:"weight"`
	Locktime     uint32       `json:"locktime"`
	LocktimeText string       `json:"locktime_text"`
	RBF          bool         `json:"rbf"`
	Inputs       []InputInfo  `json:"inputs"`
	Outputs      []OutputInfo `json:"outputs"`
}

type InputInfo struct {
	N        int    `json:"n"`
	Prevout  string `json:"prevout"` // txid:vout
	Sats     *int64 `json:"sats,omitempty"`
	Type     string `json:"type"`  // e.g. "P2WSH (SegWit script)"
	Spend    string `json:"spend"` // plain-language spending conditions
	Sequence uint32 `json:"sequence"`
	Relative string `json:"relative,omitempty"` // BIP68 relative timelock, if any
}

type OutputInfo struct {
	N         int    `json:"n"`
	Sats      int64  `json:"sats"`
	Kind      string `json:"kind"` // raw script type from the node, e.g. "nulldata"
	Type      string `json:"type"`
	Address   string `json:"address,omitempty"`
	DataBytes int    `json:"data_bytes,omitempty"` // OP_RETURN payload size
	Dust      bool   `json:"dust,omitempty"`       // below the default dust threshold
	Note      string `json:"note,omitempty"`
}

type prevout struct {
	Sats int64
	Type string
	Hex  string
}

var typeNames = map[string]string{
	"pubkeyhash":            "P2PKH (legacy)",
	"scripthash":            "P2SH",
	"witness_v0_keyhash":    "P2WPKH (SegWit)",
	"witness_v0_scripthash": "P2WSH (SegWit script)",
	"witness_v1_taproot":    "P2TR (Taproot)",
	"nulldata":              "OP_RETURN",
	"multisig":              "Bare multisig",
	"anchor":                "P2A (anchor)",
	"pubkey":                "P2PK",
	"nonstandard":           "Non-standard",
	"witness_unknown":       "Future SegWit version",
}

func typeName(t string) string {
	if n, ok := typeNames[t]; ok {
		return n
	}
	if t == "" {
		return "Unknown"
	}
	return t
}

// Dust thresholds at the default dustrelayfee of 3 sat/vB.
var dustLimits = map[string]int64{
	"pubkeyhash": 546, "scripthash": 540, "witness_v0_keyhash": 294,
	"witness_v0_scripthash": 330, "witness_v1_taproot": 330, "multisig": 546, "pubkey": 546,
}

// prevouts finds the coin each input spends: first among the request's own
// transactions (packages, mempool setup), then in the builder's UTXO set.
func (e *Engine) prevouts(ctx context.Context, decoded map[string]decodedTx, txs []string) map[string]prevout {
	out := map[string]prevout{}
	for _, d := range decoded {
		for _, o := range d.Vout {
			out[fmt.Sprintf("%s:%d", d.TxID, o.N)] = prevout{btcToSats(o.Value), o.SPK.Type, o.SPK.Hex}
		}
	}
	for _, h := range txs {
		for _, vin := range decoded[h].Vin {
			key := fmt.Sprintf("%s:%d", vin.TxID, vin.Vout)
			if _, ok := out[key]; ok {
				continue
			}
			var utxo *struct {
				Value float64 `json:"value"`
				SPK   struct {
					Hex  string `json:"hex"`
					Type string `json:"type"`
				} `json:"scriptPubKey"`
			}
			if e.builder().Call(ctx, &utxo, "gettxout", vin.TxID, vin.Vout, false) == nil && utxo != nil {
				out[key] = prevout{btcToSats(utxo.Value), utxo.SPK.Type, utxo.SPK.Hex}
			}
		}
	}
	return out
}

func (e *Engine) decodeScript(ctx context.Context, scriptHex string) (asm, typ string) {
	var d struct {
		Asm  string `json:"asm"`
		Type string `json:"type"`
	}
	if e.builder().Call(ctx, &d, "decodescript", scriptHex) != nil {
		return "", ""
	}
	return d.Asm, d.Type
}

// describeScript turns a revealed script into plain language.
func describeScript(asm, typ string) string {
	toks := strings.Fields(asm)
	var parts []string
	switch {
	case typ == "multisig" && len(toks) >= 3:
		parts = append(parts, fmt.Sprintf("%s-of-%s multisig", toks[0], toks[len(toks)-2]))
	case strings.Contains(asm, "OP_CHECKSIGADD"):
		n := strings.Count(asm, "OP_CHECKSIGADD") + 1
		k := "?"
		for i, t := range toks {
			if (t == "OP_NUMEQUAL" || t == "OP_NUMEQUALVERIFY") && i > 0 {
				k = toks[i-1]
			}
		}
		parts = append(parts, fmt.Sprintf("%s-of-%d multisig", k, n))
	case strings.Count(asm, "OP_CHECKSIG") == 1 || strings.Count(asm, "OP_CHECKSIGVERIFY") == 1:
		parts = append(parts, "Single signature")
	case strings.Contains(asm, "OP_CHECKSIG"):
		parts = append(parts, "Several signatures")
	}
	for i, t := range toks {
		switch t {
		case "OP_CHECKLOCKTIMEVERIFY":
			v := ""
			if i > 0 {
				v = " " + toks[i-1]
			}
			parts = append(parts, "absolute timelock (CLTV"+v+")")
		case "OP_CHECKSEQUENCEVERIFY":
			v := ""
			if i > 0 {
				v = " " + toks[i-1]
			}
			parts = append(parts, "relative timelock (CSV"+v+")")
		case "OP_SHA256", "OP_HASH160", "OP_RIPEMD160", "OP_HASH256":
			if !strings.Contains(strings.Join(parts, " "), "hash lock") {
				parts = append(parts, "hash lock (secret required)")
			}
		}
	}
	if len(parts) == 0 {
		return "Custom script"
	}
	s := strings.Join(parts, ", ")
	return strings.ToUpper(s[:1]) + s[1:]
}

// lastPush returns the last data push of a scriptSig (the P2SH redeem script).
func lastPush(scriptSig string) string {
	b, err := hex.DecodeString(scriptSig)
	if err != nil {
		return ""
	}
	var last []byte
	for i := 0; i < len(b); {
		op := int(b[i])
		i++
		n := 0
		switch {
		case op >= 1 && op <= 75:
			n = op
		case op == 0x4c && i < len(b):
			n, i = int(b[i]), i+1
		case op == 0x4d && i+1 < len(b):
			n, i = int(b[i])|int(b[i+1])<<8, i+2
		default:
			continue
		}
		if i+n > len(b) {
			return ""
		}
		last = b[i : i+n]
		i += n
	}
	return hex.EncodeToString(last)
}

func (e *Engine) describeInput(ctx context.Context, typ, scriptSig string, witness []string) string {
	switch typ {
	case "pubkeyhash", "witness_v0_keyhash":
		return "Single signature"
	case "pubkey":
		return "Single signature (bare public key)"
	case "anchor":
		return "Anchor: anyone can spend it, no signature needed"
	case "witness_v0_scripthash":
		if len(witness) == 0 {
			return "Not revealed (no witness data)"
		}
		return describeScript(e.decodeScript(ctx, witness[len(witness)-1]))
	case "witness_v1_taproot":
		w := witness
		if len(w) >= 2 && strings.HasPrefix(w[len(w)-1], "50") { // drop the annex
			w = w[:len(w)-1]
		}
		switch {
		case len(w) == 1:
			return "Taproot key path: one signature. Whether one key or several combined keys (MuSig) signed isn't revealed"
		case len(w) >= 2:
			return "Taproot script path: " + strings.ToLower(describeScript(e.decodeScript(ctx, w[len(w)-2])))
		}
		return "Not revealed (no witness data)"
	case "scripthash":
		redeem := lastPush(scriptSig)
		switch {
		case strings.HasPrefix(redeem, "0014") && len(redeem) == 44:
			return "Single signature (SegWit nested in P2SH)"
		case strings.HasPrefix(redeem, "0020") && len(redeem) == 68 && len(witness) > 0:
			return describeScript(e.decodeScript(ctx, witness[len(witness)-1])) + " (SegWit nested in P2SH)"
		case redeem != "":
			return describeScript(e.decodeScript(ctx, redeem))
		}
		return "Not revealed"
	}
	// Unknown previous output: infer only what the witness shape makes certain.
	if len(witness) == 2 && len(witness[1]) == 66 {
		return "Single signature (inferred from the signature data)"
	}
	return "Not known: the coin being spent isn't available to inspect"
}

// relativeLock decodes a BIP68 relative timelock from an input's sequence.
func relativeLock(version, seq uint32) string {
	if version < 2 || seq&(1<<31) != 0 {
		return ""
	}
	v := seq & 0xffff
	if seq&(1<<22) != 0 {
		return fmt.Sprintf("Relative timelock: %d × 512 seconds after the coin confirmed", v)
	}
	if v == 0 {
		return ""
	}
	return fmt.Sprintf("Relative timelock: %d blocks after the coin confirmed", v)
}

func opReturnBytes(scriptHex string) int {
	if !strings.HasPrefix(scriptHex, "6a") {
		return 0
	}
	b, err := hex.DecodeString(scriptHex[2:])
	if err != nil {
		return 0
	}
	total := 0
	for i := 0; i < len(b); {
		op := int(b[i])
		i++
		n := 0
		switch {
		case op >= 1 && op <= 75:
			n = op
		case op == 0x4c && i < len(b):
			n, i = int(b[i]), i+1
		case op == 0x4d && i+1 < len(b):
			n, i = int(b[i])|int(b[i+1])<<8, i+2
		}
		if i+n > len(b) {
			break
		}
		total += n
		i += n
	}
	return total
}

func (e *Engine) breakdown(ctx context.Context, d decodedTx, prev map[string]prevout) *Breakdown {
	bd := &Breakdown{Weight: d.Weight, Locktime: d.Locktime}
	allFinal := true
	for i, vin := range d.Vin {
		in := InputInfo{N: i, Prevout: fmt.Sprintf("%s:%d", vin.TxID, vin.Vout), Sequence: vin.Sequence}
		p, known := prev[in.Prevout]
		if known {
			s := p.Sats
			in.Sats = &s
			in.Type = typeName(p.Type)
		} else {
			in.Type = "Unknown"
		}
		in.Spend = e.describeInput(ctx, p.Type, vin.ScriptSig.Hex, vin.Witness)
		in.Relative = relativeLock(d.Version, vin.Sequence)
		if vin.Sequence < 0xfffffffe {
			bd.RBF = true
		}
		if vin.Sequence != 0xffffffff {
			allFinal = false
		}
		bd.Inputs = append(bd.Inputs, in)
	}
	switch {
	case d.Locktime == 0:
		bd.LocktimeText = "None"
	case allFinal:
		bd.LocktimeText = fmt.Sprintf("%d, ignored because every input opts out", d.Locktime)
	case d.Locktime < 500_000_000:
		bd.LocktimeText = fmt.Sprintf("Not valid before block %d", d.Locktime)
	default:
		bd.LocktimeText = fmt.Sprintf("Not valid before Unix time %d", d.Locktime)
	}
	for _, o := range d.Vout {
		out := OutputInfo{N: int(o.N), Sats: btcToSats(o.Value), Kind: o.SPK.Type, Type: typeName(o.SPK.Type), Address: o.SPK.Address}
		switch o.SPK.Type {
		case "nulldata":
			out.DataBytes = opReturnBytes(o.SPK.Hex)
			out.Note = fmt.Sprintf("Carries %d bytes of data (script is %d bytes)", out.DataBytes, len(o.SPK.Hex)/2)
		case "anchor":
			if out.Sats == 0 {
				out.Note = "Zero-value anchor for a child to spend (ephemeral dust)"
			}
		case "multisig":
			out.Note = "Bare multisig output: some nodes don't relay these"
		default:
			if lim, ok := dustLimits[o.SPK.Type]; ok && out.Sats < lim {
				out.Dust = true
				out.Note = fmt.Sprintf("Below the %d-sat dust threshold for this output type", lim)
			}
		}
		bd.Outputs = append(bd.Outputs, out)
	}
	return bd
}
