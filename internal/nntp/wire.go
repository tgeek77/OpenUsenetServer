package nntp

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// Conn speaks NNTP on a byte stream: command lines, replies, and
// RFC 3977 dot-stuffed multi-line blocks.
type Conn struct {
	c       net.Conn
	r       *bufio.Reader
	w       *bufio.Writer
	idle    time.Duration
	maxLine int
}

func NewConn(c net.Conn, idle time.Duration) *Conn {
	if idle <= 0 {
		idle = 3 * time.Minute
	}
	return &Conn{
		c:       c,
		r:       bufio.NewReaderSize(c, 64*1024),
		w:       bufio.NewWriterSize(c, 64*1024),
		idle:    idle,
		maxLine: MaxCommand,
	}
}

func (c *Conn) SetIdle(d time.Duration) { c.idle = d }

func (c *Conn) bumpRead() {
	_ = c.c.SetReadDeadline(time.Now().Add(c.idle))
}

func (c *Conn) bumpWrite() {
	_ = c.c.SetWriteDeadline(time.Now().Add(c.idle))
}

// ReadCommand reads one command line (max 512 octets including CRLF).
func (c *Conn) ReadCommand() (string, error) {
	c.maxLine = MaxCommand
	line, err := c.readLine()
	if err != nil {
		return "", err
	}
	return line, nil
}

func (c *Conn) readLine() (string, error) {
	c.bumpRead()
	var buf []byte
	for {
		b, err := c.r.ReadByte()
		if err != nil {
			return "", err
		}
		buf = append(buf, b)
		if len(buf) > c.maxLine {
			// Consume until CRLF so the next command is aligned, then error.
			for {
				if b == '\n' {
					break
				}
				b, err := c.r.ReadByte()
				if err != nil {
					return "", errTooLong
				}
				buf = append(buf, b)
				if len(buf) > 1<<20 {
					return "", errTooLong
				}
			}
			return "", errTooLong
		}
		if b == '\n' {
			end := len(buf) - 1
			if end > 0 && buf[end-1] == '\r' {
				end--
			}
			return string(buf[:end]), nil
		}
	}
}

var errTooLong = fmt.Errorf("line too long")

func IsTooLong(err error) bool { return err == errTooLong }

// ReadArticle reads a dot-stuffed multi-line block (POST/IHAVE body).
func (c *Conn) ReadArticle(maxBytes int) ([]byte, error) {
	c.bumpRead()
	var out []byte
	for {
		c.maxLine = 1 << 20 // article lines are not capped at 512
		line, err := c.readLine()
		if err != nil {
			return nil, err
		}
		c.bumpRead()
		if line == "." {
			return out, nil
		}
		if strings.HasPrefix(line, ".") {
			line = line[1:]
		}
		out = append(out, line...)
		out = append(out, '\r', '\n')
		if maxBytes > 0 && len(out) > maxBytes {
			// Drain to terminator.
			for {
				line, err := c.readLine()
				if err != nil {
					return nil, err
				}
				if line == "." {
					break
				}
			}
			return nil, errTooLong
		}
	}
}

// ReadReply reads one NNTP status line and returns its code and full line.
func (c *Conn) ReadReply() (int, string, error) {
	c.maxLine = MaxCommand
	line, err := c.readLine()
	if err != nil {
		return 0, "", err
	}
	if len(line) < 3 {
		return 0, line, fmt.Errorf("short reply %q", line)
	}
	var code int
	for i := 0; i < 3; i++ {
		if line[i] < '0' || line[i] > '9' {
			return 0, line, fmt.Errorf("bad reply %q", line)
		}
		code = code*10 + int(line[i]-'0')
	}
	return code, line, nil
}

// ReplyRaw writes a command line (no status code) and flushes.
func (c *Conn) ReplyRaw(line string) error {
	c.bumpWrite()
	if _, err := fmt.Fprintf(c.w, "%s\r\n", line); err != nil {
		return err
	}
	return c.w.Flush()
}

func (c *Conn) Reply(code int, text string) error {
	c.bumpWrite()
	if text == "" {
		_, err := fmt.Fprintf(c.w, "%d\r\n", code)
		if err != nil {
			return err
		}
	} else {
		_, err := fmt.Fprintf(c.w, "%d %s\r\n", code, text)
		if err != nil {
			return err
		}
	}
	return c.w.Flush()
}

func (c *Conn) ReplyArgs(code int, args []string, text string) error {
	c.bumpWrite()
	var b strings.Builder
	fmt.Fprintf(&b, "%d", code)
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(a)
	}
	if text != "" {
		b.WriteByte(' ')
		b.WriteString(text)
	}
	b.WriteString("\r\n")
	if _, err := c.w.WriteString(b.String()); err != nil {
		return err
	}
	return c.w.Flush()
}

// WriteBlock writes a 2xx/1xx initial line and a dot-stuffed block.
func (c *Conn) WriteBlock(code int, args []string, text string, body []byte) error {
	c.bumpWrite()
	if err := c.ReplyArgs(code, args, text); err != nil {
		return err
	}
	return c.WriteDot(body)
}

// WriteDot writes a pre-assembled article or text as a multi-line block.
// body uses CRLF line endings. Trailing CRLF on the last line is optional.
func (c *Conn) WriteDot(body []byte) error {
	c.bumpWrite()
	if len(body) > 0 {
		lines := splitCRLF(body)
		for _, line := range lines {
			if len(line) > 0 && line[0] == '.' {
				if err := c.w.WriteByte('.'); err != nil {
					return err
				}
			}
			if _, err := c.w.Write(line); err != nil {
				return err
			}
			if _, err := c.w.WriteString("\r\n"); err != nil {
				return err
			}
		}
	}
	if _, err := c.w.WriteString(".\r\n"); err != nil {
		return err
	}
	return c.w.Flush()
}

func splitCRLF(b []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			end := i
			if end > start && b[end-1] == '\r' {
				end--
			}
			lines = append(lines, b[start:end])
			start = i + 1
		}
	}
	if start < len(b) {
		end := len(b)
		if end > start && b[end-1] == '\r' {
			end--
		}
		lines = append(lines, b[start:end])
	}
	return lines
}

func (c *Conn) Close() error {
	_ = c.w.Flush()
	return c.c.Close()
}

func (c *Conn) Remote() string {
	if c.c == nil || c.c.RemoteAddr() == nil {
		return ""
	}
	return c.c.RemoteAddr().String()
}

func SplitArgs(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	return strings.Fields(line)
}

func IsTimeout(err error) bool {
	if err == nil {
		return false
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}
	return err == io.EOF
}
