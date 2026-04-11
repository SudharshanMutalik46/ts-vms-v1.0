#pragma once

#include <windows.h>

#ifdef TSVmsPlaybackEngine_EXPORTS
#define TSVMS_PLAYBACK_API __declspec(dllexport)
#else
#define TSVMS_PLAYBACK_API __declspec(dllimport)
#endif

extern "C" {

TSVMS_PLAYBACK_API void* tsplay_create();
TSVMS_PLAYBACK_API void  tsplay_destroy(void* engine);

TSVMS_PLAYBACK_API int   tsplay_initialize(void* engine, HWND hwnd);
TSVMS_PLAYBACK_API int   tsplay_set_window_handle(void* engine, HWND hwnd);
TSVMS_PLAYBACK_API int   tsplay_set_window_size(void* engine, int width, int height);
TSVMS_PLAYBACK_API int   tsplay_set_media_path(void* engine, const wchar_t* path);
TSVMS_PLAYBACK_API int   tsplay_play(void* engine);
TSVMS_PLAYBACK_API int   tsplay_pause(void* engine);
TSVMS_PLAYBACK_API int   tsplay_stop(void* engine);
TSVMS_PLAYBACK_API int   tsplay_seek_seconds(void* engine, double seconds);
TSVMS_PLAYBACK_API int   tsplay_set_rate(void* engine, double rate);
TSVMS_PLAYBACK_API int   tsplay_step_frame(void* engine, int frames);
TSVMS_PLAYBACK_API int   tsplay_set_playlist(void* engine, const wchar_t* const* paths, int count, int startIndex);
TSVMS_PLAYBACK_API int   tsplay_get_playlist_index(void* engine);
TSVMS_PLAYBACK_API double tsplay_get_rate(void* engine);
TSVMS_PLAYBACK_API int   tsplay_set_last_sample_enabled(void* engine, int enabled);
TSVMS_PLAYBACK_API int   tsplay_force_expose(void* engine);


TSVMS_PLAYBACK_API double tsplay_get_position_seconds(void* engine);
TSVMS_PLAYBACK_API double tsplay_get_duration_seconds(void* engine);
TSVMS_PLAYBACK_API int    tsplay_get_state(void* engine);
TSVMS_PLAYBACK_API int    tsplay_has_reached_eos(void* engine);
TSVMS_PLAYBACK_API int    tsplay_get_video_width(void* engine);
TSVMS_PLAYBACK_API int    tsplay_get_video_height(void* engine);
TSVMS_PLAYBACK_API const wchar_t* tsplay_get_last_error(void* engine);

TSVMS_PLAYBACK_API int TSPlayback_SetRotationDegrees(void* engine, int degrees);
TSVMS_PLAYBACK_API int TSPlayback_GetRotationDegrees(void* engine);

}
