package article

import (
	"testing"

	"golang.org/x/text/encoding"
	"golang.org/x/text/transform"
)

func TestCharsetRoundTrip(t *testing.T) {
	samples := []struct {
		charset string
		text    string
	}{
		{"utf-8", "café こんにちは Привет 你好 한국어 مرحبا שלום ไทย"},
		{"iso-8859-1", "café naïve"},
		{"iso-8859-2", "Zażółć gęślą jaźń"},
		{"windows-1251", "Привет мир"},
		{"koi8-r", "Привет"},
		{"gb2312", "你好世界"},
		{"gbk", "你好世界"},
		{"big5", "繁體字"},
		{"shift_jis", "日本語"},
		{"euc-jp", "日本語"},
		{"euc-kr", "한국어"},
		{"iso-2022-jp", "日本語"},
		{"windows-1256", "مرحبا"},
		{"windows-1255", "שלום"},
		{"windows-874", "สวัสดี"},
		{"x-gbk", "你好"},
	}
	for _, tc := range samples {
		t.Run(tc.charset, func(t *testing.T) {
			enc, err := encodingForCharset(tc.charset)
			if err != nil || enc == nil {
				t.Fatalf("lookup: %v", err)
			}
			raw := tc.text
			if enc != encoding.Nop {
				out, _, err := transform.String(enc.NewEncoder(), tc.text)
				if err != nil {
					t.Fatalf("encode: %v", err)
				}
				raw = out
			}
			headers := "Content-Type: text/plain; charset=" + tc.charset + "\r\n" +
				"Content-Transfer-Encoding: 8bit\r\n"
			got := DecodeBodyForDisplay(headers, raw)
			if got != tc.text {
				t.Fatalf("got %q want %q", got, tc.text)
			}
		})
	}
}

func TestEncodingForCharsetUnknown(t *testing.T) {
	if _, err := encodingForCharset("not-a-real-charset"); err == nil {
		t.Fatal("expected error")
	}
}
