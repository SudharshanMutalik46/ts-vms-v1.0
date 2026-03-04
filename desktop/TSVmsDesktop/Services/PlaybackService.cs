using System.Collections.Generic;
using System.Linq;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class PlaybackService
    {
        public List<RecordingSegment> Normalize(IEnumerable<RecordingSegment>? segments)
        {
            return segments?
                .Where(s => !string.IsNullOrWhiteSpace(s.Path))
                .OrderByDescending(s => s.StartTs)
                .ToList()
                ?? new List<RecordingSegment>();
        }
    }
}
