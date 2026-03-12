using System;
using System.Collections.Generic;
using System.Linq;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public sealed class PlaybackTimelineBuilder
    {
        public PlaybackSessionModel Build(string cameraId, DateTime fromUtc, DateTime toUtc, IReadOnlyList<RecordingSegment> rawSegments)
        {
            var segments = Normalize(rawSegments)
                .Where(s => s.EndTs > fromUtc && s.StartTs < toUtc && s.IsFinalized)
                .Select(s => new PlaybackSessionSegment
                {
                    Segment = s,
                    WindowOffsetSeconds = Math.Max(0, (s.StartTs - fromUtc).TotalSeconds)
                })
                .ToList();

            var blocks = new List<PlaybackTimelineBlock>(segments.Count);
            for (int i = 0; i < segments.Count; i++)
            {
                var item = segments[i];
                var clippedStart = item.Segment.StartTs < fromUtc ? fromUtc : item.Segment.StartTs;
                var clippedEnd = item.Segment.EndTs > toUtc ? toUtc : item.Segment.EndTs;

                blocks.Add(new PlaybackTimelineBlock
                {
                    StartOffsetSeconds = Math.Max(0, (clippedStart - fromUtc).TotalSeconds),
                    EndOffsetSeconds = Math.Max(0, (clippedEnd - fromUtc).TotalSeconds),
                    Label = item.Segment.StartTs.ToLocalTime().ToString("HH:mm:ss"),
                    HasGapBefore = i > 0 && item.Segment.StartTs > segments[i - 1].Segment.EndTs.AddSeconds(1)
                });
            }

            return new PlaybackSessionModel
            {
                CameraId = cameraId,
                WindowStartUtc = fromUtc,
                WindowEndUtc = toUtc,
                Segments = segments,
                TimelineBlocks = blocks,
                TotalWindowSeconds = Math.Max(1, (toUtc - fromUtc).TotalSeconds)
            };
        }

        private static IReadOnlyList<RecordingSegment> Normalize(IReadOnlyList<RecordingSegment> source)
        {
            var ordered = source
                .Where(s => s != null && !string.IsNullOrWhiteSpace(s.Path) && s.EndTs > s.StartTs)
                .OrderBy(s => s.StartTs)
                .ThenBy(s => s.EndTs)
                .ThenBy(s => s.Path, StringComparer.OrdinalIgnoreCase)
                .ToList();

            var result = new List<RecordingSegment>(ordered.Count);
            var seenPaths = new HashSet<string>(StringComparer.OrdinalIgnoreCase);

            foreach (var segment in ordered)
            {
                if (!seenPaths.Add(segment.Path))
                    continue;

                if (result.Count > 0)
                {
                    var previous = result[^1];
                    if (segment.StartTs <= previous.StartTs.AddMilliseconds(100) &&
                        segment.EndTs <= previous.EndTs.AddMilliseconds(100))
                    {
                        continue;
                    }
                }

                result.Add(segment);
            }

            return result;
        }
    }
}
