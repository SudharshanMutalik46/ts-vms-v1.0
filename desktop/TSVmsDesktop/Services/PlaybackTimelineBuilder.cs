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
            var normalizedFromUtc = ToUtcInstant(fromUtc);
            var normalizedToUtc = ToUtcInstant(toUtc);

            var segments = Normalize(rawSegments)
                .Select(NormalizeSegment)
                .Where(s => ToUtcInstant(s.EndTs) > normalizedFromUtc &&
                            ToUtcInstant(s.StartTs) < normalizedToUtc &&
                            (s.IsFinalized || s.HealthState == "finalized"))
                .Select(s => new PlaybackSessionSegment
                {
                    Segment = s,
                    WindowOffsetSeconds = Math.Max(0, (ToUtcInstant(s.StartTs) - normalizedFromUtc).TotalSeconds)
                })
                .ToList();

            var blocks = new List<PlaybackTimelineBlock>(segments.Count);
            for (int i = 0; i < segments.Count; i++)
            {
                var item = segments[i];
                var segmentStartUtc = ToUtcInstant(item.Segment.StartTs);
                var segmentEndUtc = ToUtcInstant(item.Segment.EndTs);
                var clippedStart = segmentStartUtc < normalizedFromUtc ? normalizedFromUtc : segmentStartUtc;
                var clippedEnd = segmentEndUtc > normalizedToUtc ? normalizedToUtc : segmentEndUtc;

                blocks.Add(new PlaybackTimelineBlock
                {
                    StartOffsetSeconds = Math.Max(0, (clippedStart - normalizedFromUtc).TotalSeconds),
                    EndOffsetSeconds = Math.Max(0, (clippedEnd - normalizedFromUtc).TotalSeconds),
                    Label = segmentStartUtc.ToLocalTime().ToString("HH:mm:ss"),
                    HasGapBefore = i > 0 && segmentStartUtc > ToUtcInstant(segments[i - 1].Segment.EndTs).AddSeconds(1)
                });
            }

            return new PlaybackSessionModel
            {
                CameraId = cameraId,
                WindowStartUtc = normalizedFromUtc,
                WindowEndUtc = normalizedToUtc,
                Segments = segments,
                TimelineBlocks = blocks,
                TotalWindowSeconds = Math.Max(1, (normalizedToUtc - normalizedFromUtc).TotalSeconds)
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

        private static RecordingSegment NormalizeSegment(RecordingSegment source)
        {
            return new RecordingSegment
            {
                Id = source.Id,
                CameraId = source.CameraId,
                StartTs = ToUtcInstant(source.StartTs),
                EndTs = ToUtcInstant(source.EndTs),
                DurationMs = source.DurationMs,
                Path = source.Path,
                SizeBytes = source.SizeBytes,
                IsProtected = source.IsProtected,
                Container = source.Container,
                ChecksumSha256 = source.ChecksumSha256,
                HealthState = source.HealthState,
                IsFinalized = source.IsFinalized
            };
        }

        private static DateTime ToUtcInstant(DateTime value)
        {
            return value.Kind switch
            {
                DateTimeKind.Utc => value,
                DateTimeKind.Local => value.ToUniversalTime(),
                _ => DateTime.SpecifyKind(value, DateTimeKind.Local).ToUniversalTime()
            };
        }
    }
}
