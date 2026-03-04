namespace TSVmsDesktop.Models
{
    public class PlaybackTimelineBlock
    {
        public double StartOffsetSeconds { get; set; }
        public double EndOffsetSeconds { get; set; }
        public string Label { get; set; } = string.Empty;
        public bool HasGapBefore { get; set; }
    }
}
