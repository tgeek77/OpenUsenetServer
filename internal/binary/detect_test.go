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

func TestLooksBinaryPGPSignatureNotBinary(t *testing.T) {
	// Typical Debian BTS / mailing-list multipart/signed armor.
	body := "Hello,\r\nthis is a text bug report with a patch attachment mention.\r\n" +
		"-----BEGIN PGP SIGNATURE-----\r\n" +
		"Version: GnuPG v2\r\n" +
		"\r\n" +
		"iQIzBAABCgAdFiEE+JIdOnQEyG4RNSIVxxl2mbKbIyoFAmqrCawACgkQxxl2mbKb\r\n" +
		"Iyo5uQ/+Jv735aVc5hIxdxnzPmbrMsk7dofAam5VLQREQjg/+ZfDnMIg/RyYeNgZ\r\n" +
		"Ra5Ssnc4GWefU1tUN4/ZCkIbTnkXrdSQv6nbu++YqE6j0Mzwz3S5k2B0cfBpmlFe\r\n" +
		"nsZ+Dcb0qL6KzieTCaJ8momxkbOCcHuV3ePY2NZ5HX9e3rvMCk0HT7AuYcDvV3bY\r\n" +
		"ZzufnEDkg5XzLgQk6FvsbKu+0xkXYpWEkPkBx2LXCtlQoZo8ATHiEwo8tJnikgpr\r\n" +
		"mTLoWQ0OhhA7xf6+fOPvalalD7e1M0jNE/t7wnevZSiTtUS+kK9Qlhv598bETV6A\r\n" +
		"Shru/O9y8eGy68Lx2A0/lUS5vgLKun8FAXRF0TN=\r\n" +
		"=abcd\r\n" +
		"-----END PGP SIGNATURE-----\r\n"
	hdr := "Content-Type: multipart/signed; protocol=\"application/pgp-signature\"\r\n"
	if LooksBinary(hdr, body) {
		t.Fatal("PGP-signed text mail should not be binary")
	}
}

func TestLooksBinaryBulkBase64OutsidePGP(t *testing.T) {
	line := stringsRepeat("B", 72)
	var payload string
	for i := 0; i < 10; i++ {
		payload += line + "\r\n"
	}
	body := "-----BEGIN PGP SIGNATURE-----\r\n" +
		line + "\r\n" + line + "\r\n" +
		"-----END PGP SIGNATURE-----\r\n" +
		payload
	if !LooksBinary("", body) {
		t.Fatal("bulk base64 outside PGP armor should still count")
	}
}

func stringsRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
