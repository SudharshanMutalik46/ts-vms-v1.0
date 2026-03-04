using System;
using System.Collections.Generic;
using System.Linq;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class PlaybackManifestService
    {
        public sealed class PlaybackManifest
        {
            public IReadOnlyList<RecordingSegment> Segments { get; init; } = Array.Empty<RecordingSegment>();
            public IReadOnlyList<PlaybackTimelineBlock> TimelineBlocks { get; init; } = Array.Empty<PlaybackTimelineBlock>();
            public double TotalDurationSeconds { get; init; }
        }

        public sealed class ResolvedPosition
        {
            public int SegmentIndex { get; init; }
            public RecordingSegment Segment { get; init; } = new();
            public double LocalOffsetSeconds { get; init; }
        }

        public PlaybackManifest Build(IReadOnlyList<RecordingSegment> rawSegments)
        {
            var segments = rawSegments
                .Where(s => !string.IsNullOrWhiteSpace(s.Path))
                .OrderBy(s => s.StartTs)
                .ToList();

            var blocks = new List<PlaybackTimelineBlock>();
            double cursor = 0;
            DateTime? previousEnd = null;

            foreach (var segment in segments)
            {
                bool hasGap = previousEnd.HasValue && segment.StartTs > previousEnd.Value.AddSeconds(1);
                double duration = Math.Max(0.1, segment.DurationSeconds);

                blocks.Add(new PlaybackTimelineBlock
                {
                    StartOffsetSeconds = cursor,
                    EndOffsetSeconds = cursor + duration,
                    Label = segment.StartTs.ToLocalTime().ToString("HH:mm:ss"),
                    HasGapBefore = hasGap
                });

                cursor += duration;
                previousEnd = segment.EndTs;
            }

            return new PlaybackManifest
            {
                Segments = segments,
                TimelineBlocks = blocks,
                TotalDurationSeconds = cursor
            };
        }

        public ResolvedPosition? Resolve(IReadOnlyList<RecordingSegment> segments, double globalOffsetSeconds)
        {
            if (segments.Count == 0)
                return null;

            double remaining = Math.Max(0, globalOffsetSeconds);
            for (int i = 0; i < segments.Count; i++)
            {
                double duration = Math.Max(0.1, segments[i].DurationSeconds);
                if (remaining <= duration || i == segments.Count - 1)
                {
                    return new ResolvedPosition
                    {
                        SegmentIndex = i,
                        Segment = segments[i],
                        LocalOffsetSeconds = Math.Min(remaining, duration)
                    };
                }
                remaining -= duration;
            }

            return null;
        }

        public double GetGlobalOffset(IReadOnlyList<RecordingSegment> segments, int currentSegmentIndex, double localOffsetSeconds)
        {
            double total = 0;
            for (int i = 0; i < currentSegmentIndex && i < segments.Count; i++)
                total += Math.Max(0.1, segments[i].DurationSeconds);
            return total + Math.Max(0, localOffsetSeconds);
        }
    }
}
