package media

import (
	"testing"
)

func TestSanitizeRTSPURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"rtsp://user:pass@192.168.1.1/stream", "rtsp://192.168.1.1/stream"},
		{"rtsp://192.168.1.1/stream", "rtsp://192.168.1.1/stream"},
		{"http://user:pass@domain.com/feed", "http://domain.com/feed"},
		{"", ""},
	}

	for _, tc := range tests {
		got := SanitizeRTSPURL(tc.input)
		if got != tc.expected {
			t.Errorf("SanitizeRTSPURL(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}

func TestSelectProfiles_Determinism(t *testing.T) {
	// Create a mixed bag of profiles
	// Main Candidates: H264/H265, High Res
	// Sub Candidates: H264, Low Res

	p1 := Profile{Token: "t1", Name: "Main1", VideoCodec: CodecH264, Width: 1920, Height: 1080, RTSPURL: "u1"}
	p2 := Profile{Token: "t2", Name: "Sub1", VideoCodec: CodecH264, Width: 640, Height: 360, RTSPURL: "u2"}    // Perfect Sub
	p3 := Profile{Token: "t3", Name: "Main2", VideoCodec: CodecH265, Width: 2560, Height: 1440, RTSPURL: "u3"} // Better Main?
	p4 := Profile{Token: "t4", Name: "BadSub", VideoCodec: CodecMJPEG, Width: 320, Height: 240, RTSPURL: "u4"}

	// Shuffle input order?
	// The selector sorts internally, so order shouldn't matter.
	input := []Profile{p4, p1, p3, p2}

	res := SelectProfiles(input)

	// Expectation:
	// Main: p1 (H264 1080p) or p3 (H265 1440p). Logic says H264 preferred if both present?
	// Selector logic: Sort by Codec Pref (H264 > H265), then Resolution (Higher better closer to target?).
	// Code says: Codec Rank (H264=1, H265=2). So H264 wins?
	// Let's check selector.go logic.
	// But assuming H264 is rank 0 or 1.

	if res.MainToken == "" {
		t.Fatal("Main profile not selected")
	}
	if res.SubToken == "" {
		t.Fatal("Sub profile not selected")
	}

	// Stability Check
	input2 := []Profile{p2, p3, p4, p1} // Different order
	res2 := SelectProfiles(input2)

	if res.MainToken != res2.MainToken {
		t.Errorf("Indeterministic Main: %v vs %v", res.MainToken, res2.MainToken)
	}
	if res.SubToken != res2.SubToken {
		t.Errorf("Indeterministic Sub: %v vs %v", res.SubToken, res2.SubToken)
	}

	// Single Profile Case
	single := []Profile{p1}
	resSingle := SelectProfiles(single)
	if resSingle.MainToken != p1.Token {
		t.Errorf("Single profile should be main")
	}
	if !resSingle.SubIsSameAsMain {
		t.Errorf("Single profile should mean sub == main")
	}
}

func TestDeduplication(t *testing.T) {
	// If main and sub are same
	p1 := Profile{Token: "t1", Name: "OnlyOne", VideoCodec: CodecH264, Width: 1920, Height: 1080, RTSPURL: "u1"}
	res := SelectProfiles([]Profile{p1})

	if res.SubToken != res.MainToken {
		t.Errorf("Expected sub=main")
	}
	if !res.SubIsSameAsMain {
		t.Error("Flag SubIsSameAsMain should be true")
	}
}
