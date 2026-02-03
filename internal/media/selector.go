package media

import (
	"sort"
	"strings"
)

type Codec string

const (
	CodecH264    Codec = "H264"
	CodecH265    Codec = "H265"
	CodecMJPEG   Codec = "MJPEG"
	CodecUnknown Codec = "UNKNOWN"
)

type Profile struct {
	Token       string
	Name        string
	VideoCodec  Codec
	Width       int
	Height      int
	FPS         float64 // 0 if missing
	BitrateKbps int     // 0 if missing
	RTSPURL     string  // Raw/Sanitized? Input should arguably be raw, output selection uses it.
}

type SelectionResult struct {
	MainToken     string
	MainSupported bool
	MainRTSP      string

	SubToken        string
	SubSupported    bool
	SubIsSameAsMain bool
	SubRTSP         string

	ReasonCode string
}

// Configurable priorities
var SupportedCodecs = []Codec{CodecH264} // Default

func IsSupported(c Codec) bool {
	for _, sc := range SupportedCodecs {
		if strings.EqualFold(string(c), string(sc)) {
			return true
		}
	}
	return false
}

// SelectProfiles Deterministic Selection Logic
// Determinism: Supported > Res > FPS > Bitrate > Token
func SelectProfiles(profiles []Profile) SelectionResult {
	if len(profiles) == 0 {
		return SelectionResult{ReasonCode: "missing_profiles"}
	}

	// 1. Sort Profiles Deterministically (Best First)
	sort.SliceStable(profiles, func(i, j int) bool {
		p1, p2 := profiles[i], profiles[j]

		// A. Supported Codec
		s1 := IsSupported(p1.VideoCodec)
		s2 := IsSupported(p2.VideoCodec)
		if s1 != s2 {
			return s1 // True (supported) comes first
		}

		// B. Resolution (WxH)
		res1 := p1.Width * p1.Height
		res2 := p2.Width * p2.Height
		if res1 != res2 {
			return res1 > res2 // Higher res first
		}

		// C. FPS
		if p1.FPS != p2.FPS {
			return p1.FPS > p2.FPS
		}

		// D. Bitrate
		if p1.BitrateKbps != p2.BitrateKbps {
			return p1.BitrateKbps > p2.BitrateKbps
		}

		// E. Token Lexical (Tie-Breaker, Ascending for stability)
		return p1.Token < p2.Token
	})

	// Main is highest score
	main := profiles[0]

	res := SelectionResult{
		MainToken:     main.Token,
		MainSupported: IsSupported(main.VideoCodec),
		MainRTSP:      main.RTSPURL,
	}
	if !res.MainSupported {
		res.ReasonCode = "unsupported_codec"
	}

	// Sub Selection
	// Filter for Sub candidates
	// Rule: Prefer distinct, supported, lower res (Target <= 640x360 or similar class)
	// If no distinct sub, fallback to Main.

	// Create candidates list for sub (exclude Main unless forced)
	// Sort for Sub: Prefer Supported, then "Best Fit Low Res", then standard quality
	// Actually "Best Fit Low Res" means closest to target without going over?
	// Or just smallest available?
	// Req: "Prefer <= 640x360 or <= 704x576 class if available. Otherwise choose smallest supported."

	sub := selectSubProfile(profiles, main)

	res.SubToken = sub.Token
	res.SubSupported = IsSupported(sub.VideoCodec)
	res.SubRTSP = sub.RTSPURL
	res.SubIsSameAsMain = (sub.Token == main.Token)

	return res
}

func selectSubProfile(profiles []Profile, main Profile) Profile {
	// Filter candidates:
	// 1. IsSupported (Strong preference)
	// 2. Distinct from Main (Preference)
	// 3. Resolution tiers

	var candidates []Profile
	for _, p := range profiles {
		candidates = append(candidates, p)
	}

	// Sort candidates for SUB preference:
	// 1. Supported Codec
	// 2. Distinctness (p != main) ?? Maybe hard constraint or weight?
	//    Let's prioritize: Supported > Distinct > Resolution Match (Target) > Smallest Res

	sort.SliceStable(candidates, func(i, j int) bool {
		p1, p2 := candidates[i], candidates[j]

		// A. Supported
		s1 := IsSupported(p1.VideoCodec)
		s2 := IsSupported(p2.VideoCodec)
		if s1 != s2 {
			return s1
		}

		// B. Distinctness (Prefer distinct from Main)
		d1 := (p1.Token != main.Token)
		d2 := (p2.Token != main.Token)
		if d1 != d2 {
			return d1 // True (distinct) first
		}

		// C. Target Class logic
		// We want "Smallest resolution that is reasonably usable" or "Closest to 360p"?
		// User rule: "Prefer <= 640x360 ... Otherwise choose smallest supported."
		// Let's implement: "Smallest available" as the driver, but prefer supported.
		// Actually, if we just sort by Resolution ASCENDING, we get smallest.

		res1 := p1.Width * p1.Height
		res2 := p2.Width * p2.Height

		// Which is better? Usually 640x360 for grid. 100x100 is too small.
		// Detailed logic:
		// Priority Class 1: <= 640x360 AND >= 320x240 (Ideal Grid)
		// Priority Class 2: <= 704x576 (SD)
		// Priority Class 3: Anything else (Higher or Tiny)

		// Simplifying for standard VMS:
		// Just sort by ABS(res - target_res) to find closest to 640x360?
		// Or strictly follow user prompt "Prefer <= ... otherwise smallest".
		// Interpreted:
		// 1. If in <= 640x360 class: Prefer Largest within this class (quality maximization within constraints) or Smallest (bandwidth min)?
		// User said "Sub stream selection (lower resolution for grid view)".
		// Let's prefer "Closest to 640x360".

		// Let's implement simpler sort:
		// Just ascending resolution. Smallest is usually 320x240 or 640x360.
		// If main is 4K, and we have 1080p and 720p.
		// Ascending -> 720p picked. Good.
		// If main is 720p, and we have 720p.
		// Ascending -> 720p.
		// Distinctness check handled earlier.

		if res1 != res2 {
			return res1 < res2 // Ascending Resolution (Smaller first)
		}

		return p1.Token < p2.Token
	})

	return candidates[0]
}
