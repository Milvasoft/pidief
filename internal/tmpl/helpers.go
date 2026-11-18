package tmpl

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type helperFunc func(r *renderer, args []any) (any, error)

var helpers map[string]helperFunc

// zeroArgHelpers may be written without arguments ({{bugun}}).
var zeroArgHelpers = map[string]bool{"bugun": true, "today": true, "simdi": true, "now": true}

func init() {
	helpers = map[string]helperFunc{
		// tr-TR formatting
		"para": hPara, "currency": hPara,
		"sayi": hSayi, "number": hSayi,
		"yuzde": hYuzde, "percent": hYuzde,
		"tarih": hTarih, "date": hTarih,
		"yaziyla": hYaziyla, "inwords": hYaziyla,
		"buyuk": hUpper, "upper": hUpper,
		"kucuk": hLower, "lower": hLower,
		"baslik": hTitle, "title": hTitle,
		"bugun": hNow, "today": hNow, "simdi": hNow, "now": hNow,
		// logic
		"eq": cmp(func(c int) bool { return c == 0 }), "ne": cmp(func(c int) bool { return c != 0 }),
		"gt": cmp(func(c int) bool { return c > 0 }), "gte": cmp(func(c int) bool { return c >= 0 }),
		"lt": cmp(func(c int) bool { return c < 0 }), "lte": cmp(func(c int) bool { return c <= 0 }),
		"and": hAnd, "or": hOr, "not": hNot,
		"default": hDefault, "varsayilan": hDefault,
		// math (exact decimals)
		"add": arith((*big.Rat).Add), "topla": arith((*big.Rat).Add),
		"sub": arith((*big.Rat).Sub), "cikar": arith((*big.Rat).Sub),
		"mul": arith((*big.Rat).Mul), "carp": arith((*big.Rat).Mul),
		"div": hDiv, "bol": hDiv,
		"round": hRound, "yuvarla": hRound,
		// lists and strings
		"toplam": hSum, "sum": hSum,
		"length": hLen, "uzunluk": hLen,
		"join": hJoin, "birlestir": hJoin,
		"json":     hJSON,
		"concat":   hConcat,
		"replace":  hReplace,
		"truncate": hTruncate,
	}
}

// ---------- numbers ----------

func toRat(v any) (*big.Rat, bool) {
	switch t := v.(type) {
	case nil:
		return nil, false
	case *big.Rat:
		return t, true
	case json.Number:
		r, ok := new(big.Rat).SetString(t.String())
		return r, ok
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return nil, false
		}
		return new(big.Rat).SetFloat64(t), true
	case int:
		return new(big.Rat).SetInt64(int64(t)), true
	case int64:
		return new(big.Rat).SetInt64(t), true
	case bool:
		return nil, false
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil, false
		}
		// Accept Turkish notation "1.234,56" as well as "1234.56".
		if strings.Contains(s, ",") {
			s = strings.ReplaceAll(s, ".", "")
			s = strings.ReplaceAll(s, ",", ".")
		}
		r, ok := new(big.Rat).SetString(s)
		return r, ok
	}
	return nil, false
}

func formatFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// roundRat rounds half away from zero to dec decimals.
func roundRat(r *big.Rat, dec int) *big.Rat {
	scale := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))
	x := new(big.Rat).Mul(r, scale)
	num, den := x.Num(), x.Denom()
	q, m := new(big.Int).QuoRem(num, den, new(big.Int))
	twice := new(big.Int).Mul(new(big.Int).Abs(m), big.NewInt(2))
	if twice.Cmp(den) >= 0 {
		if num.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return new(big.Rat).Quo(new(big.Rat).SetInt(q), scale)
}

// FormatNumber formats r with tr-TR grouping ("1.234.567,89"). dec < 0 trims trailing zeros (max 6).
func FormatNumber(r *big.Rat, dec int) string {
	trim := dec < 0
	if trim {
		dec = 6
	}
	s := roundRat(r, dec).FloatString(dec)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, frac, _ := strings.Cut(s, ".")
	if trim {
		frac = strings.TrimRight(frac, "0")
	}
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	out := b.String()
	if frac != "" {
		out += "," + frac
	}
	if neg && strings.Trim(out, "0.,") != "" {
		out = "-" + out
	}
	return out
}

func intArg(args []any, i, def int) int {
	if len(args) <= i {
		return def
	}
	if r, ok := toRat(args[i]); ok {
		f, _ := r.Float64()
		return int(f)
	}
	return def
}

func strArg(args []any, i int, def string) string {
	if len(args) <= i || args[i] == nil {
		return def
	}
	return toString(args[i])
}

var symbols = map[string]string{"TRY": "₺", "₺": "₺", "USD": "$", "EUR": "€", "GBP": "£"}

func hPara(_ *renderer, args []any) (any, error) {
	if len(args) == 0 {
		return nil, errors.New("usage: para amount [currency] [decimals]")
	}
	r, ok := toRat(args[0])
	if !ok {
		return "", nil
	}
	cur := strings.ToUpper(strArg(args, 1, "TRY"))
	n := FormatNumber(r, intArg(args, 2, 2))
	if sym, ok := symbols[cur]; ok {
		if strings.HasPrefix(n, "-") {
			return "-" + sym + n[1:], nil
		}
		return sym + n, nil
	}
	return n + " " + strArg(args, 1, "TL"), nil
}

func hSayi(_ *renderer, args []any) (any, error) {
	if len(args) == 0 {
		return nil, errors.New("usage: sayi value [decimals]")
	}
	r, ok := toRat(args[0])
	if !ok {
		return "", nil
	}
	return FormatNumber(r, intArg(args, 1, -1)), nil
}

func hYuzde(_ *renderer, args []any) (any, error) {
	if len(args) == 0 {
		return nil, errors.New("usage: yuzde value [decimals]")
	}
	r, ok := toRat(args[0])
	if !ok {
		return "", nil
	}
	return "%" + FormatNumber(r, intArg(args, 1, -1)), nil
}

func hRound(_ *renderer, args []any) (any, error) {
	r, ok := toRat(first(args))
	if !ok {
		return nil, nil
	}
	return roundRat(r, intArg(args, 1, 0)), nil
}

func arith(op func(z, x, y *big.Rat) *big.Rat) helperFunc {
	return func(_ *renderer, args []any) (any, error) {
		if len(args) < 2 {
			return nil, errors.New("needs at least two numbers")
		}
		acc, ok := toRat(args[0])
		if !ok {
			acc = new(big.Rat)
		}
		acc = new(big.Rat).Set(acc)
		for _, a := range args[1:] {
			v, ok := toRat(a)
			if !ok {
				v = new(big.Rat)
			}
			op(acc, acc, v)
		}
		return acc, nil
	}
}

func hDiv(_ *renderer, args []any) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("usage: div a b")
	}
	a, ok1 := toRat(args[0])
	b, ok2 := toRat(args[1])
	if !ok1 || !ok2 || b.Sign() == 0 {
		return nil, nil
	}
	return new(big.Rat).Quo(a, b), nil
}

func first(args []any) any {
	if len(args) == 0 {
		return nil
	}
	return args[0]
}

func hSum(_ *renderer, args []any) (any, error) {
	items, ok := list(first(args))
	if !ok {
		return nil, errors.New("usage: toplam list [field]")
	}
	field := strArg(args, 1, "")
	sum := new(big.Rat)
	for _, it := range items {
		v := it
		if field != "" {
			v, _ = resolve(it, strings.Split(field, "."))
		}
		if r, ok := toRat(v); ok {
			sum.Add(sum, r)
		}
	}
	return sum, nil
}

func hLen(_ *renderer, args []any) (any, error) {
	switch t := first(args).(type) {
	case []any:
		return len(t), nil
	case string:
		return len([]rune(t)), nil
	case map[string]any:
		return len(t), nil
	}
	return 0, nil
}

func hJoin(_ *renderer, args []any) (any, error) {
	items, _ := list(first(args))
	sep := strArg(args, 1, ", ")
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, toString(it))
	}
	return strings.Join(parts, sep), nil
}

func hJSON(_ *renderer, args []any) (any, error) {
	b, err := json.Marshal(first(args))
	return string(b), err
}

func hConcat(_ *renderer, args []any) (any, error) {
	var b strings.Builder
	for _, a := range args {
		b.WriteString(toString(a))
	}
	return b.String(), nil
}

func hReplace(_ *renderer, args []any) (any, error) {
	if len(args) != 3 {
		return nil, errors.New("usage: replace text old new")
	}
	return strings.ReplaceAll(toString(args[0]), toString(args[1]), toString(args[2])), nil
}

func hTruncate(_ *renderer, args []any) (any, error) {
	s := []rune(toString(first(args)))
	n := intArg(args, 1, 100)
	if len(s) <= n {
		return string(s), nil
	}
	return string(s[:n]) + "…", nil
}

// ---------- logic ----------

func compare(a, b any) int {
	ra, ok1 := toRat(a)
	rb, ok2 := toRat(b)
	if ok1 && ok2 {
		if _, isStr := a.(string); !isStr {
			return ra.Cmp(rb)
		}
		if _, isStr := b.(string); !isStr {
			return ra.Cmp(rb)
		}
	}
	return strings.Compare(toString(a), toString(b))
}

func cmp(pred func(int) bool) helperFunc {
	return func(_ *renderer, args []any) (any, error) {
		if len(args) != 2 {
			return nil, errors.New("needs two arguments")
		}
		return pred(compare(args[0], args[1])), nil
	}
}

func hAnd(_ *renderer, args []any) (any, error) {
	for _, a := range args {
		if !truthy(a) {
			return false, nil
		}
	}
	return len(args) > 0, nil
}

func hOr(_ *renderer, args []any) (any, error) {
	for _, a := range args {
		if truthy(a) {
			return true, nil
		}
	}
	return false, nil
}

func hNot(_ *renderer, args []any) (any, error) { return !truthy(first(args)), nil }

func hDefault(_ *renderer, args []any) (any, error) {
	if len(args) < 2 {
		return nil, errors.New("usage: default value fallback")
	}
	if truthy(args[0]) {
		return args[0], nil
	}
	return args[1], nil
}

// ---------- text (Turkish casing) ----------

var trUpper = unicode.TurkishCase

func hUpper(_ *renderer, args []any) (any, error) {
	return strings.ToUpperSpecial(trUpper, toString(first(args))), nil
}

func hLower(_ *renderer, args []any) (any, error) {
	return strings.ToLowerSpecial(trUpper, toString(first(args))), nil
}

func hTitle(_ *renderer, args []any) (any, error) {
	words := strings.Fields(toString(first(args)))
	for i, w := range words {
		rs := []rune(strings.ToLowerSpecial(trUpper, w))
		if len(rs) > 0 {
			rs[0] = unicode.TurkishCase.ToUpper(rs[0])
		}
		words[i] = string(rs)
	}
	return strings.Join(words, " "), nil
}

// ---------- dates ----------

var trMonths = []string{"Ocak", "Şubat", "Mart", "Nisan", "Mayıs", "Haziran", "Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık"}
var trDays = []string{"Pazar", "Pazartesi", "Salı", "Çarşamba", "Perşembe", "Cuma", "Cumartesi"}

func parseTime(v any, loc *time.Location) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t.In(loc), true
	case string:
		s := strings.TrimSpace(t)
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02", "02.01.2006", "02.01.2006 15:04"} {
			var tm time.Time
			var err error
			if strings.Contains(layout, "Z07") {
				tm, err = time.Parse(layout, s)
				if err == nil {
					return tm.In(loc), true
				}
				continue
			}
			tm, err = time.ParseInLocation(layout, s, loc)
			if err == nil {
				return tm, true
			}
		}
	case json.Number:
		if n, err := t.Int64(); err == nil {
			if n > 1e12 {
				return time.UnixMilli(n).In(loc), true
			}
			return time.Unix(n, 0).In(loc), true
		}
	}
	return time.Time{}, false
}

// FormatDate formats with .NET-style tokens: yyyy yy MMMM MMM MM M dddd ddd dd d HH H mm ss.
func FormatDate(t time.Time, f string) string {
	var b strings.Builder
	rs := []rune(f)
	for i := 0; i < len(rs); {
		c := rs[i]
		n := 1
		for i+n < len(rs) && rs[i+n] == c {
			n++
		}
		switch c {
		case 'y':
			if n >= 3 {
				fmt.Fprintf(&b, "%04d", t.Year())
			} else {
				fmt.Fprintf(&b, "%02d", t.Year()%100)
			}
		case 'M':
			switch {
			case n >= 4:
				b.WriteString(trMonths[t.Month()-1])
			case n == 3:
				b.WriteString(string([]rune(trMonths[t.Month()-1])[:3]))
			case n == 2:
				fmt.Fprintf(&b, "%02d", int(t.Month()))
			default:
				fmt.Fprintf(&b, "%d", int(t.Month()))
			}
		case 'd':
			switch {
			case n >= 4:
				b.WriteString(trDays[t.Weekday()])
			case n == 3:
				b.WriteString(string([]rune(trDays[t.Weekday()])[:3]))
			case n == 2:
				fmt.Fprintf(&b, "%02d", t.Day())
			default:
				fmt.Fprintf(&b, "%d", t.Day())
			}
		case 'H':
			if n >= 2 {
				fmt.Fprintf(&b, "%02d", t.Hour())
			} else {
				fmt.Fprintf(&b, "%d", t.Hour())
			}
		case 'm':
			fmt.Fprintf(&b, "%02d", t.Minute())
		case 's':
			fmt.Fprintf(&b, "%02d", t.Second())
		default:
			b.WriteString(strings.Repeat(string(c), n))
		}
		i += n
	}
	return b.String()
}

func hTarih(r *renderer, args []any) (any, error) {
	if len(args) == 0 {
		return nil, errors.New("usage: tarih value [format]")
	}
	t, ok := parseTime(args[0], r.opts.Location)
	if !ok {
		return toString(args[0]), nil
	}
	return FormatDate(t, strArg(args, 1, "dd.MM.yyyy")), nil
}

func hNow(r *renderer, args []any) (any, error) {
	return FormatDate(time.Now().In(r.opts.Location), strArg(args, 0, "dd.MM.yyyy")), nil
}

// ---------- amounts in words ----------

var ones = []string{"", "bir", "iki", "üç", "dört", "beş", "altı", "yedi", "sekiz", "dokuz"}
var tens = []string{"", "on", "yirmi", "otuz", "kırk", "elli", "altmış", "yetmiş", "seksen", "doksan"}
var scales = []string{"", "bin", "milyon", "milyar", "trilyon", "katrilyon"}

func below1000(n int) string {
	var parts []string
	if h := n / 100; h > 0 {
		if h > 1 {
			parts = append(parts, ones[h])
		}
		parts = append(parts, "yüz")
	}
	if t := (n % 100) / 10; t > 0 {
		parts = append(parts, tens[t])
	}
	if o := n % 10; o > 0 {
		parts = append(parts, ones[o])
	}
	return strings.Join(parts, " ")
}

// NumberInWords spells a non-negative integer in Turkish ("bin iki yüz otuz dört").
func NumberInWords(n *big.Int) string {
	if n.Sign() == 0 {
		return "sıfır"
	}
	var groups []int
	x := new(big.Int).Set(n)
	k := big.NewInt(1000)
	for x.Sign() > 0 {
		m := new(big.Int)
		x.QuoRem(x, k, m)
		groups = append(groups, int(m.Int64()))
	}
	var parts []string
	for i := len(groups) - 1; i >= 0; i-- {
		g := groups[i]
		if g == 0 {
			continue
		}
		if i >= len(scales) {
			return n.String()
		}
		if i == 1 && g == 1 {
			parts = append(parts, "bin") // "bin", not "bir bin"
			continue
		}
		parts = append(parts, below1000(g))
		if scales[i] != "" {
			parts = append(parts, scales[i])
		}
	}
	return strings.Join(parts, " ")
}

func hYaziyla(_ *renderer, args []any) (any, error) {
	r, ok := toRat(first(args))
	if !ok {
		return "", nil
	}
	cur := strings.ToUpper(strArg(args, 1, "TRY"))
	main, sub := "Türk lirası", "kuruş"
	switch cur {
	case "USD":
		main, sub = "ABD doları", "sent"
	case "EUR":
		main, sub = "avro", "sent"
	case "GBP":
		main, sub = "İngiliz sterlini", "peni"
	case "", "TRY", "TL", "₺":
	default:
		main, sub = cur, ""
	}
	neg := r.Sign() < 0
	cents := roundRat(new(big.Rat).Abs(r), 2)
	scaled := new(big.Rat).Mul(cents, big.NewRat(100, 1))
	total := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	whole, frac := new(big.Int).QuoRem(total, big.NewInt(100), new(big.Int))
	s := NumberInWords(whole) + " " + main
	if frac.Sign() > 0 && sub != "" {
		s += " " + NumberInWords(frac) + " " + sub
	}
	if neg {
		s = "eksi " + s
	}
	return s, nil
}
