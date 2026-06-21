package release

import (
	"reflect"
	"testing"
)

func TestDetectLanguages(t *testing.T) {
	cases := []struct {
		name string
		want []string
	}{
		{"Avatar.2009.Collectors.Extended.Cut.Eng.Fre.Ger.Spa.2160p.BluRay.Remux-SGF", []string{"EN", "FR", "DE", "ES"}},
		{"Avengers.Endgame.2019.German.EAC3.DL.2160p.UHD.BluRay.HDR.HEVC.Remux-NIMA4K", []string{"DE", "EN"}},
		{"Anaconda.2025.2160p.UHD.BluRay.REMUX.DV.HDR10.MULTi-Ben.The.Men", []string{"MULTI"}},
		{"The.Matrix.1999.1080p.BluRay.x264-GRP", nil},
	}
	for _, c := range cases {
		if got := DetectLanguages(c.name); !reflect.DeepEqual(got, c.want) {
			t.Errorf("DetectLanguages(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
