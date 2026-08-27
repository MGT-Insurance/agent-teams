package promptsync

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

// encodeTOMLBasicMultiline encodes canonical prompt bytes for insertion inside
// an already-open TOML multiline basic string. Newlines remain literal so
// generated prompts stay reviewable; bytes with TOML syntax or escape meaning
// are escaped so decoding returns the exact canonical text.
func encodeTOMLBasicMultiline(content []byte) ([]byte, error) {
	if !utf8.Valid(content) {
		return nil, fmt.Errorf("content is not valid UTF-8")
	}

	var encoded bytes.Buffer
	encoded.Grow(len(content))
	for _, r := range string(content) {
		switch r {
		case '\b':
			encoded.WriteString(`\b`)
		case '\t':
			encoded.WriteString(`\t`)
		case '\n':
			encoded.WriteByte('\n')
		case '\f':
			encoded.WriteString(`\f`)
		case '\r':
			encoded.WriteString(`\r`)
		case '"':
			encoded.WriteString(`\"`)
		case '\\':
			encoded.WriteString(`\\`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&encoded, `\u%04X`, r)
				continue
			}
			encoded.WriteRune(r)
		}
	}
	return encoded.Bytes(), nil
}

func validateTOML(content []byte) error {
	var document map[string]any
	return toml.Unmarshal(content, &document)
}
