package multimodalprofile

import (
	"errors"
	"strings"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

const (
	TongyiFlashSnapshotProfile = "tongyi-embedding-vision-flash-2026-03-06"
	TongyiFlashStableProfile   = "tongyi-embedding-vision-flash"
	DefaultProfile             = TongyiFlashSnapshotProfile

	TongyiProviderAlias = "alibaba-bailian"
	TongyiModelAlias    = "tongyi-embedding-vision-flash"
	TongyiDimension     = 768
)

type VideoFusionMode string

const (
	VideoFusionNative          VideoFusionMode = "native"
	VideoFusionIndependentMean VideoFusionMode = "independent-mean"
)

var ErrUnsupportedProfile = errors.New("unsupported multimodal profile")

type Profile struct {
	ID               string
	UpstreamModel    string
	Dimension        int
	ResolutionLevel  int
	IncludeDimension bool
	MaxImages        int
	VideoFusion      VideoFusionMode
	Contract         domainembedding.MultimodalContractIdentity
}

func Resolve(value string) (Profile, error) {
	profileID := strings.ToLower(strings.TrimSpace(value))
	if profileID == "" {
		profileID = DefaultProfile
	}
	switch profileID {
	case TongyiFlashSnapshotProfile:
		return newProfile(
			profileID,
			"2026-03-06-res1",
			domainembedding.MultimodalFusionPolicyV1,
			VideoFusionNative,
			true,
			1,
			16,
		)
	case TongyiFlashStableProfile:
		return newProfile(
			profileID,
			"stable-independent-mean-v1",
			domainembedding.MultimodalNormalizedMeanFusionV1,
			VideoFusionIndependentMean,
			false,
			0,
			8,
		)
	default:
		return Profile{}, ErrUnsupportedProfile
	}
}

func newProfile(
	profileID string,
	revisionAlias string,
	fusionPolicy string,
	videoFusion VideoFusionMode,
	includeDimension bool,
	resolutionLevel int,
	maxImages int,
) (Profile, error) {
	contract, err := domainembedding.NewMultimodalContractIdentity(
		TongyiProviderAlias,
		TongyiModelAlias,
		revisionAlias,
		TongyiDimension,
		domainembedding.MultimodalTextCanonicalizerV1,
		domainembedding.MultimodalFrameSamplingPolicyV1,
		domainembedding.MultimodalImagePreprocessingV1,
		fusionPolicy,
	)
	if err != nil {
		return Profile{}, err
	}
	return Profile{
		ID: profileID, UpstreamModel: profileID, Dimension: TongyiDimension,
		ResolutionLevel: resolutionLevel, IncludeDimension: includeDimension,
		MaxImages: maxImages, VideoFusion: videoFusion, Contract: contract,
	}, nil
}
