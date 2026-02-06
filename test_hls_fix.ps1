Write-Host "Testing hlssink2 with byte-stream..."
gst-launch-1.0 videotestsrc num-buffers=100 ! videoconvert ! openh264enc ! h264parse ! "video/x-h264,stream-format=byte-stream" ! hlssink2 target-duration=2 location=test_byte_%05d.ts playlist-location=test_byte.m3u8
if ($LASTEXITCODE -eq 0) {
    Write-Host "hlssink2 worked!"
}
else {
    Write-Host "hlssink2 failed!"
}
