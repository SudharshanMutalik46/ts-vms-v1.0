using System;
using System.Collections.Generic;

namespace TSVmsDesktop.Models
{
    public sealed class PlaybackSessionModel
    {
        public string CameraId { get; set; } = string.Empty;
        public DateTime WindowStartUtc { get; set; }
        public DateTime WindowEndUtc { get; set; }
        public double TotalWindowSeconds { get; set; }

        public List<PlaybackSessionSegment> Segments { get; set; } = new();
        public List<PlaybackTimelineBlock> TimelineBlocks { get; set; } = new();
    }

    public sealed class PlaybackSessionSegment
    {
        public RecordingSegment Segment { get; set; } = new();
        public double WindowOffsetSeconds { get; set; }
    }

    public sealed class PlaybackTimelineBlock
    {
        public double StartOffsetSeconds { get; set; }
        public double EndOffsetSeconds { get; set; }
        public string Label { get; set; } = string.Empty;
        public bool HasGapBefore { get; set; }
    }

    public sealed class PlaybackSeekResult
    {
        public int SegmentIndex { get; set; }
        public PlaybackSessionSegment Segment { get; set; } = new();
        public double LocalOffsetSeconds { get; set; }
        public bool LandedAfterGap { get; set; }
    }
}
