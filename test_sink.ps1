$ErrorActionPreference = "Stop"
Write-Host "Testing hlssink2 TS mode..."
try {
    gst-launch-1.0 videotestsrc num-buffers=50 ! x264enc ! hlssink2 location=test_ts_%05d.ts target-duration=1
    Write-Host "TS Mode Success"
}
catch {
    Write-Host "TS Mode Failed"
}

Write-Host "Testing hlssink2 MP4 mode..."
try {
    # Is there a forced fMP4 mode? usually extension triggers it in some versions, or property?
    # hlssink2 doesn't have 'playlist-type' for format, but it checks caps?
    # If I feed it 'video/mp4' it might use mp4mux?
    # No, hlssink2 normally muxes itself using a child element.
    gst-launch-1.0 videotestsrc num-buffers=50 ! x264enc ! hlssink2 location=test_mp4_%05d.mp4 target-duration=1
    Write-Host "MP4 Mode Success"
}
catch {
    Write-Host "MP4 Mode Failed"
}
