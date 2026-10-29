// Package bundle reads and writes ProPresenter .probundle files. See docs/probundle-format.md.
package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"google.golang.org/protobuf/proto"

	"probundle/internal/rvpb"
)

const ManifestPath = ".procore/legacy-export-manifest.json"

type Member struct {
	Name string
	Data []byte
}

type Bundle struct {
	Members  []Member
	Manifest *Manifest // nil if the bundle has no manifest
	Notes    []string  // format problems that were repaired while reading
}

func (b *Bundle) Member(name string) *Member {
	for i := range b.Members {
		if NormalizeName(b.Members[i].Name) == NormalizeName(name) {
			return &b.Members[i]
		}
	}
	return nil
}

// Presentation decodes the first .pro member.
func (b *Bundle) Presentation() (*rvpb.Presentation, string, error) {
	for _, m := range b.Members {
		if strings.HasSuffix(m.Name, ".pro") {
			p := &rvpb.Presentation{}
			if err := proto.Unmarshal(m.Data, p); err != nil {
				return nil, "", fmt.Errorf("%s: %w", m.Name, err)
			}
			return p, m.Name, nil
		}
	}
	return nil, "", fmt.Errorf("bundle has no .pro member")
}

func Read(path string) (*Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b Bundle
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		fixed, note, ok := fixCentralDirectorySize(data)
		if !ok {
			return nil, err
		}
		b.Notes = append(b.Notes, note)
		if zr, err = zip.NewReader(bytes.NewReader(fixed), int64(len(fixed))); err != nil {
			return nil, err
		}
	}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		b.Members = append(b.Members, Member{Name: f.Name, Data: body})
	}
	if m := b.Member(ManifestPath); m != nil {
		b.Manifest = &Manifest{}
		if err := json.Unmarshal(m.Data, b.Manifest); err != nil {
			return nil, fmt.Errorf("%s: %w", ManifestPath, err)
		}
	}
	return &b, nil
}

// fixCentralDirectorySize repairs the ProPresenter 21 writer bug: the central directory size
// includes the 98 bytes of end records after it.
func fixCentralDirectorySize(data []byte) ([]byte, string, bool) {
	eocd64 := bytes.LastIndex(data, []byte("PK\x06\x06"))
	eocd := bytes.LastIndex(data, []byte("PK\x05\x06"))
	if eocd64 < 0 || eocd < 0 || eocd64+56 > len(data) || eocd+22 > len(data) {
		return nil, "", false
	}
	claimed := binary.LittleEndian.Uint64(data[eocd64+40:])
	offset := binary.LittleEndian.Uint64(data[eocd64+48:])
	real := uint64(eocd64) - offset
	if claimed == real {
		return nil, "", false
	}
	fixed := bytes.Clone(data)
	binary.LittleEndian.PutUint64(fixed[eocd64+40:], real)
	binary.LittleEndian.PutUint32(fixed[eocd+12:], uint32(real))
	return fixed, fmt.Sprintf("central directory size claims %d bytes, real size is %d (end records counted in); repaired in memory", claimed, real), true
}
