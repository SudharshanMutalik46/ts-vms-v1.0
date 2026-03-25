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
	FPS         float64
	BitrateKbps int
	RTSPURL     string
}

type SelectionResult struct {
	MainToken     string
	MainSupported bool
	MainRTSP      string
	MainCodec     string

	SubToken        string
	SubSupported    bool
	SubIsSameAsMain bool
	SubRTSP         string
	SubCodec        string

	ReasonCode string
}

func codecRank(preferred []Codec, c Codec) int {
	for i, p := range preferred {
		if strings.EqualFold(string(p), string(c)) {
			return i
		}
	}
	return 999
}

func isSupportedFor(preferred []Codec, c Codec) bool {
	return codecRank(preferred, c) < 999
}

func SelectProfilesForCodecs(profiles []Profile, preferred []Codec) SelectionResult {
	if len(profiles) == 0 {
		return SelectionResult{ReasonCode: "missing_profiles"}
	}
	if len(preferred) == 0 {
		preferred = []Codec{CodecH264}
	}

	sort.SliceStable(profiles, func(i, j int) bool {
		p1, p2 := profiles[i], profiles[j]

		r1 := codecRank(preferred, p1.VideoCodec)
		r2 := codecRank(preferred, p2.VideoCodec)
		if r1 != r2 {
			return r1 < r2
		}

		a1 := p1.Width * p1.Height
		a2 := p2.Width * p2.Height
		if a1 != a2 {
			return a1 > a2
		}

		if p1.FPS != p2.FPS {
			return p1.FPS > p2.FPS
		}

		if p1.BitrateKbps != p2.BitrateKbps {
			return p1.BitrateKbps > p2.BitrateKbps
		}

		return p1.Token < p2.Token
	})

	main := profiles[0]

	// Pick a smaller substream, but stay in supported/preferred codecs if possible.
	sub := main
	for _, p := range profiles {
		if p.Token == main.Token {
			continue
		}
		if !isSupportedFor(preferred, p.VideoCodec) {
			continue
		}
		if p.Width*p.Height < main.Width*main.Height {
			sub = p
			break
		}
	}

	return SelectionResult{
		MainToken:     main.Token,
		MainSupported: isSupportedFor(preferred, main.VideoCodec),
		MainRTSP:      main.RTSPURL,
		MainCodec:     string(main.VideoCodec),

		SubToken:        sub.Token,
		SubSupported:    isSupportedFor(preferred, sub.VideoCodec),
		SubIsSameAsMain: sub.Token == main.Token,
		SubRTSP:         sub.RTSPURL,
		SubCodec:        string(sub.VideoCodec),
	}
}
