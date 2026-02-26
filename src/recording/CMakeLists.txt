cmake_minimum_required(VERSION 3.15)
project(vms-recording-harnesses)

# Enforce C++17 for std::filesystem
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

find_package(PkgConfig REQUIRED)
pkg_check_modules(GST REQUIRED gstreamer-1.0 gstreamer-app-1.0)

# --- Phase 4.3 Segment Writer Harness ---
add_executable(vms-segment-harness 
    SegmentWriterTestHarness.cpp 
    SegmentWriter.cpp 
    StartupScanner.cpp 
    FileSync.cpp
)
target_include_directories(vms-segment-harness PRIVATE ${GST_INCLUDE_DIRS})

target_link_directories(vms-segment-harness PRIVATE ${GST_LIBRARY_DIRS})
if(WIN32 AND "$ENV{GSTREAMER_1_0_ROOT_MSVC_X86_64}" MATCHES ".")
    target_link_directories(vms-segment-harness PRIVATE "$ENV{GSTREAMER_1_0_ROOT_MSVC_X86_64}/lib")
endif()
target_link_libraries(vms-segment-harness PRIVATE ${GST_LIBRARIES})


# --- Phase 4.6 Pre-Buffer Harness ---
add_executable(vms-prebuffer-harness 
    TestHarness46.cpp 
    PreBufferManager.cpp 
    PreBufferRing.cpp 
    BufferedIngress.cpp 
    BackfillController.cpp
)
target_include_directories(vms-prebuffer-harness PRIVATE ${GST_INCLUDE_DIRS})

target_link_directories(vms-prebuffer-harness PRIVATE ${GST_LIBRARY_DIRS})
if(WIN32 AND "$ENV{GSTREAMER_1_0_ROOT_MSVC_X86_64}" MATCHES ".")
    target_link_directories(vms-prebuffer-harness PRIVATE "$ENV{GSTREAMER_1_0_ROOT_MSVC_X86_64}/lib")
endif()
target_link_libraries(vms-prebuffer-harness PRIVATE ${GST_LIBRARIES})


# --- Phase 4.8 Disk I/O Harness ---
add_executable(vms-diskio-harness 
    TestHarness48.cpp 
    diskio/AsyncFileWriter.cpp 
    diskio/IoPriority.cpp 
    diskio/DiskMetrics.cpp 
)
target_include_directories(vms-diskio-harness PRIVATE ${GST_INCLUDE_DIRS})

# Link against Windows PDH dynamically for performance counters
target_link_libraries(vms-diskio-harness PRIVATE pdh)

target_link_directories(vms-diskio-harness PRIVATE ${GST_LIBRARY_DIRS})
if(WIN32 AND "$ENV{GSTREAMER_1_0_ROOT_MSVC_X86_64}" MATCHES ".")
    target_link_directories(vms-diskio-harness PRIVATE "$ENV{GSTREAMER_1_0_ROOT_MSVC_X86_64}/lib")
endif()
target_link_libraries(vms-diskio-harness PRIVATE ${GST_LIBRARIES})
