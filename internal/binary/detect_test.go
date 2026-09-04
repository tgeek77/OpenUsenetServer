package binary

import "testing"

func TestLooksBinaryYEnc(t *testing.T) {
	body := "something\r\n=ybegin line=128 size=1000 name=foo.rar\r\n=yend\r\n"
	if !LooksBinary("Subject: file\r\n", body) {
		t.Fatal("expected yEnc binary")
	}
}

func TestLooksBinaryUuencode(t *testing.T) {
	body := "begin 644 file.bin\r\nM1234\r\n`\r\nend\r\n"
	if !LooksBinary("", body) {
		t.Fatal("expected uuencode binary")
	}
}

func TestLooksBinaryPlainText(t *testing.T) {
	body := "Hello folks,\r\n\r\nThis is a normal text post about cooking.\r\n"
	if LooksBinary("Content-Type: text/plain\r\n", body) {
		t.Fatal("plain text should not be binary")
	}
}

func TestLooksBinaryMIMEImage(t *testing.T) {
	hdr := "Content-Type: image/jpeg\r\n"
	body := stringsRepeat("x", 600)
	if !LooksBinary(hdr, body) {
		t.Fatal("expected image MIME binary")
	}
}

func TestLooksBinaryBulkBase64(t *testing.T) {
	line := stringsRepeat("A", 72)
	var b string
	for i := 0; i < 10; i++ {
		b += line + "\r\n"
	}
	if !LooksBinary("", b) {
		t.Fatal("expected bulk base64")
	}
}

func stringsRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
