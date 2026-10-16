package article_test

import (
	"strings"
	"testing"

	"openusenet/internal/article"
)

func TestDecodeWordsAdjacentBQWords(t *testing.T) {
	in := "=?UTF-8?B?UmU6IGxlIHZpcnVzIGEgZMOpasOgIGNvw7t0w6kgbGEgdmllIMOgIHBy?= =?UTF-8?Q?_=C3=A8s_de_1000_enfants_depuis_le_d=C3=A9but_de_l=27=C3=A9pid?= =?UTF-8?Q?=C3=A9mie_=2C_six_mois_plus_t=C3=B4t?="
	got := article.DecodeWords(in)
	for _, want := range []string{"Re:", "déjà", "coûté", "enfants", "épidémie"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "=?") {
		t.Fatalf("still encoded: %q", got)
	}
}

func TestDecodeWordsUTF8QWords(t *testing.T) {
	in := "=?utf-8?q?=5BS=5D_=E5=AD=98=E5=9C=A8=E6=AF=94=E5=9C=B0=E7=90=83?= =?utf-8?q?=E6=9B=B4=E5=AE=9C=E5=B1=85=E7=9A=84=E6=98=9F=E7=90=83?="
	got := article.DecodeWords(in)
	want := "[S] 存在比地球更宜居的星球"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDecodeHeaderBlockOrganization(t *testing.T) {
	head := "Organization: =?UTF-8?Q?Si_a_40_ans_t=27es_pas_hasbeen_t=27as_rat=C3=A9_ta_v?= =?UTF-8?Q?ie?=\r\n"
	got := article.DecodeHeaderBlock(head)
	if !strings.Contains(got, "raté") || strings.Contains(got, "=?") {
		t.Fatalf("%q", got)
	}
}

func TestDecodeBodyQuotedPrintableUTF8(t *testing.T) {
	headers := "Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n"
	body := "[S] =E5=AD=98=E5=9C=A8=E6=AF=94=E5=9C=B0=E7=90=83=E6=9B=B4=E5=AE=9C=E5=B1=\r\n" +
		"=85=E7=9A=84=E6=98=9F=E7=90=83\r\n" +
		"https://www.solidot.org/story?sid=3D85606\r\n"
	got := article.DecodeBodyForDisplay(headers, body)
	if !strings.Contains(got, "[S] 存在比地球更宜居的星球") {
		t.Fatalf("decoded: %q", got)
	}
	if !strings.Contains(got, "sid=85606") {
		t.Fatalf("qp equals: %q", got)
	}
	if strings.Contains(got, "=E5") || strings.Contains(got, "=3D") {
		t.Fatalf("still quoted-printable: %q", got)
	}
}

func TestDecodeBodyEightBitUTF8(t *testing.T) {
	headers := "Content-Type: text/plain; charset=UTF-8; format=flowed\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n"
	body := "t'as même pas capté\nvous vivez tous pété, nom de djieu.\n"
	got := article.DecodeBodyForDisplay(headers, body)
	if !strings.Contains(got, "même") || !strings.Contains(got, "pété") {
		t.Fatalf("%q", got)
	}
}

func TestDecodeBodyLatin1QuotedPrintable(t *testing.T) {
	headers := "Content-Type: text/plain; charset=iso-8859-1\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n"
	got := article.DecodeBodyForDisplay(headers, "caf=E9\r\n")
	if got != "café\r\n" && got != "café\n" && !strings.Contains(got, "café") {
		t.Fatalf("%q", got)
	}
}
