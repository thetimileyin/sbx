package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
)

const (
	SeqRBF   = 0xfffffffd // signals BIP125 replaceability
	SeqFinal = 0xffffffff // opts out of BIP125 signalling
	P2A      = "51024e73" // pay-to-anchor: OP_1 <0x4e73>, spendable with an empty witness
)

type Coin struct {
	TxID string
	Vout uint32
	Sats int64
	SPK  string
}

type Out struct {
	Sats int64
	SPK  string
}

type Built struct {
	Hex, TxID, WTxID string
	VSize            int
	Fee              int64
	Outputs          []Coin
}

type Opts struct {
	FeeRate  float64 // sat/vB, matched against the real signed size
	FeeAbs   int64   // used instead of FeeRate when Absolute is set
	Absolute bool
	Version  uint32 // default 2
	Sequence uint32 // default SeqRBF
	Locktime uint32
	// Extra prevtx fields per input index (e.g. "witnessScript" for a multisig input).
	PrevExtra map[int]map[string]any
}

type decodedTx struct {
	TxID     string `json:"txid"`
	Hash     string `json:"hash"`
	Version  uint32 `json:"version"`
	VSize    int    `json:"vsize"`
	Weight   int    `json:"weight"`
	Locktime uint32 `json:"locktime"`
	Vin      []struct {
		TxID      string `json:"txid"`
		Vout      uint32 `json:"vout"`
		ScriptSig struct {
			Hex string `json:"hex"`
		} `json:"scriptSig"`
		Witness  []string `json:"txinwitness"`
		Sequence uint32   `json:"sequence"`
	} `json:"vin"`
	Vout []struct {
		Value float64 `json:"value"`
		N     uint32  `json:"n"`
		SPK   struct {
			Hex     string `json:"hex"`
			Type    string `json:"type"`
			Address string `json:"address"`
		} `json:"scriptPubKey"`
	} `json:"vout"`
}

func btcToSats(v float64) int64 { return int64(math.Round(v * 1e8)) }

// Builder constructs and signs transactions with the builder node's wallet without
// broadcasting them. Methods panic with buildErr; Engine.build recovers it.
type Builder struct {
	ctx  context.Context
	e    *Engine
	used map[string]bool
}

type buildErr struct{ error }

func (e *Engine) build(ctx context.Context, fn func(b *Builder)) (err error) {
	defer func() {
		if r := recover(); r != nil {
			be, ok := r.(buildErr)
			if !ok {
				panic(r)
			}
			err = be.error
		}
	}()
	fn(&Builder{ctx: ctx, e: e, used: map[string]bool{}})
	return nil
}

func (b *Builder) call(out any, method string, params ...any) {
	if err := b.e.wallet().Call(b.ctx, out, method, params...); err != nil {
		panic(buildErr{fmt.Errorf("%s: %w", method, err)})
	}
}

// FreshCoin returns a confirmed wallet coin not yet used in this build.
func (b *Builder) FreshCoin() Coin {
	var utxos []struct {
		TxID   string  `json:"txid"`
		Vout   uint32  `json:"vout"`
		Amount float64 `json:"amount"`
		SPK    string  `json:"scriptPubKey"`
	}
	b.call(&utxos, "listunspent", 1, 9999999)
	for _, u := range utxos {
		key := fmt.Sprintf("%s:%d", u.TxID, u.Vout)
		if !b.used[key] {
			b.used[key] = true
			return Coin{u.TxID, u.Vout, btcToSats(u.Amount), u.SPK}
		}
	}
	panic(buildErr{errors.New("the builder wallet has no unused coins; run: sbx up")})
}

// Address returns the scriptPubKey of a new wallet address.
func (b *Builder) Address() string { return b.spkOf("getnewaddress") }

func (b *Builder) change() string { return b.spkOf("getrawchangeaddress") }

func (b *Builder) spkOf(method string) string {
	var addr string
	b.call(&addr, method)
	var info struct {
		SPK string `json:"scriptPubKey"`
	}
	b.call(&info, "getaddressinfo", addr)
	return info.SPK
}

// Make signs a transaction spending inputs to fixed outputs plus a change output.
// Inputs may be unconfirmed outputs of other built transactions; pay-to-anchor
// inputs need no signature.
func (b *Builder) Make(inputs []Coin, fixed []Out, o Opts) Built {
	if o.Version == 0 {
		o.Version = 2
	}
	if o.Sequence == 0 {
		o.Sequence = SeqRBF
	}
	var total, fixedSum int64
	for _, in := range inputs {
		total += in.Sats
	}
	for _, out := range fixed {
		fixedSum += out.Sats
	}
	changeSPK := b.change()
	prevtxs := make([]map[string]any, len(inputs))
	anchors := map[string]bool{}
	for i, in := range inputs {
		prevtxs[i] = map[string]any{
			"txid": in.TxID, "vout": in.Vout, "scriptPubKey": in.SPK,
			"amount": fmt.Sprintf("%.8f", float64(in.Sats)/1e8),
		}
		for k, v := range o.PrevExtra[i] {
			prevtxs[i][k] = v
		}
		if in.SPK == P2A {
			anchors[fmt.Sprintf("%s:%d", in.TxID, in.Vout)] = true
		}
	}

	fee := int64(1000)
	switch {
	case o.Absolute:
		fee = o.FeeAbs
	case o.FeeRate == 0:
		fee = 0
	}
	var outs []Out
	var signedHex string
	var dec decodedTx
	for attempt := 0; attempt < 4; attempt++ {
		outs = append(append([]Out{}, fixed...), Out{total - fixedSum - fee, changeSPK})
		if outs[len(outs)-1].Sats < 0 {
			panic(buildErr{errors.New("inputs don't cover the outputs plus fee")})
		}
		var signed struct {
			Hex    string `json:"hex"`
			Errors []struct {
				TxID  string `json:"txid"`
				Vout  uint32 `json:"vout"`
				Error string `json:"error"`
			} `json:"errors"`
		}
		b.call(&signed, "signrawtransactionwithwallet", serialize(o.Version, inputs, o.Sequence, outs, o.Locktime), prevtxs)
		for _, se := range signed.Errors {
			if !anchors[fmt.Sprintf("%s:%d", se.TxID, se.Vout)] {
				panic(buildErr{fmt.Errorf("signing failed: %s", se.Error)})
			}
		}
		signedHex = signed.Hex
		dec = decodedTx{}
		b.call(&dec, "decoderawtransaction", signedHex)
		if o.Absolute {
			break
		}
		want := int64(math.Ceil(o.FeeRate * float64(dec.VSize)))
		if want == fee {
			break
		}
		fee = want
	}
	built := Built{Hex: signedHex, TxID: dec.TxID, WTxID: dec.Hash, VSize: dec.VSize, Fee: fee}
	for n, out := range outs {
		built.Outputs = append(built.Outputs, Coin{dec.TxID, uint32(n), out.Sats, out.SPK})
	}
	return built
}

// Simple spends one fresh coin: 10,000 sats to the wallet, extra outputs, rest to change.
func (b *Builder) Simple(feeRate float64, extra []Out, version uint32) string {
	outs := append([]Out{{10_000, b.Address()}}, extra...)
	return b.Make([]Coin{b.FreshCoin()}, outs, Opts{FeeRate: feeRate, Version: version}).Hex
}

func varint(n int) []byte {
	switch {
	case n < 0xfd:
		return []byte{byte(n)}
	case n <= 0xffff:
		return binary.LittleEndian.AppendUint16([]byte{0xfd}, uint16(n))
	default:
		return binary.LittleEndian.AppendUint32([]byte{0xfe}, uint32(n))
	}
}

// serialize returns an unsigned, non-witness transaction for the wallet to sign.
func serialize(version uint32, inputs []Coin, sequence uint32, outs []Out, locktime uint32) string {
	var buf bytes.Buffer
	buf.Write(binary.LittleEndian.AppendUint32(nil, version))
	buf.Write(varint(len(inputs)))
	for _, in := range inputs {
		txid, _ := hex.DecodeString(in.TxID)
		for i := len(txid) - 1; i >= 0; i-- {
			buf.WriteByte(txid[i])
		}
		buf.Write(binary.LittleEndian.AppendUint32(nil, in.Vout))
		buf.WriteByte(0) // empty scriptSig
		buf.Write(binary.LittleEndian.AppendUint32(nil, sequence))
	}
	buf.Write(varint(len(outs)))
	for _, out := range outs {
		script, _ := hex.DecodeString(out.SPK)
		buf.Write(binary.LittleEndian.AppendUint64(nil, uint64(out.Sats)))
		buf.Write(varint(len(script)))
		buf.Write(script)
	}
	buf.Write(binary.LittleEndian.AppendUint32(nil, locktime))
	return hex.EncodeToString(buf.Bytes())
}

// opReturn returns an OP_RETURN script carrying data.
func opReturn(data []byte) string {
	push := []byte{byte(len(data))}
	if len(data) > 75 {
		push = []byte{0x4c, byte(len(data))} // OP_PUSHDATA1
	}
	return "6a" + hex.EncodeToString(append(push, data...))
}
