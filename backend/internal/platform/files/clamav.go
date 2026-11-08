package files

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// ClamAV talks to a clamd daemon using its INSTREAM protocol.
type ClamAV struct{ Addr string }

// Scan sends the bytes to clamd. An error means the result is unknown, and the file stays unusable.
func (c ClamAV) Scan(ctx context.Context, body []byte) (bool, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", c.Addr)
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Minute)); err != nil {
		return false, err
	}
	if _, err := conn.Write([]byte("zINSTREAM\x00")); err != nil {
		return false, err
	}
	const chunk = 64 << 10
	for r := bytes.NewReader(body); r.Len() > 0; {
		n := min(r.Len(), chunk)
		buf := make([]byte, n)
		if _, err := r.Read(buf); err != nil {
			return false, err
		}
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(n)) //nolint:gosec // At most 64 KiB.
		if _, err := conn.Write(append(size[:], buf...)); err != nil {
			return false, err
		}
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return false, err
	}
	reply, err := bufio.NewReader(conn).ReadString(0)
	if err != nil {
		return false, err
	}
	reply = strings.TrimRight(reply, "\x00\n ")
	switch {
	case strings.HasSuffix(reply, "OK"):
		return true, nil
	case strings.HasSuffix(reply, "FOUND"):
		return false, nil
	}
	return false, fmt.Errorf("clamd replied: %s", reply)
}

// NoScanner refuses every file. It is the default, so a missing scanner cannot pass files as clean.
type NoScanner struct{}

// Scan always fails, which leaves the file unusable, and the job retrying.
func (NoScanner) Scan(context.Context, []byte) (bool, error) {
	return false, errors.New("no malware scanner is configured")
}

// AllowAllScanner marks everything clean. Local development only: config refuses it elsewhere.
type AllowAllScanner struct{}

// Scan always passes.
func (AllowAllScanner) Scan(context.Context, []byte) (bool, error) { return true, nil }
