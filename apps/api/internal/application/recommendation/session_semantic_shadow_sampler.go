package applicationrecommendation

import (
	"crypto/sha256"
	"encoding/binary"
	"strconv"
	"strings"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

const SessionSemanticShadowSamplerV1 = "session-semantic-shadow-sampler-v1"

// ShouldSampleSessionSemanticShadow selects authenticated first-page Recommendation
// requests deterministically across retries and replicas.
func ShouldSampleSessionSemanticShadow(
	samplePPM int,
	userID int64,
	scene string,
	requestID string,
	firstPage bool,
) bool {
	scene = strings.ToLower(strings.TrimSpace(scene))
	requestID = strings.TrimSpace(requestID)
	if !firstPage || samplePPM <= 0 || userID <= 0 ||
		scene != domainrecommendation.RecommendationRequestLogScene ||
		requestID == "" || len(requestID) > domainrecommendation.MaxRequestIDLength {
		return false
	}
	if samplePPM >= domainrecommendation.MaxSamplingRatePPM {
		return true
	}

	hasher := sha256.New()
	writeShadowSampleField(hasher, SessionSemanticShadowSamplerV1)
	writeShadowSampleField(hasher, scene)
	writeShadowSampleField(hasher, strconv.FormatInt(userID, 10))
	writeShadowSampleField(hasher, requestID)
	sum := hasher.Sum(nil)
	bucket := binary.BigEndian.Uint64(sum[:8]) % uint64(domainrecommendation.MaxSamplingRatePPM)
	return int(bucket) < samplePPM
}

type shadowSampleWriter interface {
	Write([]byte) (int, error)
}

func writeShadowSampleField(writer shadowSampleWriter, value string) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write([]byte(value))
}
