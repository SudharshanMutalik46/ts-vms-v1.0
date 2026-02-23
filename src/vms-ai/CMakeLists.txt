cmake_minimum_required(VERSION 3.20)
project(vms-ai LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# Dependencies via vcpkg
find_package(nlohmann_json CONFIG REQUIRED)
find_package(OpenSSL REQUIRED)
find_package(Threads REQUIRED)
find_package(cnats CONFIG REQUIRED)
# ONNX Runtime is often manual, but let's see if vcpkg handles it. 
# If not, we'll verify includes manually.
find_package(onnxruntime CONFIG REQUIRED)

# Include directories
include_directories(include)

# Source files
file(GLOB SOURCES "src/*.cpp")

# Executable
add_executable(vms-ai ${SOURCES})

# Link libraries
target_link_libraries(vms-ai PRIVATE 
    nlohmann_json::nlohmann_json 
    cnats::nats
    onnxruntime::onnxruntime
    ws2_32
    winhttp
    windowscodecs
    ole32
    shlwapi
)

# Copy DLLs to output directory (Custom command to simulate "install")
# This is a placeholder; real copy happens in build script
