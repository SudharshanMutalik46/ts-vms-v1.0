using System;

namespace TSVmsDesktop.Models
{
    public abstract class TimelineItemBase
    {
        public double LeftPx { get; set; }
        public double WidthPx { get; set; }
        public DateTime StartUtc { get; set; }
        public DateTime EndUtc { get; set; }
        public string Label { get; set; } = "";
    }

    public sealed class TimelineSegmentItem : TimelineItemBase
    {
        public RecordingSegment Segment { get; set; } = new();
        public bool IsSelected { get; set; }
        public bool IsProtected => Segment.IsProtected;
    }

    public sealed class TimelineGapItem : TimelineItemBase
    {
    }
}
