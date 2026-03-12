using System;
using System.Collections.Generic;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public sealed class PlaybackManifestService
    {
        private readonly PlaybackTimelineBuilder _timelineBuilder;
        private readonly GapResolver _gapResolver;

        public PlaybackManifestService(PlaybackTimelineBuilder timelineBuilder, GapResolver gapResolver)
        {
            _timelineBuilder = timelineBuilder;
            _gapResolver = gapResolver;
        }

        public PlaybackSessionModel Build(string cameraId, DateTime fromUtc, DateTime toUtc, IReadOnlyList<RecordingSegment> rawSegments)
            => _timelineBuilder.Build(cameraId, fromUtc, toUtc, rawSegments);

        public PlaybackSeekResult? Resolve(PlaybackSessionModel session, double windowSeconds)
        {
            if (session == null)
                return null;

            var clamped = Math.Max(0, Math.Min(session.TotalWindowSeconds, windowSeconds));
            var targetUtc = session.WindowStartUtc.AddSeconds(clamped);
            return _gapResolver.Resolve(session.Segments, targetUtc);
        }
    }
}
