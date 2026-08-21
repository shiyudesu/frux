package infraofflineevaluation

import (
	"crypto/sha256"
	"encoding/hex"

	applicationofflineevaluation "github.com/shiyudesu/frux/internal/application/offlineevaluation"
)

type LoadedGoldenBundle struct {
	SHA256 string
	Bundle applicationofflineevaluation.GoldenBundle
}

func LoadGoldenBundle(path string) (*LoadedGoldenBundle, error) {
	content, err := readBoundedJSONFile(path, 32<<20)
	if err != nil {
		return nil, err
	}
	var bundle applicationofflineevaluation.GoldenBundle
	if err := decodeStrictJSON(content, &bundle); err != nil {
		return nil, &InputError{Code: FailureSchema, Role: "golden"}
	}
	hash := sha256.Sum256(content)
	return &LoadedGoldenBundle{SHA256: hex.EncodeToString(hash[:]), Bundle: bundle}, nil
}
