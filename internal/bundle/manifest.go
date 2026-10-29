package bundle

import (
	"crypto/sha1"
	"fmt"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Manifest is .procore/legacy-export-manifest.json. Field order follows the ProPresenter export.
type Manifest struct {
	Version               int                `json:"version"`
	TargetContract        string             `json:"target_contract"`
	SourceRoot            string             `json:"source_root"`
	TargetRoot            string             `json:"target_root"`
	Documents             []ManifestDocument `json:"documents"`
	Media                 []ManifestMedia    `json:"media"`
	MediaReferences       []ManifestMediaRef `json:"media_references"`
	CompatibilityWarnings []string           `json:"compatibility_warnings"`
	ElapsedMillis         int                `json:"elapsed_millis"`
}

type ManifestDocument struct {
	DocumentID string           `json:"document_id"`
	Location   ManifestLocation `json:"location"`
	OutputPath string           `json:"output_path"`
	ByteCount  int              `json:"byte_count"`
	Blake3     string           `json:"blake3"`
	Reopened   bool             `json:"reopened"`
}

type ManifestLocation struct {
	Type     string `json:"type"`
	Kind     string `json:"kind"`
	CorePath string `json:"core_path"`
}

type ManifestMedia struct {
	OutputPath string `json:"output_path"`
	ByteCount  int    `json:"byte_count"`
	Blake3     string `json:"blake3"`
}

type ManifestMediaRef struct {
	DocumentID       string `json:"document_id"`
	Scope            string `json:"scope"`
	OccurrencePath   string `json:"occurrence_path"`
	ReferenceID      string `json:"reference_id"`
	AssetID          string `json:"asset_id"`
	SourcePath       string `json:"source_path"`
	OutputPath       string `json:"output_path"`
	LocatorRewritten bool   `json:"locator_rewritten"`
}

// NormalizeName compares member names without caring about NFC or NFD.
func NormalizeName(name string) string { return norm.NFC.String(name) }

// FileTitle makes a song title safe as a file name. macOS stores names in NFD, and so does ProPresenter.
func FileTitle(title string) string {
	t := strings.NewReplacer("/", "-", ":", "-").Replace(strings.TrimSpace(title))
	return norm.NFD.String(t)
}

// uuidNamespace scopes the deterministic UUIDs of exported objects.
var uuidNamespace = [16]byte{0x6b, 0x2f, 0x1c, 0x52, 0x8e, 0x1d, 0x4b, 0x7a, 0x9a, 0x3e, 0x51, 0x0c, 0x77, 0xd2, 0x41, 0x90}

// DerivedUUID returns a version 5 UUID for key. Exporting the same song twice gives the same file.
func DerivedUUID(key string) string {
	h := sha1.New()
	h.Write(uuidNamespace[:])
	h.Write([]byte(key))
	b := h.Sum(nil)[:16]
	b[6] = b[6]&0x0f | 0x50
	b[8] = b[8]&0x3f | 0x80
	return strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
}
