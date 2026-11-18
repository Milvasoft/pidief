package tmpl

import (
	"strings"
	"testing"
)

func render(t *testing.T, src, data string, o Options) string {
	t.Helper()
	tp, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d, err := DecodeJSON([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	out, err := tp.Render(d, o)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return out
}

func TestRender(t *testing.T) {
	data := `{"cari":{"unvan":"Örnek Yapı <A.Ş.>"},"tarih":"2026-11-18","oran":18.5,
	"kalemler":[{"poz":"15.150","ad":"Beton","tutar":1234567.555},{"poz":"15.160","ad":"Demir","tutar":"1.000,10"}],
	"onayli":true,"bos":[],"html":"<b>x</b>"}`
	cases := []struct{ src, want string }{
		{`{{cari.unvan}}`, `Örnek Yapı &lt;A.Ş.&gt;`},
		{`{{{html}}}`, `<b>x</b>`},
		{`{{#each kalemler}}{{@number}}.{{poz}}={{para tutar}};{{/each}}`, `1.15.150=₺1.234.567,56;2.15.160=₺1.000,10;`},
		{`{{#kalemler}}[{{ad}}]{{/kalemler}}`, `[Beton][Demir]`},
		{`{{#each kalemler as |kalem|}}{{kalem.ad}} {{../cari.unvan}}|{{/each}}`, `Beton Örnek Yapı &lt;A.Ş.&gt;|Demir Örnek Yapı &lt;A.Ş.&gt;|`},
		{`{{para (toplam kalemler "tutar")}}`, `₺1.235.567,66`},
		{`{{para 1234.5 "TL"}} {{para 10 "USD"}} {{para 5 "CHF"}}`, `1.234,50 TL $10,00 5,00 CHF`},
		{`{{tarih tarih "d MMMM yyyy dddd"}}`, `18 Kasım 2026 Çarşamba`},
		{`{{tarih tarih}}`, `18.11.2026`},
		{`{{yuzde oran}}`, `%18,5`},
		{`{{sayi 1234567.891 2}}`, `1.234.567,89`},
		{`{{#if onayli}}E{{else}}H{{/if}}{{#if bos}}x{{else if onayli}}Y{{/if}}`, `EY`},
		{`{{#each bos}}x{{else}}boş{{/each}}{{^bos}}!{{/bos}}`, `boş!`},
		{`{{#if (gt oran 10)}}büyük{{/if}}`, `büyük`},
		{`{{buyuk "istanbul ılık"}} {{kucuk "IŞIK İZMİR"}} {{baslik "çağrı İNŞAAT"}}`, `İSTANBUL ILIK ışık izmir Çağrı İnşaat`},
		{`{{yaziyla 1234.56}}`, `bin iki yüz otuz dört Türk lirası elli altı kuruş`},
		{`{{yaziyla 1000000}}`, `bir milyon Türk lirası`},
		{`{{! not }}a{{~ " b " ~}} c`, `a b c`},
		{`{{default yok "-"}}`, `-`},
	}
	for _, c := range cases {
		got := render(t, c.src, data, Options{})
		if got != c.want {
			t.Errorf("%s\n got: %s\nwant: %s", c.src, got, c.want)
		}
	}
}

func TestStrictAndErrors(t *testing.T) {
	tp, _ := Parse(`{{a.b}}`)
	if _, err := tp.Render(map[string]any{}, Options{Strict: true}); err == nil || !strings.Contains(err.Error(), "a.b") {
		t.Fatalf("want missing error, got %v", err)
	}
	for _, src := range []string{`{{#each x}}`, `{{/if}}`, `{{#if a}}{{/each}}`, `{{x`, `{{bilinmez a}}`} {
		tp, err := Parse(src)
		if err == nil {
			_, err = tp.Render(map[string]any{}, Options{})
		}
		if err == nil {
			t.Errorf("%q: want error", src)
		}
	}
	if got := render(t, `{{x}}`, `{}`, Options{Empty: "-"}); got != "-" {
		t.Errorf("empty placeholder: %q", got)
	}
}
