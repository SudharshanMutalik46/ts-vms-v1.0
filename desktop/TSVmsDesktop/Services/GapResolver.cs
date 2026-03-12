using System;
using System.Collections.Generic;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public sealed class GapResolver
    {
        public PlaybackSeekResult? Resolve(
            IReadOnlyList<PlaybackSessionSegment> segments,
            DateTime targetUtc)
        {
            // GUARD — add this if not already present
            if (segments == null || segments.Count == 0) return null;

            // Find the segment whose window contains targetUtc
            for (int i = 0; i < segments.Count; i++)
            {
                var seg = segments[i];
                if (targetUtc <= seg.Segment.EndTs)
                {
                    double localOffset = Math.Max(0,
                        (targetUtc - seg.Segment.StartTs).TotalSeconds);
                    bool landedAfterGap = i > 0 &&
                        targetUtc < seg.Segment.StartTs;
                    return new PlaybackSeekResult
                    {
                        SegmentIndex       = i,
                        Segment            = seg,
                        LocalOffsetSeconds = localOffset,
                        LandedAfterGap     = landedAfterGap
                    };
                }
            }
            // targetUtc is past the last segment — return last segment at its end
            var last = segments[^1];
            return new PlaybackSeekResult
            {
                SegmentIndex       = segments.Count - 1,
                Segment            = last,
                LocalOffsetSeconds = Math.Max(0, last.Segment.DurationSeconds - 0.25),
                LandedAfterGap     = false
            };
        }
    }
}
