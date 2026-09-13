package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"os"
)

const MaxFrameBytes = 4 << 20

// MCP stdio is one JSON message per line. Bound and validate each line before
// handing it to the SDK's JSON decoder, including unterminated input.
type framedReader struct {
	scanner *bufio.Scanner
	source  io.ReadCloser
	pending []byte
}

func newFramedReader(source io.ReadCloser) *framedReader {
	s := bufio.NewScanner(source)
	s.Buffer(make([]byte, 4096), MaxFrameBytes)
	return &framedReader{scanner: s, source: source}
}
func (r *framedReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 {
		if !r.scanner.Scan() {
			if err := r.scanner.Err(); err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		line := r.scanner.Bytes()
		if !json.Valid(line) {
			return 0, errors.New("MCP frame must contain one JSON value")
		}
		r.pending = append(append([]byte(nil), line...), '\n')
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}
func (r *framedReader) Close() error { return r.source.Close() }
func (s *Server) Run(ctx context.Context) error {
	return s.MCP.Run(ctx, &mcp.IOTransport{Reader: newFramedReader(os.Stdin), Writer: os.Stdout})
}
