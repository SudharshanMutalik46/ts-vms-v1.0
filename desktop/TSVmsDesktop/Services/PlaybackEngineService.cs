using System;
using System.ComponentModel;
using System.IO;
using System.Runtime.InteropServices;
using TSVmsDesktop.Interop;

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

        public void Play()
        {
            lock (_sync)
            {
                EnsureReady();
                ThrowIfFailed(NativePlayback.tsplay_play(_engine));
            }
        }

        public void Pause()
        {
            lock (_sync)
            {
                EnsureReady();
                ThrowIfFailed(NativePlayback.tsplay_pause(_engine));
            }
        }

        public void Stop()
        {
            lock (_sync)
            {
                EnsureReady();
                ThrowIfFailed(NativePlayback.tsplay_stop(_engine));
            }
        }

        public void Seek(double seconds)
        {
            lock (_sync)
            {
                EnsureReady();
                ThrowIfFailed(NativePlayback.tsplay_seek_seconds(_engine, seconds));
            }
        }

        public void SetRate(double rate)
        {
            lock (_sync)
            {
                EnsureReady();
                ThrowIfFailed(NativePlayback.tsplay_set_rate(_engine, rate));
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

        public void EnsureNativeDllPresent(string baseDirectory)
        {
            string path = Path.Combine(baseDirectory, "native", "win-x64", "TSVmsPlaybackEngine.dll");
            if (!File.Exists(path))
                throw new FileNotFoundException("Native playback DLL not found. Build/copy TSVmsPlaybackEngine.dll first.", path);
        }

        private void EnsureCreated()
        {
            if (_engine == IntPtr.Zero)
                _engine = NativePlayback.tsplay_create();
        }

        private void EnsureReady()
        {
            EnsureCreated();
            if (!_initialized)
                throw new InvalidOperationException("Playback host is not attached yet.");
        }

        private void ThrowIfFailed(int result)
        {
            if (result != 0) return;
            string error = _engine == IntPtr.Zero ? "Native playback engine is unavailable." : Marshal.PtrToStringUni(NativePlayback.tsplay_get_last_error(_engine)) ?? "Native playback operation failed.";
            throw new InvalidOperationException(error);
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
            }
        }
    }
}
