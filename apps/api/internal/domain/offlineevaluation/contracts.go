package domainofflineevaluation

import "slices"

const (
	ManifestVersion       = "recommendation-public-dataset-manifest-v1"
	ReportVersion         = "recommendation-offline-evaluation-v1"
	ReportKind            = "offline_recommendation_evaluation"
	SessionProfileV1      = "short-video-session-v1"
	KuaiRecSchemaV2       = "kuairec-v2"
	MicroLensCanonicalV1  = "microlens-canonical-v1"
	LicenseOperatorReview = "operator_reviewed"
	ExternalModelCalls    = 0

	MinK = 1
	MaxK = 100
)

type Track string

const (
	TrackPublicDataset Track = "public_dataset"
	TrackReplay        Track = "production_replay"
	TrackGolden        Track = "human_golden"
)

func ValidTrack(value Track) bool {
	return value == TrackPublicDataset || value == TrackReplay || value == TrackGolden
}

type DatasetKind string

const (
	DatasetKuaiRec   DatasetKind = "kuairec"
	DatasetMicroLens DatasetKind = "microlens"
)

func ValidDatasetKind(value DatasetKind) bool {
	return value == DatasetKuaiRec || value == DatasetMicroLens
}

type Baseline string

const (
	BaselinePopularity        Baseline = "popularity"
	BaselineRecent            Baseline = "recent_interaction"
	BaselineCategory          Baseline = "category"
	BaselineText              Baseline = "text"
	BaselineImage             Baseline = "image"
	BaselineMultimodal        Baseline = "multimodal"
	BaselineMultimodalSession Baseline = "multimodal_session"
)

func Baselines() []Baseline {
	return []Baseline{
		BaselinePopularity,
		BaselineRecent,
		BaselineCategory,
		BaselineText,
		BaselineImage,
		BaselineMultimodal,
		BaselineMultimodalSession,
	}
}

func ValidBaseline(value Baseline) bool {
	return slices.Contains(Baselines(), value)
}

type Availability string

const (
	AvailabilityAvailable   Availability = "available"
	AvailabilityUnavailable Availability = "unavailable"
)

type ExclusionCode string

const (
	ExclusionMissingWatchRatio   ExclusionCode = "missing_watch_ratio"
	ExclusionInsufficientHistory ExclusionCode = "insufficient_history"
	ExclusionMissingTarget       ExclusionCode = "missing_target"
	ExclusionPriorItem           ExclusionCode = "prior_item"
	ExclusionMissingCategory     ExclusionCode = "missing_category"
	ExclusionMissingFeature      ExclusionCode = "missing_feature"
	ExclusionIncompatibleVector  ExclusionCode = "incompatible_vector"
	ExclusionUnsupportedMetric   ExclusionCode = "unsupported_metric"
)

func ValidK(values []int) bool {
	if len(values) == 0 || len(values) > MaxK {
		return false
	}
	previous := 0
	for _, value := range values {
		if value < MinK || value > MaxK || value <= previous {
			return false
		}
		previous = value
	}
	return true
}
