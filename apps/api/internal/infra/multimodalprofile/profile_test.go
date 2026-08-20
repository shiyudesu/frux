package multimodalprofile

import (
	"errors"
	"testing"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

func TestResolveTongyiProfiles(t *testing.T) {
	snapshot, err := Resolve(TongyiFlashSnapshotProfile)
	if err != nil {
		t.Fatal(err)
	}
	stable, err := Resolve(TongyiFlashStableProfile)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.VideoFusion != VideoFusionNative || !snapshot.IncludeDimension ||
		snapshot.ResolutionLevel != 1 || snapshot.MaxImages != 16 ||
		snapshot.Contract.FusionPolicy != domainembedding.MultimodalFusionPolicyV1 {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	if stable.VideoFusion != VideoFusionIndependentMean || stable.IncludeDimension ||
		stable.ResolutionLevel != 0 || stable.MaxImages != 8 ||
		stable.Contract.FusionPolicy != domainembedding.MultimodalNormalizedMeanFusionV1 {
		t.Fatalf("stable=%#v", stable)
	}
	if snapshot.Contract.Equal(stable.Contract) ||
		snapshot.Contract.RevisionAlias == stable.Contract.RevisionAlias {
		t.Fatal("profile contracts were not isolated")
	}
}

func TestResolveTongyiProfileDefaultsAndRejectsUnknown(t *testing.T) {
	profile, err := Resolve("")
	if err != nil || profile.ID != DefaultProfile {
		t.Fatalf("profile=%#v err=%v", profile, err)
	}
	if _, err := Resolve("tongyi-embedding-vision-plus"); !errors.Is(err, ErrUnsupportedProfile) {
		t.Fatalf("error=%v", err)
	}
}
