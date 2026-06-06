package ftp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// ftpConn wraps a raw FTP control connection with helpers for common commands.
type ftpConn struct {
	conn net.Conn
	r    *bufio.Reader
}

func dialFTP(ctx context.Context, addr, user, pass string) (*ftpConn, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("ftp: dial %s: %w", addr, err)
	}
	fc := &ftpConn{conn: c, r: bufio.NewReader(c)}

	// Read banner
	if _, _, err := fc.readResponse(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("ftp: banner: %w", err)
	}

	username := strings.TrimSpace(user)
	password := pass
	if username == "" {
		username = "anonymous"
		password = "anonymous@"
	}

	// USER
	if err := fc.sendCommand("USER %s", username); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("ftp: USER: %w", err)
	}
	code, msg, err := fc.readResponse()
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("ftp: USER response: %w", err)
	}
	// 230 = logged in, 331 = need password
	if code == 331 {
		if err := fc.sendCommand("PASS %s", password); err != nil {
			_ = c.Close()
			return nil, fmt.Errorf("ftp: PASS: %w", err)
		}
		if _, _, err := fc.readResponse(); err != nil {
			_ = c.Close()
			return nil, fmt.Errorf("ftp: PASS response: %w", err)
		}
	} else if code != 230 {
		_ = c.Close()
		return nil, fmt.Errorf("ftp: login failed: %d %s", code, msg)
	}

	// Binary mode
	if err := fc.sendCommand("TYPE I"); err != nil {
		_ = c.Close()
		return nil, err
	}
	if _, _, err := fc.readResponse(); err != nil {
		_ = c.Close()
		return nil, err
	}

	return fc, nil
}

// Quit sends QUIT and closes the connection.
func (c *ftpConn) Quit() error {
	_ = c.sendCommand("QUIT")
	_, _, _ = c.readResponse()
	return c.conn.Close()
}

// List returns the directory listing for path (empty = current directory).
func (c *ftpConn) List(path string) ([]ftpEntry, error) {
	dc, err := c.dataConn("LIST " + path)
	if err != nil {
		return nil, err
	}
	defer dc.Close()

	var entries []ftpEntry
	scanner := bufio.NewScanner(dc)
	for scanner.Scan() {
		line := scanner.Text()
		if e := parseListLine(line); e != nil {
			entries = append(entries, *e)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Read the control connection response after data transfer
	if _, _, err := c.readResponse(); err != nil {
		return nil, err
	}
	return entries, nil
}

// Retr downloads a file from the given path.
func (c *ftpConn) Retr(path string) (io.ReadCloser, error) {
	dc, err := c.dataConn("RETR " + path)
	if err != nil {
		return nil, err
	}
	return &ftpDataConn{rc: dc, fc: c}, nil
}

// dataConn opens a passive-mode data connection for the given command.
func (c *ftpConn) dataConn(cmd string) (net.Conn, error) {
	// PASV
	if err := c.sendCommand("PASV"); err != nil {
		return nil, err
	}
	code, msg, err := c.readResponse()
	if err != nil {
		return nil, err
	}
	if code != 227 {
		return nil, fmt.Errorf("ftp: PASV failed: %d %s", code, msg)
	}
	host, port, err := parsePasv(msg)
	if err != nil {
		return nil, fmt.Errorf("ftp: parse PASV: %w", err)
	}

	// Send the actual command
	if err := c.sendCommandRaw(cmd); err != nil {
		return nil, err
	}

	// Connect to the data port
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dc, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("ftp: data dial %s: %w", addr, err)
	}
	return dc, nil
}

// sendCommand sends a formatted FTP command.
func (c *ftpConn) sendCommand(format string, args ...any) error {
	cmd := fmt.Sprintf(format, args...)
	return c.sendCommandRaw(cmd)
}

func (c *ftpConn) sendCommandRaw(cmd string) error {
	_, err := fmt.Fprintf(c.conn, "%s\r\n", cmd)
	return err
}

// readResponse reads the FTP response code and message.
func (c *ftpConn) readResponse() (int, string, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return 0, "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) < 4 {
		return 0, "", errors.New("ftp: short response")
	}
	code, err := strconv.Atoi(line[:3])
	if err != nil {
		return 0, "", fmt.Errorf("ftp: bad response code: %s", line)
	}
	msg := strings.TrimSpace(line[3:])

	// Handle multi-line responses (code-SPACE = last line, code-HYPHEN = more)
	if len(line) > 3 && line[3] == '-' {
		terminator := line[:3] + " "
		for {
			next, err := c.r.ReadString('\n')
			if err != nil {
				return code, msg, err
			}
			next = strings.TrimRight(next, "\r\n")
			if strings.HasPrefix(next, terminator) {
				msg += "\n" + strings.TrimSpace(next[4:])
				break
			}
			msg += "\n" + strings.TrimSpace(next)
		}
	}
	return code, msg, nil
}

// parsePasv extracts the (h1,h2,h3,h4,p1,p2) address from a PASV response.
func parsePasv(msg string) (string, int, error) {
	start := strings.Index(msg, "(")
	end := strings.LastIndex(msg, ")")
	if start < 0 || end <= start {
		return "", 0, errors.New("ftp: no parentheses in PASV response")
	}
	parts := strings.Split(msg[start+1:end], ",")
	if len(parts) != 6 {
		return "", 0, fmt.Errorf("ftp: unexpected PASV format: %s", msg[start+1:end])
	}
	nums := make([]int, 6)
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return "", 0, fmt.Errorf("ftp: PASV parse %q: %w", p, err)
		}
		nums[i] = n
	}
	host := fmt.Sprintf("%d.%d.%d.%d", nums[0], nums[1], nums[2], nums[3])
	port := nums[4]*256 + nums[5]
	return host, port, nil
}

// ftpEntry represents a single entry from an FTP LIST response.
type ftpEntry struct {
	Name  string
	IsDir bool
	Size  int64
}

// parseListLine parses a single line of FTP LIST output (Unix "ls -l" format).
func parseListLine(line string) *ftpEntry {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	// Unix format: "drwxr-xr-x  3 user group    4096 Jan 01 12:00 name"
	// or:         "-rw-r--r--  1 user group  12345 Jan 01 12:00 name"
	fields := strings.Fields(line)
	if len(fields) < 9 {
		return nil
	}
	perm := fields[0]
	if len(perm) < 1 {
		return nil
	}
	isDir := perm[0] == 'd'
	// Name is the last field (may contain spaces, but our Fields splits on spaces)
	// For simplicity, take everything after the 8th field (0-indexed) as the name
	name := strings.Join(fields[8:], " ")

	var size int64
	if !isDir {
		// Size is the 5th field (0-indexed: 4)
		if s, err := strconv.ParseInt(fields[4], 10, 64); err == nil {
			size = s
		}
	}

	// Skip . and ..
	if name == "." || name == ".." {
		return nil
	}
	return &ftpEntry{Name: name, IsDir: isDir, Size: size}
}

// ftpDataConn wraps a data connection so that closing it also reads the
// control connection's transfer-complete response.
type ftpDataConn struct {
	rc net.Conn
	fc *ftpConn
}

func (d *ftpDataConn) Read(p []byte) (int, error) { return d.rc.Read(p) }
func (d *ftpDataConn) Close() error {
	err := d.rc.Close()
	// Read the transfer-complete response from the control connection
	if _, _, rerr := d.fc.readResponse(); rerr != nil && err == nil {
		err = rerr
	}
	return err
}
