cmake_minimum_required(VERSION 3.15)
project(vms-mosaic)

find_package(PkgConfig REQUIRED)
pkg_check_modules(GST REQUIRED gstreamer-1.0 gstreamer-rtsp-server-1.0)

add_executable(vms-mosaic main.cpp)

target_include_directories(vms-mosaic PRIVATE ${GST_INCLUDE_DIRS})

# Explicitly tell the MSVC linker where to find the .lib files
target_link_directories(vms-mosaic PRIVATE ${GST_LIBRARY_DIRS})

# Fallback for standard Windows GStreamer installations
if(WIN32 AND "$ENV{GSTREAMER_1_0_ROOT_MSVC_X86_64}" MATCHES ".")
    target_link_directories(vms-mosaic PRIVATE "$ENV{GSTREAMER_1_0_ROOT_MSVC_X86_64}/lib")
endif()

target_link_libraries(vms-mosaic PRIVATE ${GST_LIBRARIES})
