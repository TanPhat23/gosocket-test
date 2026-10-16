package runner

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"text/template"
)

// ErrTemplateIntegrity is returned when the Dockerfile template bytes do not hash to the pinned value.
var ErrTemplateIntegrity = errors.New("dockerfile template failed integrity check")

//go:embed templates/Dockerfile.project.template
var embeddedTemplate []byte

//go:embed templates/atm-entrypoint.sh
var embeddedEntrypoint []byte

// ExpectedTemplateSHA256 is the pinned SHA-256 of templates/Dockerfile.project.template.
// Update it deliberately, in the same reviewed change as any template edit; the golden test
// in image_template_test.go pins the same value.
const ExpectedTemplateSHA256 = "40928be57cb1772c6a1b9a1037292a18db8bc2cae00864d00d9d657e90021f59"

// ExpectedEntrypointSHA256 is the pinned SHA-256 of templates/atm-entrypoint.sh, which is copied into
// the image build context. Update it in the same reviewed change as any edit to the script.
const ExpectedEntrypointSHA256 = "87f8f33f44f76cfe1e1087d4671149a2e80d060309b661635e72c008c9dae4fe"

// loadVerifiedTemplate returns raw only if it hashes to expectedSHA. It never parses raw.
func loadVerifiedTemplate(raw []byte, expectedSHA string) ([]byte, error) {
	if !sha256HexRe.MatchString(expectedSHA) {
		return nil, ErrTemplateIntegrity
	}
	sum := sha256.Sum256(raw)
	got := hex.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(got), []byte(expectedSHA)) != 1 {
		return nil, ErrTemplateIntegrity
	}
	return raw, nil
}

// requireSHA256 is a template function that fails closed unless s is 64 lowercase hex characters.
func requireSHA256(s string) (string, error) {
	if !sha256HexRe.MatchString(s) {
		return "", errors.New("toolchain checksum is not a valid sha256")
	}
	return s, nil
}

// tarFlag maps an archive format to its tar decompression flag.
func tarFlag(format string) (string, error) {
	switch format {
	case "tar.gz":
		return "z", nil
	case "tar.xz":
		return "J", nil
	}
	return "", errors.New("unsupported archive format")
}

// requireInstallLayout is a template function that fails closed unless s is a known install layout.
func requireInstallLayout(s string) (string, error) {
	if !isKnownInstallLayout(s) {
		return "", errors.New("unsupported install layout")
	}
	return s, nil
}

// newDockerfileTemplate parses verified template bytes with strict missing-key handling.
func newDockerfileTemplate(verified []byte) (*template.Template, error) {
	return template.New("dockerfile").
		Option("missingkey=error").
		Funcs(template.FuncMap{"requireSHA256": requireSHA256, "tarFlag": tarFlag, "requireInstallLayout": requireInstallLayout, "requirePinnedRef": requirePinnedRef}).
		Parse(string(verified))
}

// executeTemplate parses and executes already-verified template bytes.
func executeTemplate(verified []byte, data TemplateData) ([]byte, error) {
	tmpl, err := newDockerfileTemplate(verified)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}
	return out.Bytes(), nil
}
