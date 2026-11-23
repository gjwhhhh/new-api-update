package conversationaudit

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/QuantumNous/new-api/common"
)

const cyberCaptureChunkBytes = 64 * 1024

// Cyber responses must be retained before their terminal policy result is known.
// Spill encrypted chunks to disk, then discard ordinary requests' raw payloads.
type cyberPayloadCapture struct {
	version string
	aad     string
	buffer  bytes.Buffer
	file    *os.File
	chunks  int
	err     error
}

type cyberCaptureChunk struct {
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func (p *cyberPayloadCapture) Write(data []byte) {
	if p == nil || p.err != nil {
		return
	}
	for len(data) > 0 {
		count := min(len(data), cyberCaptureChunkBytes-p.buffer.Len())
		p.buffer.Write(data[:count])
		data = data[count:]
		if p.buffer.Len() == cyberCaptureChunkBytes {
			p.err = p.flush()
			if p.err != nil {
				return
			}
		}
	}
}

func (p *cyberPayloadCapture) flush() error {
	if p.buffer.Len() == 0 {
		return nil
	}
	if p.file == nil {
		file, err := os.CreateTemp("", "new-api-cyber-audit-*")
		if err != nil {
			return err
		}
		p.file = file
	}
	// Binding each chunk to its request and sequence prevents reordering.
	aad := fmt.Sprintf("%s|%d", p.aad, p.chunks)
	nonce, ciphertext, err := encrypt(p.version, p.buffer.String(), aad)
	if err != nil {
		return err
	}
	encoded, err := common.Marshal(cyberCaptureChunk{Nonce: nonce, Ciphertext: ciphertext})
	if err != nil {
		return err
	}
	if _, err := p.file.Write(append(encoded, '\n')); err != nil {
		return err
	}
	p.chunks++
	p.buffer.Reset()
	return nil
}

func (p *cyberPayloadCapture) Body() (string, error) {
	if p.err != nil {
		return "", p.err
	}
	if p.file == nil {
		return p.buffer.String(), nil
	}
	if err := p.flush(); err != nil {
		return "", err
	}
	if _, err := p.file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	reader := bufio.NewReader(p.file)
	var body bytes.Buffer
	for sequence := range p.chunks {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return "", err
		}
		var chunk cyberCaptureChunk
		if err := common.Unmarshal(line, &chunk); err != nil {
			return "", err
		}
		text, err := decrypt(p.version, chunk.Nonce, chunk.Ciphertext, fmt.Sprintf("%s|%d", p.aad, sequence))
		if err != nil {
			return "", err
		}
		body.WriteString(text)
	}
	return body.String(), nil
}

func (p *cyberPayloadCapture) Close() {
	if p == nil || p.file == nil {
		return
	}
	name := p.file.Name()
	_ = p.file.Close()
	_ = os.Remove(name)
}

// Preserve large client bodies lazily when summary parsing skips BodyStorage.
// Provider middleware may replace that storage before the result is known.
type cyberPayloadReader struct {
	io.ReadCloser
	capture   *cyberPayloadCapture
	readBytes int64
	readError error
}

func (r *cyberPayloadReader) Read(data []byte) (int, error) {
	n, err := r.ReadCloser.Read(data)
	r.capture.Write(data[:n])
	r.readBytes += int64(n)
	if err != nil {
		r.readError = err
	}
	return n, err
}
