cmake_minimum_required(VERSION 3.20)
project(TSVmsPlaybackEngine LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_WINDOWS_EXPORT_ALL_SYMBOLS ON)

if(NOT DEFINED ENV{GSTREAMER_1_0_ROOT_MSVC_X86_64})
  message(FATAL_ERROR "GSTREAMER_1_0_ROOT_MSVC_X86_64 is not set.")
endif()

set(GST_ROOT $ENV{GSTREAMER_1_0_ROOT_MSVC_X86_64})
set(GST_INCLUDE_DIRS
  ${GST_ROOT}/include/gstreamer-1.0
  ${GST_ROOT}/include/glib-2.0
  ${GST_ROOT}/lib/glib-2.0/include
)
set(GST_LIB_DIR ${GST_ROOT}/lib)

add_library(TSVmsPlaybackEngine SHARED
  TSVmsPlaybackEngine.cpp
)

target_include_directories(TSVmsPlaybackEngine PRIVATE ${GST_INCLUDE_DIRS})
target_link_directories(TSVmsPlaybackEngine PRIVATE ${GST_LIB_DIR})

target_link_libraries(TSVmsPlaybackEngine PRIVATE
  gstreamer-1.0.lib
  gstvideo-1.0.lib
  gstbase-1.0.lib
  gobject-2.0.lib
  glib-2.0.lib
)

target_compile_definitions(TSVmsPlaybackEngine PRIVATE WIN32_LEAN_AND_MEAN NOMINMAX)
