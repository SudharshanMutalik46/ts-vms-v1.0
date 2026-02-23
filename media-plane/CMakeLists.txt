cmake_minimum_required(VERSION 3.20)
project(vms-media LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# Find dependencies
find_package(gRPC CONFIG REQUIRED)
find_package(Protobuf CONFIG REQUIRED)
find_package(spdlog CONFIG REQUIRED)
find_package(nlohmann_json CONFIG REQUIRED)
find_package(prometheus-cpp CONFIG REQUIRED)

# GStreamer hints for Windows
if(WIN32 AND NOT DEFINED GSTREAMER_DIR)
    set(GSTREAMER_DIR "$ENV{GSTREAMER_1_0_ROOT_MSVC_X86_64}")
endif()

if(GSTREAMER_DIR)
    set(PKG_CONFIG_EXECUTABLE "${GSTREAMER_DIR}/bin/pkg-config.exe")
    set(ENV{PKG_CONFIG_PATH} "${GSTREAMER_DIR}/lib/pkgconfig")
endif()

find_package(PkgConfig REQUIRED)
pkg_check_modules(GSTREAMER REQUIRED gstreamer-1.0>=1.20)
pkg_check_modules(GSTREAMER_BASE REQUIRED gstreamer-base-1.0>=1.20)
pkg_check_modules(GSTREAMER_APP REQUIRED gstreamer-app-1.0>=1.20)
pkg_check_modules(GSTREAMER_VIDEO REQUIRED gstreamer-video-1.0>=1.20)

link_directories(
    ${GSTREAMER_LIBRARY_DIRS}
    ${GSTREAMER_BASE_LIBRARY_DIRS}
    ${GSTREAMER_APP_LIBRARY_DIRS}
    ${GSTREAMER_VIDEO_LIBRARY_DIRS}
)

message(STATUS "GSTREAMER_LIBRARIES: ${GSTREAMER_LIBRARIES}")
message(STATUS "GSTREAMER_LIBRARY_DIRS: ${GSTREAMER_LIBRARY_DIRS}")

# Protobuf/gRPC codegen
set(PROTO_DIR "${CMAKE_SOURCE_DIR}/../proto")
set(MEDIA_PROTO "${PROTO_DIR}/ts/vms/media/v1/media.proto")
set(COMMON_PROTO "${PROTO_DIR}/ts/vms/common/v1/common.proto")

# Output directory for generated files
set(GEN_DIR "${CMAKE_BINARY_DIR}/generated")
file(MAKE_DIRECTORY ${GEN_DIR})

get_filename_component(media_proto_path "${MEDIA_PROTO}" ABSOLUTE)
get_filename_component(proto_root "${PROTO_DIR}" ABSOLUTE)

add_custom_command(
    OUTPUT "${GEN_DIR}/ts/vms/media/v1/media.pb.cc"
           "${GEN_DIR}/ts/vms/media/v1/media.pb.h"
           "${GEN_DIR}/ts/vms/media/v1/media.grpc.pb.cc"
           "${GEN_DIR}/ts/vms/media/v1/media.grpc.pb.h"
           "${GEN_DIR}/ts/vms/common/v1/common.pb.cc"
           "${GEN_DIR}/ts/vms/common/v1/common.pb.h"
    COMMAND  protobuf::protoc
    ARGS --grpc_out="${GEN_DIR}"
         --cpp_out="${GEN_DIR}"
         -I "${proto_root}"
         --plugin=protoc-gen-grpc=$<TARGET_FILE:gRPC::grpc_cpp_plugin>
         "${media_proto_path}"
         "${proto_root}/ts/vms/common/v1/common.proto"
    DEPENDS "${media_proto_path}" "${proto_root}/ts/vms/common/v1/common.proto"
)

# Include directories
include_directories(
    src
    ${GEN_DIR}
    ${GSTREAMER_INCLUDE_DIRS}
    ${GSTREAMER_BASE_INCLUDE_DIRS}
    ${GSTREAMER_APP_INCLUDE_DIRS}
    ${GSTREAMER_VIDEO_INCLUDE_DIRS}
)

# Source files
set(SOURCES
    src/service/main.cpp
    src/service/media_service.cpp
    src/service/ingest_manager.cpp
    src/service/disk_cleanup.cpp
    src/pipeline/ingest_pipeline.cpp
    src/pipeline/pipeline_fsm.cpp
    src/utils/logger.cpp
    src/utils/metrics.cpp
    "${GEN_DIR}/ts/vms/media/v1/media.pb.cc"
    "${GEN_DIR}/ts/vms/media/v1/media.grpc.pb.cc"
    "${GEN_DIR}/ts/vms/common/v1/common.pb.cc"
)

# Executable
add_executable(vms-media ${SOURCES})

# Links
target_link_libraries(vms-media
    PRIVATE
    gRPC::grpc++
    protobuf::libprotobuf
    spdlog::spdlog
    nlohmann_json::nlohmann_json
    prometheus-cpp::core
    prometheus-cpp::pull
    ${GSTREAMER_LIBRARIES}
    ${GSTREAMER_BASE_LIBRARIES}
    ${GSTREAMER_APP_LIBRARIES}
    ${GSTREAMER_VIDEO_LIBRARIES}
)

# Tests
find_package(GTest CONFIG REQUIRED)
enable_testing()

add_executable(vms-media-tests
    tests/test_hls.cpp
    tests/test_fsm.cpp
    tests/test_utils.cpp
    tests/test_manager.cpp
    src/pipeline/pipeline_fsm.cpp
    src/utils/logger.cpp
    src/service/ingest_manager.cpp
    src/service/disk_cleanup.cpp
    src/pipeline/ingest_pipeline.cpp
    src/utils/metrics.cpp
    "${GEN_DIR}/ts/vms/media/v1/media.pb.cc"
    "${GEN_DIR}/ts/vms/media/v1/media.grpc.pb.cc"
    "${GEN_DIR}/ts/vms/common/v1/common.pb.cc"
)

target_link_libraries(vms-media-tests
    PRIVATE
    GTest::gtest_main
    gRPC::grpc++
    protobuf::libprotobuf
    spdlog::spdlog
    nlohmann_json::nlohmann_json
    prometheus-cpp::core
    prometheus-cpp::pull
    ${GSTREAMER_LIBRARIES}
    ${GSTREAMER_BASE_LIBRARIES}
    ${GSTREAMER_APP_LIBRARIES}
    ${GSTREAMER_VIDEO_LIBRARIES}
)

add_test(NAME AllTests COMMAND vms-media-tests)

# Compilation flags for Windows
if(MSVC)
    target_compile_options(vms-media PRIVATE /W4 /MP)
    target_compile_options(vms-media-tests PRIVATE /W4 /MP)
    target_compile_definitions(vms-media PRIVATE _CRT_SECURE_NO_WARNINGS WIN32_LEAN_AND_MEAN)
    target_compile_definitions(vms-media-tests PRIVATE _CRT_SECURE_NO_WARNINGS WIN32_LEAN_AND_MEAN)
endif()
