using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.IO;
using System.Linq;
using System.Runtime.InteropServices;
using TSVmsDesktop.Interop;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class PlaybackEngineService : IDisposable
    {
        private IntPtr _engine = IntPtr.Zero;
        private IntPtr _hostHandle = IntPtr.Zero;
        private bool _initialized;
        private readonly object _sync = new();

        public void AttachHost(IntPtr hwnd)
        {
            lock (_sync)
            {
                _hostHandle = hwnd;
                EnsureCreated();
                if (_hostHandle != IntPtr.Zero)
                {
                    ThrowIfFailed(NativePlayback.tsplay_initialize(_engine, _hostHandle));
                    ThrowIfFailed(NativePlayback.tsplay_set_window_handle(_engine, _hostHandle));
                    _initialized = true;
                }
            }
        }

        public IntPtr HostHandle
        {
            get
            {
                lock (_sync)
                {
                    return _hostHandle;
                }
            }
        }

        public void SetHostSize(int width, int height)
        {
            lock (_sync)
            {
                EnsureReady();
                ThrowIfFailed(NativePlayback.tsplay_set_window_size(_engine, Math.Max(1, width), Math.Max(1, height)));
            }
        }

        public void RebindHost(int width, int height)
        {
            lock (_sync)
            {
                EnsureReady();
                if (_hostHandle == IntPtr.Zero)
                    return;

                ThrowIfFailed(NativePlayback.tsplay_set_window_handle(_engine, _hostHandle));
                if (width > 0 && height > 0)
                {
                    ThrowIfFailed(NativePlayback.tsplay_set_window_size(_engine, width, height));
                }
            }
        }

        public void Load(string mediaPath)
        {
            lock (_sync)
            {
                EnsureReady();
                if (string.IsNullOrWhiteSpace(mediaPath))
                    throw new ArgumentException("Media path is empty.", nameof(mediaPath));
                ThrowIfFailed(NativePlayback.tsplay_set_media_path(_engine, mediaPath));
            }
        }

        public void LoadSession(PlaybackSessionModel session, int startIndex)
        {
            if (session == null)
                throw new ArgumentNullException(nameof(session));

            var playlist = session.Segments
                .Select(s => s.Segment.Path)
                .Where(p => !string.IsNullOrWhiteSpace(p))
                .ToArray();

            lock (_sync)
            {
                EnsureReady();
                if (playlist.Length == 0)
                    throw new InvalidOperationException("Playback session contains no archive segments.");
                if (startIndex < 0 || startIndex >= playlist.Length)
                    startIndex = 0;
                ThrowIfFailed(NativePlayback.tsplay_set_playlist(_engine, playlist, playlist.Length, startIndex));
            }
        }

        public int GetPlaylistIndex()
        {
            lock (_sync)
            {
                return _engine == IntPtr.Zero ? -1 : NativePlayback.tsplay_get_playlist_index(_engine);
            }
        }

        public void Play() { lock (_sync) { EnsureReady(); ThrowIfFailed(NativePlayback.tsplay_play(_engine)); } }
        public void Pause() { lock (_sync) { EnsureReady(); ThrowIfFailed(NativePlayback.tsplay_pause(_engine)); } }
        public void Stop() { lock (_sync) { EnsureReady(); ThrowIfFailed(NativePlayback.tsplay_stop(_engine)); } }
        public void Seek(double seconds) { lock (_sync) { EnsureReady(); ThrowIfFailed(NativePlayback.tsplay_seek_seconds(_engine, seconds)); } }

        public void SetRate(double rate)
        {
            var clamped = Math.Clamp(rate, 0.25, 4.0);

            lock (_sync)
            {
                EnsureReady();
                ThrowIfFailed(NativePlayback.tsplay_set_rate(_engine, clamped));
            }
        }

        public double GetRate()
        {
            lock (_sync)
            {
                return _engine == IntPtr.Zero ? 1.0 : NativePlayback.tsplay_get_rate(_engine);
            }
        }

        public void SetLastSampleEnabled(bool enabled)
        {
            lock (_sync)
            {
                EnsureReady();
                NativePlayback.tsplay_set_last_sample_enabled(_engine, enabled ? 1 : 0);
            }
        }

        public void ForceExpose()
        {
            lock (_sync)
            {
                EnsureReady();
                NativePlayback.tsplay_force_expose(_engine);
            }
        }

        public void StepFrame(int frames)
        {
            lock (_sync)
            {
                EnsureReady();
                ThrowIfFailed(NativePlayback.tsplay_step_frame(_engine, frames));
            }
        }

        public void SetRotationDegrees(int degrees)
        {
            lock (_sync)
            {
                EnsureReady();
                ThrowIfFailed(NativePlayback.TSPlayback_SetRotationDegrees(_engine, degrees));
            }
        }

        public int GetRotationDegrees()
        {
            lock (_sync)
            {
                return _engine == IntPtr.Zero ? 0 : NativePlayback.TSPlayback_GetRotationDegrees(_engine);
            }
        }

        public double GetPositionSeconds()
        {
            lock (_sync)
            {
                return _engine == IntPtr.Zero ? 0 : NativePlayback.tsplay_get_position_seconds(_engine);
            }
        }

        public double GetDurationSeconds()
        {
            lock (_sync)
            {
                return _engine == IntPtr.Zero ? 0 : NativePlayback.tsplay_get_duration_seconds(_engine);
            }
        }

        public int GetState()
        {
            lock (_sync)
            {
                return _engine == IntPtr.Zero ? 0 : NativePlayback.tsplay_get_state(_engine);
            }
        }

        public bool HasReachedEos()
        {
            lock (_sync)
            {
                return _engine != IntPtr.Zero && NativePlayback.tsplay_has_reached_eos(_engine) != 0;
            }
        }

        public (int width, int height) GetVideoSize()
        {
            lock (_sync)
            {
                if (_engine == IntPtr.Zero)
                    return (0, 0);

                return (
                    NativePlayback.tsplay_get_video_width(_engine),
                    NativePlayback.tsplay_get_video_height(_engine));
            }
        }

        public void EnsureNativeDllPresent(string baseDirectory)
        {
            string path = Path.Combine(baseDirectory, "native", "win-x64", "TSVmsPlaybackEngine.dll");
            if (!File.Exists(path))
                throw new FileNotFoundException("Native playback DLL not found. Build/copy TSVmsPlaybackEngine.dll first.", path);
        }

        private void EnsureCreated()
        {
            if (_engine != IntPtr.Zero)
                return;

            _engine = NativePlayback.tsplay_create();
            if (_engine == IntPtr.Zero)
                throw new InvalidOperationException("Failed to create playback engine.");
        }

        public void ResetEngine()
        {
            lock (_sync)
            {
                if (_engine != IntPtr.Zero)
                {
                    NativePlayback.tsplay_destroy(_engine);
                    _engine = IntPtr.Zero;
                }

                _initialized = false;
            }
        }

        private void EnsureReady()
        {
            EnsureCreated();

            if (!_initialized && _hostHandle != IntPtr.Zero)
            {
                ThrowIfFailed(NativePlayback.tsplay_initialize(_engine, _hostHandle));
                ThrowIfFailed(NativePlayback.tsplay_set_window_handle(_engine, _hostHandle));
                _initialized = true;
            }

            if (!_initialized)
                throw new InvalidOperationException("Playback engine is not initialized.");
        }

        private void ThrowIfFailed(int code)
        {
            if (code != 0)
                return;

            string message = "Playback engine operation failed.";
            if (_engine != IntPtr.Zero)
            {
                var ptr = NativePlayback.tsplay_get_last_error(_engine);
                if (ptr != IntPtr.Zero)
                {
                    string? native = Marshal.PtrToStringUni(ptr);
                    if (!string.IsNullOrWhiteSpace(native))
                        message = native;
                }
            }

            throw new InvalidOperationException(message);
        }

        public void Dispose()
        {
            lock (_sync)
            {
                if (_engine != IntPtr.Zero)
                {
                    NativePlayback.tsplay_destroy(_engine);
                    _engine = IntPtr.Zero;
                }

                _initialized = false;
                _hostHandle = IntPtr.Zero;
            }
        }
    }
}
